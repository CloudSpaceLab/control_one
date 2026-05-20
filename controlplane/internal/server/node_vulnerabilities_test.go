package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestHandleNodeVulnerabilitiesReturnsCVEPackageEvidence(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 19, 14, 0, 0, 0, time.UTC)
	cvss := 9.8
	epss := 0.921
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "tn", CreatedAt: now}},
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "host",
			State:     storage.NodeStateActive,
			CreatedAt: now,
			UpdatedAt: now,
		}},
	}
	if err := store.UpsertVulnerabilityFindings(context.Background(), []storage.VulnerabilityFinding{{
		ID:               uuid.New(),
		TenantID:         tenantID,
		NodeID:           nodeID,
		PackageName:      "openssl",
		InstalledVersion: "3.0.2-0ubuntu1",
		PackageSource:    "apt",
		Arch:             "amd64",
		CVEID:            "CVE-2026-0001",
		Severity:         "critical",
		CVSSScore:        &cvss,
		EPSSScore:        &epss,
		KEV:              true,
		FixedVersion:     "3.0.2-0ubuntu1.15",
		AdvisoryURL:      "https://vendor.example/advisories/CVE-2026-0001",
		EvidenceSource:   "vendor",
		References:       []string{"https://nvd.nist.gov/vuln/detail/CVE-2026-0001"},
		Evidence: map[string]any{
			"feed_bundle": "ubuntu-security-2026-05-19",
			"matcher":     "package-name-version-range",
		},
		FirstSeenAt: now.Add(-time.Hour),
		LastSeenAt:  now,
	}}); err != nil {
		t.Fatalf("seed vulnerability findings: %v", err)
	}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/vulnerabilities?kev_only=true", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var body struct {
		Data       []nodeVulnerabilityResponse `json:"data"`
		Summary    nodeVulnerabilitySummary    `json:"summary"`
		Guardrails []string                    `json:"guardrails"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("rows = %d, want 1 (%s)", len(body.Data), rec.Body.String())
	}
	row := body.Data[0]
	if row.CVEID != "CVE-2026-0001" || row.PackageName != "openssl" || row.FixedVersion != "3.0.2-0ubuntu1.15" {
		t.Fatalf("unexpected vulnerability row: %+v", row)
	}
	if row.CVSSScore == nil || *row.CVSSScore != cvss || row.EPSSScore == nil || *row.EPSSScore != epss || !row.KEV {
		t.Fatalf("missing exploitability evidence: %+v", row)
	}
	if row.CitationID == "" || row.Evidence["feed_bundle"] != "ubuntu-security-2026-05-19" {
		t.Fatalf("missing citation/evidence: %+v", row)
	}
	if body.Summary.Total != 1 || body.Summary.KEV != 1 || body.Summary.WithFix != 1 || body.Summary.BySeverity["critical"] != 1 {
		t.Fatalf("unexpected summary: %+v", body.Summary)
	}
	if len(body.Guardrails) == 0 {
		t.Fatalf("expected guardrails")
	}
}

func TestHandleNodeVulnerabilitiesUnknownNode404(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+uuid.New().String()+"/vulnerabilities", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestNodeVulnerabilitiesAIToolReturnsCitedFixedVersionEvidence(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 19, 15, 0, 0, 0, time.UTC)
	cvss := 8.8
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "db-01",
			State:     storage.NodeStateActive,
			CreatedAt: now,
			UpdatedAt: now,
		}},
	}
	if err := store.UpsertVulnerabilityFindings(context.Background(), []storage.VulnerabilityFinding{{
		ID:               uuid.New(),
		TenantID:         tenantID,
		NodeID:           nodeID,
		PackageName:      "postgresql",
		InstalledVersion: "15.6-1",
		PackageSource:    "apt",
		CVEID:            "CVE-2026-0100",
		Severity:         "high",
		CVSSScore:        &cvss,
		FixedVersion:     "15.6-1ubuntu0.2",
		EvidenceSource:   "signed-feed-bundle",
		References:       []string{"https://nvd.nist.gov/vuln/detail/CVE-2026-0100"},
		Evidence: map[string]any{
			"bundle_id":   "enterprise-offline-2026-05-19",
			"import_mode": "airgapped",
		},
		FirstSeenAt: now.Add(-time.Hour),
		LastSeenAt:  now,
	}}); err != nil {
		t.Fatalf("seed vulnerability findings: %v", err)
	}
	srv := &Server{store: store}

	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "node_vulnerabilities", Input: map[string]any{
			"node_id":  nodeID.String(),
			"package":  "postgresql",
			"severity": "high",
			"limit":    10,
		}},
	)
	if err != nil {
		t.Fatalf("execute tool: %v", err)
	}
	if exec.Citation.Tool != "node_vulnerabilities" || exec.Citation.Detail != "1 findings" {
		t.Fatalf("unexpected citation: %+v", exec.Citation)
	}
	raw, err := json.Marshal(exec.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var body struct {
		Data       []nodeVulnerabilityResponse `json:"data"`
		Summary    nodeVulnerabilitySummary    `json:"summary"`
		Guardrails []string                    `json:"guardrails"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("rows = %d, want 1 (%s)", len(body.Data), string(raw))
	}
	row := body.Data[0]
	if row.CVEID != "CVE-2026-0100" || row.PackageName != "postgresql" || row.FixedVersion != "15.6-1ubuntu0.2" {
		t.Fatalf("unexpected row: %+v", row)
	}
	if row.CitationID == "" || row.EvidenceSource != "signed-feed-bundle" || row.Evidence["import_mode"] != "airgapped" {
		t.Fatalf("missing citation/feed evidence: %+v", row)
	}
	if body.Summary.Total != 1 || body.Summary.WithFix != 1 || body.Summary.BySeverity["high"] != 1 {
		t.Fatalf("unexpected summary: %+v", body.Summary)
	}
	joinedGuardrails := strings.Join(body.Guardrails, ",")
	if !strings.Contains(joinedGuardrails, "source_row_citations") || !strings.Contains(joinedGuardrails, "no_patch_execution") {
		t.Fatalf("missing AI guardrails: %v", body.Guardrails)
	}
}

func TestNodeVulnerabilitiesAIToolRejectsCrossTenantNode(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}}}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "node_vulnerabilities", Input: map[string]any{"node_id": nodeID.String()}},
	)
	if err == nil || !strings.Contains(err.Error(), "node is outside requested tenant") {
		t.Fatalf("expected tenant isolation error, got %v", err)
	}
}

func TestNodeVulnerabilityPatchPlanIsProposalOnlyAndCited(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 19, 16, 0, 0, 0, time.UTC)
	cvss := 9.1
	epss := 0.73
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "app-01",
			State:     storage.NodeStateActive,
			CreatedAt: now,
			UpdatedAt: now,
		}},
	}
	if err := store.UpsertVulnerabilityFindings(context.Background(), []storage.VulnerabilityFinding{
		{
			ID:               uuid.New(),
			TenantID:         tenantID,
			NodeID:           nodeID,
			PackageName:      "openssl",
			InstalledVersion: "3.0.2-0ubuntu1.14",
			PackageSource:    "apt",
			Arch:             "amd64",
			CVEID:            "CVE-2026-0002",
			Severity:         "critical",
			CVSSScore:        &cvss,
			EPSSScore:        &epss,
			KEV:              true,
			FixedVersion:     "3.0.2-0ubuntu1.15",
			EvidenceSource:   "offline_vulnerability_feed",
			FirstSeenAt:      now.Add(-2 * time.Hour),
			LastSeenAt:       now,
		},
		{
			ID:               uuid.New(),
			TenantID:         tenantID,
			NodeID:           nodeID,
			PackageName:      "openssl",
			InstalledVersion: "3.0.2-0ubuntu1.14",
			PackageSource:    "apt",
			Arch:             "amd64",
			CVEID:            "CVE-2026-0003",
			Severity:         "high",
			FixedVersion:     "3.0.2-0ubuntu1.15",
			EvidenceSource:   "offline_vulnerability_feed",
			FirstSeenAt:      now.Add(-2 * time.Hour),
			LastSeenAt:       now,
		},
		{
			ID:               uuid.New(),
			TenantID:         tenantID,
			NodeID:           nodeID,
			PackageName:      "custom-agent",
			InstalledVersion: "1.2.3",
			PackageSource:    "manual",
			CVEID:            "CVE-2026-0004",
			Severity:         "medium",
			EvidenceSource:   "vendor",
			FirstSeenAt:      now.Add(-time.Hour),
			LastSeenAt:       now,
		},
	}); err != nil {
		t.Fatalf("seed vulnerability findings: %v", err)
	}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/vulnerabilities/patch-plan?mode=airgapped", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var body vulnerabilityPatchPlanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Mode != patchModeAirgapped || !strings.Contains(strings.Join(body.Guardrails, ","), "no_agent_actions_queued") {
		t.Fatalf("expected proposal-only airgapped plan, got %+v", body)
	}
	if body.Summary.ActiveFindings != 3 || body.Summary.Packages != 2 || body.Summary.KEV != 1 || body.Summary.HighestSeverity != "critical" {
		t.Fatalf("unexpected summary: %+v", body.Summary)
	}
	if len(body.Recommendations) != 2 {
		t.Fatalf("recommendations = %#v, want two", body.Recommendations)
	}
	first := body.Recommendations[0]
	if first.PackageName != "openssl" || first.FixedVersion != "3.0.2-0ubuntu1.15" || !first.KEV {
		t.Fatalf("expected openssl KEV recommendation first, got %+v", first)
	}
	if len(first.CVEIDs) != 2 || len(first.CitationIDs) != 2 || !first.RequiresApproval || !first.AIProposalOnly {
		t.Fatalf("missing grouped CVEs/citations/proposal gates: %+v", first)
	}
	if len(first.ExecutionPaths) != 1 || first.ExecutionPaths[0] != patchModeAirgapped {
		t.Fatalf("expected airgapped execution path, got %+v", first.ExecutionPaths)
	}
	second := body.Recommendations[1]
	if second.BlockingReason == "" || !strings.Contains(second.BlockingReason, "missing fixed_version") {
		t.Fatalf("expected missing fixed-version blocker, got %+v", second)
	}
	if !strings.Contains(strings.Join(body.VerificationSteps, ","), "staged offline repository") {
		t.Fatalf("expected airgapped verification steps, got %v", body.VerificationSteps)
	}
}

func TestVulnerabilityPatchPlanAIToolRequiresOperatorAndReturnsPlan(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 19, 17, 0, 0, 0, time.UTC)
	store := &fakeStore{
		nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "db-02", State: storage.NodeStateActive}},
	}
	if err := store.UpsertVulnerabilityFindings(context.Background(), []storage.VulnerabilityFinding{{
		ID:               uuid.New(),
		TenantID:         tenantID,
		NodeID:           nodeID,
		PackageName:      "mysql-server",
		InstalledVersion: "8.0.35",
		PackageSource:    "apt",
		CVEID:            "CVE-2026-0200",
		Severity:         "high",
		FixedVersion:     "8.0.36",
		EvidenceSource:   "offline_vulnerability_feed",
		FirstSeenAt:      now.Add(-time.Hour),
		LastSeenAt:       now,
	}}); err != nil {
		t.Fatalf("seed vulnerability finding: %v", err)
	}
	srv := &Server{store: store}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "vulnerability_patch_plan", Input: map[string]any{"node_id": nodeID.String()}},
	)
	if err == nil || !strings.Contains(err.Error(), "requires role operator") {
		t.Fatalf("expected operator role error, got %v", err)
	}

	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "operator", Roles: []string{roleOperator}},
		tenantID,
		llm.ToolCall{Name: "vulnerability_patch_plan", Input: map[string]any{"node_id": nodeID.String(), "mode": "proxy"}},
	)
	if err != nil {
		t.Fatalf("execute patch plan tool: %v", err)
	}
	if exec.Citation.Tool != "vulnerability_patch_plan" || exec.Citation.Detail != "1 recommendations" {
		t.Fatalf("unexpected citation: %+v", exec.Citation)
	}
	resp, ok := exec.Payload.(vulnerabilityPatchPlanResponse)
	if !ok {
		t.Fatalf("unexpected payload type %T", exec.Payload)
	}
	if resp.Mode != patchModeProxy || len(resp.Recommendations) != 1 {
		t.Fatalf("unexpected patch plan: %+v", resp)
	}
	item := resp.Recommendations[0]
	if !item.AIProposalOnly || !item.RequiresApproval || item.ExecutionPaths[0] != patchModeProxy || len(item.CitationIDs) != 1 {
		t.Fatalf("missing proposal-only gates/citations: %+v", item)
	}
}
