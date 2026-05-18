package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type controlRoomWebserverListStore interface {
	ListWebserverInstances(context.Context, uuid.UUID, uuid.UUID, int, int) ([]storage.WebserverInstance, int, error)
}

type controlRoomWebserverActionListStore interface {
	ListWebserverConfigActions(context.Context, uuid.UUID, uuid.UUID, int) ([]storage.WebserverConfigAction, error)
}

type controlRoomWebserverReceiptListStore interface {
	ListWebserverConfigReceipts(context.Context, uuid.UUID, uuid.UUID, int) ([]storage.WebserverConfigReceipt, error)
}

type controlRoomOverviewResponse struct {
	TenantID       string                    `json:"tenant_id"`
	GeneratedAt    string                    `json:"generated_at"`
	Period         string                    `json:"period"`
	Lanes          []controlRoomLane         `json:"lanes"`
	TopIncidents   []controlRoomIncident     `json:"top_incidents"`
	StaleWarnings  []controlRoomStaleWarning `json:"stale_warnings"`
	IPBehavior     controlRoomIPBehavior     `json:"ip_behavior"`
	Webservers     controlRoomWebservers     `json:"webservers"`
	Isolation      controlRoomIsolation      `json:"isolation"`
	Firewall       controlRoomFirewall       `json:"firewall"`
	PendingActions []controlRoomAction       `json:"pending_actions"`
}

type controlRoomLane struct {
	ID              string                     `json:"id"`
	Title           string                     `json:"title"`
	Tone            string                     `json:"tone"`
	Score           int                        `json:"score"`
	Summary         string                     `json:"summary"`
	PrimaryMetric   controlRoomMetric          `json:"primary_metric"`
	SecondaryMetric controlRoomMetric          `json:"secondary_metric"`
	Drilldown       string                     `json:"drilldown"`
	UpdatedAt       string                     `json:"updated_at"`
	Metrics         []controlRoomMetric        `json:"metrics"`
	Items           []controlRoomDrilldownItem `json:"items,omitempty"`
}

type controlRoomMetric struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	Tone      string `json:"tone"`
	Hint      string `json:"hint,omitempty"`
	Drilldown string `json:"drilldown,omitempty"`
}

type controlRoomDrilldownItem struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	Tone      string `json:"tone"`
	Hint      string `json:"hint,omitempty"`
	Drilldown string `json:"drilldown,omitempty"`
}

type controlRoomIncident struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	Source    string `json:"source"`
	Summary   string `json:"summary,omitempty"`
	Drilldown string `json:"drilldown,omitempty"`
	OpenedAt  string `json:"opened_at,omitempty"`
}

type controlRoomStaleWarning struct {
	ID        string `json:"id"`
	Tone      string `json:"tone"`
	Message   string `json:"message"`
	Drilldown string `json:"drilldown,omitempty"`
}

type controlRoomIPBehavior struct {
	RequestCount int64                              `json:"request_count"`
	BytesOut     int64                              `json:"bytes_out"`
	Countries    []storage.IPBehaviorCountrySummary `json:"countries"`
	Findings     []controlRoomIPFinding             `json:"findings"`
}

type controlRoomIPFinding struct {
	ID          string         `json:"id"`
	SourceIP    string         `json:"source_ip,omitempty"`
	CountryCode string         `json:"country_code,omitempty"`
	ASN         string         `json:"asn,omitempty"`
	Category    string         `json:"category"`
	Severity    string         `json:"severity"`
	Score       int            `json:"score"`
	Reason      string         `json:"reason"`
	Evidence    map[string]any `json:"evidence,omitempty"`
	LastSeenAt  string         `json:"last_seen_at"`
	Drilldown   string         `json:"drilldown"`
}

type controlRoomWebservers struct {
	Total        int                    `json:"total"`
	CaptureReady int                    `json:"capture_ready"`
	EnforceReady int                    `json:"enforce_ready"`
	Instances    []controlRoomWebserver `json:"instances"`
}

type controlRoomIsolation struct {
	Online       int                        `json:"online"`
	Whitelist    int                        `json:"whitelist"`
	Airgapped    int                        `json:"airgapped"`
	Protected    int                        `json:"protected"`
	WhitelistGap int                        `json:"whitelist_gaps"`
	Expired      int                        `json:"expired"`
	ExpiringSoon int                        `json:"expiring_soon"`
	Nodes        []controlRoomIsolationNode `json:"nodes"`
}

type controlRoomIsolationNode struct {
	ID                  string   `json:"id"`
	Hostname            string   `json:"hostname"`
	Mode                string   `json:"mode"`
	Active              bool     `json:"active"`
	Expired             bool     `json:"expired"`
	LocalOnly           bool     `json:"local_only"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	AllowedApplications []string `json:"allowed_applications,omitempty"`
	AllowlistCIDRs      []string `json:"allowlist_cidrs,omitempty"`
	UpdatedAt           string   `json:"updated_at,omitempty"`
}

type controlRoomFirewall struct {
	Enabled     int                       `json:"enabled"`
	Disabled    int                       `json:"disabled"`
	Unknown     int                       `json:"unknown"`
	DefaultDeny int                       `json:"default_deny"`
	Stale       int                       `json:"stale"`
	Nodes       []controlRoomFirewallNode `json:"nodes"`
}

type controlRoomFirewallNode struct {
	NodeID       string `json:"node_id"`
	Hostname     string `json:"hostname"`
	FirewallType string `json:"firewall_type,omitempty"`
	Known        bool   `json:"known"`
	Enabled      bool   `json:"enabled"`
	DefaultDeny  bool   `json:"default_deny"`
	Stale        bool   `json:"stale"`
	ObservedAt   string `json:"observed_at,omitempty"`
}

type controlRoomWebserver struct {
	ID           string                       `json:"id"`
	NodeID       string                       `json:"node_id"`
	Kind         string                       `json:"kind"`
	Version      string                       `json:"version,omitempty"`
	Service      string                       `json:"service"`
	ConfigPath   string                       `json:"config_path,omitempty"`
	LogPath      string                       `json:"log_path,omitempty"`
	ErrorLogPath string                       `json:"error_log_path,omitempty"`
	CaptureReady bool                         `json:"capture_ready"`
	EnforceReady bool                         `json:"enforce_ready"`
	VHosts       []map[string]any             `json:"vhosts,omitempty"`
	Capabilities map[string]any               `json:"capabilities,omitempty"`
	LastAction   *controlRoomWebserverAction  `json:"last_action,omitempty"`
	LastReceipt  *controlRoomWebserverReceipt `json:"last_receipt,omitempty"`
	ObservedAt   string                       `json:"observed_at"`
}

type controlRoomWebserverAction struct {
	ID           string         `json:"id"`
	JobID        string         `json:"job_id,omitempty"`
	Action       string         `json:"action"`
	Status       string         `json:"status"`
	Result       map[string]any `json:"result,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

type controlRoomWebserverReceipt struct {
	ID               string         `json:"id"`
	Action           string         `json:"action"`
	ValidationStatus string         `json:"validation_status"`
	ReloadStatus     string         `json:"reload_status"`
	RollbackRef      string         `json:"rollback_ref,omitempty"`
	ChecksumBefore   string         `json:"checksum_before,omitempty"`
	ChecksumAfter    string         `json:"checksum_after,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedAt        string         `json:"created_at"`
}

type controlRoomAction struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Tone      string `json:"tone"`
	Count     int    `json:"count"`
	Drilldown string `json:"drilldown"`
}

func (s *Server) handleControlRoomOverview(w http.ResponseWriter, r *http.Request) {
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
	tenantID, period, since, ok := parseControlRoomQuery(w, r)
	if !ok {
		return
	}
	resp := s.buildControlRoomOverview(r.Context(), tenantID, period, since, time.Now().UTC())
	writeJSON(w, http.StatusOK, resp)
}

func parseControlRoomQuery(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, time.Time, bool) {
	var tenantID uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("tenant_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return uuid.Nil, "", time.Time{}, false
		}
		tenantID = parsed
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	switch period {
	case "1h", "6h", "24h", "7d", "30d":
	default:
		period = "24h"
	}
	now := time.Now().UTC()
	since := now.Add(-periodDuration(period))
	return tenantID, period, since, true
}

func periodDuration(period string) time.Duration {
	switch period {
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func (s *Server) buildControlRoomOverview(ctx context.Context, tenantID uuid.UUID, period string, since, now time.Time) controlRoomOverviewResponse {
	resp := controlRoomOverviewResponse{
		TenantID:    tenantID.String(),
		GeneratedAt: formatTime(now),
		Period:      period,
	}

	nodes, nodeTotal, err := s.store.ListNodes(ctx, tenantID, "", 500, 0)
	if err != nil {
		s.logger.Warn("control room list nodes", zap.Error(err))
		resp.StaleWarnings = append(resp.StaleWarnings, controlRoomStaleWarning{
			ID: "nodes-unavailable", Tone: "warning", Message: "Node inventory is unavailable.", Drilldown: "/nodes",
		})
	}
	healthyNodes, staleNodes, offlineNodes := controlRoomNodeCounts(nodes, now)
	if nodeTotal == 0 {
		nodeTotal = len(nodes)
	}

	securityCounts, err := s.store.CountSecurityEvents(ctx, storage.SecurityEventFilter{TenantID: tenantID, Since: &since})
	if err != nil {
		s.logger.Warn("control room security counts", zap.Error(err))
	}
	healthCounts, err := s.store.CountOpenHealthIncidents(ctx, tenantID)
	if err != nil {
		s.logger.Warn("control room health counts", zap.Error(err))
	}
	alerts, _, err := s.store.ListAlerts(ctx, storage.AlertFilter{TenantID: tenantID, State: "open"}, 8, 0)
	if err != nil {
		s.logger.Warn("control room alerts", zap.Error(err))
	}

	services, err := s.store.ListNodeServicesForTenant(ctx, tenantID)
	if err != nil {
		s.logger.Warn("control room services", zap.Error(err))
	}
	serviceKinds := controlRoomServiceKinds(services)
	resp.Isolation = newControlRoomIsolation(nodes, now)
	resp.Firewall = s.controlRoomFirewall(ctx, nodes, now)
	publicServices := controlRoomPublicServices(services, resp.Isolation)

	activeBlocks, err := s.store.ListActiveBlocks(ctx, tenantID, 25, 0, false)
	if err != nil {
		s.logger.Warn("control room active blocks", zap.Error(err))
	}

	patchDeployments, err := s.store.ListPatchDeployments(ctx, tenantID, 25, 0)
	if err != nil {
		s.logger.Warn("control room patch deployments", zap.Error(err))
	}
	patchApprovals, _, err := s.store.ListPatchApprovals(ctx, storage.ListPatchApprovalsFilter{TenantID: tenantID, Status: storage.ApprovalStatusPending}, 25, 0)
	if err != nil {
		s.logger.Warn("control room patch approvals", zap.Error(err))
	}

	countries, findings := s.controlRoomIPBehavior(ctx, tenantID, since)
	resp.IPBehavior = newControlRoomIPBehavior(countries, findings)

	webservers := s.controlRoomWebservers(ctx, tenantID)
	resp.Webservers = webservers

	resp.Lanes = []controlRoomLane{
		newServerHealthLane(nodeTotal, healthyNodes, staleNodes, offlineNodes, healthCounts, resp.Isolation, now),
		newSecurityLane(securityCounts, alerts, now),
		newAppDBLane(nodes, services, serviceKinds, webservers, now),
		newExposureLane(nodes, services, publicServices, activeBlocks, webservers, resp.IPBehavior, resp.Isolation, resp.Firewall, now),
		newIPBehaviorLane(resp.IPBehavior, now),
		newPatchPostureLane(patchDeployments, patchApprovals, resp.Isolation, now),
	}
	resp.TopIncidents = append(resp.TopIncidents, controlRoomAlertIncidents(alerts)...)
	resp.TopIncidents = append(resp.TopIncidents, controlRoomIPIncidents(resp.IPBehavior.Findings)...)
	resp.TopIncidents = append(resp.TopIncidents, controlRoomPatchIncidents(patchDeployments)...)
	resp.TopIncidents = append(resp.TopIncidents, controlRoomNodeIncidents(nodes, now)...)
	sort.SliceStable(resp.TopIncidents, func(i, j int) bool {
		return controlRoomSeverityRank(resp.TopIncidents[i].Severity) > controlRoomSeverityRank(resp.TopIncidents[j].Severity)
	})
	if len(resp.TopIncidents) > 8 {
		resp.TopIncidents = resp.TopIncidents[:8]
	}
	resp.StaleWarnings = append(resp.StaleWarnings, controlRoomNodeWarnings(nodes, now)...)
	resp.StaleWarnings = append(resp.StaleWarnings, controlRoomIsolationWarnings(resp.Isolation, now)...)
	if len(resp.StaleWarnings) > 8 {
		resp.StaleWarnings = resp.StaleWarnings[:8]
	}
	resp.PendingActions = controlRoomPendingActions(activeBlocks, patchApprovals, findings, resp.Isolation)
	return resp
}

func (s *Server) controlRoomIPBehavior(ctx context.Context, tenantID uuid.UUID, since time.Time) ([]storage.IPBehaviorCountrySummary, []storage.IPBehaviorFinding) {
	var countries []storage.IPBehaviorCountrySummary
	if store, ok := s.store.(ipBehaviorQueryStore); ok {
		rows, err := store.ListIPBehaviorCountries(ctx, tenantID, since, "")
		if err != nil {
			s.logger.Warn("control room ip countries", zap.Error(err))
		} else {
			countries = rows
		}
	}
	var findings []storage.IPBehaviorFinding
	if store, ok := s.store.(ipBehaviorFindingPageStore); ok {
		resolved := false
		rows, _, err := store.ListIPBehaviorFindings(ctx, storage.IPBehaviorFindingFilter{TenantID: tenantID, Resolved: &resolved}, 10, 0)
		if err != nil {
			s.logger.Warn("control room ip findings", zap.Error(err))
		} else {
			findings = rows
		}
	}
	return countries, findings
}

func (s *Server) controlRoomWebservers(ctx context.Context, tenantID uuid.UUID) controlRoomWebservers {
	store, ok := s.store.(controlRoomWebserverListStore)
	if !ok {
		return controlRoomWebservers{}
	}
	rows, _, err := store.ListWebserverInstances(ctx, tenantID, uuid.Nil, 100, 0)
	if err != nil {
		s.logger.Warn("control room webservers", zap.Error(err))
		return controlRoomWebservers{}
	}
	out := controlRoomWebservers{Total: len(rows), Instances: make([]controlRoomWebserver, 0, len(rows))}
	for _, row := range rows {
		captureReady := capabilityAnyBool(row.Capabilities, "capture_supported", "capture") || row.AccessLogPath != ""
		enforceReady := capabilityAnyBool(row.Capabilities, "enforce_supported", "enforce", "blocklist_supported")
		if captureReady {
			out.CaptureReady++
		}
		if enforceReady {
			out.EnforceReady++
		}
		out.Instances = append(out.Instances, controlRoomWebserver{
			ID: row.ID.String(), NodeID: row.NodeID.String(), Kind: row.Kind, Service: row.ServiceName,
			Version: row.Version, ConfigPath: row.ConfigPath, LogPath: row.AccessLogPath, ErrorLogPath: row.ErrorLogPath,
			CaptureReady: captureReady, EnforceReady: enforceReady, VHosts: row.VHosts, Capabilities: row.Capabilities,
			LastAction:  s.controlRoomWebserverLastAction(ctx, tenantID, row.ID),
			LastReceipt: s.controlRoomWebserverLastReceipt(ctx, tenantID, row.ID),
			ObservedAt:  formatTime(row.ObservedAt),
		})
	}
	return out
}

func (s *Server) controlRoomFirewall(ctx context.Context, nodes []storage.Node, now time.Time) controlRoomFirewall {
	out := controlRoomFirewall{Nodes: make([]controlRoomFirewallNode, 0, len(nodes))}
	if s == nil || s.store == nil {
		out.Unknown = len(nodes)
		return out
	}
	for _, node := range nodes {
		row := controlRoomFirewallNode{NodeID: node.ID.String(), Hostname: node.Hostname}
		st, err := s.store.GetNodeFirewallState(ctx, node.ID)
		if err != nil || st == nil {
			out.Unknown++
			out.Nodes = append(out.Nodes, row)
			continue
		}
		row.Known = true
		row.Enabled = st.Enabled
		row.FirewallType = st.FirewallType
		row.DefaultDeny = firewallStateDefaultDeny(*st)
		row.ObservedAt = formatTime(st.ObservedAt)
		row.Stale = st.ObservedAt.IsZero() || now.Sub(st.ObservedAt.UTC()) > 30*time.Minute
		if st.Enabled {
			out.Enabled++
		} else {
			out.Disabled++
		}
		if row.DefaultDeny {
			out.DefaultDeny++
		}
		if row.Stale {
			out.Stale++
		}
		out.Nodes = append(out.Nodes, row)
	}
	sort.SliceStable(out.Nodes, func(i, j int) bool {
		leftRank := firewallNodeRank(out.Nodes[i])
		rightRank := firewallNodeRank(out.Nodes[j])
		if leftRank == rightRank {
			return out.Nodes[i].Hostname < out.Nodes[j].Hostname
		}
		return leftRank > rightRank
	})
	return out
}

func firewallNodeRank(node controlRoomFirewallNode) int {
	switch {
	case !node.Known:
		return 4
	case !node.Enabled:
		return 3
	case node.Stale:
		return 2
	case !node.DefaultDeny:
		return 1
	default:
		return 0
	}
}

func firewallStateDefaultDeny(st storage.NodeFirewallState) bool {
	if !st.Enabled {
		return false
	}
	combined := strings.ToLower(st.FirewallType)
	for _, rule := range st.Rules {
		combined += "\n" + strings.ToLower(rule.Raw)
		combined += "\n" + strings.ToLower(rule.Action+" "+rule.Direction+" "+rule.Port+" "+rule.Source+" "+rule.Comment)
	}
	for _, zone := range st.Zones {
		combined += "\n" + strings.ToLower(zone.Name+" "+strings.Join(zone.Services, " ")+" "+strings.Join(zone.Sources, " "))
	}
	switch {
	case strings.Contains(combined, "default: deny") && strings.Contains(combined, "incoming"):
		return true
	case strings.Contains(combined, "-p input drop") || strings.Contains(combined, "chain input (policy drop"):
		return true
	case strings.Contains(combined, "hook input") && strings.Contains(combined, "policy drop"):
		return true
	case strings.Contains(combined, "default action block") || strings.Contains(combined, "inbound connections that do not match a rule are blocked"):
		return true
	default:
		return false
	}
}

func (s *Server) controlRoomWebserverLastAction(ctx context.Context, tenantID, instanceID uuid.UUID) *controlRoomWebserverAction {
	store, ok := s.store.(controlRoomWebserverActionListStore)
	if !ok {
		return nil
	}
	rows, err := store.ListWebserverConfigActions(ctx, tenantID, instanceID, 1)
	if err != nil {
		s.logger.Warn("control room webserver actions", zap.Error(err))
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	row := rows[0]
	jobID := ""
	if row.JobID.Valid {
		jobID = row.JobID.UUID.String()
	}
	errMsg := ""
	if row.ErrorMessage.Valid {
		errMsg = row.ErrorMessage.String
	}
	return &controlRoomWebserverAction{
		ID: row.ID.String(), JobID: jobID, Action: row.Action, Status: row.Status,
		Result: row.Result, ErrorMessage: errMsg, CreatedAt: formatTime(row.CreatedAt), UpdatedAt: formatTime(row.UpdatedAt),
	}
}

func (s *Server) controlRoomWebserverLastReceipt(ctx context.Context, tenantID, instanceID uuid.UUID) *controlRoomWebserverReceipt {
	store, ok := s.store.(controlRoomWebserverReceiptListStore)
	if !ok {
		return nil
	}
	rows, err := store.ListWebserverConfigReceipts(ctx, tenantID, instanceID, 1)
	if err != nil {
		s.logger.Warn("control room webserver receipts", zap.Error(err))
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	row := rows[0]
	return &controlRoomWebserverReceipt{
		ID: row.ID.String(), Action: row.Action, ValidationStatus: row.ValidationStatus, ReloadStatus: row.ReloadStatus,
		RollbackRef: row.RollbackRef, ChecksumBefore: row.ChecksumBefore, ChecksumAfter: row.ChecksumAfter,
		Metadata: row.Metadata, CreatedAt: formatTime(row.CreatedAt),
	}
}

func newControlRoomIPBehavior(countries []storage.IPBehaviorCountrySummary, findings []storage.IPBehaviorFinding) controlRoomIPBehavior {
	out := controlRoomIPBehavior{Countries: countries, Findings: make([]controlRoomIPFinding, 0, len(findings))}
	for _, country := range countries {
		out.RequestCount += country.RequestCount
		out.BytesOut += country.BytesOut
	}
	for _, finding := range findings {
		src := ""
		if finding.SourceIP.Valid {
			src = finding.SourceIP.String
		}
		out.Findings = append(out.Findings, controlRoomIPFinding{
			ID: finding.ID.String(), SourceIP: src, CountryCode: finding.CountryCode, ASN: finding.ASN,
			Category: finding.Category, Severity: finding.Severity, Score: finding.Score, Reason: finding.Reason,
			Evidence: finding.Evidence, LastSeenAt: formatTime(finding.LastSeenAt),
			Drilldown: ipFindingDrilldown(src, finding.CountryCode),
		})
	}
	sort.SliceStable(out.Findings, func(i, j int) bool {
		if out.Findings[i].Score == out.Findings[j].Score {
			return controlRoomSeverityRank(out.Findings[i].Severity) > controlRoomSeverityRank(out.Findings[j].Severity)
		}
		return out.Findings[i].Score > out.Findings[j].Score
	})
	return out
}

func newServerHealthLane(total, healthy, stale, offline int, incidents storage.SecurityEventCounts, isolation controlRoomIsolation, now time.Time) controlRoomLane {
	intentionalOffline := isolation.Airgapped
	effectiveOffline := offline - intentionalOffline
	if effectiveOffline < 0 {
		effectiveOffline = 0
	}
	score := clampScore(100 - effectiveOffline*25 - stale*10 - incidents.Critical*30 - incidents.High*15)
	tone := scoreTone(score)
	if total == 0 {
		tone = "unknown"
	}
	return controlRoomLane{
		ID: "server-health", Title: "Server Health", Tone: tone, Score: score,
		Summary:         fmt.Sprintf("%d healthy, %d stale, %d offline, %d intentionally isolated", healthy, stale, effectiveOffline, intentionalOffline),
		PrimaryMetric:   controlRoomMetric{Label: "Healthy servers", Value: fmt.Sprintf("%d/%d", healthy, total), Tone: tone, Drilldown: "/nodes"},
		SecondaryMetric: controlRoomMetric{Label: "Open health incidents", Value: fmt.Sprintf("%d", incidents.Total), Tone: severityCountsTone(incidents), Drilldown: "/alerts?source=health"},
		Drilldown:       "/nodes", UpdatedAt: formatTime(now),
		Metrics: []controlRoomMetric{
			{Label: "Offline", Value: fmt.Sprintf("%d", effectiveOffline), Tone: countTone(effectiveOffline, "critical"), Drilldown: "/nodes?status=offline"},
			{Label: "Stale heartbeat", Value: fmt.Sprintf("%d", stale), Tone: countTone(stale, "warning"), Drilldown: "/nodes"},
			{Label: "Airgapped", Value: fmt.Sprintf("%d", intentionalOffline), Tone: "healthy", Drilldown: "/control-room/exposure"},
		},
	}
}

func newSecurityLane(counts storage.SecurityEventCounts, alerts []storage.Alert, now time.Time) controlRoomLane {
	tone := severityCountsTone(counts)
	return controlRoomLane{
		ID: "security", Title: "Security", Tone: tone, Score: clampScore(100 - counts.Critical*30 - counts.High*15 - counts.Medium*5),
		Summary:         fmt.Sprintf("%d security events and %d open alerts", counts.Total, len(alerts)),
		PrimaryMetric:   controlRoomMetric{Label: "Critical events", Value: fmt.Sprintf("%d", counts.Critical), Tone: countTone(counts.Critical, "critical"), Drilldown: "/alerts?severity=critical"},
		SecondaryMetric: controlRoomMetric{Label: "Open alerts", Value: fmt.Sprintf("%d", len(alerts)), Tone: countTone(len(alerts), "warning"), Drilldown: "/alerts"},
		Drilldown:       "/alerts", UpdatedAt: formatTime(now),
		Metrics: []controlRoomMetric{
			{Label: "High", Value: fmt.Sprintf("%d", counts.High), Tone: countTone(counts.High, "warning"), Drilldown: "/alerts?severity=high"},
			{Label: "Medium", Value: fmt.Sprintf("%d", counts.Medium), Tone: countTone(counts.Medium, "info"), Drilldown: "/alerts?severity=medium"},
		},
	}
}

func newAppDBLane(nodes []storage.Node, services []storage.NodeService, kinds map[string]int, webservers controlRoomWebservers, now time.Time) controlRoomLane {
	dbs := kinds["database"]
	apps := kinds["application"] + kinds["web"] + webservers.Total
	cache := kinds["cache"]
	missingCapture := webservers.Total - webservers.CaptureReady
	dbProbeErrors := countDBProbeErrors(services)
	webRoots := webRootCount(webservers)
	purposes := serverPurposeCounts(nodes)
	tone := countTone(missingCapture, "warning")
	if dbProbeErrors > 0 {
		tone = "critical"
	}
	if apps == 0 && dbs == 0 && cache == 0 {
		tone = "unknown"
	}
	return controlRoomLane{
		ID: "app-db-health", Title: "App/DB Health", Tone: tone, Score: clampScore(100 - missingCapture*15),
		Summary:         fmt.Sprintf("%d app/web, %d DB, %d cache services, %d inferred purposes", apps, dbs, cache, len(purposes)),
		PrimaryMetric:   controlRoomMetric{Label: "App/DB services", Value: fmt.Sprintf("%d", apps+dbs+cache), Tone: "info", Drilldown: "/nodes"},
		SecondaryMetric: controlRoomMetric{Label: "Missing web capture", Value: fmt.Sprintf("%d", missingCapture), Tone: countTone(missingCapture, "warning"), Drilldown: "/security/webservers"},
		Drilldown:       "/nodes", UpdatedAt: formatTime(now),
		Metrics: []controlRoomMetric{
			{Label: "Databases", Value: fmt.Sprintf("%d", dbs), Tone: countTone(dbProbeErrors, "critical"), Drilldown: "/data-security"},
			{Label: "Web roots", Value: fmt.Sprintf("%d", webRoots), Tone: "info", Drilldown: "/security/webservers"},
			{Label: "Capture ready", Value: fmt.Sprintf("%d/%d", webservers.CaptureReady, webservers.Total), Tone: countTone(missingCapture, "warning"), Drilldown: "/security/webservers"},
			{Label: "DB probe errors", Value: fmt.Sprintf("%d", dbProbeErrors), Tone: countTone(dbProbeErrors, "critical"), Drilldown: "/data-security"},
		},
		Items: appDBItems(nodes, services, webservers),
	}
}

func newExposureLane(nodes []storage.Node, services []storage.NodeService, publicServices int, activeBlocks []storage.ActiveBlock, webservers controlRoomWebservers, ip controlRoomIPBehavior, isolation controlRoomIsolation, firewall controlRoomFirewall, now time.Time) controlRoomLane {
	active := 0
	for _, block := range activeBlocks {
		if block.NodesApplied > 0 || block.NodesPending > 0 || block.NodesFailed > 0 {
			active++
		}
	}
	unprotectedWeb := webservers.Total - webservers.EnforceReady
	unprotectedCritical := unprotectedCriticalNodeCount(nodes, services, webservers, isolation, firewall)
	riskySources := len(ip.Findings)
	vhostCount := exposedVHostCount(webservers)
	protectedListeners := protectedPublicServiceCount(services, isolation, firewall)
	protectedNodeSet := isolationProtectedNodeSet(isolation)
	for nodeID := range firewallProtectedNodeSet(firewall) {
		protectedNodeSet[nodeID] = struct{}{}
	}
	protectedNodes := len(protectedNodeSet)
	unprotectedPublic := publicServices - protectedListeners
	if unprotectedPublic < 0 {
		unprotectedPublic = 0
	}
	tone := "healthy"
	if unprotectedPublic > 0 || unprotectedWeb > 0 || unprotectedCritical > 0 || isolation.WhitelistGap > 0 || firewall.Disabled > 0 || firewall.Unknown > 0 {
		tone = "warning"
	}
	if unprotectedCritical > 0 {
		tone = "critical"
	}
	return controlRoomLane{
		ID: "exposure", Title: "Current Exposure", Tone: tone, Score: clampScore(100 - unprotectedPublic*8 - unprotectedWeb*10 - unprotectedCritical*20 - isolation.WhitelistGap*10 - firewall.Disabled*8 - firewall.Unknown*5),
		Summary:         fmt.Sprintf("%d public listeners, %d protected by isolation/firewall, %d active blocks", publicServices, protectedNodes, active),
		PrimaryMetric:   controlRoomMetric{Label: "Public listeners", Value: fmt.Sprintf("%d", publicServices), Tone: countTone(publicServices, "warning"), Drilldown: "/connections"},
		SecondaryMetric: controlRoomMetric{Label: "Active blocks", Value: fmt.Sprintf("%d", active), Tone: countTone(active, "info"), Drilldown: "/security/network?tab=blocks"},
		Drilldown:       "/security/network", UpdatedAt: formatTime(now),
		Metrics: []controlRoomMetric{
			{Label: "Protected listeners", Value: fmt.Sprintf("%d", protectedListeners), Tone: "healthy", Drilldown: "/control-room/exposure"},
			{Label: "Web block ready", Value: fmt.Sprintf("%d/%d", webservers.EnforceReady, webservers.Total), Tone: countTone(unprotectedWeb, "warning"), Drilldown: "/security/webservers"},
			{Label: "Unprotected web", Value: fmt.Sprintf("%d", unprotectedWeb), Tone: countTone(unprotectedWeb, "warning"), Drilldown: "/security/webservers"},
			{Label: "Isolation protected", Value: fmt.Sprintf("%d", isolation.Protected), Tone: "healthy", Drilldown: "/control-room/exposure"},
			{Label: "Firewall default deny", Value: fmt.Sprintf("%d", firewall.DefaultDeny), Tone: countTone(firewall.Enabled-firewall.DefaultDeny, "info"), Drilldown: "/security/network?tab=firewall"},
			{Label: "Whitelist-only", Value: fmt.Sprintf("%d", isolation.Whitelist), Tone: "healthy", Drilldown: "/control-room/exposure"},
			{Label: "Whitelist gaps", Value: fmt.Sprintf("%d", isolation.WhitelistGap), Tone: countTone(isolation.WhitelistGap, "warning"), Drilldown: "/control-room/exposure"},
			{Label: "Airgapped", Value: fmt.Sprintf("%d", isolation.Airgapped), Tone: "healthy", Drilldown: "/control-room/exposure"},
			{Label: "Firewall unknown/off", Value: fmt.Sprintf("%d", firewall.Unknown+firewall.Disabled), Tone: countTone(firewall.Unknown+firewall.Disabled, "warning"), Drilldown: "/security/network?tab=firewall"},
			{Label: "Risky countries/ASNs", Value: fmt.Sprintf("%d", riskySources), Tone: countTone(riskySources, "warning"), Drilldown: "/security/network?tab=ip-behavior"},
			{Label: "Exposed vhosts", Value: fmt.Sprintf("%d", vhostCount), Tone: countTone(vhostCount, "info"), Drilldown: "/security/webservers"},
		},
		Items: exposureItems(nodes, services, activeBlocks, webservers, ip, isolation, firewall),
	}
}

func newIPBehaviorLane(ip controlRoomIPBehavior, now time.Time) controlRoomLane {
	score := 100
	tone := "healthy"
	if len(ip.Findings) > 0 {
		score = clampScore(100 - ip.Findings[0].Score)
		tone = severityTone(ip.Findings[0].Severity)
	}
	countryCount := len(ip.Countries)
	return controlRoomLane{
		ID: "ip-behavior", Title: "Connection/IP Behavior", Tone: tone, Score: score,
		Summary:         fmt.Sprintf("%d requests from %d countries", ip.RequestCount, countryCount),
		PrimaryMetric:   controlRoomMetric{Label: "Open anomalies", Value: fmt.Sprintf("%d", len(ip.Findings)), Tone: countTone(len(ip.Findings), "warning"), Drilldown: "/security/network?tab=ip-behavior"},
		SecondaryMetric: controlRoomMetric{Label: "Bytes out", Value: fmt.Sprintf("%d", ip.BytesOut), Tone: "info", Drilldown: "/security/network?tab=ip-behavior"},
		Drilldown:       "/security/network?tab=ip-behavior", UpdatedAt: formatTime(now),
		Metrics: []controlRoomMetric{
			{Label: "Countries", Value: fmt.Sprintf("%d", countryCount), Tone: "info", Drilldown: "/security/network?tab=ip-behavior"},
			{Label: "Requests", Value: fmt.Sprintf("%d", ip.RequestCount), Tone: "info", Drilldown: "/security/network?tab=ip-behavior"},
			{Label: "Top ASNs", Value: fmt.Sprintf("%d", len(topIPBehaviorValues(ip.Countries, "asn"))), Tone: "info", Drilldown: "/security/network?tab=ip-behavior"},
			{Label: "Apps/groups", Value: fmt.Sprintf("%d", len(topIPBehaviorValues(ip.Countries, "app_group"))), Tone: "info", Drilldown: "/security/network?tab=ip-behavior"},
		},
		Items: append(ipFindingItems(ip.Findings), ipCountryItems(ip.Countries)...),
	}
}

func newPatchPostureLane(deployments []storage.PatchDeployment, approvals []storage.PatchApproval, isolation controlRoomIsolation, now time.Time) controlRoomLane {
	failed := 0
	inProgress := 0
	for _, d := range deployments {
		if d.Status == "failed" || d.NodesFailed > 0 {
			failed++
		}
		if d.Status == "pending" || d.Status == "in_progress" || d.NodesPending > 0 {
			inProgress++
		}
	}
	overdueNodes := patchSummaryTotal(deployments, "overdue_nodes", "nodes_overdue", "overdue")
	criticalGaps := patchSummaryTotal(deployments, "critical_patch_gaps", "critical_gaps", "critical_packages", "critical_cves")
	maintenanceWindows := patchMaintenanceWindowCount(deployments, approvals)
	highRiskGroups := patchHighRiskGroups(deployments)
	tone := "healthy"
	if failed > 0 || criticalGaps > 0 {
		tone = "critical"
	} else if len(approvals) > 0 || inProgress > 0 || overdueNodes > 0 {
		tone = "warning"
	}
	return controlRoomLane{
		ID: "patch-posture", Title: "Patch Posture", Tone: tone, Score: clampScore(100 - failed*25 - len(approvals)*10 - criticalGaps*15 - overdueNodes*5),
		Summary:         fmt.Sprintf("%d deployments, %d pending approvals, %d isolated nodes", len(deployments), len(approvals), isolation.Airgapped+isolation.Whitelist),
		PrimaryMetric:   controlRoomMetric{Label: "Pending approvals", Value: fmt.Sprintf("%d", len(approvals)), Tone: countTone(len(approvals), "warning"), Drilldown: "/infrastructure/patch"},
		SecondaryMetric: controlRoomMetric{Label: "Failed deployments", Value: fmt.Sprintf("%d", failed), Tone: countTone(failed, "critical"), Drilldown: "/infrastructure/patch"},
		Drilldown:       "/infrastructure/patch", UpdatedAt: formatTime(now),
		Metrics: []controlRoomMetric{
			{Label: "In progress", Value: fmt.Sprintf("%d", inProgress), Tone: countTone(inProgress, "info"), Drilldown: "/infrastructure/patch"},
			{Label: "Overdue nodes", Value: fmt.Sprintf("%d", overdueNodes), Tone: countTone(overdueNodes, "warning"), Drilldown: "/infrastructure/patch"},
			{Label: "Critical gaps", Value: fmt.Sprintf("%d", criticalGaps), Tone: countTone(criticalGaps, "critical"), Drilldown: "/infrastructure/patch"},
			{Label: "Maintenance windows", Value: fmt.Sprintf("%d", maintenanceWindows), Tone: "info", Drilldown: "/infrastructure/patch"},
			{Label: "Airgapped/proxy", Value: fmt.Sprintf("%d", countPatchModes(deployments, "airgapped", "proxy")+isolation.Airgapped), Tone: "info", Drilldown: "/infrastructure/patch"},
			{Label: "Whitelist-only", Value: fmt.Sprintf("%d", isolation.Whitelist), Tone: "healthy", Drilldown: "/control-room/exposure"},
			{Label: "Target nodes", Value: fmt.Sprintf("%d", patchTargetNodes(deployments)), Tone: "info", Drilldown: "/infrastructure/patch"},
			{Label: "High-risk groups", Value: fmt.Sprintf("%d", len(highRiskGroups)), Tone: countTone(len(highRiskGroups), "warning"), Drilldown: "/infrastructure/patch"},
		},
		Items: patchItems(deployments, approvals),
	}
}

func controlRoomNodeCounts(nodes []storage.Node, now time.Time) (healthy, stale, offline int) {
	for _, node := range nodes {
		if node.LastSeenAt == nil {
			offline++
			continue
		}
		age := now.Sub(node.LastSeenAt.UTC())
		if age <= 5*time.Minute {
			healthy++
		} else if age <= 30*time.Minute {
			stale++
		} else {
			offline++
		}
	}
	return healthy, stale, offline
}

func controlRoomServiceKinds(services []storage.NodeService) map[string]int {
	kinds := map[string]int{}
	for _, svc := range services {
		kind := strings.ToLower(strings.TrimSpace(svc.ServiceKind))
		switch {
		case strings.Contains(kind, "db"), strings.Contains(kind, "postgres"), strings.Contains(kind, "mysql"), strings.Contains(kind, "mssql"), strings.Contains(kind, "oracle"), isDBPort(svc.Port):
			kinds["database"]++
		case strings.Contains(kind, "cache"), strings.Contains(kind, "redis"), strings.Contains(kind, "memcached"), svc.Port == 6379 || svc.Port == 11211:
			kinds["cache"]++
		case strings.Contains(kind, "http"), strings.Contains(kind, "web"), svc.Port == 80 || svc.Port == 443 || svc.Port == 8080 || svc.Port == 8443:
			kinds["web"]++
		default:
			kinds["application"]++
		}
	}
	return kinds
}

func controlRoomPublicServices(services []storage.NodeService, isolation controlRoomIsolation) int {
	count := 0
	isolated := isolationNodeModeSet(isolation, isolationModeAirgapped)
	for _, svc := range services {
		if _, ok := isolated[svc.NodeID.String()]; ok {
			continue
		}
		if isPublicListener(svc.ListenAddr) {
			count++
		}
	}
	return count
}

func protectedPublicServiceCount(services []storage.NodeService, isolation controlRoomIsolation, firewall controlRoomFirewall) int {
	protectedNodes := isolationProtectedNodeSet(isolation)
	for _, node := range firewall.Nodes {
		if node.Enabled && node.DefaultDeny && !node.Stale {
			protectedNodes[node.NodeID] = struct{}{}
		}
	}
	count := 0
	for _, svc := range services {
		if !isPublicListener(svc.ListenAddr) {
			continue
		}
		if _, ok := protectedNodes[svc.NodeID.String()]; ok {
			count++
		}
	}
	return count
}

func newControlRoomIsolation(nodes []storage.Node, now time.Time) controlRoomIsolation {
	out := controlRoomIsolation{Nodes: make([]controlRoomIsolationNode, 0, len(nodes))}
	for _, node := range nodes {
		posture := nodeIsolationPostureFromNode(node, now)
		switch posture.Mode {
		case isolationModeWhitelist:
			if posture.Active {
				out.Whitelist++
				if isolationPostureHasAllowlist(posture) {
					out.Protected++
				} else {
					out.WhitelistGap++
				}
			}
		case isolationModeAirgapped:
			if posture.Active {
				out.Airgapped++
				out.Protected++
			}
		default:
			out.Online++
		}
		if posture.Expired {
			out.Expired++
		}
		if posture.ExpiresAt != nil && posture.Active && posture.ExpiresAt.Sub(now) <= 2*time.Hour {
			out.ExpiringSoon++
		}
		nodeRow := controlRoomIsolationNode{
			ID:                  node.ID.String(),
			Hostname:            node.Hostname,
			Mode:                posture.Mode,
			Active:              posture.Active,
			Expired:             posture.Expired,
			LocalOnly:           posture.LocalOnly,
			Reason:              posture.Reason,
			AllowedApplications: posture.AllowedApplications,
			AllowlistCIDRs:      posture.AllowlistCIDRs,
			UpdatedAt:           labelString(node.Labels, isolationUpdatedAtLabel),
		}
		if posture.ExpiresAt != nil {
			nodeRow.ExpiresAt = posture.ExpiresAt.UTC().Format(time.RFC3339)
		}
		out.Nodes = append(out.Nodes, nodeRow)
	}
	sort.SliceStable(out.Nodes, func(i, j int) bool {
		leftRank := isolationModeRank(out.Nodes[i])
		rightRank := isolationModeRank(out.Nodes[j])
		if leftRank == rightRank {
			return out.Nodes[i].Hostname < out.Nodes[j].Hostname
		}
		return leftRank > rightRank
	})
	return out
}

func isolationModeRank(node controlRoomIsolationNode) int {
	switch {
	case node.Expired:
		return 4
	case node.Mode == isolationModeAirgapped:
		return 3
	case node.Mode == isolationModeWhitelist:
		return 2
	default:
		return 1
	}
}

func isolationNodeModeSet(isolation controlRoomIsolation, modes ...string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, mode := range modes {
		allowed[mode] = struct{}{}
	}
	out := map[string]struct{}{}
	for _, node := range isolation.Nodes {
		if !node.Active {
			continue
		}
		if _, ok := allowed[node.Mode]; ok {
			out[node.ID] = struct{}{}
		}
	}
	return out
}

func isolationProtectedNodeSet(isolation controlRoomIsolation) map[string]struct{} {
	out := map[string]struct{}{}
	for _, node := range isolation.Nodes {
		if !node.Active {
			continue
		}
		switch node.Mode {
		case isolationModeAirgapped:
			out[node.ID] = struct{}{}
		case isolationModeWhitelist:
			if isolationNodeHasAllowlist(node) {
				out[node.ID] = struct{}{}
			}
		}
	}
	return out
}

func isolationPostureHasAllowlist(posture nodeIsolationPosture) bool {
	return posture.LocalOnly || len(posture.AllowedApplications) > 0 || len(posture.AllowlistCIDRs) > 0
}

func isolationNodeHasAllowlist(node controlRoomIsolationNode) bool {
	return node.LocalOnly || len(node.AllowedApplications) > 0 || len(node.AllowlistCIDRs) > 0
}

func controlRoomAlertIncidents(alerts []storage.Alert) []controlRoomIncident {
	out := make([]controlRoomIncident, 0, len(alerts))
	for _, alert := range alerts {
		summary := ""
		if alert.Summary.Valid {
			summary = alert.Summary.String
		}
		out = append(out, controlRoomIncident{
			ID: alert.ID.String(), Title: alert.Title, Severity: alert.Severity, Source: firstNonEmptyIPBehavior(alert.Source, "alert"),
			Summary: summary, Drilldown: "/alerts", OpenedAt: formatTime(alert.OpenedAt),
		})
	}
	return out
}

func controlRoomIPIncidents(findings []controlRoomIPFinding) []controlRoomIncident {
	out := make([]controlRoomIncident, 0, len(findings))
	for _, finding := range findings {
		title := firstNonEmptyIPBehavior(finding.Category, "IP behavior anomaly")
		if finding.SourceIP != "" {
			title += " from " + finding.SourceIP
		}
		out = append(out, controlRoomIncident{
			ID: finding.ID, Title: title, Severity: finding.Severity, Source: "ip_behavior",
			Summary: finding.Reason, Drilldown: finding.Drilldown, OpenedAt: finding.LastSeenAt,
		})
	}
	return out
}

func controlRoomPatchIncidents(deployments []storage.PatchDeployment) []controlRoomIncident {
	var out []controlRoomIncident
	for _, d := range deployments {
		if d.Status != "failed" && d.NodesFailed == 0 {
			continue
		}
		out = append(out, controlRoomIncident{
			ID: d.ID.String(), Title: "Patch deployment needs attention", Severity: "high", Source: "patch",
			Summary: fmt.Sprintf("%d failed nodes in %s deployment", d.NodesFailed, d.Mode), Drilldown: "/infrastructure/patch", OpenedAt: formatTime(d.RequestedAt),
		})
	}
	return out
}

func controlRoomNodeIncidents(nodes []storage.Node, now time.Time) []controlRoomIncident {
	var out []controlRoomIncident
	for _, node := range nodes {
		if posture := nodeIsolationPostureFromNode(node, now); posture.Active && posture.Mode == isolationModeAirgapped {
			continue
		}
		if node.LastSeenAt != nil && now.Sub(node.LastSeenAt.UTC()) <= 30*time.Minute {
			continue
		}
		out = append(out, controlRoomIncident{
			ID: node.ID.String(), Title: node.Hostname + " is not reporting", Severity: "high", Source: "server_health",
			Summary: "Agent heartbeat is stale or missing.", Drilldown: "/nodes/" + node.ID.String(), OpenedAt: formatTime(now),
		})
	}
	return out
}

func controlRoomNodeWarnings(nodes []storage.Node, now time.Time) []controlRoomStaleWarning {
	var out []controlRoomStaleWarning
	for _, node := range nodes {
		if posture := nodeIsolationPostureFromNode(node, now); posture.Active && posture.Mode == isolationModeAirgapped {
			continue
		}
		if node.LastSeenAt == nil {
			out = append(out, controlRoomStaleWarning{ID: "node-never-seen-" + node.ID.String(), Tone: "warning", Message: node.Hostname + " has never reported a heartbeat.", Drilldown: "/nodes/" + node.ID.String()})
			continue
		}
		if age := now.Sub(node.LastSeenAt.UTC()); age > 15*time.Minute {
			out = append(out, controlRoomStaleWarning{ID: "node-stale-" + node.ID.String(), Tone: "warning", Message: fmt.Sprintf("%s last reported %s ago.", node.Hostname, age.Round(time.Minute)), Drilldown: "/nodes/" + node.ID.String()})
		}
	}
	return out
}

func controlRoomIsolationWarnings(isolation controlRoomIsolation, now time.Time) []controlRoomStaleWarning {
	var out []controlRoomStaleWarning
	for _, node := range isolation.Nodes {
		if node.Expired {
			out = append(out, controlRoomStaleWarning{
				ID: "isolation-expired-" + node.ID, Tone: "warning",
				Message:   node.Hostname + " isolation timer expired.",
				Drilldown: "/nodes/" + node.ID,
			})
			continue
		}
		if node.Active && node.ExpiresAt != "" {
			if expires, err := time.Parse(time.RFC3339, node.ExpiresAt); err == nil && expires.Sub(now) <= 2*time.Hour {
				out = append(out, controlRoomStaleWarning{
					ID: "isolation-expiring-" + node.ID, Tone: "info",
					Message:   node.Hostname + " isolation timer is close to expiry.",
					Drilldown: "/nodes/" + node.ID,
				})
			}
		}
		if node.Active && node.Mode == isolationModeWhitelist && !isolationNodeHasAllowlist(node) {
			out = append(out, controlRoomStaleWarning{
				ID: "isolation-whitelist-gap-" + node.ID, Tone: "warning",
				Message:   node.Hostname + " is whitelist-only without allowed applications or CIDRs.",
				Drilldown: "/nodes/" + node.ID,
			})
		}
	}
	return out
}

func controlRoomPendingActions(activeBlocks []storage.ActiveBlock, approvals []storage.PatchApproval, findings []storage.IPBehaviorFinding, isolation controlRoomIsolation) []controlRoomAction {
	proposed := 0
	for _, finding := range findings {
		if finding.Status == "open" || finding.Status == "proposed" {
			proposed++
		}
	}
	blocksPending := 0
	for _, block := range activeBlocks {
		if block.NodesPending > 0 || block.NodesFailed > 0 {
			blocksPending++
		}
	}
	actions := []controlRoomAction{
		{ID: "ip-findings", Label: "IP findings to review", Tone: countTone(proposed, "warning"), Count: proposed, Drilldown: "/security/network?tab=ip-behavior"},
		{ID: "block-enforcement", Label: "Block enforcement pending", Tone: countTone(blocksPending, "warning"), Count: blocksPending, Drilldown: "/security/network?tab=blocks"},
		{ID: "patch-approvals", Label: "Patch approvals", Tone: countTone(len(approvals), "warning"), Count: len(approvals), Drilldown: "/infrastructure/patch"},
		{ID: "isolation-posture", Label: "Isolation posture review", Tone: countTone(isolation.Expired+isolation.ExpiringSoon+isolation.WhitelistGap, "warning"), Count: isolation.Expired + isolation.ExpiringSoon + isolation.WhitelistGap, Drilldown: "/control-room/exposure"},
	}
	return actions
}

func ipFindingItems(findings []controlRoomIPFinding) []controlRoomDrilldownItem {
	limit := len(findings)
	if limit > 5 {
		limit = 5
	}
	out := make([]controlRoomDrilldownItem, 0, limit)
	for _, finding := range findings[:limit] {
		label := finding.SourceIP
		if label == "" {
			label = firstNonEmptyIPBehavior(finding.CountryCode, "unknown source")
		}
		out = append(out, controlRoomDrilldownItem{
			Label: label, Value: fmt.Sprintf("%d", finding.Score), Tone: severityTone(finding.Severity),
			Hint: firstNonEmptyIPBehavior(finding.Reason, finding.Category), Drilldown: finding.Drilldown,
		})
	}
	return out
}

func ipCountryItems(countries []storage.IPBehaviorCountrySummary) []controlRoomDrilldownItem {
	sorted := append([]storage.IPBehaviorCountrySummary(nil), countries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].RequestCount > sorted[j].RequestCount
	})
	limit := len(sorted)
	if limit > 3 {
		limit = 3
	}
	out := make([]controlRoomDrilldownItem, 0, limit)
	for _, country := range sorted[:limit] {
		label := firstNonEmptyIPBehavior(country.Country, country.CountryCode, "unknown country")
		auth := country.StatusCounts["401"] + country.StatusCounts["403"]
		hint := fmt.Sprintf("%d requests, %d unique IPs", country.RequestCount, country.UniqueSourceIPs)
		if auth > 0 {
			hint = fmt.Sprintf("%s, %d auth failures", hint, auth)
		}
		out = append(out, controlRoomDrilldownItem{
			Label: label, Value: fmt.Sprintf("%d", country.RequestCount), Tone: countTone(int(auth), "warning"),
			Hint: hint, Drilldown: ipFindingDrilldown("", country.CountryCode),
		})
	}
	return out
}

func appDBItems(nodes []storage.Node, services []storage.NodeService, webservers controlRoomWebservers) []controlRoomDrilldownItem {
	var out []controlRoomDrilldownItem
	for _, item := range serverPurposeItems(nodes) {
		if len(out) >= 5 {
			break
		}
		out = append(out, item)
	}
	for _, svc := range services {
		kind := strings.ToLower(strings.TrimSpace(svc.ServiceKind))
		if len(out) >= 5 {
			break
		}
		if kind == "" || kind == "unknown" {
			continue
		}
		if !isDBPort(svc.Port) && !strings.Contains(kind, "db") && !strings.Contains(kind, "postgres") && !strings.Contains(kind, "mysql") && !strings.Contains(kind, "mssql") && !strings.Contains(kind, "oracle") && !strings.Contains(kind, "cache") && !strings.Contains(kind, "http") && !strings.Contains(kind, "web") {
			continue
		}
		label := firstNonEmptyIPBehavior(nodeServiceTitle(svc), svc.Process, svc.ServiceKind)
		out = append(out, controlRoomDrilldownItem{
			Label: label, Value: fmt.Sprintf("%d", svc.Port), Tone: serviceTone(svc),
			Hint:      fmt.Sprintf("%s on %s", firstNonEmptyIPBehavior(svc.ServiceKind, "service"), firstNonEmptyIPBehavior(svc.ListenAddr, "unknown address")),
			Drilldown: "/nodes/" + svc.NodeID.String(),
		})
	}
	for _, web := range webservers.Instances {
		if len(out) >= 5 {
			break
		}
		if web.LogPath != "" {
			continue
		}
		out = append(out, controlRoomDrilldownItem{
			Label: firstNonEmptyIPBehavior(web.Kind, "webserver"), Value: "no capture", Tone: "warning",
			Hint: firstNonEmptyIPBehavior(web.ConfigPath, web.Service, web.NodeID), Drilldown: "/security/webservers",
		})
	}
	for _, item := range webRootItems(webservers) {
		if len(out) >= 5 {
			break
		}
		out = append(out, item)
	}
	return out
}

func exposureItems(nodes []storage.Node, services []storage.NodeService, activeBlocks []storage.ActiveBlock, webservers controlRoomWebservers, ip controlRoomIPBehavior, isolation controlRoomIsolation, firewall controlRoomFirewall) []controlRoomDrilldownItem {
	var out []controlRoomDrilldownItem
	airgapped := isolationNodeModeSet(isolation, isolationModeAirgapped)
	firewallProtected := firewallProtectedNodeSet(firewall)
	for _, svc := range services {
		if len(out) >= 4 {
			break
		}
		if _, ok := airgapped[svc.NodeID.String()]; ok {
			continue
		}
		if !isPublicListener(svc.ListenAddr) {
			continue
		}
		if _, ok := firewallProtected[svc.NodeID.String()]; ok {
			out = append(out, controlRoomDrilldownItem{
				Label:     firstNonEmptyIPBehavior(svc.Process, svc.ServiceKind, "listener"),
				Value:     "firewall",
				Tone:      "healthy",
				Hint:      fmt.Sprintf("%s protected by default-deny firewall", firstNonEmptyIPBehavior(svc.ServiceKind, "service")),
				Drilldown: "/nodes/" + svc.NodeID.String(),
			})
			continue
		}
		out = append(out, controlRoomDrilldownItem{
			Label:     firstNonEmptyIPBehavior(svc.Process, svc.ServiceKind, "listener"),
			Value:     fmt.Sprintf("%d", svc.Port),
			Tone:      publicServiceTone(svc),
			Hint:      fmt.Sprintf("%s on %s", firstNonEmptyIPBehavior(svc.ServiceKind, "service"), firstNonEmptyIPBehavior(svc.ListenAddr, "0.0.0.0")),
			Drilldown: "/nodes/" + svc.NodeID.String(),
		})
	}
	for _, finding := range ip.Findings {
		if len(out) >= 6 {
			break
		}
		if finding.CountryCode == "" && finding.ASN == "" {
			continue
		}
		out = append(out, controlRoomDrilldownItem{
			Label:     firstNonEmptyIPBehavior(finding.CountryCode, finding.ASN, "ip anomaly"),
			Value:     fmt.Sprintf("%d", finding.Score),
			Tone:      severityTone(finding.Severity),
			Hint:      firstNonEmptyIPBehavior(finding.Reason, finding.Category),
			Drilldown: finding.Drilldown,
		})
	}
	for _, block := range activeBlocks {
		if len(out) >= 6 {
			break
		}
		if block.NodesPending == 0 && block.NodesFailed == 0 {
			continue
		}
		out = append(out, controlRoomDrilldownItem{
			Label: block.EntityID, Value: fmt.Sprintf("%d pending", block.NodesPending+block.NodesFailed),
			Tone: countTone(block.NodesFailed, "critical"), Hint: "block enforcement", Drilldown: "/security/network?tab=blocks",
		})
	}
	for _, web := range webservers.Instances {
		if len(out) >= 6 {
			break
		}
		if webserverHasEnforcement(web) {
			continue
		}
		out = append(out, controlRoomDrilldownItem{
			Label: firstNonEmptyIPBehavior(web.Kind, "webserver"), Value: "no web block", Tone: "warning",
			Hint: firstNonEmptyIPBehavior(web.Service, web.ConfigPath, web.NodeID), Drilldown: "/security/webservers",
		})
	}
	for _, item := range isolationItems(isolation) {
		if len(out) >= 6 {
			break
		}
		out = append(out, item)
	}
	for _, item := range firewallGapItems(firewall) {
		if len(out) >= 6 {
			break
		}
		out = append(out, item)
	}
	for _, item := range unprotectedCriticalNodeItems(nodes, services, webservers, isolation, firewall) {
		if len(out) >= 6 {
			break
		}
		out = append(out, item)
	}
	for _, item := range exposedVHostItems(webservers) {
		if len(out) >= 6 {
			break
		}
		out = append(out, item)
	}
	return out
}

func isolationItems(isolation controlRoomIsolation) []controlRoomDrilldownItem {
	out := make([]controlRoomDrilldownItem, 0, len(isolation.Nodes))
	for _, node := range isolation.Nodes {
		if !node.Active && !node.Expired {
			continue
		}
		tone := "healthy"
		value := node.Mode
		hint := node.Reason
		if node.Expired {
			tone = "warning"
			value = "expired"
			hint = "Isolation timer expired"
		} else if node.Mode == isolationModeWhitelist && !isolationNodeHasAllowlist(node) {
			tone = "warning"
			value = "whitelist gap"
			hint = "Allowed applications or CIDRs are missing"
		} else if node.ExpiresAt != "" {
			hint = firstNonEmptyIPBehavior(hint, "expires "+node.ExpiresAt)
		}
		out = append(out, controlRoomDrilldownItem{
			Label: node.Hostname, Value: value, Tone: tone, Hint: hint, Drilldown: "/nodes/" + node.ID,
		})
	}
	return out
}

func firewallProtectedNodeSet(firewall controlRoomFirewall) map[string]struct{} {
	out := map[string]struct{}{}
	for _, node := range firewall.Nodes {
		if node.Enabled && node.DefaultDeny && !node.Stale {
			out[node.NodeID] = struct{}{}
		}
	}
	return out
}

func firewallGapItems(firewall controlRoomFirewall) []controlRoomDrilldownItem {
	out := make([]controlRoomDrilldownItem, 0, len(firewall.Nodes))
	for _, node := range firewall.Nodes {
		switch {
		case !node.Known:
			out = append(out, controlRoomDrilldownItem{
				Label: node.Hostname, Value: "unknown", Tone: "warning",
				Hint:      "firewall snapshot missing",
				Drilldown: "/nodes/" + node.NodeID,
			})
		case !node.Enabled:
			out = append(out, controlRoomDrilldownItem{
				Label: node.Hostname, Value: "off", Tone: "warning",
				Hint:      firstNonEmptyIPBehavior(node.FirewallType, "firewall disabled"),
				Drilldown: "/nodes/" + node.NodeID,
			})
		case node.Stale:
			out = append(out, controlRoomDrilldownItem{
				Label: node.Hostname, Value: "stale", Tone: "warning",
				Hint:      "firewall snapshot stale",
				Drilldown: "/nodes/" + node.NodeID,
			})
		}
	}
	return out
}

func patchItems(deployments []storage.PatchDeployment, approvals []storage.PatchApproval) []controlRoomDrilldownItem {
	var out []controlRoomDrilldownItem
	for _, approval := range approvals {
		if len(out) >= 3 {
			break
		}
		out = append(out, controlRoomDrilldownItem{
			Label: "Approval pending", Value: approval.Mode, Tone: "warning",
			Hint: "expires " + formatTime(approval.ExpiresAt), Drilldown: "/infrastructure/patch",
		})
	}
	for _, deployment := range deployments {
		if len(out) >= 6 {
			break
		}
		overdue := patchDeploymentSummaryInt(deployment, "overdue_nodes", "nodes_overdue", "overdue")
		if overdue <= 0 {
			continue
		}
		out = append(out, controlRoomDrilldownItem{
			Label: "Overdue patch nodes", Value: fmt.Sprintf("%d", overdue), Tone: "warning",
			Hint: deployment.Mode, Drilldown: "/infrastructure/patch",
		})
	}
	for _, deployment := range deployments {
		if len(out) >= 6 {
			break
		}
		gaps := patchDeploymentSummaryInt(deployment, "critical_patch_gaps", "critical_gaps", "critical_packages", "critical_cves")
		if gaps <= 0 {
			continue
		}
		out = append(out, controlRoomDrilldownItem{
			Label: "Critical patch gaps", Value: fmt.Sprintf("%d", gaps), Tone: "critical",
			Hint: deployment.Mode, Drilldown: "/infrastructure/patch",
		})
	}
	for _, deployment := range deployments {
		if len(out) >= 6 {
			break
		}
		if deployment.Status != "failed" && deployment.NodesFailed == 0 && deployment.NodesPending == 0 {
			continue
		}
		tone := "warning"
		value := fmt.Sprintf("%d pending", deployment.NodesPending)
		if deployment.Status == "failed" || deployment.NodesFailed > 0 {
			tone = "critical"
			value = fmt.Sprintf("%d failed", deployment.NodesFailed)
		}
		out = append(out, controlRoomDrilldownItem{
			Label: firstNonEmptyIPBehavior(deployment.Mode, "patch"), Value: value, Tone: tone,
			Hint: deployment.Status, Drilldown: "/infrastructure/patch",
		})
	}
	return out
}

func serverPurposeCounts(nodes []storage.Node) map[string]int {
	counts := map[string]int{}
	for _, node := range nodes {
		for _, purpose := range nodePurposeLabels(node) {
			counts[purpose]++
		}
	}
	return counts
}

func serverPurposeItems(nodes []storage.Node) []controlRoomDrilldownItem {
	counts := serverPurposeCounts(nodes)
	if len(counts) == 0 {
		return nil
	}
	type row struct {
		purpose string
		count   int
	}
	rows := make([]row, 0, len(counts))
	for purpose, count := range counts {
		rows = append(rows, row{purpose: purpose, count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count == rows[j].count {
			return rows[i].purpose < rows[j].purpose
		}
		return rows[i].count > rows[j].count
	})
	out := make([]controlRoomDrilldownItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, controlRoomDrilldownItem{
			Label: "Inferred server purpose", Value: fmt.Sprintf("%d", row.count), Tone: "info",
			Hint: displayPurpose(row.purpose), Drilldown: "/nodes",
		})
	}
	return out
}

func nodePurposeLabels(node storage.Node) []string {
	if node.Labels == nil {
		return nil
	}
	var out []string
	if purpose := strings.TrimSpace(labelString(node.Labels, "agent.primary_purpose", "primary_purpose", "server_purpose")); purpose != "" {
		out = append(out, normalizePurpose(purpose))
	}
	if raw, ok := node.Labels["agent.server_purposes"]; ok {
		for _, purpose := range stringSliceFromAny(raw) {
			purpose = normalizePurpose(purpose)
			if purpose != "" && !containsString(out, purpose) {
				out = append(out, purpose)
			}
		}
	}
	return out
}

func normalizePurpose(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func displayPurpose(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", " "))
	if value == "" {
		return "unknown purpose"
	}
	parts := strings.Fields(value)
	for i := range parts {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, " ")
}

func countDBProbeErrors(services []storage.NodeService) int {
	count := 0
	for _, svc := range services {
		if !isDBService(svc) || svc.ProbeStatus == nil {
			continue
		}
		if *svc.ProbeStatus >= 400 || *svc.ProbeStatus == 0 {
			count++
		}
	}
	return count
}

func isDBService(svc storage.NodeService) bool {
	kind := strings.ToLower(strings.TrimSpace(svc.ServiceKind))
	return isDBPort(svc.Port) || strings.Contains(kind, "db") || strings.Contains(kind, "postgres") ||
		strings.Contains(kind, "mysql") || strings.Contains(kind, "mssql") || strings.Contains(kind, "oracle") ||
		strings.Contains(kind, "mongo") || strings.Contains(kind, "cassandra")
}

func webRootCount(webservers controlRoomWebservers) int {
	seen := map[string]struct{}{}
	for _, web := range webservers.Instances {
		for _, vhost := range web.VHosts {
			root := vhostValue(vhost, "root", "document_root", "web_root", "app_root")
			if root == "" {
				continue
			}
			seen[root] = struct{}{}
		}
	}
	return len(seen)
}

func webRootItems(webservers controlRoomWebservers) []controlRoomDrilldownItem {
	var out []controlRoomDrilldownItem
	seen := map[string]struct{}{}
	for _, web := range webservers.Instances {
		for _, vhost := range web.VHosts {
			root := vhostValue(vhost, "root", "document_root", "web_root", "app_root")
			if root == "" {
				continue
			}
			if _, ok := seen[root]; ok {
				continue
			}
			seen[root] = struct{}{}
			out = append(out, controlRoomDrilldownItem{
				Label: "Web root", Value: "detected", Tone: "info",
				Hint: root, Drilldown: "/security/webservers",
			})
		}
	}
	return out
}

func exposedVHostCount(webservers controlRoomWebservers) int {
	count := 0
	for _, web := range webservers.Instances {
		count += len(web.VHosts)
	}
	return count
}

func exposedVHostItems(webservers controlRoomWebservers) []controlRoomDrilldownItem {
	var out []controlRoomDrilldownItem
	for _, web := range webservers.Instances {
		for _, vhost := range web.VHosts {
			name := vhostValue(vhost, "server_name", "vhost", "host", "name")
			app := vhostValue(vhost, "app", "application", "service")
			if name == "" && app == "" {
				continue
			}
			out = append(out, controlRoomDrilldownItem{
				Label: firstNonEmptyIPBehavior(name, app, web.Service, web.Kind), Value: web.Kind, Tone: "info",
				Hint:      firstNonEmptyIPBehavior(app, vhostValue(vhost, "root", "document_root", "app_root"), "exposed vhost"),
				Drilldown: "/security/webservers",
			})
		}
	}
	return out
}

func unprotectedCriticalNodeCount(nodes []storage.Node, services []storage.NodeService, webservers controlRoomWebservers, isolation controlRoomIsolation, firewall controlRoomFirewall) int {
	critical := criticalNodeIDs(nodes)
	public := publicServiceNodeIDs(services)
	enforced := webserverEnforcedNodeIDs(webservers)
	isolated := isolationProtectedNodeSet(isolation)
	for nodeID := range firewallProtectedNodeSet(firewall) {
		isolated[nodeID] = struct{}{}
	}
	count := 0
	for nodeID := range critical {
		if _, ok := isolated[nodeID]; ok {
			continue
		}
		if _, ok := public[nodeID]; !ok {
			continue
		}
		if _, ok := enforced[nodeID]; ok {
			continue
		}
		count++
	}
	return count
}

func unprotectedCriticalNodeItems(nodes []storage.Node, services []storage.NodeService, webservers controlRoomWebservers, isolation controlRoomIsolation, firewall controlRoomFirewall) []controlRoomDrilldownItem {
	critical := criticalNodeIDs(nodes)
	public := publicServiceNodeIDs(services)
	enforced := webserverEnforcedNodeIDs(webservers)
	isolated := isolationProtectedNodeSet(isolation)
	for nodeID := range firewallProtectedNodeSet(firewall) {
		isolated[nodeID] = struct{}{}
	}
	var out []controlRoomDrilldownItem
	for _, node := range nodes {
		nodeID := node.ID.String()
		if _, ok := isolated[nodeID]; ok {
			continue
		}
		if _, ok := critical[nodeID]; !ok {
			continue
		}
		if _, ok := public[nodeID]; !ok {
			continue
		}
		if _, ok := enforced[nodeID]; ok {
			continue
		}
		out = append(out, controlRoomDrilldownItem{
			Label: node.Hostname, Value: "unprotected", Tone: "critical",
			Hint:      firstNonEmptyIPBehavior(labelString(node.Labels, "criticality", "asset.criticality", "business_criticality"), "critical public listener"),
			Drilldown: "/nodes/" + node.ID.String(),
		})
	}
	return out
}

func criticalNodeIDs(nodes []storage.Node) map[string]struct{} {
	out := map[string]struct{}{}
	for _, node := range nodes {
		if nodeIsCritical(node) {
			out[node.ID.String()] = struct{}{}
		}
	}
	return out
}

func publicServiceNodeIDs(services []storage.NodeService) map[string]struct{} {
	out := map[string]struct{}{}
	for _, svc := range services {
		if isPublicListener(svc.ListenAddr) {
			out[svc.NodeID.String()] = struct{}{}
		}
	}
	return out
}

func webserverEnforcedNodeIDs(webservers controlRoomWebservers) map[string]struct{} {
	out := map[string]struct{}{}
	for _, web := range webservers.Instances {
		if web.EnforceReady {
			out[web.NodeID] = struct{}{}
		}
	}
	return out
}

func nodeIsCritical(node storage.Node) bool {
	value := strings.ToLower(labelString(node.Labels, "criticality", "asset.criticality", "business_criticality", "tier", "server_group", "cluster.role"))
	switch {
	case strings.Contains(value, "critical"), strings.Contains(value, "high"), strings.Contains(value, "tier0"), strings.Contains(value, "tier1"):
		return true
	case strings.Contains(value, "core"), strings.Contains(value, "payment"), strings.Contains(value, "banking"), strings.Contains(value, "domain_controller"):
		return true
	default:
		return false
	}
}

func patchSummaryTotal(deployments []storage.PatchDeployment, keys ...string) int {
	total := 0
	for _, deployment := range deployments {
		total += patchDeploymentSummaryInt(deployment, keys...)
	}
	return total
}

func patchDeploymentSummaryInt(deployment storage.PatchDeployment, keys ...string) int {
	for _, key := range keys {
		if n := int(int64FromServerAny(deployment.Summary[key])); n > 0 {
			return n
		}
	}
	return 0
}

func patchMaintenanceWindowCount(deployments []storage.PatchDeployment, approvals []storage.PatchApproval) int {
	seen := map[string]struct{}{}
	for _, approval := range approvals {
		if approval.WindowID != nil {
			seen[approval.WindowID.String()] = struct{}{}
		}
	}
	for _, deployment := range deployments {
		for _, key := range []string{"maintenance_window", "maintenance_window_id", "window_id"} {
			value := strings.TrimSpace(fmt.Sprint(deployment.Summary[key]))
			if value != "" && value != "<nil>" {
				seen[value] = struct{}{}
			}
		}
	}
	return len(seen)
}

func patchHighRiskGroups(deployments []storage.PatchDeployment) []string {
	seen := map[string]struct{}{}
	for _, deployment := range deployments {
		for _, key := range []string{"high_risk_server_groups", "server_groups", "critical_server_groups"} {
			for _, value := range stringSliceFromAny(deployment.Summary[key]) {
				value = strings.TrimSpace(value)
				if value != "" {
					seen[value] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func labelString(labels map[string]any, keys ...string) string {
	if labels == nil {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprint(labels[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func stringSliceFromAny(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return nil
	}
}

func vhostValue(vhost map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprint(vhost[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func topIPBehaviorValues(countries []storage.IPBehaviorCountrySummary, kind string) []string {
	seen := map[string]struct{}{}
	for _, country := range countries {
		var values []string
		switch kind {
		case "asn":
			values = country.TopASNs
		case "app_group":
			values = append(append([]string{}, country.TopApps...), country.ServerGroups...)
		}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func ipFindingDrilldown(sourceIP, countryCode string) string {
	if sourceIP != "" {
		return "/security/network?tab=ip-behavior&ip=" + sourceIP
	}
	if countryCode != "" {
		return "/security/network?tab=ip-behavior&country=" + countryCode
	}
	return "/security/network?tab=ip-behavior"
}

func nodeServiceTitle(svc storage.NodeService) string {
	if svc.ProbeTitle != nil {
		return strings.TrimSpace(*svc.ProbeTitle)
	}
	if svc.ProbeServer != nil {
		return strings.TrimSpace(*svc.ProbeServer)
	}
	return ""
}

func serviceTone(svc storage.NodeService) string {
	if svc.ProbeStatus != nil && *svc.ProbeStatus >= 500 {
		return "critical"
	}
	if svc.ProbeStatus != nil && *svc.ProbeStatus >= 400 {
		return "warning"
	}
	return "info"
}

func publicServiceTone(svc storage.NodeService) string {
	if isDBPort(svc.Port) {
		return "critical"
	}
	if svc.Port == 22 || svc.Port == 3389 || svc.Port == 5985 || svc.Port == 5986 {
		return "warning"
	}
	return "info"
}

func isPublicListener(addr string) bool {
	addr = strings.TrimSpace(addr)
	return addr == "" || strings.HasPrefix(addr, "0.0.0.0") || strings.HasPrefix(addr, "[::]") || strings.HasPrefix(addr, "::")
}

func webserverHasEnforcement(web controlRoomWebserver) bool {
	return web.EnforceReady
}

func countPatchModes(deployments []storage.PatchDeployment, modes ...string) int {
	want := map[string]struct{}{}
	for _, mode := range modes {
		want[strings.ToLower(strings.TrimSpace(mode))] = struct{}{}
	}
	count := 0
	for _, deployment := range deployments {
		if _, ok := want[strings.ToLower(strings.TrimSpace(deployment.Mode))]; ok {
			count++
		}
	}
	return count
}

func patchTargetNodes(deployments []storage.PatchDeployment) int {
	total := 0
	for _, deployment := range deployments {
		total += deployment.TargetNodeCount
	}
	return total
}

func isDBPort(port int) bool {
	switch port {
	case 1433, 1521, 3306, 5432, 5433, 5984, 6379, 9042, 27017:
		return true
	default:
		return false
	}
}

func capabilityBool(caps map[string]any, key string) bool {
	if caps == nil {
		return false
	}
	v, ok := caps[key]
	if !ok {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true") || b == "1" || strings.EqualFold(b, "yes")
	default:
		return false
	}
}

func capabilityAnyBool(caps map[string]any, keys ...string) bool {
	for _, key := range keys {
		if capabilityBool(caps, key) {
			return true
		}
	}
	return false
}

func severityCountsTone(counts storage.SecurityEventCounts) string {
	if counts.Critical > 0 {
		return "critical"
	}
	if counts.High > 0 {
		return "warning"
	}
	if counts.Medium > 0 || counts.Low > 0 {
		return "info"
	}
	return "healthy"
}

func severityTone(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return "critical"
	case "high":
		return "warning"
	case "medium":
		return "info"
	case "low", "watch":
		return "info"
	default:
		return "unknown"
	}
}

func controlRoomSeverityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low", "watch":
		return 1
	default:
		return 0
	}
}

func scoreTone(score int) string {
	switch {
	case score < 50:
		return "critical"
	case score < 80:
		return "warning"
	default:
		return "healthy"
	}
}

func countTone(count int, nonZeroTone string) string {
	if count <= 0 {
		return "healthy"
	}
	return nonZeroTone
}

func clampScore(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
