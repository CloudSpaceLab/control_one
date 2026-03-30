package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type policyResponse struct {
	ID          string            `json:"id"`
	TenantID    *string           `json:"tenant_id,omitempty"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	RuleType    string            `json:"rule_type"`
	Enabled     bool              `json:"enabled"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	ArchivedAt  *string           `json:"archived_at,omitempty"`
}

type policyVersionResponse struct {
	ID             string         `json:"id"`
	Version        int            `json:"version"`
	RuleDefinition string         `json:"rule_definition"`
	Checksum       *string        `json:"checksum,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedBy      *string        `json:"created_by,omitempty"`
	CreatedAt      string         `json:"created_at"`
	PromotedAt     *string        `json:"promoted_at,omitempty"`
}

type createPolicyRequest struct {
	TenantID    *string          `json:"tenant_id"`
	Name        string           `json:"name"`
	Description *string          `json:"description"`
	RuleType    string          `json:"rule_type"`
	Enabled     bool             `json:"enabled"`
	Labels      map[string]string `json:"labels"`
}

type updatePolicyRequest struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	RuleType    *string            `json:"rule_type"`
	Enabled     *bool              `json:"enabled"`
	Labels      *map[string]string `json:"labels"`
	Archived    *bool              `json:"archived"`
}

type createPolicyVersionRequest struct {
	RuleDefinition string         `json:"rule_definition"`
	Checksum       *string        `json:"checksum"`
	Metadata       map[string]any `json:"metadata"`
}

func (s *Server) handlePoliciesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListPolicies(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreatePolicy(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handlePolicySubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(trimmed, "/")

	policyID, err := uuid.Parse(segments[0])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid policy id")
		return
	}

	switch len(segments) {
	case 1:
		s.handlePolicyResource(w, r, policyID)
	case 2:
		if segments[1] == "versions" {
			s.handlePolicyVersions(w, r, policyID)
			return
		}
		http.NotFound(w, r)
	case 4:
		if segments[1] == "versions" && segments[3] == "promote" {
			versionNumber, verErr := strconv.Atoi(segments[2])
			if verErr != nil || versionNumber <= 0 {
				writeError(w, r, http.StatusBadRequest, "invalid version number")
				return
			}
			s.handlePromotePolicyVersion(w, r, policyID, versionNumber)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handlePolicyResource(w http.ResponseWriter, r *http.Request, policyID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetPolicy(w, r, policyID)
	case http.MethodPatch:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleUpdatePolicy(w, r, policyID)
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleDeletePolicy(w, r, policyID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch, http.MethodDelete}, ", "))
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handlePolicyVersions(w http.ResponseWriter, r *http.Request, policyID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		if s.store == nil {
			writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
			return
		}

		limit, offset, err := parseLimitOffset(r.URL.Query())
		if err != nil {
			writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}

		versions, total, err := s.store.ListPolicyVersions(r.Context(), policyID, limit, offset)
		if err != nil {
			s.logger.Error("list policy versions", zap.Error(err))
			writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
			return
		}

		items := make([]policyVersionResponse, 0, len(versions))
		for i := range versions {
			items = append(items, newPolicyVersionResponse(&versions[i]))
		}

		resp := paginatedResponse[policyVersionResponse]{
			Data:       items,
			Pagination: newPaginationMeta(total, limit, offset, len(items)),
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreatePolicyVersion(w, r, policyID, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handlePromotePolicyVersion(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, versionNumber int) {
	switch r.Method {
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		if s.store == nil {
			writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
			return
		}
		version, err := s.store.PromotePolicyVersion(r.Context(), policyID, versionNumber)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, fmt.Sprintf("promote policy version: %v", err))
			return
		}
		resp := newPolicyVersionResponse(version)
		writeJSON(w, http.StatusOK, resp)
	default:
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	filter := storage.PolicyFilter{
		NamePrefix:      strings.TrimSpace(r.URL.Query().Get("name_prefix")),
		RuleType:        strings.TrimSpace(r.URL.Query().Get("rule_type")),
		IncludeArchived: parseBoolQuery(r.URL.Query().Get("include_archived")),
	}

	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid tenant_id")
			return
		}
		filter.TenantID = parsed
	}

	if enabledParam := strings.TrimSpace(r.URL.Query().Get("enabled")); enabledParam != "" {
		enabled := parseBoolQuery(enabledParam)
		filter.Enabled = &enabled
	}

	policies, total, err := s.store.ListPolicies(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list policies", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}

	respItems := make([]policyResponse, 0, len(policies))
	for _, p := range policies {
		respItems = append(respItems, newPolicyResponse(p))
	}

	resp := paginatedResponse[policyResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req createPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid payload: %v", err))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, r, http.StatusBadRequest, "name is required")
		return
	}
	req.RuleType = strings.TrimSpace(req.RuleType)
	if req.RuleType == "" {
		writeError(w, r, http.StatusBadRequest, "rule_type is required")
		return
	}

	var tenantID uuid.UUID
	if req.TenantID != nil {
		parsed, err := uuid.Parse(*req.TenantID)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid tenant_id")
			return
		}
		tenantID = parsed
	}

	params := storage.CreatePolicyParams{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		RuleType:    req.RuleType,
		Enabled:     req.Enabled,
		Labels:      sanitizeLabels(req.Labels),
	}

	created, err := s.store.CreatePolicy(r.Context(), params)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("create policy failed: %v", err))
		return
	}

	resp := newPolicyResponse(*created)
	writeJSON(w, http.StatusCreated, resp)

	s.recordAudit(r.Context(), principal, created.TenantID, "policy.create", "policy", created.ID.String(), map[string]any{
		"name":     created.Name,
		"rule_type": created.RuleType,
	})
}

func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request, policyID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	policy, err := s.store.GetPolicy(r.Context(), policyID)
	if err != nil {
		s.logger.Error("get policy", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}
	if policy == nil {
		http.NotFound(w, r)
		return
	}

	resp := newPolicyResponse(*policy)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request, policyID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req updatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid payload: %v", err))
		return
	}

	params := storage.UpdatePolicyParams{}
	var hasUpdate bool

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, r, http.StatusBadRequest, "name cannot be empty")
			return
		}
		req.Name = &name
		params.Name = req.Name
		hasUpdate = true
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		req.Description = &desc
		params.Description = req.Description
		hasUpdate = true
	}
	if req.RuleType != nil {
		ruleType := strings.TrimSpace(*req.RuleType)
		if ruleType == "" {
			writeError(w, r, http.StatusBadRequest, "rule_type cannot be empty")
			return
		}
		req.RuleType = &ruleType
		params.RuleType = req.RuleType
		hasUpdate = true
	}
	if req.Enabled != nil {
		params.Enabled = req.Enabled
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
		writeError(w, r, http.StatusBadRequest, "no fields to update")
		return
	}

	updated, err := s.store.UpdatePolicy(r.Context(), policyID, params)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("update policy: %v", err))
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	resp := newPolicyResponse(*updated)
	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), principal, updated.TenantID, "policy.update", "policy", policyID.String(), map[string]any{
		"name": updated.Name,
	})
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request, policyID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	policy, err := s.store.GetPolicy(r.Context(), policyID)
	if err != nil {
		s.logger.Error("get policy for delete", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}
	if policy == nil {
		http.NotFound(w, r)
		return
	}

	if err := s.store.DeletePolicy(r.Context(), policyID); err != nil {
		s.logger.Error("delete policy", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}

	w.WriteHeader(http.StatusNoContent)
	s.recordAudit(r.Context(), principal, policy.TenantID, "policy.delete", "policy", policyID.String(), map[string]any{
		"name": policy.Name,
	})
}

func (s *Server) handleCreatePolicyVersion(w http.ResponseWriter, r *http.Request, policyID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	var req createPolicyVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid payload: %v", err))
		return
	}
	req.RuleDefinition = strings.TrimSpace(req.RuleDefinition)
	if req.RuleDefinition == "" {
		writeError(w, r, http.StatusBadRequest, "rule_definition is required")
		return
	}

	params := storage.CreatePolicyVersionParams{
		PolicyID:       policyID,
		RuleDefinition: req.RuleDefinition,
		Checksum:       req.Checksum,
		Metadata:       req.Metadata,
	}
	if params.Metadata == nil {
		params.Metadata = make(map[string]any)
	}
	if principal != nil && strings.TrimSpace(principal.Subject) != "" {
		if user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject); err == nil && user != nil {
			params.CreatedBy = &user.ID
		}
	}

	version, err := s.store.CreatePolicyVersion(r.Context(), params)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("create policy version failed: %v", err))
		return
	}

	resp := newPolicyVersionResponse(version)
	writeJSON(w, http.StatusCreated, resp)
}

func newPolicyResponse(p storage.Policy) policyResponse {
	resp := policyResponse{
		ID:        p.ID.String(),
		Name:      p.Name,
		RuleType:  p.RuleType,
		Enabled:   p.Enabled,
		Labels:    p.Labels,
		CreatedAt: formatTime(p.CreatedAt),
		UpdatedAt: formatTime(p.UpdatedAt),
	}
	if resp.Labels == nil {
		resp.Labels = map[string]string{}
	}
	if p.TenantID != uuid.Nil {
		tid := p.TenantID.String()
		resp.TenantID = &tid
	}
	if p.Description.Valid {
		desc := p.Description.String
		resp.Description = &desc
	}
	if p.ArchivedAt.Valid {
		resp.ArchivedAt = formatNullTime(p.ArchivedAt)
	}
	return resp
}

func newPolicyVersionResponse(v *storage.PolicyVersion) policyVersionResponse {
	resp := policyVersionResponse{
		ID:             v.ID.String(),
		Version:        v.Version,
		RuleDefinition: v.RuleDefinition,
		Metadata:       v.Metadata,
		CreatedAt:      formatTime(v.CreatedAt),
	}
	if v.Checksum.Valid {
		val := v.Checksum.String
		resp.Checksum = &val
	}
	if v.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	if v.CreatedBy != nil {
		id := v.CreatedBy.String()
		resp.CreatedBy = &id
	}
	if v.PromotedAt.Valid {
		resp.PromotedAt = formatNullTime(v.PromotedAt)
	}
	return resp
}


