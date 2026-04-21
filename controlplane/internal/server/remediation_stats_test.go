package server

import (
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

// operatorPrincipal returns a *auth.Principal shaped like a human operator
// user authenticated via OIDC. Stats endpoints require viewer+; operator is a
// safe baseline across read + approve actions.
func operatorPrincipal() *auth.Principal {
	return &auth.Principal{
		Type:  "user",
		Name:  "op@example.com",
		Roles: []string{"operator"},
	}
}

// seedRemediationJob inserts a remediation job of the given type with the
// given rule_id/node_id into the fakeStore, stamped at `stamp`. Returns the
// created job ID for chaining.
func seedRemediationJob(t *testing.T, store *fakeStore, jobType string, status storage.JobStatus, tenantID, nodeID uuid.UUID, ruleID string, stamp time.Time) uuid.UUID {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"rule_id": ruleID,
		"node_id": nodeID.String(),
	})
	if err != nil {
		t.Fatalf("marshal job payload: %v", err)
	}
	if store.jobs == nil {
		store.jobs = make(map[uuid.UUID]*storage.Job)
	}
	id := uuid.New()
	finished := stamp
	store.jobs[id] = &storage.Job{
		ID:         id,
		TenantID:   tenantID,
		Type:       jobType,
		Status:     status,
		Payload:    payload,
		CreatedAt:  stamp,
		UpdatedAt:  stamp,
		FinishedAt: &finished,
	}
	return id
}

// TestHandleRemediationStats_GoldenPath seeds two rules, one healthy and one
// flaky, and verifies the stats endpoint reports correct per-rule rates and
// overall totals.
func TestHandleRemediationStats_GoldenPath(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()

	store := &fakeStore{}
	now := time.Now().UTC()
	recent := now.Add(-2 * time.Hour)

	// rule-ok: 3 succeeded, 0 failed → 100%
	for i := 0; i < 3; i++ {
		seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusSucceeded, tenantID, nodeID, "rule-ok", recent.Add(time.Duration(i)*time.Minute))
	}
	// rule-flaky: 1 succeeded + 2 failed → 33.33%
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusSucceeded, tenantID, nodeID, "rule-flaky", recent)
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusFailed, tenantID, nodeID, "rule-flaky", recent.Add(time.Minute))
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusFailed, tenantID, nodeID, "rule-flaky", recent.Add(2*time.Minute))
	// rule-old: outside the default 7d window — should be ignored.
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusSucceeded, tenantID, nodeID, "rule-old", now.Add(-30*24*time.Hour))

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/stats", nil)
	req = withPrincipal(req, operatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handleRemediationStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}

	var resp remediationStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Totals.Total != 6 {
		t.Fatalf("total = %d, want 6", resp.Totals.Total)
	}
	if resp.Totals.Succeeded != 4 || resp.Totals.Failed != 2 {
		t.Fatalf("succeeded/failed = %d/%d, want 4/2", resp.Totals.Succeeded, resp.Totals.Failed)
	}
	wantRate := 4.0 / 6.0 * 100
	if resp.Totals.SuccessRate < wantRate-0.01 || resp.Totals.SuccessRate > wantRate+0.01 {
		t.Fatalf("total success rate = %.2f, want ~%.2f", resp.Totals.SuccessRate, wantRate)
	}

	perRule := make(map[string]remediationRuleStat, len(resp.PerRule))
	for _, stat := range resp.PerRule {
		perRule[stat.RuleID] = stat
	}

	okStat, ok := perRule["rule-ok"]
	if !ok {
		t.Fatalf("rule-ok missing in per_rule")
	}
	if okStat.Total != 3 || okStat.Succeeded != 3 {
		t.Fatalf("rule-ok totals = %+v", okStat)
	}
	if okStat.SuccessRate != 100 {
		t.Fatalf("rule-ok success rate = %.2f, want 100", okStat.SuccessRate)
	}

	flakyStat, ok := perRule["rule-flaky"]
	if !ok {
		t.Fatalf("rule-flaky missing in per_rule")
	}
	if flakyStat.Total != 3 || flakyStat.Failed != 2 {
		t.Fatalf("rule-flaky totals = %+v", flakyStat)
	}
	wantFlaky := 1.0 / 3.0 * 100
	if flakyStat.SuccessRate < wantFlaky-0.01 || flakyStat.SuccessRate > wantFlaky+0.01 {
		t.Fatalf("rule-flaky success rate = %.2f, want ~%.2f", flakyStat.SuccessRate, wantFlaky)
	}

	if _, present := perRule["rule-old"]; present {
		t.Fatalf("rule-old should be filtered by window; found it in per_rule")
	}
}

// TestHandleRemediationStats_TenantFilter ensures the tenant_id query arg
// flows through collectRemediationJobs without leaking other tenants.
func TestHandleRemediationStats_TenantFilter(t *testing.T) {
	t.Parallel()

	tenantA := uuid.New()
	tenantB := uuid.New()
	nodeA := uuid.New()
	nodeB := uuid.New()
	store := &fakeStore{}
	now := time.Now().UTC()

	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusSucceeded, tenantA, nodeA, "rule-a", now.Add(-time.Hour))
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusFailed, tenantB, nodeB, "rule-b", now.Add(-time.Hour))

	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/stats?tenant_id="+tenantA.String(), nil)
	req = withPrincipal(req, operatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handleRemediationStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var resp remediationStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Totals.Total != 1 {
		t.Fatalf("total = %d, want 1 (tenant filter leak)", resp.Totals.Total)
	}
	if len(resp.PerRule) != 1 || resp.PerRule[0].RuleID != "rule-a" {
		t.Fatalf("per_rule = %+v, want [rule-a]", resp.PerRule)
	}
}

// TestHandleRemediationFailures_BucketsPerDay seeds jobs across three days
// and asserts the response buckets them under distinct dates.
func TestHandleRemediationFailures_BucketsPerDay(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{}
	now := time.Now().UTC()

	// Two failures today, one failure yesterday, one success today.
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusFailed, tenantID, nodeID, "rule-x", now.Add(-time.Hour))
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusFailed, tenantID, nodeID, "rule-x", now.Add(-2*time.Hour))
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusFailed, tenantID, nodeID, "rule-x", now.Add(-26*time.Hour))
	seedRemediationJob(t, store, remediationJobTypeExecute, storage.JobStatusSucceeded, tenantID, nodeID, "rule-x", now.Add(-3*time.Hour))

	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/failures?window=3d", nil)
	req = withPrincipal(req, operatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handleRemediationFailures(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var resp remediationFailuresResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Points) < 3 {
		t.Fatalf("points = %d, want at least 3 day buckets", len(resp.Points))
	}

	totalFailed := 0
	totalJobs := 0
	for _, p := range resp.Points {
		totalFailed += p.Failed
		totalJobs += p.Total
	}
	if totalFailed != 3 {
		t.Fatalf("summed failed across buckets = %d, want 3", totalFailed)
	}
	if totalJobs != 4 {
		t.Fatalf("summed total across buckets = %d, want 4", totalJobs)
	}
}

// TestHandleRemediationVerificationStats_ClassifiesResults seeds compliance
// results in each verification state and verifies the endpoint counts them.
func TestHandleRemediationVerificationStats_ClassifiesResults(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{complianceResults: make(map[uuid.UUID][]storage.ComplianceResult)}
	now := time.Now().UTC()

	remJob := uuid.New()
	verifyJob := uuid.New()
	rollbackJob := uuid.New()

	// Seed results spanning every bucket.
	checkedAt := now.Add(-time.Hour)
	jobID := uuid.New()
	store.complianceResults[jobID] = []storage.ComplianceResult{
		// Verified
		{
			ID:               uuid.New(),
			TenantID:         tenantID,
			NodeID:           nodeID,
			RuleID:           "rule-verified",
			Passed:           true,
			CheckedAt:        &checkedAt,
			CreatedAt:        checkedAt,
			RemediationJobID: &remJob,
			Verified:         true,
		},
		// Pending verify
		{
			ID:                uuid.New(),
			TenantID:          tenantID,
			NodeID:            nodeID,
			RuleID:            "rule-pending",
			Passed:            false,
			CheckedAt:         &checkedAt,
			CreatedAt:         checkedAt,
			RemediationJobID:  &remJob,
			Verified:          false,
			VerificationJobID: &verifyJob,
		},
		// Rolled back
		{
			ID:               uuid.New(),
			TenantID:         tenantID,
			NodeID:           nodeID,
			RuleID:           "rule-rolled",
			Passed:           false,
			CheckedAt:        &checkedAt,
			CreatedAt:        checkedAt,
			RemediationJobID: &remJob,
			RollbackJobID:    &rollbackJob,
		},
		// Not verified — remediation ran, no verify job attached
		{
			ID:               uuid.New(),
			TenantID:         tenantID,
			NodeID:           nodeID,
			RuleID:           "rule-never",
			Passed:           false,
			CheckedAt:        &checkedAt,
			CreatedAt:        checkedAt,
			RemediationJobID: &remJob,
		},
		// Unrelated — no remediation_job_id, should not count.
		{
			ID:        uuid.New(),
			TenantID:  tenantID,
			NodeID:    nodeID,
			RuleID:    "rule-clean",
			Passed:    true,
			CheckedAt: &checkedAt,
			CreatedAt: checkedAt,
		},
	}

	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/remediation/verification-stats?window=7d", nil)
	req = withPrincipal(req, operatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handleRemediationVerificationStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	var resp remediationVerificationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Verified != 1 {
		t.Fatalf("verified = %d, want 1", resp.Verified)
	}
	if resp.PendingVerify != 1 {
		t.Fatalf("pending_verify = %d, want 1", resp.PendingVerify)
	}
	if resp.RolledBack != 1 {
		t.Fatalf("rolled_back = %d, want 1", resp.RolledBack)
	}
	if resp.NotVerified != 1 {
		t.Fatalf("not_verified = %d, want 1", resp.NotVerified)
	}
	if resp.TotalAttempted != 4 {
		t.Fatalf("total_attempted = %d, want 4 (unrelated result must be excluded)", resp.TotalAttempted)
	}
}

// TestParseWindowParam_AcceptsDayShorthand covers the bespoke "<n>d" UX.
func TestParseWindowParam_AcceptsDayShorthand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", defaultStatsWindow},
		{"7d", 7 * 24 * time.Hour},
		{"1d", 24 * time.Hour},
		{"24h", 24 * time.Hour},
		{"365d", maxStatsWindow}, // capped
	}
	for _, c := range cases {
		got, err := parseWindowParam(c.in)
		if err != nil {
			t.Errorf("parseWindowParam(%q) err = %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseWindowParam(%q) = %s, want %s", c.in, got, c.want)
		}
	}
	if _, err := parseWindowParam("banana"); err == nil {
		t.Errorf("expected error for invalid window, got nil")
	}
	if _, err := parseWindowParam("0d"); err == nil {
		t.Errorf("expected error for zero window, got nil")
	}
}
