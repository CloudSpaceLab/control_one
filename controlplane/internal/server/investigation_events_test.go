package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/smallanalytics"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestInvestigationScopeRequiresTenantAndClampsLimit(t *testing.T) {
	t.Parallel()

	_, _, err := investigationScopeFromRequest(nil, "", "", "", 0, 0, 100)
	if err == nil {
		t.Fatal("expected missing tenant error")
	}

	tenantID := uuid.New()
	since := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)
	scope, guardrails, err := investigationScopeFromRequest(
		nil,
		tenantID.String(),
		since.Format(time.RFC3339),
		until.Format(time.RFC3339),
		999,
		7,
		100,
	)
	if err != nil {
		t.Fatalf("scope parse failed: %v", err)
	}
	if scope.TenantID != tenantID || !scope.Since.Equal(since) || !scope.Until.Equal(until) {
		t.Fatalf("unexpected scope: %+v", scope)
	}
	if scope.Limit != maxListLimit || scope.Offset != 7 {
		t.Fatalf("unexpected limit/offset: %+v", scope)
	}
	if !containsString(guardrails, "limit clamped to 500") {
		t.Fatalf("expected limit guardrail, got %+v", guardrails)
	}
}

func TestEventsQueryHandlerValidatesAuthAndSmallAnalyticsPending(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	rec := httptest.NewRecorder()
	srv.handleEventsQuery(rec, httptest.NewRequest(http.MethodGet, "/api/v1/events/query", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d", rec.Code)
	}

	tenantID := uuid.New()
	body := bytes.NewReader([]byte(`{"tenant_id":"` + tenantID.String() + `"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events/query", body)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec = httptest.NewRecorder()
	srv.handleEventsQuery(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected pending 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var pendingResp eventsQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pendingResp); err != nil {
		t.Fatalf("decode pending response: %v", err)
	}
	if pendingResp.Source != "small-analytics-pending" || len(pendingResp.Data) != 0 || len(pendingResp.Guardrails) == 0 {
		t.Fatalf("unexpected pending response: %+v", pendingResp)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/events/query", bytes.NewReader([]byte(`{}`)))
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec = httptest.NewRecorder()
	srv.handleEventsQuery(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing tenant 400 got %d", rec.Code)
	}
}

func TestEventsQueryHandlerKeepsOLAPUnavailableLoud(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	srv := &Server{cfg: &config.Config{Analytics: config.AnalyticsConfig{Mode: "olap"}}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events/query", bytes.NewReader([]byte(`{"tenant_id":"`+tenantID.String()+`"}`)))
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()

	srv.handleEventsQuery(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected OLAP unavailable 503 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTimelineBuildRequiresPivot(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timelines/build", bytes.NewReader([]byte(`{"tenant_id":"`+tenantID.String()+`"}`)))
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()

	(&Server{}).handleTimelineBuild(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTimelineBuildHandlerSmallAnalyticsPending(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timelines/build", bytes.NewReader([]byte(`{
		"tenant_id":"`+tenantID.String()+`",
		"entity_type":"ip",
		"entity_id":"8.8.8.8"
	}`)))
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()

	(&Server{}).handleTimelineBuild(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected pending 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp timelineBuildResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode pending timeline: %v", err)
	}
	if resp.Source != "small-analytics-pending" || len(resp.Items) != 0 || len(resp.Guardrails) == 0 {
		t.Fatalf("unexpected pending timeline: %+v", resp)
	}
}

func TestEventsAndTimelineHandlersUseSmallAnalyticsSQLite(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	store, err := smallanalytics.Open(context.Background(), smallanalytics.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open small analytics: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.AppendConnectionRows(context.Background(), []map[string]any{
		smallAnalyticsConnRow(tenantID, nodeID, "conn-1", base, base.Add(2*time.Minute), "outbound", "10.0.0.5", "8.8.8.8", 100, 250, "abuseipdb"),
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	srv := &Server{
		cfg:            &config.Config{Analytics: config.AnalyticsConfig{Mode: "small"}},
		localAnalytics: store,
	}

	eventBody := bytes.NewReader([]byte(`{
		"tenant_id":"` + tenantID.String() + `",
		"event_types":["conn.open","conn.close"],
		"since":"` + base.Add(-time.Minute).Format(time.RFC3339) + `",
		"until":"` + base.Add(3*time.Minute).Format(time.RFC3339) + `",
		"limit":10
	}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events/query", eventBody)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()
	srv.handleEventsQuery(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("events status=%d body=%s", rec.Code, rec.Body.String())
	}
	var eventsResp eventsQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &eventsResp); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if eventsResp.Source != "small-analytics" || eventsResp.Pagination.Total != 2 || len(eventsResp.Data) != 2 || len(eventsResp.Citations) != 2 {
		t.Fatalf("unexpected events response: %+v", eventsResp)
	}
	if eventsResp.Data[0].EventType != "conn.close" || eventsResp.Data[0].CitationIDs[0] != eventsResp.Citations[0].ID {
		t.Fatalf("events should be cited newest-first: %+v citations=%+v", eventsResp.Data, eventsResp.Citations)
	}

	timelineBody := bytes.NewReader([]byte(`{
		"tenant_id":"` + tenantID.String() + `",
		"entity_type":"ip",
		"entity_id":"8.8.8.8",
		"since":"` + base.Add(-time.Minute).Format(time.RFC3339) + `",
		"until":"` + base.Add(3*time.Minute).Format(time.RFC3339) + `",
		"limit":10
	}`))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/timelines/build", timelineBody)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec = httptest.NewRecorder()
	srv.handleTimelineBuild(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status=%d body=%s", rec.Code, rec.Body.String())
	}
	var timelineResp timelineBuildResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &timelineResp); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	if timelineResp.Source != "small-analytics" || len(timelineResp.Items) != 2 || len(timelineResp.Citations) != 2 {
		t.Fatalf("unexpected timeline response: %+v", timelineResp)
	}
	if timelineResp.Items[0].SourceTable != "process_connections" || timelineResp.Items[0].EventType != "conn.close" {
		t.Fatalf("timeline should use cited process connection facts: %+v", timelineResp.Items)
	}
}

func TestEventAndTimelineRowsExposeStableCitations(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)
	item, citation := eventRowToResponse(doris.EventRow{
		SchemaVersion: 2,
		EventID:       "evt-1",
		RawRef:        "journal://batch/1/row/1",
		TenantID:      uuid.New().String(),
		TS:            ts,
		EventType:     "web.request",
		DetailsJSON:   `{"status_code":403}`,
	})
	if item.SourceRecordID != "evt-1" || len(item.CitationIDs) != 1 || item.CitationIDs[0] != citation.ID {
		t.Fatalf("event citation mismatch item=%+v citation=%+v", item, citation)
	}
	if citation.Table != "events" || citation.EventID != "evt-1" || citation.RawRef == "" {
		t.Fatalf("unexpected event citation: %+v", citation)
	}
	if !json.Valid(item.Details) {
		t.Fatalf("expected valid details json, got %s", string(item.Details))
	}

	timelineItem, timelineCitation := timelineRowToResponse(doris.TimelineItem{
		SourceTable: "file_accesses",
		TenantID:    uuid.New().String(),
		TS:          ts,
		EventType:   "file.open",
		Path:        "/etc/passwd",
	})
	if timelineItem.SourceRecordID == "" || timelineCitation.Table != "file_accesses" {
		t.Fatalf("timeline citation mismatch item=%+v citation=%+v", timelineItem, timelineCitation)
	}
}

func TestInvestigationDBQueryTextRedaction(t *testing.T) {
	t.Parallel()

	eventItems := []eventQueryItem{{
		EventType: "db.query",
		Message:   "select * from customers where ssn = '123-45-6789'",
		Details:   json.RawMessage(`{"query_text":"select secret"}`),
	}}
	if redacted := redactDBQueryTextItems(eventItems); redacted != 1 {
		t.Fatalf("redacted events = %d, want 1", redacted)
	}
	if strings.Contains(eventItems[0].Message, "customers") || strings.Contains(string(eventItems[0].Details), "select secret") {
		t.Fatalf("event query text was not redacted: %+v details=%s", eventItems[0], string(eventItems[0].Details))
	}

	timelineItems := []timelineItemResponse{{
		SourceTable: "db_queries",
		EventType:   "sql.query",
		Message:     "update payroll set salary = 1",
		Details:     json.RawMessage(`{"binds":["secret"]}`),
	}}
	if redacted := redactTimelineDBQueryTextItems(timelineItems); redacted != 1 {
		t.Fatalf("redacted timeline items = %d, want 1", redacted)
	}
	if strings.Contains(timelineItems[0].Message, "payroll") || strings.Contains(string(timelineItems[0].Details), "secret") {
		t.Fatalf("timeline query text was not redacted: %+v details=%s", timelineItems[0], string(timelineItems[0].Details))
	}
}

func TestAIInvestigationToolsExposeEventAndTimelineTools(t *testing.T) {
	t.Parallel()

	tools := (&Server{}).aiInvestigationTools()
	expectedRoles := map[string]string{
		"events_query":              roleViewer,
		"doris_ingest_health":       roleAdmin,
		"timeline_build":            roleViewer,
		"entity_lifecycle":          roleViewer,
		"node_vulnerabilities":      roleViewer,
		"vulnerability_patch_plan":  roleOperator,
		"db_audit_discovery":        roleViewer,
		"risk_notables":             roleViewer,
		"compliance_evidence_query": roleViewer,
		"coverage_explain":          roleViewer,
		"posture_drift_explain":     roleViewer,
		"incident_create":           roleOperator,
		"hunt_save":                 roleOperator,
		"case_note_add":             roleInvestigator,
	}
	for name, wantRole := range expectedRoles {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if tool.MinRole != wantRole {
			t.Fatalf("tool %s min role = %s, want %s", name, tool.MinRole, wantRole)
		}
		props, _ := tool.Schema["properties"].(map[string]any)
		if _, ok := props["tenant_id"]; ok {
			t.Fatalf("tool %s must not let the model provide tenant_id", name)
		}
	}
}

func TestDorisIngestHealthAIToolReturnsTenantScopedEvidence(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	batchID := uuid.New()
	receivedAt := time.Date(2026, 5, 19, 12, 30, 0, 0, time.UTC)
	lastErr := receivedAt.Add(2 * time.Minute)
	store := &fakeStore{
		eventIngestBacklog: storage.EventIngestBacklogSummary{
			PendingBatches:   1,
			PendingRows:      7,
			DueBatches:       1,
			RetryingBatches:  1,
			MaxRetryCount:    3,
			LastErrorAt:      sql.NullTime{Time: lastErr, Valid: true},
			LastErrorMessage: sql.NullString{String: "stream load timeout", Valid: true},
		},
		eventIngestBatches: []storage.EventIngestBatch{
			{
				ID:           batchID,
				TenantID:     uuid.NullUUID{UUID: tenantID, Valid: true},
				ReceivedAt:   receivedAt,
				Rows:         7,
				SizeBytes:    2048,
				Status:       "pending_doris",
				DorisStatus:  sql.NullString{String: "pending", Valid: true},
				RetryCount:   3,
				LastErrorAt:  sql.NullTime{Time: lastErr, Valid: true},
				ErrorMessage: sql.NullString{String: "stream load timeout", Valid: true},
			},
			{
				ID:       uuid.New(),
				TenantID: uuid.NullUUID{UUID: otherTenantID, Valid: true},
				Status:   "pending_doris",
				Rows:     99,
			},
		},
	}
	srv := &Server{store: store}
	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "admin", Roles: []string{roleAdmin}},
		tenantID,
		llm.ToolCall{Name: "doris_ingest_health", Input: map[string]any{"limit": 5}},
	)
	if err != nil {
		t.Fatalf("execute doris_ingest_health: %v", err)
	}
	resp, ok := exec.Payload.(dorisIngestHealthToolResponse)
	if !ok {
		t.Fatalf("payload type = %T", exec.Payload)
	}
	if exec.Citation.Tool != "doris_ingest_health" || !strings.Contains(exec.Citation.Detail, "1 pending") {
		t.Fatalf("unexpected citation: %+v", exec.Citation)
	}
	if resp.TenantID != tenantID.String() || resp.Status != "down" || resp.PendingRows != 7 {
		t.Fatalf("unexpected response summary: %+v", resp)
	}
	if len(resp.Evidence) != 1 || resp.Evidence[0].BatchID != batchID.String() {
		t.Fatalf("expected one tenant-scoped evidence row, got %+v", resp.Evidence)
	}
	if len(resp.Citations) != 1 || resp.Citations[0].SourceRecordID != "event_ingest_batches:"+batchID.String() {
		t.Fatalf("expected event_ingest_batches citation, got %+v", resp.Citations)
	}
	if !containsString(resp.Guardrails, "admin-gated because Doris writer status is operational platform health") {
		t.Fatalf("expected admin guardrail, got %+v", resp.Guardrails)
	}
}

func TestDorisIngestHealthAIToolRequiresAdmin(t *testing.T) {
	t.Parallel()

	_, err := (&Server{store: &fakeStore{}}).executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "investigator", Roles: []string{roleInvestigator}},
		uuid.New(),
		llm.ToolCall{Name: "doris_ingest_health", Input: map[string]any{"limit": 5}},
	)
	if err == nil || !strings.Contains(err.Error(), "requires role admin") {
		t.Fatalf("expected admin role error, got %v", err)
	}
}

func TestTimelineBuildAIToolRequiresPivotBeforeDoris(t *testing.T) {
	t.Parallel()

	_, err := (&Server{}).executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		uuid.New(),
		llm.ToolCall{Name: "timeline_build", Input: map[string]any{}},
	)
	if err == nil || !strings.Contains(err.Error(), "timeline requires") {
		t.Fatalf("expected timeline pivot error, got %v", err)
	}
}

func TestEntityLifecycleAIToolRequiresEntityBeforeBackend(t *testing.T) {
	t.Parallel()

	_, err := (&Server{}).executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		uuid.New(),
		llm.ToolCall{Name: "entity_lifecycle", Input: map[string]any{}},
	)
	if err == nil || !strings.Contains(err.Error(), "entity_type and entity_id") {
		t.Fatalf("expected entity requirement error, got %v", err)
	}
}

func TestEntityLifecycleAIToolRejectsCrossTenantNodeBeforeBackend(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}}}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "entity_lifecycle", Input: map[string]any{
			"entity_type": "node",
			"entity_id":   nodeID.String(),
		}},
	)
	if err == nil || !strings.Contains(err.Error(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, got %v", err)
	}
}

func TestEntityLifecycleAIToolReturnsCitedRows(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	observed := time.Date(2026, 5, 19, 10, 15, 0, 0, time.UTC)
	store := &lifecycleAIToolStore{
		items: []storage.LifecycleItem{{
			Timestamp: observed,
			Source:    "alert",
			Severity:  "high",
			Summary:   "credential attack notable opened",
			RawID:     "alert-1",
			Metadata:  map[string]any{"rule_id": "credential_attack"},
		}},
	}
	srv := &Server{store: store}

	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "entity_lifecycle", Input: map[string]any{
			"entity_type": "ip",
			"entity_id":   "203.0.113.20",
			"since":       observed.Add(-time.Hour).Format(time.RFC3339),
			"until":       observed.Add(time.Hour).Format(time.RFC3339),
			"limit":       5,
		}},
	)
	if err != nil {
		t.Fatalf("execute entity_lifecycle: %v", err)
	}
	resp, ok := exec.Payload.(entityLifecycleToolResponse)
	if !ok {
		t.Fatalf("payload type = %T", exec.Payload)
	}
	if exec.Citation.Tool != "entity_lifecycle" || exec.Citation.Detail != "1 rows" {
		t.Fatalf("unexpected citation: %+v", exec.Citation)
	}
	if store.lastFilter.TenantID != tenantID || store.lastFilter.EntityType != "ip" || store.lastFilter.EntityID != "203.0.113.20" {
		t.Fatalf("unexpected lifecycle filter: %+v", store.lastFilter)
	}
	if store.lastLimit != 5 {
		t.Fatalf("limit = %d, want 5", store.lastLimit)
	}
	if len(resp.Items) != 1 || len(resp.Citations) != 1 {
		t.Fatalf("expected one item and citation, got %+v", resp)
	}
	if len(resp.Items[0].CitationIDs) != 1 || resp.Items[0].CitationIDs[0] != resp.Citations[0].ID {
		t.Fatalf("item citation mismatch item=%+v citations=%+v", resp.Items[0], resp.Citations)
	}
	if resp.Citations[0].SourceRecordID != "alert-1" || resp.Citations[0].Source != "alert" {
		t.Fatalf("unexpected citation row ref: %+v", resp.Citations[0])
	}
}

func TestEventsQueryAIToolRejectsCrossTenantNodeBeforeDoris(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}}}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "events_query", Input: map[string]any{"node_id": nodeID.String()}},
	)
	if err == nil || !strings.Contains(err.Error(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, got %v", err)
	}
}

func TestEventsAndTimelineAIToolsUseSmallAnalyticsSQLite(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	store, err := smallanalytics.Open(context.Background(), smallanalytics.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open small analytics: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.AppendConnectionRows(context.Background(), []map[string]any{
		smallAnalyticsConnRow(tenantID, nodeID, "conn-ai-1", base, base.Add(time.Minute), "outbound", "10.0.0.5", "8.8.4.4", 20, 40, ""),
	}); err != nil {
		t.Fatalf("append rows: %v", err)
	}
	srv := &Server{
		cfg:            &config.Config{Analytics: config.AnalyticsConfig{Mode: "small"}},
		localAnalytics: store,
	}
	principal := &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}}

	exec, err := srv.executeAITool(context.Background(), principal, tenantID, llm.ToolCall{
		Name: "events_query",
		Input: map[string]any{
			"event_types": []any{"conn.open", "conn.close"},
			"since":       base.Add(-time.Minute).Format(time.RFC3339),
			"until":       base.Add(2 * time.Minute).Format(time.RFC3339),
			"limit":       10,
		},
	})
	if err != nil {
		t.Fatalf("execute events_query: %v", err)
	}
	eventsResp, ok := exec.Payload.(eventsQueryResponse)
	if !ok {
		t.Fatalf("events payload type = %T", exec.Payload)
	}
	if eventsResp.Source != "small-analytics" || len(eventsResp.Data) != 2 {
		t.Fatalf("unexpected events tool response: %+v", eventsResp)
	}

	exec, err = srv.executeAITool(context.Background(), principal, tenantID, llm.ToolCall{
		Name: "timeline_build",
		Input: map[string]any{
			"entity_type": "ip",
			"entity_id":   "8.8.4.4",
			"since":       base.Add(-time.Minute).Format(time.RFC3339),
			"until":       base.Add(2 * time.Minute).Format(time.RFC3339),
			"limit":       10,
		},
	})
	if err != nil {
		t.Fatalf("execute timeline_build: %v", err)
	}
	timelineResp, ok := exec.Payload.(timelineBuildResponse)
	if !ok {
		t.Fatalf("timeline payload type = %T", exec.Payload)
	}
	if timelineResp.Source != "small-analytics" || len(timelineResp.Items) != 2 {
		t.Fatalf("unexpected timeline tool response: %+v", timelineResp)
	}
}

type lifecycleAIToolStore struct {
	fakeStore
	items      []storage.LifecycleItem
	lastFilter storage.LifecycleFilter
	lastLimit  int
}

func (s *lifecycleAIToolStore) CreateSavedSearch(context.Context, storage.SavedSearch) (*storage.SavedSearch, error) {
	return nil, errors.New("not implemented")
}

func (s *lifecycleAIToolStore) ListSavedSearches(context.Context, uuid.UUID, uuid.UUID, int, int) ([]storage.SavedSearch, int, error) {
	return nil, 0, nil
}

func (s *lifecycleAIToolStore) GetSavedSearch(context.Context, uuid.UUID) (*storage.SavedSearch, error) {
	return nil, nil
}

func (s *lifecycleAIToolStore) UpdateSavedSearch(context.Context, uuid.UUID, storage.SavedSearch) (*storage.SavedSearch, error) {
	return nil, errors.New("not implemented")
}

func (s *lifecycleAIToolStore) DeleteSavedSearch(context.Context, uuid.UUID) error {
	return errors.New("not implemented")
}

func (s *lifecycleAIToolStore) AddEntityTag(context.Context, storage.EntityTag) (*storage.EntityTag, error) {
	return nil, errors.New("not implemented")
}

func (s *lifecycleAIToolStore) RemoveEntityTag(context.Context, uuid.UUID, string, string, string) error {
	return errors.New("not implemented")
}

func (s *lifecycleAIToolStore) ListEntityTags(context.Context, uuid.UUID, string, string) ([]storage.EntityTag, error) {
	return nil, nil
}

func (s *lifecycleAIToolStore) RecordEntityAction(context.Context, storage.EntityAction) (*storage.EntityAction, error) {
	return nil, errors.New("not implemented")
}

func (s *lifecycleAIToolStore) ListAssetCIDRs(context.Context, uuid.UUID) ([]net.IPNet, error) {
	return nil, nil
}

func (s *lifecycleAIToolStore) EntityLifecycle(_ context.Context, f storage.LifecycleFilter, limit int) ([]storage.LifecycleItem, error) {
	s.lastFilter = f
	s.lastLimit = limit
	return append([]storage.LifecycleItem(nil), s.items...), nil
}

func (s *lifecycleAIToolStore) EntitySummary(context.Context, uuid.UUID, string, string) (*storage.EntitySummary, error) {
	return nil, nil
}
