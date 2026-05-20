package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type savedSearchCreator interface {
	CreateSavedSearch(context.Context, storage.SavedSearch) (*storage.SavedSearch, error)
}

type aiWorkflowCitation struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Table          string `json:"table"`
	SourceRecordID string `json:"source_record_id"`
}

type incidentCreateToolResponse struct {
	TenantID         string               `json:"tenant_id"`
	IncidentID       string               `json:"incident_id"`
	NodeID           string               `json:"node_id,omitempty"`
	TriggerType      string               `json:"trigger_type"`
	TriggerEventType string               `json:"trigger_event_type"`
	DedupKey         string               `json:"dedup_key"`
	Severity         string               `json:"severity"`
	Summary          string               `json:"summary"`
	Status           string               `json:"status"`
	Evidence         map[string]any       `json:"evidence,omitempty"`
	Citations        []aiWorkflowCitation `json:"citations"`
	Guardrails       []string             `json:"guardrails"`
}

type huntSaveToolResponse struct {
	TenantID   string               `json:"tenant_id"`
	HuntID     string               `json:"hunt_id"`
	OwnerID    string               `json:"owner_user_id"`
	Name       string               `json:"name"`
	Query      string               `json:"query,omitempty"`
	EntityType string               `json:"entity_type,omitempty"`
	Shared     bool                 `json:"shared"`
	Filters    map[string]any       `json:"filters,omitempty"`
	Citations  []aiWorkflowCitation `json:"citations"`
	Guardrails []string             `json:"guardrails"`
}

type caseNoteAddToolResponse struct {
	TenantID   string               `json:"tenant_id"`
	CaseID     string               `json:"case_id"`
	AuditID    string               `json:"audit_id"`
	Note       string               `json:"note"`
	Citations  []aiWorkflowCitation `json:"citations"`
	Guardrails []string             `json:"guardrails"`
}

func (s *Server) runIncidentCreateTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	summary := boundedToolString(stringFromToolInput(input, "summary"), 512)
	if summary == "" {
		return aiToolExecution{}, errors.New("summary is required")
	}
	nodeIDText := strings.TrimSpace(stringFromToolInput(input, "node_id"))
	var nodeID uuid.UUID
	if nodeIDText != "" {
		parsedNodeID, err := s.validateToolNodeID(ctx, tc.TenantID, nodeIDText)
		if err != nil {
			return aiToolExecution{}, err
		}
		nodeID = parsedNodeID
	}
	backend := s.aiOperatorBackend()
	if backend == nil {
		return aiToolExecution{}, errors.New("ai operator store unavailable")
	}

	triggerType := firstNonEmptyString(boundedToolString(stringFromToolInput(input, "trigger_type"), 80), "incident")
	triggerEventType := firstNonEmptyString(boundedToolString(stringFromToolInput(input, "trigger_event_type"), 120), "ai.incident")
	severity := normalizeIncidentSeverity(stringFromToolInput(input, "severity"))
	dedupKey := boundedToolString(stringFromToolInput(input, "dedup_key"), 256)
	evidence := map[string]any{
		"source_tool": "incident_create",
		"created_at":  tc.Now.Format(time.RFC3339),
		"citations":   sanitizeStringSlice(stringListFromToolInput(input, "citations"), 100),
		"guardrails":  []string{"tenant_scoped", "case_workflow_only", "no_enforcement_execution"},
	}
	if extra := mapFromToolInput(input, "evidence"); len(extra) > 0 {
		evidence["details"] = extra
	}
	if tc.Principal != nil {
		evidence["created_by_subject"] = boundedToolString(tc.Principal.Subject, 256)
	}
	rawEvidence, _ := json.Marshal(evidence)

	row, err := backend.CreateAIInvestigation(ctx, storage.CreateAIInvestigationParams{
		TenantID:         tc.TenantID,
		NodeID:           nodeID,
		TriggerType:      triggerType,
		TriggerEventType: triggerEventType,
		TriggerDedupKey:  dedupKey,
		Severity:         severity,
		Summary:          summary,
		Evidence:         rawEvidence,
		Status:           storage.AIInvestigationStatusOpen,
	})
	if err != nil {
		return aiToolExecution{}, err
	}
	resp := incidentCreateToolResponse{
		TenantID:         row.TenantID.String(),
		IncidentID:       row.ID.String(),
		TriggerType:      row.TriggerType,
		TriggerEventType: row.TriggerEventType,
		DedupKey:         row.TriggerDedupKey,
		Severity:         row.Severity,
		Summary:          row.Summary,
		Status:           string(row.Status),
		Evidence:         evidence,
		Citations:        []aiWorkflowCitation{workflowCitation("ai_investigations", row.ID.String(), "incident")},
		Guardrails:       []string{"tenant_scoped", "operator_or_admin_only", "no_enforcement_execution", "source_row_citations"},
	}
	if row.NodeID != uuid.Nil {
		resp.NodeID = row.NodeID.String()
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "incident_create", Label: "investigation incident", Detail: row.ID.String()},
		Payload:  resp,
	}, nil
}

func (s *Server) runHuntSaveTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	name := boundedToolString(stringFromToolInput(input, "name"), 160)
	if name == "" {
		return aiToolExecution{}, errors.New("name is required")
	}
	if s == nil || s.store == nil {
		return aiToolExecution{}, errors.New("saved search store unavailable")
	}
	backend, ok := s.store.(savedSearchCreator)
	if !ok {
		return aiToolExecution{}, errors.New("saved search store unavailable")
	}
	filters := mapFromToolInput(input, "filters")
	if filters == nil {
		filters = map[string]any{}
	}
	if since := boundedToolString(stringFromToolInput(input, "since"), 64); since != "" {
		filters["since"] = since
	}
	if until := boundedToolString(stringFromToolInput(input, "until"), 64); until != "" {
		filters["until"] = until
	}
	citationRefs := sanitizeStringSlice(stringListFromToolInput(input, "citations"), 100)
	if len(citationRefs) > 0 {
		filters["citations"] = citationRefs
	}
	filters["source_tool"] = "hunt_save"
	filters["saved_at"] = tc.Now.Format(time.RFC3339)
	filters["guardrails"] = []string{"tenant_scoped", "read_query_only", "no_enforcement_execution"}
	rawFilters, _ := json.Marshal(filters)
	ownerID := uuid.Nil
	if tc.Principal != nil {
		ownerID = principalUserID(s, ctx, tc.Principal)
	}
	shared := false
	if value, ok := input["shared"].(bool); ok {
		shared = value
	}
	row, err := backend.CreateSavedSearch(ctx, storage.SavedSearch{
		TenantID:    tc.TenantID,
		OwnerUserID: ownerID,
		Name:        name,
		Query:       boundedToolString(stringFromToolInput(input, "query"), 4096),
		EntityType:  boundedToolString(stringFromToolInput(input, "entity_type"), 80),
		Filters:     rawFilters,
		Shared:      shared,
	})
	if err != nil {
		return aiToolExecution{}, err
	}
	resp := huntSaveToolResponse{
		TenantID:   row.TenantID.String(),
		HuntID:     row.ID.String(),
		OwnerID:    row.OwnerUserID.String(),
		Name:       row.Name,
		Query:      row.Query,
		EntityType: row.EntityType,
		Shared:     row.Shared,
		Filters:    filters,
		Citations:  []aiWorkflowCitation{workflowCitation("saved_searches", row.ID.String(), "hunt")},
		Guardrails: []string{"tenant_scoped", "operator_or_admin_only", "saved_query_only", "source_row_citations"},
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "hunt_save", Label: "saved hunt", Detail: row.ID.String()},
		Payload:  resp,
	}, nil
}

func (s *Server) runCaseNoteAddTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	caseID, err := uuid.Parse(strings.TrimSpace(stringFromToolInput(input, "case_id")))
	if err != nil {
		return aiToolExecution{}, errors.New("invalid case_id")
	}
	note := boundedToolString(stringFromToolInput(input, "note"), 4000)
	if note == "" {
		return aiToolExecution{}, errors.New("note is required")
	}
	if s == nil || s.store == nil {
		return aiToolExecution{}, errors.New("case store unavailable")
	}
	citationRefs := sanitizeStringSlice(stringListFromToolInput(input, "citations"), 100)
	if backend := s.aiOperatorBackend(); backend != nil {
		investigation, err := backend.GetAIInvestigation(ctx, caseID)
		if err != nil {
			return aiToolExecution{}, err
		}
		if investigation != nil {
			if investigation.TenantID != tc.TenantID {
				return aiToolExecution{}, errors.New("case is outside requested tenant")
			}
			allowedCitations := map[string]struct{}{}
			for _, ref := range socCaseEvidenceRefs(parseSOCCaseEvidence(investigation.Evidence)) {
				allowedCitations[ref.ID] = struct{}{}
			}
			for _, citation := range citationRefs {
				if _, ok := allowedCitations[citation]; !ok {
					return aiToolExecution{}, errors.New("citation is not linked to case evidence")
				}
			}
			caseIDText := caseID.String()
			metadata := map[string]any{
				"note":        note,
				"source_tool": "case_note_add",
				"citations":   citationRefs,
				"guardrails":  []string{"tenant_scoped", "note_only", "no_enforcement_execution"},
				"case_source": "ai_investigation",
			}
			entry := &storage.AuditLog{
				TenantID:     investigation.TenantID,
				ActorType:    "ai_tool",
				Action:       "soc.case.note.add",
				ResourceType: "ai_investigation",
				ResourceID:   &caseIDText,
				Metadata:     metadata,
			}
			if tc.Principal != nil {
				entry.ActorType = firstNonEmptyString(strings.TrimSpace(tc.Principal.Type), "user")
				if strings.TrimSpace(tc.Principal.Subject) != "" {
					entry.ActorType = "user"
					if user, err := s.store.GetUserByExternalID(ctx, tc.Principal.Subject); err == nil && user != nil {
						entry.ActorID = user.ID
					}
				}
			}
			created, err := s.store.CreateAuditLog(ctx, entry)
			if err != nil {
				return aiToolExecution{}, err
			}
			resp := caseNoteAddToolResponse{
				TenantID:   investigation.TenantID.String(),
				CaseID:     caseID.String(),
				AuditID:    created.ID.String(),
				Note:       note,
				Citations:  []aiWorkflowCitation{workflowCitation("audit_logs", created.ID.String(), "case_note")},
				Guardrails: []string{"tenant_scoped", "investigator_or_admin_only", "note_only", "source_row_citations"},
			}
			return aiToolExecution{
				Citation: llm.Citation{Tool: "case_note_add", Label: "case note", Detail: created.ID.String()},
				Payload:  resp,
			}, nil
		}
	}
	c, err := s.store.GetMisconductCase(ctx, caseID)
	if err != nil {
		return aiToolExecution{}, err
	}
	if c == nil {
		return aiToolExecution{}, errors.New("case not found")
	}
	if c.TenantID != tc.TenantID {
		return aiToolExecution{}, errors.New("case is outside requested tenant")
	}
	caseIDText := caseID.String()
	metadata := map[string]any{
		"note":        note,
		"source_tool": "case_note_add",
		"citations":   citationRefs,
		"guardrails":  []string{"tenant_scoped", "note_only", "no_enforcement_execution"},
	}
	entry := &storage.AuditLog{
		TenantID:     c.TenantID,
		ActorType:    "ai_tool",
		Action:       "misconduct.case.note.add",
		ResourceType: "misconduct_case",
		ResourceID:   &caseIDText,
		Metadata:     metadata,
	}
	if tc.Principal != nil {
		entry.ActorType = firstNonEmptyString(strings.TrimSpace(tc.Principal.Type), "user")
		if strings.TrimSpace(tc.Principal.Subject) != "" {
			entry.ActorType = "user"
			if user, err := s.store.GetUserByExternalID(ctx, tc.Principal.Subject); err == nil && user != nil {
				entry.ActorID = user.ID
			}
		}
	}
	created, err := s.store.CreateAuditLog(ctx, entry)
	if err != nil {
		return aiToolExecution{}, err
	}
	resp := caseNoteAddToolResponse{
		TenantID:   c.TenantID.String(),
		CaseID:     caseID.String(),
		AuditID:    created.ID.String(),
		Note:       note,
		Citations:  []aiWorkflowCitation{workflowCitation("audit_logs", created.ID.String(), "case_note")},
		Guardrails: []string{"tenant_scoped", "investigator_or_admin_only", "note_only", "source_row_citations"},
	}
	return aiToolExecution{
		Citation: llm.Citation{Tool: "case_note_add", Label: "case note", Detail: created.ID.String()},
		Payload:  resp,
	}, nil
}

func workflowCitation(table, id, kind string) aiWorkflowCitation {
	return aiWorkflowCitation{
		ID:             citationID(table, id),
		Kind:           kind,
		Table:          table,
		SourceRecordID: table + ":" + id,
	}
}

func normalizeIncidentSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "high", "medium", "low", "info":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "info"
	}
}

func mapFromToolInput(input map[string]any, key string) map[string]any {
	if input == nil {
		return nil
	}
	raw, ok := input[key]
	if !ok || raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		out := make(map[string]any, len(m))
		for k, v := range m {
			k = strings.TrimSpace(k)
			if k != "" {
				out[k] = v
			}
		}
		return out
	}
	return metadataMap(raw)
}

func boundedToolString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit]))
}
