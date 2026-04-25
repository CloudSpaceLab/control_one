package sshproxy

import (
	"context"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/sshca"
)

type fakeDialer struct{}

func (fakeDialer) Dial(context.Context, string, string) (net.Conn, error) {
	a, _ := net.Pipe()
	return a, nil
}

func TestNewValidatesConfig(t *testing.T) {
	ca, err := sshca.Generate()
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(ca.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{}); err == nil {
		t.Fatal("empty config must error")
	}
	if _, err := New(Config{CAPublicKey: ca.PublicKey}); err == nil {
		t.Fatal("missing signer must error")
	}
	if _, err := New(Config{CAPublicKey: ca.PublicKey, HostSigner: hostSigner}); err == nil {
		t.Fatal("missing dialer must error")
	}
	if _, err := New(Config{CAPublicKey: ca.PublicKey, HostSigner: hostSigner, NodeDialer: fakeDialer{}}); err != nil {
		t.Fatalf("valid config should accept: %v", err)
	}
}

func TestPublicKeyCallbackRejectsUnsignedKey(t *testing.T) {
	ca, _ := sshca.Generate()
	hostSigner, _ := ssh.NewSignerFromKey(ca.PrivateKey)
	p, _ := New(Config{CAPublicKey: ca.PublicKey, HostSigner: hostSigner, NodeDialer: fakeDialer{}})

	meta := fakeMeta{user: "alice"}
	if _, err := p.publicKeyCallback(meta, ca.PublicKey); err == nil {
		t.Fatal("plain public key (not a cert) must be rejected")
	}
}

type fakeMeta struct {
	user string
}

func (f fakeMeta) User() string          { return f.user }
func (f fakeMeta) SessionID() []byte     { return []byte("sid") }
func (f fakeMeta) ClientVersion() []byte { return []byte("") }
func (f fakeMeta) ServerVersion() []byte { return []byte("") }
func (f fakeMeta) RemoteAddr() net.Addr  { return &net.TCPAddr{} }
func (f fakeMeta) LocalAddr() net.Addr   { return &net.TCPAddr{} }
