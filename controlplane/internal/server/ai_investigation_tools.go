package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type aiToolTraceEntry struct {
	Name       string `json:"name"`
	CitationID string `json:"citation_id,omitempty"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type aiToolExecution struct {
	Citation llm.Citation
	Payload  any
}

type aiInvestigationTool struct {
	Name        string
	Description string
	MinRole     string
	Schema      map[string]any
	Run         func(context.Context, aiToolContext, map[string]any) (aiToolExecution, error)
}

type aiToolContext struct {
	TenantID  uuid.UUID
	Principal *auth.Principal
	Now       time.Time
}

type entityLifecycleToolResponse struct {
	TenantID   string                    `json:"tenant_id"`
	EntityType string                    `json:"entity_type"`
	EntityID   string                    `json:"entity_id"`
	Since      time.Time                 `json:"since"`
	Until      time.Time                 `json:"until"`
	Items      []entityLifecycleToolItem `json:"items"`
	Citations  []entityLifecycleCitation `json:"citations"`
	Guardrails []string                  `json:"guardrails"`
}

type entityLifecycleToolItem struct {
	Timestamp   time.Time      `json:"ts"`
	Source      string         `json:"source"`
	Severity    string         `json:"severity,omitempty"`
	Actor       string         `json:"actor,omitempty"`
	Target      string         `json:"target,omitempty"`
	Summary     string         `json:"summary,omitempty"`
	RawID       string         `json:"raw_id,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CitationIDs []string       `json:"citation_ids,omitempty"`
}

type entityLifecycleCitation struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Source         string `json:"source"`
	SourceRecordID string `json:"source_record_id"`
}

func (s *Server) aiInvestigationTools() map[string]aiInvestigationTool {
	tools := []aiInvestigationTool{
		{
			Name:        "node_documentation",
			Description: "Return node summary, services, firewall state, health score, recent alerts, and top connections for one node.",
			MinRole:     roleViewer,
			Schema:      nodeIDToolSchema(),
			Run:         s.runNodeDocumentationTool,
		},
		{
			Name:        "node_alerts",
			Description: "Return recent alerts for one node.",
			MinRole:     roleViewer,
			Schema:      nodeIDToolSchema(),
			Run:         s.runNodeAlertsTool,
		},
		{
			Name:        "node_packages",
			Description: "Return installed package inventory for one node.",
			MinRole:     roleViewer,
			Schema:      nodeIDToolSchema(),
			Run:         s.runNodePackagesTool,
		},
		{
			Name:        "node_app_dependencies",
			Description: "Return application dependency and SBOM inventory for one node with app roots, ecosystems, manifests, PURLs, and scopes.",
			MinRole:     roleViewer,
			Schema:      nodeIDToolSchema(),
			Run:         s.runNodeAppDependenciesTool,
		},
		{
			Name:        "node_vulnerabilities",
			Description: "Return CVE/package/fixed-version vulnerability findings for one node with source-row citations and exploitability evidence.",
			MinRole:     roleViewer,
			Schema:      nodeVulnerabilitiesToolSchema(),
			Run:         s.runNodeVulnerabilitiesTool,
		},
		{
			Name:        "vulnerability_patch_plan",
			Description: "Build a proposal-only vulnerability patch plan with fixed-version evidence, policy gates, verification steps, and source-row citations.",
			MinRole:     roleOperator,
			Schema:      vulnerabilityPatchPlanToolSchema(),
			Run:         s.runVulnerabilityPatchPlanTool,
		},
		{
			Name:        "node_firewall",
			Description: "Return the latest firewall snapshot for one node.",
			MinRole:     roleViewer,
			Schema:      nodeIDToolSchema(),
			Run:         s.runNodeFirewallTool,
		},
		{
			Name:        "node_health",
			Description: "Return the latest predictive health score for one node.",
			MinRole:     roleViewer,
			Schema:      nodeIDToolSchema(),
			Run:         s.runNodeHealthTool,
		},
		{
			Name:        "operator_propose_action",
			Description: "Create a read-only operator action proposal. This never executes or queues a mutation.",
			MinRole:     roleOperator,
			Schema: map[string]any{
				"type":     "object",
				"required": []string{"action", "reason"},
				"properties": map[string]any{
					"action":  map[string]any{"type": "string"},
					"node_id": map[string]any{"type": "string"},
					"reason":  map[string]any{"type": "string"},
				},
			},
			Run: s.runOperatorProposalTool,
		},
		{
			Name:        "flow_delta",
			Description: "Return connection-rate and bandwidth deltas for a node over a time window.",
			MinRole:     roleViewer,
			Schema:      eventCaptureToolSchema(),
			Run:         s.runFlowDeltaTool,
		},
		{
			Name:        "file_growth_delta",
			Description: "Return fastest growing files for a node over a time window.",
			MinRole:     roleViewer,
			Schema:      eventCaptureToolSchema(),
			Run:         s.runFileGrowthDeltaTool,
		},
		{
			Name:        "resource_delta",
			Description: "Return CPU, memory, disk, and load deltas for a node over a time window.",
			MinRole:     roleViewer,
			Schema:      eventCaptureToolSchema(),
			Run:         s.runResourceDeltaTool,
		},
		{
			Name:        "log_tail",
			Description: "Return bounded, redacted log excerpts for a node over a time window.",
			MinRole:     roleViewer,
			Schema:      eventCaptureToolSchema(),
			Run:         s.runLogTailTool,
		},
		{
			Name:        "root_cause_findings",
			Description: "Return deterministic root-cause findings for a node and incident window.",
			MinRole:     roleViewer,
			Schema:      eventCaptureToolSchema(),
			Run:         s.runRootCauseFindingsTool,
		},
		{
			Name:        "events_query",
			Description: "Query normalized SIEM events for the current tenant with stable row references for citations.",
			MinRole:     roleViewer,
			Schema:      eventsQueryToolSchema(),
			Run:         s.runEventsQueryTool,
		},
		{
			Name:        "ingest_health",
			Description: "Return admin-gated durable ingest backlog, replay health, and cited tenant event_ingest_batches rows for investigation completeness checks.",
			MinRole:     roleAdmin,
			Schema:      dorisIngestHealthToolSchema(),
			Run:         s.runIngestHealthTool,
		},
		{
			Name:        "doris_ingest_health",
			Description: "Compatibility alias for ingest_health; returns durable ingest backlog and replay health for cited tenant event_ingest_batches rows.",
			MinRole:     roleAdmin,
			Schema:      dorisIngestHealthToolSchema(),
			Run:         s.runDorisIngestHealthTool,
		},
		{
			Name:        "timeline_build",
			Description: "Build a bounded investigation timeline for the current tenant from a correlation, connection, node, or entity pivot.",
			MinRole:     roleViewer,
			Schema:      timelineBuildToolSchema(),
			Run:         s.runTimelineBuildTool,
		},
		{
			Name:        "entity_lifecycle",
			Description: "Return a tenant-scoped lifecycle timeline for one entity with source-row citations.",
			MinRole:     roleViewer,
			Schema:      entityLifecycleToolSchema(),
			Run:         s.runEntityLifecycleTool,
		},
		{
			Name:        "db_audit_discovery",
			Description: "Return read-only database audit discovery, capture policy, DB service candidates, missing-access states, and evidence citations.",
			MinRole:     roleViewer,
			Schema:      dbAuditDiscoveryToolSchema(),
			Run:         s.runDBAuditDiscoveryTool,
		},
		{
			Name:        "risk_notables",
			Description: "Return tenant-scoped risk notables across alerts, security events, behavioral findings, and node health with citations and disposition state.",
			MinRole:     roleViewer,
			Schema:      riskNotablesToolSchema(),
			Run:         s.runRiskNotablesTool,
		},
		{
			Name:        "compliance_evidence_query",
			Description: "Return tenant-scoped compliance evaluator evidence and uploaded evidence records with source-row citations and privacy redaction markers.",
			MinRole:     roleViewer,
			Schema:      complianceEvidenceQueryToolSchema(),
			Run:         s.runComplianceEvidenceQueryTool,
		},
		{
			Name:        "coverage_explain",
			Description: "Explain product coverage truth by domain so unsupported or manual-evidence controls are never described as passing.",
			MinRole:     roleViewer,
			Schema:      coverageExplainToolSchema(),
			Run:         s.runCoverageExplainTool,
		},
		{
			Name:        "posture_drift_explain",
			Description: "Explain network-policy desired state, agent receipts, drift, missing controls, and rollback availability for posture enforcement.",
			MinRole:     roleViewer,
			Schema:      postureDriftExplainToolSchema(),
			Run:         s.runPostureDriftExplainTool,
		},
		{
			Name:        "incident_create",
			Description: "Create a tenant-scoped investigation incident record with cited evidence metadata. This does not execute enforcement.",
			MinRole:     roleOperator,
			Schema:      incidentCreateToolSchema(),
			Run:         s.runIncidentCreateTool,
		},
		{
			Name:        "hunt_save",
			Description: "Save a tenant-scoped hunt query for reuse by the investigation UI, preserving filters and cited pivots.",
			MinRole:     roleOperator,
			Schema:      huntSaveToolSchema(),
			Run:         s.runHuntSaveTool,
		},
		{
			Name:        "case_note_add",
			Description: "Append an audited note to an investigator case with cited evidence references. This is note-only and never executes actions.",
			MinRole:     roleInvestigator,
			Schema:      caseNoteAddToolSchema(),
			Run:         s.runCaseNoteAddTool,
		},
		{
			Name:        "operator_execute_action",
			Description: "Reserved for confirmed operator-mode execution. It is gated to admins and currently refuses all execution.",
			MinRole:     roleAdmin,
			Schema: map[string]any{
				"type":     "object",
				"required": []string{"action", "confirmed"},
				"properties": map[string]any{
					"action":    map[string]any{"type": "string"},
					"node_id":   map[string]any{"type": "string"},
					"confirmed": map[string]any{"type": "boolean"},
				},
			},
			Run: s.runOperatorExecuteTool,
		},
	}
	out := make(map[string]aiInvestigationTool, len(tools))
	for _, tool := range tools {
		out[tool.Name] = tool
	}
	return out
}

func (s *Server) aiToolSpecs() []llm.Tool {
	registry := s.aiInvestigationTools()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]llm.Tool, 0, len(names))
	for _, name := range names {
		tool := registry[name]
		out = append(out, llm.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.Schema})
	}
	return out
}

func nodeIDToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"node_id"},
		"properties": map[string]any{
			"node_id": map[string]any{"type": "string"},
		},
	}
}

func eventCaptureToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"node_id"},
		"properties": map[string]any{
			"node_id": map[string]any{"type": "string"},
			"since":   map[string]any{"type": "string", "description": "RFC3339 start time"},
			"until":   map[string]any{"type": "string", "description": "RFC3339 end time"},
			"q":       map[string]any{"type": "string", "description": "Optional search term for logs"},
		},
	}
}

func nodeVulnerabilitiesToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"node_id"},
		"properties": map[string]any{
			"node_id":          map[string]any{"type": "string"},
			"cve_id":           map[string]any{"type": "string"},
			"package":          map[string]any{"type": "string"},
			"severity":         map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low", "info", "unknown"}},
			"include_resolved": map[string]any{"type": "boolean"},
			"kev_only":         map[string]any{"type": "boolean"},
			"limit":            map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			"offset":           map[string]any{"type": "integer", "minimum": 0},
		},
	}
}

func eventsQueryToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id":        map[string]any{"type": "string"},
			"correlation_id": map[string]any{"type": "string"},
			"conn_id":        map[string]any{"type": "string"},
			"event_id":       map[string]any{"type": "string"},
			"raw_ref":        map[string]any{"type": "string"},
			"event_type":     map[string]any{"type": "string"},
			"event_types":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"severity":       map[string]any{"type": "string"},
			"parser_status":  map[string]any{"type": "string"},
			"search":         map[string]any{"type": "string"},
			"since":          map[string]any{"type": "string", "description": "RFC3339 start time; defaults to 24h before until"},
			"until":          map[string]any{"type": "string", "description": "RFC3339 end time; defaults to now"},
			"limit":          map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			"offset":         map[string]any{"type": "integer", "minimum": 0},
		},
	}
}

func timelineBuildToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"correlation_id": map[string]any{"type": "string"},
			"conn_id":        map[string]any{"type": "string"},
			"node_id":        map[string]any{"type": "string"},
			"entity_type":    map[string]any{"type": "string", "enum": []string{"ip", "user", "process", "file", "host", "node", "connection", "event", "raw_ref"}},
			"entity_id":      map[string]any{"type": "string"},
			"since":          map[string]any{"type": "string", "description": "RFC3339 start time; defaults to 24h before until"},
			"until":          map[string]any{"type": "string", "description": "RFC3339 end time; defaults to now"},
			"limit":          map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
		},
	}
}

func entityLifecycleToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"entity_type", "entity_id"},
		"properties": map[string]any{
			"entity_type": map[string]any{"type": "string", "enum": []string{"ip", "user", "process", "file", "host", "node", "domain", "hash", "uuid"}},
			"entity_id":   map[string]any{"type": "string"},
			"sources":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"since":       map[string]any{"type": "string", "description": "RFC3339 start time; defaults to 24h before until"},
			"until":       map[string]any{"type": "string", "description": "RFC3339 end time; defaults to now"},
			"limit":       map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
		},
	}
}

func dbAuditDiscoveryToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id": map[string]any{"type": "string"},
			"since":   map[string]any{"type": "string", "description": "RFC3339 start time; defaults to 24h before until"},
			"until":   map[string]any{"type": "string", "description": "RFC3339 end time; defaults to now"},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		},
	}
}

func riskNotablesToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id": map[string]any{"type": "string"},
			"since":   map[string]any{"type": "string", "description": "RFC3339 start time; defaults to 24h before until"},
			"until":   map[string]any{"type": "string", "description": "RFC3339 end time; defaults to now"},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
			"offset":  map[string]any{"type": "integer", "minimum": 0},
		},
	}
}

func complianceEvidenceQueryToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id":       map[string]any{"type": "string"},
			"rule_id":       map[string]any{"type": "string"},
			"framework":     map[string]any{"type": "string"},
			"control_ref":   map[string]any{"type": "string"},
			"evidence_type": map[string]any{"type": "string"},
			"passed":        map[string]any{"type": "boolean"},
			"since":         map[string]any{"type": "string", "description": "RFC3339 start time; defaults to 24h before until"},
			"until":         map[string]any{"type": "string", "description": "RFC3339 end time; defaults to now"},
			"limit":         map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
			"offset":        map[string]any{"type": "integer", "minimum": 0},
		},
	}
}

func coverageExplainToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"domain": map[string]any{"type": "string", "enum": []string{"telemetry", "parser", "detection", "compliance", "remediation", "vulnerability", "posture", "ai", "cases"}},
			"state":  map[string]any{"type": "string", "enum": []string{"supported", "partial", "raw_only", "unsupported", "manual_evidence", "stale", "exception", "not_applicable"}},
		},
	}
}

func postureDriftExplainToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id": map[string]any{"type": "string"},
			"since":   map[string]any{"type": "string", "description": "RFC3339 start time; defaults to 24h before until"},
			"until":   map[string]any{"type": "string", "description": "RFC3339 end time; defaults to now"},
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			"offset":  map[string]any{"type": "integer", "minimum": 0},
		},
	}
}

func incidentCreateToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"summary"},
		"properties": map[string]any{
			"summary":            map[string]any{"type": "string"},
			"node_id":            map[string]any{"type": "string"},
			"severity":           map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low", "info"}},
			"trigger_type":       map[string]any{"type": "string"},
			"trigger_event_type": map[string]any{"type": "string"},
			"dedup_key":          map[string]any{"type": "string"},
			"citations":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"evidence":           map[string]any{"type": "object"},
		},
	}
}

func huntSaveToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"name"},
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"query":       map[string]any{"type": "string"},
			"entity_type": map[string]any{"type": "string"},
			"shared":      map[string]any{"type": "boolean"},
			"filters":     map[string]any{"type": "object"},
			"citations":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"since":       map[string]any{"type": "string", "description": "Optional RFC3339 hunt window start"},
			"until":       map[string]any{"type": "string", "description": "Optional RFC3339 hunt window end"},
		},
	}
}

func caseNoteAddToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"case_id", "note"},
		"properties": map[string]any{
			"case_id":   map[string]any{"type": "string"},
			"note":      map[string]any{"type": "string"},
			"citations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
}

func (s *Server) executeAITool(ctx context.Context, principal *auth.Principal, tenantID uuid.UUID, call llm.ToolCall) (aiToolExecution, error) {
	tool, ok := s.aiInvestigationTools()[call.Name]
	if !ok {
		return aiToolExecution{}, fmt.Errorf("unknown tool %q", call.Name)
	}
	if !aiRoleAllows(principal, tool.MinRole) {
		return aiToolExecution{}, fmt.Errorf("tool %s requires role %s", call.Name, tool.MinRole)
	}
	if err := s.checkTenantAccess(ctx, principal, tenantID, tool.MinRole, roleAdmin); err != nil {
		return aiToolExecution{}, fmt.Errorf("tool %s tenant access denied: %w", call.Name, err)
	}
	now := time.Now().UTC()
	if s.aiClock != nil {
		now = s.aiClock().UTC()
	}
	return tool.Run(ctx, aiToolContext{TenantID: tenantID, Principal: principal, Now: now}, call.Input)
}

func aiRoleAllows(principal *auth.Principal, minRole string) bool {
	switch minRole {
	case roleViewer:
		return hasRole(principal, roleViewer) || hasRole(principal, roleOperator) || hasRole(principal, roleInvestigator) || hasRole(principal, roleAdmin)
	case roleOperator:
		return hasRole(principal, roleOperator) || hasRole(principal, roleAdmin)
	case roleInvestigator:
		return hasRole(principal, roleInvestigator) || hasRole(principal, roleAdmin)
	case roleAdmin:
		return hasRole(principal, roleAdmin)
	default:
		return hasRole(principal, minRole)
	}
}

func (s *Server) runNodeDocumentationTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID, err := s.nodeIDFromToolInput(ctx, tc.TenantID, input)
	if err != nil {
		return aiToolExecution{}, err
	}
	doc, err := s.buildNodeDocumentation(ctx, nodeID)
	if err != nil {
		return aiToolExecution{}, err
	}
	if doc == nil {
		return aiToolExecution{}, errors.New("node documentation not found")
	}
	return aiToolExecution{Citation: llm.Citation{Tool: "node_documentation", Label: doc.Node.Hostname, Detail: nodeID.String()}, Payload: doc}, nil
}

func (s *Server) runNodeAlertsTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID, err := s.nodeIDFromToolInput(ctx, tc.TenantID, input)
	if err != nil {
		return aiToolExecution{}, err
	}
	alerts, _, err := s.store.ListAlerts(ctx, storage.AlertFilter{TenantID: tc.TenantID, NodeID: nodeID}, 10, 0)
	if err != nil {
		return aiToolExecution{}, err
	}
	out := make([]alertResponse, 0, len(alerts))
	for _, alert := range alerts {
		out = append(out, newAlertResponse(alert))
	}
	return aiToolExecution{Citation: llm.Citation{Tool: "node_alerts", Label: "recent alerts", Detail: nodeID.String()}, Payload: out}, nil
}

func (s *Server) runNodePackagesTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID, err := s.nodeIDFromToolInput(ctx, tc.TenantID, input)
	if err != nil {
		return aiToolExecution{}, err
	}
	pkgs, err := s.store.ListNodePackages(ctx, nodeID)
	if err != nil {
		return aiToolExecution{}, err
	}
	out := make([]nodePackageResponse, 0, len(pkgs))
	for _, pkg := range pkgs {
		out = append(out, newNodePackageResponse(pkg))
	}
	return aiToolExecution{Citation: llm.Citation{Tool: "node_packages", Label: "installed packages", Detail: nodeID.String()}, Payload: out}, nil
}

func (s *Server) runNodeAppDependenciesTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID, err := s.nodeIDFromToolInput(ctx, tc.TenantID, input)
	if err != nil {
		return aiToolExecution{}, err
	}
	deps, err := s.store.ListNodeAppDependencies(ctx, nodeID)
	if err != nil {
		return aiToolExecution{}, err
	}
	out := make([]nodeAppDependencyResponse, 0, len(deps))
	for _, dep := range deps {
		out = append(out, newNodeAppDependencyResponse(dep))
	}
	return aiToolExecution{Citation: llm.Citation{Tool: "node_app_dependencies", Label: "application dependencies", Detail: nodeID.String()}, Payload: out}, nil
}

func (s *Server) runNodeFirewallTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID, err := s.nodeIDFromToolInput(ctx, tc.TenantID, input)
	if err != nil {
		return aiToolExecution{}, err
	}
	state, err := s.store.GetNodeFirewallState(ctx, nodeID)
	if err != nil {
		return aiToolExecution{}, err
	}
	return aiToolExecution{Citation: llm.Citation{Tool: "node_firewall", Label: "firewall state", Detail: nodeID.String()}, Payload: state}, nil
}

func (s *Server) runNodeHealthTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID, err := s.nodeIDFromToolInput(ctx, tc.TenantID, input)
	if err != nil {
		return aiToolExecution{}, err
	}
	score, err := s.store.GetNodeHealthScore(ctx, nodeID)
	if err != nil {
		return aiToolExecution{}, err
	}
	return aiToolExecution{Citation: llm.Citation{Tool: "node_health", Label: "health score", Detail: nodeID.String()}, Payload: score}, nil
}

func (s *Server) runOperatorProposalTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	action := strings.TrimSpace(stringFromToolInput(input, "action"))
	reason := strings.TrimSpace(stringFromToolInput(input, "reason"))
	if action == "" || reason == "" {
		return aiToolExecution{}, errors.New("action and reason are required")
	}
	nodeID := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	var parsedNodeID uuid.UUID
	if nodeID != "" {
		parsed, err := uuid.Parse(nodeID)
		if err != nil {
			return aiToolExecution{}, errors.New("invalid node_id")
		}
		if _, err := s.ensureNodeInTenant(ctx, tc.TenantID, parsed); err != nil {
			return aiToolExecution{}, err
		}
		parsedNodeID = parsed
	}
	approvalKind, approvalPath := approvalRouteForOperatorAction(action)
	proposal := map[string]any{
		"action":                action,
		"node_id":               nodeID,
		"reason":                reason,
		"requires_confirmation": true,
		"dry_run":               true,
		"execute_tool":          "operator_execute_action",
		"approval_kind":         approvalKind,
		"approval_path":         approvalPath,
		"created_at":            tc.Now.Format(time.RFC3339),
	}
	if backend := s.aiOperatorBackend(); backend != nil {
		metadata, _ := json.Marshal(map[string]any{
			"requires_confirmation": true,
			"execute_tool":          "operator_execute_action",
		})
		var createdBy uuid.UUID
		if tc.Principal != nil {
			createdBy = principalUserID(s, ctx, tc.Principal)
		}
		row, err := backend.CreateAIOperatorProposal(ctx, storage.CreateAIOperatorProposalParams{
			TenantID:     tc.TenantID,
			NodeID:       parsedNodeID,
			Action:       action,
			Reason:       reason,
			Status:       storage.AIOperatorProposalStatusProposed,
			DryRun:       true,
			ApprovalKind: approvalKind,
			ApprovalPath: approvalPath,
			SourceTool:   "operator_propose_action",
			Metadata:     metadata,
			CreatedBy:    createdBy,
		})
		if err != nil {
			return aiToolExecution{}, err
		}
		proposal["proposal_id"] = row.ID.String()
		proposal["status"] = row.Status
	}
	return aiToolExecution{Citation: llm.Citation{Tool: "operator_propose_action", Label: "operator proposal", Detail: action}, Payload: proposal}, nil
}

func (s *Server) runFlowDeltaTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	return s.runEventCaptureTool(ctx, tc, input, "flow_delta", "flow-delta", "flow delta")
}

func (s *Server) runFileGrowthDeltaTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	return s.runEventCaptureTool(ctx, tc, input, "file_growth_delta", "file-growth-delta", "file growth")
}

func (s *Server) runResourceDeltaTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	return s.runEventCaptureTool(ctx, tc, input, "resource_delta", "resource-delta", "resource delta")
}

func (s *Server) runLogTailTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	return s.runEventCaptureTool(ctx, tc, input, "log_tail", "log-tail", "log tail")
}

func (s *Server) runRootCauseFindingsTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	return s.runEventCaptureTool(ctx, tc, input, "root_cause_findings", "root-cause-findings", "root cause findings")
}

func (s *Server) runEventsQueryTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	if nodeID != "" {
		if _, err := s.validateToolNodeID(ctx, tc.TenantID, nodeID); err != nil {
			return aiToolExecution{}, err
		}
	}
	scope, guardrails, err := investigationScopeFromRequest(
		nil,
		tc.TenantID.String(),
		stringFromToolInput(input, "since"),
		stringFromToolInput(input, "until"),
		intFromToolInput(input, "limit"),
		intFromToolInput(input, "offset"),
		50,
	)
	if err != nil {
		return aiToolExecution{}, err
	}
	eventTypes := stringListFromToolInput(input, "event_types")
	if eventType := strings.TrimSpace(stringFromToolInput(input, "event_type")); eventType != "" {
		eventTypes = append(eventTypes, eventType)
	}
	rows, total, source, backendGuardrails, err := s.queryInvestigationEvents(ctx, doris.EventQueryParams{
		TenantID:      tc.TenantID.String(),
		NodeID:        nodeID,
		CorrelationID: strings.TrimSpace(stringFromToolInput(input, "correlation_id")),
		ConnID:        strings.TrimSpace(stringFromToolInput(input, "conn_id")),
		EventID:       strings.TrimSpace(stringFromToolInput(input, "event_id")),
		RawRef:        strings.TrimSpace(stringFromToolInput(input, "raw_ref")),
		EventTypes:    eventTypes,
		Severity:      strings.TrimSpace(stringFromToolInput(input, "severity")),
		ParserStatus:  strings.TrimSpace(stringFromToolInput(input, "parser_status")),
		Search:        strings.TrimSpace(stringFromToolInput(input, "search")),
		Since:         scope.Since,
		Until:         scope.Until,
		Limit:         scope.Limit,
		Offset:        scope.Offset,
	})
	if err != nil {
		return aiToolExecution{}, err
	}
	guardrails = append(guardrails, backendGuardrails...)
	items := make([]eventQueryItem, 0, len(rows))
	citations := make([]eventCitation, 0, len(rows))
	for _, row := range rows {
		item, citation := eventRowToResponse(row)
		items = append(items, item)
		citations = append(citations, citation)
	}
	if !s.dbQueryTextCaptureAllowed(ctx, tc.TenantID) {
		if redacted := redactDBQueryTextItems(items); redacted > 0 {
			guardrails = append(guardrails, "db query text redacted by tenant capture policy")
		}
	}
	payload := eventsQueryResponse{
		Source:     source,
		TenantID:   tc.TenantID.String(),
		Since:      scope.Since,
		Until:      scope.Until,
		Data:       items,
		Citations:  citations,
		Pagination: newPaginationMeta(total, scope.Limit, scope.Offset, len(items)),
		Guardrails: guardrails,
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "events_query", Label: "normalized events", Detail: fmt.Sprintf("%d rows", len(items))},
		Payload:  payload,
	}, nil
}

func (s *Server) runTimelineBuildTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeID := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	if nodeID != "" {
		if _, err := s.validateToolNodeID(ctx, tc.TenantID, nodeID); err != nil {
			return aiToolExecution{}, err
		}
	}
	entityType := strings.TrimSpace(stringFromToolInput(input, "entity_type"))
	entityID := strings.TrimSpace(stringFromToolInput(input, "entity_id"))
	if (entityType == "node" || entityType == "host") && entityID != "" {
		if _, err := s.validateToolNodeID(ctx, tc.TenantID, entityID); err != nil {
			return aiToolExecution{}, err
		}
	}
	if strings.TrimSpace(stringFromToolInput(input, "correlation_id")) == "" &&
		strings.TrimSpace(stringFromToolInput(input, "conn_id")) == "" &&
		nodeID == "" &&
		(entityType == "" || entityID == "") {
		return aiToolExecution{}, errors.New("timeline requires correlation_id, conn_id, node_id, or entity_type + entity_id")
	}
	scope, guardrails, err := investigationScopeFromRequest(
		nil,
		tc.TenantID.String(),
		stringFromToolInput(input, "since"),
		stringFromToolInput(input, "until"),
		intFromToolInput(input, "limit"),
		0,
		100,
	)
	if err != nil {
		return aiToolExecution{}, err
	}
	entityType, entityID, err = normalizeTimelineEntityScope(tc.TenantID, entityType, entityID)
	if err != nil {
		return aiToolExecution{}, err
	}
	rows, source, backendGuardrails, err := s.buildInvestigationTimeline(ctx, doris.TimelineBuildParams{
		TenantID:      tc.TenantID.String(),
		CorrelationID: strings.TrimSpace(stringFromToolInput(input, "correlation_id")),
		NodeID:        nodeID,
		ConnID:        strings.TrimSpace(stringFromToolInput(input, "conn_id")),
		EntityType:    entityType,
		EntityID:      entityID,
		Since:         scope.Since,
		Until:         scope.Until,
		Limit:         scope.Limit,
	})
	if err != nil {
		return aiToolExecution{}, err
	}
	guardrails = append(guardrails, backendGuardrails...)
	items := make([]timelineItemResponse, 0, len(rows))
	citations := make([]eventCitation, 0, len(rows))
	for _, row := range rows {
		item, citation := timelineRowToResponse(row)
		items = append(items, item)
		citations = append(citations, citation)
	}
	if !s.dbQueryTextCaptureAllowed(ctx, tc.TenantID) {
		if redacted := redactTimelineDBQueryTextItems(items); redacted > 0 {
			guardrails = append(guardrails, "db query text redacted by tenant capture policy")
		}
	}
	scopeMap := map[string]string{}
	for key, value := range map[string]string{
		"correlation_id": strings.TrimSpace(stringFromToolInput(input, "correlation_id")),
		"conn_id":        strings.TrimSpace(stringFromToolInput(input, "conn_id")),
		"node_id":        nodeID,
		"entity_type":    entityType,
		"entity_id":      entityID,
	} {
		if value != "" {
			scopeMap[key] = value
		}
	}
	payload := timelineBuildResponse{
		Source:     source,
		TenantID:   tc.TenantID.String(),
		Since:      scope.Since,
		Until:      scope.Until,
		Scope:      scopeMap,
		Items:      items,
		Citations:  citations,
		Guardrails: guardrails,
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "timeline_build", Label: "investigation timeline", Detail: fmt.Sprintf("%d rows", len(items))},
		Payload:  payload,
	}, nil
}

func (s *Server) runEntityLifecycleTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	entityType := strings.ToLower(strings.TrimSpace(stringFromToolInput(input, "entity_type")))
	entityID := strings.TrimSpace(stringFromToolInput(input, "entity_id"))
	if entityType == "" || entityID == "" {
		return aiToolExecution{}, errors.New("entity_type and entity_id are required")
	}
	if (entityType == "node" || entityType == "host") && entityID != "" {
		if _, err := s.validateToolNodeID(ctx, tc.TenantID, entityID); err != nil {
			return aiToolExecution{}, err
		}
	}
	backend := s.investigateBackend()
	if backend == nil {
		return aiToolExecution{}, errors.New("investigate backend unavailable")
	}
	scope, guardrails, err := investigationScopeFromRequest(
		nil,
		tc.TenantID.String(),
		stringFromToolInput(input, "since"),
		stringFromToolInput(input, "until"),
		intFromToolInput(input, "limit"),
		0,
		100,
	)
	if err != nil {
		return aiToolExecution{}, err
	}
	since := scope.Since
	until := scope.Until
	items, err := backend.EntityLifecycle(ctx, storage.LifecycleFilter{
		TenantID:   tc.TenantID,
		EntityType: entityType,
		EntityID:   entityID,
		Since:      &since,
		Until:      &until,
		Sources:    stringListFromToolInput(input, "sources"),
	}, scope.Limit)
	if err != nil {
		return aiToolExecution{}, err
	}
	resp := entityLifecycleToolResponse{
		TenantID:   tc.TenantID.String(),
		EntityType: entityType,
		EntityID:   entityID,
		Since:      scope.Since,
		Until:      scope.Until,
		Items:      make([]entityLifecycleToolItem, 0, len(items)),
		Citations:  make([]entityLifecycleCitation, 0, len(items)),
		Guardrails: append([]string{"tenant_scoped", "read_only", "source_rows_cited"}, guardrails...),
	}
	for i, item := range items {
		citation := lifecycleCitationFromItem(i, item)
		resp.Items = append(resp.Items, entityLifecycleToolItem{
			Timestamp:   item.Timestamp,
			Source:      item.Source,
			Severity:    item.Severity,
			Actor:       item.Actor,
			Target:      item.Target,
			Summary:     item.Summary,
			RawID:       item.RawID,
			Metadata:    item.Metadata,
			CitationIDs: []string{citation.ID},
		})
		resp.Citations = append(resp.Citations, citation)
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "entity_lifecycle", Label: "entity lifecycle", Detail: fmt.Sprintf("%d rows", len(resp.Items))},
		Payload:  resp,
	}, nil
}

func (s *Server) runDBAuditDiscoveryTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeIDText := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	var nodeID uuid.UUID
	if nodeIDText != "" {
		parsedNodeID, err := s.validateToolNodeID(ctx, tc.TenantID, nodeIDText)
		if err != nil {
			return aiToolExecution{}, err
		}
		nodeID = parsedNodeID
	}
	scope, guardrails, err := investigationScopeFromRequest(
		nil,
		tc.TenantID.String(),
		stringFromToolInput(input, "since"),
		stringFromToolInput(input, "until"),
		intFromToolInput(input, "limit"),
		0,
		100,
	)
	if err != nil {
		return aiToolExecution{}, err
	}
	resp, err := s.buildDBAuditDiscovery(ctx, dbAuditDiscoveryQuery{
		TenantID: tc.TenantID,
		NodeID:   nodeID,
		Since:    scope.Since,
		Until:    scope.Until,
		Limit:    scope.Limit,
	}, guardrails)
	if err != nil {
		return aiToolExecution{}, err
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "db_audit_discovery", Label: "DB audit discovery", Detail: fmt.Sprintf("%d candidates", len(resp.Candidates))},
		Payload:  resp,
	}, nil
}

func (s *Server) runRiskNotablesTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeIDText := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	var nodeID uuid.UUID
	if nodeIDText != "" {
		parsedNodeID, err := s.validateToolNodeID(ctx, tc.TenantID, nodeIDText)
		if err != nil {
			return aiToolExecution{}, err
		}
		nodeID = parsedNodeID
	}
	scope, guardrails, err := investigationScopeFromRequest(
		nil,
		tc.TenantID.String(),
		stringFromToolInput(input, "since"),
		stringFromToolInput(input, "until"),
		intFromToolInput(input, "limit"),
		intFromToolInput(input, "offset"),
		100,
	)
	if err != nil {
		return aiToolExecution{}, err
	}
	resp, err := s.buildRiskNotables(ctx, riskNotablesQuery{
		TenantID: tc.TenantID,
		NodeID:   nodeID,
		Since:    scope.Since,
		Until:    scope.Until,
		Limit:    scope.Limit,
		Offset:   scope.Offset,
	}, guardrails)
	if err != nil {
		return aiToolExecution{}, err
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "risk_notables", Label: "risk notables", Detail: fmt.Sprintf("%d notables", len(resp.Notables))},
		Payload:  resp,
	}, nil
}

func (s *Server) runComplianceEvidenceQueryTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeIDText := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	var nodeID uuid.UUID
	if nodeIDText != "" {
		parsedNodeID, err := s.validateToolNodeID(ctx, tc.TenantID, nodeIDText)
		if err != nil {
			return aiToolExecution{}, err
		}
		nodeID = parsedNodeID
	}
	scope, guardrails, err := investigationScopeFromRequest(
		nil,
		tc.TenantID.String(),
		stringFromToolInput(input, "since"),
		stringFromToolInput(input, "until"),
		intFromToolInput(input, "limit"),
		intFromToolInput(input, "offset"),
		100,
	)
	if err != nil {
		return aiToolExecution{}, err
	}
	var passed *bool
	if raw, ok := input["passed"].(bool); ok {
		passed = &raw
	}
	resp, err := s.buildComplianceEvidenceQuery(ctx, complianceEvidenceQuery{
		TenantID:     tc.TenantID,
		NodeID:       nodeID,
		RuleID:       strings.TrimSpace(stringFromToolInput(input, "rule_id")),
		Framework:    strings.TrimSpace(stringFromToolInput(input, "framework")),
		ControlRef:   strings.TrimSpace(stringFromToolInput(input, "control_ref")),
		EvidenceType: strings.TrimSpace(stringFromToolInput(input, "evidence_type")),
		Passed:       passed,
		Since:        scope.Since,
		Until:        scope.Until,
		Limit:        scope.Limit,
		Offset:       scope.Offset,
	}, guardrails)
	if err != nil {
		return aiToolExecution{}, err
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "compliance_evidence_query", Label: "compliance evidence", Detail: fmt.Sprintf("%d evidence records", resp.Summary.Total)},
		Payload:  resp,
	}, nil
}

func (s *Server) runCoverageExplainTool(_ context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	resp := newCoverageExplainResponse(tc.TenantID)
	domain := strings.TrimSpace(stringFromToolInput(input, "domain"))
	state := strings.TrimSpace(stringFromToolInput(input, "state"))
	if domain != "" || state != "" {
		filtered := make([]coverageExplanation, 0, len(resp.Explanations))
		for _, explanation := range resp.Explanations {
			if domain != "" && !strings.EqualFold(domain, explanation.Domain) {
				continue
			}
			if state != "" && !strings.EqualFold(state, string(explanation.State)) {
				continue
			}
			filtered = append(filtered, explanation)
		}
		resp.Explanations = filtered
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "coverage_explain", Label: "coverage truth", Detail: fmt.Sprintf("%d domains", len(resp.Explanations))},
		Payload:  resp,
	}, nil
}

func (s *Server) runPostureDriftExplainTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	nodeIDText := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	var nodeID uuid.UUID
	if nodeIDText != "" {
		parsedNodeID, err := s.validateToolNodeID(ctx, tc.TenantID, nodeIDText)
		if err != nil {
			return aiToolExecution{}, err
		}
		nodeID = parsedNodeID
	}
	scope, guardrails, err := investigationScopeFromRequest(
		nil,
		tc.TenantID.String(),
		stringFromToolInput(input, "since"),
		stringFromToolInput(input, "until"),
		intFromToolInput(input, "limit"),
		intFromToolInput(input, "offset"),
		100,
	)
	if err != nil {
		return aiToolExecution{}, err
	}
	resp, err := s.buildPostureDriftExplain(ctx, postureDriftExplainQuery{
		TenantID: tc.TenantID,
		NodeID:   nodeID,
		Since:    scope.Since,
		Until:    scope.Until,
		Limit:    scope.Limit,
		Offset:   scope.Offset,
	}, guardrails)
	if err != nil {
		return aiToolExecution{}, err
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "posture_drift_explain", Label: "posture drift", Detail: fmt.Sprintf("%d receipts", len(resp.Receipts))},
		Payload:  resp,
	}, nil
}

func (s *Server) runEventCaptureTool(ctx context.Context, tc aiToolContext, input map[string]any, toolName, kind, label string) (aiToolExecution, error) {
	nodeID, err := s.nodeIDFromToolInput(ctx, tc.TenantID, input)
	if err != nil {
		return aiToolExecution{}, err
	}
	filter := eventCaptureFilterFromToolInput(tc.TenantID, nodeID, input)
	payload, err := s.eventCapturePayload(ctx, kind, filter)
	if err != nil {
		return aiToolExecution{}, err
	}
	return aiToolExecution{Citation: llm.Citation{Tool: toolName, Label: label, Detail: nodeID.String()}, Payload: payload}, nil
}

func (s *Server) runOperatorExecuteTool(context.Context, aiToolContext, map[string]any) (aiToolExecution, error) {
	return aiToolExecution{}, errors.New("operator execution is not enabled; create an explicit approval through the existing product flow")
}

func (s *Server) nodeIDFromToolInput(ctx context.Context, tenantID uuid.UUID, input map[string]any) (uuid.UUID, error) {
	raw := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	if raw == "" {
		return uuid.Nil, errors.New("node_id is required")
	}
	return s.validateToolNodeID(ctx, tenantID, raw)
}

func (s *Server) validateToolNodeID(ctx context.Context, tenantID uuid.UUID, raw string) (uuid.UUID, error) {
	nodeID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errors.New("invalid node_id")
	}
	if s == nil || s.store == nil {
		return uuid.Nil, errors.New("storage unavailable for node validation")
	}
	_, err = s.ensureNodeInTenant(ctx, tenantID, nodeID)
	return nodeID, err
}

func (s *Server) ensureNodeInTenant(ctx context.Context, tenantID, nodeID uuid.UUID) (*storage.Node, error) {
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("node not found")
	}
	if node.TenantID != tenantID {
		return nil, errors.New("node is outside requested tenant")
	}
	return node, nil
}

func stringFromToolInput(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	raw, ok := input[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func intFromToolInput(input map[string]any, key string) int {
	if input == nil {
		return 0
	}
	raw, ok := input[key]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimSpace(v), "%d", &n)
		return n
	default:
		return 0
	}
}

func stringListFromToolInput(input map[string]any, key string) []string {
	if input == nil {
		return nil
	}
	raw, ok := input[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		return splitCSV(v)
	default:
		return nil
	}
}

func lifecycleCitationFromItem(index int, item storage.LifecycleItem) entityLifecycleCitation {
	source := strings.TrimSpace(item.Source)
	if source == "" {
		source = "unknown"
	}
	rawID := strings.TrimSpace(item.RawID)
	if rawID == "" {
		rawID = fmt.Sprintf("row-%d", index+1)
	}
	sourceKey := strings.NewReplacer(":", "_", " ", "_", "/", "_").Replace(strings.ToLower(source))
	return entityLifecycleCitation{
		ID:             fmt.Sprintf("lifecycle:%s:%s", sourceKey, rawID),
		Kind:           "lifecycle",
		Source:         source,
		SourceRecordID: rawID,
	}
}

func encodeToolPayload(exec aiToolExecution) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"citation": exec.Citation,
		"data":     exec.Payload,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
