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
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestCoverageMatrixHandlerReturnsDeterministicTenantScopedCatalog(t *testing.T) {
	tenantID := uuid.New()
	srv := &Server{}

	req := coverageRequest(http.MethodGet, "/api/v1/coverage/matrix?tenant_id="+tenantID.String())
	rec := httptest.NewRecorder()
	srv.handleCoverageMatrix(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp coverageMatrixResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode matrix response: %v", err)
	}
	if resp.CatalogVersion != coverageCatalogVersion {
		t.Fatalf("unexpected catalog version %q", resp.CatalogVersion)
	}
	if resp.Scope != "tenant" {
		t.Fatalf("expected tenant scope got %q", resp.Scope)
	}
	if resp.TenantID != tenantID.String() {
		t.Fatalf("expected tenant_id %s got %q", tenantID, resp.TenantID)
	}
	if len(resp.Matrix) != 9 {
		t.Fatalf("expected static no-store matrix to stay at 9 rows got %d", len(resp.Matrix))
	}

	expectedDomains := []string{
		"telemetry",
		"parser",
		"detection",
		"compliance",
		"remediation",
		"vulnerability",
		"posture",
		"ai",
		"cases",
	}
	for i, domain := range expectedDomains {
		if resp.Matrix[i].Domain != domain {
			t.Fatalf("expected domain %q at row %d got %q", domain, i, resp.Matrix[i].Domain)
		}
		if len(resp.Matrix[i].Evidence) == 0 {
			t.Fatalf("expected evidence for domain %q", domain)
		}
	}

	requireCoverageState(t, resp.Legend.States, "supported")
	requireCoverageState(t, resp.Legend.States, "partial")
	requireCoverageState(t, resp.Legend.States, "raw_only")
	requireCoverageState(t, resp.Legend.States, "unsupported")
	requireCoverageState(t, resp.Legend.States, "manual_evidence")
	requireCoverageState(t, resp.Legend.States, "stale")
	requireCoverageState(t, resp.Legend.States, "exception")
	requireCoverageState(t, resp.Legend.States, "not_applicable")
	requireCoverageQuality(t, resp.Legend.QualityStates, "fixture_tested")
	requireCoverageQuality(t, resp.Legend.QualityStates, "production_tested")

	rec2 := httptest.NewRecorder()
	srv.handleCoverageMatrix(rec2, req.Clone(req.Context()))
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected second response 200 got %d", rec2.Code)
	}
	if rec.Body.String() != rec2.Body.String() {
		t.Fatalf("expected deterministic response bodies")
	}
}

func TestCoverageMatrixAddsTenantHeartbeatFreshnessOverlay(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()
	fresh := now.Add(-2 * time.Minute)
	stale := now.Add(-20 * time.Minute)
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}},
		nodes: []storage.Node{
			{ID: uuid.New(), TenantID: tenantID, Hostname: "fresh-1", State: storage.NodeStateActive, LastSeenAt: &fresh},
			{ID: uuid.New(), TenantID: tenantID, Hostname: "stale-1", State: storage.NodeStateActive, LastSeenAt: &stale},
			{ID: uuid.New(), TenantID: tenantID, Hostname: "missing-1", State: storage.NodeStateActive},
		},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "coverage-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coverage/matrix?tenant_id="+tenantID.String()+"&domain=telemetry", nil)
	req.Header.Set("Authorization", "Bearer coverage-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp coverageMatrixResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode matrix response: %v", err)
	}
	if resp.Scope != "tenant" || resp.TenantID != tenantID.String() || resp.GeneratedAt == "" {
		t.Fatalf("expected generated tenant-scoped response, got %+v", resp)
	}
	for _, row := range resp.Matrix {
		if row.Domain != "telemetry" {
			t.Fatalf("domain filter leaked row %+v", row)
		}
	}
	row := findCoverageRow(resp.Matrix, "Tenant heartbeat freshness")
	if row == nil {
		t.Fatalf("expected tenant heartbeat row, got %+v", resp.Matrix)
	}
	if row.State != coverageStateStale {
		t.Fatalf("expected stale heartbeat state, got %+v", row)
	}
	if !containsString(row.Signals, "fresh=1") || !containsString(row.Signals, "stale=1") || !containsString(row.Signals, "missing=1") {
		t.Fatalf("expected heartbeat counters in signals, got %+v", row.Signals)
	}
}

func TestCoverageMatrixHeartbeatOverlayFreshAndNotApplicable(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Minute)
	row := tenantHeartbeatCoverageFromNodes([]storage.Node{{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		Hostname:   "fresh-1",
		LastSeenAt: &fresh,
	}}, 1, now)
	if row.State != coverageStateSupported {
		t.Fatalf("expected supported for all fresh nodes, got %+v", row)
	}
	none := tenantHeartbeatCoverageFromNodes(nil, 0, now)
	if none.State != coverageStateNotApplicable {
		t.Fatalf("expected not_applicable for zero nodes, got %+v", none)
	}
}

func TestCoverageMatrixTenantAccessDenied(t *testing.T) {
	tenantID := uuid.New()
	store := &coverageAccessStore{
		fakeStore: &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}},
		allowed:   false,
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "coverage-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coverage/matrix?tenant_id="+tenantID.String(), nil)
	req.Header.Set("Authorization", "Bearer coverage-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected tenant access denial, got %d body=%s", rec.Code, rec.Body.String())
	}
	if store.checkedTenant != tenantID {
		t.Fatalf("tenant gate not called with requested tenant: got %s want %s", store.checkedTenant, tenantID)
	}
}

func TestCoverageExplainHandlerReturnsRationalesWithoutStore(t *testing.T) {
	srv := &Server{}
	req := coverageRequest(http.MethodGet, "/api/v1/coverage/explain")
	rec := httptest.NewRecorder()

	srv.handleCoverageSubroutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp coverageExplainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode explain response: %v", err)
	}
	if resp.Scope != "global" {
		t.Fatalf("expected global scope got %q", resp.Scope)
	}
	if resp.TenantID != "" {
		t.Fatalf("expected tenant_id omitted got %q", resp.TenantID)
	}
	if len(resp.Explanations) != len(resp.Domains) {
		t.Fatalf("expected explanations to match domains, got %d explanations for %d domains", len(resp.Explanations), len(resp.Domains))
	}

	byDomain := map[string]coverageExplanation{}
	for _, exp := range resp.Explanations {
		if exp.Rationale == "" {
			t.Fatalf("expected rationale for domain %q", exp.Domain)
		}
		byDomain[exp.Domain] = exp
	}
	if byDomain["vulnerability"].State != coverageStatePartial {
		t.Fatalf("expected conservative vulnerability state partial got %q", byDomain["vulnerability"].State)
	}
	if byDomain["ai"].State != coverageStateException {
		t.Fatalf("expected AI state exception got %q", byDomain["ai"].State)
	}
	if byDomain["parser"].State != coverageStateRawOnly {
		t.Fatalf("expected parser state raw_only got %q", byDomain["parser"].State)
	}
}

func TestCoverageHandlersValidateMethodAuthAndTenant(t *testing.T) {
	srv := &Server{}

	t.Run("method not allowed", func(t *testing.T) {
		req := coverageRequest(http.MethodPost, "/api/v1/coverage/matrix")
		rec := httptest.NewRecorder()
		srv.handleCoverageMatrix(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 got %d", rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
			t.Fatalf("expected Allow GET got %q", allow)
		}
	})

	t.Run("requires principal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/coverage/matrix", nil)
		rec := httptest.NewRecorder()
		srv.handleCoverageMatrix(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 got %d", rec.Code)
		}
	})

	t.Run("invalid tenant", func(t *testing.T) {
		req := coverageRequest(http.MethodGet, "/api/v1/coverage/explain?tenant_id=not-a-uuid")
		rec := httptest.NewRecorder()
		srv.handleCoverageExplain(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("unknown coverage subroute", func(t *testing.T) {
		req := coverageRequest(http.MethodGet, "/api/v1/coverage/unknown")
		rec := httptest.NewRecorder()
		srv.handleCoverageSubroutes(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d", rec.Code)
		}
	})
}

func TestCoverageExplainAIToolFiltersConservativeStates(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	exec, err := (&Server{}).executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "coverage_explain", Input: map[string]any{"state": "manual_evidence"}},
	)
	if err != nil {
		t.Fatalf("execute coverage tool: %v", err)
	}
	resp, ok := exec.Payload.(coverageExplainResponse)
	if !ok {
		t.Fatalf("unexpected payload type %T", exec.Payload)
	}
	if resp.TenantID != tenantID.String() || resp.Scope != "tenant" {
		t.Fatalf("expected tenant scoped response, got %+v", resp)
	}
	if len(resp.Explanations) == 0 {
		t.Fatalf("expected manual-evidence explanations")
	}
	for _, explanation := range resp.Explanations {
		if explanation.State != coverageStateManualEvidence {
			t.Fatalf("unexpected state in filtered response: %+v", explanation)
		}
	}
	if exec.Citation.Tool != "coverage_explain" {
		t.Fatalf("unexpected citation: %+v", exec.Citation)
	}
}

func coverageRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	principal := &auth.Principal{
		Type:    "user",
		Subject: "coverage-test-viewer",
		Roles:   []string{roleViewer},
	}
	return req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal))
}

type coverageAccessStore struct {
	*fakeStore
	allowed       bool
	checkedTenant uuid.UUID
}

func (s *coverageAccessStore) UserHasTenantRole(_ context.Context, _ uuid.UUID, tenantID uuid.UUID, _ []string) (bool, error) {
	s.checkedTenant = tenantID
	return s.allowed, nil
}

func findCoverageRow(rows []coverageMatrixRow, title string) *coverageMatrixRow {
	for i := range rows {
		if rows[i].Title == title {
			return &rows[i]
		}
	}
	return nil
}

func requireCoverageState(t *testing.T, states []coverageStateDefinition, want string) {
	t.Helper()
	for _, state := range states {
		if state.State == want {
			return
		}
	}
	t.Fatalf("missing state %q in legend", want)
}

func requireCoverageQuality(t *testing.T, states []coverageQualityDefinition, want string) {
	t.Helper()
	for _, state := range states {
		if state.State == want {
			return
		}
	}
	t.Fatalf("missing quality state %q in legend", want)
}
