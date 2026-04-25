// Package firewall provides a single interface over the host firewall stacks
// the agent might encounter:
//
//   - ufw       (Ubuntu / Debian default frontend)
//   - firewalld (RHEL / Fedora / SUSE / Rocky default)
//   - nftables  (modern Linux baseline)
//   - iptables  (legacy Linux fallback)
//   - fail2ban  (jail-based dynamic blocker — orthogonal, used alongside)
//   - netsh     (Windows Firewall, advfirewall API)
//   - pf        (FreeBSD / macOS) — minimal stub
//
// The Manager picks the first available backend on Detect() and exposes
// Block / Unblock / List operations that the auto-block pipeline calls when a
// threat-intel match or a port-rule violation fires.
//
// Every backend honours the same Rule shape so callers never branch on OS.
package firewall

import (
	"context"
	"errors"
	"fmt"
	"runtime"
)

// Direction is "in" or "out".
type Direction string

const (
	DirectionIn  Direction = "in"
	DirectionOut Direction = "out"
)

// Action is "block" or "allow".
type Action string

const (
	ActionBlock Action = "block"
	ActionAllow Action = "allow"
)

// Rule is the cross-platform rule shape. Tag (string) is recorded in a comment
// when the backend supports it so the pipeline can find/update its own rules
// without trampling rules the operator added by hand.
type Rule struct {
	Source    string    // IP or CIDR; empty == any
	Dest      string    // IP or CIDR; empty == any
	Port      int       // 0 == any
	Protocol  string    // tcp | udp | icmp | ""
	Direction Direction
	Action    Action
	Tag       string    // e.g. "controlone:ti:abuseipdb"
	Comment   string
}

// Backend is the per-platform implementation.
type Backend interface {
	Name() string
	Available() bool
	Apply(ctx context.Context, r Rule) error
	Remove(ctx context.Context, r Rule) error
	List(ctx context.Context, tag string) ([]Rule, error)
}

// Manager picks a backend and dispatches calls to it.
type Manager struct {
	backends []Backend
	chosen   Backend
}

// New registers all platform-appropriate backends in priority order. Caller
// must call Detect() before using Apply/Remove/List.
func New() *Manager {
	m := &Manager{}
	switch runtime.GOOS {
	case "linux":
		m.backends = []Backend{
			&ufwBackend{},
			&firewalldBackend{},
			&nftablesBackend{},
			&iptablesBackend{},
		}
	case "windows":
		m.backends = []Backend{&netshBackend{}}
	case "darwin", "freebsd":
		m.backends = []Backend{&pfBackend{}}
	}
	return m
}

// WithBackends overrides default detection (used in tests + offline bundles).
func (m *Manager) WithBackends(backends ...Backend) *Manager {
	m.backends = backends
	return m
}

// Detect picks the first available backend. Returns ErrNoBackend if none
// match — callers should surface this as a config error.
func (m *Manager) Detect() error {
	for _, b := range m.backends {
		if b.Available() {
			m.chosen = b
			return nil
		}
	}
	return ErrNoBackend
}

// Backend returns the active backend (nil before Detect / on failure).
func (m *Manager) Backend() Backend { return m.chosen }

// Apply enforces a rule via the active backend.
func (m *Manager) Apply(ctx context.Context, r Rule) error {
	if m.chosen == nil {
		return ErrNoBackend
	}
	if err := validateRule(r); err != nil {
		return err
	}
	return m.chosen.Apply(ctx, r)
}

// Remove drops a previously applied rule.
func (m *Manager) Remove(ctx context.Context, r Rule) error {
	if m.chosen == nil {
		return ErrNoBackend
	}
	return m.chosen.Remove(ctx, r)
}

// List returns rules tagged by Control One. Empty tag returns all rules the
// backend can enumerate.
func (m *Manager) List(ctx context.Context, tag string) ([]Rule, error) {
	if m.chosen == nil {
		return nil, ErrNoBackend
	}
	return m.chosen.List(ctx, tag)
}

// Errors -------------------------------------------------------------------

var (
	// ErrNoBackend is returned when no firewall backend is available on the host.
	ErrNoBackend = errors.New("no firewall backend available on this host")
	// ErrUnsupported is returned when a backend cannot fulfil a rule shape
	// (e.g., direction the backend does not model directly).
	ErrUnsupported = errors.New("rule shape not supported by backend")
)

// validateRule enforces the small invariants every backend depends on.
func validateRule(r Rule) error {
	if r.Action != ActionBlock && r.Action != ActionAllow {
		return fmt.Errorf("invalid action %q", r.Action)
	}
	if r.Direction != DirectionIn && r.Direction != DirectionOut && r.Direction != "" {
		return fmt.Errorf("invalid direction %q", r.Direction)
	}
	if r.Port < 0 || r.Port > 65535 {
		return fmt.Errorf("port out of range: %d", r.Port)
	}
	if r.Source == "" && r.Dest == "" && r.Port == 0 {
		return errors.New("rule must scope at least one of source / dest / port")
	}
	return nil
}
