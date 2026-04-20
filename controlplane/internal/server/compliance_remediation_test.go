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
	mu      sync.Mutex
	scripts map[string]*storage.RemediationScript // keyed by ruleID
}

func (r *remediationTestStore) GetRemediationScript(_ context.Context, ruleID, platform string) (*storage.RemediationScript, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if script, ok := r.scripts[ruleID]; ok {
		return script, nil
	}
	return nil, nil
}

// trackingQueue records enqueued tasks for test inspection.
type trackingQueue struct {
	mu    sync.Mutex
	tasks []worker.Task
}

func (q *trackingQueue) Enqueue(task worker.Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, task)
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
	result := compliance.Result{
		RuleID:    "rule-vuln-1",
		Passed:    false,
		Severity:  "critical",
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

	result := compliance.Result{
		RuleID:   ruleID,
		Passed:   false,
		Severity: "high",
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
