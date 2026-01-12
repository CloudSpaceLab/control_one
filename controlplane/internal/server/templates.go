package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type templateResponse struct {
	ID                string                   `json:"id"`
	Name              string                   `json:"name"`
	Provider          string                   `json:"provider"`
	Description       *string                  `json:"description,omitempty"`
	Labels            map[string]string        `json:"labels"`
	TemplateType      string                   `json:"template_type"`
	CreatedAt         string                   `json:"created_at"`
	UpdatedAt         string                   `json:"updated_at"`
	ArchivedAt        *string                  `json:"archived_at,omitempty"`
	PromotedVersionID *string                  `json:"promoted_version_id,omitempty"`
	PromotedVersion   *templateVersionResponse `json:"promoted_version,omitempty"`
}

type templateVersionResponse struct {
	ID             string          `json:"id"`
	Version        int             `json:"version"`
	Checksum       *string         `json:"checksum,omitempty"`
	Body           string          `json:"body"`
	MetadataSchema json.RawMessage `json:"metadata_schema,omitempty"`
	RolloutNotes   *string         `json:"rollout_notes,omitempty"`
	CreatedBy      *string         `json:"created_by,omitempty"`
	CreatedAt      string          `json:"created_at"`
	PromotedAt     *string         `json:"promoted_at,omitempty"`
}

type createTemplateRequest struct {
	Name         string            `json:"name"`
	Provider     string            `json:"provider"`
	Description  *string           `json:"description"`
	Labels       map[string]string `json:"labels"`
	TemplateType string            `json:"template_type"`
}

type createTemplateVersionRequest struct {
	Body           string          `json:"body"`
	Checksum       *string         `json:"checksum"`
	MetadataSchema json.RawMessage `json:"metadata_schema"`
	RolloutNotes   *string         `json:"rollout_notes"`
	Metadata       map[string]any  `json:"metadata"`
}

type updateTemplateRequest struct {
	Name         *string            `json:"name"`
	Provider     *string            `json:"provider"`
	Description  *string            `json:"description"`
	Labels       *map[string]string `json:"labels"`
	TemplateType *string            `json:"template_type"`
	Archived     *bool              `json:"archived"`
}

type templateExecutionRequest struct {
	TemplateID   string         `json:"template_id"`
	TemplateType string         `json:"template_type"`
	TargetType   string         `json:"target_type"` // 'tenant', 'node', 'global'
	TargetID     *string        `json:"target_id,omitempty"`
	Parameters   map[string]any `json:"parameters"`
	DryRun       bool           `json:"dry_run"`
}

type templateExecutionResponse struct {
	ID                string          `json:"id"`
	TemplateID        string          `json:"template_id"`
	TemplateType      string          `json:"template_type"`
	TargetType        string          `json:"target_type"`
	TargetID          *string         `json:"target_id,omitempty"`
	Parameters        map[string]any  `json:"parameters"`
	Status            string          `json:"status"` // 'pending', 'running', 'completed', 'failed'
	Result            json.RawMessage `json:"result,omitempty"`
	Error             *string         `json:"error,omitempty"`
	CreatedAt         string          `json:"created_at"`
	StartedAt         *string         `json:"started_at,omitempty"`
	CompletedAt       *string         `json:"completed_at,omitempty"`
	CreatedJobs       []string        `json:"created_jobs,omitempty"`
	ComplianceResults []string        `json:"compliance_results,omitempty"`
}

func (s *Server) handleTemplatesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListTemplates(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateTemplate(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTemplateSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/templates/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(trimmed, "/")

	templateID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid template id", http.StatusBadRequest)
		return
	}

	switch len(segments) {
	case 1:
		s.handleTemplateResource(w, r, templateID)
	case 2:
		if segments[1] == "versions" {
			s.handleTemplateVersions(w, r, templateID)
			return
		}
		if segments[1] == "rollouts" {
			s.handleTemplateRollouts(w, r, templateID)
			return
		}
		if segments[1] == "execute" {
			s.handleTemplateExecution(w, r, templateID)
			return
		}
		if segments[1] == "archive" {
			s.handleArchiveTemplate(w, r, templateID)
			return
		}
		if segments[1] == "unarchive" {
			s.handleUnarchiveTemplate(w, r, templateID)
			return
		}
		http.NotFound(w, r)
	case 4:
		if segments[1] == "versions" && segments[3] == "promote" {
			versionNumber, verErr := strconv.Atoi(segments[2])
			if verErr != nil || versionNumber <= 0 {
				http.Error(w, "invalid version number", http.StatusBadRequest)
				return
			}
			s.handlePromoteTemplateVersion(w, r, templateID, versionNumber)
			return
		}
		if segments[1] == "rollouts" {
			rolloutID, err := uuid.Parse(segments[2])
			if err != nil {
				http.Error(w, "invalid rollout id", http.StatusBadRequest)
				return
			}
			if segments[3] == "cancel" {
				s.handleCancelRollout(w, r, templateID, rolloutID)
				return
			}
			s.handleRolloutResource(w, r, templateID, rolloutID)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleTemplateResource(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetTemplate(w, r, templateID)
	case http.MethodPatch:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleUpdateTemplate(w, r, templateID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTemplateVersions(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		if s.store == nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}

		limit, offset, err := parseLimitOffset(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		versions, total, err := s.store.ListProvisioningTemplateVersions(r.Context(), templateID, limit, offset)
		if err != nil {
			s.logger.Error("list template versions", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		items := make([]templateVersionResponse, 0, len(versions))
		for i := range versions {
			if resp := newTemplateVersionResponse(&versions[i]); resp != nil {
				items = append(items, *resp)
			}
		}

		resp := paginatedResponse[templateVersionResponse]{
			Data:       items,
			Pagination: newPaginationMeta(total, limit, offset, len(items)),
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreateTemplateVersion(w, r, templateID, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePromoteTemplateVersion(w http.ResponseWriter, r *http.Request, templateID uuid.UUID, versionNumber int) {
	switch r.Method {
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		if s.store == nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		version, err := s.store.PromoteProvisioningTemplateVersion(r.Context(), templateID, versionNumber)
		if err != nil {
			http.Error(w, fmt.Sprintf("promote template version: %v", err), http.StatusBadRequest)
			return
		}
		resp := newTemplateVersionResponse(version)
		writeJSON(w, http.StatusOK, resp)
	default:
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filter := storage.ProvisioningTemplateFilter{
		Provider:        strings.TrimSpace(r.URL.Query().Get("provider")),
		NamePrefix:      strings.TrimSpace(r.URL.Query().Get("name_prefix")),
		TemplateType:    strings.TrimSpace(r.URL.Query().Get("type")),
		IncludeArchived: parseBoolQuery(r.URL.Query().Get("include_archived")),
	}

	templates, total, err := s.store.ListProvisioningTemplates(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list provisioning templates", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]templateResponse, 0, len(templates))
	for _, tpl := range templates {
		respItems = append(respItems, newTemplateResponse(tpl, nil))
	}

	resp := paginatedResponse[templateResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req createTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	template := &storage.ProvisioningTemplate{
		Name:         req.Name,
		Provider:     strings.TrimSpace(req.Provider),
		TemplateType: strings.TrimSpace(req.TemplateType),
		Labels:       sanitizeLabels(req.Labels),
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		if desc != "" {
			template.Description = sql.NullString{String: desc, Valid: true}
		}
	}

	created, err := s.store.CreateProvisioningTemplate(r.Context(), template)
	if err != nil {
		http.Error(w, fmt.Sprintf("create template failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newTemplateResponse(*created, nil)
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	template, err := s.store.GetProvisioningTemplate(r.Context(), templateID)
	if err != nil {
		s.logger.Error("get provisioning template", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if template == nil {
		http.NotFound(w, r)
		return
	}

	var promotedResp *templateVersionResponse
	if template.PromotedVersionID != nil {
		if version, err := s.store.GetPromotedProvisioningTemplateVersion(r.Context(), template.ID); err != nil {
			s.logger.Warn("fetch promoted template version", zap.Error(err))
		} else if version != nil {
			promotedResp = newTemplateVersionResponse(version)
		}
	}

	resp := newTemplateResponse(*template, promotedResp)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req updateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	params := storage.UpdateProvisioningTemplateParams{}
	var hasUpdate bool

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		req.Name = &name
		params.Name = req.Name
		hasUpdate = true
	}
	if req.Provider != nil {
		provider := strings.TrimSpace(*req.Provider)
		req.Provider = &provider
		params.Provider = req.Provider
		hasUpdate = true
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		req.Description = &desc
		params.Description = req.Description
		hasUpdate = true
	}
	if req.Labels != nil {
		sanitized := sanitizeLabels(*req.Labels)
		params.Labels = &sanitized
		hasUpdate = true
	}
	if req.TemplateType != nil {
		templateType := strings.TrimSpace(*req.TemplateType)
		params.TemplateType = &templateType
		hasUpdate = true
	}
	if req.Archived != nil {
		params.Archived = req.Archived
		hasUpdate = true
	}

	if !hasUpdate {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	updated, err := s.store.UpdateProvisioningTemplate(r.Context(), templateID, params)
	if err != nil {
		http.Error(w, fmt.Sprintf("update template: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	var promotedResp *templateVersionResponse
	if updated.PromotedVersionID != nil {
		if version, err := s.store.GetPromotedProvisioningTemplateVersion(r.Context(), updated.ID); err == nil && version != nil {
			promotedResp = newTemplateVersionResponse(version)
		}
	}

	resp := newTemplateResponse(*updated, promotedResp)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateTemplateVersion(w http.ResponseWriter, r *http.Request, templateID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req createTemplateVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		http.Error(w, "body is required", http.StatusBadRequest)
		return
	}

	params := storage.CreateTemplateVersionParams{
		TemplateID:     templateID,
		Body:           req.Body,
		MetadataSchema: req.MetadataSchema,
		RolloutNotes:   req.RolloutNotes,
	}
	if req.Checksum != nil {
		checksum := strings.TrimSpace(*req.Checksum)
		params.Checksum = &checksum
	}
	if principal != nil && strings.TrimSpace(principal.Subject) != "" {
		if user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject); err == nil && user != nil {
			params.CreatedBy = &user.ID
		}
	}

	version, err := s.store.CreateProvisioningTemplateVersion(r.Context(), params)
	if err != nil {
		http.Error(w, fmt.Sprintf("create template version failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newTemplateVersionResponse(version)
	writeJSON(w, http.StatusCreated, resp)
}

func newTemplateResponse(tpl storage.ProvisioningTemplate, promoted *templateVersionResponse) templateResponse {
	resp := templateResponse{
		ID:           tpl.ID.String(),
		Name:         tpl.Name,
		Provider:     tpl.Provider,
		TemplateType: tpl.TemplateType,
		Labels:       tpl.Labels,
		CreatedAt:    formatTime(tpl.CreatedAt),
		UpdatedAt:    formatTime(tpl.UpdatedAt),
	}
	if resp.Labels == nil {
		resp.Labels = map[string]string{}
	}
	if tpl.Description.Valid {
		desc := tpl.Description.String
		resp.Description = &desc
	}
	if tpl.ArchivedAt.Valid {
		resp.ArchivedAt = formatNullTime(tpl.ArchivedAt)
	}
	if tpl.PromotedVersionID != nil {
		id := tpl.PromotedVersionID.String()
		resp.PromotedVersionID = &id
	}
	if promoted != nil {
		resp.PromotedVersion = promoted
	}
	return resp
}

func newTemplateVersionResponse(version *storage.ProvisioningTemplateVersion) *templateVersionResponse {
	resp := &templateVersionResponse{
		ID:        version.ID.String(),
		Version:   version.Version,
		Body:      version.Body,
		CreatedAt: formatTime(version.CreatedAt),
	}
	if version.Checksum.Valid {
		val := version.Checksum.String
		resp.Checksum = &val
	}
	if len(version.MetadataSchema) > 0 {
		resp.MetadataSchema = version.MetadataSchema
	}
	if version.RolloutNotes.Valid {
		n := version.RolloutNotes.String
		resp.RolloutNotes = &n
	}
	if version.CreatedBy != nil {
		id := version.CreatedBy.String()
		resp.CreatedBy = &id
	}
	if version.PromotedAt.Valid {
		resp.PromotedAt = formatNullTime(version.PromotedAt)
	}
	return resp
}

func sanitizeLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(labels))
	for k, v := range labels {
		trimmedKey := strings.TrimSpace(k)
		if trimmedKey == "" {
			continue
		}
		result[trimmedKey] = strings.TrimSpace(v)
	}
	return result
}

func parseBoolQuery(val string) bool {
	v := strings.TrimSpace(strings.ToLower(val))
	return v == "true" || v == "1" || v == "yes"
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatNullTime(nt sql.NullTime) *string {
	if !nt.Valid {
		return nil
	}
	formatted := nt.Time.UTC().Format(time.RFC3339)
	return &formatted
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleTemplateExecution(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	switch r.Method {
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateTemplateExecution(w, r, templateID)
	default:
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreateTemplateExecution(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req templateExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	// Validate request
	if strings.TrimSpace(req.TemplateType) == "" {
		http.Error(w, "template_type is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.TargetType) == "" {
		http.Error(w, "target_type is required", http.StatusBadRequest)
		return
	}
	if !isValidTargetType(req.TargetType) {
		http.Error(w, "target_type must be one of: tenant, node, global", http.StatusBadRequest)
		return
	}
	if req.TargetID == nil && req.TargetType != "global" {
		http.Error(w, "target_id is required for tenant and node targets", http.StatusBadRequest)
		return
	}

	// Get user ID from principal
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	var createdBy *uuid.UUID
	if principal != nil && strings.TrimSpace(principal.Subject) != "" {
		if user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject); err == nil && user != nil {
			createdBy = &user.ID
		}
	}

	// Parse target ID if provided
	var targetID *uuid.UUID
	if req.TargetID != nil {
		if id, err := uuid.Parse(*req.TargetID); err == nil {
			targetID = &id
		} else {
			http.Error(w, "invalid target_id format", http.StatusBadRequest)
			return
		}
	}

	// Create execution
	params := storage.CreateTemplateExecutionParams{
		TemplateID:   templateID,
		TemplateType: strings.TrimSpace(req.TemplateType),
		TargetType:   strings.TrimSpace(req.TargetType),
		TargetID:     targetID,
		Parameters:   req.Parameters,
		CreatedBy:    createdBy,
	}

	execution, err := s.store.CreateTemplateExecution(r.Context(), params)
	if err != nil {
		s.logger.Error("create template execution", zap.Error(err))
		http.Error(w, fmt.Sprintf("create execution failed: %v", err), http.StatusBadRequest)
		return
	}

	s.logger.Info("template execution created",
		zap.String("execution_id", execution.ID.String()),
		zap.String("template_id", templateID.String()),
		zap.String("template_type", execution.TemplateType),
		zap.String("target_type", execution.TargetType))

	resp := newTemplateExecutionResponse(execution)
	writeJSON(w, http.StatusCreated, resp)
}

func newTemplateExecutionResponse(execution *storage.TemplateExecution) templateExecutionResponse {
	resp := templateExecutionResponse{
		ID:           execution.ID.String(),
		TemplateID:   execution.TemplateID.String(),
		TemplateType: execution.TemplateType,
		TargetType:   execution.TargetType,
		Parameters:   execution.Parameters,
		Status:       execution.Status,
		CreatedAt:    formatTime(execution.CreatedAt),
	}

	if execution.TargetID != nil {
		id := execution.TargetID.String()
		resp.TargetID = &id
	}

	if execution.StartedAt.Valid {
		startedAt := formatNullTime(execution.StartedAt)
		resp.StartedAt = startedAt
	}

	if execution.CompletedAt.Valid {
		completedAt := formatNullTime(execution.CompletedAt)
		resp.CompletedAt = completedAt
	}

	if execution.ErrorMessage != nil {
		resp.Error = execution.ErrorMessage
	}

	if len(execution.ExecutionResult) > 0 {
		resp.Result = execution.ExecutionResult
	}

	return resp
}

func isValidTargetType(targetType string) bool {
	switch strings.TrimSpace(strings.ToLower(targetType)) {
	case "tenant", "node", "global":
		return true
	default:
		return false
	}
}

// Helper function for nullable UUID pointers
func nullableUUIDPtr(id *uuid.UUID) any {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	return *id
}

func (s *Server) handleArchiveTemplate(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	switch r.Method {
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleArchiveTemplateAction(w, r, templateID)
	default:
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUnarchiveTemplate(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	switch r.Method {
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleUnarchiveTemplateAction(w, r, templateID)
	default:
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleArchiveTemplateAction(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	// Archive the template by setting the archived flag
	updated, err := s.store.UpdateProvisioningTemplate(r.Context(), templateID, storage.UpdateProvisioningTemplateParams{
		Archived: func() *bool { b := true; return &b }(),
	})
	if err != nil {
		s.logger.Error("archive template", zap.Error(err))
		http.Error(w, fmt.Sprintf("archive template failed: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	var promotedResp *templateVersionResponse
	if updated.PromotedVersionID != nil {
		if version, err := s.store.GetPromotedProvisioningTemplateVersion(r.Context(), updated.ID); err == nil && version != nil {
			promotedResp = newTemplateVersionResponse(version)
		}
	}

	resp := newTemplateResponse(*updated, promotedResp)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUnarchiveTemplateAction(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	// Unarchive the template by setting the archived flag to false
	updated, err := s.store.UpdateProvisioningTemplate(r.Context(), templateID, storage.UpdateProvisioningTemplateParams{
		Archived: func() *bool { b := false; return &b }(),
	})
	if err != nil {
		s.logger.Error("unarchive template", zap.Error(err))
		http.Error(w, fmt.Sprintf("unarchive template failed: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	var promotedResp *templateVersionResponse
	if updated.PromotedVersionID != nil {
		if version, err := s.store.GetPromotedProvisioningTemplateVersion(r.Context(), updated.ID); err == nil && version != nil {
			promotedResp = newTemplateVersionResponse(version)
		}
	}

	resp := newTemplateResponse(*updated, promotedResp)
	writeJSON(w, http.StatusOK, resp)
}
