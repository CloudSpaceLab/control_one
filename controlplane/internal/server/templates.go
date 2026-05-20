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
	TenantID          *string                  `json:"tenant_id,omitempty"`
	Name              string                   `json:"name"`
	Provider          string                   `json:"provider"`
	Description       *string                  `json:"description,omitempty"`
	Labels            map[string]string        `json:"labels"`
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
	TenantID    *string           `json:"tenant_id"`
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	Description *string           `json:"description"`
	Labels      map[string]string `json:"labels"`
}

type createTemplateAssignmentRequest struct {
	TenantID  string         `json:"tenant_id"`
	ScopeType string         `json:"scope_type"`
	ScopeID   *string        `json:"scope_id"`
	Selector  map[string]any `json:"selector"`
	ExpiresAt *string        `json:"expires_at"`
}

type templateAssignmentResponse struct {
	ID         string         `json:"id"`
	TemplateID string         `json:"template_id"`
	TenantID   string         `json:"tenant_id"`
	ScopeType  string         `json:"scope_type"`
	ScopeID    *string        `json:"scope_id,omitempty"`
	Selector   map[string]any `json:"selector"`
	AssignedAt string         `json:"assigned_at"`
	AssignedBy *string        `json:"assigned_by,omitempty"`
	ExpiresAt  *string        `json:"expires_at,omitempty"`
}

type createTemplateVersionRequest struct {
	Body           string          `json:"body"`
	Checksum       *string         `json:"checksum"`
	MetadataSchema json.RawMessage `json:"metadata_schema"`
	RolloutNotes   *string         `json:"rollout_notes"`
	Metadata       map[string]any  `json:"metadata"`
}

type updateTemplateRequest struct {
	Name        *string            `json:"name"`
	Provider    *string            `json:"provider"`
	Description *string            `json:"description"`
	Labels      *map[string]string `json:"labels"`
	Archived    *bool              `json:"archived"`
}

func (s *Server) handleTemplatesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.handleListTemplates(w, r, principal)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreateTemplate(w, r, principal)
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
		if segments[1] == "assignments" {
			s.handleTemplateAssignmentsCollection(w, r, templateID)
			return
		}
		if segments[1] == "rollouts" {
			s.handleTemplateRollouts(w, r, templateID)
			return
		}
		http.NotFound(w, r)
	case 3:
		if segments[1] == "assignments" {
			assignmentID, err := uuid.Parse(segments[2])
			if err != nil {
				http.Error(w, "invalid assignment id", http.StatusBadRequest)
				return
			}
			s.handleDeleteTemplateAssignment(w, r, templateID, assignmentID)
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
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.handleGetTemplate(w, r, templateID, principal)
	case http.MethodPatch:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleUpdateTemplate(w, r, templateID, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTemplateVersions(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		if s.store == nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, ok := s.requireTemplateAccess(w, r, principal, templateID, roleViewer, roleOperator, roleAdmin); !ok {
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
		if _, ok := s.requireMutableTemplateAccess(w, r, principal, templateID, roleAdmin); !ok {
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
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		if s.store == nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, ok := s.requireMutableTemplateAccess(w, r, principal, templateID, roleAdmin); !ok {
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

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
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
		IncludeArchived: parseBoolQuery(r.URL.Query().Get("include_archived")),
		IncludeGlobal:   true,
	}
	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantParam)
	if err != nil || tenantID == uuid.Nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer, roleOperator, roleAdmin) {
		return
	}
	filter.TenantID = tenantID

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

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
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
	var tenantID uuid.UUID
	if req.TenantID == nil || strings.TrimSpace(*req.TenantID) == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	parsed, err := uuid.Parse(strings.TrimSpace(*req.TenantID))
	if err != nil || parsed == uuid.Nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, parsed, roleAdmin) {
		return
	}
	tenantID = parsed

	template := &storage.ProvisioningTemplate{
		TenantID: tenantID,
		Name:     req.Name,
		Provider: strings.TrimSpace(req.Provider),
		Labels:   sanitizeLabels(req.Labels),
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

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request, templateID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	template, ok := s.requireTemplateAccess(w, r, principal, templateID, roleViewer, roleOperator, roleAdmin)
	if !ok {
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

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request, templateID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := s.requireMutableTemplateAccess(w, r, principal, templateID, roleAdmin); !ok {
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

func (s *Server) handleTemplateAssignmentsCollection(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.handleListTemplateAssignments(w, r, templateID, principal)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreateTemplateAssignment(w, r, templateID, principal)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreateTemplateAssignment(w http.ResponseWriter, r *http.Request, templateID uuid.UUID, principal *auth.Principal) {
	template, ok := s.requireTemplateAccess(w, r, principal, templateID, roleAdmin)
	if !ok {
		return
	}

	var req createTemplateAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil || tenantID == uuid.Nil {
		http.Error(w, "valid tenant_id is required", http.StatusBadRequest)
		return
	}
	if template.TenantID != uuid.Nil && template.TenantID != tenantID {
		http.Error(w, "assignment tenant_id must match template tenant_id", http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleAdmin) {
		return
	}

	params := storage.CreateProvisioningTemplateAssignmentParams{
		TemplateID: templateID,
		TenantID:   tenantID,
		ScopeType:  strings.TrimSpace(req.ScopeType),
		Selector:   req.Selector,
	}
	if params.ScopeType == "" {
		params.ScopeType = storage.AssignmentScopeTenant
	}
	if req.ScopeID != nil {
		scopeID, err := uuid.Parse(strings.TrimSpace(*req.ScopeID))
		if err != nil || scopeID == uuid.Nil {
			http.Error(w, "invalid scope_id", http.StatusBadRequest)
			return
		}
		params.ScopeID = scopeID
	}
	if err := s.validatePolicyAssignmentScope(r.Context(), tenantID, storage.CreatePolicyAssignmentParams{
		TenantID:  tenantID,
		ScopeType: params.ScopeType,
		ScopeID:   params.ScopeID,
		Selector:  params.Selector,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			http.Error(w, "expires_at must be RFC3339", http.StatusBadRequest)
			return
		}
		params.ExpiresAt = &t
	}
	if principal != nil && strings.TrimSpace(principal.Subject) != "" {
		if user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject); err == nil && user != nil {
			params.AssignedBy = &user.ID
		}
	}

	assignment, err := s.store.CreateProvisioningTemplateAssignment(r.Context(), params)
	if err != nil {
		s.logger.Error("create template assignment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, newTemplateAssignmentResponse(assignment))
}

func (s *Server) handleListTemplateAssignments(w http.ResponseWriter, r *http.Request, templateID uuid.UUID, principal *auth.Principal) {
	template, ok := s.requireTemplateAccess(w, r, principal, templateID, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	assignments, total, err := s.store.ListProvisioningTemplateAssignments(r.Context(), templateID, limit, offset)
	if err != nil {
		s.logger.Error("list template assignments", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]templateAssignmentResponse, 0, len(assignments))
	for _, a := range assignments {
		if template.TenantID == uuid.Nil {
			if err := s.checkTenantAccess(r.Context(), principal, a.TenantID, roleViewer, roleOperator, roleAdmin); err != nil {
				continue
			}
		}
		items = append(items, newTemplateAssignmentResponse(&a))
	}
	if len(items) != len(assignments) {
		total = len(items)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

func (s *Server) handleDeleteTemplateAssignment(w http.ResponseWriter, r *http.Request, templateID, assignmentID uuid.UUID) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	if _, ok := s.requireTemplateAccess(w, r, principal, templateID, roleAdmin); !ok {
		return
	}
	assignment, err := s.store.GetProvisioningTemplateAssignment(r.Context(), assignmentID)
	if err != nil {
		s.logger.Error("get template assignment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if assignment == nil || assignment.TemplateID != templateID {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, assignment.TenantID, roleAdmin) {
		return
	}
	if err := s.store.DeleteProvisioningTemplateAssignment(r.Context(), assignmentID); err != nil {
		s.logger.Error("delete template assignment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requireTemplateAccess(w http.ResponseWriter, r *http.Request, principal *auth.Principal, templateID uuid.UUID, roles ...string) (*storage.ProvisioningTemplate, bool) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	template, err := s.store.GetProvisioningTemplate(r.Context(), templateID)
	if err != nil {
		s.logger.Error("get provisioning template", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, false
	}
	if template == nil {
		http.NotFound(w, r)
		return nil, false
	}
	if template.TenantID != uuid.Nil && !s.requireTenantAccess(w, r, principal, template.TenantID, roles...) {
		return nil, false
	}
	return template, true
}

func (s *Server) requireMutableTemplateAccess(w http.ResponseWriter, r *http.Request, principal *auth.Principal, templateID uuid.UUID, roles ...string) (*storage.ProvisioningTemplate, bool) {
	template, ok := s.requireTemplateAccess(w, r, principal, templateID, roles...)
	if !ok {
		return nil, false
	}
	if template.TenantID == uuid.Nil {
		http.Error(w, "platform-global templates are read-only; create a tenant-owned template before editing", http.StatusForbidden)
		return nil, false
	}
	return template, true
}

func newTemplateResponse(tpl storage.ProvisioningTemplate, promoted *templateVersionResponse) templateResponse {
	resp := templateResponse{
		ID:        tpl.ID.String(),
		Name:      tpl.Name,
		Provider:  tpl.Provider,
		Labels:    tpl.Labels,
		CreatedAt: formatTime(tpl.CreatedAt),
		UpdatedAt: formatTime(tpl.UpdatedAt),
	}
	if resp.Labels == nil {
		resp.Labels = map[string]string{}
	}
	if tpl.TenantID != uuid.Nil {
		tid := tpl.TenantID.String()
		resp.TenantID = &tid
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

func newTemplateAssignmentResponse(a *storage.ProvisioningTemplateAssignment) templateAssignmentResponse {
	resp := templateAssignmentResponse{
		ID:         a.ID.String(),
		TemplateID: a.TemplateID.String(),
		TenantID:   a.TenantID.String(),
		ScopeType:  a.ScopeType,
		Selector:   a.Selector,
		AssignedAt: formatTime(a.AssignedAt),
	}
	if resp.Selector == nil {
		resp.Selector = map[string]any{}
	}
	if a.ScopeID != uuid.Nil {
		id := a.ScopeID.String()
		resp.ScopeID = &id
	}
	if a.AssignedBy != nil {
		id := a.AssignedBy.String()
		resp.AssignedBy = &id
	}
	if a.ExpiresAt.Valid {
		resp.ExpiresAt = formatNullTime(a.ExpiresAt)
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
