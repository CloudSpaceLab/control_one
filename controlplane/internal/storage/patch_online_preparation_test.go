package storage

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOnlinePreparationDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: map[string]any{"patch.runtime": patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", OnlinePlanVersion: 1}, ObservedAt: time.Now().UTC()}}})
	require.NoError(t, err)
	p := patchsupport.OnlinePreparation{NodeID: node.ID.String(), Mode: "direct", TransportContext: strings.Repeat("a", 64), Packages: []string{"liba"}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute), RequestedBy: "test"}
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, e := s.QueueOnlinePreparation(ctx, tenant.ID, node.ID, p)
			errs <- e
			if e == nil {
				ids <- job.ID
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
	for candidate := range ids {
		if id == uuid.Nil {
			id = candidate
		}
		require.Equal(t, id, candidate, "concurrent equivalent requests must deduplicate")
	}
	changed := p
	changed.Packages = []string{"libb"}
	_, err = s.QueueOnlinePreparation(ctx, tenant.ID, node.ID, changed)
	require.Error(t, err)
	require.NoError(t, s.UpdateNodeLabels(ctx, node.ID, node.Labels))
	fresh, err := s.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, id.String(), fresh.Labels["patch.online_preparation"], "stale heartbeat cannot erase job provenance")
	now := time.Now().UTC()
	plan := patchsupport.OnlinePlan{Version: 1, NodeID: p.NodeID, Mode: p.Mode, TransportContext: p.TransportContext, InstalledStateSHA256: strings.Repeat("b", 64), InputsSHA256: strings.Repeat("c", 64), ManifestSHA256: strings.Repeat("d", 64), Requested: p.Packages, Resolved: []patchsupport.ExactPackage{}, Review: patchsupport.OfflineReview{PreparedAt: now, ExpiresAt: now.Add(time.Hour), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []patchsupport.PlanChange{}}}
	plan.SHA256 = plan.Digest()
	require.Error(t, s.FinishOnlinePreparation(ctx, tenant.ID, uuid.New(), id, true, "succeeded", "forged", &plan), "another node cannot report this job")
	require.NoError(t, s.FinishOnlinePreparation(ctx, tenant.ID, node.ID, id, true, "succeeded", "prepared only", &plan))
	job, err := s.GetJob(ctx, id)
	require.NoError(t, err)
	require.Equal(t, JobStatusSucceeded, job.Status)
	fresh, err = s.GetNode(ctx, node.ID)
	require.NoError(t, err)
	before := fresh.Labels["patch.online_plan"]
	require.NoError(t, s.UpdateNodeLabels(ctx, node.ID, node.Labels))
	fresh, err = s.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, before, fresh.Labels["patch.online_plan"], "stale labels cannot erase immutable plan or completion provenance")
	require.NoError(t, s.FinishOnlinePreparation(ctx, tenant.ID, node.ID, id, true, "succeeded", "duplicate", &plan))
	fresh, err = s.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, before, fresh.Labels["patch.online_plan"], "duplicate receipt cannot renew publication time")
	require.Error(t, s.FinishOnlinePreparation(ctx, tenant.ID, node.ID, id, true, "failed", "conflict", nil))
	require.NoError(t, s.RefreshOnlinePlanReport(ctx, tenant.ID, node.ID, strings.Repeat("e", 64), "unavailable"))
	fresh, err = s.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, before, fresh.Labels["patch.online_plan"], "stale report for another digest cannot withdraw this plan")
	require.NoError(t, s.RefreshOnlinePlanReport(ctx, tenant.ID, node.ID, plan.SHA256, "unavailable"))
	fresh, err = s.GetNode(ctx, node.ID)
	require.NoError(t, err)
	record := fresh.Labels["patch.online_plan"].(map[string]any)
	require.Equal(t, "unavailable", record["status"])
	require.Equal(t, id.String(), record["job_id"])
	// A late success is acknowledged durably but cannot revive the timed-out job.
	late, err := s.QueueOnlinePreparation(ctx, tenant.ID, node.ID, p)
	require.NoError(t, err)
	require.NoError(t, s.FinishOnlinePreparation(ctx, tenant.ID, node.ID, late.ID, false, "failed", "timed out", nil))
	require.NoError(t, s.FinishOnlinePreparation(ctx, tenant.ID, node.ID, late.ID, true, "succeeded", "late result", &plan))
	lateJob, err := s.GetJob(ctx, late.ID)
	require.NoError(t, err)
	require.Equal(t, JobStatusFailed, lateJob.Status)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(lateJob.Payload, &fields))
	var receipt string
	require.NoError(t, json.Unmarshal(fields["_online_preparation_receipt"], &receipt))
	require.Equal(t, "succeeded", receipt)
	fresh, err = s.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, id.String(), fresh.Labels["patch.online_plan"].(map[string]any)["job_id"])
}
