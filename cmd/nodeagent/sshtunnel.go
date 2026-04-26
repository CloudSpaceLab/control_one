package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/eventstream"
)

// BastionSessionEvent describes an open/close bastion session for the
// eventstream. Caller wires this through `internal/eventstream` so the
// UI can join sessions to connection rows.
type BastionSessionEvent struct {
	Kind       string // "open" | "close"
	SessionID  string
	RemoteIP   string
	RemotePort int
	BytesIn    int64 // bastion -> nodeagent (close only)
	BytesOut   int64 // nodeagent -> bastion (close only)
	StartedAt  time.Time
	EndedAt    time.Time
}

// SessionEmitter is invoked once on open and once on close. May be nil.
type SessionEmitter func(BastionSessionEvent)

// bastionEmitter converts BastionSessionEvent → eventstream.Event.
// tenantID is left blank — the controlplane's ingest path resolves it
// from the agent's mTLS principal so we don't have to plumb it here.
func bastionEmitter(stream *eventstream.Stream, nodeID, tenantID string) SessionEmitter {
	return func(e BastionSessionEvent) {
		ts := e.StartedAt
		if e.Kind == "close" && !e.EndedAt.IsZero() {
			ts = e.EndedAt
		}
		dur := int64(0)
		if !e.EndedAt.IsZero() {
			dur = e.EndedAt.Sub(e.StartedAt).Milliseconds()
		}
		stream.Publish(eventstream.Event{
			Type:          "bastion.session." + e.Kind,
			TS:            ts,
			NodeID:        nodeID,
			TenantID:      tenantID,
			BastionSessID: e.SessionID,
			SrcIP:         e.RemoteIP,
			SrcPort:       e.RemotePort,
			BytesIn:       e.BytesIn,
			BytesOut:      e.BytesOut,
			DurationMS:    dur,
			Severity:      "info",
			Message:       fmt.Sprintf("bastion session %s from %s", e.Kind, e.RemoteIP),
			DedupKey:      fmt.Sprintf("bastion.%s:%s", e.Kind, e.SessionID),
		})
	}
}

// sshTunnelConfig describes the bastion-facing listener the nodeagent runs.
//
// Flow:
//
//   bastion (control plane)  --mTLS-->  nodeagent listen :2222  -->  127.0.0.1:22 (sshd)
//
// The mTLS step authenticates the bastion: only the bastion's client cert
// (signed by the control-plane CA) can connect. Once authenticated the
// nodeagent forwards bytes verbatim to the local sshd, which enforces the
// short-lived user certificate the bastion issued.
type sshTunnelConfig struct {
	ListenAddr     string // default :2222
	ClientCAFile   string // PEM bundle of trusted bastion CAs
	ServerCertFile string
	ServerKeyFile  string
	UpstreamAddr   string // default 127.0.0.1:22
	EmitSession    SessionEmitter
}

// startSSHTunnel begins listening for bastion connections. Errors during
// Accept loop are logged but do not stop the agent — operators rely on the
// agent for telemetry even when bastion access is misconfigured.
func startSSHTunnel(ctx context.Context, log *zap.Logger, cfg sshTunnelConfig) error {
	if cfg.ServerCertFile == "" || cfg.ServerKeyFile == "" || cfg.ClientCAFile == "" {
		return errors.New("ssh tunnel requires server cert, key, and client CA")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":2222"
	}
	if cfg.UpstreamAddr == "" {
		cfg.UpstreamAddr = "127.0.0.1:22"
	}

	cert, err := tls.LoadX509KeyPair(cfg.ServerCertFile, cfg.ServerKeyFile)
	if err != nil {
		return fmt.Errorf("load tunnel cert: %w", err)
	}
	caRaw, err := os.ReadFile(cfg.ClientCAFile)
	if err != nil {
		return fmt.Errorf("read client ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caRaw) {
		return errors.New("invalid client CA bundle")
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	listener, err := tls.Listen("tcp", cfg.ListenAddr, tlsCfg)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.ListenAddr, err)
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	go acceptLoop(ctx, log, listener, cfg.UpstreamAddr, cfg.EmitSession)
	log.Info("ssh tunnel listening", zap.String("addr", cfg.ListenAddr), zap.String("upstream", cfg.UpstreamAddr))
	return nil
}

func acceptLoop(ctx context.Context, log *zap.Logger, l net.Listener, upstream string, emit SessionEmitter) {
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("tunnel accept", zap.Error(err))
			continue
		}
		go handleTunnelConn(ctx, log, conn, upstream, emit)
	}
}

// countingConn wraps net.Conn so we can report total bytes on close.
type countingConn struct {
	net.Conn
	rx, tx atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.rx.Add(int64(n))
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.tx.Add(int64(n))
	return n, err
}

func handleTunnelConn(ctx context.Context, log *zap.Logger, conn net.Conn, upstream string, emit SessionEmitter) {
	defer func() { _ = conn.Close() }()

	sessionID := uuid.NewString()
	startedAt := time.Now().UTC()
	remoteIP, remotePort := splitAddr(conn.RemoteAddr())
	if emit != nil {
		emit(BastionSessionEvent{
			Kind: "open", SessionID: sessionID,
			RemoteIP: remoteIP, RemotePort: remotePort,
			StartedAt: startedAt,
		})
	}

	cc := &countingConn{Conn: conn}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	upstreamConn, err := dialer.DialContext(dialCtx, "tcp", upstream)
	if err != nil {
		log.Warn("tunnel upstream dial", zap.Error(err), zap.String("upstream", upstream))
		if emit != nil {
			emit(BastionSessionEvent{
				Kind: "close", SessionID: sessionID,
				RemoteIP: remoteIP, RemotePort: remotePort,
				StartedAt: startedAt, EndedAt: time.Now().UTC(),
			})
		}
		return
	}
	defer func() { _ = upstreamConn.Close() }()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstreamConn, cc); done <- struct{}{} }()
	go func() { _, _ = io.Copy(cc, upstreamConn); done <- struct{}{} }()
	<-done

	if emit != nil {
		emit(BastionSessionEvent{
			Kind: "close", SessionID: sessionID,
			RemoteIP: remoteIP, RemotePort: remotePort,
			BytesIn: cc.rx.Load(), BytesOut: cc.tx.Load(),
			StartedAt: startedAt, EndedAt: time.Now().UTC(),
		})
	}
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
