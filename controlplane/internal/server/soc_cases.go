package server

import (
	"context"
	"encoding/json"
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
	Citations        []aiWorkflowCitation   `json:"citations"`
	CoverageBadges   []socCaseCoverageBadge `json:"coverage_badges"`
	ExportURL        string                 `json:"export_url"`
	CreatedAt        string                 `json:"created_at"`
	UpdatedAt        string                 `json:"updated_at"`
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
	AuditID    string               `json:"audit_id"`
	CreatedAt  string               `json:"created_at"`
	CreatedBy  string               `json:"created_by,omitempty"`
	Guardrails []string             `json:"guardrails"`
}

func (s *Server) handleSOCCasesCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
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
	for _, row := range rows {
		out = append(out, newSOCCaseResponse(row))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[socCaseResponse]{
		Data:       out,
		Pagination: newPaginationMeta(total, limit, offset, len(out)),
	})
}

func (s *Server) handleSOCCaseSubroutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
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
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/soc/cases/")
	segments := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(segments) == 0 || segments[0] == "" || len(segments) > 2 {
		http.NotFound(w, r)
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
	resp := newSOCCaseResponse(*row)
	if len(segments) == 2 {
		switch segments[1] {
		case "export":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
				return
			}
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
	notes, _, err := s.listSOCCaseNotes(r.Context(), tenantID, id, 100, 0)
	if err != nil {
		s.logger.Warn("list soc case notes for detail", zap.Error(err), zap.String("case_id", id.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp.Notes = notes
	writeJSON(w, http.StatusOK, resp)
}

func newSOCCaseResponse(row storage.AIInvestigation) socCaseResponse {
	evidence := parseSOCCaseEvidence(row.Evidence)
	citation := workflowCitation("ai_investigations", row.ID.String(), "soc_case")
	resp := socCaseResponse{
		CaseID:           row.ID.String(),
		TenantID:         row.TenantID.String(),
		Title:            firstNonEmptyString(strings.TrimSpace(row.Summary), strings.TrimSpace(row.TriggerEventType), "Investigation case"),
		Status:           string(row.Status),
		Severity:         firstNonEmptyString(strings.ToLower(strings.TrimSpace(row.Severity)), "info"),
		Source:           "ai_investigation",
		TriggerType:      row.TriggerType,
		TriggerEventType: row.TriggerEventType,
		DedupKey:         row.TriggerDedupKey,
		Summary:          row.Summary,
		Evidence:         evidence,
		EvidenceRefs:     socCaseEvidenceRefs(evidence),
		Citations:        []aiWorkflowCitation{citation},
		CoverageBadges: []socCaseCoverageBadge{
			{ID: "source_row_citations", Label: "Source-row cited", Tone: "healthy"},
			{ID: "evidence_linked", Label: "Evidence linked", Tone: socCaseEvidenceTone(evidence)},
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
	resp.Timeline = []socCaseTimelineItem{
		{
			Timestamp:   resp.CreatedAt,
			Event:       "case.created",
			Source:      "ai_investigations",
			CitationID:  citation.ID,
			Description: resp.Summary,
		},
	}
	if !row.UpdatedAt.Equal(row.CreatedAt) {
		resp.Timeline = append(resp.Timeline, socCaseTimelineItem{
			Timestamp:   resp.UpdatedAt,
			Event:       "case.updated",
			Source:      "ai_investigations",
			CitationID:  citation.ID,
			Description: "Investigation case updated",
		})
	}
	return resp
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
	for _, value := range stringSliceFromAny(evidence["citations"]) {
		refs = append(refs, socCaseEvidenceRef{ID: value, Kind: socCaseEvidenceKind(value)})
	}
	if details, ok := evidence["details"].(map[string]any); ok {
		for _, value := range stringSliceFromAny(details["citations"]) {
			refs = append(refs, socCaseEvidenceRef{ID: value, Kind: socCaseEvidenceKind(value)})
		}
	}
	return refs
}

func socCaseEvidenceKind(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	switch {
	case strings.Contains(ref, "node_vulnerability_findings"):
		return "vulnerability_finding"
	case strings.Contains(ref, "normalized_events"), strings.Contains(ref, "doris"):
		return "event"
	case strings.Contains(ref, "compliance"):
		return "compliance_evidence"
	case strings.Contains(ref, "posture"):
		return "posture_receipt"
	default:
		return "evidence"
	}
}

func socCaseEvidenceTone(evidence map[string]any) string {
	if len(socCaseEvidenceRefs(evidence)) > 0 {
		return "healthy"
	}
	return "warning"
}

func (s *Server) handleCreateSOCCaseNote(w http.ResponseWriter, r *http.Request, principal *auth.Principal, row storage.AIInvestigation) {
	var req struct {
		Note      string   `json:"note"`
		Citations []string `json:"citations"`
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
	for _, ref := range socCaseEvidenceRefs(parseSOCCaseEvidence(row.Evidence)) {
		allowedCitations[ref.ID] = struct{}{}
	}
	for _, citation := range citations {
		if _, ok := allowedCitations[citation]; !ok {
			http.Error(w, "citation is not linked to case evidence", http.StatusBadRequest)
			return
		}
	}
	caseID := row.ID.String()
	metadata := map[string]any{
		"note":        note,
		"citations":   citations,
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
