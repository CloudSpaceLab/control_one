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
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	Description *string           `json:"description"`
	Labels      map[string]string `json:"labels"`
}

type createTemplateVersionRequest struct {
	Body           string          `json:"body"`
	Checksum       *string         `json:"checksum"`
	MetadataSchema json.RawMessage `json:"metadata_schema"`
	RolloutNotes   *string         `json:"rollout_notes"`
	Metadata       map[string]any  `json:"metadata"`
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
	default:
		w.Header().Set("Allow", http.MethodGet)
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
