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
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
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

func TestCoverageMatrixAddsTenantEdgeCollectorOverlay(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Minute)
	store := &coverageEdgeCollectorStore{
		fakeStore: &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}},
		collectors: []storage.ContentPackEdgeCollector{
			{
				ID:                   uuid.New(),
				TenantID:             tenantID,
				CollectorID:          "edge-healthy-1",
				Kind:                 storage.ContentPackEdgeCollectorKindOTel,
				Status:               storage.ContentPackEdgeCollectorStatusHealthy,
				DesiredConfigVersion: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				RunningConfigVersion: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Health:               map[string]any{"exporter_queue_depth": float64(0)},
				LastHeartbeatAt:      &fresh,
				CreatedAt:            now.Add(-30 * time.Minute),
				UpdatedAt:            fresh,
			},
			{
				ID:                   uuid.New(),
				TenantID:             tenantID,
				CollectorID:          "edge-degraded-1",
				Kind:                 storage.ContentPackEdgeCollectorKindOTel,
				Status:               storage.ContentPackEdgeCollectorStatusDegraded,
				DesiredConfigVersion: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				RunningConfigVersion: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				LastError:            "receiver queue backpressure",
				LastHeartbeatAt:      &fresh,
				CreatedAt:            now.Add(-30 * time.Minute),
				UpdatedAt:            fresh,
			},
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
	row := findCoverageRow(resp.Matrix, "Tenant SIEM edge collectors")
	if row == nil {
		t.Fatalf("expected edge collector row, got %+v", resp.Matrix)
	}
	if row.State != coverageStatePartial {
		t.Fatalf("expected partial edge collector state, got %+v", row)
	}
	if !containsString(row.Signals, "healthy=1") || !containsString(row.Signals, "degraded=1") || !containsString(row.Signals, "config_mismatch=1") {
		t.Fatalf("expected edge collector counters in signals, got %+v", row.Signals)
	}
}

func TestCoverageMatrixEdgeCollectorOverlayStates(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Minute)
	supported := tenantEdgeCollectorCoverageFromCollectors([]storage.ContentPackEdgeCollector{{
		ID:                   uuid.New(),
		TenantID:             uuid.New(),
		CollectorID:          "edge-1",
		Status:               storage.ContentPackEdgeCollectorStatusHealthy,
		DesiredConfigVersion: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RunningConfigVersion: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LastHeartbeatAt:      &fresh,
	}}, 1, now)
	if supported.State != coverageStateSupported {
		t.Fatalf("expected supported for healthy fresh collector, got %+v", supported)
	}
	none := tenantEdgeCollectorCoverageFromCollectors(nil, 0, now)
	if none.State != coverageStateRawOnly {
		t.Fatalf("expected raw_only for zero collectors, got %+v", none)
	}
}

func TestCoverageMatrixAddsTenantSourceHealthOverlay(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Minute)
	store := &coverageEdgeCollectorStore{
		fakeStore: &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}},
		collectors: []storage.ContentPackEdgeCollector{{
			ID:                   uuid.New(),
			TenantID:             tenantID,
			CollectorID:          "edge-source-1",
			Kind:                 storage.ContentPackEdgeCollectorKindOTel,
			Status:               storage.ContentPackEdgeCollectorStatusHealthy,
			DesiredConfigVersion: "sha256:source",
			RunningConfigVersion: "sha256:source",
			LastHeartbeatAt:      &fresh,
			Health: map[string]any{
				"sources": map[string]any{
					"fortinet.firewall": map[string]any{
						"coverage_state":  contentpacks.CoverageParserHealthy,
						"events_received": float64(100),
						"events_parsed":   float64(100),
						"last_parsed_at":  fresh.Format(time.RFC3339),
					},
					"paloalto.firewall": map[string]any{
						"status":          contentpacks.CoverageParserFailed,
						"events_received": float64(25),
						"parse_failures":  float64(2),
						"last_error":      "CEF extension parse failed",
					},
				},
				"receivers": map[string]any{
					"syslog/controlone.weblogic.audit": "ok",
				},
			},
			CreatedAt: now.Add(-30 * time.Minute),
			UpdatedAt: fresh,
		}},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "coverage-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coverage/matrix?tenant_id="+tenantID.String()+"&domain=parser", nil)
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
	row := findCoverageRow(resp.Matrix, "Tenant SIEM source health")
	if row == nil {
		t.Fatalf("expected source health row, got %+v", resp.Matrix)
	}
	if row.State != coverageStatePartial {
		t.Fatalf("expected partial source health state, got %+v", row)
	}
	for _, signal := range []string{"source_health_sources_total=3", "parser_healthy=1", "parser_failed=1", "collecting=1", "events_received=125", "parse_failures=2"} {
		if !containsString(row.Signals, signal) {
			t.Fatalf("missing signal %q in %+v", signal, row.Signals)
		}
	}
	if len(row.Gaps) == 0 || !strings.Contains(strings.Join(row.Gaps, " "), "parser failures") {
		t.Fatalf("expected parser failure gap, got %+v", row.Gaps)
	}
}

func TestCoverageMatrixSourceHealthOverlayStates(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Minute)
	supported := tenantSourceHealthCoverageFromCollectors([]storage.ContentPackEdgeCollector{{
		ID:              uuid.New(),
		TenantID:        uuid.New(),
		CollectorID:     "edge-1",
		Status:          storage.ContentPackEdgeCollectorStatusHealthy,
		LastHeartbeatAt: &fresh,
		Health: map[string]any{
			"sources": map[string]any{
				"linux.auth": map[string]any{
					"status":          contentpacks.CoverageParserHealthy,
					"events_received": 10,
					"events_parsed":   10,
					"last_parsed_at":  fresh.Format(time.RFC3339),
				},
			},
		},
	}}, 1, now)
	if supported.State != coverageStateSupported {
		t.Fatalf("expected supported for parser healthy source, got %+v", supported)
	}
	none := tenantSourceHealthCoverageFromCollectors([]storage.ContentPackEdgeCollector{{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		CollectorID: "edge-1",
		Status:      storage.ContentPackEdgeCollectorStatusHealthy,
		Health:      map[string]any{"exporter_queue_depth": 0},
	}}, 1, now)
	if none.State != coverageStateRawOnly {
		t.Fatalf("expected raw_only without source health evidence, got %+v", none)
	}

	proposalRow := tenantSourceHealthCoverageFromRuntimeStates([]storage.ContentPackSourceRuntimeStateRecord{
		{
			ID:       uuid.New(),
			TenantID: uuid.New(),
			State: contentpacks.SourceRuntimeState{
				SourceInstanceID: "node-1/nginx",
				SourceID:         "nginx",
				NodeID:           uuid.New().String(),
				CoverageState:    contentpacks.CoverageState(contentpacks.CoverageApproved),
				LastHealthAt:     &fresh,
			},
			UpdatedAt: fresh,
		},
		{
			ID:       uuid.New(),
			TenantID: uuid.New(),
			State: contentpacks.SourceRuntimeState{
				SourceInstanceID: "node-1/temenos",
				SourceID:         "temenos-t24",
				NodeID:           uuid.New().String(),
				CoverageState:    contentpacks.CoverageState(contentpacks.CoverageApprovalRequired),
				LastHealthAt:     &fresh,
			},
			UpdatedAt: fresh,
		},
	}, 2, now)
	if proposalRow.State != coverageStatePartial {
		t.Fatalf("expected partial for pre-collection proposal runtime states, got %+v", proposalRow)
	}
	for _, signal := range []string{"approved=1", "approval_required=1", "source_runtime_persisted=true"} {
		if !containsString(proposalRow.Signals, signal) {
			t.Fatalf("missing signal %q in %+v", signal, proposalRow.Signals)
		}
	}

	conflictIdentity := "proposal:" + uuid.NewString()
	conflictRow := tenantSourceHealthCoverageFromRuntimeStates([]storage.ContentPackSourceRuntimeStateRecord{
		{
			ID:       uuid.New(),
			TenantID: uuid.New(),
			State: contentpacks.SourceRuntimeState{
				SourceInstanceID: "node-1/linux.auth",
				SourceID:         "linux.auth",
				NodeID:           uuid.New().String(),
				CollectorID:      "node-1",
				CollectorMode:    contentpacks.CollectorNodeFileLog,
				CoverageState:    contentpacks.CoverageState(contentpacks.CoverageCollecting),
				LastHealthAt:     &fresh,
				Labels: map[string]string{
					contentPackCollectionOwnerLabel:    contentPackCollectionOwnerNodeAgent,
					contentPackCollectionIdentityLabel: conflictIdentity,
				},
			},
			UpdatedAt: fresh,
		},
		{
			ID:       uuid.New(),
			TenantID: uuid.New(),
			State: contentpacks.SourceRuntimeState{
				SourceInstanceID: "edge-1/linux.auth",
				SourceID:         "linux.auth",
				CollectorID:      "edge-1",
				CollectorMode:    contentpacks.CollectorSyslog,
				CoverageState:    contentpacks.CoverageState(contentpacks.CoverageConfigRendered),
				LastHealthAt:     &fresh,
				Labels: map[string]string{
					contentPackCollectionOwnerLabel:    contentPackCollectionOwnerOTelEdge,
					contentPackCollectionIdentityLabel: conflictIdentity,
				},
			},
			UpdatedAt: fresh,
		},
	}, 2, now)
	if conflictRow.State != coverageStatePartial {
		t.Fatalf("expected partial for duplicate collection conflict, got %+v", conflictRow)
	}
	if !containsString(conflictRow.Signals, "collection_conflict=2") {
		t.Fatalf("missing collection conflict signal in %+v", conflictRow.Signals)
	}
	if !containsSubstring(conflictRow.Gaps, "duplicate collection owners") {
		t.Fatalf("missing collection conflict gap in %+v", conflictRow.Gaps)
	}
}

func TestCoverageMatrixAddsTenantSourceProposalOverlay(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}},
		sourceProposals: []storage.ContentPackSourceProposalRecord{
			{
				ID:                  uuid.New(),
				TenantID:            tenantID,
				NodeID:              nodeID,
				ProposalID:          "local-log:nginx",
				Kind:                "local_log",
				Program:             "nginx",
				Status:              storage.ContentPackSourceProposalStatusAutoEligible,
				AutoConnectEligible: true,
				LastSeenAt:          now.Add(-1 * time.Minute),
				CreatedAt:           now.Add(-2 * time.Minute),
				UpdatedAt:           now.Add(-1 * time.Minute),
			},
			{
				ID:               uuid.New(),
				TenantID:         tenantID,
				NodeID:           nodeID,
				ProposalID:       "local-log:temenos-t24",
				Kind:             "local_log",
				Program:          "temenos-t24",
				Status:           storage.ContentPackSourceProposalStatusApprovalRequired,
				RequiresApproval: true,
				LastSeenAt:       now.Add(-30 * time.Second),
				CreatedAt:        now.Add(-2 * time.Minute),
				UpdatedAt:        now.Add(-30 * time.Second),
			},
			{
				ID:         uuid.New(),
				TenantID:   tenantID,
				NodeID:     nodeID,
				ProposalID: "local-log:custom-pii",
				Kind:       "local_log",
				Program:    "custom-pii",
				Status:     storage.ContentPackSourceProposalStatusPrivacyBlocked,
				LastSeenAt: now.Add(-20 * time.Second),
				CreatedAt:  now.Add(-2 * time.Minute),
				UpdatedAt:  now.Add(-20 * time.Second),
			},
		},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "coverage-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coverage/matrix?tenant_id="+tenantID.String()+"&domain=parser", nil)
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
	row := findCoverageRow(resp.Matrix, "Tenant SIEM source proposals")
	if row == nil {
		t.Fatalf("expected source proposal row, got %+v", resp.Matrix)
	}
	if row.State != coverageStatePartial {
		t.Fatalf("expected partial proposal state, got %+v", row)
	}
	for _, signal := range []string{"source_proposals_total=3", "auto_eligible=1", "approval_required=1", "privacy_blocked=1"} {
		if !containsString(row.Signals, signal) {
			t.Fatalf("missing signal %q in %+v", signal, row.Signals)
		}
	}
	if len(row.Gaps) == 0 || !strings.Contains(strings.Join(row.Gaps, " "), "explicit approval") || !strings.Contains(strings.Join(row.Gaps, " "), "privacy-blocked") {
		t.Fatalf("expected approval/privacy gaps, got %+v", row.Gaps)
	}
}

func TestCoverageMatrixSourceProposalOverlayUsesDurableSummary(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	proposals := make([]storage.ContentPackSourceProposalRecord, 0, 501)
	for i := 0; i < 501; i++ {
		proposals = append(proposals, storage.ContentPackSourceProposalRecord{
			ID:         uuid.New(),
			TenantID:   tenantID,
			NodeID:     nodeID,
			ProposalID: "local-log:" + uuid.NewString(),
			Kind:       "local_log",
			Program:    "approved",
			Status:     storage.ContentPackSourceProposalStatusApproved,
			LastSeenAt: now,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	store := &fakeStore{
		tenants:         []storage.Tenant{{ID: tenantID, Name: "Acme Security"}},
		sourceProposals: proposals,
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "coverage-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coverage/matrix?tenant_id="+tenantID.String()+"&domain=parser", nil)
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
	row := findCoverageRow(resp.Matrix, "Tenant SIEM source proposals")
	if row == nil {
		t.Fatalf("expected source proposal row, got %+v", resp.Matrix)
	}
	for _, signal := range []string{"source_proposals_summary=true", "source_proposals_total=501", "approved=501"} {
		if !containsString(row.Signals, signal) {
			t.Fatalf("missing signal %q in %+v", signal, row.Signals)
		}
	}
	if containsString(row.Signals, "source_proposal_count_truncated=true") || strings.Contains(strings.Join(row.Gaps, " "), "sampled") {
		t.Fatalf("summary-backed proposal row should not be sampled: %+v", row)
	}
}

func TestCoverageMatrixSourceHealthOverlayUsesRuntimeRowsForConflictDetection(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()
	sourceStates := make([]storage.ContentPackSourceRuntimeStateRecord, 0, 501)
	for i := 0; i < 501; i++ {
		sourceID := "linux.auth." + uuid.NewString()
		sourceStates = append(sourceStates, storage.ContentPackSourceRuntimeStateRecord{
			ID:       uuid.New(),
			TenantID: tenantID,
			State: contentpacks.SourceRuntimeState{
				SourceInstanceID: "edge-summary/" + sourceID,
				SourceID:         sourceID,
				CollectorID:      "edge-summary",
				CoverageState:    contentpacks.CoverageState(contentpacks.CoverageParserHealthy),
				LastHealthAt:     &now,
				Metrics: contentpacks.SourceRuntimeMetrics{
					EventsReceived: 1,
					EventsParsed:   1,
				},
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	store := &contentPackSnapshotFakeStore{
		fakeStore:    &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}},
		sourceStates: sourceStates,
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "coverage-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coverage/matrix?tenant_id="+tenantID.String()+"&domain=parser", nil)
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
	row := findCoverageRow(resp.Matrix, "Tenant SIEM source health")
	if row == nil {
		t.Fatalf("expected source health row, got %+v", resp.Matrix)
	}
	for _, signal := range []string{"source_runtime_persisted=true", "source_health_scope_total=501", "source_health_scope_sampled=500", "parser_healthy=500", "events_received=500", "source_runtime_count_truncated=true"} {
		if !containsString(row.Signals, signal) {
			t.Fatalf("missing signal %q in %+v", signal, row.Signals)
		}
	}
	if containsString(row.Signals, "source_runtime_summary=true") || !strings.Contains(strings.Join(row.Gaps, " "), "sampled") {
		t.Fatalf("source health overlay should prefer runtime rows for duplicate-owner detection: %+v", row)
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

type coverageEdgeCollectorStore struct {
	*fakeStore
	collectors []storage.ContentPackEdgeCollector
}

func (s *coverageEdgeCollectorStore) ListContentPackEdgeCollectors(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.ContentPackEdgeCollector, int, error) {
	var filtered []storage.ContentPackEdgeCollector
	for _, collector := range s.collectors {
		if collector.TenantID == tenantID {
			filtered = append(filtered, collector)
		}
	}
	total := len(filtered)
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return append([]storage.ContentPackEdgeCollector(nil), filtered[offset:end]...), total, nil
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

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
