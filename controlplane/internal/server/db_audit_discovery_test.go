package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

func TestDBAuditDiscoveryReturnsServiceCandidateAndMissingAccess(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	serviceID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "db-01",
			State:     storage.NodeStateActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
		nodeServices: map[uuid.UUID][]storage.NodeService{
			nodeID: {{
				ID:          serviceID,
				NodeID:      nodeID,
				TenantID:    tenantID,
				Process:     "postgres",
				ListenAddr:  "0.0.0.0",
				Port:        5432,
				ServiceKind: "postgres",
				ObservedAt:  time.Now().UTC(),
			}},
		},
		eventFilters: map[uuid.UUID]storage.TenantEventFilters{
			tenantID: {
				TenantID:           tenantID,
				CaptureDBQueries:   true,
				DBQueryTextCapture: false,
			},
		},
	}
	srv := buildHeartbeatServer(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/db-audit/discovery?tenant_id="+tenantID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:  "user",
		Name:  "viewer",
		Roles: []string{roleViewer},
	}))
	rec := httptest.NewRecorder()
	srv.handleDBAuditDiscovery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp dbAuditDiscoveryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1: %#v", len(resp.Candidates), resp.Candidates)
	}
	got := resp.Candidates[0]
	if got.Engine != "postgres" || got.AccessState != "missing_access" {
		t.Fatalf("unexpected candidate: %#v", got)
	}
	if got.CoverageState != "discoverable_no_access" {
		t.Fatalf("coverage_state = %q, want discoverable_no_access", got.CoverageState)
	}
	if got.AccessRequest == nil || got.AccessRequest.LeastPrivilegeRole != "controlone_audit_reader" {
		t.Fatalf("expected least-privilege access artifact, got %#v", got.AccessRequest)
	}
	if len(got.AccessRequest.GrantStatements) == 0 || !strings.Contains(got.AccessRequest.GrantStatements[0], "CREATE ROLE controlone_audit_reader") {
		t.Fatalf("expected postgres grant artifact, got %#v", got.AccessRequest.GrantStatements)
	}
	if len(got.AccessRequest.ExpectedEvidence) == 0 || got.AccessRequest.RiskNote == "" {
		t.Fatalf("expected evidence/risk note, got %#v", got.AccessRequest)
	}
	if len(got.AccessRequest.SetupNotes) == 0 {
		t.Fatalf("expected setup notes split from grant statements, got %#v", got.AccessRequest)
	}
	assertDBAuditGrantStatementsExecutable(t, got.AccessRequest)
	if !containsString(got.CoverageStatus, "query_capture_enabled") || !containsString(got.CoverageStatus, "target_config_missing") {
		t.Fatalf("coverage status missing truth states: %#v", got.CoverageStatus)
	}
	if len(got.SourceCoverage) == 0 {
		t.Fatalf("expected source coverage matrix rows, got %#v", got.SourceCoverage)
	}
	if !containsDBAuditSource(got.SourceCoverage, "pg_stat_activity") || !containsDBAuditSource(got.SourceCoverage, "pgaudit/postgresql logs") {
		t.Fatalf("expected postgres source coverage rows, got %#v", got.SourceCoverage)
	}
	if len(got.SideChannelEvidence) == 0 || got.SideChannelEvidence[0].Kind != "node_service" {
		t.Fatalf("expected node service side-channel evidence, got %#v", got.SideChannelEvidence)
	}
	if len(got.CitationIDs) == 0 || len(resp.Citations) == 0 {
		t.Fatalf("expected citations, got candidate=%#v citations=%#v", got.CitationIDs, resp.Citations)
	}
	if !containsString(resp.Guardrails, "doris_unavailable") {
		t.Fatalf("expected Doris guardrail when analytic store is absent, got %#v", resp.Guardrails)
	}
}

func TestDBAuditAccessRequestArtifactsCoverMajorEngines(t *testing.T) {
	t.Parallel()

	for _, engine := range []string{"postgres", "mysql", "mssql", "oracle", "db2", "mongodb"} {
		artifact := dbAuditAccessRequestForEngine(engine)
		if artifact == nil {
			t.Fatalf("missing artifact for %s", engine)
		}
		if artifact.Engine != engine || artifact.LeastPrivilegeRole == "" || len(artifact.GrantStatements) == 0 || len(artifact.ExpectedEvidence) == 0 || artifact.RiskNote == "" {
			t.Fatalf("incomplete artifact for %s: %#v", engine, artifact)
		}
		assertDBAuditGrantStatementsExecutable(t, artifact)
	}
}

func TestDBAuditDiscoveryRejectsCrossTenantNodeHTTP(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := buildHeartbeatServer(t, &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/db-audit/discovery?tenant_id="+tenantID.String()+"&node_id="+nodeID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:  "user",
		Name:  "viewer",
		Roles: []string{roleViewer},
	}))
	rec := httptest.NewRecorder()

	srv.handleDBAuditDiscovery(rec, req)

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestDBAuditDiscoveryAIToolRejectsCrossTenantNode(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}}}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "db_audit_discovery", Input: map[string]any{"node_id": nodeID.String()}},
	)
	if err == nil || !strings.Contains(err.Error(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, got %v", err)
	}
}

func containsDBAuditSource(rows []dbAuditSourceCoverage, source string) bool {
	for _, row := range rows {
		if row.Source == source {
			return true
		}
	}
	return false
}

func assertDBAuditGrantStatementsExecutable(t *testing.T, artifact *dbAuditAccessRequestArtifact) {
	t.Helper()
	nonExecutablePhrases := []string{
		"Configure ",
		"Grant read access",
		"Enable ",
		"Use profiler",
		"Provide exported",
		"Create CONTROLONE",
	}
	for _, stmt := range artifact.GrantStatements {
		for _, phrase := range nonExecutablePhrases {
			if strings.Contains(stmt, phrase) {
				t.Fatalf("grant statement for %s contains setup prose %q: %q", artifact.Engine, phrase, stmt)
			}
		}
	}
}
