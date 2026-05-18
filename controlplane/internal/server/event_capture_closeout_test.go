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

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestFileGrowthDeltasFromTelemetryMetricSnapshots(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	since := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	until := since.Add(8 * time.Minute)
	filter := EventCaptureFilter{TenantID: tenantID, NodeID: nodeID, Since: since, Until: until, Limit: 10}

	rows := fileGrowthDeltasFromTelemetryMetrics(filter, []storage.TelemetryMetric{
		{
			TenantID: tenantID, NodeID: nodeID, MetricName: metricFileSizeBytes, MetricValue: 30 * 1024 * 1024,
			Labels: map[string]string{"path": "/var/log/app.log"}, Timestamp: since,
		},
		{
			TenantID: tenantID, NodeID: nodeID, MetricName: metricFileSizeBytes, MetricValue: 13 * 1024 * 1024 * 1024,
			Labels: map[string]string{"path": "/var/log/app.log"}, Timestamp: until,
		},
		{
			TenantID: tenantID, NodeID: nodeID, MetricName: metricFileSizeBytes, MetricValue: 40 * 1024 * 1024,
			Labels: map[string]string{"path": "/var/log/other.log"}, Timestamp: since,
		},
		{
			TenantID: tenantID, NodeID: nodeID, MetricName: metricFileSizeBytes, MetricValue: 42 * 1024 * 1024,
			Labels: map[string]string{"path": "/var/log/other.log"}, Timestamp: until,
		},
	})

	if len(rows) != 2 {
		t.Fatalf("expected two file growth rows, got %+v", rows)
	}
	if rows[0].Path != "/var/log/app.log" {
		t.Fatalf("largest growth should sort first, got %+v", rows)
	}
	if rows[0].StartBytes != 30*1024*1024 || rows[0].EndBytes != 13*1024*1024*1024 {
		t.Fatalf("unexpected app.log delta: %+v", rows[0])
	}
}

func TestTelemetryIngestAcceptsLabeledMetricSamples(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	srv := sprint5AIServer(store, &scriptedLLMClient{})

	body := `{
		"node_id":"` + nodeID.String() + `",
		"timestamp":"2026-05-15T08:00:00Z",
		"samples":[
			{"name":"file.size.bytes","value":31457280,"unit":"bytes","labels":{"path":"/var/log/app.log","source":"app"}},
			{"name":"file.size.bytes","value":41943040,"unit":"bytes","labels":{"path":"/var/log/other.log","source":"app"}}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", strings.NewReader(body))
	req = withPrincipal(req, &auth.Principal{Type: "agent", Subject: nodeID.String(), Roles: []string{"agent"}})
	rec := httptest.NewRecorder()
	srv.handleTelemetryIngest(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.telemetryMetricCreates) != 2 {
		t.Fatalf("expected two labeled metric rows, got %+v", store.telemetryMetricCreates)
	}
	got := store.telemetryMetricCreates[0]
	if got.MetricName != metricFileSizeBytes || got.MetricValue != 31457280 {
		t.Fatalf("unexpected first metric: %+v", got)
	}
	if got.MetricUnit == nil || *got.MetricUnit != "bytes" {
		t.Fatalf("expected bytes unit, got %+v", got.MetricUnit)
	}
	if got.Labels["path"] != "/var/log/app.log" || got.Labels["source"] != "app" {
		t.Fatalf("labels were not preserved: %+v", got.Labels)
	}
}

func TestLogTailToolRedactsSecretsAndCapsOutput(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint6IncidentStore(tenantID, nodeID)
	store.logTailRows = []LogTailRow{{
		TenantID:  tenantID,
		NodeID:    nodeID,
		Source:    "app",
		Message:   "login failed password=hunter2 api_key=sk-live-token bearer abc.def.ghi " + strings.Repeat("x", maxLogTailMessageBytes*2),
		Timestamp: time.Date(2026, 5, 15, 8, 7, 0, 0, time.UTC),
	}}
	srv := sprint5AIServer(store, &scriptedLLMClient{})

	exec, err := srv.executeAITool(context.Background(), &auth.Principal{Roles: []string{roleOperator}}, tenantID, llm.ToolCall{
		Name:  "log_tail",
		Input: sprint6ToolInput(nodeID),
	})
	if err != nil {
		t.Fatalf("log_tail tool: %v", err)
	}
	raw, err := json.Marshal(exec.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	body := string(raw)
	for _, forbidden := range []string{"hunter2", "sk-live-token", "abc.def.ghi"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("log tail leaked secret %q in %s", forbidden, body)
		}
	}
	if len(body) > maxLogTailMessageBytes+512 {
		t.Fatalf("log tail response was not capped: len=%d body=%s", len(body), body)
	}
}

func TestLogTailToolRequiresOperatorRole(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint6IncidentStore(tenantID, nodeID)
	srv := sprint5AIServer(store, &scriptedLLMClient{})

	_, err := srv.executeAITool(context.Background(), &auth.Principal{Roles: []string{roleViewer}}, tenantID, llm.ToolCall{
		Name:  "log_tail",
		Input: sprint6ToolInput(nodeID),
	})
	if err == nil {
		t.Fatal("viewer role should not execute log_tail")
	}

	if _, err := srv.executeAITool(context.Background(), &auth.Principal{Roles: []string{roleOperator}}, tenantID, llm.ToolCall{
		Name:  "log_tail",
		Input: sprint6ToolInput(nodeID),
	}); err != nil {
		t.Fatalf("operator role should execute log_tail: %v", err)
	}
}

func TestRootCauseToolPersistsFindingAsInvestigationEvidence(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint6IncidentStore(tenantID, nodeID)
	srv := sprint5AIServer(store, &scriptedLLMClient{})

	exec, err := srv.executeAITool(context.Background(), &auth.Principal{Roles: []string{roleOperator}}, tenantID, llm.ToolCall{
		Name:  "root_cause_findings",
		Input: sprint6ToolInput(nodeID),
	})
	if err != nil {
		t.Fatalf("root_cause_findings tool: %v", err)
	}
	if exec.Citation.Tool != "root_cause_findings" {
		t.Fatalf("unexpected citation: %+v", exec.Citation)
	}
	if len(store.aiInvestigations) != 1 {
		t.Fatalf("expected one persisted root-cause investigation, got %+v", store.aiInvestigations)
	}
	inv := store.aiInvestigations[0]
	if inv.TriggerType != "root_cause" || inv.TriggerEventType != "sprint6.event_capture" {
		t.Fatalf("unexpected persisted investigation trigger: %+v", inv)
	}
	if !strings.Contains(inv.Summary, "nginx request loop") {
		t.Fatalf("root-cause summary was not persisted: %+v", inv)
	}
	if !strings.Contains(string(inv.Evidence), "file-growth-delta") {
		t.Fatalf("persisted evidence is missing event-capture ids: %s", string(inv.Evidence))
	}
}
