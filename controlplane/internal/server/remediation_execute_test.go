package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestExecuteRemediationScriptBindsTenantAndLease(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	scriptID := uuid.New()
	script := &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        "cis-1.1.1",
		Platform:      "linux",
		ScriptType:    "plan",
		ScriptContent: "collect planned remediation evidence",
		Enabled:       true,
		Metadata: map[string]any{
			"safety_class":      "read_only",
			"requires_approval": false,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store := &remediationTestStore{
		scripts:     map[string]*storage.RemediationScript{script.RuleID: script},
		scriptsByID: map[uuid.UUID]*storage.RemediationScript{scriptID: script},
	}
	store.nodes = []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "prod-web-01"}}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)
	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)
	srv.auditAsync = false

	body, _ := json.Marshal(executeRemediationScriptRequest{NodeID: nodeID.String()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/scripts/"+scriptID.String()+"/execute", bytes.NewReader(body))
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: uuid.New().String(), Roles: []string{roleOperator}})
	rec := httptest.NewRecorder()

	srv.handleExecuteRemediationScript(rec, req, scriptID)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", rec.Code, rec.Body.String())
	}
	if len(store.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(store.jobs))
	}
	for _, job := range store.jobs {
		if job.TenantID != tenantID {
			t.Fatalf("job tenant = %s, want %s", job.TenantID, tenantID)
		}
		var payload map[string]any
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			t.Fatalf("decode job payload: %v", err)
		}
		if payload["tenant_id"] != tenantID.String() || payload["manual_triggered"] != true {
			t.Fatalf("unexpected job payload: %#v", payload)
		}
		if _, ok := payload["action_plan_id"].(string); !ok {
			t.Fatalf("job payload missing action_plan_id: %#v", payload)
		}
	}
	if len(store.actionPlans) != 1 {
		t.Fatalf("action plans = %d, want 1", len(store.actionPlans))
	}
	store.mu.Lock()
	lease, ok := store.leases[nodeID]
	store.mu.Unlock()
	if !ok || lease.TenantID != tenantID {
		t.Fatalf("expected remediation lease for node/tenant, got ok=%v lease=%+v", ok, lease)
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(queue.tasks))
	}
}

func TestExecuteRemediationScriptRequiresApprovalForMutableScript(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	scriptID := uuid.New()
	script := &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        "cis-1.1.5",
		Platform:      "linux",
		ScriptType:    "bash",
		ScriptContent: "systemctl restart sshd",
		Enabled:       true,
		Metadata: map[string]any{
			"safety_class":      "read_only",
			"requires_approval": false,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store := &remediationTestStore{
		scripts:     map[string]*storage.RemediationScript{script.RuleID: script},
		scriptsByID: map[uuid.UUID]*storage.RemediationScript{scriptID: script},
	}
	store.nodes = []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "prod-web-01"}}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)
	store.remediationApprovals = map[uuid.UUID]storage.RemediationApproval{}
	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)
	srv.auditAsync = false

	body, _ := json.Marshal(executeRemediationScriptRequest{NodeID: nodeID.String(), Reason: "maintenance window"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/scripts/"+scriptID.String()+"/execute", bytes.NewReader(body))
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: uuid.New().String(), Roles: []string{roleOperator}})
	rec := httptest.NewRecorder()

	srv.handleExecuteRemediationScript(rec, req, scriptID)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", rec.Code, rec.Body.String())
	}
	var resp executeRemediationScriptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "approval_required" || resp.ApprovalID == "" || !resp.RequiresApproval || resp.SafetyClass != "privileged" {
		t.Fatalf("unexpected approval response: %#v", resp)
	}
	if len(store.jobs) != 0 {
		t.Fatalf("approval-required request created %d jobs", len(store.jobs))
	}
	queue.mu.Lock()
	if len(queue.tasks) != 0 {
		t.Fatalf("approval-required request queued %d tasks", len(queue.tasks))
	}
	queue.mu.Unlock()
	if len(store.remediationApprovals) != 1 {
		t.Fatalf("expected one approval, got %d", len(store.remediationApprovals))
	}
	for _, approval := range store.remediationApprovals {
		if approval.TenantID != tenantID || approval.NodeID != nodeID || approval.RuleID != script.RuleID || approval.ScriptID != scriptID {
			t.Fatalf("approval was not bound to target/script: %+v", approval)
		}
		if approval.Status != storage.ApprovalStatusPending || approval.Severity != "high" {
			t.Fatalf("unexpected approval state: %+v", approval)
		}
		var payload map[string]any
		if err := json.Unmarshal(approval.TaskPayload, &payload); err != nil {
			t.Fatalf("decode approval payload: %v", err)
		}
		if _, ok := payload["script_content"]; ok {
			t.Fatalf("approval payload must not persist script content: %#v", payload)
		}
		if payload["operator_reason"] != "maintenance window" || payload["safety_class"] != "privileged" {
			t.Fatalf("unexpected approval payload: %#v", payload)
		}
	}
}

func TestApproveRemediationApprovalRejectsChangedScriptArtifact(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	scriptID := uuid.New()
	script := &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        "cis-1.1.7",
		Platform:      "linux",
		ScriptType:    "bash",
		ScriptContent: "systemctl restart auditd",
		Version:       4,
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := &remediationTestStore{
		scripts:     map[string]*storage.RemediationScript{script.RuleID: script},
		scriptsByID: map[uuid.UUID]*storage.RemediationScript{scriptID: script},
	}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)
	store.remediationApprovals = map[uuid.UUID]storage.RemediationApproval{}
	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, queue)
	srv.auditAsync = false

	approval, err := srv.createManualRemediationApproval(context.Background(), tenantID, nodeID, script.RuleID, script, remediationDescriptorFromScript(*script), executeRemediationScriptRequest{})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}

	script.ScriptContent = "systemctl restart sshd"
	script.UpdatedAt = time.Now()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/approvals/"+approval.ID.String()+"/approve?tenant_id="+tenantID.String(), nil)
	srv.handleApproveRemediationApproval(rec, req, approval.ID, &auth.Principal{Type: "user", Subject: uuid.New().String(), Roles: []string{roleOperator}})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
	got, err := store.GetRemediationApproval(context.Background(), approval.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if got.Status != storage.ApprovalStatusPending {
		t.Fatalf("approval status = %s, want pending", got.Status)
	}
	if len(store.jobs) != 0 {
		t.Fatalf("changed script approval created %d jobs", len(store.jobs))
	}
	queue.mu.Lock()
	if len(queue.tasks) != 0 {
		t.Fatalf("changed script approval queued %d tasks", len(queue.tasks))
	}
	queue.mu.Unlock()
}

func TestApproveRemediationApprovalRejectsDisabledScript(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	scriptID := uuid.New()
	script := &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        "cis-1.1.8",
		Platform:      "linux",
		ScriptType:    "bash",
		ScriptContent: "systemctl restart auditd",
		Version:       2,
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := &remediationTestStore{
		scripts:     map[string]*storage.RemediationScript{script.RuleID: script},
		scriptsByID: map[uuid.UUID]*storage.RemediationScript{scriptID: script},
	}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)
	store.remediationApprovals = map[uuid.UUID]storage.RemediationApproval{}
	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, queue)
	srv.auditAsync = false

	approval, err := srv.createManualRemediationApproval(context.Background(), tenantID, nodeID, script.RuleID, script, remediationDescriptorFromScript(*script), executeRemediationScriptRequest{})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}

	script.Enabled = false

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/approvals/"+approval.ID.String()+"/approve?tenant_id="+tenantID.String(), nil)
	srv.handleApproveRemediationApproval(rec, req, approval.ID, &auth.Principal{Type: "user", Subject: uuid.New().String(), Roles: []string{roleOperator}})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
	got, err := store.GetRemediationApproval(context.Background(), approval.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if got.Status != storage.ApprovalStatusPending {
		t.Fatalf("approval status = %s, want pending", got.Status)
	}
	if len(store.jobs) != 0 {
		t.Fatalf("disabled script approval created %d jobs", len(store.jobs))
	}
}

func TestApproveRemediationApprovalRechecksDispatchSafetyGates(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	scriptID := uuid.New()
	script := &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        "cis-1.1.9",
		Platform:      "linux",
		ScriptType:    "bash",
		ScriptContent: "systemctl restart auditd",
		Version:       1,
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := &remediationTestStore{
		scripts:     map[string]*storage.RemediationScript{script.RuleID: script},
		scriptsByID: map[uuid.UUID]*storage.RemediationScript{scriptID: script},
	}
	store.nodes = []storage.Node{{
		ID:       nodeID,
		TenantID: tenantID,
		Hostname: "prod-web-01",
		Labels:   map[string]any{"remediation": "manual-only"},
	}}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)
	store.remediationApprovals = map[uuid.UUID]storage.RemediationApproval{}
	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, queue)
	srv.auditAsync = false

	approval, err := srv.createManualRemediationApproval(context.Background(), tenantID, nodeID, script.RuleID, script, remediationDescriptorFromScript(*script), executeRemediationScriptRequest{})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/approvals/"+approval.ID.String()+"/approve?tenant_id="+tenantID.String(), nil)
	srv.handleApproveRemediationApproval(rec, req, approval.ID, &auth.Principal{Type: "user", Subject: uuid.New().String(), Roles: []string{roleOperator}})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("manual-only")) {
		t.Fatalf("expected manual-only gate response, got %s", rec.Body.String())
	}
	got, err := store.GetRemediationApproval(context.Background(), approval.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if got.Status != storage.ApprovalStatusPending {
		t.Fatalf("approval status = %s, want pending", got.Status)
	}
	if len(store.jobs) != 0 {
		t.Fatalf("safety-gated approval created %d jobs", len(store.jobs))
	}
	queue.mu.Lock()
	if len(queue.tasks) != 0 {
		t.Fatalf("safety-gated approval queued %d tasks", len(queue.tasks))
	}
	queue.mu.Unlock()
}

func TestApproveRemediationApprovalRechecksCircuitBreaker(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	scriptID := uuid.New()
	script := &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        "cis-1.1.10",
		Platform:      "linux",
		ScriptType:    "bash",
		ScriptContent: "systemctl restart auditd",
		Version:       1,
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := &remediationTestStore{
		scripts:     map[string]*storage.RemediationScript{script.RuleID: script},
		scriptsByID: map[uuid.UUID]*storage.RemediationScript{scriptID: script},
	}
	store.nodes = []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "prod-web-01"}}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)
	store.remediationApprovals = map[uuid.UUID]storage.RemediationApproval{}
	store.circuitBreakers = map[string]storage.RemediationCircuitBreakerState{
		fakeBreakerKey(tenantID, script.RuleID): {
			TenantID:      tenantID,
			RuleID:        script.RuleID,
			TrippedAt:     time.Now().UTC(),
			TrippedReason: "recent failures",
		},
	}
	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, queue)
	srv.auditAsync = false

	approval, err := srv.createManualRemediationApproval(context.Background(), tenantID, nodeID, script.RuleID, script, remediationDescriptorFromScript(*script), executeRemediationScriptRequest{})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/approvals/"+approval.ID.String()+"/approve?tenant_id="+tenantID.String(), nil)
	srv.handleApproveRemediationApproval(rec, req, approval.ID, &auth.Principal{Type: "user", Subject: uuid.New().String(), Roles: []string{roleOperator}})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("circuit breaker")) {
		t.Fatalf("expected circuit breaker gate response, got %s", rec.Body.String())
	}
	if len(store.jobs) != 0 {
		t.Fatalf("circuit-gated approval created %d jobs", len(store.jobs))
	}
}

func TestExecuteRemediationScriptRejectsCrossTenantNode(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	scriptID := uuid.New()
	script := &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        "cis-1.1.2",
		Platform:      "linux",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := &remediationTestStore{
		scripts:     map[string]*storage.RemediationScript{script.RuleID: script},
		scriptsByID: map[uuid.UUID]*storage.RemediationScript{scriptID: script},
	}
	store.nodes = []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "prod-web-01"}}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)
	srv.auditAsync = false

	body, _ := json.Marshal(executeRemediationScriptRequest{
		TenantID: otherTenantID.String(),
		NodeID:   nodeID.String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remediation/scripts/"+scriptID.String()+"/execute", bytes.NewReader(body))
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: uuid.New().String(), Roles: []string{roleOperator}})
	rec := httptest.NewRecorder()

	srv.handleExecuteRemediationScript(rec, req, scriptID)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
	if len(store.jobs) != 0 {
		t.Fatalf("cross-tenant request created %d jobs", len(store.jobs))
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 0 {
		t.Fatalf("cross-tenant request queued %d tasks", len(queue.tasks))
	}
}
