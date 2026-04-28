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

	var tenantID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	ctx := r.Context()
	since := time.Now().UTC().Add(-24 * time.Hour)
	resp := dashboardOverviewResponse{
		TenantID:             tenantID.String(),
		GeneratedAt:          formatTime(time.Now().UTC()),
		RuleTriggerCounts24h: map[string]int{},
	}

	if sev, err := s.store.CountSecurityEvents(ctx, storage.SecurityEventFilter{TenantID: tenantID, Since: &since}); err == nil {
		resp.SecurityEventCounts = severityBreakdown(sev)
	} else {
		s.logger.Warn("dashboard security counts", zap.Error(err))
	}
	if hic, err := s.store.CountOpenHealthIncidents(ctx, tenantID); err == nil {
		resp.HealthIncidentCounts = severityBreakdown(hic)
	} else {
		s.logger.Warn("dashboard health counts", zap.Error(err))
	}
	if triggers, err := s.store.CountRuleTriggersSince(ctx, tenantID, since); err == nil {
		resp.RuleTriggerCounts24h = triggers
	} else {
		s.logger.Warn("dashboard rule trigger counts", zap.Error(err))
	}

	if agg, err := s.store.GetComplianceAggregation(ctx, storage.ComplianceResultFilter{TenantID: tenantID, Since: &since}); err == nil && agg != nil {
		resp.ComplianceSummary.Total = agg.Total
		resp.ComplianceSummary.Passed = agg.Passed
		resp.ComplianceSummary.Failed = agg.Failed
	}

	if nodes, total, err := s.store.ListNodes(ctx, tenantID, "", 500, 0); err == nil {
		resp.NodeCounts.Total = total
		now := time.Now().UTC()
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
