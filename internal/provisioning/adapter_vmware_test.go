package provisioning

import (
	"context"
	"encoding/json"
	"testing"

	"go.uber.org/zap"
)

func TestVMwareAdapterDestroyForwardsToHTTP(t *testing.T) {
	t.Parallel()
	client := &capturingClient{}
	a := &vmwareAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), client)}

	if err := a.Destroy(context.Background(), "vm-vsphere"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(client.calls))
	}
}

func TestVMwareAdapterRegisterLBForwardsNSXPool(t *testing.T) {
	t.Parallel()
	client := &capturingClient{}
	a := &vmwareAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), client)}

	meta := map[string]any{"lb_pool": "nsx-pool-1"}
	if err := a.RegisterLB(context.Background(), "vm-1", meta); err != nil {
		t.Fatalf("register: %v", err)
	}
	var payload struct {
		ClusterMeta map[string]any `json:"cluster_meta"`
	}
	if err := json.Unmarshal(client.calls[0].Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.ClusterMeta["lb_pool"] != "nsx-pool-1" {
		t.Fatalf("expected lb_pool propagated, got %+v", payload.ClusterMeta)
	}
}
