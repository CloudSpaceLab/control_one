package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManagedProxyLifecycleDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: map[string]any{"patch.runtime": patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", ProxyLifecycleVersion: 2}, ObservedAt: time.Now().UTC()}}})
	require.NoError(t, err)
	proxy, err := s.CreateSquidProxy(ctx, SquidProxy{TenantID: tenant.ID, Host: node.Hostname, Port: 3128, Whitelist: []string{"archive.ubuntu.com"}})
	require.NoError(t, err)
	clients := []string{"127.0.0.1/32"}
	probes := []string{"https://archive.ubuntu.com/"}
	_, err = s.BindProxyManagement(ctx, uuid.New(), proxy.ID, node.ID, clients, probes)
	require.Error(t, err)
	policy, err := s.BindProxyManagement(ctx, tenant.ID, proxy.ID, node.ID, clients, probes)
	require.NoError(t, err)
	require.EqualValues(t, 1, policy.Revision)
	_, err = s.BindProxyManagement(ctx, tenant.ID, proxy.ID, node.ID, clients, probes)
	require.Error(t, err, "host binding is immutable")
	p := patchsupport.ProxyLifecycle{ProxyID: proxy.ID.String(), NodeID: node.ID.String(), Revision: 2, RequestedBy: "admin", Whitelist: proxy.Whitelist, ClientCIDRs: clients, ProbeURLs: probes, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	job, err := s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.reconfigure", p)
	require.NoError(t, err)
	require.Error(t, s.RecordProxyLifecycleReceipt(ctx, tenant.ID, node.ID, job.ID, "squid.reconfigure", "succeeded", "no proof"))
	proof := patchsupport.ProxyLifecycleProof{ServiceActive: true, UpstreamVerified: true, ObservedAt: time.Now().UTC()}
	require.Error(t, s.RecordManagedProxyReceipt(ctx, tenant.ID, node.ID, job.ID, "squid.reconfigure", "succeeded", "wrong digest", proof))
	saved, err := s.GetJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, JobStatusRunning, saved.Status)
	proof.ConfigSHA256, err = patchsupport.ProxyConfigSHA(p.Whitelist, p.ClientCIDRs)
	require.NoError(t, err)
	require.NoError(t, s.RecordManagedProxyReceipt(ctx, tenant.ID, node.ID, job.ID, "squid.reconfigure", "succeeded", "service and HTTPS verified", proof))
	require.NoError(t, s.RecordManagedProxyReceipt(ctx, tenant.ID, node.ID, job.ID, "squid.reconfigure", "succeeded", "duplicate", proof))
	proxy, err = s.GetSquidProxy(ctx, proxy.ID)
	require.NoError(t, err)
	require.Equal(t, "healthy", proxy.Status)
	require.NotNil(t, proxy.LastValidatedAt)
	stale := p
	_, err = s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.reconfigure", stale)
	require.Error(t, err, "old policy revision cannot authorize a new job")
	p.Revision = 3
	job, err = s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.reconfigure", p)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `UPDATE jobs SET payload=payload||jsonb_build_object('expires_at',NOW()-INTERVAL '1 second') WHERE id=$1`, job.ID)
	require.NoError(t, err)
	require.NoError(t, s.ExpireManagedProxy(ctx, tenant.ID, proxy.ID, job.ID))
	proof.ObservedAt = time.Now().UTC().Add(-2 * time.Second)
	require.NoError(t, s.RecordManagedProxyReceipt(ctx, tenant.ID, node.ID, job.ID, "squid.reconfigure", "succeeded", "late success cannot certify health", proof))
	proxy, err = s.GetSquidProxy(ctx, proxy.ID)
	require.NoError(t, err)
	require.Equal(t, "degraded", proxy.Status)
	require.Nil(t, proxy.LastValidatedAt)
	removal := p
	removal.Revision = 4
	removal.Whitelist = nil
	removal.ClientCIDRs = nil
	removal.ProbeURLs = nil
	_, err = s.UpsertNodePatchConfig(ctx, NodePatchConfig{NodeID: node.ID, Mode: "proxy", ProxyID: &proxy.ID})
	require.NoError(t, err)
	_, err = s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.remove", removal)
	require.ErrorContains(t, err, "reassign")
	_, err = s.UpsertNodePatchConfig(ctx, NodePatchConfig{NodeID: node.ID, Mode: "direct"})
	require.NoError(t, err)
	job, err = s.QueueProxyLifecycle(ctx, tenant.ID, node.ID, "squid.remove", removal)
	require.NoError(t, err)
	_, err = s.UpsertNodePatchConfig(ctx, NodePatchConfig{NodeID: node.ID, Mode: "proxy", ProxyID: &proxy.ID})
	require.Error(t, err, "a removing proxy cannot acquire new clients")
	require.Error(t, s.RecordManagedProxyReceipt(ctx, tenant.ID, node.ID, job.ID, "squid.remove", "succeeded", "still active", proof))
	removed := patchsupport.ProxyLifecycleProof{Removed: true, ObservedAt: time.Now().UTC()}
	require.NoError(t, s.RecordManagedProxyReceipt(ctx, tenant.ID, node.ID, job.ID, "squid.remove", "succeeded", "service stopped and configuration restored; packages retained", removed))
	proxy, err = s.GetSquidProxy(ctx, proxy.ID)
	require.NoError(t, err)
	require.Equal(t, "removed", proxy.Status)
	saved, err = s.GetJob(ctx, job.ID)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(saved.Payload, &fields))
	require.NotEmpty(t, fields["_proxy_proof"])
	replacement, err := s.CreateSquidProxy(ctx, SquidProxy{TenantID: tenant.ID, Host: node.Hostname, Port: 3128, Whitelist: []string{"archive.ubuntu.com"}})
	require.NoError(t, err)
	_, err = s.BindProxyManagement(ctx, tenant.ID, replacement.ID, node.ID, clients, probes)
	require.NoError(t, err, "retired bindings preserve history without permanently reserving the host")
}
