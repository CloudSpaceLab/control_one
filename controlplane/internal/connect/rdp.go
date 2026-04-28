package connect

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// RDPConnector emits a TCP-only reachability probe. There is no
// production-quality Go RDP client suitable for the wizard's "verify
// credentials" flow, so we restrict the probe to a TCP three-way
// handshake. Operators who need full RDP-managed onboarding should pair
// the RDP target with a parallel WinRM credential — the WinRM probe
// handles credential verification and the agent install.
type RDPConnector struct{}

func NewRDPConnector() *RDPConnector { return &RDPConnector{} }

func (c *RDPConnector) Name() Protocol { return ProtoRDP }

func (c *RDPConnector) Test(ctx context.Context, t Target) (*Probe, error) {
	if t.Host == "" {
		return nil, errors.New("rdp: host required")
	}
	port := t.Port
	if port == 0 {
		port = DefaultPort(ProtoRDP, false)
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(port))

	dialer := &net.Dialer{Timeout: applyTimeout(t.Timeout)}
	dialCtx, cancel := context.WithTimeout(ctx, applyTimeout(t.Timeout))
	defer cancel()

	start := time.Now()
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("rdp tcp probe: %w", err)
	}
	_ = conn.Close()

	return &Probe{
		Reachable:    true,
		LatencyMs:    time.Since(start).Milliseconds(),
		OS:           "windows", // RDP is overwhelmingly Windows
		Capabilities: []string{"rdp_tcp_only"},
		Detected:     time.Now().UTC(),
		Banner:       "TCP reachable (no credential verification — pair with WinRM for full enrolment)",
	}, nil
}
