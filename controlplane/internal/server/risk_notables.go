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

type riskNotablesResponse struct {
	TenantID         string                 `json:"tenant_id"`
	Since            time.Time              `json:"since"`
	Until            time.Time              `json:"until"`
	Summary          riskNotablesSummary    `json:"summary"`
	Notables         []riskNotable          `json:"notables"`
	DependencyHealth []riskDependencyHealth `json:"dependency_health"`
	Citations        []riskCitation         `json:"citations"`
	Guardrails       []string               `json:"guardrails"`
}

type riskNotablesSummary struct {
	Total    int            `json:"total"`
	Critical int            `json:"critical"`
	High     int            `json:"high"`
	Medium   int            `json:"medium"`
	Low      int            `json:"low"`
	BySource map[string]int `json:"by_source"`
}

type riskNotable struct {
	ID                 string               `json:"id"`
	TenantID           string               `json:"tenant_id"`
	EntityType         string               `json:"entity_type"`
	EntityID           string               `json:"entity_id"`
	EntityLabel        string               `json:"entity_label,omitempty"`
	NodeID             string               `json:"node_id,omitempty"`
	SourceType         string               `json:"source_type"`
	SourceID           string               `json:"source_id"`
	Source             string               `json:"source,omitempty"`
	Severity           string               `json:"severity"`
	RiskScore          int                  `json:"risk_score"`
	RiskLevel          string               `json:"risk_level"`
	Title              string               `json:"title"`
	Summary            string               `json:"summary,omitempty"`
	State              string               `json:"state"`
	Disposition        string               `json:"disposition"`
	DetectionID        string               `json:"detection_id,omitempty"`
	DetectionHealth    string               `json:"detection_health"`
	DetectionInputs    []string             `json:"detection_inputs"`
	MITRE              []riskMITRETechnique `json:"mitre,omitempty"`
	CitationIDs        []string             `json:"citation_ids"`
	DispositionActions []string             `json:"disposition_actions,omitempty"`
	ObservedAt         time.Time            `json:"observed_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

type riskMITRETechnique struct {
	Tactic    string `json:"tactic"`
	Technique string `json:"technique"`
	Name      string `json:"name"`
}

type riskDependencyHealth struct {
	Source string `json:"source"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type riskCitation struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Table          string `json:"table"`
	SourceRecordID string `json:"source_record_id"`
}

type riskNotablesQuery struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
}

func (s *Server) handleRiskNotables(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"), 100)
	if err != nil {
		http.Error(w, "invalid limit", http.StatusBadRequest)
		return
	}
	nodeID, err := parseOptionalUUID(r.URL.Query().Get("node_id"))
	if err != nil {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	offset, err := parseOptionalInt(r.URL.Query().Get("offset"), 0)
	if err != nil {
		http.Error(w, "invalid offset", http.StatusBadRequest)
		return
	}
	scope, guardrails, err := investigationScopeFromRequest(
		r,
		r.URL.Query().Get("tenant_id"),
		r.URL.Query().Get("since"),
		r.URL.Query().Get("until"),
		limit,
		offset,
		100,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, scope.TenantID, roleViewer, roleOperator, roleInvestigator, roleAdmin) {
		return
	}
	resp, err := s.buildRiskNotables(r.Context(), riskNotablesQuery{
		TenantID: scope.TenantID,
		NodeID:   nodeID,
		Since:    scope.Since,
		Until:    scope.Until,
		Limit:    scope.Limit,
		Offset:   scope.Offset,
	}, guardrails)
	if err != nil {
		if writeTenantScopedNodeError(w, err) {
			return
		}
		s.logger.Error("risk notables", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) buildRiskNotables(ctx context.Context, q riskNotablesQuery, guardrails []string) (riskNotablesResponse, error) {
	if q.Limit <= 0 || q.Limit > maxListLimit {
		q.Limit = 100
	}
	resp := riskNotablesResponse{
		TenantID:   q.TenantID.String(),
		Since:      q.Since,
		Until:      q.Until,
		Guardrails: append([]string{"tenant_scoped", "read_only", "source_disposition_preserved", "disposition_feedback_applied"}, guardrails...),
		Summary: riskNotablesSummary{
			BySource: map[string]int{},
		},
	}
	if q.NodeID != uuid.Nil {
		if _, err := s.ensureNodeInTenant(ctx, q.TenantID, q.NodeID); err != nil {
			return resp, err
		}
	}
	nodes, _, err := s.store.ListNodes(ctx, q.TenantID, "", maxListLimit, 0)
	if err != nil {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "nodes", Status: "unavailable", Detail: err.Error()})
	} else {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "nodes", Status: "available"})
	}
	nodeByID := make(map[uuid.UUID]storage.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}

	alerts, _, err := s.store.ListAlerts(ctx, storage.AlertFilter{TenantID: q.TenantID, NodeID: q.NodeID, Since: &q.Since}, q.Limit, 0)
	if err != nil {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "alerts", Status: "unavailable", Detail: err.Error()})
	} else {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "alerts", Status: "available"})
		for _, alert := range alerts {
			if alert.OpenedAt.Before(q.Since) || alert.OpenedAt.After(q.Until) {
				continue
			}
			notable, citation := riskNotableFromAlert(alert, nodeByID)
			resp.Notables = append(resp.Notables, notable)
			resp.Citations = append(resp.Citations, citation)
		}
	}

	securityEvents, _, err := s.store.ListSecurityEvents(ctx, storage.SecurityEventFilter{TenantID: q.TenantID, NodeID: q.NodeID, Since: &q.Since, Until: &q.Until}, q.Limit, 0)
	if err != nil {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "security_events", Status: "unavailable", Detail: err.Error()})
	} else {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "security_events", Status: "available"})
		for _, event := range securityEvents {
			notable, citation := riskNotableFromSecurityEvent(event, nodeByID)
			resp.Notables = append(resp.Notables, notable)
			resp.Citations = append(resp.Citations, citation)
		}
	}

	if store, ok := s.store.(ipBehaviorFindingPageStore); ok {
		findings, _, err := store.ListIPBehaviorFindings(ctx, storage.IPBehaviorFindingFilter{TenantID: q.TenantID}, q.Limit, 0)
		if err != nil {
			resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "ip_behavior", Status: "unavailable", Detail: err.Error()})
		} else {
			resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "ip_behavior", Status: "available"})
			for _, finding := range findings {
				if q.NodeID != uuid.Nil && (!finding.NodeID.Valid || finding.NodeID.UUID != q.NodeID) {
					continue
				}
				if finding.LastSeenAt.Before(q.Since) || finding.LastSeenAt.After(q.Until) {
					continue
				}
				notable, citation := riskNotableFromIPBehaviorFinding(finding, nodeByID)
				resp.Notables = append(resp.Notables, notable)
				resp.Citations = append(resp.Citations, citation)
			}
		}
	} else {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "ip_behavior", Status: "unavailable", Detail: "store does not expose IP behavior findings"})
	}

	atRiskNodes, err := s.store.ListAtRiskNodes(ctx, q.TenantID, 49)
	if err != nil {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "node_health", Status: "unavailable", Detail: err.Error()})
	} else {
		resp.DependencyHealth = append(resp.DependencyHealth, riskDependencyHealth{Source: "node_health", Status: "available"})
		for _, row := range atRiskNodes {
			if q.NodeID != uuid.Nil && row.NodeID != q.NodeID {
				continue
			}
			if row.ComputedAt.Before(q.Since) || row.ComputedAt.After(q.Until) {
				continue
			}
			notable, citation := riskNotableFromAtRiskNode(row)
			resp.Notables = append(resp.Notables, notable)
			resp.Citations = append(resp.Citations, citation)
		}
	}

	for i := range resp.Notables {
		resp.Notables[i] = applyDispositionFeedback(resp.Notables[i])
	}

	sort.SliceStable(resp.Notables, func(i, j int) bool {
		if resp.Notables[i].RiskScore != resp.Notables[j].RiskScore {
			return resp.Notables[i].RiskScore > resp.Notables[j].RiskScore
		}
		return resp.Notables[i].ObservedAt.After(resp.Notables[j].ObservedAt)
	})
	if q.Offset > len(resp.Notables) {
		resp.Notables = []riskNotable{}
	} else if q.Offset > 0 {
		resp.Notables = resp.Notables[q.Offset:]
	}
	if len(resp.Notables) > q.Limit {
		resp.Notables = resp.Notables[:q.Limit]
		resp.Guardrails = append(resp.Guardrails, "notable_limit_applied")
	}
	resp.Summary = summarizeRiskNotables(resp.Notables)
	return resp, nil
}

func riskNotableFromAlert(alert storage.Alert, nodes map[uuid.UUID]storage.Node) (riskNotable, riskCitation) {
	nodeID, nodeLabel := nullNode(alert.NodeID, nodes)
	sourceID := alert.ID.String()
	severity := normalizeRiskSeverity(alert.Severity)
	entityType, entityID := entityFromAlert(alert, nodeID)
	notable := riskNotable{
		ID:                 "alert:" + sourceID,
		TenantID:           alert.TenantID.String(),
		EntityType:         entityType,
		EntityID:           entityID,
		EntityLabel:        nodeLabel,
		NodeID:             nodeID,
		SourceType:         "alert",
		SourceID:           sourceID,
		Source:             alert.Source,
		Severity:           severity,
		RiskScore:          riskScoreFromSeverity(severity),
		RiskLevel:          riskLevelFromSeverity(severity),
		Title:              alert.Title,
		Summary:            nullString(alert.Summary),
		State:              nonEmpty(alert.State, "open"),
		Disposition:        dispositionFromAlert(alert),
		DetectionID:        nonEmpty(alert.Source, "alert"),
		DetectionHealth:    "available",
		DetectionInputs:    []string{"alerts"},
		MITRE:              mitreForRisk(alert.Source, alert.Title, nullString(alert.Summary), alert.Context),
		CitationIDs:        []string{citationID("alerts", sourceID)},
		DispositionActions: []string{"alert_ack", "alert_resolve", "alert_disposition"},
		ObservedAt:         alert.OpenedAt,
		UpdatedAt:          alert.OpenedAt,
	}
	return notable, riskCitation{ID: notable.CitationIDs[0], Kind: "alert", Table: "alerts", SourceRecordID: "alerts:" + sourceID}
}

func riskNotableFromSecurityEvent(event storage.SecurityEvent, nodes map[uuid.UUID]storage.Node) (riskNotable, riskCitation) {
	nodeID, nodeLabel := nullNode(event.NodeID, nodes)
	sourceID := event.ID.String()
	severity := normalizeRiskSeverity(event.Severity)
	title := nonEmpty(stringFromMap(event.Details, "title"), event.EventType)
	summary := stringFromMap(event.Details, "summary")
	notable := riskNotable{
		ID:                 "security_event:" + sourceID,
		TenantID:           event.TenantID.String(),
		EntityType:         entityTypeForSecurityEvent(event),
		EntityID:           entityIDForSecurityEvent(event, nodeID),
		EntityLabel:        nodeLabel,
		NodeID:             nodeID,
		SourceType:         "security_event",
		SourceID:           sourceID,
		Source:             event.Source,
		Severity:           severity,
		RiskScore:          riskScoreFromSeverity(severity),
		RiskLevel:          riskLevelFromSeverity(severity),
		Title:              title,
		Summary:            summary,
		State:              "observed",
		Disposition:        "none",
		DetectionID:        event.EventType,
		DetectionHealth:    "available",
		DetectionInputs:    []string{"security_events"},
		MITRE:              mitreForRisk(event.EventType, title, summary, event.Details),
		CitationIDs:        []string{citationID("security_events", sourceID)},
		DispositionActions: []string{"create_case", "create_alert"},
		ObservedAt:         event.FiredAt,
		UpdatedAt:          event.FiredAt,
	}
	return notable, riskCitation{ID: notable.CitationIDs[0], Kind: "security_event", Table: "security_events", SourceRecordID: "security_events:" + sourceID}
}

func riskNotableFromIPBehaviorFinding(f storage.IPBehaviorFinding, nodes map[uuid.UUID]storage.Node) (riskNotable, riskCitation) {
	nodeID, nodeLabel := nullNode(f.NodeID, nodes)
	sourceID := f.ID.String()
	severity := normalizeRiskSeverity(f.Severity)
	entityID := strings.TrimSpace(f.SourceIP.String)
	if entityID == "" {
		entityID = f.DedupKey
	}
	notable := riskNotable{
		ID:                 "ip_behavior:" + sourceID,
		TenantID:           f.TenantID.String(),
		EntityType:         "ip",
		EntityID:           entityID,
		EntityLabel:        entityID,
		NodeID:             nodeID,
		SourceType:         "ip_behavior",
		SourceID:           sourceID,
		Source:             "ip_behavior",
		Severity:           severity,
		RiskScore:          clampInt(f.Score, riskScoreFromSeverity(severity), 100),
		RiskLevel:          riskLevelFromSeverity(severity),
		Title:              ipBehaviorTitle(f),
		Summary:            f.Reason,
		State:              nonEmpty(f.Status, "open"),
		Disposition:        dispositionFromFindingStatus(f.Status),
		DetectionID:        f.Category,
		DetectionHealth:    "available",
		DetectionInputs:    []string{"ip_behavior_findings", "behavioral_baselines"},
		MITRE:              mitreForRisk(f.Category, f.Reason, f.SourceIP.String, f.Evidence),
		CitationIDs:        []string{citationID("ip_behavior_findings", sourceID)},
		DispositionActions: []string{"behavioral_anomaly_resolve", "block_proposal"},
		ObservedAt:         f.LastSeenAt,
		UpdatedAt:          f.UpdatedAt,
	}
	if nodeLabel != "" && notable.EntityLabel == "" {
		notable.EntityLabel = nodeLabel
	}
	return notable, riskCitation{ID: notable.CitationIDs[0], Kind: "ip_behavior_finding", Table: "ip_behavior_findings", SourceRecordID: "ip_behavior_findings:" + sourceID}
}

func riskNotableFromAtRiskNode(row storage.AtRiskNodeRow) (riskNotable, riskCitation) {
	severity := severityFromHealthRisk(row.RiskLevel)
	sourceID := row.NodeID.String()
	title := fmt.Sprintf("Node health risk: %s", nonEmpty(row.RiskLevel, "unknown"))
	notable := riskNotable{
		ID:                 "node_health:" + sourceID,
		TenantID:           row.TenantID.String(),
		EntityType:         "node",
		EntityID:           sourceID,
		EntityLabel:        row.Hostname,
		NodeID:             sourceID,
		SourceType:         "node_health",
		SourceID:           sourceID,
		Source:             "predictive_health",
		Severity:           severity,
		RiskScore:          100 - row.Score,
		RiskLevel:          nonEmpty(row.RiskLevel, riskLevelFromSeverity(severity)),
		Title:              title,
		Summary:            stringFromMap(row.Components, "primary_component"),
		State:              "open",
		Disposition:        "none",
		DetectionID:        "health.predictive",
		DetectionHealth:    "available",
		DetectionInputs:    []string{"telemetry_metrics", "behavioral_baselines", "node_health_scores"},
		MITRE:              nil,
		CitationIDs:        []string{citationID("node_health_scores", sourceID)},
		DispositionActions: []string{"create_case", "create_maintenance_task"},
		ObservedAt:         row.ComputedAt,
		UpdatedAt:          row.ComputedAt,
	}
	return notable, riskCitation{ID: notable.CitationIDs[0], Kind: "node_health_score", Table: "node_health_scores", SourceRecordID: "node_health_scores:" + sourceID}
}

func summarizeRiskNotables(notables []riskNotable) riskNotablesSummary {
	out := riskNotablesSummary{Total: len(notables), BySource: map[string]int{}}
	for _, notable := range notables {
		out.BySource[notable.SourceType]++
		switch normalizeRiskSeverity(notable.Severity) {
		case "critical":
			out.Critical++
		case "high":
			out.High++
		case "medium":
			out.Medium++
		default:
			out.Low++
		}
	}
	return out
}

func applyDispositionFeedback(notable riskNotable) riskNotable {
	switch strings.ToLower(strings.TrimSpace(notable.Disposition)) {
	case "false_positive", "benign_positive":
		notable.State = "closed"
		capNotableRisk(&notable, 5, "low")
	case "resolved":
		notable.State = "resolved"
		capNotableRisk(&notable, 10, "low")
	case "suppressed":
		notable.State = "suppressed"
		capNotableRisk(&notable, 25, "low")
	case "accepted_risk":
		notable.State = "accepted_risk"
		capNotableRisk(&notable, 55, "medium")
	case "true_positive":
		if strings.EqualFold(notable.State, "resolved") {
			notable.State = "acked"
		}
	}
	return notable
}

func capNotableRisk(notable *riskNotable, maxScore int, severity string) {
	if notable.RiskScore > maxScore {
		notable.RiskScore = maxScore
	}
	notable.Severity = normalizeRiskSeverity(severity)
	notable.RiskLevel = riskLevelFromSeverity(severity)
}

func nullNode(nodeID uuid.NullUUID, nodes map[uuid.UUID]storage.Node) (string, string) {
	if !nodeID.Valid || nodeID.UUID == uuid.Nil {
		return "", ""
	}
	if node, ok := nodes[nodeID.UUID]; ok {
		return nodeID.UUID.String(), node.Hostname
	}
	return nodeID.UUID.String(), ""
}

func nullString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeRiskSeverity(sev string) string {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "critical", "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(sev))
	default:
		return "medium"
	}
}

func riskScoreFromSeverity(sev string) int {
	switch normalizeRiskSeverity(sev) {
	case "critical":
		return 95
	case "high":
		return 80
	case "medium":
		return 55
	default:
		return 25
	}
}

func riskLevelFromSeverity(sev string) string {
	switch normalizeRiskSeverity(sev) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium":
		return "medium"
	default:
		return "low"
	}
}

func severityFromHealthRisk(risk string) string {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium":
		return "medium"
	default:
		return "low"
	}
}

func dispositionFromAlert(alert storage.Alert) string {
	if disposition := strings.ToLower(strings.TrimSpace(stringFromMap(metadataMap(alert.Context["disposition"]), "value"))); disposition != "" {
		return disposition
	}
	return dispositionFromAlertState(alert.State)
}

func dispositionFromAlertState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "acked":
		return "acknowledged"
	case "resolved":
		return "resolved"
	default:
		return "open"
	}
}

func dispositionFromFindingStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "resolved", "suppressed", "false_positive", "accepted_risk":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "open"
	}
}

func entityFromAlert(alert storage.Alert, nodeID string) (string, string) {
	if nodeID != "" {
		return "node", nodeID
	}
	if ip := stringFromMap(alert.Context, "source_ip"); ip != "" {
		return "ip", ip
	}
	if user := stringFromMap(alert.Context, "user_name"); user != "" {
		return "user", user
	}
	return "alert", alert.ID.String()
}

func entityTypeForSecurityEvent(event storage.SecurityEvent) string {
	for _, key := range []string{"entity_type", "target_type"} {
		if value := stringFromMap(event.Details, key); value != "" {
			return value
		}
	}
	if event.NodeID.Valid {
		return "node"
	}
	if stringFromMap(event.Details, "source_ip") != "" {
		return "ip"
	}
	if stringFromMap(event.Details, "user_name") != "" {
		return "user"
	}
	return "event"
}

func entityIDForSecurityEvent(event storage.SecurityEvent, nodeID string) string {
	for _, key := range []string{"entity_id", "target_id", "source_ip", "user_name"} {
		if value := stringFromMap(event.Details, key); value != "" {
			return value
		}
	}
	if nodeID != "" {
		return nodeID
	}
	return event.ID.String()
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func ipBehaviorTitle(f storage.IPBehaviorFinding) string {
	category := strings.ReplaceAll(nonEmpty(f.Category, "ip_behavior"), "_", " ")
	source := strings.TrimSpace(f.SourceIP.String)
	if source == "" {
		source = f.DedupKey
	}
	return strings.TrimSpace(category + " " + source)
}

func mitreForRisk(parts ...any) []riskMITRETechnique {
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch v := part.(type) {
		case string:
			textParts = append(textParts, v)
		case map[string]any:
			for key, value := range v {
				textParts = append(textParts, key, fmt.Sprint(value))
			}
		}
	}
	text := strings.ToLower(strings.Join(textParts, " "))
	var out []riskMITRETechnique
	add := func(tactic, technique, name string) {
		for _, existing := range out {
			if existing.Technique == technique {
				return
			}
		}
		out = append(out, riskMITRETechnique{Tactic: tactic, Technique: technique, Name: name})
	}
	if strings.Contains(text, "credential") || strings.Contains(text, "brute") || strings.Contains(text, "auth failure") || strings.Contains(text, "login") {
		add("Credential Access", "T1110", "Brute Force")
	}
	if strings.Contains(text, "exfil") || strings.Contains(text, "bytes out") || strings.Contains(text, "bulk transfer") {
		add("Exfiltration", "T1041", "Exfiltration Over C2 Channel")
	}
	if strings.Contains(text, "web") || strings.Contains(text, "public-facing") || strings.Contains(text, "exploit") {
		add("Initial Access", "T1190", "Exploit Public-Facing Application")
	}
	if strings.Contains(text, "persistence") || strings.Contains(text, "startup") || strings.Contains(text, "service install") {
		add("Persistence", "T1543", "Create or Modify System Process")
	}
	if strings.Contains(text, "privilege") || strings.Contains(text, "sudo") || strings.Contains(text, "admin") {
		add("Privilege Escalation", "T1068", "Exploitation for Privilege Escalation")
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Technique < out[j].Technique })
	return out
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
