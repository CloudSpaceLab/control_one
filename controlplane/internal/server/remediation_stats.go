package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// Remediation job types considered "remediation runs" for stats purposes. We
// fold rollback jobs into the per-rule view because operators want to see the
// complete lifecycle when judging a rule's safety.
const (
	remediationJobTypeExecute  = "remediation.execute"
	remediationJobTypeRollback = "remediation.rollback"
)

// defaultStatsWindow is the lookback when the caller omits ?window=. Matches
// the compliance dashboard's 7d default.
const defaultStatsWindow = 7 * 24 * time.Hour

// maxStatsWindow caps the window param so an operator cannot issue a
// prohibitively expensive scan over all jobs.
const maxStatsWindow = 90 * 24 * time.Hour

// statsJobFetchLimit bounds how many jobs we pull per type for aggregation.
// Sprint 2 produces at most a few thousand remediation jobs per tenant per
// week, so 5000 is a comfortable ceiling without paging forever.
const statsJobFetchLimit = 5000

// remediationRuleStat is one row of the per-rule success-rate table.
type remediationRuleStat struct {
	RuleID      string  `json:"rule_id"`
	Total       int     `json:"total"`
	Succeeded   int     `json:"succeeded"`
	Failed      int     `json:"failed"`
	Running     int     `json:"running"`
	Queued      int     `json:"queued"`
	Cancelled   int     `json:"cancelled"`
	SuccessRate float64 `json:"success_rate"`
	LastRunAt   *string `json:"last_run_at,omitempty"`
}

// remediationStatsResponse is the wire shape for /api/v1/remediation/stats.
type remediationStatsResponse struct {
	WindowStart string                `json:"window_start"`
	WindowEnd   string                `json:"window_end"`
	Totals      remediationStatTotals `json:"totals"`
	PerRule     []remediationRuleStat `json:"per_rule"`
}

type remediationStatTotals struct {
	Total       int     `json:"total"`
	Succeeded   int     `json:"succeeded"`
	Failed      int     `json:"failed"`
	Running     int     `json:"running"`
	Queued      int     `json:"queued"`
	Cancelled   int     `json:"cancelled"`
	SuccessRate float64 `json:"success_rate"`
}

// remediationFailurePoint is one day bucket of the failures timeline.
type remediationFailurePoint struct {
	Date   string `json:"date"`
	Failed int    `json:"failed"`
	Total  int    `json:"total"`
}

type remediationFailuresResponse struct {
	WindowStart string                    `json:"window_start"`
	WindowEnd   string                    `json:"window_end"`
	Points      []remediationFailurePoint `json:"points"`
}

// remediationVerificationResponse is the wire shape for
// /api/v1/remediation/verification-stats. Counts come from compliance_results
// rows attached to rule evaluations that have ever carried a
// remediation_job_id (Sprint 1 wiring).
type remediationVerificationResponse struct {
	WindowStart    string `json:"window_start"`
	WindowEnd      string `json:"window_end"`
	Verified       int    `json:"verified"`
	NotVerified    int    `json:"not_verified"`
	RolledBack     int    `json:"rolled_back"`
	PendingVerify  int    `json:"pending_verify"`
	TotalAttempted int    `json:"total_attempted"`
}

// parseWindowParam accepts Go-style duration strings ("24h", "7d", "168h") and
// returns the since time. Missing or unparseable values fall back to the
// default window.
func parseWindowParam(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultStatsWindow, nil
	}
	// Accept a plain "<n>d" shorthand as operator UX sugar since
	// time.ParseDuration does not recognise days.
	if strings.HasSuffix(trimmed, "d") {
		days := strings.TrimSuffix(trimmed, "d")
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err != nil {
			return 0, fmt.Errorf("invalid window: %s", raw)
		}
		if n <= 0 {
			return 0, fmt.Errorf("window must be positive")
		}
		dur := time.Duration(n) * 24 * time.Hour
		if dur > maxStatsWindow {
			return maxStatsWindow, nil
		}
		return dur, nil
	}
	dur, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid window: %s", raw)
	}
	if dur <= 0 {
		return 0, fmt.Errorf("window must be positive")
	}
	if dur > maxStatsWindow {
		return maxStatsWindow, nil
	}
	return dur, nil
}

// handleRemediationStats returns per-rule success rates over a window.
//
// GET /api/v1/remediation/stats?window=7d&tenant_id=...
func (s *Server) handleRemediationStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	window, err := parseWindowParam(r.URL.Query().Get("window"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tenantID, err := parseOptionalTenantID(r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nodeID, err := parseOptionalNodeID(r.URL.Query().Get("node_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	since := now.Add(-window)

	jobs, err := s.collectRemediationJobs(r.Context(), tenantID, since)
	if err != nil {
		s.logger.Error("collect remediation jobs", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	byRule := make(map[string]*remediationRuleStat)
	totals := remediationStatTotals{}
	for _, job := range jobs {
		ruleID, jobNode := extractJobRuleAndNode(job.Payload)
		if ruleID == "" {
			continue
		}
		if nodeID != uuid.Nil && jobNode != uuid.Nil && jobNode != nodeID {
			continue
		}

		stat, ok := byRule[ruleID]
		if !ok {
			stat = &remediationRuleStat{RuleID: ruleID}
			byRule[ruleID] = stat
		}

		stat.Total++
		totals.Total++
		switch job.Status {
		case storage.JobStatusSucceeded:
			stat.Succeeded++
			totals.Succeeded++
		case storage.JobStatusFailed:
			stat.Failed++
			totals.Failed++
		case storage.JobStatusRunning:
			stat.Running++
			totals.Running++
		case storage.JobStatusQueued:
			stat.Queued++
			totals.Queued++
		case storage.JobStatusCancelled:
			stat.Cancelled++
			totals.Cancelled++
		}

		// Track the freshest run per rule. FinishedAt wins over CreatedAt.
		latest := job.CreatedAt
		if job.FinishedAt != nil && !job.FinishedAt.IsZero() {
			latest = *job.FinishedAt
		}
		if stat.LastRunAt == nil {
			formatted := latest.UTC().Format(time.RFC3339)
			stat.LastRunAt = &formatted
		} else {
			prev, err := time.Parse(time.RFC3339, *stat.LastRunAt)
			if err == nil && latest.After(prev) {
				formatted := latest.UTC().Format(time.RFC3339)
				stat.LastRunAt = &formatted
			}
		}
	}

	perRule := make([]remediationRuleStat, 0, len(byRule))
	for _, stat := range byRule {
		if stat.Succeeded+stat.Failed > 0 {
			stat.SuccessRate = float64(stat.Succeeded) / float64(stat.Succeeded+stat.Failed) * 100
		}
		perRule = append(perRule, *stat)
	}
	sort.SliceStable(perRule, func(i, j int) bool {
		if perRule[i].Total != perRule[j].Total {
			return perRule[i].Total > perRule[j].Total
		}
		return perRule[i].RuleID < perRule[j].RuleID
	})

	if totals.Succeeded+totals.Failed > 0 {
		totals.SuccessRate = float64(totals.Succeeded) / float64(totals.Succeeded+totals.Failed) * 100
	}

	writeJSON(w, http.StatusOK, remediationStatsResponse{
		WindowStart: since.Format(time.RFC3339),
		WindowEnd:   now.Format(time.RFC3339),
		Totals:      totals,
		PerRule:     perRule,
	})
}

// handleRemediationFailures returns a day-bucket time series of failed
// remediation jobs over the window.
//
// GET /api/v1/remediation/failures?window=7d&tenant_id=...&rule_id=...&node_id=...
func (s *Server) handleRemediationFailures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	window, err := parseWindowParam(r.URL.Query().Get("window"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tenantID, err := parseOptionalTenantID(r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nodeID, err := parseOptionalNodeID(r.URL.Query().Get("node_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ruleFilter := strings.TrimSpace(r.URL.Query().Get("rule_id"))

	now := time.Now().UTC()
	since := now.Add(-window)

	jobs, err := s.collectRemediationJobs(r.Context(), tenantID, since)
	if err != nil {
		s.logger.Error("collect remediation jobs for failures", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	days := int(window.Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	buckets := make(map[string]*remediationFailurePoint, days)
	// Pre-seed every day in the window so the UI can render a contiguous axis
	// even if no jobs fell on a given day.
	cursorDay := startOfUTCDay(since)
	endDay := startOfUTCDay(now)
	for !cursorDay.After(endDay) {
		key := cursorDay.Format("2006-01-02")
		buckets[key] = &remediationFailurePoint{Date: cursorDay.Format(time.RFC3339)}
		cursorDay = cursorDay.Add(24 * time.Hour)
	}

	for _, job := range jobs {
		ruleID, jobNode := extractJobRuleAndNode(job.Payload)
		if ruleFilter != "" && !strings.EqualFold(ruleID, ruleFilter) {
			continue
		}
		if nodeID != uuid.Nil && jobNode != uuid.Nil && jobNode != nodeID {
			continue
		}

		stamp := job.CreatedAt
		if job.FinishedAt != nil && !job.FinishedAt.IsZero() {
			stamp = *job.FinishedAt
		}
		bucketKey := startOfUTCDay(stamp).Format("2006-01-02")
		point, ok := buckets[bucketKey]
		if !ok {
			// Landed outside seeded range (clock skew etc). Drop rather than
			// grow the response shape unpredictably.
			continue
		}
		point.Total++
		if job.Status == storage.JobStatusFailed {
			point.Failed++
		}
	}

	ordered := make([]remediationFailurePoint, 0, len(buckets))
	for _, p := range buckets {
		ordered = append(ordered, *p)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Date < ordered[j].Date })

	writeJSON(w, http.StatusOK, remediationFailuresResponse{
		WindowStart: since.Format(time.RFC3339),
		WindowEnd:   now.Format(time.RFC3339),
		Points:      ordered,
	})
}

// handleRemediationVerificationStats returns counts of verified /
// not-verified / rolled-back / pending-verify compliance results that were
// attached to a remediation run within the window.
//
// GET /api/v1/remediation/verification-stats?window=7d&tenant_id=...
func (s *Server) handleRemediationVerificationStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	window, err := parseWindowParam(r.URL.Query().Get("window"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tenantID, err := parseOptionalTenantID(r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nodeID, err := parseOptionalNodeID(r.URL.Query().Get("node_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	since := now.Add(-window)

	filter := storage.ComplianceResultFilter{
		TenantID: tenantID,
		NodeID:   nodeID,
		Since:    &since,
		Until:    &now,
	}
	results, _, err := s.store.ListComplianceResultsFiltered(r.Context(), filter, statsJobFetchLimit, 0)
	if err != nil {
		s.logger.Error("list compliance results for verification stats", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := remediationVerificationResponse{
		WindowStart: since.Format(time.RFC3339),
		WindowEnd:   now.Format(time.RFC3339),
	}
	for _, res := range results {
		// Only consider results that kicked off a remediation run.
		if res.RemediationJobID == nil {
			continue
		}
		resp.TotalAttempted++
		switch {
		case res.RollbackJobID != nil:
			resp.RolledBack++
		case res.Verified:
			resp.Verified++
		case res.VerificationJobID != nil:
			// A verify job was queued but hasn't flipped verified=true yet.
			// Treat this as pending so operators see mid-flight work.
			resp.PendingVerify++
		default:
			// Remediation ran but no verify cycle has been scheduled.
			resp.NotVerified++
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// collectRemediationJobs pulls every remediation execute + rollback job within
// the window for the given tenant (or all tenants when tenantID is nil). Jobs
// older than `since` are trimmed after the list call because the storage layer
// does not expose a created_at filter yet.
func (s *Server) collectRemediationJobs(ctx context.Context, tenantID uuid.UUID, since time.Time) ([]storage.Job, error) {
	if s.store == nil {
		return nil, errors.New("store unavailable")
	}

	// We make two list calls so we do not lose rollback jobs when the
	// caller is paginating ad-hoc.
	exec, _, err := s.store.ListJobs(ctx, tenantID, remediationJobTypeExecute, storage.JobStatus(""), statsJobFetchLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("list execute jobs: %w", err)
	}
	rollback, _, err := s.store.ListJobs(ctx, tenantID, remediationJobTypeRollback, storage.JobStatus(""), statsJobFetchLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("list rollback jobs: %w", err)
	}

	combined := make([]storage.Job, 0, len(exec)+len(rollback))
	for _, j := range exec {
		if j.CreatedAt.Before(since) {
			continue
		}
		combined = append(combined, j)
	}
	for _, j := range rollback {
		if j.CreatedAt.Before(since) {
			continue
		}
		combined = append(combined, j)
	}
	return combined, nil
}

// extractJobRuleAndNode lifts rule_id + node_id out of a job's payload blob.
// The payload shape is set in compliance_remediation.go:dispatchRemediationTask.
// Missing or malformed payloads yield empty strings / uuid.Nil.
func extractJobRuleAndNode(payload []byte) (string, uuid.UUID) {
	if len(payload) == 0 {
		return "", uuid.Nil
	}
	var body struct {
		RuleID string `json:"rule_id"`
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", uuid.Nil
	}
	nodeID := uuid.Nil
	if body.NodeID != "" {
		if parsed, err := uuid.Parse(body.NodeID); err == nil {
			nodeID = parsed
		}
	}
	return body.RuleID, nodeID
}

func startOfUTCDay(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func parseOptionalTenantID(raw string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return uuid.Nil, nil
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid tenant_id")
	}
	return parsed, nil
}

func parseOptionalNodeID(raw string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return uuid.Nil, nil
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid node_id")
	}
	return parsed, nil
}
