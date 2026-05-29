package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
)

func TestSprint6AIAskUsesEventCaptureEvidence(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint6IncidentStore(tenantID, nodeID)
	client := &scriptedLLMClient{responses: []llm.Response{
		{
			StopReason: llm.StopToolUse,
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "flow", Name: "flow_delta", Input: sprint6ToolInput(nodeID)}},
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "resource", Name: "resource_delta", Input: sprint6ToolInput(nodeID)}},
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "files", Name: "file_growth_delta", Input: sprint6ToolInput(nodeID)}},
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "logs", Name: "log_tail", Input: sprint6ToolInput(nodeID)}},
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "root", Name: "root_cause_findings", Input: sprint6ToolInput(nodeID)}},
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "proposal", Name: "operator_propose_action", Input: map[string]any{"action": "log.rotate", "node_id": nodeID.String(), "reason": "runaway log growth"}}},
			}},
		},
		{StopReason: llm.StopEndTurn, Message: llm.TextMessage(llm.RoleAssistant, "Port 80 doubled from 15 to 30 cps while logs grew to 13 GB; rotate logs through approval [tool:root_cause_findings:5].")},
	}}
	srv := sprint5AIServer(store, client)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/ask?tenant_id="+tenantID.String(), bytes.NewBufferString(`{"question":"What caused the incident on `+nodeID.String()+`?"}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got aiAskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.ToolTrace) != 6 {
		t.Fatalf("expected 6 tool traces, got %+v", got.ToolTrace)
	}
	for _, want := range []string{"flow_delta", "resource_delta", "file_growth_delta", "log_tail", "root_cause_findings", "operator_propose_action"} {
		if !traceContains(got.ToolTrace, want) {
			t.Fatalf("missing tool trace %s in %+v", want, got.ToolTrace)
		}
	}
	if len(got.Citations) < 5 {
		t.Fatalf("expected event-capture citations, got %+v", got.Citations)
	}
	for _, log := range store.auditLogs {
		if strings.Contains(log.Action, ".execute") || log.Action == "patch.deploy.queued" {
			t.Fatalf("sprint 6 proposals must not execute mutations, got audit %+v", log)
		}
	}
}

func TestNodeEventCaptureEndpointsReturnIncidentDeltas(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint6IncidentStore(tenantID, nodeID)
	srv := sprint5AIServer(store, &scriptedLLMClient{})

	for _, path := range []string{
		"/api/v1/nodes/" + nodeID.String() + "/flow-delta?tenant_id=" + tenantID.String(),
		"/api/v1/nodes/" + nodeID.String() + "/file-growth-delta?tenant_id=" + tenantID.String(),
		"/api/v1/nodes/" + nodeID.String() + "/resource-delta?tenant_id=" + tenantID.String(),
		"/api/v1/nodes/" + nodeID.String() + "/log-tail?tenant_id=" + tenantID.String(),
		"/api/v1/nodes/" + nodeID.String() + "/root-cause-findings?tenant_id=" + tenantID.String(),
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer operator-token")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), nodeID.String()) {
			t.Fatalf("%s response did not include node id: %s", path, rec.Body.String())
		}
	}
}

func sprint6ToolInput(nodeID uuid.UUID) map[string]any {
	return map[string]any{
		"node_id": nodeID.String(),
		"since":   "2026-05-15T08:00:00Z",
		"until":   "2026-05-15T08:08:00Z",
	}
}

func sprint6IncidentStore(tenantID, nodeID uuid.UUID) *fakeStore {
	store := sprint5AIStore(tenantID, nodeID)
	store.nodes[0].PublicIP = sql.NullString{String: "203.0.113.80", Valid: true}
	store.flowDeltas = []FlowDeltaRow{{
		TenantID: tenantID, NodeID: nodeID, Process: "nginx", Port: 80, Direction: "inbound",
		PreviousCPS: 15, CurrentCPS: 30, BytesIn: 2 * 1024 * 1024 * 1024 * 1024,
		Since: time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC), Until: time.Date(2026, 5, 15, 8, 8, 0, 0, time.UTC),
	}}
	store.fileGrowthDeltas = []FileGrowthDeltaRow{{
		TenantID: tenantID, NodeID: nodeID, Path: "/var/log/app.log", StartBytes: 30 * 1024 * 1024, EndBytes: 13 * 1024 * 1024 * 1024,
		Since: time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC), Until: time.Date(2026, 5, 15, 8, 8, 0, 0, time.UTC),
	}}
	store.resourceDeltas = []ResourceDeltaRow{
		{TenantID: tenantID, NodeID: nodeID, Metric: "cpu_percent", Previous: 20, Current: 99, SampleCount: 8},
		{TenantID: tenantID, NodeID: nodeID, Metric: "memory_percent", Previous: 60, Current: 99, SampleCount: 8},
	}
	store.logTailRows = []LogTailRow{{TenantID: tenantID, NodeID: nodeID, Source: "app", Message: "request loop writing verbose access logs", Timestamp: time.Date(2026, 5, 15, 8, 7, 0, 0, time.UTC)}}
	store.rootCauseFindings = []RootCauseFinding{{TenantID: tenantID, NodeID: nodeID, Summary: "nginx request loop drove port 80 traffic and runaway app.log growth", Confidence: "high", EvidenceIDs: []string{"flow", "files", "logs"}}}
	return store
}

func traceContains(trace []aiToolTraceEntry, name string) bool {
	for _, entry := range trace {
		if entry.Name == name && entry.OK {
			return true
		}
	}
	return false
}
