package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type windowAcceptanceStore struct {
	*patchTestStore
	window  *storage.MaintenanceWindow
	markErr error
	marked  string
}

type failedWindowQueue struct{ stubQueue }

func (q *failedWindowQueue) Enqueue(worker.Task) error { return errors.New("queue unavailable") }

func TestCalendarWindowAPIActuallyEnqueuesWorker(t *testing.T) {
	for _, scenario := range []string{"success", "queue missing", "queue failure", "network blocked"} {
		t.Run(scenario, func(t *testing.T) {
			tenant, nodeID := uuid.New(), uuid.New()
			store := &windowAcceptanceStore{patchTestStore: newPatchTestStore(tenant, nodeID), window: &storage.MaintenanceWindow{ID: uuid.New(), TenantID: tenant, OpensAt: time.Now().Add(-time.Minute), ClosesAt: time.Now().Add(time.Hour)}}
			srv := newPatchTestServer(store)
			queue := &stubQueue{}
			srv.worker = queue
			switch scenario {
			case "queue missing":
				srv.worker = nil
			case "queue failure":
				srv.worker = &failedWindowQueue{}
			case "network blocked":
				store.window.AllowRepos = []string{"archive.ubuntu.com"}
			}
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			srv.dispatchMaintenanceWindowAction(rr, req, nil, store.window.ID, "open")
			if scenario == "success" {
				require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
				require.Len(t, queue.tasks, 1)
				require.NotEmpty(t, queue.tasks[0].DurableJob.ID)
				require.NoError(t, queue.tasks[0].Job(context.Background()))
				require.Equal(t, "open", store.marked)
			} else {
				require.GreaterOrEqual(t, rr.Code, 400)
				require.Empty(t, queue.tasks)
				require.Empty(t, store.marked)
			}
			if scenario == "queue missing" || scenario == "network blocked" {
				require.Empty(t, store.jobs)
			}
			if scenario == "queue failure" {
				for _, job := range store.jobs {
					require.Equal(t, storage.JobStatusFailed, job.Status)
				}
			}
		})
	}
}

func (s *windowAcceptanceStore) GetMaintenanceWindow(context.Context, uuid.UUID) (*storage.MaintenanceWindow, error) {
	return s.window, nil
}
func (s *windowAcceptanceStore) MarkMaintenanceWindowOpen(context.Context, uuid.UUID, *uuid.UUID) error {
	if s.markErr == nil {
		s.marked = "open"
	}
	return s.markErr
}
func (s *windowAcceptanceStore) MarkMaintenanceWindowClosed(context.Context, uuid.UUID) error {
	if s.markErr == nil {
		s.marked = "closed"
	}
	return s.markErr
}

func TestCalendarWindowWorkerDoesNotClaimNetworkEnforcement(t *testing.T) {
	for _, scenario := range []string{"calendar open", "calendar close", "network open", "network close", "expired", "force closed", "other tenant", "store failure"} {
		t.Run(scenario, func(t *testing.T) {
			tenant, nodeID := uuid.New(), uuid.New()
			store := &windowAcceptanceStore{patchTestStore: newPatchTestStore(tenant, nodeID), window: &storage.MaintenanceWindow{ID: uuid.New(), TenantID: tenant, NodeIDs: []uuid.UUID{nodeID}, OpensAt: time.Now().Add(-time.Minute), ClosesAt: time.Now().Add(time.Hour)}}
			action := "open"
			switch scenario {
			case "calendar close":
				action = "close"
			case "network open":
				store.window.AllowRepos = []string{"archive.ubuntu.com"}
			case "network close":
				store.window.AllowRepos = []string{"archive.ubuntu.com"}
				action = "close"
			case "expired":
				store.window.ClosesAt = time.Now().Add(-time.Second)
			case "force closed":
				now := time.Now()
				store.window.ForceClosedAt = &now
			case "other tenant":
				store.window.TenantID = uuid.New()
			case "store failure":
				store.markErr = errors.New("database unavailable")
			}
			srv := newPatchTestServer(store.patchTestStore)
			srv.store = store
			payload, _ := json.Marshal(map[string]string{"window_id": store.window.ID.String(), "action": action})
			err := srv.handlePatchWindowJob(context.Background(), &storage.Job{TenantID: tenant, Payload: payload})
			if scenario == "calendar open" || scenario == "calendar close" {
				require.NoError(t, err)
				require.NotEmpty(t, store.marked)
			} else {
				require.Error(t, err)
				require.Empty(t, store.marked)
			}
			require.Empty(t, store.jobs, "must not fan out unconfirmed firewall commands")
		})
	}
}
