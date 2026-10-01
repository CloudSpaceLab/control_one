package server

import (
	"context"
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

type controlRoomExecutiveAttentionStore interface {
	GetExecutiveAttentionSummary(context.Context, uuid.UUID, time.Time, time.Time, int) (storage.ExecutiveAttentionSummary, error)
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

	response, responseAvailable := s.controlRoomExecutiveAutomaticResponse(ctx, tenantID, since, now)
	resp.Response = response
	resp.Availability.Response = responseAvailable

	attention, attentionAvailable := s.controlRoomExecutiveAttention(ctx, tenantID, since, now)
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
) (controlRoomExecutiveResponse, bool) {
	var out controlRoomExecutiveResponse
	store, ok := s.store.(controlRoomExecutiveAutomaticResponseStore)
	if !ok {
		return out, false
	}
	summary, err := store.GetAutomaticResponseSummary(ctx, tenantID, since, until, 8)
	if err != nil {
		s.logger.Warn("control room executive automatic responses", zap.Error(err))
		return out, false
	}
	out = controlRoomExecutiveResponse{
		HandledAutomatically: summary.HandledAutomatically,
		Blocked:              summary.Blocked,
		Contained:            summary.Contained,
		Remediated:           summary.Remediated,
		Failed:               summary.Failed,
		FailedCritical:       summary.FailedCritical,
	}
	return out, true
}

func (s *Server) controlRoomExecutiveAttention(
	ctx context.Context,
	tenantID uuid.UUID,
	since time.Time,
	until time.Time,
) (controlRoomExecutiveAttention, bool) {
	out := controlRoomExecutiveAttention{Items: []controlRoomExecutiveAttentionItem{}}
	store, ok := s.store.(controlRoomExecutiveAttentionStore)
	if !ok {
		return out, false
	}
	summary, err := store.GetExecutiveAttentionSummary(ctx, tenantID, since, until, 8)
	if err != nil {
		s.logger.Warn("control room executive attention", zap.Error(err))
		return out, false
	}
	out.Total = summary.Total
	out.Critical = summary.Critical
	out.Reviews = summary.Reviews
	out.Approvals = summary.Approvals
	out.Interventions = summary.Interventions
	out.Items = make([]controlRoomExecutiveAttentionItem, 0, len(summary.Items))
	for _, item := range summary.Items {
		out.Items = append(out.Items, controlRoomExecutiveAttentionItemFromStorage(item))
	}
	return out, true
}

func controlRoomExecutiveAttentionItemFromStorage(item storage.ExecutiveAttentionItem) controlRoomExecutiveAttentionItem {
	out := controlRoomExecutiveAttentionItem{
		ID:        item.ID.String(),
		Kind:      firstNonEmptyIPBehavior(item.Kind, "review"),
		Severity:  firstNonEmptyIPBehavior(item.Severity, "medium"),
		Domain:    firstNonEmptyIPBehavior(item.Domain, "alerts"),
		CreatedAt: formatTime(item.CreatedAt),
	}
	switch item.Source {
	case "alert":
		out.Title = firstNonEmptyIPBehavior(item.AlertTitle, "Alert requires review")
		out.Reason = strings.TrimSpace(item.AlertSummary)
		out.Drilldown = "/alerts?alert_id=" + item.ID.String()
	case "patch":
		out.Title = "Patch approval"
		if hostname := strings.TrimSpace(item.NodeHostname); hostname != "" {
			out.Title = "Patch " + hostname
		}
		out.Reason = firstNonEmptyIPBehavior(item.Mode, "patch deployment")
		out.Drilldown = "/infrastructure/patch"
	case "remediation":
		out.Title = "Remediation approval"
		if hostname := strings.TrimSpace(item.NodeHostname); hostname != "" {
			out.Title = "Remediate " + hostname
		}
		out.Reason = strings.TrimSpace(item.RuleID)
		out.Drilldown = "/compliance"
	case "network":
		out.Title = "Block " + strings.TrimSpace(item.IPCIDR)
		out.Reason = strings.TrimSpace(item.Reason)
		out.Drilldown = "/security/network?tab=approvals&proposal_id=" + item.ID.String()
	case "automatic_response":
		out.Title = controlRoomExecutiveFailedActionTitleFromDomain(item.ActionDomain)
		out.Reason = "Automatic response failed"
		out.Drilldown = controlRoomExecutivePlanDrilldownFromDomain(item.ActionDomain)
	default:
		out.Title = "Action requires attention"
		out.Reason = strings.TrimSpace(item.Reason)
		out.Drilldown = "/control-room"
	}
	return out
}

func controlRoomExecutiveFailedActionTitleFromDomain(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
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

func controlRoomExecutivePlanDrilldownFromDomain(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
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
