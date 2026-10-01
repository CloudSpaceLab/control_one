package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type controlRoomExecutiveRuleViolationStore interface {
	GetRuleViolationSummary(context.Context, uuid.UUID, time.Time, time.Time, time.Time, time.Time, int) (storage.RuleViolationSummary, error)
}

type controlRoomExecutiveAutomaticResponseStore interface {
	GetAutomaticResponseSummary(context.Context, uuid.UUID, time.Time, time.Time, int) (storage.AutomaticResponseSummary, error)
}

type controlRoomExecutiveOverviewResponse struct {
	TenantID     string                           `json:"tenant_id"`
	GeneratedAt  string                           `json:"generated_at"`
	Period       string                           `json:"period"`
	Estate       controlRoomExecutiveEstate       `json:"estate"`
	Violations   controlRoomExecutiveViolations   `json:"violations"`
	Response     controlRoomExecutiveResponse     `json:"response"`
	Attention    controlRoomExecutiveAttention    `json:"attention"`
	Protection   controlRoomExecutiveProtection   `json:"protection"`
	Activity     []controlRoomExecutiveActivity   `json:"activity"`
	Availability controlRoomExecutiveAvailability `json:"availability"`
}

type controlRoomExecutiveAvailability struct {
	Estate     bool `json:"estate"`
	Violations bool `json:"violations"`
	Response   bool `json:"response"`
	Attention  bool `json:"attention"`
	Protection bool `json:"protection"`
	Activity   bool `json:"activity"`
}

type controlRoomExecutiveEstate struct {
	GroupsTotal    int                         `json:"groups_total"`
	GroupsHealthy  int                         `json:"groups_healthy"`
	GroupsDegraded int                         `json:"groups_degraded"`
	GroupsCritical int                         `json:"groups_critical"`
	GroupsUnknown  int                         `json:"groups_unknown"`
	NodesTotal     int                         `json:"nodes_total"`
	NodesHealthy   int                         `json:"nodes_healthy"`
	Groups         []controlRoomExecutiveGroup `json:"groups"`
}

type controlRoomExecutiveGroup struct {
	Name                  string `json:"name"`
	State                 string `json:"state"`
	NodesTotal            int    `json:"nodes_total"`
	NodesHealthy          int    `json:"nodes_healthy"`
	NodesStale            int    `json:"nodes_stale"`
	NodesOffline          int    `json:"nodes_offline"`
	IntentionallyIsolated int    `json:"intentionally_isolated"`
	Drilldown             string `json:"drilldown"`
}

type controlRoomExecutiveViolations struct {
	Total         int                           `json:"total"`
	Critical      int                           `json:"critical"`
	High          int                           `json:"high"`
	Medium        int                           `json:"medium"`
	Low           int                           `json:"low"`
	Info          int                           `json:"info"`
	Other         int                           `json:"other"`
	PreviousTotal int                           `json:"previous_total"`
	DeltaPct      float64                       `json:"delta_pct"`
	TopRules      []controlRoomExecutiveTopRule `json:"top_rules"`
}

type controlRoomExecutiveTopRule struct {
	RuleID    string `json:"rule_id"`
	Name      string `json:"name"`
	RuleType  string `json:"rule_type"`
	Severity  string `json:"severity"`
	Count     int    `json:"count"`
	Drilldown string `json:"drilldown"`
}

type controlRoomExecutiveResponse struct {
	HandledAutomatically int `json:"handled_automatically"`
	Blocked              int `json:"blocked"`
	Contained            int `json:"contained"`
	Remediated           int `json:"remediated"`
	Failed               int `json:"failed"`
	FailedCritical       int `json:"-"`
}

type controlRoomExecutiveAttention struct {
	Total         int                                 `json:"total"`
	Critical      int                                 `json:"critical"`
	Reviews       int                                 `json:"reviews"`
	Approvals     int                                 `json:"approvals"`
	Interventions int                                 `json:"interventions"`
	Items         []controlRoomExecutiveAttentionItem `json:"items"`
}

type controlRoomExecutiveAttentionItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Severity  string `json:"severity"`
	Domain    string `json:"domain"`
	Title     string `json:"title"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"created_at"`
	Drilldown string `json:"drilldown"`
}

type controlRoomExecutiveProtection struct {
	Protected  int                                 `json:"protected"`
	Total      int                                 `json:"total"`
	Percentage float64                             `json:"percentage"`
	Gaps       int                                 `json:"gaps"`
	GapTypes   []controlRoomExecutiveProtectionGap `json:"gap_types"`
}

type controlRoomExecutiveProtectionGap struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type controlRoomExecutiveActivity struct {
	Ts       string `json:"ts"`
	Critical int    `json:"critical"`
	High     int    `json:"high"`
	Total    int    `json:"total"`
}

func (s *Server) handleControlRoomExecutiveOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	tenantID, period, since, ok := parseControlRoomQuery(w, r)
	if !ok {
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer, roleOperator, roleInvestigator, roleAdmin) {
		return
	}

	resp := s.buildControlRoomExecutiveOverview(r.Context(), tenantID, period, since, time.Now().UTC())
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) buildControlRoomExecutiveOverview(
	ctx context.Context,
	tenantID uuid.UUID,
	period string,
	since time.Time,
	now time.Time,
) controlRoomExecutiveOverviewResponse {
	resp := controlRoomExecutiveOverviewResponse{
		TenantID:    tenantID.String(),
		GeneratedAt: formatTime(now),
		Period:      period,
		Estate:      controlRoomExecutiveEstate{Groups: []controlRoomExecutiveGroup{}},
		Violations:  controlRoomExecutiveViolations{TopRules: []controlRoomExecutiveTopRule{}},
		Attention:   controlRoomExecutiveAttention{Items: []controlRoomExecutiveAttentionItem{}},
		Protection:  controlRoomExecutiveProtection{GapTypes: []controlRoomExecutiveProtectionGap{}},
		Activity:    []controlRoomExecutiveActivity{},
	}

	nodes, err := s.controlRoomExecutiveNodes(ctx, tenantID)
	if err != nil {
		s.logger.Warn("control room executive nodes", zap.Error(err))
	} else {
		resp.Estate = buildControlRoomExecutiveEstate(nodes, now)
		resp.Availability.Estate = true
	}

	prevUntil := since
	prevSince := since.Add(-periodDuration(period))
	if store, ok := s.store.(controlRoomExecutiveRuleViolationStore); ok {
		summary, err := store.GetRuleViolationSummary(ctx, tenantID, since, now, prevSince, prevUntil, 5)
		if err != nil {
			s.logger.Warn("control room executive rule violations", zap.Error(err))
		} else {
			resp.Violations = s.controlRoomExecutiveViolations(ctx, tenantID, summary)
			resp.Availability.Violations = true
		}
	}

	response, failedAutomaticPlans, handledAlertIDs, responseAvailable := s.controlRoomExecutiveAutomaticResponse(ctx, tenantID, since, now)
	resp.Response = response
	resp.Availability.Response = responseAvailable

	attention, attentionAvailable := s.controlRoomExecutiveAttention(ctx, tenantID, failedAutomaticPlans, response.Failed, response.FailedCritical, handledAlertIDs)
	resp.Attention = attention
	resp.Availability.Attention = attentionAvailable

	if nodes != nil {
		protection, protectionAvailable := s.controlRoomExecutiveProtection(ctx, tenantID, nodes, now)
		resp.Protection = protection
		resp.Availability.Protection = protectionAvailable
	}

	bucket := "day"
	if period == "1h" || period == "6h" || period == "24h" {
		bucket = "hour"
	}
	if series, err := s.store.GetSecurityEventSeries(ctx, tenantID, since, bucket); err != nil {
		s.logger.Warn("control room executive activity", zap.Error(err))
	} else {
		resp.Activity = make([]controlRoomExecutiveActivity, 0, len(series))
		for _, point := range series {
			resp.Activity = append(resp.Activity, controlRoomExecutiveActivity{
				Ts:       formatTime(point.Timestamp),
				Critical: point.Critical,
				High:     point.High,
				Total:    point.Total,
			})
		}
		resp.Availability.Activity = true
	}
	return resp
}

func (s *Server) controlRoomExecutiveNodes(ctx context.Context, tenantID uuid.UUID) ([]storage.Node, error) {
	const pageSize = 500
	var out []storage.Node
	for offset := 0; ; offset += pageSize {
		rows, total, err := s.store.ListNodes(ctx, tenantID, "", pageSize, offset)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(out) >= total || len(rows) == 0 {
			return out, nil
		}
	}
}

type controlRoomExecutiveGroupAccumulator struct {
	controlRoomExecutiveGroup
	criticalOffline bool
}

func buildControlRoomExecutiveEstate(nodes []storage.Node, now time.Time) controlRoomExecutiveEstate {
	out := controlRoomExecutiveEstate{
		NodesTotal: len(nodes),
		Groups:     []controlRoomExecutiveGroup{},
	}
	isolation := newControlRoomIsolation(nodes, now)
	airgapped := isolationNodeModeSet(isolation, isolationModeAirgapped)
	groups := map[string]*controlRoomExecutiveGroupAccumulator{}

	for _, node := range nodes {
		name := controlRoomExecutiveGroupName(node)
		group := groups[name]
		if group == nil {
			group = &controlRoomExecutiveGroupAccumulator{
				controlRoomExecutiveGroup: controlRoomExecutiveGroup{
					Name:      name,
					State:     "unknown",
					Drilldown: "/nodes",
				},
			}
			groups[name] = group
		}
		group.NodesTotal++

		if node.LastSeenAt != nil && now.Sub(node.LastSeenAt.UTC()) <= 5*time.Minute {
			group.NodesHealthy++
			out.NodesHealthy++
			continue
		}
		if _, ok := airgapped[node.ID.String()]; ok {
			group.IntentionallyIsolated++
			continue
		}
		if node.LastSeenAt != nil && now.Sub(node.LastSeenAt.UTC()) <= 30*time.Minute {
			group.NodesStale++
			continue
		}
		group.NodesOffline++
		if nodeIsCritical(node) {
			group.criticalOffline = true
		}
	}

	for _, group := range groups {
		switch {
		case group.criticalOffline:
			group.State = "critical"
			out.GroupsCritical++
		case group.NodesOffline > 0 || group.NodesStale > 0:
			group.State = "degraded"
			out.GroupsDegraded++
		case group.NodesHealthy > 0 || group.IntentionallyIsolated > 0:
			group.State = "healthy"
			out.GroupsHealthy++
		default:
			group.State = "unknown"
			out.GroupsUnknown++
		}
		out.Groups = append(out.Groups, group.controlRoomExecutiveGroup)
	}
	out.GroupsTotal = len(out.Groups)
	sort.SliceStable(out.Groups, func(i, j int) bool {
		left := controlRoomExecutiveStateRank(out.Groups[i].State)
		right := controlRoomExecutiveStateRank(out.Groups[j].State)
		if left == right {
			return out.Groups[i].Name < out.Groups[j].Name
		}
		return left > right
	})
	return out
}

func controlRoomExecutiveGroupName(node storage.Node) string {
	value := labelString(
		node.Labels,
		"dashboard_group",
		"server_group",
		"cluster",
		"cluster.name",
		"application_group",
		"app_group",
		"service_group",
		"agent.primary_purpose",
		"primary_purpose",
		"server_purpose",
	)
	if strings.TrimSpace(value) == "" {
		return "Ungrouped"
	}
	return displayPurpose(normalizePurpose(value))
}

func controlRoomExecutiveStateRank(state string) int {
	switch state {
	case "critical":
		return 4
	case "degraded":
		return 3
	case "unknown":
		return 2
	case "healthy":
		return 1
	default:
		return 0
	}
}

func (s *Server) controlRoomExecutiveViolations(
	ctx context.Context,
	tenantID uuid.UUID,
	summary storage.RuleViolationSummary,
) controlRoomExecutiveViolations {
	out := controlRoomExecutiveViolations{
		Total:         summary.Total,
		Critical:      summary.Critical,
		High:          summary.High,
		Medium:        summary.Medium,
		Low:           summary.Low,
		Info:          summary.Info,
		Other:         summary.Other,
		PreviousTotal: summary.PreviousTotal,
		DeltaPct:      deltaPct(summary.Total, summary.PreviousTotal),
		TopRules:      make([]controlRoomExecutiveTopRule, 0, len(summary.TopRules)),
	}
	for _, rule := range summary.TopRules {
		name, drilldown := s.controlRoomExecutiveRuleName(ctx, tenantID, rule)
		out.TopRules = append(out.TopRules, controlRoomExecutiveTopRule{
			RuleID:    rule.RuleID.String(),
			Name:      name,
			RuleType:  rule.RuleType,
			Severity:  rule.Severity,
			Count:     rule.Count,
			Drilldown: drilldown,
		})
	}
	return out
}

func (s *Server) controlRoomExecutiveRuleName(
	ctx context.Context,
	tenantID uuid.UUID,
	rule storage.RuleViolationTopRule,
) (string, string) {
	switch rule.RuleType {
	case "port":
		if row, err := s.store.GetPortRule(ctx, rule.RuleID); err == nil && row != nil && row.TenantID == tenantID && strings.TrimSpace(row.Name) != "" {
			return row.Name, "/rules"
		}
		return "Port rule", "/rules"
	case "log":
		if row, err := s.store.GetLogRule(ctx, rule.RuleID); err == nil && row != nil && row.TenantID == tenantID && strings.TrimSpace(row.Name) != "" {
			return row.Name, "/rules"
		}
		return "Log rule", "/rules"
	case "compliance":
		if row, err := s.store.GetPolicy(ctx, rule.RuleID); err == nil && row != nil && row.TenantID == tenantID && strings.TrimSpace(row.Name) != "" {
			return row.Name, "/compliance"
		}
		return "Compliance rule", "/compliance"
	default:
		return "Rule " + rule.RuleID.String()[:8], "/rules"
	}
}

func (s *Server) controlRoomExecutiveAutomaticResponse(
	ctx context.Context,
	tenantID uuid.UUID,
	since time.Time,
	until time.Time,
) (controlRoomExecutiveResponse, []storage.ActionPlan, map[uuid.UUID]struct{}, bool) {
	var out controlRoomExecutiveResponse
	handledAlertIDs := map[uuid.UUID]struct{}{}
	store, ok := s.store.(controlRoomExecutiveAutomaticResponseStore)
	if !ok {
		return out, nil, handledAlertIDs, false
	}
	summary, err := store.GetAutomaticResponseSummary(ctx, tenantID, since, until, 8)
	if err != nil {
		s.logger.Warn("control room executive automatic responses", zap.Error(err))
		return out, nil, handledAlertIDs, false
	}
	for _, alertID := range summary.HandledAlertIDs {
		if alertID != uuid.Nil {
			handledAlertIDs[alertID] = struct{}{}
		}
	out = controlRoomExecutiveResponse{
		HandledAutomatically: summary.HandledAutomatically,
		Blocked:              summary.Blocked,
		Contained:            summary.Contained,
		Remediated:           summary.Remediated,
		Failed:               summary.Failed,
		FailedCritical:       summary.FailedCritical,
	}
	return out, summary.FailedPlans, handledAlertIDs, true
}

func (s *Server) controlRoomExecutiveAttention(
	ctx context.Context,
	tenantID uuid.UUID,
	failedAutomaticPlans []storage.ActionPlan,
	failedAutomaticTotal int,
	failedAutomaticCritical int,
	handledAlertIDs map[uuid.UUID]struct{},
) (controlRoomExecutiveAttention, bool) {
	out := controlRoomExecutiveAttention{Items: []controlRoomExecutiveAttentionItem{}}
	available := true

	blockProposals := []storage.IPBlocklistEntry{}
	blockProposalTotal := 0
	linkedReviewAlerts := map[uuid.UUID]storage.Alert{}
	if store, ok := s.store.(ipBlockProposalQueryStore); ok {
		proposals, total, err := controlRoomExecutiveProposedBlocks(ctx, store, tenantID)
		if err != nil {
			available = false
			s.logger.Warn("control room executive block approvals", zap.Error(err))
		} else {
			blockProposals = proposals
			blockProposalTotal = total
			for _, proposal := range proposals {
				alertID, ok := controlRoomExecutiveProposalAlertID(proposal.Reason)
				if !ok {
					continue
				}
				alert, err := s.store.GetAlert(ctx, alertID)
				if err != nil {
					available = false
					s.logger.Warn("control room executive linked alert", zap.Error(err), zap.String("alert_id", alertID.String()))
					continue
				}
				if alert != nil && alert.TenantID == tenantID && (alert.State == "open" || alert.State == "acked") {
					linkedReviewAlerts[alert.ID] = *alert
				}
			}
		}
	} else {
		available = false
	}

	openRows, openTotal, err := s.store.ListAlerts(ctx, storage.AlertFilter{TenantID: tenantID, State: "open"}, 8, 0)
	if err != nil {
		available = false
		s.logger.Warn("control room executive open alerts", zap.Error(err))
	}
	ackedRows, ackedTotal, err := s.store.ListAlerts(ctx, storage.AlertFilter{TenantID: tenantID, State: "acked"}, 8, 0)
	if err != nil {
		available = false
		s.logger.Warn("control room executive acked alerts", zap.Error(err))
	}
	excludedReviewAlerts := make(map[uuid.UUID]storage.Alert, len(linkedReviewAlerts)+len(handledAlertIDs))
	for id, alert := range linkedReviewAlerts {
		excludedReviewAlerts[id] = alert
	}
	for id := range handledAlertIDs {
		if _, exists := excludedReviewAlerts[id]; exists {
			continue
		}
		alert, err := s.store.GetAlert(ctx, id)
		if err != nil {
			available = false
			s.logger.Warn("control room executive handled alert", zap.Error(err), zap.String("alert_id", id.String()))
			continue
		}
		if alert != nil && alert.TenantID == tenantID && (alert.State == "open" || alert.State == "acked") {
			excludedReviewAlerts[id] = *alert
		}
	}
	out.Reviews = controlRoomNonNegative(openTotal + ackedTotal - len(excludedReviewAlerts))

	_, openCritical, err := s.store.ListAlerts(ctx, storage.AlertFilter{TenantID: tenantID, State: "open", Severity: "critical"}, 1, 0)
	if err != nil {
		available = false
	}
	_, ackedCritical, err := s.store.ListAlerts(ctx, storage.AlertFilter{TenantID: tenantID, State: "acked", Severity: "critical"}, 1, 0)
	if err != nil {
		available = false
	}
	linkedCritical := 0
	for _, alert := range excludedReviewAlerts {
		if strings.EqualFold(strings.TrimSpace(alert.Severity), "critical") {
			linkedCritical++
		}
	}
	out.Critical = controlRoomNonNegative(openCritical + ackedCritical - linkedCritical)

	for _, alert := range append(openRows, ackedRows...) {
		if _, excluded := excludedReviewAlerts[alert.ID]; excluded {
			continue
		}
		out.Items = append(out.Items, controlRoomExecutiveAttentionItem{
			ID:        alert.ID.String(),
			Kind:      "review",
			Severity:  firstNonEmptyIPBehavior(alert.Severity, "medium"),
			Domain:    "alerts",
			Title:     firstNonEmptyIPBehavior(alert.Title, "Alert requires review"),
			Reason:    nullableStringTrimmed(alert.Summary),
			CreatedAt: formatTime(alert.OpenedAt),
			Drilldown: "/alerts?alert_id=" + alert.ID.String(),
		})
	}

	approvals, approvalTotal, err := s.store.ListPatchApprovals(
		ctx,
		storage.ListPatchApprovalsFilter{TenantID: tenantID, Status: storage.ApprovalStatusPending},
		4,
		0,
	)
	if err != nil {
		available = false
		s.logger.Warn("control room executive patch approvals", zap.Error(err))
	} else {
		out.Approvals = approvalTotal
		for _, approval := range approvals {
			title := "Patch approval"
			if node, err := s.store.GetNode(ctx, approval.NodeID); err == nil && node != nil && node.TenantID == tenantID && strings.TrimSpace(node.Hostname) != "" {
				title = "Patch " + node.Hostname
			}
			out.Items = append(out.Items, controlRoomExecutiveAttentionItem{
				ID:        approval.ID.String(),
				Kind:      "approval",
				Severity:  "medium",
				Domain:    "patch",
				Title:     title,
				Reason:    firstNonEmptyIPBehavior(approval.Mode, "patch deployment"),
				CreatedAt: formatTime(approval.CreatedAt),
				Drilldown: "/infrastructure/patch",
			})
		}
	}

	remediationApprovals, remediationApprovalTotal, err := s.store.ListRemediationApprovals(
		ctx,
		storage.ListRemediationApprovalsFilter{TenantID: tenantID, Status: storage.ApprovalStatusPending},
		0,
		0,
	)
	if err != nil {
		available = false
		s.logger.Warn("control room executive remediation approvals", zap.Error(err))
	} else {
		out.Approvals += remediationApprovalTotal
		for index, approval := range remediationApprovals {
			severity := firstNonEmptyIPBehavior(approval.Severity, "high")
			if strings.EqualFold(severity, "critical") {
				out.Critical++
			}
			if index >= 4 {
				continue
			}
			title := "Remediation approval"
			if node, err := s.store.GetNode(ctx, approval.NodeID); err == nil && node != nil && node.TenantID == tenantID && strings.TrimSpace(node.Hostname) != "" {
				title = "Remediate " + node.Hostname
			}
			out.Items = append(out.Items, controlRoomExecutiveAttentionItem{
				ID:        approval.ID.String(),
				Kind:      "approval",
				Severity:  severity,
				Domain:    "compliance",
				Title:     title,
				Reason:    strings.TrimSpace(approval.RuleID),
				CreatedAt: formatTime(approval.CreatedAt),
				Drilldown: "/compliance",
			})
		}
	}

	out.Approvals += blockProposalTotal
	for index, proposal := range blockProposals {
		severity := controlRoomExecutiveProposalSeverity(proposal, linkedReviewAlerts)
		if severity == "critical" {
			out.Critical++
		}
		if index >= 4 {
			continue
		}
		out.Items = append(out.Items, controlRoomExecutiveAttentionItem{
			ID:        proposal.ID.String(),
			Kind:      "approval",
			Severity:  severity,
			Domain:    "network",
			Title:     "Block " + proposal.IPCIDR,
			Reason:    strings.TrimSpace(proposal.Reason),
			CreatedAt: formatTime(proposal.CreatedAt),
			Drilldown: "/security/network?tab=approvals&proposal_id=" + proposal.ID.String(),
		})
	}

	out.Interventions = failedAutomaticTotal
	out.Critical += failedAutomaticCritical
	for _, plan := range failedAutomaticPlans {
		severity := controlRoomExecutiveRiskSeverity(plan.Risk)
		out.Items = append(out.Items, controlRoomExecutiveAttentionItem{
			ID:        plan.ID.String(),
			Kind:      "intervention",
			Severity:  severity,
			Domain:    firstNonEmptyIPBehavior(plan.Domain, "automation"),
			Title:     controlRoomExecutiveFailedActionTitle(plan),
			Reason:    "Automatic response failed",
			CreatedAt: formatTime(plan.UpdatedAt),
			Drilldown: controlRoomExecutivePlanDrilldown(plan),
		})
	}

	out.Total = out.Reviews + out.Approvals + out.Interventions
	sort.SliceStable(out.Items, func(i, j int) bool {
		left := controlRoomSeverityRank(out.Items[i].Severity)
		right := controlRoomSeverityRank(out.Items[j].Severity)
		if left == right {
			return out.Items[i].CreatedAt > out.Items[j].CreatedAt
		}
		return left > right
	})
	if len(out.Items) > 8 {
		out.Items = out.Items[:8]
	}
	return out, available
}

func controlRoomExecutiveProposedBlocks(
	ctx context.Context,
	store ipBlockProposalQueryStore,
	tenantID uuid.UUID,
) ([]storage.IPBlocklistEntry, int, error) {
	const pageSize = 500
	var out []storage.IPBlocklistEntry
	total := 0
	for offset := 0; ; offset += pageSize {
		rows, count, err := store.ListIPBlocklistEntries(
			ctx,
			storage.IPBlocklistEntryFilter{TenantID: tenantID, Status: "proposed"},
			pageSize,
			offset,
		)
		if err != nil {
			return nil, 0, err
		}
		if offset == 0 {
			total = count
		}
		out = append(out, rows...)
		if len(out) >= total || len(rows) == 0 {
			return out, total, nil
		}
	}
}

func controlRoomExecutiveProposalAlertID(reason string) (uuid.UUID, bool) {
	reason = strings.TrimSpace(reason)
	if !strings.HasPrefix(reason, "Correlation response:") {
		return uuid.Nil, false
	}
	for _, part := range strings.Split(strings.TrimPrefix(reason, "Correlation response:"), ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(key) != "alert_id" {
			continue
		}
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || id == uuid.Nil {
			return uuid.Nil, false
		}
		return id, true
	}
	return uuid.Nil, false
}

func controlRoomExecutiveProposalSeverity(
	proposal storage.IPBlocklistEntry,
	linkedAlerts map[uuid.UUID]storage.Alert,
) string {
	severity := controlRoomExecutiveScoreSeverity(proposal.Score)
	alertID, ok := controlRoomExecutiveProposalAlertID(proposal.Reason)
	if !ok {
		return severity
	}
	alert, ok := linkedAlerts[alertID]
	if !ok {
		return severity
	}
	alertSeverity := firstNonEmptyIPBehavior(alert.Severity, "medium")
	if controlRoomSeverityRank(alertSeverity) > controlRoomSeverityRank(severity) {
		return alertSeverity
	}
	return severity
}

func controlRoomNonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func controlRoomExecutiveScoreSeverity(score int) string {
	switch {
	case score >= 100:
		return "critical"
	case score >= 80:
		return "high"
	case score >= 50:
		return "medium"
	default:
		return "low"
	}
}

func nullableStringTrimmed(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return strings.TrimSpace(value.String)
}

func controlRoomExecutiveRiskSeverity(risk string) string {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

func controlRoomExecutiveFailedActionTitle(plan storage.ActionPlan) string {
	switch plan.Domain {
	case "firewall":
		return "Firewall response needs intervention"
	case "patch":
		return "Patch response needs intervention"
	case "remediation":
		return "Remediation needs intervention"
	case "webserver":
		return "Webserver response needs intervention"
	default:
		return "Automatic response needs intervention"
	}
}

func controlRoomExecutivePlanDrilldown(plan storage.ActionPlan) string {
	switch plan.Domain {
	case "firewall":
		return "/security/network?tab=blocks"
	case "patch":
		return "/infrastructure/patch"
	case "remediation":
		return "/compliance"
	case "webserver":
		return "/security/webservers"
	default:
		return "/control-room"
	}
}

func (s *Server) controlRoomExecutiveProtection(
	ctx context.Context,
	tenantID uuid.UUID,
	nodes []storage.Node,
	now time.Time,
) (controlRoomExecutiveProtection, bool) {
	out := controlRoomExecutiveProtection{GapTypes: []controlRoomExecutiveProtectionGap{}}
	services, err := s.store.ListNodeServicesForTenant(ctx, tenantID)
	if err != nil {
		s.logger.Warn("control room executive services", zap.Error(err))
		return out, false
	}
	isolation := newControlRoomIsolation(nodes, now)
	firewall := s.controlRoomFirewall(ctx, nodes, now)
	exposure := newControlRoomExposure(nodes, services, isolation, firewall)

	gaps := map[string]int{}
	for _, listener := range exposure.PublicListeners {
		out.Total++
		if strings.HasPrefix(listener.ExposureState, "protected_") {
			out.Protected++
			continue
		}
		out.Gaps++
		gaps[controlRoomExecutiveProtectionGapLabel(listener.ExposureState)]++
	}
	if out.Total > 0 {
		out.Percentage = float64(out.Protected) / float64(out.Total) * 100
	}
	for label, count := range gaps {
		out.GapTypes = append(out.GapTypes, controlRoomExecutiveProtectionGap{Type: label, Count: count})
	}
	sort.SliceStable(out.GapTypes, func(i, j int) bool {
		if out.GapTypes[i].Count == out.GapTypes[j].Count {
			return out.GapTypes[i].Type < out.GapTypes[j].Type
		}
		return out.GapTypes[i].Count > out.GapTypes[j].Count
	})
	return out, true
}

func controlRoomExecutiveProtectionGapLabel(state string) string {
	switch state {
	case "gap_unknown_firewall":
		return "Firewall state unknown"
	case "gap_firewall_off":
		return "Firewall disabled"
	case "gap_stale_firewall":
		return "Firewall data stale"
	case "gap_default_allow":
		return "Firewall default allow"
	case "gap_whitelist":
		return "Whitelist incomplete"
	default:
		return "Other protection gap"
	}
}

func controlRoomExecutiveDebugSummary(resp controlRoomExecutiveOverviewResponse) string {
	return fmt.Sprintf(
		"groups=%d/%d violations=%d handled=%d attention=%d protection=%d/%d",
		resp.Estate.GroupsHealthy,
		resp.Estate.GroupsTotal,
		resp.Violations.Total,
		resp.Response.HandledAutomatically,
		resp.Attention.Total,
		resp.Protection.Protected,
		resp.Protection.Total,
	)
}
