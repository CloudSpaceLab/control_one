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

type secretGroupResponse struct {
	ID                 string  `json:"id"`
	TenantID           *string `json:"tenant_id,omitempty"`
	Name               string  `json:"name"`
	Backend            string  `json:"backend"`
	Endpoint           *string `json:"endpoint,omitempty"`
	SyncIntervalSeconds *int64  `json:"sync_interval_seconds,omitempty"`
	LastSyncAt         *string `json:"last_sync_at,omitempty"`
	SyncStatus         string  `json:"sync_status"`
	SyncError          *string `json:"sync_error,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type createSecretGroupRequest struct {
	TenantID           *string `json:"tenant_id,omitempty"`
	Name               string  `json:"name"`
	Backend            string  `json:"backend"`
	Endpoint           *string `json:"endpoint,omitempty"`
	SyncIntervalSeconds *int   `json:"sync_interval_seconds,omitempty"`
}

type secretSyncResponse struct {
	ID            string  `json:"id"`
	SecretGroupID string  `json:"secret_group_id"`
	NodeID        *string `json:"node_id,omitempty"`
	SecretPath    string  `json:"secret_path"`
	SecretVersion *string `json:"secret_version,omitempty"`
	SyncedAt      string  `json:"synced_at"`
	SyncStatus    string  `json:"sync_status"`
	SyncError     *string `json:"sync_error,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type secretsSyncRequest struct {
	Backend  string   `json:"backend"`
	Groups   []string `json:"groups,omitempty"`
	NodeID   string   `json:"node_id,omitempty"`
	Endpoint string   `json:"endpoint,omitempty"`
}

type secretsSyncResponse struct {
	SyncedAt time.Time        `json:"synced_at"`
	Secrets  []secretResponse  `json:"secrets"`
}

type secretResponse struct {
	Name      string            `json:"name"`
	Value     string            `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	UpdatedAt string           `json:"updated_at"`
}

func (s *Server) handleSecretGroupsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListSecretGroups(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateSecretGroup(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handleSecretGroupSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/secrets/groups/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	segments := strings.Split(trimmed, "/")
	groupID, err := uuid.Parse(segments[0])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid group id")
		return
	}

	if len(segments) == 1 {
		s.handleSecretGroupResource(w, r, groupID)
		return
	}

	if len(segments) == 2 && segments[1] == "syncs" {
		s.handleListSecretSyncs(w, r, groupID)
		return
	}

	if len(segments) == 2 && segments[1] == "sync" {
		s.handleSyncSecretGroup(w, r, groupID)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleListSecretGroups(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	var tenantID uuid.UUID
	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid tenant_id")
			return
		}
		tenantID = parsed
	}

	groups, total, err := s.store.ListSecretGroups(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Error("list secret groups", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}

	respItems := make([]secretGroupResponse, 0, len(groups))
	for _, group := range groups {
		respItems = append(respItems, newSecretGroupResponse(group))
	}

	resp := paginatedResponse[secretGroupResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateSecretGroup(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	var req createSecretGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid payload: %v", err))
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeError(w, r, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Backend) == "" {
		writeError(w, r, http.StatusBadRequest, "backend is required")
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

	params := storage.CreateSecretGroupParams{
		TenantID:           tenantID,
		Name:               req.Name,
		Backend:            req.Backend,
		Endpoint:           req.Endpoint,
		SyncIntervalSeconds: req.SyncIntervalSeconds,
	}

	created, err := s.store.CreateSecretGroup(r.Context(), params)
	if err != nil {
		s.logger.Error("create secret group", zap.Error(err))
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("create secret group failed: %v", err))
		return
	}

	resp := newSecretGroupResponse(*created)
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleSecretGroupResource(w http.ResponseWriter, r *http.Request, groupID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetSecretGroup(w, r, groupID)
	default:
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handleGetSecretGroup(w http.ResponseWriter, r *http.Request, groupID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	group, err := s.store.GetSecretGroup(r.Context(), groupID)
	if err != nil {
		s.logger.Error("get secret group", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}
	if group == nil {
		http.NotFound(w, r)
		return
	}

	resp := newSecretGroupResponse(*group)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListSecretSyncs(w http.ResponseWriter, r *http.Request, groupID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

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

	syncs, total, err := s.store.ListSecretSyncs(r.Context(), groupID, limit, offset)
	if err != nil {
		s.logger.Error("list secret syncs", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}

	respItems := make([]secretSyncResponse, 0, len(syncs))
	for _, sync := range syncs {
		respItems = append(respItems, newSecretSyncResponse(sync))
	}

	resp := paginatedResponse[secretSyncResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSyncSecretGroup(w http.ResponseWriter, r *http.Request, groupID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	// TODO: Trigger sync via sync service
	// For now, just return success
	writeJSON(w, http.StatusOK, map[string]string{"status": "sync_triggered", "group_id": groupID.String()})
}

func (s *Server) handleSecretsSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	// This endpoint is called by node agents to sync secrets
	// For now, return empty secrets list
	// TODO: Implement actual secret retrieval and distribution
	resp := secretsSyncResponse{
		SyncedAt: time.Now().UTC(),
		Secrets:  []secretResponse{},
	}
	writeJSON(w, http.StatusOK, resp)
}

func newSecretGroupResponse(group storage.SecretGroup) secretGroupResponse {
	resp := secretGroupResponse{
		ID:         group.ID.String(),
		Name:       group.Name,
		Backend:    group.Backend,
		SyncStatus: group.SyncStatus,
		CreatedAt:  formatTime(group.CreatedAt),
		UpdatedAt:  formatTime(group.UpdatedAt),
	}
	if group.TenantID != uuid.Nil {
		tid := group.TenantID.String()
		resp.TenantID = &tid
	}
	if group.Endpoint.Valid {
		endpoint := group.Endpoint.String
		resp.Endpoint = &endpoint
	}
	if group.SyncIntervalSeconds.Valid {
		interval := group.SyncIntervalSeconds.Int64
		resp.SyncIntervalSeconds = &interval
	}
	if group.LastSyncAt.Valid {
		lastSync := formatTime(group.LastSyncAt.Time)
		resp.LastSyncAt = &lastSync
	}
	if group.SyncError.Valid {
		errMsg := group.SyncError.String
		resp.SyncError = &errMsg
	}
	return resp
}

func newSecretSyncResponse(sync storage.SecretSync) secretSyncResponse {
	resp := secretSyncResponse{
		ID:            sync.ID.String(),
		SecretGroupID: sync.SecretGroupID.String(),
		SecretPath:    sync.SecretPath,
		SyncedAt:      formatTime(sync.SyncedAt),
		SyncStatus:    sync.SyncStatus,
		Metadata:      sync.Metadata,
	}
	if sync.NodeID.Valid {
		nid := sync.NodeID.UUID.String()
		resp.NodeID = &nid
	}
	if sync.SecretVersion.Valid {
		version := sync.SecretVersion.String
		resp.SecretVersion = &version
	}
	if sync.SyncError.Valid {
		errMsg := sync.SyncError.String
		resp.SyncError = &errMsg
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	return resp
}

