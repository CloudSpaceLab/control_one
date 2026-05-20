package server

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

// remediationTestStore wraps fakeStore with remediation script lookup support.
type remediationTestStore struct {
	fakeStore
	mu          sync.Mutex
	scripts     map[string]*storage.RemediationScript    // keyed by ruleID
	scriptsByID map[uuid.UUID]*storage.RemediationScript // keyed by script ID
}

func (r *remediationTestStore) GetRemediationScript(_ context.Context, ruleID, platform string) (*storage.RemediationScript, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if script, ok := r.scripts[ruleID]; ok {
		return script, nil
	}
	return nil, nil
}

func (r *remediationTestStore) GetRemediationScriptByID(_ context.Context, id uuid.UUID) (*storage.RemediationScript, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if script, ok := r.scriptsByID[id]; ok {
		return script, nil
	}
	for _, script := range r.scripts {
		if script != nil && script.ID == id {
			return script, nil
		}
	}
	return nil, nil
}

// trackingQueue records enqueued tasks for test inspection.
type trackingQueue struct {
	mu         sync.Mutex
	tasks      []worker.Task
	processAts []time.Time // processAt per enqueued task; zero when immediate.
}

func (q *trackingQueue) Enqueue(task worker.Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, task)
	q.processAts = append(q.processAts, time.Time{})
	return nil
}

func (q *trackingQueue) EnqueueAt(task worker.Task, processAt time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, task)
	q.processAts = append(q.processAts, processAt)
	return nil
}

func TestTriggerAutoRemediation_Disabled(t *testing.T) {
	t.Parallel()

	store := &remediationTestStore{}
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, queue)

	result := compliance.Result{
		RuleID:   "rule-1",
		Passed:   false,
		Severity: "high",
	}

	jobID := srv.triggerAutoRemediation(context.Background(), uuid.New(), uuid.New(), result, false)
	if jobID != nil {
		t.Fatalf("expected nil jobID when auto-remediation is disabled, got %s", jobID)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 0 {
		t.Fatalf("expected 0 enqueued tasks, got %d", len(queue.tasks))
	}
}

func TestTriggerAutoRemediation_PassedResult(t *testing.T) {
	t.Parallel()

	store := &remediationTestStore{
		scripts: map[string]*storage.RemediationScript{
			"rule-1": {
				ID:            uuid.New(),
				RuleID:        "rule-1",
				Platform:      "all",
				ScriptType:    "bash",
				ScriptContent: "echo fix",
				Enabled:       true,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			},
		},
	}
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, queue)

	result := compliance.Result{
		RuleID:   "rule-1",
		Passed:   true, // passed -> no remediation needed
		Severity: "high",
	}

	jobID := srv.triggerAutoRemediation(context.Background(), uuid.New(), uuid.New(), result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID for passed result, got %s", jobID)
	}
}

func TestTriggerAutoRemediation_NoScript(t *testing.T) {
	t.Parallel()

	store := &remediationTestStore{
		scripts: map[string]*storage.RemediationScript{}, // no scripts
	}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, queue)

	result := compliance.Result{
		RuleID:   "rule-missing",
		Passed:   false,
		Severity: "high",
	}

	jobID := srv.triggerAutoRemediation(context.Background(), uuid.New(), uuid.New(), result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID when no script exists, got %s", jobID)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 0 {
		t.Fatalf("expected 0 enqueued tasks, got %d", len(queue.tasks))
	}
}

func TestTriggerAutoRemediation_CreatesJob(t *testing.T) {
	t.Parallel()

	scriptID := uuid.New()
	store := &remediationTestStore{
		scripts: map[string]*storage.RemediationScript{
			"rule-vuln-1": {
				ID:            scriptID,
				RuleID:        "rule-vuln-1",
				Platform:      "linux",
				ScriptType:    "bash",
				ScriptContent: "#!/bin/bash\necho 'remediate'",
				Enabled:       true,
				Metadata:      map[string]any{},
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			},
		},
	}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP:   config.HTTPConfig{Address: ":0"},
		Worker: config.WorkerConfig{},
	}, store, queue)

	tenantID := uuid.New()
	nodeID := uuid.New()
	// Severity=medium keeps the result below the default high approval gate
	// so the dispatch path should reach the worker directly.
	result := compliance.Result{
		RuleID:    "rule-vuln-1",
		Passed:    false,
		Severity:  "medium",
		Details:   "vulnerable package detected",
		CheckedAt: time.Now(),
	}

	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID == nil {
		t.Fatalf("expected non-nil jobID")
	}

	// Verify job was created in the store.
	job, ok := store.jobs[*jobID]
	if !ok {
		t.Fatalf("expected job %s to exist in store", jobID)
	}
	if job.Type != "remediation.execute" {
		t.Fatalf("expected job type remediation.execute, got %s", job.Type)
	}
	if job.TenantID != tenantID {
		t.Fatalf("expected tenant %s, got %s", tenantID, job.TenantID)
	}

	// Verify task was enqueued.
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(queue.tasks))
	}
	if queue.tasks[0].MaxAttempts != 3 {
		t.Errorf("expected 3 max attempts, got %d", queue.tasks[0].MaxAttempts)
	}
}

func TestTriggerAutoRemediation_DisabledScript(t *testing.T) {
	t.Parallel()

	store := &remediationTestStore{
		scripts: map[string]*storage.RemediationScript{
			"rule-disabled": {
				ID:            uuid.New(),
				RuleID:        "rule-disabled",
				Platform:      "all",
				ScriptType:    "bash",
				ScriptContent: "echo fix",
				Enabled:       false, // script disabled
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			},
		},
	}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, queue)

	result := compliance.Result{
		RuleID:   "rule-disabled",
		Passed:   false,
		Severity: "high",
	}

	jobID := srv.triggerAutoRemediation(context.Background(), uuid.New(), uuid.New(), result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID for disabled script, got %s", jobID)
	}
}

// Compile-time check that remediationTestStore satisfies the Store interface.
var _ Store = (*remediationTestStore)(nil)

// newTestStoreWithScript returns a ready-to-use remediationTestStore seeded
// with a single rule-id -> script mapping.
func newTestStoreWithScript(ruleID string, script *storage.RemediationScript) *remediationTestStore {
	store := &remediationTestStore{
		scripts: map[string]*storage.RemediationScript{
			ruleID: script,
		},
	}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)
	store.complianceResults = make(map[uuid.UUID][]storage.ComplianceResult)
	return store
}

func TestTriggerAutoRemediation_LeaseHeldSkipsJob(t *testing.T) {
	t.Parallel()

	ruleID := "rule-lease"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := newTestStoreWithScript(ruleID, script)
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	tenantID := uuid.New()
	nodeID := uuid.New()

	// Pre-populate a lease so the trigger has to skip.
	_, err := store.AcquireRemediationLease(context.Background(), tenantID, nodeID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatalf("pre-seed lease: %v", err)
	}

	result := compliance.Result{
		RuleID:   ruleID,
		Passed:   false,
		Severity: "high",
	}

	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID when lease is held, got %s", jobID)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 0 {
		t.Fatalf("expected 0 enqueued tasks when lease held, got %d", len(queue.tasks))
	}
}

func TestTriggerAutoRemediation_TenantCapDeferred(t *testing.T) {
	t.Parallel()

	ruleID := "rule-cap"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := newTestStoreWithScript(ruleID, script)
	queue := &trackingQueue{}

	// Cap at 1 so the first existing lease blocks the next.
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 1, LeaseTTL: time.Minute},
	}, store, queue)

	tenantID := uuid.New()
	otherNode := uuid.New()
	_, err := store.AcquireRemediationLease(context.Background(), tenantID, otherNode, uuid.New(), time.Minute)
	if err != nil {
		t.Fatalf("pre-seed lease: %v", err)
	}

	result := compliance.Result{
		RuleID:   ruleID,
		Passed:   false,
		Severity: "high",
	}

	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, uuid.New(), result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID when tenant cap reached, got %s", jobID)
	}
}

func TestTriggerAutoRemediation_AcquiresLeaseOnCreate(t *testing.T) {
	t.Parallel()

	ruleID := "rule-acq"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := newTestStoreWithScript(ruleID, script)
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	tenantID := uuid.New()
	nodeID := uuid.New()

	// Medium severity stays below the default high approval gate so the
	// dispatch path acquires a lease as part of the worker enqueue.
	result := compliance.Result{
		RuleID:   ruleID,
		Passed:   false,
		Severity: "medium",
	}

	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID == nil {
		t.Fatalf("expected job to be created")
	}

	count, err := store.CountTenantLeases(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("CountTenantLeases: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 in-flight lease after trigger, got %d", count)
	}
}

func TestEnqueueComplianceVerify_PersistsVerificationJobID(t *testing.T) {
	t.Parallel()

	ruleID := "rule-verify"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := newTestStoreWithScript(ruleID, script)
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	tenantID := uuid.New()
	nodeID := uuid.New()

	// Pre-seed a compliance result the verify step should link to.
	resultID := uuid.New()
	store.complianceResults[uuid.New()] = []storage.ComplianceResult{{
		ID:        resultID,
		TenantID:  tenantID,
		NodeID:    nodeID,
		RuleID:    ruleID,
		Passed:    false,
		CreatedAt: time.Now(),
	}}

	triggeringJob := uuid.New()
	if err := srv.enqueueComplianceVerify(context.Background(), tenantID, nodeID, ruleID, triggeringJob, script); err != nil {
		t.Fatalf("enqueueComplianceVerify: %v", err)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(queue.tasks))
	}

	got, err := store.GetLatestComplianceResultForRule(context.Background(), nodeID, ruleID)
	if err != nil {
		t.Fatalf("GetLatestComplianceResultForRule: %v", err)
	}
	if got == nil || got.VerificationJobID == nil {
		t.Fatalf("expected verification_job_id populated, got %+v", got)
	}

	// Jobs map should now contain exactly the verify job.
	var verifyJob *storage.Job
	for _, j := range store.jobs {
		if j.Type == JobTypeComplianceVerify {
			verifyJob = j
			break
		}
	}
	if verifyJob == nil {
		t.Fatalf("expected compliance.verify job created, got jobs: %v", store.jobs)
	}
	if *got.VerificationJobID != verifyJob.ID {
		t.Fatalf("expected VerificationJobID=%s, got %s", verifyJob.ID, *got.VerificationJobID)
	}
}

func TestEnqueueRemediationRollback_UsesRollbackContent(t *testing.T) {
	t.Parallel()

	ruleID := "rule-rollback"
	script := &storage.RemediationScript{
		ID:              uuid.New(),
		RuleID:          ruleID,
		Platform:        "all",
		ScriptType:      "bash",
		ScriptContent:   "echo apply",
		RollbackContent: sql.NullString{String: "echo undo", Valid: true},
		Enabled:         true,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	store := newTestStoreWithScript(ruleID, script)
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	tenantID := uuid.New()
	nodeID := uuid.New()

	// Pre-seed a compliance result the rollback step should link to.
	resultID := uuid.New()
	store.complianceResults[uuid.New()] = []storage.ComplianceResult{{
		ID:        resultID,
		TenantID:  tenantID,
		NodeID:    nodeID,
		RuleID:    ruleID,
		Passed:    false,
		CreatedAt: time.Now(),
	}}

	if err := srv.enqueueRemediationRollback(context.Background(), tenantID, nodeID, ruleID, uuid.New(), script); err != nil {
		t.Fatalf("enqueueRemediationRollback: %v", err)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(queue.tasks))
	}

	var rollbackJob *storage.Job
	for _, j := range store.jobs {
		if j.Type == JobTypeRemediationRollback {
			rollbackJob = j
			break
		}
	}
	if rollbackJob == nil {
		t.Fatalf("expected remediation.rollback job created")
	}

	// Result should have its rollback_job_id set.
	got, err := store.GetLatestComplianceResultForRule(context.Background(), nodeID, ruleID)
	if err != nil {
		t.Fatalf("GetLatestComplianceResultForRule: %v", err)
	}
	if got == nil || got.RollbackJobID == nil {
		t.Fatalf("expected rollback_job_id populated, got %+v", got)
	}
	if *got.RollbackJobID != rollbackJob.ID {
		t.Fatalf("expected RollbackJobID=%s, got %s", rollbackJob.ID, *got.RollbackJobID)
	}
}

func TestEnqueueRemediationRollback_NoRollbackContentFails(t *testing.T) {
	t.Parallel()

	ruleID := "rule-no-rollback"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo apply",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	store := newTestStoreWithScript(ruleID, script)
	queue := &trackingQueue{}

	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	err := srv.enqueueRemediationRollback(context.Background(), uuid.New(), uuid.New(), ruleID, uuid.New(), script)
	if err == nil {
		t.Fatalf("expected error when script has no rollback content")
	}
	if !errors.Is(err, err) { // sanity reference
		t.Fatalf("unexpected error shape: %v", err)
	}
}

func TestVerifyResultsPassed_MissingRuleTreatedAsPassed(t *testing.T) {
	t.Parallel()
	if !verifyResultsPassed([]compliance.Result{}, "any") {
		t.Fatalf("empty results should return passed=true (rule deleted semantics)")
	}
	if verifyResultsPassed([]compliance.Result{{RuleID: "other", Passed: true}}, "any") != true {
		t.Fatalf("unrelated result should return passed=true")
	}
	if verifyResultsPassed([]compliance.Result{{RuleID: "x", Passed: false}}, "x") != false {
		t.Fatalf("matching failed result should return false")
	}
	if verifyResultsPassed([]compliance.Result{{RuleID: "x", Passed: true}}, "x") != true {
		t.Fatalf("matching passed result should return true")
	}
}

// Keep sync unused import silent in case we remove tests later.
var _ = sync.Mutex{}

// ---------------------------------------------------------------------------
// Sprint 2 safety-gate tests.
// ---------------------------------------------------------------------------

// TestTriggerAutoRemediation_OptOutLabelSkips verifies the first gate:
// nodes labelled `remediation=manual-only` must not enqueue.
func TestTriggerAutoRemediation_OptOutLabelSkips(t *testing.T) {
	t.Parallel()

	ruleID := "rule-manual-only"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
	}
	store := newTestStoreWithScript(ruleID, script)
	tenantID := uuid.New()
	nodeID := uuid.New()
	store.nodes = append(store.nodes, storage.Node{
		ID:       nodeID,
		TenantID: tenantID,
		Hostname: "n-manual",
		Labels: map[string]any{
			"remediation": "manual-only",
		},
	})

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	result := compliance.Result{RuleID: ruleID, Passed: false, Severity: "medium"}
	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID when node is manual-only, got %s", jobID)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 0 {
		t.Fatalf("expected 0 enqueued tasks, got %d", len(queue.tasks))
	}
}

func TestTriggerAutoRemediation_IsolationSkips(t *testing.T) {
	t.Parallel()

	ruleID := "rule-isolation"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
	}
	store := newTestStoreWithScript(ruleID, script)
	tenantID := uuid.New()
	nodeID := uuid.New()
	store.nodes = append(store.nodes, storage.Node{
		ID:       nodeID,
		TenantID: tenantID,
		Hostname: "n-airgap",
		Labels: map[string]any{
			isolationModeLabel: isolationModeAirgapped,
		},
	})

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	result := compliance.Result{RuleID: ruleID, Passed: false, Severity: "medium"}
	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID when node is airgapped, got %s", jobID)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 0 {
		t.Fatalf("expected 0 enqueued tasks, got %d", len(queue.tasks))
	}
}

// TestTriggerAutoRemediation_ChangeWindowDeferred verifies gate 2: outside the
// tenant's change windows a non-critical result is deferred to the next open.
func TestTriggerAutoRemediation_ChangeWindowDeferred(t *testing.T) {
	t.Parallel()

	ruleID := "rule-change-window"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
	}
	store := newTestStoreWithScript(ruleID, script)

	tenantID := uuid.New()
	// Change window only at hour 0-6 UTC every day. Any wall-clock time outside
	// that range triggers deferral. Running locally this will be outside that
	// range for most of the day; if the test runs in [0,6) UTC the deferral
	// won't happen, so we compute the expected behaviour at runtime.
	now := time.Now().UTC()
	storeCfg := storage.TenantRemediationConfig{
		TenantID:                 tenantID,
		MinApprovalSeverity:      "high",
		ChangeWindows:            []storage.ChangeWindow{{StartHour: 0, EndHour: 6}},
		CriticalOverride:         true,
		CircuitBreakerWindowMin:  15,
		CircuitBreakerFailPct:    30,
		CircuitBreakerMinSamples: 5,
	}
	if _, err := store.UpsertTenantRemediationConfig(context.Background(), storeCfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	nodeID := uuid.New()
	store.nodes = append(store.nodes, storage.Node{ID: nodeID, TenantID: tenantID, Hostname: "n"})

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	result := compliance.Result{RuleID: ruleID, Passed: false, Severity: "medium"}
	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID == nil {
		t.Fatalf("expected jobID (deferred but still enqueued)")
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(queue.tasks))
	}
	processAt := queue.processAts[0]
	insideWindow := storage.IsInsideChangeWindow(storeCfg.ChangeWindows, now)
	if insideWindow {
		// Currently inside the window — dispatcher should enqueue immediately.
		if !processAt.IsZero() {
			t.Fatalf("expected immediate enqueue inside window, got processAt=%s", processAt)
		}
	} else {
		if processAt.IsZero() || !processAt.After(now) {
			t.Fatalf("expected processAt in future when deferred, got %s (now=%s)", processAt, now)
		}
	}
}

// TestIsInsideChangeWindowAndNextOpen directly exercises the change-window
// helpers so the critical-override logic is unit-covered without having to
// juggle approval and breaker gates at the dispatch level.
func TestIsInsideChangeWindowAndNextOpen(t *testing.T) {
	t.Parallel()

	// Windows open daily between 0:00 and 6:00 UTC.
	windows := []storage.ChangeWindow{{StartHour: 0, EndHour: 6}}

	inside := time.Date(2026, 4, 20, 2, 0, 0, 0, time.UTC)
	outside := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	if !storage.IsInsideChangeWindow(windows, inside) {
		t.Fatalf("expected 02:00 UTC to be inside [0,6) window")
	}
	if storage.IsInsideChangeWindow(windows, outside) {
		t.Fatalf("expected 10:00 UTC to be outside [0,6) window")
	}

	nextOpen := storage.NextChangeWindowStart(windows, outside)
	if !nextOpen.After(outside) {
		t.Fatalf("expected next open after outside time; got %s", nextOpen)
	}
	if nextOpen.Hour() != 0 || nextOpen.Minute() != 0 {
		t.Fatalf("expected next open at 00:00, got %s", nextOpen)
	}
}

// TestSeverityAtLeastAndCriticalOverride proves the tiered severity ranking
// and verifies that critical_override only kicks in for severity=="critical".
func TestSeverityAtLeastAndCriticalOverride(t *testing.T) {
	t.Parallel()

	cases := []struct {
		actual, min string
		want        bool
	}{
		{"low", "low", true},
		{"low", "medium", false},
		{"medium", "high", false},
		{"high", "high", true},
		{"high", "critical", false},
		{"critical", "critical", true},
		{"critical", "high", true},
		{"", "high", false},
		{"banana", "medium", false},
	}
	for _, c := range cases {
		if got := storage.SeverityAtLeast(c.actual, c.min); got != c.want {
			t.Errorf("SeverityAtLeast(%q,%q) = %v; want %v", c.actual, c.min, got, c.want)
		}
	}
}

// TestTriggerAutoRemediation_CircuitBreakerTripped verifies gate 3: an unacked
// breaker short-circuits the dispatch path.
func TestTriggerAutoRemediation_CircuitBreakerTripped(t *testing.T) {
	t.Parallel()

	ruleID := "rule-breaker"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
	}
	store := newTestStoreWithScript(ruleID, script)
	tenantID := uuid.New()
	nodeID := uuid.New()
	store.nodes = append(store.nodes, storage.Node{ID: nodeID, TenantID: tenantID, Hostname: "n"})

	if _, err := store.TripCircuitBreaker(context.Background(), tenantID, ruleID, "seeded"); err != nil {
		t.Fatalf("trip breaker: %v", err)
	}

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	result := compliance.Result{RuleID: ruleID, Passed: false, Severity: "medium"}
	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID when breaker tripped, got %s", jobID)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 0 {
		t.Fatalf("expected 0 enqueued tasks, got %d", len(queue.tasks))
	}
}

// TestTriggerAutoRemediation_CircuitBreakerAckedAllows verifies that an acked
// breaker does NOT short-circuit.
func TestTriggerAutoRemediation_CircuitBreakerAckedAllows(t *testing.T) {
	t.Parallel()

	ruleID := "rule-breaker-acked"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
	}
	store := newTestStoreWithScript(ruleID, script)
	tenantID := uuid.New()
	nodeID := uuid.New()
	store.nodes = append(store.nodes, storage.Node{ID: nodeID, TenantID: tenantID, Hostname: "n"})

	if _, err := store.TripCircuitBreaker(context.Background(), tenantID, ruleID, "seeded"); err != nil {
		t.Fatalf("trip breaker: %v", err)
	}
	if _, err := store.AckCircuitBreaker(context.Background(), tenantID, ruleID, uuid.New()); err != nil {
		t.Fatalf("ack breaker: %v", err)
	}

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	result := compliance.Result{RuleID: ruleID, Passed: false, Severity: "medium"}
	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID == nil {
		t.Fatalf("expected jobID after breaker acked, got nil")
	}
}

// TestTriggerAutoRemediation_CircuitBreakerFailRateTrips verifies that a
// seeded fail-rate above threshold trips the breaker fresh.
func TestTriggerAutoRemediation_CircuitBreakerFailRateTrips(t *testing.T) {
	t.Parallel()

	ruleID := "rule-fail-rate"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
	}
	store := newTestStoreWithScript(ruleID, script)
	tenantID := uuid.New()
	nodeID := uuid.New()
	store.nodes = append(store.nodes, storage.Node{ID: nodeID, TenantID: tenantID, Hostname: "n"})

	store.remediationFailRates = map[string]storage.RemediationFailRate{
		fakeBreakerKey(tenantID, ruleID): {
			Samples: 10,
			Failed:  5,
			Pct:     50,
		},
	}

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	result := compliance.Result{RuleID: ruleID, Passed: false, Severity: "medium"}
	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID when fail rate exceeds threshold, got %s", jobID)
	}

	// Breaker row must now exist with acked_at=NULL.
	state, err := store.GetCircuitBreakerState(context.Background(), tenantID, ruleID)
	if err != nil {
		t.Fatalf("GetCircuitBreakerState: %v", err)
	}
	if state == nil {
		t.Fatalf("expected breaker row to be tripped")
	}
	if state.AckedAt != nil {
		t.Fatalf("expected breaker unacked, got acked_at=%v", state.AckedAt)
	}
}

// TestTriggerAutoRemediation_ApprovalGate verifies gate 4: severity at/above
// threshold creates a pending approval row and does NOT enqueue.
func TestTriggerAutoRemediation_ApprovalGate(t *testing.T) {
	t.Parallel()

	ruleID := "rule-approval"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
	}
	store := newTestStoreWithScript(ruleID, script)
	tenantID := uuid.New()
	nodeID := uuid.New()
	store.nodes = append(store.nodes, storage.Node{ID: nodeID, TenantID: tenantID, Hostname: "n"})

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	// Critical >= high default, so approval gate should fire.
	result := compliance.Result{RuleID: ruleID, Passed: false, Severity: "critical"}
	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, nodeID, result, true)
	if jobID != nil {
		t.Fatalf("expected nil jobID when approval required, got %s", jobID)
	}

	approvals, _, err := store.ListRemediationApprovals(context.Background(), storage.ListRemediationApprovalsFilter{
		TenantID: tenantID,
		Status:   storage.ApprovalStatusPending,
	}, 0, 0)
	if err != nil {
		t.Fatalf("list approvals: %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(approvals))
	}
	if approvals[0].RuleID != ruleID {
		t.Fatalf("expected rule_id %s, got %s", ruleID, approvals[0].RuleID)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 0 {
		t.Fatalf("expected 0 enqueued tasks while awaiting approval, got %d", len(queue.tasks))
	}
}

// TestDispatchRemediationTask_RedispatchAfterApproval verifies the approval
// handler can re-dispatch via the helper, bypassing the severity gate.
func TestDispatchRemediationTask_RedispatchAfterApproval(t *testing.T) {
	t.Parallel()

	ruleID := "rule-approval-dispatch"
	script := &storage.RemediationScript{
		ID:            uuid.New(),
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
		Enabled:       true,
	}
	store := newTestStoreWithScript(ruleID, script)
	tenantID := uuid.New()
	nodeID := uuid.New()

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:        config.HTTPConfig{Address: ":0"},
		Remediation: config.RemediationConfig{MaxConcurrentPerTenant: 10, LeaseTTL: time.Minute},
	}, store, queue)

	jobID := srv.dispatchRemediationTask(context.Background(), dispatchRemediationTaskParams{
		TenantID:  tenantID,
		NodeID:    nodeID,
		RuleID:    ruleID,
		Script:    script,
		EnqueueAt: time.Time{}, // immediate
	})
	if jobID == nil {
		t.Fatalf("expected dispatch to enqueue after approval")
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.tasks) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(queue.tasks))
	}
}

// TestReapExpiredRemediationApprovals verifies the reaper flips expired rows.
func TestReapExpiredRemediationApprovals(t *testing.T) {
	t.Parallel()

	store := &remediationTestStore{scripts: map[string]*storage.RemediationScript{}}
	store.jobs = make(map[uuid.UUID]*storage.Job)
	store.events = make(map[uuid.UUID][]storage.JobEvent)

	tenantID := uuid.New()
	nodeID := uuid.New()
	scriptID := uuid.New()

	// Seed one expired and one fresh pending approval.
	ctx := context.Background()
	expired, err := store.CreateRemediationApproval(ctx, storage.CreateRemediationApprovalParams{
		TenantID:    tenantID,
		NodeID:      nodeID,
		RuleID:      "rule-expired",
		ScriptID:    scriptID,
		Severity:    "high",
		TaskPayload: []byte(`{}`),
		ExpiresAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("seed expired approval: %v", err)
	}
	fresh, err := store.CreateRemediationApproval(ctx, storage.CreateRemediationApprovalParams{
		TenantID:    tenantID,
		NodeID:      nodeID,
		RuleID:      "rule-fresh",
		ScriptID:    scriptID,
		Severity:    "high",
		TaskPayload: []byte(`{}`),
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("seed fresh approval: %v", err)
	}

	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &trackingQueue{})
	n, err := srv.reapExpiredRemediationApprovals(ctx)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 approval expired, got %d", n)
	}

	got, err := store.GetRemediationApproval(ctx, expired.ID)
	if err != nil {
		t.Fatalf("get expired: %v", err)
	}
	if got.Status != storage.ApprovalStatusExpired {
		t.Fatalf("expected expired status, got %s", got.Status)
	}

	got, err = store.GetRemediationApproval(ctx, fresh.ID)
	if err != nil {
		t.Fatalf("get fresh: %v", err)
	}
	if got.Status != storage.ApprovalStatusPending {
		t.Fatalf("expected fresh still pending, got %s", got.Status)
	}
}
