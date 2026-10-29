package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type patchEffectsStore struct {
	*patchCompletionStore
	ready bool
	err   error
	calls int
}

func (s *patchEffectsStore) CompletePatchJob(context.Context, storage.CompletePatchJobParams) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	s.ready = true
	return false, nil // Already terminal: side effects were reconciled.
}

func (s *patchEffectsStore) PatchCompletionEffectsCommitted(context.Context, uuid.UUID) (bool, error) {
	return s.ready, s.err
}

func TestPatchCompletionRetriesReconcileBeforeAcknowledgment(t *testing.T) {
	for _, scenario := range []string{"missing_effects", "failed_repair", "ready"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, nodeID, stateID, deploymentID, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			payload, err := json.Marshal(patchJobPayload{NodeID: nodeID.String(), DeploymentID: deploymentID.String(), NodePatchStateID: stateID.String(), PostPatchRescan: true})
			require.NoError(t, err)
			ps := &storage.NodePatchState{ID: stateID, TenantID: tenantID, NodeID: nodeID, DeploymentID: deploymentID, JobID: &jobID, Status: "applied"}
			job := &storage.Job{ID: jobID, TenantID: tenantID, Type: JobTypePatchDeployDirect, Status: storage.JobStatusSucceeded, Payload: payload}
			store := &patchEffectsStore{patchCompletionStore: &patchCompletionStore{fakeStore: &fakeStore{jobs: map[uuid.UUID]*storage.Job{jobID: job}}, state: ps}}
			if scenario == "failed_repair" {
				store.err = errors.New("receipt storage unavailable")
			}
			if scenario == "ready" {
				store.ready = true
			}
			srv := &Server{store: store, logger: zap.NewNop()}
			reports := []heartbeatCompletedAction{{Action: job.Type, JobID: jobID.String(), Status: "succeeded"}}
			if !store.ready {
				require.Empty(t, srv.persistedPatchCompletionAcknowledgments(context.Background(), nodeID, reports))
			}
			rescan := srv.processHeartbeatCompletedActions(context.Background(), nodeID, reports)
			require.Equal(t, 1, store.calls, "terminal retries must reach reconciliation")
			if store.err != nil {
				require.False(t, rescan)
				require.Empty(t, srv.persistedPatchCompletionAcknowledgments(context.Background(), nodeID, reports))
			} else {
				require.True(t, rescan, "lost response must preserve inventory refresh request")
				require.Equal(t, []string{jobID.String()}, srv.persistedPatchCompletionAcknowledgments(context.Background(), nodeID, reports))
			}
		})
	}
}
