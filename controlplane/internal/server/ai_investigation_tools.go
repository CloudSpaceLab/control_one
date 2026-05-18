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
			MinRole:     roleOperator,
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

func (s *Server) executeAITool(ctx context.Context, principal *auth.Principal, tenantID uuid.UUID, call llm.ToolCall) (aiToolExecution, error) {
	tool, ok := s.aiInvestigationTools()[call.Name]
	if !ok {
		return aiToolExecution{}, fmt.Errorf("unknown tool %q", call.Name)
	}
	if !aiRoleAllows(principal, tool.MinRole) {
		return aiToolExecution{}, fmt.Errorf("tool %s requires role %s", call.Name, tool.MinRole)
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
		return hasRole(principal, roleViewer) || hasRole(principal, roleOperator) || hasRole(principal, roleAdmin)
	case roleOperator:
		return hasRole(principal, roleOperator) || hasRole(principal, roleAdmin)
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
	if toolName == "root_cause_findings" {
		if err := s.persistRootCauseFindings(ctx, filter, payload); err != nil {
			return aiToolExecution{}, err
		}
	}
	return aiToolExecution{Citation: llm.Citation{Tool: toolName, Label: label, Detail: nodeID.String()}, Payload: payload}, nil
}

func (s *Server) persistRootCauseFindings(ctx context.Context, filter EventCaptureFilter, payload any) error {
	backend := s.aiOperatorBackend()
	if backend == nil {
		return nil
	}
	findings, ok := payload.([]RootCauseFinding)
	if !ok || len(findings) == 0 {
		return nil
	}
	evidence, err := json.Marshal(map[string]any{
		"kind":     "root-cause-findings",
		"since":    filter.Since.Format(time.RFC3339),
		"until":    filter.Until.Format(time.RFC3339),
		"findings": findings,
	})
	if err != nil {
		return err
	}
	summary := strings.TrimSpace(findings[0].Summary)
	if summary == "" {
		summary = "root cause finding"
	}
	dedup := fmt.Sprintf("root-cause:%s:%s:%s", filter.NodeID, filter.Since.Format(time.RFC3339), filter.Until.Format(time.RFC3339))
	_, err = backend.CreateAIInvestigation(ctx, storage.CreateAIInvestigationParams{
		TenantID:         filter.TenantID,
		NodeID:           filter.NodeID,
		TriggerType:      "root_cause",
		TriggerEventType: "sprint6.event_capture",
		TriggerDedupKey:  dedup,
		Severity:         rootCauseSeverity(findings[0].Confidence),
		Summary:          summary,
		Evidence:         evidence,
		Status:           storage.AIInvestigationStatusOpen,
	})
	return err
}

func rootCauseSeverity(confidence string) string {
	switch strings.ToLower(strings.TrimSpace(confidence)) {
	case "high":
		return "warning"
	case "medium":
		return "info"
	case "low":
		return "info"
	default:
		return "info"
	}
}

func (s *Server) runOperatorExecuteTool(context.Context, aiToolContext, map[string]any) (aiToolExecution, error) {
	return aiToolExecution{}, errors.New("operator execution is not enabled; create an explicit approval through the existing product flow")
}

func (s *Server) nodeIDFromToolInput(ctx context.Context, tenantID uuid.UUID, input map[string]any) (uuid.UUID, error) {
	raw := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	if raw == "" {
		return uuid.Nil, errors.New("node_id is required")
	}
	nodeID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errors.New("invalid node_id")
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
