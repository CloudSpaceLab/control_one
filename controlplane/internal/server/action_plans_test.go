package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestActionPlansCreateListAndReceipts(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "bank-app-01",
			State:     storage.NodeStateActive,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}},
		jobs:           map[uuid.UUID]*storage.Job{},
		actionPlans:    map[uuid.UUID]storage.ActionPlan{},
		actionReceipts: map[uuid.UUID][]storage.ActionReceipt{},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("operator", "operator-token"),
	}, store, &stubQueue{})
	handler := srv.Handler()

	createBody := map[string]any{
		"tenant_id":       tenantID.String(),
		"node_id":         nodeID.String(),
		"domain":          "patch",
		"action_kind":     "os_package_wave",
		"state":           "needs_approval",
		"risk":            "high",
		"idempotency_key": "patch-wave-1",
		"scope": map[string]any{
			"node_ids": []any{nodeID.String()},
		},
		"diff": map[string]any{
			"summary": "upgrade openssl and nginx",
		},
		"required_approvals": map[string]any{
			"roles":                []any{"admin"},
			"min_approvers":        2,
			"separation_of_duties": true,
		},
		"rollback_plan": map[string]any{
			"strategy": "package downgrade from cached repo",
		},
		"verification_plan": map[string]any{
			"checks": []any{"package_inventory", "vulnerability_rescan"},
		},
		"source_ref": map[string]any{
			"kind": "patch_deployment",
		},
	}
	rec := callActionPlanAPI(t, handler, http.MethodPost, "/api/v1/action-plans", "operator-token", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var created actionPlanResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.State != "needs_approval" || created.Diff["summary"] != "upgrade openssl and nginx" {
		t.Fatalf("unexpected created plan: %+v", created)
	}

	rec = callActionPlanAPI(t, handler, http.MethodPost, "/api/v1/action-plans", "operator-token", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected idempotent 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var idem actionPlanResponse
	if err := json.NewDecoder(rec.Body).Decode(&idem); err != nil {
		t.Fatalf("decode idempotent response: %v", err)
	}
	if idem.ID != created.ID {
		t.Fatalf("expected idempotent create to return %s, got %s", created.ID, idem.ID)
	}

	rec = callActionPlanAPI(t, handler, http.MethodGet, "/api/v1/action-plans?tenant_id="+tenantID.String()+"&domain=patch", "operator-token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var listed paginatedResponse[actionPlanResponse]
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listed.Pagination.Total != 1 || len(listed.Data) != 1 || listed.Data[0].ID != created.ID {
		t.Fatalf("unexpected list response: %+v", listed)
	}

	receiptBody := map[string]any{
		"job_id": jobID.String(),
		"state":  "verified",
		"receipt": map[string]any{
			"packages_upgraded": 2,
		},
		"verification": map[string]any{
			"vulnerabilities_remaining": 0,
		},
		"rollback_ref": "repo-cache://patch-wave-1",
	}
	rec = callActionPlanAPI(t, handler, http.MethodPost, "/api/v1/action-plans/"+created.ID+"/receipts", "operator-token", receiptBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected receipt 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var receipt actionReceiptResponse
	if err := json.NewDecoder(rec.Body).Decode(&receipt); err != nil {
		t.Fatalf("decode receipt response: %v", err)
	}
	if receipt.State != "verified" || receipt.JobID == nil || *receipt.JobID != jobID.String() {
		t.Fatalf("unexpected receipt response: %+v", receipt)
	}

	rec = callActionPlanAPI(t, handler, http.MethodGet, "/api/v1/action-plans/"+created.ID, "operator-token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected get 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var updated actionPlanResponse
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if updated.State != "verified" {
		t.Fatalf("expected plan state to mirror receipt, got %s", updated.State)
	}

	rec = callActionPlanAPI(t, handler, http.MethodGet, "/api/v1/action-plans/"+created.ID+"/receipts", "operator-token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected receipts 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var receipts struct {
		Data []actionReceiptResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&receipts); err != nil {
		t.Fatalf("decode receipts response: %v", err)
	}
	if len(receipts.Data) != 1 || receipts.Data[0].ID != receipt.ID {
		t.Fatalf("unexpected receipts response: %+v", receipts)
	}
}

func TestActionPlansRejectDirectApprovedCreate(t *testing.T) {
	tenantID := uuid.New()
	store := &fakeStore{
		actionPlans:    map[uuid.UUID]storage.ActionPlan{},
		actionReceipts: map[uuid.UUID][]storage.ActionReceipt{},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("operator", "operator-token"),
	}, store, &stubQueue{})

	rec := callActionPlanAPI(t, srv.Handler(), http.MethodPost, "/api/v1/action-plans", "operator-token", map[string]any{
		"tenant_id":   tenantID.String(),
		"domain":      "patch",
		"action_kind": "os_package_wave",
		"state":       "approved",
		"risk":        "medium",
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "cannot be created directly") {
		t.Fatalf("expected direct approved create to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestActionPlansRequireDualApprovalForHighRisk(t *testing.T) {
	tenantID := uuid.New()
	store := &fakeStore{
		actionPlans:    map[uuid.UUID]storage.ActionPlan{},
		actionReceipts: map[uuid.UUID][]storage.ActionReceipt{},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("operator", "operator-token"),
	}, store, &stubQueue{})

	rec := callActionPlanAPI(t, srv.Handler(), http.MethodPost, "/api/v1/action-plans", "operator-token", map[string]any{
		"tenant_id":   tenantID.String(),
		"domain":      "patch",
		"action_kind": "os_package_wave",
		"state":       "needs_approval",
		"risk":        "high",
		"required_approvals": map[string]any{
			"roles":         []any{"admin"},
			"min_approvers": 1,
		},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "min_approvers >= 2") {
		t.Fatalf("expected missing dual approval to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestActionPlansDualApprovalWorkflow(t *testing.T) {
	tenantID := uuid.New()
	store := &fakeStore{
		actionPlans:         map[uuid.UUID]storage.ActionPlan{},
		actionReceipts:      map[uuid.UUID][]storage.ActionReceipt{},
		actionPlanApprovals: map[uuid.UUID][]storage.ActionPlanApproval{},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("admin", "creator-token", "admin-a-token", "admin-b-token"),
	}, store, &stubQueue{})
	handler := srv.Handler()

	rec := callActionPlanAPI(t, handler, http.MethodPost, "/api/v1/action-plans", "creator-token", map[string]any{
		"tenant_id":   tenantID.String(),
		"domain":      "patch",
		"action_kind": "os_package_wave",
		"state":       "needs_approval",
		"risk":        "high",
		"required_approvals": map[string]any{
			"roles":                []any{"admin"},
			"min_approvers":        2,
			"separation_of_duties": true,
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var created actionPlanResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	rec = callActionPlanAPI(t, handler, http.MethodPost, "/api/v1/action-plans/"+created.ID+"/approvals", "creator-token", map[string]any{
		"decision": "approve",
		"note":     "self approval should fail",
	})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "separation of duties") {
		t.Fatalf("expected self-approval rejection, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = callActionPlanAPI(t, handler, http.MethodPost, "/api/v1/action-plans/"+created.ID+"/approvals", "admin-a-token", map[string]any{
		"decision": "approve",
		"note":     "CAB-100 first approver",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected first approval 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var first actionPlanApprovalResultResponse
	if err := json.NewDecoder(rec.Body).Decode(&first); err != nil {
		t.Fatalf("decode first approval: %v", err)
	}
	if first.ActionPlan.State != "needs_approval" || first.ApprovedCount != 1 || first.RequiredCount != 2 || first.NextActionHint != "additional_approval_required" {
		t.Fatalf("unexpected first approval result: %+v", first)
	}

	rec = callActionPlanAPI(t, handler, http.MethodPost, "/api/v1/action-plans/"+created.ID+"/approvals", "admin-a-token", map[string]any{
		"decision": "approve",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected duplicate approval conflict got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = callActionPlanAPI(t, handler, http.MethodPost, "/api/v1/action-plans/"+created.ID+"/approvals", "admin-b-token", map[string]any{
		"decision": "approve",
		"note":     "CAB-100 second approver",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected second approval 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var second actionPlanApprovalResultResponse
	if err := json.NewDecoder(rec.Body).Decode(&second); err != nil {
		t.Fatalf("decode second approval: %v", err)
	}
	if second.ActionPlan.State != "approved" || second.ApprovedCount != 2 || !second.StateChanged {
		t.Fatalf("expected approved plan after second approver, got %+v", second)
	}

	rec = callActionPlanAPI(t, handler, http.MethodGet, "/api/v1/action-plans/"+created.ID+"/approvals", "admin-b-token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected approvals list 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Data []actionPlanApprovalResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode approvals list: %v", err)
	}
	if len(listed.Data) != 2 || listed.Data[0].ActorSubject != "admin-a-token" || listed.Data[1].ActorSubject != "admin-b-token" {
		t.Fatalf("unexpected approvals list: %+v", listed)
	}
}

func callActionPlanAPI(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
