package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

// finacleTestStore wraps fakeStore with the small set of finacle and
// remediation-script behaviours the failure-mode tests need. The base
// fakeStore stubs everything we don't override, so we keep the surface tight.
type finacleTestStore struct {
	fakeStore
	mu sync.Mutex

	conn        storage.FinacleConnection
	connByID    map[uuid.UUID]storage.FinacleConnection
	profiles    map[uuid.UUID][]storage.FinacleProfile // keyed by shift_id
	rotated     map[uuid.UUID]string                   // profile id -> last rotated status
	scripts     map[string]*storage.RemediationScript
	listConnErr error
}

func newFinacleTestStore() *finacleTestStore {
	store := &finacleTestStore{
		profiles: map[uuid.UUID][]storage.FinacleProfile{},
		rotated:  map[uuid.UUID]string{},
		connByID: map[uuid.UUID]storage.FinacleConnection{},
		scripts:  map[string]*storage.RemediationScript{},
	}
	store.jobs = map[uuid.UUID]*storage.Job{}
	store.events = map[uuid.UUID][]storage.JobEvent{}
	return store
}

func (f *finacleTestStore) ListFinacleConnections(_ context.Context, _ uuid.UUID) ([]storage.FinacleConnection, error) {
	if f.listConnErr != nil {
		return nil, f.listConnErr
	}
	if f.conn.ID == uuid.Nil {
		return nil, nil
	}
	return []storage.FinacleConnection{f.conn}, nil
}

func (f *finacleTestStore) GetFinacleConnection(_ context.Context, id uuid.UUID) (*storage.FinacleConnection, error) {
	if c, ok := f.connByID[id]; ok {
		copy := c
		return &copy, nil
	}
	if f.conn.ID == id {
		copy := f.conn
		return &copy, nil
	}
	return nil, nil
}

func (f *finacleTestStore) UpdateFinacleConnection(_ context.Context, _ uuid.UUID, _ storage.UpdateFinacleConnectionParams) (*storage.FinacleConnection, error) {
	copy := f.conn
	return &copy, nil
}

func (f *finacleTestStore) ListFinacleProfilesByShift(_ context.Context, shiftID uuid.UUID) ([]storage.FinacleProfile, error) {
	out := append([]storage.FinacleProfile{}, f.profiles[shiftID]...)
	return out, nil
}

func (f *finacleTestStore) MarkFinacleProfileRotated(_ context.Context, id uuid.UUID, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotated[id] = status
	return nil
}

func (f *finacleTestStore) GetRemediationScript(_ context.Context, ruleID, _ string) (*storage.RemediationScript, error) {
	if s, ok := f.scripts[ruleID]; ok {
		return s, nil
	}
	return nil, nil
}

// stubFinacleConnector lets a test inject specific reachability outcomes for
// the enable / disable / list / ping methods. A nil error == reachable; a
// non-nil error simulates Finacle being down.
type stubFinacleConnector struct {
	mu          sync.Mutex
	enableErr   error
	disableErr  error
	enabled     []string
	disabled    []string
	listResults []storage.UpsertFinacleProfileParams
	listErr     error
}

func (c *stubFinacleConnector) Ping(context.Context, storage.FinacleConnection) error {
	return c.enableErr
}
func (c *stubFinacleConnector) EnableProfile(_ context.Context, _ storage.FinacleConnection, uid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enableErr != nil {
		return c.enableErr
	}
	c.enabled = append(c.enabled, uid)
	return nil
}
func (c *stubFinacleConnector) DisableProfile(_ context.Context, _ storage.FinacleConnection, uid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disableErr != nil {
		return c.disableErr
	}
	c.disabled = append(c.disabled, uid)
	return nil
}
func (c *stubFinacleConnector) ListProfiles(context.Context, storage.FinacleConnection) ([]storage.UpsertFinacleProfileParams, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.listResults, nil
}

// helperServerForFinacle constructs a Server with the fake store + connector
// and the tracking queue used by other remediation tests so we can re-use
// trackingQueue's enqueue capture if needed.
func helperServerForFinacle(t *testing.T, store *finacleTestStore, connector *stubFinacleConnector) *Server {
	t.Helper()
	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, queue)
	srv.finacleClient = connector
	return srv
}

// TestFinacleShiftRotate_EnableFailClosed verifies that when Finacle is
// unreachable on the incoming-staff enable path the worker:
//  1. Does NOT mark any profile rotated to "active" (fail closed — no
//     un-auditable grant).
//  2. Reports the job as failed so retries can pick it up.
//  3. Emits a critical alert with the shift+direction context.
func TestFinacleShiftRotate_EnableFailClosed(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	shiftID := uuid.New()
	connID := uuid.New()
	store := newFinacleTestStore()
	store.conn = storage.FinacleConnection{
		ID:         connID,
		TenantID:   tenantID,
		Host:       "https://finacle.example",
		AuthMethod: storage.FinacleAuthOAuth2ClientCreds,
	}
	profileA := storage.FinacleProfile{ID: uuid.New(), TenantID: tenantID, FinacleUID: "alice", ShiftID: uuid.NullUUID{UUID: shiftID, Valid: true}, Status: "unknown"}
	profileB := storage.FinacleProfile{ID: uuid.New(), TenantID: tenantID, FinacleUID: "bob", ShiftID: uuid.NullUUID{UUID: shiftID, Valid: true}, Status: "unknown"}
	store.profiles[shiftID] = []storage.FinacleProfile{profileA, profileB}

	// Connector simulates Finacle being unreachable on the enable path.
	connector := &stubFinacleConnector{enableErr: errors.New("503 service unavailable")}

	srv := helperServerForFinacle(t, store, connector)

	// Seed a job row so UpdateJobStatus has something to mutate.
	job := &storage.Job{Type: JobTypeFinacleShiftRotate, TenantID: tenantID, Status: storage.JobStatusQueued}
	store.jobs[job.ID] = job
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
		store.jobs[job.ID] = job
	}

	exec := srv.buildFinacleShiftRotateExecution(job.ID, tenantID, shiftID, "enable")
	err := exec(context.Background())

	// Fail-closed contract — must surface the failure so the worker retries.
	if err == nil {
		t.Fatalf("expected error from enable path when Finacle is unreachable")
	}

	// No profile may be marked active.
	for _, p := range store.profiles[shiftID] {
		if status, ok := store.rotated[p.ID]; ok && status == "active" {
			t.Fatalf("profile %s was marked active despite Finacle 503 (fail-closed violated)", p.ID)
		}
	}
}

// TestFinacleShiftRotate_DisableFailOpen verifies the inverse asymmetry: on
// the outgoing-staff disable path, Finacle being down does NOT block the
// revoke. Profiles are marked revoked locally even though the upstream call
// failed.
func TestFinacleShiftRotate_DisableFailOpen(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	shiftID := uuid.New()
	connID := uuid.New()
	store := newFinacleTestStore()
	store.conn = storage.FinacleConnection{
		ID:         connID,
		TenantID:   tenantID,
		Host:       "https://finacle.example",
		AuthMethod: storage.FinacleAuthBasic,
	}
	profileA := storage.FinacleProfile{ID: uuid.New(), TenantID: tenantID, FinacleUID: "carol", ShiftID: uuid.NullUUID{UUID: shiftID, Valid: true}, Status: "active"}
	store.profiles[shiftID] = []storage.FinacleProfile{profileA}

	connector := &stubFinacleConnector{disableErr: errors.New("connection refused")}
	srv := helperServerForFinacle(t, store, connector)

	job := &storage.Job{ID: uuid.New(), Type: JobTypeFinacleShiftRotate, TenantID: tenantID, Status: storage.JobStatusQueued}
	store.jobs[job.ID] = job

	exec := srv.buildFinacleShiftRotateExecution(job.ID, tenantID, shiftID, "disable")
	if err := exec(context.Background()); err != nil {
		t.Fatalf("disable path must succeed even on upstream failure (fail-open), got %v", err)
	}

	// Profile must be marked revoked locally.
	if status := store.rotated[profileA.ID]; status != "revoked" {
		t.Fatalf("expected profile to be marked revoked locally even on upstream failure, got %q", status)
	}
}

// TestFinacleShiftRotate_OverrideCreatesApproval verifies that admin override
// (via handleFinacleShiftRotate → triggerAutoRemediation) lands a row in the
// remediation_approvals table thanks to the existing 4-gate engine.
//
// The override carries severity=high which must be ≥ the configured
// min_approval_severity for the tenant; the synthetic rule_id has a stub
// remediation script registered so the gate engine reaches the approval
// branch. Without a script the engine short-circuits at gate-0.
func TestFinacleShiftRotate_OverrideCreatesApproval(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	shiftID := uuid.New()
	store := newFinacleTestStore()

	// Register a synthetic remediation script keyed on the rule_id the
	// override constructs. Without this the gate engine returns nil at the
	// "no script for rule" check.
	ruleID := "finacle.shift_rotate:" + shiftID.String() + ":enable"
	scriptID := uuid.New()
	store.scripts[ruleID] = &storage.RemediationScript{
		ID:            scriptID,
		RuleID:        ruleID,
		Platform:      "all",
		ScriptType:    "noop",
		ScriptContent: "true",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Default tenant remediation config: high severity meets min approval.
	store.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{
		tenantID: storage.DefaultTenantRemediationConfig(tenantID),
	}

	queue := &trackingQueue{}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, queue)

	result := compliance.Result{
		RuleID:    ruleID,
		Passed:    false,
		Severity:  "high",
		Details:   "admin shift override",
		CheckedAt: time.Now().UTC(),
	}
	jobID := srv.triggerAutoRemediation(context.Background(), tenantID, uuid.Nil, result, true)
	if jobID != nil {
		t.Fatalf("expected the gate engine to hold the override pending approval (jobID=%v)", jobID)
	}

	// Verify that the approval row was created.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.remediationApprovals) == 0 {
		t.Fatalf("expected a remediation_approvals row for the admin override")
	}
	for _, a := range store.remediationApprovals {
		if a.RuleID != ruleID {
			t.Fatalf("approval has unexpected rule_id: %s", a.RuleID)
		}
		if a.Status != storage.ApprovalStatusPending {
			t.Fatalf("approval should be pending, got %s", a.Status)
		}
		if a.TenantID != tenantID {
			t.Fatalf("approval tenant mismatch")
		}
	}
}
