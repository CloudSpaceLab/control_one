package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type entitlementResponse struct {
	ID        string         `json:"id"`
	TenantID  *string        `json:"tenant_id,omitempty"`
	UserID    string         `json:"user_id"`
	NodeID    string         `json:"node_id"`
	GroupName *string        `json:"group_name,omitempty"`
	Role      string         `json:"role"`
	GrantedBy *string        `json:"granted_by,omitempty"`
	GrantedAt string         `json:"granted_at"`
	ExpiresAt *string        `json:"expires_at,omitempty"`
	RevokedAt *string        `json:"revoked_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

type createEntitlementRequest struct {
	TenantID  *string        `json:"tenant_id,omitempty"`
	UserID    string         `json:"user_id"`
	NodeID    string         `json:"node_id"`
	GroupName *string        `json:"group_name,omitempty"`
	Role      string         `json:"role"`
	ExpiresAt *string        `json:"expires_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type updateEntitlementRequest struct {
	Role      *string         `json:"role,omitempty"`
	ExpiresAt *string         `json:"expires_at,omitempty"`
	Metadata  *map[string]any `json:"metadata,omitempty"`
}

type accessSyncResponse struct {
	SyncedAt time.Time   `json:"synced_at"`
	Users    []userSync  `json:"users"`
	Groups   []groupSync `json:"groups"`
}

type userSync struct {
	ID     string   `json:"id"`
	Role   string   `json:"role"`
	Groups []string `json:"groups"`
}

type groupSync struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

func (s *Server) handleEntitlementsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListEntitlements(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateEntitlement(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEntitlementSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/access/entitlements/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	entitlementID, err := uuid.Parse(trimmed)
	if err != nil {
		http.Error(w, "invalid entitlement id", http.StatusBadRequest)
		return
	}

	s.handleEntitlementResource(w, r, entitlementID)
}

func (s *Server) handleListEntitlements(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filter := storage.EntitlementFilter{}

	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = parsed
	}

	if userParam := strings.TrimSpace(r.URL.Query().Get("user_id")); userParam != "" {
		parsed, err := uuid.Parse(userParam)
		if err != nil {
			http.Error(w, "invalid user_id", http.StatusBadRequest)
			return
		}
		filter.UserID = parsed
	}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}

	if roleParam := strings.TrimSpace(r.URL.Query().Get("role")); roleParam != "" {
		filter.Role = roleParam
	}

	if expiredParam := strings.TrimSpace(r.URL.Query().Get("expired")); expiredParam != "" {
		expired := parseBoolQuery(expiredParam)
		filter.Expired = &expired
	}

	entitlements, total, err := s.store.ListEntitlements(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list entitlements", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]entitlementResponse, 0, len(entitlements))
	for _, ent := range entitlements {
		respItems = append(respItems, newEntitlementResponse(ent))
	}

	resp := paginatedResponse[entitlementResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateEntitlement(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req createEntitlementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		http.Error(w, "invalid user_id", http.StatusBadRequest)
		return
	}

	nodeID, err := uuid.Parse(req.NodeID)
	if err != nil {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Role) == "" {
		http.Error(w, "role is required", http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if req.TenantID != nil {
		parsed, err := uuid.Parse(*req.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		ts, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			http.Error(w, "invalid expires_at timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		expiresAt = &ts
	}

	var grantedBy *uuid.UUID
	if principal.Subject != "" {
		if id, err := uuid.Parse(principal.Subject); err == nil {
			grantedBy = &id
		}
	}

	params := storage.CreateEntitlementParams{
		TenantID:  tenantID,
		UserID:    userID,
		NodeID:    nodeID,
		GroupName: req.GroupName,
		Role:      req.Role,
		GrantedBy: grantedBy,
		ExpiresAt: expiresAt,
		Metadata:  req.Metadata,
	}
	if params.Metadata == nil {
		params.Metadata = make(map[string]any)
	}

	created, err := s.store.CreateEntitlement(r.Context(), params)
	if err != nil {
		s.logger.Error("create entitlement", zap.Error(err))
		http.Error(w, fmt.Sprintf("create entitlement failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newEntitlementResponse(*created)
	writeJSON(w, http.StatusCreated, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "entitlement.created", "entitlement", created.ID.String(), map[string]any{
		"user_id": userID.String(),
		"node_id": nodeID.String(),
		"role":    req.Role,
	})
}

func (s *Server) handleEntitlementResource(w http.ResponseWriter, r *http.Request, entitlementID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetEntitlement(w, r, entitlementID)
	case http.MethodPatch:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleUpdateEntitlement(w, r, entitlementID)
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleDeleteEntitlement(w, r, entitlementID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch, http.MethodDelete}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetEntitlement(w http.ResponseWriter, r *http.Request, entitlementID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	entitlement, err := s.store.GetEntitlement(r.Context(), entitlementID)
	if err != nil {
		s.logger.Error("get entitlement", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if entitlement == nil {
		http.NotFound(w, r)
		return
	}

	resp := newEntitlementResponse(*entitlement)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdateEntitlement(w http.ResponseWriter, r *http.Request, entitlementID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req updateEntitlementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	params := storage.UpdateEntitlementParams{
		Role:     req.Role,
		Metadata: req.Metadata,
	}

	if req.ExpiresAt != nil {
		ts, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			http.Error(w, "invalid expires_at timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		params.ExpiresAt = &ts
	}

	updated, err := s.store.UpdateEntitlement(r.Context(), entitlementID, params)
	if err != nil {
		s.logger.Error("update entitlement", zap.Error(err))
		http.Error(w, fmt.Sprintf("update entitlement failed: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	resp := newEntitlementResponse(*updated)
	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "entitlement.updated", "entitlement", entitlementID.String(), map[string]any{})
}

func (s *Server) handleDeleteEntitlement(w http.ResponseWriter, r *http.Request, entitlementID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	if err := s.store.DeleteEntitlement(r.Context(), entitlementID); err != nil {
		s.logger.Error("delete entitlement", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)

	s.recordAudit(r.Context(), principal, uuid.Nil, "entitlement.deleted", "entitlement", entitlementID.String(), map[string]any{})
}

func (s *Server) handleAccessSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Provider    string `json:"provider"`
		DefaultRole string `json:"default_role"`
		NodeID      string `json:"node_id"`
		APIEndpoint string `json:"api_endpoint,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	resp := accessSyncResponse{
		SyncedAt: time.Now().UTC(),
		Users:    []userSync{},
		Groups:   []groupSync{},
	}

	if s.store == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	filter := storage.EntitlementFilter{}
	entitlements, _, err := s.store.ListEntitlements(ctx, filter, 0, 0)
	if err != nil {
		s.logger.Warn("list entitlements for sync", zap.Error(err))
		writeJSON(w, http.StatusOK, resp)
		return
	}

	userMap := make(map[string]*userSync)
	for _, ent := range entitlements {
		userID := ent.UserID.String()
		if user, exists := userMap[userID]; exists {
			user.Groups = append(user.Groups, ent.NodeID.String())
		} else {
			userMap[userID] = &userSync{
				ID:     userID,
				Role:   ent.Role,
				Groups: []string{ent.NodeID.String()},
			}
		}
	}

	resp.Users = make([]userSync, 0, len(userMap))
	for _, user := range userMap {
		resp.Users = append(resp.Users, *user)
	}

	writeJSON(w, http.StatusOK, resp)
}

func newEntitlementResponse(ent storage.AccessEntitlement) entitlementResponse {
	resp := entitlementResponse{
		ID:        ent.ID.String(),
		UserID:    ent.UserID.String(),
		NodeID:    ent.NodeID.String(),
		Role:      ent.Role,
		GrantedAt: formatTime(ent.GrantedAt),
		Metadata:  ent.Metadata,
		CreatedAt: formatTime(ent.CreatedAt),
		UpdatedAt: formatTime(ent.UpdatedAt),
	}
	if ent.TenantID != uuid.Nil {
		tid := ent.TenantID.String()
		resp.TenantID = &tid
	}
	if ent.GroupName.Valid {
		groupName := ent.GroupName.String
		resp.GroupName = &groupName
	}
	if ent.GrantedBy.Valid {
		grantedBy := ent.GrantedBy.UUID.String()
		resp.GrantedBy = &grantedBy
	}
	if ent.ExpiresAt.Valid {
		expiresAt := formatTime(ent.ExpiresAt.Time)
		resp.ExpiresAt = &expiresAt
	}
	if ent.RevokedAt.Valid {
		revokedAt := formatTime(ent.RevokedAt.Time)
		resp.RevokedAt = &revokedAt
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	return resp
}
