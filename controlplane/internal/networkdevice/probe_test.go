package networkdevice

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestNetworkCredentialValidation(t *testing.T) {
	c := Credential{Username: "readonly", AuthProtocol: "SHA256", AuthSecret: "authsecret", PrivProtocol: "AES", PrivSecret: "privsecret"}
	require.NoError(t, c.Validate("snmpv3"))
	c.PrivProtocol = "DES"
	require.Error(t, c.Validate("snmpv3"))
	require.Error(t, c.Validate("snmpv2c"))
	require.Error(t, (Credential{Username: "readonly", Password: "pass"}).Validate("ssh"))
}

func TestDestinationPolicy(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "169.254.169.254", "::1", "0.0.0.0", "224.0.0.1"} {
		_, err := Resolve(context.Background(), address, []string{"0.0.0.0/0", "::/0"})
		require.EqualError(t, err, "policy_blocked")
	}
	_, err := Resolve(context.Background(), "192.0.2.1", nil)
	require.EqualError(t, err, "policy_blocked")
	ip, err := Resolve(context.Background(), "192.0.2.1", []string{"192.0.2.0/24"})
	require.NoError(t, err)
	require.Equal(t, "192.0.2.1", ip)
}

func TestFingerprintEvidenceAndSecretRedaction(t *testing.T) {
	for _, tc := range []struct{ identity, vendor, platform, model, typ string }{
		{"Cisco IOS XE Software, Catalyst C9300-48P", "Cisco", "IOS XE", "C9300-48P", "switch"},
		{"FortiGate-100F FortiOS v7.4", "Fortinet", "FortiOS", "FortiGate-100F", "firewall"},
	} {
		r := Fingerprint(tc.identity, "", Credential{})
		require.Equal(t, "authenticated", r.State)
		require.Equal(t, tc.vendor, r.Vendor)
		require.Equal(t, tc.model, r.Model)
		require.Equal(t, tc.platform, r.Platform)
		require.Equal(t, tc.typ, r.SuggestedType)
		require.GreaterOrEqual(t, r.Confidence, 80)
	}
	r := Fingerprint("Cisco model: MYSECRET", "", Credential{Password: "MYSECRET"})
	require.Empty(t, r.Model)
	r = Fingerprint("unknown appliance", "", Credential{})
	require.Zero(t, r.Confidence)
	require.Equal(t, "network_appliance", r.SuggestedType)
}

// Actual SSH handshake/authentication and exec exchanges with a test appliance.
// The fixture rejects every command except the adapter's fixed read-only one.
func TestSSHReadOnlyProbe(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(private)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go serveAppliance(raw, signer)
		}
	}()
	_, portRaw, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portRaw)
	c := Credential{Username: "readonly", Password: "fixture-pass", HostKeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey())}
	r := probeSSH(ctx, "127.0.0.1", port, c)
	require.Equal(t, "authenticated", r.State)
	require.Equal(t, "Cisco", r.Vendor)
	require.Equal(t, []string{"ssh_identity"}, r.Capabilities)
	c.Password = "wrong"
	r = probeSSH(ctx, "127.0.0.1", port, c)
	require.Equal(t, "auth_failed", r.State)
	c.Password = "fixture-pass"
	c.HostKeyFingerprint = "SHA256:wrong"
	r = probeSSH(ctx, "127.0.0.1", port, c)
	require.Equal(t, "policy_blocked", r.State)
	require.NoError(t, listener.Close())
	r = probeSSH(ctx, "127.0.0.1", port, c)
	require.Equal(t, "unreachable", r.State)
}

func serveAppliance(raw net.Conn, signer ssh.Signer) {
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
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}
		for request := range requests {
			var payload struct{ Command string }
			_ = ssh.Unmarshal(request.Payload, &payload)
			allowed := request.Type == "exec" && payload.Command == "show version"
			_ = request.Reply(allowed, nil)
			if allowed {
				_, _ = channel.Write([]byte("Cisco IOS XE Software, Catalyst C9300-48P"))
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, 0)
				_, _ = channel.SendRequest("exit-status", false, status)
				_ = channel.Close()
				break
			}
		}
	}
}
