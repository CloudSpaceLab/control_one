package networkdevice

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// ManagementConfig lives only at the site collector. Operators select a bound
// source, never supply executable commands or device credentials to the API.
type ManagementConfig struct {
	Port               int    `yaml:"port"`
	Adapter            string `yaml:"adapter"`
	Username           string `yaml:"username"`
	PasswordEnv        string `yaml:"password_env"`
	PrivateKeyEnv      string `yaml:"private_key_env"`
	PassphraseEnv      string `yaml:"passphrase_env"`
	HostKeyFingerprint string `yaml:"host_key_fingerprint"`
	Path               string `yaml:"path"`
	TokenEnv           string `yaml:"token_env"`
	CAFile             string `yaml:"ca_file"`
}

func (m ManagementConfig) Credential() Credential {
	return Credential{Username: m.Username, Password: os.Getenv(m.PasswordEnv), PrivateKey: os.Getenv(m.PrivateKeyEnv), Passphrase: os.Getenv(m.PassphraseEnv), HostKeyFingerprint: m.HostKeyFingerprint}
}
func (m ManagementConfig) Validate(source string) error {
	if m.Port < 0 || m.Port > 65535 {
		return errors.New("invalid management port")
	}
	switch source {
	case "ssh_config", "netconf":
		if err := m.Credential().Validate("ssh"); err != nil {
			return err
		}
		if source == "ssh_config" {
			if _, ok := snapshotCommands[m.Adapter]; !ok {
				return errors.New("snapshot adapter must be cisco, juniper, or fortinet")
			}
		}
	case "restconf", "vendor_api":
		u, err := url.Parse(m.Path)
		if err != nil || u.Host != "" || u.Scheme != "" || !strings.HasPrefix(m.Path, "/") || strings.HasPrefix(m.Path, "//") || u.Fragment != "" || strings.ContainsAny(m.Path, "\r\n") {
			return errors.New("management path must be relative to the device")
		}
		if source == "restconf" && !strings.HasPrefix(u.Path, "/restconf/data") {
			return errors.New("RESTCONF collection requires a data resource")
		}
		if (m.TokenEnv == "") == (m.PasswordEnv == "") || (m.TokenEnv != "" && os.Getenv(m.TokenEnv) == "") || (m.PasswordEnv != "" && (m.Username == "" || os.Getenv(m.PasswordEnv) == "")) {
			return errors.New("exactly one local API credential required")
		}
	default:
		return errors.New("unsupported management source")
	}
	return nil
}

var snapshotCommands = map[string]string{"cisco": "show running-config", "juniper": "show configuration | display set", "fortinet": "show full-configuration"}

func managementSSH(ctx context.Context, ip string, port int, c Credential) (*ssh.Client, func(), string) {
	if c.Validate("ssh") != nil {
		return nil, func() {}, "auth_failed"
	}
	method := ssh.Password(c.Password)
	if c.PrivateKey != "" {
		var signer ssh.Signer
		var err error
		if c.Passphrase == "" {
			signer, err = ssh.ParsePrivateKey([]byte(c.PrivateKey))
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(c.PrivateKey), []byte(c.Passphrase))
		}
		if err != nil {
			return nil, func() {}, "auth_failed"
		}
		method = ssh.PublicKeys(signer)
	}
	blocked := false
	cfg := &ssh.ClientConfig{User: c.Username, Auth: []ssh.AuthMethod{method}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if ssh.FingerprintSHA256(key) != c.HostKeyFingerprint {
			blocked = true
			return errors.New("host key mismatch")
		}
		return nil
	}}
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, func() {}, "unreachable"
	}
	deadline := time.Now().Add(8 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = raw.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	cleanup := func() { stop(); _ = raw.Close() }
	conn, chans, reqs, err := ssh.NewClientConn(raw, address, cfg)
	if err != nil {
		cleanup()
		if blocked {
			return nil, func() {}, "policy_blocked"
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, func() {}, "auth_failed"
		}
		return nil, func() {}, "unreachable"
	}
	return ssh.NewClient(conn, chans, reqs), cleanup, "ready"
}

// PollManagement implements GET-only HTTPS, fixed CLI snapshots, and NETCONF
// get-config. No enable, edit-config, exec API, or arbitrary SSH command exists.
func PollManagement(ctx context.Context, host, ip, source string, m ManagementConfig) (string, []map[string]any) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if m.Validate(source) != nil {
		return "auth_failed", nil
	}
	if source == "restconf" || source == "vendor_api" {
		return pollHTTPS(ctx, host, ip, source, m)
	}
	port := m.Port
	if port == 0 {
		if source == "netconf" {
			port = 830
		} else {
			port = 22
		}
	}
	client, cleanup, state := managementSSH(ctx, ip, port, m.Credential())
	defer cleanup()
	if client == nil {
		return state, nil
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "unsupported", nil
	}
	defer session.Close()
	var raw []byte
	if source == "ssh_config" {
		output := &snapshotOutput{}
		session.Stdout = output
		session.Stderr = io.Discard
		if session.Run(snapshotCommands[m.Adapter]) != nil || output.overflow {
			return "unsupported", nil
		}
		raw = output.data
	} else {
		raw, err = readNETCONF(session)
		if err != nil {
			return "unsupported", nil
		}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "unsupported", nil
	}
	format := "text"
	if source == "netconf" {
		format = "xml"
	}
	record, err := snapshotRecord(raw, format, []string{m.Credential().Password, m.Credential().PrivateKey, m.Credential().Passphrase})
	if err != nil {
		return "unsupported", nil
	}
	record["adapter"] = snapshotAdapter(source, m.Adapter)
	record["adapter_version"] = "management-read/v1"
	return "ready", []map[string]any{record}
}

func snapshotAdapter(source, adapter string) string {
	if source == "ssh_config" {
		return "ssh_config/" + adapter
	}
	return source
}

type snapshotOutput struct {
	data     []byte
	overflow bool
}

func (w *snapshotOutput) Write(p []byte) (int, error) {
	if len(w.data)+len(p) > 262144 {
		w.overflow = true
		return 0, errors.New("snapshot too large")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

func readNETCONF(s *ssh.Session) ([]byte, error) {
	in, err := s.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := s.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = s.RequestSubsystem("netconf"); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(out)
	hello := `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities><capability>urn:ietf:params:netconf:base:1.0</capability><capability>urn:ietf:params:netconf:base:1.1</capability></capabilities></hello>]]>]]>`
	if _, err = io.WriteString(in, hello); err != nil {
		return nil, err
	}
	peer, err := readNETCONFFrame(reader)
	if err != nil {
		return nil, err
	}
	var h struct {
		XMLName      xml.Name
		Capabilities []string `xml:"capabilities>capability"`
	}
	if xml.Unmarshal(peer, &h) != nil || h.XMLName.Local != "hello" {
		return nil, errors.New("invalid NETCONF hello")
	}
	supported, chunked := false, false
	for _, capability := range h.Capabilities {
		if capability == "urn:ietf:params:netconf:base:1.0" {
			supported = true
		}
		if capability == "urn:ietf:params:netconf:base:1.1" {
			supported, chunked = true, true
		}
	}
	if !supported {
		return nil, errors.New("NETCONF base 1.0 required")
	}
	request := `<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><get-config><source><running/></source></get-config></rpc>`
	if err = writeNETCONFFrame(in, request, chunked); err != nil {
		return nil, err
	}
	var reply []byte
	if chunked {
		reply, err = readNETCONFChunks(reader)
	} else {
		reply, err = readNETCONFFrame(reader)
	}
	if err != nil {
		return nil, err
	}
	var r struct {
		XMLName   xml.Name
		MessageID string `xml:"message-id,attr"`
		Data      *struct {
			Inner string `xml:",innerxml"`
		} `xml:"data"`
		Error *struct{} `xml:"rpc-error"`
	}
	if xml.Unmarshal(reply, &r) != nil || r.XMLName.Local != "rpc-reply" || r.MessageID != "1" || r.Error != nil || r.Data == nil {
		return nil, errors.New("NETCONF read rejected")
	}
	_ = writeNETCONFFrame(in, `<rpc message-id="2" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><close-session/></rpc>`, chunked)
	return []byte("<data>" + r.Data.Inner + "</data>"), nil
}
func readNETCONFFrame(r *bufio.Reader) ([]byte, error) {
	result := []byte{}
	for len(result) <= 262144 {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		result = append(result, b)
		if bytes.HasSuffix(result, []byte("]]>]]>")) {
			return result[:len(result)-6], nil
		}
	}
	return nil, errors.New("NETCONF frame exceeds limit")
}

func writeNETCONFFrame(w io.Writer, body string, chunked bool) error {
	if chunked {
		body = "\n#" + strconv.Itoa(len(body)) + "\n" + body + "\n##\n"
	} else {
		body += "]]>]]>"
	}
	_, err := io.WriteString(w, body)
	return err
}
func readNETCONFChunks(r *bufio.Reader) ([]byte, error) {
	result := []byte{}
	for {
		for _, expected := range []byte{'\n', '#'} {
			b, err := r.ReadByte()
			if err != nil || b != expected {
				return nil, errors.New("invalid NETCONF chunk header")
			}
		}
		size := []byte{}
		for len(size) <= 10 {
			b, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			if b == '\n' {
				break
			}
			size = append(size, b)
		}
		if string(size) == "#" {
			if len(result) == 0 {
				return nil, errors.New("empty NETCONF chunks")
			}
			return result, nil
		}
		if len(size) == 0 || size[0] == '0' {
			return nil, errors.New("invalid NETCONF chunk size")
		}
		count, err := strconv.Atoi(string(size))
		if err != nil || count < 1 || len(result)+count > 262144 {
			return nil, errors.New("NETCONF chunks exceed limit")
		}
		chunk := make([]byte, count)
		if _, err = io.ReadFull(r, chunk); err != nil {
			return nil, err
		}
		result = append(result, chunk...)
	}
}
func pollHTTPS(ctx context.Context, host, ip, source string, m ManagementConfig) (string, []map[string]any) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if m.CAFile != "" {
		pem, e := os.ReadFile(m.CAFile)
		if e != nil || !roots.AppendCertsFromPEM(pem) {
			return "policy_blocked", nil
		}
	}
	port := m.Port
	if port == 0 {
		port = 443
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	u := "https://" + net.JoinHostPort(host, strconv.Itoa(port)) + m.Path
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "policy_blocked", nil
	}
	if m.TokenEnv != "" {
		request.Header.Set("Authorization", "Bearer "+os.Getenv(m.TokenEnv))
	} else {
		request.SetBasicAuth(m.Username, os.Getenv(m.PasswordEnv))
	}
	request.Header.Set("Accept", "application/json")
	if source == "restconf" {
		request.Header.Set("Accept", "application/yang-data+json, application/yang-data+xml")
	}
	response, err := client.Do(request)
	if err != nil {
		var certErr *tls.CertificateVerificationError
		if errors.As(err, &certErr) {
			return "policy_blocked", nil
		}
		return "unreachable", nil
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return "auth_failed", nil
	}
	if response.StatusCode != 200 {
		return "unsupported", nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 262145))
	if err != nil || len(raw) > 262144 {
		return "unsupported", nil
	}
	format := "json"
	if strings.Contains(response.Header.Get("Content-Type"), "xml") {
		format = "xml"
	}
	record, err := snapshotRecord(raw, format, []string{os.Getenv(m.TokenEnv), os.Getenv(m.PasswordEnv)})
	if err != nil {
		return "unsupported", nil
	}
	record["adapter"] = source
	record["adapter_version"] = "management-read/v1"
	return "ready", []map[string]any{record}
}

func sensitiveField(key string) bool {
	key = strings.ToLower(key)
	for _, word := range []string{"password", "secret", "community", "token", "private-key", "private_key", "credential", "authentication", "auth", "priv", "encryption-key", "encryption_key", "passphrase", "key-string", "key_string", "authorization", "pre-shared", "pre_shared", "psk"} {
		if strings.Contains(key, word) {
			return true
		}
	}
	return key == "key" || strings.HasPrefix(key, "key ") || strings.Contains(key, " key ") || strings.HasSuffix(key, " key")
}

// Snapshots are sanitized before hashing and forwarding. Bodies exceeding the
// bounded journal record size are represented by a digest and explicit omission.
func snapshotRecord(raw []byte, format string, secrets []string) (map[string]any, error) {
	var clean []byte
	switch format {
	case "json":
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return nil, errors.New("invalid JSON")
		}
		redactJSON(value)
		clean, _ = json.Marshal(value)
	case "xml":
		var out bytes.Buffer
		decoder := xml.NewDecoder(bytes.NewReader(raw))
		encoder := xml.NewEncoder(&out)
		depth := 0
		for {
			tok, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			switch el := tok.(type) {
			case xml.StartElement:
				if depth > 0 {
					depth++
					continue
				}
				if sensitiveField(el.Name.Local) {
					depth = 1
					continue
				}
				for i := range el.Attr {
					if sensitiveField(el.Attr[i].Name.Local) {
						el.Attr[i].Value = "[redacted]"
					}
				}
				tok = el
			case xml.EndElement:
				if depth > 0 {
					depth--
					continue
				}
			}
			if depth == 0 {
				if err = encoder.EncodeToken(tok); err != nil {
					return nil, err
				}
			}
		}
		if encoder.Flush() != nil {
			return nil, errors.New("invalid XML")
		}
		clean = out.Bytes()
	default:
		text := strings.ReplaceAll(strings.ReplaceAll(strings.ToValidUTF8(string(raw), ""), "\r\n", "\n"), "\r", "\n")
		lines := strings.Split(text, "\n")
		private := false
		for i, line := range lines {
			line = strings.TrimRight(line, " \t")
			if strings.Contains(line, "BEGIN ") && strings.Contains(line, "PRIVATE KEY") {
				private = true
			}
			if private || sensitiveField(line) {
				lines[i] = "[redacted]"
			}
			if strings.Contains(line, "END ") && strings.Contains(line, "PRIVATE KEY") {
				private = false
			}
		}
		clean = []byte(strings.Join(lines, "\n"))
	}
	for _, secret := range secrets {
		if secret != "" {
			clean = bytes.ReplaceAll(clean, []byte(secret), []byte("[redacted]"))
		}
	}
	digest := sha256.Sum256(clean)
	record := map[string]any{"format": format, "sha256": hex.EncodeToString(digest[:]), "bytes": len(raw), "sanitized": true}
	if len(clean) <= 16384 {
		record["snapshot"] = string(clean)
	} else {
		record["snapshot_omitted"] = "sanitized snapshot exceeds 16 KiB"
	}
	return record, nil
}
func redactJSON(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if sensitiveField(key) {
				v[key] = "[redacted]"
			} else {
				redactJSON(child)
			}
		}
	case []any:
		for _, child := range v {
			redactJSON(child)
		}
	}
}
