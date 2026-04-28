package connect

import (
	"context"
	"fmt"
	"sync"
)

// Registry is the package's public entry point: it routes a Target to the
// right Connector. Server handlers depend on this — not the concrete
// SSHConnector / WinRMConnector / RDPConnector — so they don't have to
// switch on protocol strings.
type Registry struct {
	mu     sync.RWMutex
	byProto map[Protocol]Connector
}

// NewRegistry returns a registry pre-populated with all built-in
// connectors. Tests can construct an empty registry via &Registry{} and
// register stubs.
func NewRegistry() *Registry {
	r := &Registry{byProto: make(map[Protocol]Connector)}
	r.Register(NewSSHConnector())
	r.Register(NewWinRMConnector())
	r.Register(NewRDPConnector())
	return r
}

// Register replaces any existing connector for the same protocol. Last
// writer wins so tests can override.
func (r *Registry) Register(c Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byProto[c.Name()] = c
}

// Test routes to the protocol-specific connector and runs Probe.
func (r *Registry) Test(ctx context.Context, t Target) (*Probe, error) {
	r.mu.RLock()
	c, ok := r.byProto[t.Protocol]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("connect: no connector registered for protocol %q", t.Protocol)
	}
	return c.Test(ctx, t)
}

// Supported returns the protocols the registry can probe. Used by the UI
// to populate the protocol dropdown.
func (r *Registry) Supported() []Protocol {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Protocol, 0, len(r.byProto))
	for p := range r.byProto {
		out = append(out, p)
	}
	return out
}
