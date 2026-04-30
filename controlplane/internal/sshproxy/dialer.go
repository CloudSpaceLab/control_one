package sshproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// MTLSDialer establishes a tunnel to a target node via that node's mTLS
// endpoint. The bastion holds a leaf cert signed by the control plane CA;
// each node's nodeagent listens on its tunnel port and forwards bytes to its
// local sshd.
//
// In production deployments the same WireGuard mesh used for telemetry is
// reused so the bastion never traverses the public internet to reach a node.
// The dialer doesn't model the WireGuard side directly: the operator wires
// the WG tunnel as the route, and the dialer just dials the node's tunnel
// listen address.
type MTLSDialer struct {
	store       DialerStore
	clientCert  tls.Certificate
	caCert      *x509.CertPool
	dialTimeout time.Duration
}

// DialerStore is the slice of storage.Store the dialer needs to look up node
// addresses. Kept narrow for testability.
type DialerStore interface {
	GetNode(context.Context, uuid.UUID) (*storage.Node, error)
}

// MTLSDialerConfig captures mTLS material + lookup wiring.
type MTLSDialerConfig struct {
	Store          DialerStore
	ClientCertFile string
	ClientKeyFile  string
	CACertFile     string
	DialTimeout    time.Duration
}

func NewMTLSDialer(cfg MTLSDialerConfig) (*MTLSDialer, error) {
	if cfg.Store == nil {
		return nil, errors.New("dialer store required")
	}
	if cfg.ClientCertFile == "" || cfg.ClientKeyFile == "" {
		return nil, errors.New("client cert + key required for mTLS dialer")
	}
	cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load bastion mtls cert: %w", err)
	}
	pool := x509.NewCertPool()
	if cfg.CACertFile != "" {
		raw, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("read ca cert: %w", err)
		}
		if !pool.AppendCertsFromPEM(raw) {
			return nil, errors.New("ca cert PEM had no usable certificates")
		}
	}
	timeout := cfg.DialTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &MTLSDialer{store: cfg.Store, clientCert: cert, caCert: pool, dialTimeout: timeout}, nil
}

// Dial resolves the target node and connects to its tunnel endpoint over mTLS.
// principal is logged for audit but is not forwarded — the node enforces its
// own ACL on incoming streams.
func (d *MTLSDialer) Dial(ctx context.Context, principal string, nodeID string) (net.Conn, error) {
	parsed, err := uuid.Parse(nodeID)
	if err != nil {
		return nil, fmt.Errorf("invalid node id: %w", err)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	node, err := d.store.GetNode(lookupCtx, parsed)
	if err != nil {
		return nil, fmt.Errorf("lookup node: %w", err)
	}
	if node == nil {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}
	addr := nodeTunnelAddress(node)
	if addr == "" {
		return nil, fmt.Errorf("node %s has no tunnel address", nodeID)
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{d.clientCert},
		RootCAs:      d.caCert,
		ServerName:   node.Hostname,
	}
	dialCtx, dialCancel := context.WithTimeout(ctx, d.dialTimeout)
	defer dialCancel()
	dialer := &tls.Dialer{Config: tlsCfg, NetDialer: &net.Dialer{Timeout: d.dialTimeout}}
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("mtls dial %s: %w", addr, err)
	}
	_ = principal // intentional — node-side audit picks up principal from cert
	return conn, nil
}

// nodeTunnelAddress prefers the tunnel port (default 2222) on the node's
// PublicIP. If the node only has a private mesh IP (WireGuard), that is used
// instead so the bastion can reach private fleets.
func nodeTunnelAddress(n *storage.Node) string {
	host := ""
	if n.PublicIP.Valid && n.PublicIP.String != "" {
		host = n.PublicIP.String
	} else if n.Hostname != "" {
		host = n.Hostname
	}
	if host == "" {
		return ""
	}
	return host + ":2222"
}
