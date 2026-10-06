package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type allTenantScopeStore struct {
	fakeStore
	accessible []storage.Tenant
	nodeScope  []uuid.UUID
	riskScope  []uuid.UUID
	jobScope   []uuid.UUID
}

func (f *allTenantScopeStore) ListAccessibleTenants(
	_ context.Context,
	_ uuid.UUID,
	_ []string,
	_ string,
	_, _ int,
) ([]storage.Tenant, int, error) {
	return append([]storage.Tenant(nil), f.accessible...), len(f.accessible), nil
}

func (f *allTenantScopeStore) ListNodesForTenants(
	_ context.Context,
	tenantIDs []uuid.UUID,
	_ string,
	_, _ int,
) ([]storage.Node, int, error) {
	f.nodeScope = append([]uuid.UUID(nil), tenantIDs...)
	if len(tenantIDs) == 0 {
		return []storage.Node{}, 0, nil
	}
	return []storage.Node{{
		ID:       uuid.New(),
		TenantID: tenantIDs[0],
		Hostname: "scoped-node",
		State:    storage.NodeStateActive,
		Labels:   map[string]any{},
	}}, 1, nil
}

func (f *allTenantScopeStore) ListAtRiskNodesForTenants(
	_ context.Context,
	tenantIDs []uuid.UUID,
	_ int,
) ([]storage.AtRiskNodeRow, error) {
	f.riskScope = append([]uuid.UUID(nil), tenantIDs...)
	if len(tenantIDs) == 0 {
		return []storage.AtRiskNodeRow{}, nil
	}
	return []storage.AtRiskNodeRow{{
		NodeID:     uuid.New(),
		TenantID:   tenantIDs[0],
		Hostname:   "scoped-node",
		Score:      20,
		RiskLevel:  "critical",
		Components: map[string]any{},
		ComputedAt: time.Now().UTC(),
	}}, nil
}

func (f *allTenantScopeStore) ListJobsForTenants(
	_ context.Context,
	tenantIDs []uuid.UUID,
	jobType string,
	_ storage.JobStatus,
	_, _ int,
) ([]storage.Job, int, error) {
	f.jobScope = append([]uuid.UUID(nil), tenantIDs...)
	if len(tenantIDs) == 0 {
		return []storage.Job{}, 0, nil
	}
	return []storage.Job{{
		ID:        uuid.New(),
		TenantID:  tenantIDs[0],
		Type:      jobType,
		Status:    storage.JobStatusQueued,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}}, 1, nil
}

func TestAllTenantReadHandlersUseAccessibleTenantScope(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	first := storage.Tenant{ID: uuid.New(), Name: "Bank A"}
	second := storage.Tenant{ID: uuid.New(), Name: "Bank B"}
	store := &allTenantScopeStore{
		accessible: []storage.Tenant{first, second},
		fakeStore: fakeStore{users: map[string]*storage.User{
			"viewer-subject": {ID: userID, ExternalID: "viewer-subject"},
		}},
	}
	server := &Server{store: store, logger: zap.NewNop()}
	principal := &auth.Principal{
		Type:    "user",
		Subject: "viewer-subject",
		Roles:   []string{roleViewer},
	}
	want := []uuid.UUID{first.ID, second.ID}

	t.Run("nodes", func(t *testing.T) {
		req := withPrincipal(
			httptest.NewRequest(http.MethodGet, "/api/v1/nodes?limit=25", nil),
			principal,
		)
		rec := httptest.NewRecorder()

		server.handleListNodes(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		assertUUIDScope(t, store.nodeScope, want)
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		data, ok := body["data"].([]any)
		if !ok || len(data) != 1 {
			t.Fatalf("unexpected node payload: %s", rec.Body.String())
		}
	})

	t.Run("predictive risk", func(t *testing.T) {
		req := withPrincipal(
			httptest.NewRequest(http.MethodGet, "/api/v1/health/at-risk", nil),
			principal,
		)
		rec := httptest.NewRecorder()

		server.handleAtRiskFleet(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		assertUUIDScope(t, store.riskScope, want)
	})

	t.Run("jobs", func(t *testing.T) {
		req := withPrincipal(
			httptest.NewRequest(http.MethodGet, "/api/v1/jobs?type=agent.update&limit=100", nil),
			principal,
		)
		rec := httptest.NewRecorder()

		server.handleListJobs(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		assertUUIDScope(t, store.jobScope, want)
	})
}

func assertUUIDScope(t *testing.T, got, want []uuid.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("scope=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scope=%v want=%v", got, want)
		}
	}
}
