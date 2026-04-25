package server

import (
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
