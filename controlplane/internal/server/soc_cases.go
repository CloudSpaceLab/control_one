package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type socCaseResponse struct {
	CaseID           string                 `json:"case_id"`
	TenantID         string                 `json:"tenant_id"`
	NodeID           string                 `json:"node_id,omitempty"`
	Title            string                 `json:"title"`
	Status           string                 `json:"status"`
	Severity         string                 `json:"severity"`
	Source           string                 `json:"source"`
	TriggerType      string                 `json:"trigger_type"`
	TriggerEventType string                 `json:"trigger_event_type"`
	DedupKey         string                 `json:"dedup_key"`
	Summary          string                 `json:"summary"`
	Evidence         map[string]any         `json:"evidence,omitempty"`
	EvidenceRefs     []socCaseEvidenceRef   `json:"evidence_refs,omitempty"`
	Timeline         []socCaseTimelineItem  `json:"timeline"`
	Notes            []socCaseNoteResponse  `json:"notes,omitempty"`
	Assignee         *socCaseUserRef        `json:"assignee,omitempty"`
	MentionedUsers   []socCaseUserRef       `json:"mentioned_users,omitempty"`
	Citations        []aiWorkflowCitation   `json:"citations"`
	CoverageBadges   []socCaseCoverageBadge `json:"coverage_badges"`
	ExportURL        string                 `json:"export_url"`
	CreatedAt        string                 `json:"created_at"`
	UpdatedAt        string                 `json:"updated_at"`
}

type socCaseUserRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type socCaseEvidenceRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type socCaseTimelineItem struct {
	Timestamp   string `json:"timestamp"`
	Event       string `json:"event"`
	Source      string `json:"source"`
	CitationID  string `json:"citation_id"`
	Description string `json:"description"`
}

type socCaseCoverageBadge struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Tone  string `json:"tone"`
}

type socCaseExportResponse struct {
	ExportVersion string                `json:"export_version"`
	GeneratedAt   string                `json:"generated_at"`
	TenantID      string                `json:"tenant_id"`
	Case          socCaseResponse       `json:"case"`
	Evidence      []socCaseEvidenceRef  `json:"evidence"`
	Notes         []socCaseNoteResponse `json:"notes,omitempty"`
	Guardrails    []string              `json:"guardrails"`
}

type socCaseNoteResponse struct {
	ID         string               `json:"id"`
	TenantID   string               `json:"tenant_id"`
	CaseID     string               `json:"case_id"`
	Note       string               `json:"note"`
	Citations  []socCaseEvidenceRef `json:"citations,omitempty"`
	Mentions   []string             `json:"mentions,omitempty"`
	AuditID    string               `json:"audit_id"`
	CreatedAt  string               `json:"created_at"`
	CreatedBy  string               `json:"created_by,omitempty"`
	Guardrails []string             `json:"guardrails"`
}

func (s *Server) handleSOCCasesCollection(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleInvestigator, roleOperator, roleAdmin) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleListSOCCases(w, r, principal, tenantID)
	case http.MethodPost:
		s.handleCreateSOCCaseFromAlert(w, r, principal, tenantID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListSOCCases(w http.ResponseWriter, r *http.Request, principal *auth.Principal, tenantID uuid.UUID) {
	backend := s.aiOperatorBackend()
	if backend == nil {
		http.Error(w, "case store unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.ListAIInvestigationsFilter{
		TenantID:         tenantID,
		Status:           storage.AIInvestigationStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		TriggerType:      strings.TrimSpace(r.URL.Query().Get("trigger_type")),
		TriggerEventType: strings.TrimSpace(r.URL.Query().Get("trigger_event_type")),
		Severity:         strings.TrimSpace(r.URL.Query().Get("severity")),
		Search:           strings.TrimSpace(r.URL.Query().Get("q")),
		SortBy:           strings.TrimSpace(r.URL.Query().Get("sort_by")),
		SortOrder:        strings.TrimSpace(r.URL.Query().Get("sort_order")),
	}
	if filter.SortBy == "" {
		filter.SortBy = "created_at"
	}
	if filter.SortOrder == "" {
		filter.SortOrder = "desc"
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			http.Error(w, "invalid since", http.StatusBadRequest)
			return
		}
		filter.Since = &parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("until")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			http.Error(w, "invalid until", http.StatusBadRequest)
			return
		}
		filter.Until = &parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("node_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}
	rows, total, err := backend.ListAIInvestigations(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Warn("list soc cases", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]socCaseResponse, 0, len(rows))
	includeNotes := parseBoolQuery(r.URL.Query().Get("include_notes"))
	teamUsers, err := s.store.ListTenantUsers(r.Context(), tenantID, "", 1000)
	if err != nil {
		s.logger.Warn("list tenant users for soc case collection", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		teamUsers = nil
	}
	for _, row := range rows {
		resp := newSOCCaseResponse(row)
		if teamUsers != nil {
			s.hydrateCaseCollaborationWithUsers(r.Context(), tenantID, row, &resp, teamUsers)
		}
		if includeNotes {
			notes, _, err := s.listSOCCaseNotes(r.Context(), tenantID, row.ID, 3, 0)
			if err != nil {
				s.logger.Warn("list soc case notes for collection", zap.Error(err), zap.String("case_id", row.ID.String()))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			resp.Notes = notes
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, paginatedResponse[socCaseResponse]{
		Data:       out,
		Pagination: newPaginationMeta(total, limit, offset, len(out)),
	})
}

type createSOCCaseFromAlertRequest struct {
	AlertID string `json:"alert_id"`
	Summary string `json:"summary,omitempty"`
}

func (s *Server) handleCreateSOCCaseFromAlert(w http.ResponseWriter, r *http.Request, principal *auth.Principal, tenantID uuid.UUID) {
	var req createSOCCaseFromAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	alertID, err := uuid.Parse(strings.TrimSpace(req.AlertID))
	if err != nil {
		http.Error(w, "invalid alert_id", http.StatusBadRequest)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	alertRow, err := s.store.GetAlert(r.Context(), alertID)
	if err != nil {
		s.logger.Warn("get alert for case", zap.Error(err), zap.String("alert_id", alertID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if alertRow == nil || alertRow.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	backend := s.aiOperatorBackend()
	if backend == nil {
		http.Error(w, "case store unavailable", http.StatusServiceUnavailable)
		return
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		summary = "Investigation case promoted from alert: " + strings.TrimSpace(alertRow.Title)
	}
	var nodeID uuid.UUID
	if alertRow.NodeID.Valid {
		nodeID = alertRow.NodeID.UUID
	}
	evidence, err := json.Marshal(map[string]any{
		"alert_id":      alertID.String(),
		"alert_rule_id": socCaseEvidenceRuleID(alertRow),
		"title":         alertRow.Title,
		"severity":      alertRow.Severity,
		"state":         alertRow.State,
		"source":        alertRow.Source,
		"opened_at":     formatTime(alertRow.OpenedAt),
		"context":       alertRow.Context,
		"collector":     firstNonEmptyString(alertRow.Source, "alerts"),
	})
	if err != nil {
		s.logger.Warn("marshal alert case evidence", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	var createdBy uuid.UUID
	if principal != nil && strings.TrimSpace(principal.Subject) != "" {
		if user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject); err == nil && user != nil {
			createdBy = user.ID
		}
	}
	row, err := backend.CreateAIInvestigation(r.Context(), storage.CreateAIInvestigationParams{
		TenantID:         tenantID,
		NodeID:           nodeID,
		AlertID:          uuid.NullUUID{UUID: alertID, Valid: true},
		TriggerType:      "alert",
		TriggerEventType: firstNonEmptyString(alertRow.Source, "alert"),
		TriggerDedupKey:  "alert:" + alertID.String(),
		Severity:         alertRow.Severity,
		Summary:          summary,
		Evidence:         evidence,
		Status:           storage.AIInvestigationStatusOpen,
		CreatedBy:        createdBy,
	})
	if err != nil {
		s.logger.Warn("create soc case from alert", zap.Error(err), zap.String("alert_id", alertID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	caseID := row.ID.String()
	metadata := map[string]any{
		"alert_id":   alertID.String(),
		"case_id":    caseID,
		"source":     "soc_cases_api",
		"guardrails": []string{"tenant_scoped", "from_alert", "proposal_only"},
	}
	entry := &storage.AuditLog{
		TenantID:     tenantID,
		ActorType:    "user",
		Action:       "soc.case.from_alert",
		ResourceType: "ai_investigation",
		ResourceID:   &caseID,
		Metadata:     metadata,
	}
	if principal != nil {
		entry.ActorType = firstNonEmptyString(strings.TrimSpace(principal.Type), "user")
		if createdBy != uuid.Nil {
			entry.ActorID = createdBy
		} else if strings.TrimSpace(principal.Subject) != "" {
			metadata["created_by_subject"] = boundedToolString(principal.Subject, 256)
		}
	}
	if _, err := s.store.CreateAuditLog(r.Context(), entry); err != nil {
		s.logger.Warn("audit soc case from alert", zap.Error(err), zap.String("case_id", caseID))
	}
	resp := newSOCCaseResponse(*row)
	s.hydrateCaseCollaboration(r.Context(), tenantID, *row, &resp)
	writeJSON(w, http.StatusCreated, resp)
}

func socCaseEvidenceRuleID(alertRow *storage.Alert) string {
	if alertRow.RuleID.Valid {
		return alertRow.RuleID.UUID.String()
	}
	return ""
}

func (s *Server) handleSOCCaseSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/soc/cases/")
	segments := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(segments) == 0 || segments[0] == "" || len(segments) > 2 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		allow := "GET, POST"
		if len(segments) == 2 && segments[1] == "assign" {
			allow = http.MethodPost
		}
		w.Header().Set("Allow", allow)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleInvestigator, roleOperator, roleAdmin) {
		return
	}
	id, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "case id must be a UUID", http.StatusBadRequest)
		return
	}
	backend := s.aiOperatorBackend()
	if backend == nil {
		http.Error(w, "case store unavailable", http.StatusServiceUnavailable)
		return
	}
	row, err := backend.GetAIInvestigation(r.Context(), id)
	if err != nil {
		s.logger.Warn("get soc case", zap.Error(err), zap.String("case_id", id.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if row == nil || row.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	if len(segments) == 2 {
		switch segments[1] {
		case "export":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
				return
			}
			resp := newSOCCaseResponse(*row)
			s.hydrateCaseCollaboration(r.Context(), tenantID, *row, &resp)
			notes, _, err := s.listSOCCaseNotes(r.Context(), tenantID, id, 500, 0)
			if err != nil {
				s.logger.Warn("list soc case notes for export", zap.Error(err), zap.String("case_id", id.String()))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			resp.Notes = notes
			writeJSON(w, http.StatusOK, newSOCCaseExportResponse(resp, time.Now().UTC()))
			return
		case "notes":
			switch r.Method {
			case http.MethodGet:
				limit, offset, err := parseLimitOffset(r.URL.Query())
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				notes, total, err := s.listSOCCaseNotes(r.Context(), tenantID, id, limit, offset)
				if err != nil {
					s.logger.Warn("list soc case notes", zap.Error(err), zap.String("case_id", id.String()))
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, paginatedResponse[socCaseNoteResponse]{
					Data:       notes,
					Pagination: newPaginationMeta(total, limit, offset, len(notes)),
				})
				return
			case http.MethodPost:
				s.handleCreateSOCCaseNote(w, r, principal, *row)
				return
			}
		case "assign":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
				return
			}
			s.handleAssignSOCCase(w, r, principal, *row)
			return
		default:
			http.NotFound(w, r)
			return
		}
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	resp := newSOCCaseResponse(*row)
	s.hydrateCaseCollaboration(r.Context(), tenantID, *row, &resp)
	notes, _, err := s.listSOCCaseNotes(r.Context(), tenantID, id, 100, 0)
	if err != nil {
		s.logger.Warn("list soc case notes for detail", zap.Error(err), zap.String("case_id", id.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp.Notes = notes
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAssignSOCCase(w http.ResponseWriter, r *http.Request, principal *auth.Principal, row storage.AIInvestigation) {
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid assign payload", http.StatusBadRequest)
		return
	}
	assigneeRaw, ok := payload["assignee_id"]
	if !ok {
		http.Error(w, "assignee_id is required", http.StatusBadRequest)
		return
	}
	var requestedAssigneeID *string
	if err := json.Unmarshal(assigneeRaw, &requestedAssigneeID); err != nil {
		http.Error(w, "invalid assign payload", http.StatusBadRequest)
		return
	}

	assigneeID := uuid.Nil
	if requestedAssigneeID != nil {
		parsed, err := uuid.Parse(strings.TrimSpace(*requestedAssigneeID))
		if err != nil || parsed == uuid.Nil {
			http.Error(w, "invalid assignee_id", http.StatusBadRequest)
			return
		}
		assigneeID = parsed
		users, err := s.store.ListTenantUsers(r.Context(), row.TenantID, "", 1000)
		if err != nil {
			s.logger.Warn("list tenant users for case assignment", zap.Error(err), zap.String("tenant_id", row.TenantID.String()), zap.String("case_id", row.ID.String()))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		found := false
		for _, user := range users {
			if user.ID == assigneeID {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "assignee must be a team member in this tenant", http.StatusBadRequest)
			return
		}
	}

	actorID, _ := s.userIDForPrincipal(r.Context(), principal)
	currentAssigneeID, err := s.store.CaseAssignee(r.Context(), row.TenantID, row.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Warn("get soc case assignee", zap.Error(err), zap.String("tenant_id", row.TenantID.String()), zap.String("case_id", row.ID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	changed := currentAssigneeID != assigneeID
	if changed {
		if err := s.store.AssignCase(r.Context(), row.TenantID, row.ID, assigneeID, actorID, time.Now().UTC()); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			s.logger.Warn("assign soc case", zap.Error(err), zap.String("tenant_id", row.TenantID.String()), zap.String("case_id", row.ID.String()))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		backend := s.aiOperatorBackend()
		if backend == nil {
			http.Error(w, "case store unavailable", http.StatusServiceUnavailable)
			return
		}
		refreshed, err := backend.GetAIInvestigation(r.Context(), row.ID)
		if err != nil {
			s.logger.Warn("refresh assigned soc case", zap.Error(err), zap.String("tenant_id", row.TenantID.String()), zap.String("case_id", row.ID.String()))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if refreshed == nil || refreshed.TenantID != row.TenantID {
			http.NotFound(w, r)
			return
		}
		row = *refreshed
		caseID := row.ID.String()
		metadata := map[string]any{
			"assignee_id": nullableUUIDString(assigneeID),
			"assigned_by": nullableUUIDString(actorID),
			"case_id":     caseID,
			"case_source": "ai_investigation",
			"guardrails":  []string{"tenant_scoped", "owner_only"},
		}
		entry := &storage.AuditLog{
			TenantID:     row.TenantID,
			ActorID:      actorID,
			ActorType:    firstNonEmptyString(strings.TrimSpace(principal.Type), "user"),
			Action:       "soc.case.assign",
			ResourceType: "ai_investigation",
			ResourceID:   &caseID,
			Metadata:     metadata,
		}
		if _, err := s.store.CreateAuditLog(r.Context(), entry); err != nil {
			s.logger.Warn("audit soc case assignment", zap.Error(err), zap.String("tenant_id", row.TenantID.String()), zap.String("case_id", row.ID.String()))
		}
		if assigneeID != uuid.Nil && assigneeID != actorID {
			if _, err := s.store.CreateNotification(r.Context(), storage.CreateNotificationParams{
				TenantID: row.TenantID, RecipientID: assigneeID, ActorID: actorID, Kind: "case_assigned", CaseID: row.ID, CaseTitle: newSOCCaseResponse(row).Title,
			}); err != nil {
				s.logger.Warn("notify case assignee", zap.Error(err), zap.String("tenant_id", row.TenantID.String()), zap.String("case_id", row.ID.String()), zap.String("recipient_id", assigneeID.String()))
			}
		}
	}
	if !changed {
		row.AssigneeID = uuid.NullUUID{UUID: assigneeID, Valid: assigneeID != uuid.Nil}
	}
	resp := newSOCCaseResponse(row)
	s.hydrateCaseCollaboration(r.Context(), row.TenantID, row, &resp)
	writeJSON(w, http.StatusOK, resp)
}

func nullableUUIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func newSOCCaseResponse(row storage.AIInvestigation) socCaseResponse {
	evidence := parseSOCCaseEvidence(row.Evidence)
	citation := workflowCitation("ai_investigations", row.ID.String(), "soc_case")
	evidenceRefs := socCaseEvidenceRefsForRow(row, evidence, citation)
	summary := cleanSOCCaseText(row.Summary)
	resp := socCaseResponse{
		CaseID:           row.ID.String(),
		TenantID:         row.TenantID.String(),
		Title:            firstNonEmptyString(summary, strings.TrimSpace(row.TriggerEventType), "Investigation case"),
		Status:           string(row.Status),
		Severity:         firstNonEmptyString(strings.ToLower(strings.TrimSpace(row.Severity)), "info"),
		Source:           "ai_investigation",
		TriggerType:      row.TriggerType,
		TriggerEventType: row.TriggerEventType,
		DedupKey:         row.TriggerDedupKey,
		Summary:          summary,
		Evidence:         evidence,
		EvidenceRefs:     evidenceRefs,
		Citations:        []aiWorkflowCitation{citation},
		CoverageBadges: []socCaseCoverageBadge{
			{ID: "source_row_citations", Label: "Source-row cited", Tone: "healthy"},
			{ID: "evidence_linked", Label: "Evidence linked", Tone: socCaseEvidenceTone(evidenceRefs)},
			{ID: "export_ready", Label: "Audit export ready", Tone: "healthy"},
			{ID: "actions_proposal_only", Label: "Actions proposal-only", Tone: "info"},
		},
		ExportURL: "/api/v1/soc/cases/" + row.ID.String() + "/export?tenant_id=" + row.TenantID.String(),
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.NodeID != uuid.Nil {
		resp.NodeID = row.NodeID.String()
	}
	resp.Timeline = socCaseTimeline(row, evidence, evidenceRefs, citation, resp.CreatedAt, resp.UpdatedAt, resp.Summary)
	return resp
}

func cleanSOCCaseText(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "first connection to ") && strings.HasSuffix(strings.ToLower(value), " by") {
		return strings.TrimSpace(value[:len(value)-len(" by")])
	}
	return value
}

func newSOCCaseExportResponse(c socCaseResponse, generatedAt time.Time) socCaseExportResponse {
	caseForExport := c
	caseForExport.Evidence = nil
	caseForExport.Notes = nil
	return socCaseExportResponse{
		ExportVersion: "soc-case-export-v1",
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339),
		TenantID:      c.TenantID,
		Case:          caseForExport,
		Evidence:      c.EvidenceRefs,
		Notes:         c.Notes,
		Guardrails: []string{
			"tenant_scoped",
			"source_row_citations",
			"evidence_refs_only",
			"no_enforcement_execution",
			"audit_ready_json",
		},
	}
}

func parseSOCCaseEvidence(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err == nil {
		return out
	}
	return map[string]any{"raw": string(raw)}
}

func socCaseEvidenceRefs(evidence map[string]any) []socCaseEvidenceRef {
	if len(evidence) == 0 {
		return nil
	}
	refs := []socCaseEvidenceRef{}
	seen := map[string]struct{}{}
	refs = appendSOCCaseEvidenceRefs(refs, seen, evidence["citations"])
	if details, ok := evidence["details"].(map[string]any); ok {
		refs = appendSOCCaseEvidenceRefs(refs, seen, details["citations"])
	}
	return refs
}

func appendSOCCaseEvidenceRefs(refs []socCaseEvidenceRef, seen map[string]struct{}, raw any) []socCaseEvidenceRef {
	for _, ref := range parseSOCCaseEvidenceRefs(raw) {
		ref.ID = strings.TrimSpace(ref.ID)
		ref.Kind = strings.TrimSpace(ref.Kind)
		if ref.ID == "" {
			continue
		}
		if ref.Kind == "" {
			ref.Kind = socCaseEvidenceKind(ref.ID)
		}
		key := ref.Kind + "\x00" + ref.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

func parseSOCCaseEvidenceRefs(raw any) []socCaseEvidenceRef {
	switch v := raw.(type) {
	case nil:
		return nil
	case []string:
		refs := make([]socCaseEvidenceRef, 0, len(v))
		for _, item := range v {
			if ref, ok := socCaseEvidenceRefFromAny(item); ok {
				refs = append(refs, ref)
			}
		}
		return refs
	case string:
		values := stringSliceFromAny(v)
		refs := make([]socCaseEvidenceRef, 0, len(values))
		for _, item := range values {
			if ref, ok := socCaseEvidenceRefFromAny(item); ok {
				refs = append(refs, ref)
			}
		}
		return refs
	case []any:
		refs := make([]socCaseEvidenceRef, 0, len(v))
		for _, item := range v {
			if ref, ok := socCaseEvidenceRefFromAny(item); ok {
				refs = append(refs, ref)
			}
		}
		return refs
	default:
		if ref, ok := socCaseEvidenceRefFromAny(v); ok {
			return []socCaseEvidenceRef{ref}
		}
		return nil
	}
}

func socCaseEvidenceRefFromAny(raw any) (socCaseEvidenceRef, bool) {
	switch v := raw.(type) {
	case socCaseEvidenceRef:
		return v, strings.TrimSpace(v.ID) != ""
	case string:
		id := strings.TrimSpace(v)
		if id == "" {
			return socCaseEvidenceRef{}, false
		}
		return socCaseEvidenceRef{ID: id, Kind: socCaseEvidenceKind(id)}, true
	case map[string]string:
		fields := make(map[string]any, len(v))
		for key, value := range v {
			fields[key] = value
		}
		return socCaseEvidenceRefFromMap(fields)
	case map[string]any:
		return socCaseEvidenceRefFromMap(v)
	default:
		values := stringSliceFromAny(v)
		if len(values) == 0 {
			return socCaseEvidenceRef{}, false
		}
		id := strings.TrimSpace(values[0])
		if id == "" {
			return socCaseEvidenceRef{}, false
		}
		return socCaseEvidenceRef{ID: id, Kind: socCaseEvidenceKind(id)}, true
	}
}

func socCaseEvidenceRefFromMap(fields map[string]any) (socCaseEvidenceRef, bool) {
	id := firstNonEmptyString(
		socCaseEvidenceFieldString(fields, "id"),
		socCaseEvidenceFieldString(fields, "citation_id"),
		socCaseEvidenceFieldString(fields, "ref"),
		socCaseEvidenceFieldString(fields, "reference"),
		socCaseStructuredEvidenceID(fields),
	)
	if id == "" {
		return socCaseEvidenceRef{}, false
	}
	kind := firstNonEmptyString(
		socCaseEvidenceFieldString(fields, "kind"),
		socCaseEvidenceFieldString(fields, "type"),
		socCaseEvidenceKind(id),
	)
	return socCaseEvidenceRef{ID: id, Kind: kind}, true
}

func socCaseStructuredEvidenceID(fields map[string]any) string {
	typ := strings.ToLower(socCaseEvidenceFieldString(fields, "type"))
	if runtimeStateID := socCaseEvidenceFieldString(fields, "runtime_state_id"); runtimeStateID != "" {
		switch typ {
		case "content_pack_source_runtime_state", "source_runtime_state":
			return "content_pack_source_runtime_state:" + runtimeStateID
		}
		if strings.Contains(typ, "source_runtime_state") {
			return "content_pack_source_runtime_state:" + runtimeStateID
		}
	}
	return ""
}

func socCaseEvidenceFieldString(fields map[string]any, key string) string {
	value, ok := fields[key]
	if !ok {
		return ""
	}
	out := strings.TrimSpace(fmt.Sprint(value))
	if out == "" || out == "<nil>" {
		return ""
	}
	return out
}

func socCaseEvidenceRefsForRow(row storage.AIInvestigation, evidence map[string]any, citation aiWorkflowCitation) []socCaseEvidenceRef {
	refs := []socCaseEvidenceRef{}
	appendSOCCaseEvidenceRef(&refs, citation.ID, "soc_case")
	if row.NodeID != uuid.Nil {
		appendSOCCaseEvidenceRef(&refs, "nodes:"+row.NodeID.String(), "node")
	}
	for _, ref := range socCaseEvidenceRefs(evidence) {
		appendSOCCaseEvidenceRef(&refs, ref.ID, ref.Kind)
	}
	appendDerivedSOCCaseEvidenceRefs(&refs, evidence, 0)
	return refs
}

func appendDerivedSOCCaseEvidenceRefs(refs *[]socCaseEvidenceRef, value any, depth int) {
	if refs == nil || value == nil || depth > 5 || len(*refs) >= 32 {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		appendSOCCaseKnownMapRefs(refs, typed)
		for key, child := range typed {
			if len(*refs) >= 32 {
				return
			}
			if strings.EqualFold(key, "citations") {
				continue
			}
			if strings.EqualFold(key, "evidence_refs") || strings.EqualFold(key, "evidence") {
				appendDerivedSOCCaseEvidenceRefs(refs, child, depth+1)
				continue
			}
			switch child.(type) {
			case map[string]any, []any, []map[string]any:
				appendDerivedSOCCaseEvidenceRefs(refs, child, depth+1)
			}
		}
	case []any:
		for _, item := range typed {
			if len(*refs) >= 32 {
				return
			}
			appendDerivedSOCCaseEvidenceRefs(refs, item, depth+1)
		}
	case []map[string]any:
		for _, item := range typed {
			if len(*refs) >= 32 {
				return
			}
			appendDerivedSOCCaseEvidenceRefs(refs, item, depth+1)
		}
	case string:
		if id := strings.TrimSpace(typed); looksLikeSOCCaseCitation(id) {
			appendSOCCaseEvidenceRef(refs, id, socCaseEvidenceKind(id))
		}
	}
}

func appendSOCCaseKnownMapRefs(refs *[]socCaseEvidenceRef, value map[string]any) {
	if len(value) == 0 {
		return
	}
	for _, candidate := range []struct {
		key    string
		prefix string
		kind   string
	}{
		{key: "source_record_id", kind: "evidence"},
		{key: "citation_id", kind: "evidence"},
		{key: "event_id", prefix: "events", kind: "event"},
		{key: "raw_ref", kind: "event"},
		{key: "conn_id", prefix: "connections", kind: "connection"},
		{key: "connection_id", prefix: "connections", kind: "connection"},
		{key: "request_id", prefix: "requests", kind: "request"},
		{key: "response_request_id", prefix: "requests", kind: "request"},
		{key: "traceparent", prefix: "traces", kind: "trace"},
		{key: "db_query_id", prefix: "db_queries", kind: "db_query"},
		{key: "patch_state_id", prefix: "patch_states", kind: "patch_state"},
		{key: "exposure_id", prefix: "exposures", kind: "exposure"},
		{key: "file_event_id", prefix: "file_events", kind: "file"},
		{key: "process_event_id", prefix: "process_events", kind: "process"},
		{key: "source_file", prefix: "files", kind: "file"},
		{key: "node_id", prefix: "nodes", kind: "node"},
		{key: "rule_id", prefix: "rules", kind: "rule"},
		{key: "correlation_id", prefix: "correlations", kind: "correlation"},
		{key: "dedup_key", prefix: "dedup", kind: "evidence"},
	} {
		id := socCaseRefID(candidate.prefix, value[candidate.key])
		if id == "" {
			continue
		}
		kind := candidate.kind
		if kind == "" {
			kind = socCaseEvidenceKind(id)
		}
		appendSOCCaseEvidenceRef(refs, id, kind)
	}
	if id, ok := socCaseSyntheticEvidenceRef(value); ok {
		appendSOCCaseEvidenceRef(refs, id, socCaseEvidenceKind(id))
	}
}

func socCaseSyntheticEvidenceRef(value map[string]any) (string, bool) {
	eventType := socCaseString(value["type"])
	timestamp := socCaseString(value["timestamp"])
	if timestamp == "" {
		timestamp = socCaseString(value["ts"])
	}
	path := socCaseString(value["path"])
	status := socCaseString(value["status_code"])
	if eventType == "" || (timestamp == "" && path == "" && status == "") {
		return "", false
	}
	parts := []string{eventType}
	if timestamp != "" {
		parts = append(parts, timestamp)
	}
	if path != "" {
		parts = append(parts, path)
	}
	if status != "" {
		parts = append(parts, status)
	}
	return "events:" + strings.Join(parts, "|"), true
}

func socCaseRefID(prefix string, value any) string {
	text := socCaseString(value)
	if text == "" {
		return ""
	}
	if looksLikeSOCCaseCitation(text) || prefix == "" {
		return text
	}
	return prefix + ":" + text
}

func socCaseString(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func appendSOCCaseEvidenceRef(refs *[]socCaseEvidenceRef, id, kind string) {
	id = strings.TrimSpace(id)
	if refs == nil || id == "" || len(*refs) >= 32 {
		return
	}
	for _, existing := range *refs {
		if strings.EqualFold(existing.ID, id) {
			return
		}
	}
	if strings.TrimSpace(kind) == "" {
		kind = socCaseEvidenceKind(id)
	}
	*refs = append(*refs, socCaseEvidenceRef{ID: id, Kind: kind})
}

func looksLikeSOCCaseCitation(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	for _, prefix := range []string{
		"ai_investigations:", "normalized_events:", "events:", "doris:", "node_vulnerability_findings:",
		"content_pack_source_runtime_state:", "source_runtime_state:", "private_access_exposure_findings:",
		"private_access_exposure:",
		"posture", "compliance", "connections:", "requests:", "traces:", "db_queries:", "patch_states:",
		"exposures:", "file_events:", "process_events:", "files:", "nodes:", "rules:", "correlations:",
		"dedup:",
	} {
		if strings.HasPrefix(lower, prefix) || strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

func socCaseEvidenceKind(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	switch {
	case strings.Contains(ref, "content_pack_source_runtime_state"), strings.Contains(ref, "source_runtime_state"):
		return "content_pack_source_runtime_state"
	case strings.Contains(ref, "private_access_exposure_findings"), strings.Contains(ref, "private_access_exposure"):
		return "private_access_exposure_finding"
	case strings.Contains(ref, "ai_investigations"):
		return "soc_case"
	case strings.Contains(ref, "nodes:"):
		return "node"
	case strings.Contains(ref, "connections:"):
		return "connection"
	case strings.Contains(ref, "requests:"):
		return "request"
	case strings.Contains(ref, "traces:"):
		return "trace"
	case strings.Contains(ref, "db_queries:"):
		return "db_query"
	case strings.Contains(ref, "files:"), strings.Contains(ref, "file_events:"):
		return "file"
	case strings.Contains(ref, "process_events:"):
		return "process"
	case strings.Contains(ref, "rules:"):
		return "rule"
	case strings.Contains(ref, "correlations:"):
		return "correlation"
	case strings.Contains(ref, "node_vulnerability_findings"):
		return "vulnerability_finding"
	case strings.Contains(ref, "normalized_events"), strings.Contains(ref, "events:"), strings.Contains(ref, "doris"):
		return "event"
	case strings.Contains(ref, "compliance"):
		return "compliance_evidence"
	case strings.Contains(ref, "posture"):
		return "posture_receipt"
	default:
		return "evidence"
	}
}

func socCaseEvidenceTone(refs []socCaseEvidenceRef) string {
	if len(refs) > 0 {
		return "healthy"
	}
	return "warning"
}

func socCaseTimeline(row storage.AIInvestigation, evidence map[string]any, refs []socCaseEvidenceRef, citation aiWorkflowCitation, createdAt, updatedAt, summary string) []socCaseTimelineItem {
	items := []socCaseTimelineItem{}
	if observedAt := firstSOCCaseEvidenceTimestamp(evidence); observedAt != "" && observedAt != createdAt {
		items = append(items, socCaseTimelineItem{
			Timestamp:   observedAt,
			Event:       "signal.observed",
			Source:      firstNonEmptyString(socCaseString(evidence["collector"]), socCaseString(evidence["parser"]), "collector"),
			CitationID:  firstSOCCaseEvidenceCitation(refs, citation.ID),
			Description: cleanSOCCaseText(firstNonEmptyString(socCaseString(evidence["message"]), summary, row.TriggerEventType)),
		})
	}
	items = append(items, socCaseTimelineItem{
		Timestamp:   createdAt,
		Event:       "case.created",
		Source:      "ai_investigations",
		CitationID:  citation.ID,
		Description: summary,
	})
	if row.NodeID != uuid.Nil {
		items = append(items, socCaseTimelineItem{
			Timestamp:   createdAt,
			Event:       "node.scoped",
			Source:      "ai_investigations",
			CitationID:  "nodes:" + row.NodeID.String(),
			Description: "Case scoped to node " + row.NodeID.String(),
		})
	}
	if strings.TrimSpace(row.TriggerDedupKey) != "" {
		items = append(items, socCaseTimelineItem{
			Timestamp:   createdAt,
			Event:       "signal.deduplicated",
			Source:      "ai_investigations",
			CitationID:  citation.ID,
			Description: "Dedup key " + row.TriggerDedupKey,
		})
	}
	linked := 0
	for _, ref := range refs {
		if strings.EqualFold(ref.ID, citation.ID) {
			continue
		}
		items = append(items, socCaseTimelineItem{
			Timestamp:   createdAt,
			Event:       "evidence.linked",
			Source:      ref.Kind,
			CitationID:  ref.ID,
			Description: "Linked " + ref.Kind + " evidence " + ref.ID,
		})
		linked++
		if linked >= 6 {
			break
		}
	}
	if !row.UpdatedAt.Equal(row.CreatedAt) {
		items = append(items, socCaseTimelineItem{
			Timestamp:   updatedAt,
			Event:       "case.updated",
			Source:      "ai_investigations",
			CitationID:  citation.ID,
			Description: "Investigation case updated",
		})
	}
	return items
}

func firstSOCCaseEvidenceTimestamp(evidence map[string]any) string {
	if len(evidence) == 0 {
		return ""
	}
	for _, key := range []string{"ts", "timestamp", "observed_at", "created_at", "updated_at"} {
		if text := socCaseString(evidence[key]); text != "" {
			if parsed, err := time.Parse(time.RFC3339, text); err == nil {
				return parsed.UTC().Format(time.RFC3339)
			}
			return text
		}
	}
	return ""
}

func firstSOCCaseEvidenceCitation(refs []socCaseEvidenceRef, fallback string) string {
	for _, ref := range refs {
		if ref.Kind == "event" || ref.Kind == "connection" || ref.Kind == "request" {
			return ref.ID
		}
	}
	return fallback
}

func (s *Server) handleCreateSOCCaseNote(w http.ResponseWriter, r *http.Request, principal *auth.Principal, row storage.AIInvestigation) {
	var req struct {
		Note      string   `json:"note"`
		Citations []string `json:"citations"`
		Mentions  []string `json:"mentions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid note payload", http.StatusBadRequest)
		return
	}
	note := boundedToolString(req.Note, 4000)
	if note == "" {
		http.Error(w, "note is required", http.StatusBadRequest)
		return
	}
	citations := sanitizeStringSlice(req.Citations, 100)
	allowedCitations := map[string]struct{}{}
	citation := workflowCitation("ai_investigations", row.ID.String(), "soc_case")
	for _, ref := range socCaseEvidenceRefsForRow(row, parseSOCCaseEvidence(row.Evidence), citation) {
		allowedCitations[ref.ID] = struct{}{}
	}
	for _, citation := range citations {
		if _, ok := allowedCitations[citation]; !ok {
			http.Error(w, "citation is not linked to case evidence", http.StatusBadRequest)
			return
		}
	}
	mentions := sanitizeStringSlice(req.Mentions, 8)
	mentionedUserIDs := make([]uuid.UUID, 0, len(mentions))
	if len(mentions) > 0 {
		users, err := s.store.ListTenantUsers(r.Context(), row.TenantID, "", 1000)
		if err != nil {
			s.logger.Warn("list tenant users for case mentions", zap.Error(err), zap.String("tenant_id", row.TenantID.String()), zap.String("case_id", row.ID.String()))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		teamUsers := make(map[uuid.UUID]struct{}, len(users))
		for _, user := range users {
			teamUsers[user.ID] = struct{}{}
		}
		for _, mention := range mentions {
			id, err := uuid.Parse(mention)
			if err != nil || id == uuid.Nil {
				http.Error(w, "mention must reference a team member in this tenant", http.StatusBadRequest)
				return
			}
			if _, ok := teamUsers[id]; !ok {
				http.Error(w, "mention must reference a team member in this tenant", http.StatusBadRequest)
				return
			}
			mentionedUserIDs = append(mentionedUserIDs, id)
		}
	}
	caseID := row.ID.String()
	metadata := map[string]any{
		"note":        note,
		"citations":   citations,
		"mentions":    mentions,
		"source":      "soc_cases_api",
		"guardrails":  []string{"tenant_scoped", "note_only", "no_enforcement_execution"},
		"case_source": "ai_investigation",
	}
	entry := &storage.AuditLog{
		TenantID:     row.TenantID,
		ActorType:    "user",
		Action:       "soc.case.note.add",
		ResourceType: "ai_investigation",
		ResourceID:   &caseID,
		Metadata:     metadata,
	}
	if principal != nil {
		entry.ActorType = firstNonEmptyString(strings.TrimSpace(principal.Type), "user")
		if strings.TrimSpace(principal.Subject) != "" {
			if user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject); err == nil && user != nil {
				entry.ActorID = user.ID
			} else {
				metadata["created_by_subject"] = boundedToolString(principal.Subject, 256)
			}
		}
	}
	created, err := s.store.CreateAuditLog(r.Context(), entry)
	if err != nil {
		s.logger.Warn("create soc case note", zap.Error(err), zap.String("case_id", row.ID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	for _, mentionedUserID := range mentionedUserIDs {
		if mentionedUserID == entry.ActorID {
			continue
		}
		if _, err := s.store.CreateNotification(r.Context(), storage.CreateNotificationParams{
			TenantID: row.TenantID, RecipientID: mentionedUserID, ActorID: entry.ActorID, Kind: "case_mentioned", CaseID: row.ID, CaseTitle: newSOCCaseResponse(row).Title,
		}); err != nil {
			s.logger.Warn("notify case mention", zap.Error(err), zap.String("tenant_id", row.TenantID.String()), zap.String("case_id", row.ID.String()), zap.String("recipient_id", mentionedUserID.String()))
		}
	}
	writeJSON(w, http.StatusCreated, socCaseNoteFromAudit(*created))
}

func (s *Server) listSOCCaseNotes(ctx context.Context, tenantID, caseID uuid.UUID, limit, offset int) ([]socCaseNoteResponse, int, error) {
	logs, total, err := s.store.ListAuditLogs(ctx, storage.AuditLogFilter{
		TenantID:     tenantID,
		Action:       "soc.case.note.add",
		ResourceType: "ai_investigation",
		ResourceID:   caseID.String(),
	}, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]socCaseNoteResponse, 0, len(logs))
	for _, log := range logs {
		out = append(out, socCaseNoteFromAudit(log))
	}
	return out, total, nil
}

func socCaseNoteFromAudit(log storage.AuditLog) socCaseNoteResponse {
	caseID := ""
	if log.ResourceID != nil {
		caseID = *log.ResourceID
	}
	note := boundedToolString(fmt.Sprint(log.Metadata["note"]), 4000)
	refs := []socCaseEvidenceRef{}
	for _, ref := range stringSliceFromAny(log.Metadata["citations"]) {
		refs = append(refs, socCaseEvidenceRef{ID: ref, Kind: socCaseEvidenceKind(ref)})
	}
	resp := socCaseNoteResponse{
		ID:        log.ID.String(),
		TenantID:  log.TenantID.String(),
		CaseID:    caseID,
		Note:      note,
		Citations: refs,
		Mentions:  stringSliceFromAny(log.Metadata["mentions"]),
		AuditID:   log.ID.String(),
		CreatedAt: log.CreatedAt.UTC().Format(time.RFC3339),
		Guardrails: []string{
			"tenant_scoped",
			"note_only",
			"source_row_citations",
			"no_enforcement_execution",
		},
	}
	if log.ActorID != uuid.Nil {
		resp.CreatedBy = log.ActorID.String()
	}
	return resp
}
