package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// periodDelta holds current-window and previous-window counts plus the % change.
type periodDelta struct {
	Current  int     `json:"current"`
	Previous int     `json:"previous"`
	DeltaPct float64 `json:"delta_pct"`
}

// secEventPoint is one time-bucket in the security-event sparkline series.
type secEventPoint struct {
	Ts       string `json:"ts"`
	Critical int    `json:"critical"`
	High     int    `json:"high"`
	Total    int    `json:"total"`
}

// complianceTrendPoint is one time-bucket in the compliance-pass-rate series.
type complianceTrendPoint struct {
	Ts       string  `json:"ts"`
	PassRate float64 `json:"pass_rate"`
	Total    int     `json:"total"`
}

// dashboardOverviewResponse powers the 5-section home dashboard.
type dashboardOverviewResponse struct {
	TenantID              string              `json:"tenant_id,omitempty"`
	GeneratedAt           string              `json:"generated_at"`
	NodeCounts            nodeCountsBreakdown `json:"node_counts"`
	SecurityEventCounts   severityBreakdown   `json:"security_event_counts"`
	HealthIncidentCounts  severityBreakdown   `json:"health_incident_counts"`
	ComplianceSummary     complianceSnapshot  `json:"compliance_summary"`
	RuleTriggerCounts24h  map[string]int      `json:"rule_trigger_counts_24h"`
	RemediationsApplied24 int                 `json:"remediations_applied_24h"`
	// Period-comparison fields (populated when ?period= is supplied).
	Period               string                 `json:"period,omitempty"`
	SecurityEventDelta   *periodDelta           `json:"security_event_delta,omitempty"`
	RuleTriggerDelta     *periodDelta           `json:"rule_trigger_delta,omitempty"`
	RemediationDelta     *periodDelta           `json:"remediation_delta,omitempty"`
	CompliancePassRate   float64                `json:"compliance_pass_rate,omitempty"`
	CompliancePassDelta  *periodDelta           `json:"compliance_pass_delta,omitempty"`
	SecurityEventSeries  []secEventPoint        `json:"security_event_series,omitempty"`
	ComplianceSeries     []complianceTrendPoint `json:"compliance_series,omitempty"`
}

// periodWindow returns the current and previous half-open time windows for
// "24h", "7d", or "30d". Any other value defaults to "24h".
func periodWindow(period string) (since, prevSince, prevUntil time.Time) {
	now := time.Now().UTC()
	var dur time.Duration
	switch period {
	case "7d":
		dur = 7 * 24 * time.Hour
	case "30d":
		dur = 30 * 24 * time.Hour
	default:
		dur = 24 * time.Hour
	}
	since = now.Add(-dur)
	prevUntil = since
	prevSince = since.Add(-dur)
	return
}

// deltaPct computes percentage change, avoiding division by zero.
func deltaPct(current, previous int) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return float64(current-previous) / float64(previous) * 100
}

type severityBreakdown struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Total    int `json:"total"`
}

type nodeCountsBreakdown struct {
	Total   int `json:"total"`
	Healthy int `json:"healthy"`
	Offline int `json:"offline"`
}

type complianceSnapshot struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

func (s *Server) handleDashboardOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	var tenantID uuid.UUID
	if v := strings.TrimSpace(q.Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	period := strings.TrimSpace(q.Get("period"))
	switch period {
	case "7d", "30d":
		// valid
	default:
		period = "24h"
	}

	ctx := r.Context()
	since, prevSince, prevUntil := periodWindow(period)
	now := time.Now().UTC()

	resp := dashboardOverviewResponse{
		TenantID:             tenantID.String(),
		GeneratedAt:          formatTime(now),
		Period:               period,
		RuleTriggerCounts24h: map[string]int{},
	}

	// --- security events (current window) ---
	if sev, err := s.store.CountSecurityEvents(ctx, storage.SecurityEventFilter{TenantID: tenantID, Since: &since}); err == nil {
		resp.SecurityEventCounts = severityBreakdown(sev)
	} else {
		s.logger.Warn("dashboard security counts", zap.Error(err))
	}

	// --- security events delta (reuse already-computed current counts) ---
	if prev, err := s.store.CountSecurityEvents(ctx, storage.SecurityEventFilter{TenantID: tenantID, Since: &prevSince, Until: &prevUntil}); err == nil {
		resp.SecurityEventDelta = &periodDelta{
			Current:  resp.SecurityEventCounts.Total,
			Previous: prev.Total,
			DeltaPct: deltaPct(resp.SecurityEventCounts.Total, prev.Total),
		}
	}

	// --- sparkline series ---
	bucket := "day"
	if period == "24h" {
		bucket = "hour"
	}
	if series, err := s.store.GetSecurityEventSeries(ctx, tenantID, since, bucket); err == nil {
		pts := make([]secEventPoint, 0, len(series))
		for _, p := range series {
			pts = append(pts, secEventPoint{
				Ts:       p.Timestamp.Format(time.RFC3339),
				Critical: p.Critical,
				High:     p.High,
				Total:    p.Total,
			})
		}
		resp.SecurityEventSeries = pts
	} else {
		s.logger.Warn("dashboard security series", zap.Error(err))
	}

	// --- health incidents ---
	if hic, err := s.store.CountOpenHealthIncidents(ctx, tenantID); err == nil {
		resp.HealthIncidentCounts = severityBreakdown(hic)
	} else {
		s.logger.Warn("dashboard health counts", zap.Error(err))
	}

	// --- rule triggers ---
	if triggers, err := s.store.CountRuleTriggersSince(ctx, tenantID, since); err == nil {
		resp.RuleTriggerCounts24h = triggers
		curTotal := 0
		for _, v := range triggers {
			curTotal += v
		}
		if prevTriggers, err := s.store.CountRuleTriggersBetween(ctx, tenantID, prevSince, prevUntil); err == nil {
			prevTotal := 0
			for _, v := range prevTriggers {
				prevTotal += v
			}
			resp.RuleTriggerDelta = &periodDelta{
				Current:  curTotal,
				Previous: prevTotal,
				DeltaPct: deltaPct(curTotal, prevTotal),
			}
		}
	} else {
		s.logger.Warn("dashboard rule trigger counts", zap.Error(err))
	}

	// --- remediations ---
	if cur, err := s.store.CountRemediationsSince(ctx, tenantID, since, now); err == nil {
		resp.RemediationsApplied24 = cur
		if prev, err := s.store.CountRemediationsSince(ctx, tenantID, prevSince, prevUntil); err == nil {
			resp.RemediationDelta = &periodDelta{
				Current:  cur,
				Previous: prev,
				DeltaPct: deltaPct(cur, prev),
			}
		}
	}

	// --- compliance ---
	if agg, err := s.store.GetComplianceAggregation(ctx, storage.ComplianceResultFilter{TenantID: tenantID, Since: &since}); err == nil && agg != nil {
		resp.ComplianceSummary.Total = agg.Total
		resp.ComplianceSummary.Passed = agg.Passed
		resp.ComplianceSummary.Failed = agg.Failed
		if agg.Total > 0 {
			resp.CompliancePassRate = float64(agg.Passed) / float64(agg.Total) * 100
		}
		// compliance delta using previous window
		if prevAgg, err := s.store.GetComplianceAggregation(ctx, storage.ComplianceResultFilter{TenantID: tenantID, Since: &prevSince, Until: &prevUntil}); err == nil && prevAgg != nil {
			curPct := 0
			prevPct := 0
			if agg.Total > 0 {
				curPct = int(float64(agg.Passed) / float64(agg.Total) * 100)
			}
			if prevAgg.Total > 0 {
				prevPct = int(float64(prevAgg.Passed) / float64(prevAgg.Total) * 100)
			}
			resp.CompliancePassDelta = &periodDelta{
				Current:  curPct,
				Previous: prevPct,
				DeltaPct: deltaPct(curPct, prevPct),
			}
		}
	}

	// --- compliance sparkline series ---
	if trends, err := s.store.GetComplianceTrends(ctx, storage.ComplianceResultFilter{TenantID: tenantID, Since: &since}, 30); err == nil {
		pts := make([]complianceTrendPoint, 0, len(trends))
		for _, t := range trends {
			pts = append(pts, complianceTrendPoint{
				Ts:       t.Date.Format(time.RFC3339),
				PassRate: t.PassRate,
				Total:    t.Total,
			})
		}
		resp.ComplianceSeries = pts
	}

	// --- fleet ---
	if nodes, total, err := s.store.ListNodes(ctx, tenantID, "", 500, 0); err == nil {
		resp.NodeCounts.Total = total
		for _, n := range nodes {
			if n.LastSeenAt != nil && now.Sub(*n.LastSeenAt) < 5*time.Minute {
				resp.NodeCounts.Healthy++
			} else {
				resp.NodeCounts.Offline++
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleMetricsMTTD returns Mean Time To Detect metrics for security events.
func (s *Server) handleMetricsMTTD(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var tenantID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	severity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("severity")))
	if severity == "" {
		severity = "critical"
	}

	since := time.Now().UTC().AddDate(0, 0, -7)
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		var days int
		if _, err := fmt.Sscanf(daysStr, "%d", &days); err == nil && days > 0 {
			since = time.Now().UTC().AddDate(0, 0, -days)
		}
	}

	metrics, err := s.store.GetMTTDMetrics(r.Context(), tenantID, severity, since)
	if err != nil {
		s.logger.Warn("mttd metrics", zap.Error(err))
		// Return zero-value response instead of 500 to keep UI functional
		metrics = &storage.MTTDMetrics{
			Severity:     severity,
			Period:       since.Format("2006-01-02") + " to " + time.Now().Format("2006-01-02"),
			CalculatedAt: time.Now().UTC(),
		}
	}

	writeJSON(w, http.StatusOK, metrics)
}

// handleMetricsMTTR returns Mean Time To Remediate metrics for compliance findings.
func (s *Server) handleMetricsMTTR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var tenantID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	severity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("severity")))
	if severity == "" {
		severity = "critical"
	}

	since := time.Now().UTC().AddDate(0, 0, -7)
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		var days int
		if _, err := fmt.Sscanf(daysStr, "%d", &days); err == nil && days > 0 {
			since = time.Now().UTC().AddDate(0, 0, -days)
		}
	}

	metrics, err := s.store.GetMTTRMetrics(r.Context(), tenantID, severity, since)
	if err != nil {
		s.logger.Warn("mttr metrics", zap.Error(err))
		// Return zero-value response instead of 500 to keep UI functional
		metrics = &storage.MTTRMetrics{
			Severity:     severity,
			Period:       since.Format("2006-01-02") + " to " + time.Now().Format("2006-01-02"),
			CalculatedAt: time.Now().UTC(),
		}
	}

	writeJSON(w, http.StatusOK, metrics)
}

// handleMetricsRemediationVelocity returns remediation velocity metrics.
func (s *Server) handleMetricsRemediationVelocity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var tenantID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	periodDays := 30
	if daysStr := r.URL.Query().Get("period"); daysStr != "" {
		if _, err := fmt.Sscanf(daysStr, "%d", &periodDays); err != nil {
			periodDays = 30
		}
	}

	velocity, err := s.store.GetRemediationVelocity(r.Context(), tenantID, periodDays)
	if err != nil {
		s.logger.Warn("remediation velocity metrics", zap.Error(err))
		// Return zero-value response instead of 500 to keep UI functional
		velocity = &storage.RemediationVelocity{
			Period:         fmt.Sprintf("%d days", periodDays),
			TrendDirection: "stable",
		}
	}

	writeJSON(w, http.StatusOK, velocity)
}

// handleMetricsFindingsAging returns the age distribution of open findings.
func (s *Server) handleMetricsFindingsAging(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var tenantID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	severity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("severity")))
	if severity == "" {
		severity = "critical"
	}

	aging, err := s.store.GetFindingAging(r.Context(), tenantID, severity)
	if err != nil {
		s.logger.Warn("findings aging metrics", zap.Error(err))
		// Return zero-value response instead of 500 to keep UI functional
		aging = &storage.FindingAging{Severity: severity}
	}

	writeJSON(w, http.StatusOK, aging)
}

// handleMetricsRiskScore returns the executive risk score.
func (s *Server) handleMetricsRiskScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var tenantID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	score, err := s.store.CalculateRiskScore(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("risk score calculation", zap.Error(err))
		// Return zero-value response instead of 500 to keep UI functional
		now := time.Now().UTC()
		score = &storage.RiskScore{
			Score:          0,
			MaxScore:       100,
			Percent:        0,
			TrendDirection: "stable",
			CalculatedAt:   now,
			Components:     []storage.RiskComponent{},
		}
	}

	writeJSON(w, http.StatusOK, score)
}

// riskScoreHistoryPoint mirrors storage.RiskScorePoint with snake_case JSON tags.
type riskScoreHistoryPoint struct {
	Timestamp string `json:"ts"`
	Score     int    `json:"score"`
}

type riskScoreHistoryResponse struct {
	Points []riskScoreHistoryPoint `json:"points"`
}

// handleMetricsRiskScoreHistory returns daily risk-score points for the last
// `days` days (defaults to 30).
func (s *Server) handleMetricsRiskScoreHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	tenantID, ok := parseTenantQuery(w, r)
	if !ok {
		return
	}
	days := parseDaysQuery(r, 30)

	resp := riskScoreHistoryResponse{Points: []riskScoreHistoryPoint{}}
	points, err := s.store.GetRiskScoreHistory(r.Context(), tenantID, days)
	if err != nil {
		s.logger.Warn("risk score history", zap.Error(err))
	} else {
		for _, p := range points {
			resp.Points = append(resp.Points, riskScoreHistoryPoint{
				Timestamp: formatTime(p.Timestamp),
				Score:     p.Score,
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type remediationVelocityPoint struct {
	Timestamp string `json:"ts"`
	Count     int    `json:"count"`
}

type remediationVelocityHistoryResponse struct {
	Points []remediationVelocityPoint `json:"points"`
}

// handleMetricsRemediationVelocityHistory returns one count per day over the
// requested period.
func (s *Server) handleMetricsRemediationVelocityHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	tenantID, ok := parseTenantQuery(w, r)
	if !ok {
		return
	}
	days := parseDaysQuery(r, 30)

	resp := remediationVelocityHistoryResponse{Points: []remediationVelocityPoint{}}
	points, err := s.store.GetRemediationVelocityHistory(r.Context(), tenantID, days)
	if err != nil {
		s.logger.Warn("remediation velocity history", zap.Error(err))
	} else {
		for _, p := range points {
			resp.Points = append(resp.Points, remediationVelocityPoint{
				Timestamp: formatTime(p.Timestamp),
				Count:     p.Count,
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type frameworkSummary struct {
	Name     string  `json:"name"`
	Pass     int     `json:"pass"`
	Fail     int     `json:"fail"`
	Coverage float64 `json:"coverage"`
}

type complianceByFrameworkResponse struct {
	Frameworks []frameworkSummary `json:"frameworks"`
}

// handleMetricsComplianceByFramework groups recent compliance results by their
// framework prefix.
func (s *Server) handleMetricsComplianceByFramework(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	tenantID, ok := parseTenantQuery(w, r)
	if !ok {
		return
	}

	resp := complianceByFrameworkResponse{Frameworks: []frameworkSummary{}}
	rows, err := s.store.GetComplianceByFramework(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("compliance by framework", zap.Error(err))
	} else {
		for _, row := range rows {
			resp.Frameworks = append(resp.Frameworks, frameworkSummary{
				Name:     row.Name,
				Pass:     row.Pass,
				Fail:     row.Fail,
				Coverage: row.Coverage,
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseTenantQuery extracts an optional tenant_id query param. When absent it
// returns uuid.Nil. When malformed it writes a 400 and returns ok=false.
func parseTenantQuery(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	v := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if v == "" {
		return uuid.Nil, true
	}
	parsed, err := uuid.Parse(v)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return parsed, true
}

// parseDaysQuery reads the `days` query parameter, falling back to def if
// missing or unparseable. The value is clamped to [1, 365].
func parseDaysQuery(r *http.Request, def int) int {
	days := def
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		var parsed int
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil && parsed > 0 {
			days = parsed
		}
	}
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	return days
}
