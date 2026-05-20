package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
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
		t.Fatalf("expected 9 matrix rows got %d", len(resp.Matrix))
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
