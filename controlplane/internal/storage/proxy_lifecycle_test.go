package storage

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProxyLifecycleDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: map[string]any{"patch.runtime": patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", ProxyLifecycleVersion: 1}, ObservedAt: time.Now().UTC()}}})
	require.NoError(t, err)
	proxy, err := s.CreateSquidProxy(ctx, SquidProxy{TenantID: tenant.ID, Host: "proxy.example.invalid", Port: 3128})
	require.NoError(t, err)
	p := patchsupport.ProxyLifecycle{NodeID: node.ID.String(), ProxyID: proxy.ID.String(), RequestedBy: "test-admin", ExpiresAt: time.Now().UTC().Add(10 * time.Minute), Whitelist: []string{"archive.ubuntu.com"}}
	const concurrent = 6
	ids := make(chan uuid.UUID, concurrent)
	errs := make(chan error, concurrent)
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.reconfigure", p)
			errs <- e
			if e == nil {
				ids <- j.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var id uuid.UUID
	for current := range ids {
		if id == uuid.Nil {
			id = current
		}
		require.Equal(t, id, current)
	}
	jobs, err := s.PendingProxyLifecycle(ctx, tenant.ID, node.ID)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	// Read again through a new Store using the same database. No memory queue.
	other := *s
	jobs, err = other.PendingProxyLifecycle(ctx, tenant.ID, node.ID)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, id, jobs[0].ID)
	_, err = s.QueueProxyLifecycle(ctx, uuid.New(), node.ID, "squid.reconfigure", p)
	require.Error(t, err)
	changed := p
	changed.Whitelist = []string{"security.ubuntu.com"}
	_, err = s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.reconfigure", changed)
	require.Error(t, err)
	_, err = s.DispatchPatchJob(ctx, DispatchPatchJobParams{TenantID: tenant.ID, NodeID: node.ID, Mode: "direct", Payload: json.RawMessage(`{}`)})
	require.ErrorContains(t, err, "unreconciled proxy action")
	second, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: node.Labels})
	require.NoError(t, err)
	otherTarget := p
	otherTarget.NodeID = second.ID.String()
	_, err = s.QueueProxyLifecycle(ctx, tenant.ID, second.ID, "squid.reconfigure", otherTarget)
	require.ErrorContains(t, err, "unreconciled proxy action", "same proxy cannot be changed through a second node")
	require.Error(t, s.RecordProxyLifecycleReceipt(ctx, tenant.ID, uuid.New(), id, "squid.reconfigure", "succeeded", "forged"))
	require.Error(t, s.RecordProxyLifecycleReceipt(ctx, uuid.New(), node.ID, id, "squid.reconfigure", "succeeded", "forged"))
	require.Error(t, s.RecordProxyLifecycleReceipt(ctx, tenant.ID, node.ID, id, "squid.install", "succeeded", "wrong action"))
	require.Error(t, s.RecordProxyLifecycleReceipt(ctx, tenant.ID, node.ID, id, "squid.reconfigure", "running", "not terminal"))
	// A timeout does not release an unknown remote operation.
	_, err = s.db.ExecContext(ctx, `UPDATE jobs SET status='failed',finished_at=NOW() WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.reconfigure", changed)
	require.Error(t, err)
	require.NoError(t, s.RecordProxyLifecycleReceipt(ctx, tenant.ID, node.ID, id, "squid.reconfigure", "failed", "interrupted; inspect configuration"))
	require.NoError(t, s.RecordProxyLifecycleReceipt(ctx, tenant.ID, node.ID, id, "squid.reconfigure", "failed", "duplicate must not replace evidence"))
	require.Error(t, s.RecordProxyLifecycleReceipt(ctx, tenant.ID, node.ID, id, "squid.reconfigure", "succeeded", "conflicting"))
	events, err := s.ListJobEvents(ctx, id)
	require.NoError(t, err)
	require.Len(t, events, 2)
	job, err := s.GetJob(ctx, id)
	require.NoError(t, err)
	require.Equal(t, JobStatusFailed, job.Status)
	var receipt map[string]any
	require.NoError(t, json.Unmarshal(job.Payload, &receipt))
	require.Equal(t, "interrupted; inspect configuration", receipt["_proxy_receipt_message"])
	newJob, err := s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.reconfigure", changed)
	require.NoError(t, err)
	require.NotEqual(t, id, newJob.ID)
	proxy, err = s.GetSquidProxy(ctx, proxy.ID)
	require.NoError(t, err)
	require.Equal(t, "registered", proxy.Status)
	require.Nil(t, proxy.LastValidatedAt)
}
