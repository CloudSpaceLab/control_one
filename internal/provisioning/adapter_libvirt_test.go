package provisioning

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// TestLibvirtAdapterDestroyForwardsToHTTP — libvirt is a local-dev provider,
// Destroy still forwards so local HAProxy hooks can react if configured.
func TestLibvirtAdapterDestroyForwardsToHTTP(t *testing.T) {
	t.Parallel()
	client := &capturingClient{}
	a := &libvirtAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), client)}

	if err := a.Destroy(context.Background(), "libvirt-dom-1"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(client.calls))
	}
}

// TestLibvirtAdapterLBNoOpWithoutClient — no backend wiring means register/
// deregister log WARN and succeed; libvirt deployments typically don't have a
// native cluster LB.
func TestLibvirtAdapterLBNoOpWithoutClient(t *testing.T) {
	t.Parallel()
	a := &libvirtAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), nil)}

	if err := a.RegisterLB(context.Background(), "dom-1", map[string]any{"lb_pool": "ignored"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := a.DeregisterLB(context.Background(), "dom-1", map[string]any{"lb_pool": "ignored"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
