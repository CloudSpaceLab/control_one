package provisioning

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// TestMockAdapterRecordsCalls exercises the mock adapter's Destroy/LB surface
// to lock in the contract used by cluster-shrink/teardown tests in the server
// package.
func TestMockAdapterRecordsCalls(t *testing.T) {
	t.Parallel()

	ad := newMockAdapter(zap.NewNop())
	ctx := context.Background()

	if _, err := ad.Apply(ctx, "node-1", Options{Template: "demo"}, map[string]string{"role": "worker"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ad.Destroy(ctx, "node-1"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if err := ad.RegisterLB(ctx, "node-1", map[string]any{"lb_target_group_arn": "arn:aws:elbv2:tg/foo"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := ad.DeregisterLB(ctx, "node-1", map[string]any{"lb_target_group_arn": "arn:aws:elbv2:tg/foo"}); err != nil {
		t.Fatalf("deregister: %v", err)
	}

	calls := ad.Calls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 recorded calls, got %d: %+v", len(calls), calls)
	}

	expect := []string{"Apply", "Destroy", "RegisterLB", "DeregisterLB"}
	for i, m := range expect {
		if calls[i].Method != m {
			t.Fatalf("expected call[%d]=%s, got %s", i, m, calls[i].Method)
		}
		if calls[i].NodeID != "node-1" {
			t.Fatalf("expected node id node-1, got %s", calls[i].NodeID)
		}
	}
	if calls[2].ClusterMeta["lb_target_group_arn"] != "arn:aws:elbv2:tg/foo" {
		t.Fatalf("expected target group propagated, got %+v", calls[2].ClusterMeta)
	}
}

func TestMockAdapterResetClearsCalls(t *testing.T) {
	t.Parallel()

	ad := newMockAdapter(zap.NewNop())
	if err := ad.Destroy(context.Background(), "n"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(ad.Calls()) != 1 {
		t.Fatalf("expected 1 call before reset")
	}
	ad.Reset()
	if len(ad.Calls()) != 0 {
		t.Fatalf("expected 0 calls after reset, got %d", len(ad.Calls()))
	}
}

// TestHTTPAdapterDestroyNoClientReturnsNil verifies the back-compat degrade
// path: when the provisioning backend isn't wired up, Destroy must log WARN and
// return nil rather than block the cluster teardown drain loop.
func TestHTTPAdapterDestroyNoClientReturnsNil(t *testing.T) {
	t.Parallel()

	h := &httpAdapter{log: zap.NewNop(), client: nil}
	if err := h.Destroy(context.Background(), "node-id"); err != nil {
		t.Fatalf("expected nil error with no client, got %v", err)
	}
	if err := h.RegisterLB(context.Background(), "node-id", map[string]any{"lb": "pool-1"}); err != nil {
		t.Fatalf("expected nil error with no client, got %v", err)
	}
	if err := h.DeregisterLB(context.Background(), "node-id", map[string]any{"lb": "pool-1"}); err != nil {
		t.Fatalf("expected nil error with no client, got %v", err)
	}
}

// TestHTTPAdapterDestroyRequiresNodeID is a cheap guardrail so empty node ids
// can't accidentally hit the backend and destroy the wrong resource.
func TestHTTPAdapterDestroyRequiresNodeID(t *testing.T) {
	t.Parallel()

	h := &httpAdapter{log: zap.NewNop(), client: nil}
	if err := h.Destroy(context.Background(), ""); err == nil {
		t.Fatalf("expected error for empty node id")
	}
	if err := h.RegisterLB(context.Background(), "", nil); err == nil {
		t.Fatalf("expected error for empty node id")
	}
	if err := h.DeregisterLB(context.Background(), "", nil); err == nil {
		t.Fatalf("expected error for empty node id")
	}
}
