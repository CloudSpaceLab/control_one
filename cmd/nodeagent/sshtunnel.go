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
	"time"

	"go.uber.org/zap"
)

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
	go acceptLoop(ctx, log, listener, cfg.UpstreamAddr)
	log.Info("ssh tunnel listening", zap.String("addr", cfg.ListenAddr), zap.String("upstream", cfg.UpstreamAddr))
	return nil
}

func acceptLoop(ctx context.Context, log *zap.Logger, l net.Listener, upstream string) {
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("tunnel accept", zap.Error(err))
			continue
		}
		go handleTunnelConn(ctx, log, conn, upstream)
	}
}

func handleTunnelConn(ctx context.Context, log *zap.Logger, conn net.Conn, upstream string) {
	defer func() { _ = conn.Close() }()
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	upstreamConn, err := dialer.DialContext(dialCtx, "tcp", upstream)
	if err != nil {
		log.Warn("tunnel upstream dial", zap.Error(err), zap.String("upstream", upstream))
		return
	}
	defer func() { _ = upstreamConn.Close() }()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstreamConn, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, upstreamConn); done <- struct{}{} }()
	<-done
}
