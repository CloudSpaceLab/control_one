// Package sshproxy is a minimal bastion that accepts SSH connections from
// operators, verifies their user cert against the tenant SSH CA, and either
// forwards the session to a downstream node or records it locally.
//
// This package is a scaffold: it wires up the auth + cert-check path and
// leaves transport to the downstream node as a pluggable NodeDialer. The
// production wire-up still needs (a) a node-side tunnel receiver inside
// nodeagent and (b) a replay writer that feeds the session into the existing
// session_recordings pipeline.
package sshproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

// NodeDialer connects to a target node's SSH endpoint. Implementations should
// establish an authenticated tunnel (mTLS + nodeagent relay in production).
type NodeDialer interface {
	Dial(ctx context.Context, principal string, nodeID string) (net.Conn, error)
}

// Recorder receives a copy of the session bytes (stdin + stdout). Pass
// io.Discard to disable recording.
type Recorder interface {
	io.WriteCloser
}

// Config describes how the proxy authenticates + routes connections.
type Config struct {
	// CAPublicKey is the tenant SSH CA public key — the bastion rejects any
	// client certificate not signed by this authority.
	CAPublicKey ssh.PublicKey

	// HostSigner authenticates the bastion itself to the client.
	HostSigner ssh.Signer

	// NodeDialer determines where to proxy a session once authenticated.
	NodeDialer NodeDialer

	// NewRecorder returns a Recorder for each session (may be nil).
	NewRecorder func(sessionID string) Recorder

	// Log receives connection-level events. Optional.
	Log *zap.Logger

	// EmitSession is called once on session open and once on close so the
	// eventbus can publish bastion.session.{open,close} rows joinable to
	// connection rows via session_id.
	EmitSession func(BastionSessionEvent)
}

// BastionSessionEvent captures what the upstream eventbus needs to know
// about a bastion session for forensic linkage.
type BastionSessionEvent struct {
	Kind       string // "open" | "close"
	SessionID  string
	Principal  string
	NodeID     string
	RemoteIP   string
	RemotePort int
	BytesUp    int64 // operator -> node (close only)
	BytesDown  int64 // node -> operator (close only)
	StartedAt  time.Time
	EndedAt    time.Time
}

// Proxy serves bastion SSH connections.
type Proxy struct {
	cfg Config
}

// New returns a Proxy. Serve is the main entry point.
func New(cfg Config) (*Proxy, error) {
	if cfg.CAPublicKey == nil {
		return nil, errors.New("ca public key required")
	}
	if cfg.HostSigner == nil {
		return nil, errors.New("host signer required")
	}
	if cfg.NodeDialer == nil {
		return nil, errors.New("node dialer required")
	}
	return &Proxy{cfg: cfg}, nil
}

// Serve accepts connections on l until ctx is cancelled.
func (p *Proxy) Serve(ctx context.Context, l net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go p.handleConn(ctx, conn)
	}
}

func (p *Proxy) handleConn(ctx context.Context, raw net.Conn) {
	defer func() { _ = raw.Close() }()
	serverCfg := &ssh.ServerConfig{
		PublicKeyCallback: p.publicKeyCallback,
	}
	serverCfg.AddHostKey(p.cfg.HostSigner)

	remoteIP, remotePort := splitAddr(raw.RemoteAddr())
	startedAt := time.Now().UTC()

	sshConn, chans, reqs, err := ssh.NewServerConn(raw, serverCfg)
	if err != nil {
		p.debug("bastion handshake failed", zap.Error(err))
		return
	}
	defer func() { _ = sshConn.Close() }()
	go ssh.DiscardRequests(reqs)

	principal := sshConn.User()
	nodeID := stringFromExtensions(sshConn.Permissions, "target-node-id")
	if nodeID == "" {
		p.debug("bastion no target-node-id extension on cert", zap.String("user", principal))
		return
	}

	sessionID := fmt.Sprintf("%x-%s", sshConn.SessionID(), principal)
	if p.cfg.EmitSession != nil {
		p.cfg.EmitSession(BastionSessionEvent{
			Kind: "open", SessionID: sessionID, Principal: principal, NodeID: nodeID,
			RemoteIP: remoteIP, RemotePort: remotePort, StartedAt: startedAt,
		})
	}
	var bytesUp, bytesDown int64
	defer func() {
		if p.cfg.EmitSession == nil {
			return
		}
		p.cfg.EmitSession(BastionSessionEvent{
			Kind: "close", SessionID: sessionID, Principal: principal, NodeID: nodeID,
			RemoteIP: remoteIP, RemotePort: remotePort,
			BytesUp: bytesUp, BytesDown: bytesDown,
			StartedAt: startedAt, EndedAt: time.Now().UTC(),
		})
	}()

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	nodeConn, err := p.cfg.NodeDialer.Dial(dialCtx, principal, nodeID)
	if err != nil {
		p.debug("bastion dial node failed", zap.Error(err))
		return
	}
	defer func() { _ = nodeConn.Close() }()

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "only session channels supported")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer func() { _ = ch.Close() }()
			go ssh.DiscardRequests(chReqs)
			var recorder io.WriteCloser
			if p.cfg.NewRecorder != nil {
				recorder = p.cfg.NewRecorder(sessionID)
			}
			up, down := p.proxySession(ch, nodeConn, recorder)
			bytesUp += up
			bytesDown += down
		}()
	}
}

func (p *Proxy) publicKeyCallback(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	cert, ok := key.(*ssh.Certificate)
	if !ok {
		return nil, errors.New("only certificate-based auth allowed")
	}
	checker := ssh.CertChecker{
		IsUserAuthority: func(auth ssh.PublicKey) bool {
			return string(auth.Marshal()) == string(p.cfg.CAPublicKey.Marshal())
		},
	}
	if err := checker.CheckCert(meta.User(), cert); err != nil {
		return nil, fmt.Errorf("cert check: %w", err)
	}
	// Propagate cert extensions so handleConn can route to the right node.
	return &ssh.Permissions{
		Extensions: cert.Extensions,
	}, nil
}

func (p *Proxy) proxySession(ch ssh.Channel, nodeConn net.Conn, rec io.WriteCloser) (int64, int64) {
	if rec != nil {
		defer func() { _ = rec.Close() }()
	}
	var up, down int64
	done := make(chan struct{}, 2)
	go func() {
		n, _ := copyWithTap(nodeConn, ch, rec)
		up = n
		done <- struct{}{}
	}()
	go func() {
		n, _ := copyWithTap(ch, nodeConn, rec)
		down = n
		done <- struct{}{}
	}()
	<-done
	<-done
	return up, down
}

// copyWithTap proxies src -> dst and optionally writes a copy to tap.
func copyWithTap(dst io.Writer, src io.Reader, tap io.Writer) (int64, error) {
	if tap == nil {
		return io.Copy(dst, src)
	}
	return io.Copy(io.MultiWriter(dst, tap), src)
}

func splitAddr(a net.Addr) (string, int) {
	if a == nil {
		return "", 0
	}
	if t, ok := a.(*net.TCPAddr); ok {
		return t.IP.String(), t.Port
	}
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String(), 0
	}
	p := 0
	fmt.Sscanf(port, "%d", &p)
	return host, p
}

func stringFromExtensions(perms *ssh.Permissions, key string) string {
	if perms == nil {
		return ""
	}
	if v, ok := perms.Extensions[key]; ok {
		return v
	}
	return ""
}

func (p *Proxy) debug(msg string, fields ...zap.Field) {
	if p.cfg.Log != nil {
		p.cfg.Log.Debug(msg, fields...)
	}
}
