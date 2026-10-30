package server

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type preparationTestStore struct {
	*patchSecurityStore
	requested   *patchsupport.OfflinePreparation
	completed   int
	wantedRoles []string
	pending     []storage.Job
}

func (s *preparationTestStore) UserHasTenantRole(_ context.Context, _ uuid.UUID, _ uuid.UUID, roles []string) (bool, error) {
	s.wantedRoles = append([]string(nil), roles...)
	return s.allowed, nil
}

func (s *preparationTestStore) SetOfflineRepository(_ context.Context, tenant, node uuid.UUID, repo patchsupport.OfflineRepository) error {
	s.nodes[0].Labels["patch.offline_repository"] = repo
	return nil
}
func (s *preparationTestStore) QueueOfflinePreparation(_ context.Context, tenant, node uuid.UUID, p patchsupport.OfflinePreparation) (*storage.Job, error) {
	s.requested = &p
	body, _ := json.Marshal(p)
	return &storage.Job{ID: uuid.New(), TenantID: tenant, Type: patchsupport.PrepareOfflineJob, Status: storage.JobStatusRunning, Payload: body}, nil
}
func (s *preparationTestStore) PendingOfflinePreparations(context.Context, uuid.UUID, uuid.UUID) ([]storage.Job, error) {
	return s.pending, nil
}

func TestOfflinePreparationDispatchRejectsExpiredOrChangedSource(t *testing.T) {
	for _, scenario := range []string{"current", "expired", "disabled", "changed", "inactive"} {
		t.Run(scenario, func(t *testing.T) {
			tenant, node := uuid.New(), uuid.New()
			store := &preparationTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: newPatchTestStore(tenant, node), allowed: true}}
			repo := patchsupport.OfflineRepository{ID: uuid.NewString(), Name: "approved", Enabled: true, KeyringSHA256: strings.Repeat("a", 64), Suite: "noble-updates", Component: "main"}
			p := patchsupport.OfflinePreparation{NodeID: node.String(), Repository: repo, Packages: []patchsupport.ExactPackage{{Name: "libudev1", Version: "1.2"}}, ExpiresAt: time.Now().Add(5 * time.Minute), RequestedBy: "operator"}
			switch scenario {
			case "expired":
				p.ExpiresAt = time.Now().Add(-time.Minute)
			case "disabled":
				repo.Enabled = false
			case "changed":
				repo.KeyringSHA256 = strings.Repeat("b", 64)
			case "inactive":
				store.nodes[0].State = "inactive"
			}
			store.nodes[0].Labels["patch.offline_repository"] = repo
			body, _ := json.Marshal(p)
			store.pending = []storage.Job{{ID: uuid.New(), TenantID: tenant, Type: patchsupport.PrepareOfflineJob, Payload: body}}
			srv := &Server{store: store, logger: zap.NewNop()}
			actions := srv.offlinePreparationActions(context.Background(), &store.nodes[0])
			if scenario == "current" {
				require.Len(t, actions, 1)
				require.Zero(t, store.completed)
			} else {
				require.Empty(t, actions)
				require.Equal(t, 1, store.completed)
			}
		})
	}
}
func (s *preparationTestStore) CompleteOfflinePreparation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool, string) error {
	s.completed++
	return nil
}

func (s *preparationTestStore) RecordOfflinePreparationReceipt(ctx context.Context, tenant, node, job uuid.UUID, succeeded bool, message string, manifest ...string) error {
	return s.CompleteOfflinePreparation(ctx, tenant, node, job, succeeded, message)
}

func TestOfflinePreparationAcknowledgmentRequiresDurableReceipt(t *testing.T) {
	tenant, node, jobID := uuid.New(), uuid.New(), uuid.New()
	store := &preparationTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: newPatchTestStore(tenant, node), allowed: true}}
	store.jobs = map[uuid.UUID]*storage.Job{}
	fields := map[string]any{"node_id": node.String()}
	body, _ := json.Marshal(fields)
	store.jobs[jobID] = &storage.Job{ID: jobID, TenantID: tenant, Type: patchsupport.PrepareOfflineJob, Status: storage.JobStatusFailed, Payload: body}
	srv := &Server{store: store, logger: zap.NewNop()}
	completion := []heartbeatCompletedAction{{Action: patchsupport.PrepareOfflineJob, JobID: jobID.String(), Status: "succeeded"}}
	require.Empty(t, srv.persistedPatchCompletionAcknowledgments(context.Background(), node, completion))
	fields["_offline_preparation_receipt"] = "succeeded"
	body, _ = json.Marshal(fields)
	store.jobs[jobID].Payload = body
	require.Equal(t, []string{jobID.String()}, srv.persistedPatchCompletionAcknowledgments(context.Background(), node, completion))
	require.Empty(t, srv.persistedPatchCompletionAcknowledgments(context.Background(), uuid.New(), completion))
	completion[0].Status = "failed"
	require.Empty(t, srv.persistedPatchCompletionAcknowledgments(context.Background(), node, completion))
}

func TestOfflineRepositoryAdministrationAndAgentOwnership(t *testing.T) {
	tenant, node, user := uuid.New(), uuid.New(), uuid.New()
	store := &preparationTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: newPatchTestStore(tenant, node), allowed: true}}
	store.users = map[string]*storage.User{"admin": {ID: user, ExternalID: "admin"}}
	srv := &Server{store: store, logger: zap.NewNop()}
	repo := patchsupport.OfflineRepository{Name: "approved", KeyringSHA256: strings.Repeat("a", 64), Suite: "noble-updates", Component: "main", Enabled: true}
	raw, _ := json.Marshal(map[string]any{"node_id": node.String(), "repository": repo})
	put := httptest.NewRequest(http.MethodPut, "/api/v1/patch/offline/repository", bytes.NewReader(raw))
	put = put.WithContext(context.WithValue(put.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "user", Subject: "admin", Roles: []string{roleAdmin}}))
	w := httptest.NewRecorder()
	srv.handleOfflineRepository(w, put)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, []string{roleAdmin}, store.wantedRoles)
	for _, id := range []uuid.UUID{node, uuid.New()} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/patch/offline/repository?node_id="+node.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "agent", Name: id.String()}))
		w := httptest.NewRecorder()
		srv.handleOfflineRepository(w, req)
		if id == node {
			require.Equal(t, 200, w.Code, w.Body.String())
		} else {
			require.Equal(t, 403, w.Code)
		}
	}
	jobID := uuid.New()
	body, _ := json.Marshal(map[string]any{"node_id": node.String()})
	store.jobs = map[uuid.UUID]*storage.Job{}
	store.jobs[jobID] = &storage.Job{ID: jobID, TenantID: tenant, Type: patchsupport.PrepareOfflineJob, Status: storage.JobStatusRunning, Payload: body}
	c := heartbeatCompletedAction{Action: patchsupport.PrepareOfflineJob, JobID: jobID.String(), Status: "succeeded"}
	srv.completeOfflinePreparation(context.Background(), uuid.New(), c)
	require.Zero(t, store.completed)
	srv.completeOfflinePreparation(context.Background(), node, c)
	require.Equal(t, 1, store.completed)
}

func TestOfflinePreparationRoutesAndSeparation(t *testing.T) {
	tenant, node, user := uuid.New(), uuid.New(), uuid.New()
	store := &preparationTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: newPatchTestStore(tenant, node), allowed: true}}
	store.users = map[string]*storage.User{"operator": {ID: user, ExternalID: "operator"}}
	repo := patchsupport.OfflineRepository{ID: uuid.NewString(), Name: "approved", KeyringSHA256: strings.Repeat("a", 64), Suite: "noble-updates", Component: "main", Enabled: true}
	store.nodes[0].Labels["patch.offline_repository"] = repo
	store.nodes[0].Labels["patch.runtime"] = patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", OfflineAPTVersion: 1, OfflinePrepareVersion: 1}, ObservedAt: time.Now().UTC()}
	srv := &Server{store: store, logger: zap.NewNop()}
	request := func(body map[string]any, roles []string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/patch/offline/prepare", bytes.NewReader(raw))
		req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "user", Subject: "operator", Roles: roles}))
		w := httptest.NewRecorder()
		srv.handleOfflinePreparation(w, req)
		return w
	}
	body := map[string]any{"node_id": node.String(), "repository_id": repo.ID, "packages": []patchsupport.ExactPackage{{Name: "libudev1", Version: "1.2"}}}
	w := request(body, []string{roleOperator})
	require.Equal(t, 202, w.Code, w.Body.String())
	require.NotNil(t, store.requested)
	require.Empty(t, store.deployments)
	require.Empty(t, store.states)
	require.Empty(t, store.jobs)
	body["command"] = "apt-get upgrade"
	w = request(body, []string{roleAdmin})
	require.Equal(t, 400, w.Code)
	delete(body, "command")
	store.allowed = false
	w = request(body, []string{roleOperator})
	require.Equal(t, 403, w.Code)
	store.allowed = true
	body["repository_id"] = uuid.NewString()
	w = request(body, []string{roleAdmin})
	require.Equal(t, 409, w.Code)
}
