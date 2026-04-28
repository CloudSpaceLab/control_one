// Package connect provides a unified abstraction for testing connectivity
// to a target server across multiple remote-administration protocols (SSH,
// WinRM, plus a TCP-only probe for RDP). It powers the operator
// "onboard a new server" wizard: an admin enters credentials, the wizard
// runs a Connector.Test, surfaces a structured Probe result (latency,
// detected OS, hostname, capability flags), and on success enrolls the
// node via the existing fleet-enroll path.
//
// There is no single Go library that spans every protocol cleanly:
//
//   - SSH        — golang.org/x/crypto/ssh (stdlib-grade, already a dep)
//   - WinRM      — github.com/masterzen/winrm (actively maintained)
//   - RDP        — no production-quality Go RDP client; we emit a TCP probe
//                  only and rely on a downstream Windows agent install via
//                  WinRM when full management is needed.
//
// Each protocol implements Connector. The Registry chooses the right one
// based on Target.Protocol so handlers don't switch on protocol strings.
package connect

import (
	"context"
	"errors"
	"time"
)

// Protocol identifies a remote-administration channel. The zero value is
// invalid; use a named constant.
type Protocol string

const (
	ProtoSSH   Protocol = "ssh"
	ProtoWinRM Protocol = "winrm"
	ProtoRDP   Protocol = "rdp"
)

// AuthMethod selects how Credentials should be interpreted.
type AuthMethod string

const (
	AuthPassword       AuthMethod = "password"
	AuthPrivateKey     AuthMethod = "private_key"
	AuthAgentSocket    AuthMethod = "agent" // SSH only — relies on SSH_AUTH_SOCK on the controlplane host
)

// Target describes a server the operator wants to onboard.
type Target struct {
	Protocol   Protocol   `json:"protocol"`
	Host       string     `json:"host"`
	Port       int        `json:"port,omitempty"` // 0 = protocol default
	Username   string     `json:"username"`
	Auth       AuthMethod `json:"auth"`
	Password   string     `json:"password,omitempty"`     // when Auth == AuthPassword
	PrivateKey string     `json:"private_key,omitempty"`  // PEM body (Auth == AuthPrivateKey)
	Passphrase string     `json:"passphrase,omitempty"`   // optional, for encrypted keys
	HTTPS      bool       `json:"https,omitempty"`        // WinRM over HTTPS
	SkipVerify bool       `json:"skip_verify,omitempty"`  // bypass cert verification (lab only)
	Timeout    time.Duration `json:"-"`                   // optional override, default 10s
}

// Probe is the structured result of Connector.Test.
type Probe struct {
	Reachable    bool          `json:"reachable"`
	LatencyMs    int64         `json:"latency_ms,omitempty"`
	OS           string        `json:"os,omitempty"`             // linux | windows | macos | unknown
	OSVersion    string        `json:"os_version,omitempty"`
	Hostname     string        `json:"hostname,omitempty"`
	Architecture string        `json:"architecture,omitempty"`
	Capabilities []string      `json:"capabilities,omitempty"`   // e.g. "sudo", "powershell", "package_apt"
	Banner       string        `json:"banner,omitempty"`         // SSH banner / WinRM PSVersion
	Detected     time.Time     `json:"detected_at"`
}

// Connector tests connectivity for one protocol.
type Connector interface {
	Name() Protocol
	Test(ctx context.Context, t Target) (*Probe, error)
}

// ErrUnsupported is returned by RDP (or any future stub) when full probe
// is not implemented; callers should fall back to TCP reachability only.
var ErrUnsupported = errors.New("connect: full probe unsupported for protocol")

// DefaultPort returns the canonical port per protocol when Target.Port == 0.
func DefaultPort(p Protocol, https bool) int {
	switch p {
	case ProtoSSH:
		return 22
	case ProtoWinRM:
		if https {
			return 5986
		}
		return 5985
	case ProtoRDP:
		return 3389
	}
	return 0
}

// applyTimeout fills a sane default when Target.Timeout is zero.
func applyTimeout(t time.Duration) time.Duration {
	if t > 0 {
		return t
	}
	return 10 * time.Second
}
