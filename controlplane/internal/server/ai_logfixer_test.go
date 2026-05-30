package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	contractsv1 "github.com/CloudSpaceLab/control_one/controlplane/internal/ailogfixercontracts/v1"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestBuildAILogFixerTriggerBundleFromWebRequest5xx(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	ev := IngestedEvent{
		Type:        "web.request",
		TS:          time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
		NodeID:      nodeID.String(),
		EventID:     "evt-1",
		Severity:    "warning",
		Message:     "GET /orders returned 500",
		DedupKey:    "web-500-orders",
		Collector:   "nginx",
		ProcessName: "nginx",
		Details: map[string]any{
			"status_code":   500,
			"service":       "checkout-api",
			"environment":   "prod",
			"server_group":  "web",
			"path_template": "/orders/:id",
		},
	}

	bundle, ok := buildAILogFixerTriggerBundle(tenantID, uuid.Nil, ev)
	if !ok {
		t.Fatal("expected web.request 5xx to produce an AI LogFixer trigger")
	}
	if bundle.ServiceKey != "checkout-api-prod-web" {
		t.Fatalf("unexpected service key: %s", bundle.ServiceKey)
	}
	if bundle.Request.ContractVersion != contractsv1.ContractVersion {
		t.Fatalf("unexpected contract version: %s", bundle.Request.ContractVersion)
	}
	if bundle.RemediationPlan.Status != contractsv1.RemediationStatusAwaitingApproval {
		t.Fatalf("unexpected remediation status: %s", bundle.RemediationPlan.Status)
	}
	if err := bundle.Request.Validate(); err != nil {
		t.Fatalf("request should be contract-valid: %v", err)
	}
}

func TestBuildAILogFixerTriggerBundleRecognizesLaravelConfigCacheDrift(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	ev := IngestedEvent{
		Type:        "log.line",
		TS:          time.Date(2026, 5, 30, 9, 0, 0, 0, time.UTC),
		NodeID:      nodeID.String(),
		EventID:     "evt-laravel-db-credential-cache",
		Severity:    "error",
		Message:     "Laravel production.ERROR: SQLSTATE[HY000] [1045] Access denied for user 'bank_app'@'10.0.3.14'",
		DedupKey:    "laravel-db-auth-failure",
		Collector:   "filelog",
		ProcessName: "php-fpm",
		Details: map[string]any{
			"service":      "core-banking",
			"environment":  "prod",
			"server_group": "app",
			"framework":    "laravel",
			"log_path":     "storage/logs/laravel.log",
		},
	}

	bundle, ok := buildAILogFixerTriggerBundle(tenantID, uuid.Nil, ev)
	if !ok {
		t.Fatal("expected Laravel database credential log to trigger AI LogFixer")
	}
	if bundle.Diagnosis.Confidence < 0.8 {
		t.Fatalf("confidence = %.2f, want Laravel-specific confidence", bundle.Diagnosis.Confidence)
	}
	if !strings.Contains(strings.ToLower(bundle.Diagnosis.SuspectedRootCause), "config/cache") {
		t.Fatalf("root cause did not identify cached config drift: %s", bundle.Diagnosis.SuspectedRootCause)
	}
	if !bundle.RemediationPlan.ApprovalRequired {
		t.Fatalf("Laravel temporary fix must require approval: %+v", bundle.RemediationPlan)
	}
	var hasApply bool
	for _, action := range bundle.RemediationPlan.NextActions {
		if action.ActionType == JobTypeAILogFixerApply {
			hasApply = true
			break
		}
	}
	if !hasApply {
		t.Fatalf("remediation plan missing approved apply next action: %+v", bundle.RemediationPlan.NextActions)
	}
	var hasArtisanStep bool
	for _, rec := range bundle.Diagnosis.Recommendations {
		for _, step := range rec.Steps {
			if strings.Contains(step, "php artisan config:clear") {
				hasArtisanStep = true
				break
			}
		}
	}
	if !hasArtisanStep {
		t.Fatalf("recommendations missing Laravel artisan cache refresh: %+v", bundle.Diagnosis.Recommendations)
	}
	var hasLaravelTag bool
	for _, tag := range bundle.Request.SignalFingerprint.Tags {
		if tag == "framework:laravel" {
			hasLaravelTag = true
			break
		}
	}
	if !hasLaravelTag {
		t.Fatalf("request tags missing Laravel marker: %+v", bundle.Request.SignalFingerprint.Tags)
	}
}

func TestBuildAILogFixerTriggerBundleSkipsInfoLogLine(t *testing.T) {
	_, ok := buildAILogFixerTriggerBundle(uuid.New(), uuid.New(), IngestedEvent{
		Type:     "log.line",
		TS:       time.Now().UTC(),
		Severity: "info",
		Message:  "request completed",
	})
	if ok {
		t.Fatal("did not expect an info log.line to trigger AI LogFixer")
	}
}

func TestBuildDorisEventRowsRoutesLongRunningDBQueries(t *testing.T) {
	rows := buildDorisEventRows(uuid.New(), uuid.New(), []IngestedEvent{{
		Type:       "db.query.long_running",
		TS:         time.Now().UTC(),
		DurationMS: 120000,
		Details: map[string]any{
			"engine":        "postgres",
			"database_name": "orders",
			"query_hash":    "abc123",
		},
	}})
	if len(rows.db) != 1 {
		t.Fatalf("expected one db_queries row, got %d", len(rows.db))
	}
}

func TestJobTargetsAgentNode(t *testing.T) {
	nodeID := uuid.New()
	matching := &auth.Principal{Type: "agent", Name: nodeID.String(), Subject: nodeID.String()}
	other := &auth.Principal{Type: "agent", Name: uuid.NewString(), Subject: uuid.NewString()}
	job := &storage.Job{Payload: []byte(`{"node_id":"` + nodeID.String() + `"}`)}

	if !jobTargetsAgentNode(job, matching) {
		t.Fatal("expected matching agent node to access job")
	}
	if jobTargetsAgentNode(job, other) {
		t.Fatal("did not expect non-matching agent node to access job")
	}
}

func TestAILogFixerActionCreatesUnifiedPlanAndReceipt(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	runID := uuid.New()
	jobID := uuid.New()
	job := &storage.Job{
		ID:       jobID,
		TenantID: tenantID,
		Type:     JobTypeAILogFixerApply,
		Status:   storage.JobStatusQueued,
		Payload:  []byte(`{}`),
	}
	store := &fakeStore{
		jobs:              map[uuid.UUID]*storage.Job{jobID: job},
		aiLogFixerActions: map[uuid.UUID]storage.AILogFixerAction{},
		actionPlans:       map[uuid.UUID]storage.ActionPlan{},
		actionReceipts:    map[uuid.UUID][]storage.ActionReceipt{},
	}
	srv := &Server{store: store, logger: zap.NewNop()}
	payload := aiLogFixerJobPayload{
		TenantID:   tenantID.String(),
		NodeID:     nodeID.String(),
		RunID:      runID.String(),
		ServiceKey: "checkout-api-prod",
		Action:     JobTypeAILogFixerApply,
		Policy: map[string]any{
			"approved": true,
		},
		RemediationPlan: []byte(`{"summary":"restart service after config validation"}`),
	}
	if err := srv.createAILogFixerActionForJob(context.Background(), job, payload); err != nil {
		t.Fatalf("create action: %v", err)
	}
	action := store.aiLogFixerActions[jobID]
	rawPlanID, _ := action.Policy["action_plan_id"].(string)
	planID, err := uuid.Parse(rawPlanID)
	if err != nil {
		t.Fatalf("expected action_plan_id in policy, got %#v", action.Policy)
	}
	if len(store.actionPlans) != 1 || store.actionPlans[planID].Domain != "ai_logfixer" {
		t.Fatalf("unexpected action plans: %+v", store.actionPlans)
	}

	srv.processAILogFixerCompletedAction(context.Background(), jobID, heartbeatCompletedAction{
		Action: JobTypeAILogFixerApply,
		Status: "succeeded",
		Metadata: map[string]any{
			"attempt": map[string]any{"command": "ailogfixer apply"},
			"receipt": map[string]any{
				"status": "succeeded",
				"diff":   "service restarted",
			},
		},
	})
	receipts := store.actionReceipts[planID]
	if len(receipts) != 1 {
		t.Fatalf("expected one unified receipt, got %d", len(receipts))
	}
	if receipts[0].State != storage.ActionPlanStateSucceeded || receipts[0].JobID.UUID != jobID {
		t.Fatalf("unexpected unified receipt: %+v", receipts[0])
	}
	if got := store.actionPlans[planID].State; got != storage.ActionPlanStateSucceeded {
		t.Fatalf("action plan state = %s, want succeeded", got)
	}
}
