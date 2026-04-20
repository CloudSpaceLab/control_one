package server

import (
	"context"
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
