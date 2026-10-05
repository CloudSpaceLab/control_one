// Package networkdevice provides bounded, read-only appliance probes. It never
// invokes the machine installer or accepts operator-supplied commands.
package networkdevice

import (
	"context"
	"errors"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"golang.org/x/crypto/ssh"
)

type Credential struct {
	Username           string `json:"username"`
	Password           string `json:"password,omitempty"`
	PrivateKey         string `json:"private_key,omitempty"`
	Passphrase         string `json:"passphrase,omitempty"`
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	AuthProtocol       string `json:"auth_protocol,omitempty"`
	AuthSecret         string `json:"auth_secret,omitempty"`
	PrivProtocol       string `json:"priv_protocol,omitempty"`
	PrivSecret         string `json:"priv_secret,omitempty"`
}

func (c Credential) Validate(protocol string) error {
	if c.Username == "" || len(c.Username) > 128 || strings.ContainsAny(c.Username, "\x00\r\n") {
		return errors.New("username is required and must be single-line")
	}
	if protocol == "snmpv3" {
		if c.AuthProtocol != "SHA256" && c.AuthProtocol != "SHA" {
			return errors.New("SNMPv3 authentication must be SHA256 or SHA")
		}
		if c.PrivProtocol != "AES" || len(c.AuthSecret) < 8 || len(c.PrivSecret) < 8 || len(c.AuthSecret) > 255 || len(c.PrivSecret) > 255 {
			return errors.New("SNMPv3 requires AES privacy and auth/priv secrets of 8 to 255 bytes")
		}
		if c.Password != "" || c.PrivateKey != "" || c.Passphrase != "" || c.HostKeyFingerprint != "" {
			return errors.New("SSH fields are not accepted for SNMPv3")
		}
		return nil
	}
	if protocol != "ssh" {
		return errors.New("supported protocols are snmpv3 and ssh")
	}
	if !strings.HasPrefix(c.HostKeyFingerprint, "SHA256:") || len(c.HostKeyFingerprint) != 50 {
		return errors.New("SSH requires a trusted SHA256 host-key fingerprint")
	}
	if (c.Password == "") == (c.PrivateKey == "") {
		return errors.New("provide exactly one SSH password or private key")
	}
	if len(c.Password) > 1024 || len(c.PrivateKey) > 16384 || len(c.Passphrase) > 1024 {
		return errors.New("SSH credential exceeds size limit")
	}
	if c.PrivateKey != "" {
		var err error
		if c.Passphrase != "" {
			_, err = ssh.ParsePrivateKeyWithPassphrase([]byte(c.PrivateKey), []byte(c.Passphrase))
		} else {
			_, err = ssh.ParsePrivateKey([]byte(c.PrivateKey))
		}
		if err != nil {
			return errors.New("invalid SSH private key or passphrase")
		}
	} else if c.Passphrase != "" {
		return errors.New("passphrase requires a private key")
	}
	if c.AuthSecret != "" || c.PrivSecret != "" || c.AuthProtocol != "" || c.PrivProtocol != "" {
		return errors.New("SNMP fields are not accepted for SSH")
	}
	return nil
}

type Result struct {
	State              string   `json:"state"`
	Message            string   `json:"message"`
	Vendor             string   `json:"vendor"`
	Model              string   `json:"model"`
	Platform           string   `json:"platform"`
	SuggestedType      string   `json:"suggested_type"`
	Confidence         int      `json:"confidence"`
	Evidence           []string `json:"evidence"`
	Capabilities       []string `json:"capabilities"`
	RequiredPrivileges string   `json:"required_privileges"`
}

func Outcome(state string) Result {
	messages := map[string]string{
		"authenticated":  "Read-only connection authenticated. Review the detected identity before saving.",
		"auth_failed":    "The device rejected authentication. Check the saved credential.",
		"unreachable":    "No usable response within the connection timeout. Check reachability and protocol configuration; silent credential rejection is also possible with SNMP.",
		"unsupported":    "The endpoint did not provide a supported read-only identity response.",
		"policy_blocked": "Destination or SSH host key is blocked by connection policy.",
	}
	return Result{State: state, Message: messages[state], SuggestedType: "network_appliance", Evidence: []string{}, Capabilities: []string{}}
}

// Resolve all answers before connecting; reject mixed allowed/blocked DNS and
// pin the chosen IP for the entire probe to prevent DNS rebinding.
func Resolve(ctx context.Context, host string, allowedCIDRs []string) (string, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return "", errors.New("unreachable")
	}
	for _, candidate := range ips {
		ip := candidate.IP
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
			return "", errors.New("policy_blocked")
		}
		allowed := false
		for _, raw := range allowedCIDRs {
			_, network, e := net.ParseCIDR(raw)
			if e == nil && network.Contains(ip) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", errors.New("policy_blocked")
		}
	}
	return ips[0].IP.String(), nil
}

// Adapter defines the read-only connection contract. Future NETCONF, RESTCONF
// and vendor API adapters must return normalized, secret-free evidence and
// declare their required privileges before being exposed by onboarding.
type Adapter struct {
	RequiredPrivileges  string
	InventoryPrivileges string
	Probe               func(context.Context, string, int, Credential) Result
	Inventory           func(context.Context, string, int, Credential) Inventory
}

var adapters = map[string]Adapter{
	"ssh":    {RequiredPrivileges: "Read-only login permitted to execute show version; no enable/config privileges.", Probe: probeSSH},
	"snmpv3": {RequiredPrivileges: "SNMPv3 authPriv user with read-only access to sysDescr and sysObjectID.", InventoryPrivileges: "Read-only system, IF/IF-X, ENTITY/SENSOR, LLDP/CDP, IP, Q-BRIDGE and HOST-RESOURCES MIB access where exposed. No SET permissions.", Probe: probeSNMP, Inventory: collectSNMPInventory},
}

func Probe(ctx context.Context, ip string, port int, protocol string, credential Credential) Result {
	adapter, ok := adapters[protocol]
	if !ok {
		return Outcome("unsupported")
	}
	result := adapter.Probe(ctx, ip, port, credential)
	result.RequiredPrivileges = adapter.RequiredPrivileges
	return result
}

type limitedOutput struct{ data []byte }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 32768 {
		return 0, errors.New("identity output limit exceeded")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func probeSSH(ctx context.Context, ip string, port int, c Credential) Result {
	if port == 0 {
		port = 22
	}
	auth := ssh.Password(c.Password)
	if c.PrivateKey != "" {
		var signer ssh.Signer
		var err error
		if c.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(c.PrivateKey), []byte(c.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(c.PrivateKey))
		}
		if err != nil {
			return Outcome("auth_failed")
		}
		auth = ssh.PublicKeys(signer)
	}
	policyBlocked := false
	cfg := &ssh.ClientConfig{User: c.Username, Auth: []ssh.AuthMethod{auth}, Timeout: 8 * time.Second, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if ssh.FingerprintSHA256(key) != c.HostKeyFingerprint {
			policyBlocked = true
			return errors.New("host key mismatch")
		}
		return nil
	}}
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return Outcome("unreachable")
	}
	defer raw.Close()
	deadline := time.Now().Add(8 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = raw.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	conn, chans, reqs, err := ssh.NewClientConn(raw, address, cfg)
	if err != nil {
		if policyBlocked {
			return Outcome("policy_blocked")
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return Outcome("auth_failed")
		}
		var networkErr net.Error
		if errors.As(err, &networkErr) {
			return Outcome("unreachable")
		}
		return Outcome("unsupported")
	}
	client := ssh.NewClient(conn, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return Outcome("unsupported")
	}
	defer session.Close()
	output := &limitedOutput{}
	session.Stdout = output
	session.Stderr = io.Discard
	// Fixed read-only command; no shell, enable, config, or arbitrary commands.
	if err = session.Run("show version"); err != nil {
		return Outcome("unsupported")
	}
	result := Fingerprint(string(output.data), "", c)
	result.RequiredPrivileges = "Read-only login permitted to execute show version; no enable/config privileges."
	result.Capabilities = []string{"ssh_identity"}
	return result
}

func probeSNMP(ctx context.Context, ip string, port int, c Credential) Result {
	if port == 0 {
		port = 161
	}
	auth := gosnmp.SHA256
	if c.AuthProtocol == "SHA" {
		auth = gosnmp.SHA
	}
	client := &gosnmp.GoSNMP{Target: ip, Port: uint16(port), Version: gosnmp.Version3, Timeout: 3 * time.Second, Retries: 0, Context: ctx,
		SecurityModel: gosnmp.UserSecurityModel, MsgFlags: gosnmp.AuthPriv, MaxOids: 3,
		SecurityParameters: &gosnmp.UsmSecurityParameters{UserName: c.Username, AuthenticationProtocol: auth, AuthenticationPassphrase: c.AuthSecret, PrivacyProtocol: gosnmp.AES, PrivacyPassphrase: c.PrivSecret},
	}
	if err := client.Connect(); err != nil {
		return Outcome("unreachable")
	}
	defer client.Conn.Close()
	packet, err := client.Get([]string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0"})

	// USM reports can accompany an error (including engine-discovery retries).
	// Inspect the explicit report before falling back to transport errors.
	if packet != nil && packet.PDUType == gosnmp.Report {
		for _, pdu := range packet.Variables {
			switch strings.TrimPrefix(pdu.Name, ".") {
			case "1.3.6.1.6.3.15.1.1.1.0", "1.3.6.1.6.3.15.1.1.3.0", "1.3.6.1.6.3.15.1.1.5.0", "1.3.6.1.6.3.15.1.1.6.0":
				return Outcome("auth_failed")
			}
		}
		return Outcome("unsupported")
	}
	if err != nil {
		if errors.Is(err, gosnmp.ErrUnknownUsername) || errors.Is(err, gosnmp.ErrWrongDigest) || errors.Is(err, gosnmp.ErrDecryption) || errors.Is(err, gosnmp.ErrUnknownSecurityLevel) {
			return Outcome("auth_failed")
		}
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "authentication") || strings.Contains(lower, "not authentic") || strings.Contains(lower, "unknown user") || strings.Contains(lower, "wrong digest") || strings.Contains(lower, "decryption") {
			return Outcome("auth_failed")
		}
		return Outcome("unreachable")
	}
	if packet == nil {
		return Outcome("unsupported")
	}
	if packet.Error != gosnmp.NoError {
		if packet.Error == gosnmp.AuthorizationError {
			return Outcome("auth_failed")
		}
		return Outcome("unsupported")
	}
	var description, oid string
	for _, pdu := range packet.Variables {
		name := strings.TrimPrefix(pdu.Name, ".")
		if name == "1.3.6.1.2.1.1.1.0" {
			if b, ok := pdu.Value.([]byte); ok {
				description = string(b)
			}
		}
		if name == "1.3.6.1.2.1.1.2.0" {
			if value, ok := pdu.Value.(string); ok {
				oid = value
			}
		}
	}
	if description == "" && oid == "" {
		return Outcome("unsupported")
	}
	result := Fingerprint(description, oid, c)
	result.RequiredPrivileges = "SNMPv3 authPriv user with read-only access to sysDescr and sysObjectID."
	result.Capabilities = []string{"snmp_identity"}
	return result
}

// Persist only normalized evidence. Raw banners/CLI/sysDescr may contain
// secrets or unrelated text and never leave the probe layer.
func Fingerprint(description, oid string, c Credential) Result {
	for _, secret := range []string{c.Password, c.AuthSecret, c.PrivSecret, c.Passphrase, c.PrivateKey} {
		if secret != "" {
			description = strings.ReplaceAll(description, secret, "[redacted]")
		}
	}
	if len(description) > 32768 {
		description = description[:32768]
	}
	lower := strings.ToLower(description)
	r := Outcome("authenticated")
	switch {
	case strings.Contains(lower, "cisco") || strings.HasPrefix(oid, ".1.3.6.1.4.1.9.") || strings.HasPrefix(oid, "1.3.6.1.4.1.9."):
		r.Vendor = "Cisco"
		r.Confidence = 70
		r.Evidence = []string{"Cisco vendor signature in protocol identity"}
		if strings.Contains(lower, "ios xe") || strings.Contains(lower, "ios-xe") {
			r.Platform = "IOS XE"
		} else if strings.Contains(lower, "ios software") || strings.Contains(lower, "internetwork operating system") {
			r.Platform = "IOS"
		}
		if strings.Contains(lower, "nx-os") {
			r.Platform = "NX-OS"
		}
		if strings.Contains(lower, "catalyst") || strings.Contains(lower, "c9300") {
			r.SuggestedType = "switch"
			r.Confidence = 90
		}
		if strings.Contains(lower, "adaptive security") {
			r.Platform = "ASA"
			r.SuggestedType = "firewall"
			r.Confidence = 90
		}
	case strings.Contains(lower, "fortigate") || strings.Contains(lower, "fortios"):
		r.Vendor = "Fortinet"
		r.Platform = "FortiOS"
		r.SuggestedType = "firewall"
		r.Confidence = 95
		r.Evidence = []string{"FortiGate/FortiOS signature in protocol identity"}
	case strings.Contains(lower, "junos"):
		r.Vendor = "Juniper"
		r.Platform = "Junos"
		r.Confidence = 70
		r.Evidence = []string{"Junos signature in protocol identity"}
	case strings.Contains(lower, "arista"):
		r.Vendor = "Arista"
		r.Platform = "EOS"
		r.SuggestedType = "switch"
		r.Confidence = 80
		r.Evidence = []string{"Arista signature in protocol identity"}
	case strings.Contains(lower, "pan-os"):
		r.Vendor = "Palo Alto Networks"
		r.Platform = "PAN-OS"
		r.SuggestedType = "firewall"
		r.Confidence = 90
		r.Evidence = []string{"PAN-OS signature in protocol identity"}
	case strings.Contains(lower, "big-ip"):
		r.Vendor = "F5"
		r.Platform = "BIG-IP"
		r.SuggestedType = "load_balancer"
		r.Confidence = 80
		r.Evidence = []string{"BIG-IP signature in protocol identity"}
	default:
		r.Evidence = []string{"Authenticated protocol identity; no supported vendor signature. Operator review required."}
	}
	for _, pattern := range []string{`(?i)\b(C9[0-9]{3}[A-Z0-9-]*)\b`, `(?i)\b(FortiGate-[A-Z0-9-]+)\b`, `(?i)\bmodel\s*:\s*([A-Z0-9-]{2,40})\b`} {
		match := regexp.MustCompile(pattern).FindStringSubmatch(description)
		if len(match) > 1 {
			r.Model = match[1]
			break
		}
	}
	return r
}
