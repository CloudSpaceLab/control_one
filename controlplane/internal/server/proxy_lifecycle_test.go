package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type proxyReceiptTestStore struct {
	*patchTestStore
	fail   bool
	writes int
}

type proxyDeliveryTestStore struct {
	*proxyReceiptTestStore
	proxy   *storage.SquidProxy
	pending []storage.Job
}

func (s *proxyDeliveryTestStore) GetSquidProxy(context.Context, uuid.UUID) (*storage.SquidProxy, error) {
	return s.proxy, nil
}
func (s *proxyDeliveryTestStore) PendingProxyLifecycle(context.Context, uuid.UUID, uuid.UUID) ([]storage.Job, error) {
	return s.pending, nil
}

func TestProxyLifecycleHeartbeatRedeliveryIsBoundAndCapabilityGated(t *testing.T) {
	tenant, nodeID, id, proxyID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &proxyDeliveryTestStore{proxyReceiptTestStore: &proxyReceiptTestStore{patchTestStore: newPatchTestStore(tenant, nodeID)}, proxy: &storage.SquidProxy{ID: proxyID, TenantID: tenant, Status: "registered"}}
	p := patchsupport.ProxyLifecycle{NodeID: nodeID.String(), ProxyID: proxyID.String(), RequestedBy: "admin", ExpiresAt: time.Now().UTC().Add(time.Minute), Whitelist: []string{"archive.ubuntu.com"}}
	raw, _ := json.Marshal(p)
	store.pending = []storage.Job{{ID: id, TenantID: tenant, Type: "squid.reconfigure", Status: storage.JobStatusRunning, Payload: raw}}
	srv := newProxyTestServer(store)
	node := &store.nodes[0]
	require.Empty(t, srv.proxyLifecycleActions(context.Background(), node), "old agent cannot receive lifecycle jobs")
	node.Labels["patch.runtime"] = patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", ProxyLifecycleVersion: 1}, ObservedAt: time.Now().UTC()}
	for i := 0; i < 2; i++ {
		require.Equal(t, []string{"squid.reconfigure:" + id.String()}, srv.proxyLifecycleActions(context.Background(), node), "unacknowledged job is delivered again without creating another job")
	}
	for _, scenario := range []string{"expired", "wrong node", "wrong tenant", "terminal", "wrong proxy tenant", "stale runtime", "changed proxy identity"} {
		t.Run(scenario, func(t *testing.T) {
			job := store.pending[0]
			candidate := *node
			candidate.Labels = map[string]any{"patch.runtime": node.Labels["patch.runtime"]}
			payload := p
			savedProxy := *store.proxy
			defer func() { *store.proxy = savedProxy; store.pending[0] = job }()
			switch scenario {
			case "expired":
				payload.ExpiresAt = time.Now().Add(-time.Minute)
			case "wrong node":
				payload.NodeID = uuid.NewString()
			case "wrong tenant":
				store.pending[0].TenantID = uuid.New()
			case "terminal":
				store.pending[0].Status = storage.JobStatusSucceeded
			case "wrong proxy tenant":
				store.proxy.TenantID = uuid.New()
			case "stale runtime":
				candidate.Labels["patch.runtime"] = patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", ProxyLifecycleVersion: 1}, ObservedAt: time.Now().Add(-time.Hour)}
			case "changed proxy identity":
				store.proxy.ID = uuid.New()
			}
			store.pending[0].Payload, _ = json.Marshal(payload)
			require.Empty(t, srv.proxyLifecycleActions(context.Background(), &candidate))
		})
	}
}

func (s *proxyReceiptTestStore) RecordProxyLifecycleReceipt(_ context.Context, tenant, node, id uuid.UUID, action, status, message string) error {
	if s.fail {
		return fmt.Errorf("database unavailable")
	}
	s.writes++
	job := s.jobs[id]
	var p map[string]any
	_ = json.Unmarshal(job.Payload, &p)
	p["_proxy_receipt"] = status
	job.Payload, _ = json.Marshal(p)
	return nil
}

func TestProxyLifecycleAcknowledgmentRequiresCommittedOwnedReceipt(t *testing.T) {
	tenant, node, id := uuid.New(), uuid.New(), uuid.New()
	store := &proxyReceiptTestStore{patchTestStore: newPatchTestStore(tenant, node)}
	raw, _ := json.Marshal(map[string]string{"node_id": node.String(), "proxy_id": uuid.NewString()})
	store.jobs = map[uuid.UUID]*storage.Job{id: {ID: id, TenantID: tenant, Type: "squid.reconfigure", Status: storage.JobStatusRunning, Payload: raw}}
	srv := newProxyTestServer(store)
	ctx := context.Background()
	c := heartbeatCompletedAction{JobID: id.String(), Action: "squid.reconfigure", Status: "succeeded"}
	require.Empty(t, srv.persistedPatchCompletionAcknowledgments(ctx, node, []heartbeatCompletedAction{c}))
	store.fail = true
	srv.processHeartbeatCompletedActions(ctx, node, []heartbeatCompletedAction{c})
	require.Empty(t, srv.persistedPatchCompletionAcknowledgments(ctx, node, []heartbeatCompletedAction{c}))
	store.fail = false
	srv.processHeartbeatCompletedActions(ctx, uuid.New(), []heartbeatCompletedAction{c})
	require.Zero(t, store.writes)
	srv.processHeartbeatCompletedActions(ctx, node, []heartbeatCompletedAction{c})
	require.Equal(t, 1, store.writes)
	require.Equal(t, []string{id.String()}, srv.persistedPatchCompletionAcknowledgments(ctx, node, []heartbeatCompletedAction{c}))
	bad := c
	bad.Status = "failed"
	require.Empty(t, srv.persistedPatchCompletionAcknowledgments(ctx, node, []heartbeatCompletedAction{bad}))
	bad = c
	bad.Action = "squid.install"
	require.Empty(t, srv.persistedPatchCompletionAcknowledgments(ctx, node, []heartbeatCompletedAction{bad}))
	store.jobs[id].TenantID = uuid.New()
	require.Empty(t, srv.persistedPatchCompletionAcknowledgments(ctx, node, []heartbeatCompletedAction{c}))
}
