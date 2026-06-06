package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestOperatorProposalToolPersistsDryRunProposal(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	client := &scriptedLLMClient{responses: []llm.Response{
		{
			StopReason: llm.StopToolUse,
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				{Type: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: "proposal", Name: "operator_propose_action", Input: map[string]any{
					"action":  "log.rotate",
					"node_id": nodeID.String(),
					"reason":  "runaway log growth",
				}}},
			}},
		},
		{StopReason: llm.StopEndTurn, Message: llm.TextMessage(llm.RoleAssistant, "Queued a dry-run proposal [tool:operator_propose_action:1].")},
	}}
	srv := sprint5AIServer(store, client)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/ask?tenant_id="+tenantID.String(), bytes.NewBufferString(`{"question":"Propose a safe log rotation for this node"}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	if len(store.aiOperatorProposals) != 1 {
		t.Fatalf("expected one proposal, got %+v", store.aiOperatorProposals)
	}
	proposal := store.aiOperatorProposals[0]
	if proposal.TenantID != tenantID || proposal.NodeID != nodeID {
		t.Fatalf("proposal scope mismatch: %+v", proposal)
	}
	if proposal.Action != "log.rotate" || proposal.Reason != "runaway log growth" {
		t.Fatalf("proposal content mismatch: %+v", proposal)
	}
	if !proposal.DryRun || proposal.Status != storage.AIOperatorProposalStatusProposed {
		t.Fatalf("proposal safety state mismatch: %+v", proposal)
	}
	if proposal.ApprovalKind != "remediation" || proposal.ApprovalPath != "/api/v1/remediation/approvals" {
		t.Fatalf("proposal approval route mismatch: %+v", proposal)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/ai/operator/proposals?tenant_id="+tenantID.String(), nil)
	listReq.Header.Set("Authorization", "Bearer operator-token")
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var list paginatedResponse[storage.AIOperatorProposal]
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode proposal list: %v", err)
	}
	if list.Pagination.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != proposal.ID {
		t.Fatalf("unexpected proposal list: %+v", list)
	}
}

func TestFanOutEventsPersistsAnomalyInvestigation(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := sprint5AIStore(tenantID, nodeID)
	srv := sprint5AIServer(store, &scriptedLLMClient{})
	ts := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	_, err := srv.fanOutEvents(context.Background(), tenantID, nodeID, []IngestedEvent{{
		Type:        "conn.open",
		TS:          ts,
		NodeID:      nodeID.String(),
		DstIP:       "198.51.100.10",
		DstPort:     443,
		ProcessName: "curl",
	}})
	if err != nil {
		t.Fatalf("fan out events: %v", err)
	}

	if len(store.aiInvestigations) != 1 {
		t.Fatalf("expected one investigation, got %+v", store.aiInvestigations)
	}
	inv := store.aiInvestigations[0]
	if inv.TriggerType != "anomaly" || inv.TriggerEventType != "anomaly.new_destination" {
		t.Fatalf("unexpected investigation trigger: %+v", inv)
	}
	if inv.TenantID != tenantID || inv.NodeID != nodeID || inv.Status != storage.AIInvestigationStatusOpen {
		t.Fatalf("unexpected investigation scope/status: %+v", inv)
	}
	if !strings.Contains(inv.Summary, "first connection to 198.51.100.10") {
		t.Fatalf("unexpected investigation summary: %q", inv.Summary)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/ai/investigations?tenant_id="+tenantID.String()+"&trigger_type=anomaly", nil)
	listReq.Header.Set("Authorization", "Bearer operator-token")
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var list paginatedResponse[storage.AIInvestigation]
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode investigation list: %v", err)
	}
	if list.Pagination.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != inv.ID {
		t.Fatalf("unexpected investigation list: %+v", list)
	}
}

func TestFirstSeenDestinationMessageOmitsUnknownProcess(t *testing.T) {
	tests := []struct {
		name    string
		dstIP   string
		process string
		want    string
	}{
		{
			name:    "with process",
			dstIP:   "198.51.100.10",
			process: "curl",
			want:    "first connection to 198.51.100.10 by curl",
		},
		{
			name:  "without process",
			dstIP: "20.169.85.72",
			want:  "first connection to 20.169.85.72",
		},
		{
			name:    "trims process",
			dstIP:   "20.169.85.72",
			process: "  nginx  ",
			want:    "first connection to 20.169.85.72 by nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstSeenDestinationMessage(tt.dstIP, tt.process); got != tt.want {
				t.Fatalf("firstSeenDestinationMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
