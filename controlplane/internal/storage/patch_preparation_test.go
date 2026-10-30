package storage

import (
	"context"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOfflinePreparationDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: map[string]any{"patch.runtime": patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", OfflineAPTVersion: 1, OfflinePrepareVersion: 1}, ObservedAt: time.Now().UTC()}}})
	require.NoError(t, err)
	repo := patchsupport.OfflineRepository{ID: uuid.NewString(), Name: "approved", KeyringSHA256: strings.Repeat("a", 64), Suite: "noble-updates", Component: "main", Enabled: true}
	require.NoError(t, s.SetOfflineRepository(ctx, tenant.ID, node.ID, repo))
	require.NoError(t, s.UpdateNodeLabels(ctx, node.ID, node.Labels), "a heartbeat holding stale labels must not erase a newly registered repository")
	p := patchsupport.OfflinePreparation{NodeID: node.ID.String(), Repository: repo, Packages: []patchsupport.ExactPackage{{Name: "libudev1", Version: "1.2"}}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute), RequestedBy: "operator"}
	const concurrent = 4
	ids := make(chan uuid.UUID, concurrent)
	errs := make(chan error, concurrent)
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, e := s.QueueOfflinePreparation(ctx, tenant.ID, node.ID, p)
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
	var jobID uuid.UUID
	for id := range ids {
		if jobID == uuid.Nil {
			jobID = id
		}
		require.Equal(t, jobID, id)
	}
	require.NoError(t, s.UpdateNodeLabels(ctx, node.ID, node.Labels))
	current, err := s.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, jobID.String(), current.Labels["patch.offline_preparation"], "stale label writes must retain the latest preparation link")
	wrong := p
	wrong.Repository.KeyringSHA256 = strings.Repeat("b", 64)
	_, err = s.QueueOfflinePreparation(ctx, tenant.ID, node.ID, wrong)
	require.Error(t, err)
	_, err = s.QueueOfflinePreparation(ctx, uuid.New(), node.ID, p)
	require.Error(t, err)
	before, err := s.GetJob(ctx, jobID)
	require.NoError(t, err)
	var payload patchsupport.OfflinePreparation
	require.NoError(t, json.Unmarshal(before.Payload, &payload))
	require.Equal(t, p, payload)
	require.NoError(t, s.CompleteOfflinePreparation(ctx, tenant.ID, uuid.New(), jobID, true, "forged"))
	job, err := s.GetJob(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, JobStatusRunning, job.Status)
	require.NoError(t, s.CompleteOfflinePreparation(ctx, tenant.ID, node.ID, jobID, true, "prepared without installing"))
	require.NoError(t, s.CompleteOfflinePreparation(ctx, tenant.ID, node.ID, jobID, false, "late conflicting failure"))
	job, err = s.GetJob(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, JobStatusSucceeded, job.Status)
	events, err := s.ListJobEvents(ctx, jobID)
	require.NoError(t, err)
	require.Len(t, events, 2, "one request event and exactly one terminal event")
	var count int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM node_patch_state WHERE node_id=$1`, node.ID).Scan(&count))
	require.Zero(t, count, "preparation must not create an installation state")
	t.Run("late receipt after timeout remains failed and is durable", func(t *testing.T) {
		late, e := s.QueueOfflinePreparation(ctx, tenant.ID, node.ID, p)
		require.NoError(t, e)
		require.NoError(t, s.CompleteOfflinePreparation(ctx, tenant.ID, node.ID, late.ID, false, "Preparation timed out"))
		require.Error(t, s.RecordOfflinePreparationReceipt(ctx, tenant.ID, uuid.New(), late.ID, true, "forged"))
		require.NoError(t, s.RecordOfflinePreparationReceipt(ctx, tenant.ID, node.ID, late.ID, true, "prepared late; no packages installed"))
		require.NoError(t, s.RecordOfflinePreparationReceipt(ctx, tenant.ID, node.ID, late.ID, true, "duplicate"))
		require.Error(t, s.RecordOfflinePreparationReceipt(ctx, tenant.ID, node.ID, late.ID, false, "conflict"))
		stored, e := s.GetJob(ctx, late.ID)
		require.NoError(t, e)
		require.Equal(t, JobStatusFailed, stored.Status)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(stored.Payload, &fields))
		require.Equal(t, "succeeded", fields["_offline_preparation_receipt"])
		require.Equal(t, "prepared late; no packages installed", fields["_offline_preparation_receipt_message"])
		lateEvents, e := s.ListJobEvents(ctx, late.ID)
		require.NoError(t, e)
		require.Len(t, lateEvents, 2, "late receipts must not overwrite the timeout event")
		freshNode, e := s.GetNode(ctx, node.ID)
		require.NoError(t, e)
		require.Nil(t, freshNode.Labels["patch.offline_prepared_plan"], "late success after timeout must not advertise successful preparation")
	})
	t.Run("preparation identity stays bound to content and survives heartbeat", func(t *testing.T) {
		prepared, e := s.QueueOfflinePreparation(ctx, tenant.ID, node.ID, p)
		require.NoError(t, e)
		digest := strings.Repeat("d", 64)
		require.NoError(t, s.RecordOfflinePreparationReceipt(ctx, tenant.ID, node.ID, prepared.ID, true, "prepared", digest))
		fresh, e := s.GetNode(ctx, node.ID)
		require.NoError(t, e)
		provenance := fresh.Labels["patch.offline_prepared_plan"].(map[string]any)
		require.Equal(t, prepared.ID.String(), provenance["job_id"])
		require.Equal(t, digest, provenance["manifest_sha256"])
		require.NotEmpty(t, provenance["completed_at"])
		require.NoError(t, s.UpdateNodeLabels(ctx, node.ID, node.Labels))
		require.NoError(t, s.RecordOfflinePreparationReceipt(ctx, tenant.ID, node.ID, prepared.ID, true, "duplicate", strings.Repeat("e", 64)))
		fresh, e = s.GetNode(ctx, node.ID)
		require.NoError(t, e)
		require.Equal(t, provenance, fresh.Labels["patch.offline_prepared_plan"], "stale heartbeat and duplicate receipt must not replace identity or renew completion time")
	})
	repo.Enabled = false
	require.NoError(t, s.SetOfflineRepository(ctx, tenant.ID, node.ID, repo))
	_, err = s.QueueOfflinePreparation(ctx, tenant.ID, node.ID, p)
	require.Error(t, err)
}
