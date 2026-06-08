package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// dashboardAdminHarness wires a server with a fake store and a single
// configurable role so each test can flip between admin and viewer easily.
func dashboardAdminHarness(t *testing.T, role, token string) (*Server, *fakeStore) {
	t.Helper()
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens(role, token),
	}
	store := &fakeStore{
		userRoles: map[uuid.UUID][]string{},
		tenants: []storage.Tenant{
			{ID: uuid.New(), Name: "Acme"},
		},
	}
	srv := New(logger, cfg, store, &stubQueue{})
	return srv, store
}

func dashboardCall(t *testing.T, srv *Server, token, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAdminSelfHealthRoleGate(t *testing.T) {
	t.Run("admin can read self-health", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "admin", "admin-token")
		rec := dashboardCall(t, srv, "admin-token", http.MethodGet, "/api/v1/admin/self-health")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp adminSelfHealthResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Status == "" {
			t.Fatalf("expected status field populated")
		}
	})
	t.Run("viewer is denied", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
		rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/admin/self-health")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d", rec.Code)
		}
	})
}

func TestAdminIngestThroughputRoleGate(t *testing.T) {
	t.Run("admin gets empty series envelope", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "admin", "admin-token")
		rec := dashboardCall(t, srv, "admin-token", http.MethodGet, "/api/v1/admin/ingest/throughput?stream=events&interval=1m")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		var resp ingestThroughputResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Stream != "events" || resp.Interval != "1m" {
			t.Fatalf("query params not echoed: %+v", resp)
		}
		if resp.Series == nil {
			t.Fatalf("series should be a non-nil slice")
		}
	})
	t.Run("viewer is denied", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
		rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/admin/ingest/throughput")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d", rec.Code)
		}
	})
}

func TestAdminIngestBacklogRoleGate(t *testing.T) {
	t.Run("admin sees durable replay backlog", func(t *testing.T) {
		srv, store := dashboardAdminHarness(t, "admin", "admin-token")
		lastErr := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
		store.eventIngestBacklog = storage.EventIngestBacklogSummary{
			PendingBatches:   2,
			PendingRows:      42,
			DueBatches:       1,
			RetryingBatches:  2,
			MaxRetryCount:    3,
			LastErrorAt:      sql.NullTime{Time: lastErr, Valid: true},
			LastErrorMessage: sql.NullString{String: "stream load timeout", Valid: true},
		}
		rec := dashboardCall(t, srv, "admin-token", http.MethodGet, "/api/v1/admin/ingest/backlog")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp ingestBacklogResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Status != "degraded" || resp.AnalyticsStatus != "degraded" || resp.AnalyticsMode != analyticsModeSmall || resp.WarehouseStatus != "disabled" {
			t.Fatalf("small-mode backlog should be degraded without requiring a warehouse: %+v", resp)
		}
		if resp.PendingBatches != 2 || resp.PendingRows != 42 || resp.LastErrorMessage == "" {
			t.Fatalf("unexpected backlog response: %+v", resp)
		}
	})
	t.Run("explicit OLAP without warehouse is down when replay is pending", func(t *testing.T) {
		srv, store := dashboardAdminHarness(t, "admin", "admin-token")
		srv.cfg.Analytics.Mode = analyticsModeOLAP
		store.eventIngestBacklog = storage.EventIngestBacklogSummary{
			PendingBatches: 1,
			PendingRows:    7,
		}
		rec := dashboardCall(t, srv, "admin-token", http.MethodGet, "/api/v1/admin/ingest/backlog")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp ingestBacklogResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Status != "down" || resp.AnalyticsStatus != "down" || resp.AnalyticsMode != analyticsModeOLAP || resp.WarehouseStatus != "unconfigured" {
			t.Fatalf("OLAP backlog without warehouse should be down: %+v", resp)
		}
	})
	t.Run("viewer is denied", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
		rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/admin/ingest/backlog")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d", rec.Code)
		}
	})
}

func TestAdminTenantsActivityRoleGate(t *testing.T) {
	t.Run("admin can read tenant activity", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "admin", "admin-token")
		rec := dashboardCall(t, srv, "admin-token", http.MethodGet, "/api/v1/admin/tenants/activity?period=24h")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp tenantsActivityResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Period != "24h" {
			t.Fatalf("period not echoed: %+v", resp)
		}
		if resp.Top == nil {
			t.Fatalf("top should be a non-nil slice")
		}
	})
	t.Run("operator is denied", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "operator", "op-token")
		rec := dashboardCall(t, srv, "op-token", http.MethodGet, "/api/v1/admin/tenants/activity")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d", rec.Code)
		}
	})
}

func TestAdminSLORoleGate(t *testing.T) {
	t.Run("admin sees SLO definitions", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "admin", "admin-token")
		rec := dashboardCall(t, srv, "admin-token", http.MethodGet, "/api/v1/admin/slo")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		var resp sloResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.SLOs) == 0 {
			t.Fatalf("expected at least one SLO definition")
		}
	})
	t.Run("viewer is denied", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
		rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/admin/slo")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d", rec.Code)
		}
	})
}

func TestAdminCapacityRoleGate(t *testing.T) {
	t.Run("admin sees capacity envelope", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "admin", "admin-token")
		rec := dashboardCall(t, srv, "admin-token", http.MethodGet, "/api/v1/admin/capacity")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		var resp capacityResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.PostgresStatus == "" {
			t.Fatalf("expected postgres_status populated")
		}
		if resp.AnalyticsMode == "" || resp.AnalyticsStatus == "" || resp.WarehouseStatus == "" {
			t.Fatalf("expected analytics-neutral capacity status populated: %+v", resp)
		}
		if resp.WarehouseConfigured {
			t.Fatalf("small-mode capacity should not require a configured warehouse: %+v", resp)
		}
	})
	t.Run("viewer is denied", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
		rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/admin/capacity")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d", rec.Code)
		}
	})
}

func TestRiskScoreHistoryRoleGate(t *testing.T) {
	t.Run("viewer can read history", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
		rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/dashboard/metrics/risk-score/history?days=7&tenant_id="+uuid.New().String())
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp riskScoreHistoryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Points) != 7 {
			t.Fatalf("expected 7 points, got %d", len(resp.Points))
		}
	})
	t.Run("unauthenticated request rejected", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/metrics/risk-score/history", nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 got %d", rec.Code)
		}
	})
}

func TestRemediationVelocityHistoryRoleGate(t *testing.T) {
	t.Run("viewer can read history", func(t *testing.T) {
		srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
		rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/dashboard/metrics/remediation-velocity/history?days=14&tenant_id="+uuid.New().String())
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp remediationVelocityHistoryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Points) != 14 {
			t.Fatalf("expected 14 points, got %d", len(resp.Points))
		}
	})
}

func TestComplianceByFrameworkShape(t *testing.T) {
	srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
	rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/dashboard/metrics/compliance/by-framework?tenant_id="+uuid.New().String())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp complianceByFrameworkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Frameworks) == 0 {
		t.Fatalf("expected at least one framework row")
	}
	if resp.Frameworks[0].Name == "" {
		t.Fatalf("expected framework name populated")
	}
}
