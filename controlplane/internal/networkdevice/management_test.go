package networkdevice

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestManagementSSHAndNETCONFReadOnlyTransport(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(key)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveManagementFixture(conn, signer)
		}
	}()
	_, p, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(p)
	t.Setenv("MANAGEMENT_PASSWORD", "fixture-pass")
	m := ManagementConfig{Port: port, Adapter: "cisco", Username: "readonly", PasswordEnv: "MANAGEMENT_PASSWORD", HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey())}
	for _, source := range []string{"ssh_config", "netconf"} {
		t.Run(source, func(t *testing.T) {
			state, records := PollManagement(context.Background(), "127.0.0.1", "127.0.0.1", source, m)
			require.Equal(t, "ready", state)
			require.Len(t, records, 1)
			require.Contains(t, records[0]["snapshot"], "fixture-switch")
			require.NotContains(t, records[0]["snapshot"], "TOP_SECRET")
			require.NotContains(t, records[0]["snapshot"], "fixture-pass")
		})
	}
	t.Setenv("MANAGEMENT_PASSWORD", "wrong")
	state, _ := PollManagement(context.Background(), "127.0.0.1", "127.0.0.1", "ssh_config", m)
	require.Equal(t, "auth_failed", state)
	t.Setenv("MANAGEMENT_PASSWORD", "fixture-pass")
	m.HostKeyFingerprint = "SHA256:" + strings.Repeat("x", 43)
	state, _ = PollManagement(context.Background(), "127.0.0.1", "127.0.0.1", "netconf", m)
	require.Equal(t, "policy_blocked", state)
}
func serveManagementFixture(raw net.Conn, signer ssh.Signer) {
	defer raw.Close()
	cfg := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
		if meta.User() == "readonly" && string(pass) == "fixture-pass" {
			return nil, nil
		}
		return nil, io.EOF
	}}
	cfg.AddHostKey(signer)
	_, channels, requests, err := ssh.NewServerConn(raw, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for pending := range channels {
		if pending.ChannelType() != "session" {
			_ = pending.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, requests, err := pending.Accept()
		if err != nil {
			return
		}
		for request := range requests {
			var payload struct{ Value string }
			_ = ssh.Unmarshal(request.Payload, &payload)
			if request.Type == "exec" && payload.Value == "show running-config" {
				_ = request.Reply(true, nil)
				_, _ = io.WriteString(ch, "hostname fixture-switch\nusername admin secret TOP_SECRET\nlogin password fixture-pass\ninterface Gi1\n")
				code := make([]byte, 4)
				binary.BigEndian.PutUint32(code, 0)
				_, _ = ch.SendRequest("exit-status", false, code)
				_ = ch.Close()
				break
			}
			if request.Type == "subsystem" && payload.Value == "netconf" {
				_ = request.Reply(true, nil)
				_, _ = io.WriteString(ch, `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities><capability>urn:ietf:params:netconf:base:1.0</capability></capabilities><session-id>1</session-id></hello>]]>]]>`)
				reader := bufio.NewReader(ch)
				_, err = readNETCONFFrame(reader)
				if err != nil {
					_ = ch.Close()
					break
				}
				rpc, err := readNETCONFFrame(reader)
				if err == nil && strings.Contains(string(rpc), "<get-config><source><running/></source></get-config>") {
					_, _ = io.WriteString(ch, `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><data><hostname>fixture-switch</hostname><password>TOP_SECRET</password></data></rpc-reply>]]>]]>`)
					_, _ = readNETCONFFrame(reader)
				}
				_ = ch.Close()
				break
			}
			_ = request.Reply(false, nil)
		}
	}
}

func TestHTTPSManagementTLSIdentityAuthenticationAndRedaction(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://example.com", 302)
			return
		}
		w.Header().Set("Content-Type", "application/yang-data+json")
		_, _ = io.WriteString(w, `{"interfaces":[{"name":"Gi1","password":"TOP_SECRET"}],"api_token":"TOP_SECRET","name":"fixture-switch"}`)
	}))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	host, p, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	port, _ := strconv.Atoi(p)
	t.Setenv("MANAGEMENT_TOKEN", "fixture-token")
	m := ManagementConfig{Port: port, TokenEnv: "MANAGEMENT_TOKEN", Path: "/restconf/data/ietf-interfaces:interfaces", CAFile: ca}
	for _, source := range []string{"restconf", "vendor_api"} {
		state, records := PollManagement(context.Background(), host, host, source, m)
		require.Equal(t, "ready", state)
		require.Len(t, records, 1)
		require.Contains(t, records[0]["snapshot"], "Gi1")
		require.NotContains(t, records[0]["snapshot"], "TOP_SECRET")
	}
	t.Setenv("MANAGEMENT_TOKEN", "wrong")
	state, _ := PollManagement(context.Background(), host, host, "restconf", m)
	require.Equal(t, "auth_failed", state)
	t.Setenv("MANAGEMENT_TOKEN", "fixture-token")
	m.CAFile = ""
	state, _ = PollManagement(context.Background(), host, host, "restconf", m)
	require.Equal(t, "policy_blocked", state)
	m.CAFile = ca
	m.Path = "/redirect"
	state, _ = PollManagement(context.Background(), host, host, "vendor_api", m)
	require.Equal(t, "unsupported", state)
	m.Path = "https://example.com"
	require.Error(t, m.Validate("vendor_api"))
}

func TestSnapshotRedactionAndSizeLimits(t *testing.T) {
	record, err := snapshotRecord([]byte("hostname fixture\n-----BEGIN PRIVATE KEY-----\nTOP_SECRET\n-----END PRIVATE KEY-----\n"), "text", nil)
	require.NoError(t, err)
	require.NotContains(t, record["snapshot"], "TOP_SECRET")
	record, err = snapshotRecord([]byte(strings.Repeat("x", 17000)), "text", nil)
	require.NoError(t, err)
	require.NotContains(t, record, "snapshot")
	require.Contains(t, record, "snapshot_omitted")
	_, err = snapshotRecord([]byte("<data>broken"), "xml", nil)
	require.Error(t, err)
	_, err = readNETCONFFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", 262150))))
	require.Error(t, err)
}
