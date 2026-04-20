package provisioning

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"go.uber.org/zap"
)

func TestAzureAdapterDestroyForwardsToHTTP(t *testing.T) {
	t.Parallel()
	client := &capturingClient{}
	a := &azureAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), client)}

	if err := a.Destroy(context.Background(), "vm-azure"); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(client.calls))
	}
	if client.calls[0].Method != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", client.calls[0].Method)
	}
}

func TestAzureAdapterRegisterLBForwardsBackendPool(t *testing.T) {
	t.Parallel()
	client := &capturingClient{}
	a := &azureAdapter{httpAdapter: newHTTPAdapter(zap.NewNop(), client)}

	meta := map[string]any{"lb_backend_pool_id": "/subscriptions/xxx/providers/Microsoft.Network/loadBalancers/lb1/backendAddressPools/pool1"}
	if err := a.RegisterLB(context.Background(), "vm-1", meta); err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(client.calls))
	}
	var payload struct {
		ClusterMeta map[string]any `json:"cluster_meta"`
	}
	if err := json.Unmarshal(client.calls[0].Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.ClusterMeta["lb_backend_pool_id"] != meta["lb_backend_pool_id"] {
		t.Fatalf("expected backend pool id propagated, got %+v", payload.ClusterMeta)
	}
}
