package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type scriptedLLMClient struct {
	requests  []llm.Request
	responses []llm.Response
}

func (c *scriptedLLMClient) Generate(_ context.Context, req llm.Request) (llm.Response, error) {
	c.requests = append(c.requests, req)
	if len(c.responses) == 0 {
		return llm.Response{Message: llm.TextMessage(llm.RoleAssistant, "done"), StopReason: llm.StopEndTurn}, nil
	}
	resp := c.responses[0]
	c.responses = c.responses[1:]
	return resp, nil
}

func TestAIAskUsesInvestigationToolsAndReturnsCitations(t *testing.T) {
	t.Setenv("FEATURE_AI_ASK", "true")
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	client := &scriptedLLMClient{responses: []llm.Response{
		{
			StopReason: llm.StopToolUse,
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "toolu_1", Name: "node_documentation", Input: map[string]any{"node_id": nodeID.String()}}},
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "toolu_2", Name: "node_packages", Input: map[string]any{"node_id": nodeID.String()}}},
			}},
		},
		{StopReason: llm.StopEndTurn, Message: llm.TextMessage(llm.RoleAssistant, "core-api-01 is healthy enough but nginx is exposed [tool:node_documentation:1].")},
	}}
	srv := sprint5AIServer(store, client)

	body := bytes.NewBufferString(`{"question":"For node ` + nodeID.String() + `, explain health, services, and packages."}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/ask?tenant_id="+tenantID.String(), body)
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
	if got.Answer == "" || len(got.Citations) < 2 {
		t.Fatalf("expected answer with citations, got %+v", got)
	}
	if len(got.ToolTrace) != 2 {
		t.Fatalf("expected two tool trace entries, got %+v", got.ToolTrace)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two LLM calls, got %d", len(client.requests))
	}
	if len(client.requests[0].Tools) < 5 {
		t.Fatalf("expected investigation tools to be exposed, got %+v", client.requests[0].Tools)
	}
	if len(store.auditLogs) == 0 || store.auditLogs[len(store.auditLogs)-1].Action != "ai.tool_call" {
		t.Fatalf("expected ai.tool_call audit entries, got %+v", store.auditLogs)
	}
}

func TestAIAskToolRBACDeniesOperatorAdminTool(t *testing.T) {
	t.Setenv("FEATURE_AI_ASK", "true")
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	client := &scriptedLLMClient{responses: []llm.Response{
		{
			StopReason: llm.StopToolUse,
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "toolu_1", Name: "operator_execute_action", Input: map[string]any{"action": "patch.deploy", "confirmed": true}}},
			}},
		},
		{StopReason: llm.StopEndTurn, Message: llm.TextMessage(llm.RoleAssistant, "I cannot execute that action without the right gate.")},
	}}
	srv := sprint5AIServer(store, client)

	body := bytes.NewBufferString(`{"question":"execute a patch action"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/ask?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer operator-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	second := client.requests[1]
	foundDeniedResult := false
	for _, msg := range second.Messages {
		for _, block := range msg.Content {
			if block.ToolResult != nil && block.ToolResult.IsError && strings.Contains(block.ToolResult.Content, "requires role admin") {
				foundDeniedResult = true
			}
		}
	}
	if !foundDeniedResult {
		t.Fatalf("expected denied tool result in second request: %+v", second.Messages)
	}
}

func TestAIAskOperatorProposalIsReadOnly(t *testing.T) {
	t.Setenv("FEATURE_AI_ASK", "true")
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	client := &scriptedLLMClient{responses: []llm.Response{
		{
			StopReason: llm.StopToolUse,
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "toolu_1", Name: "operator_propose_action", Input: map[string]any{"action": "patch.deploy", "node_id": nodeID.String(), "reason": "critical package"}}},
			}},
		},
		{StopReason: llm.StopEndTurn, Message: llm.TextMessage(llm.RoleAssistant, "I can propose a patch action, but execution requires confirmation.")},
	}}
	srv := sprint5AIServer(store, client)

	body := bytes.NewBufferString(`{"question":"propose how to patch the node"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/ask?tenant_id="+tenantID.String(), body)
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
	if len(got.ToolTrace) != 1 || got.ToolTrace[0].Name != "operator_propose_action" {
		t.Fatalf("expected proposal trace, got %+v", got.ToolTrace)
	}
	for _, log := range store.auditLogs {
		if log.Action == "patch.deploy.queued" || strings.Contains(log.Action, ".execute") {
			t.Fatalf("proposal must not mutate or execute actions, got audit %+v", log)
		}
	}
}

func sprint5AIStore(tenantID, nodeID uuid.UUID) *fakeStore {
	arch := "amd64"
	return &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "Bank Ops"}},
		nodes: []storage.Node{{
			ID:           nodeID,
			TenantID:     tenantID,
			Hostname:     "core-api-01",
			OS:           sql.NullString{String: "linux", Valid: true},
			Arch:         sql.NullString{String: "amd64", Valid: true},
			PublicIP:     sql.NullString{String: "203.0.113.10", Valid: true},
			State:        storage.NodeStateActive,
			AgentVersion: sql.NullString{String: "1.2.3", Valid: true},
		}},
		nodeServices: map[uuid.UUID][]storage.NodeService{nodeID: {{
			NodeID: nodeID, TenantID: tenantID, Process: "nginx", ListenAddr: "0.0.0.0", Port: 443, ServiceKind: "https",
		}}},
		nodePackages: map[uuid.UUID][]storage.NodePackage{nodeID: {{
			NodeID: nodeID, Name: "openssl", Version: "3.0.2", Arch: &arch, Source: "dpkg",
		}}},
		firewallStates: map[uuid.UUID]storage.NodeFirewallState{nodeID: {
			NodeID: nodeID, FirewallType: "ufw", Enabled: true, ObservedAt: time.Now().UTC(),
		}},
		nodeHealthScores: map[uuid.UUID]storage.NodeHealthScore{nodeID: {
			NodeID: nodeID, Score: 82, RiskLevel: "low", Components: map[string]any{"load1_high": 0}, ComputedAt: time.Now().UTC(),
		}},
		alerts: []storage.Alert{{
			ID: uuid.New(), TenantID: tenantID, NodeID: uuid.NullUUID{UUID: nodeID, Valid: true}, Severity: "high", Title: "CPU spike", State: "open", OpenedAt: time.Now().UTC(),
		}},
		auditLogs: []storage.AuditLog{},
		aiConfig:  &storage.AIConfig{TenantID: tenantID, Provider: "anthropic", Model: "test-model", APIKey: "test-key"},
	}
}

func sprint5AIServer(store *fakeStore, client *scriptedLLMClient) *Server {
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("operator", "operator-token"),
	}, store, &stubQueue{})
	srv.auditAsync = false
	srv.aiClientFactory = func(storage.AIConfig) (llm.Client, error) { return client, nil }
	srv.aiClock = func() time.Time { return time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC) }
	return srv
}
