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
				recorder = p.cfg.NewRecorder(string(sshConn.SessionID()) + "-" + principal)
			}
			p.proxySession(ch, nodeConn, recorder)
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

func (p *Proxy) proxySession(ch ssh.Channel, nodeConn net.Conn, rec io.WriteCloser) {
	if rec != nil {
		defer func() { _ = rec.Close() }()
	}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = copyWithTap(nodeConn, ch, rec)
		done <- struct{}{}
	}()
	go func() {
		_, _ = copyWithTap(ch, nodeConn, rec)
		done <- struct{}{}
	}()
	<-done
}

// copyWithTap proxies src -> dst and optionally writes a copy to tap.
func copyWithTap(dst io.Writer, src io.Reader, tap io.Writer) (int64, error) {
	if tap == nil {
		return io.Copy(dst, src)
	}
	return io.Copy(io.MultiWriter(dst, tap), src)
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
