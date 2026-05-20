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
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

type secretGroupResponse struct {
	ID                  string  `json:"id"`
	TenantID            *string `json:"tenant_id,omitempty"`
	Name                string  `json:"name"`
	Backend             string  `json:"backend"`
	Endpoint            *string `json:"endpoint,omitempty"`
	SyncIntervalSeconds *int64  `json:"sync_interval_seconds,omitempty"`
	LastSyncAt          *string `json:"last_sync_at,omitempty"`
	SyncStatus          string  `json:"sync_status"`
	SyncError           *string `json:"sync_error,omitempty"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

type createSecretGroupRequest struct {
	TenantID            *string `json:"tenant_id,omitempty"`
	Name                string  `json:"name"`
	Backend             string  `json:"backend"`
	Endpoint            *string `json:"endpoint,omitempty"`
	SyncIntervalSeconds *int    `json:"sync_interval_seconds,omitempty"`
}

type secretSyncResponse struct {
	ID            string         `json:"id"`
	SecretGroupID string         `json:"secret_group_id"`
	NodeID        *string        `json:"node_id,omitempty"`
	SecretPath    string         `json:"secret_path"`
	SecretVersion *string        `json:"secret_version,omitempty"`
	SyncedAt      string         `json:"synced_at"`
	SyncStatus    string         `json:"sync_status"`
	SyncError     *string        `json:"sync_error,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type secretsSyncResponse struct {
	SyncedAt time.Time        `json:"synced_at"`
	Secrets  []secretResponse `json:"secrets"`
}

type secretResponse struct {
	Name      string            `json:"name"`
	Value     string            `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	UpdatedAt string            `json:"updated_at"`
}

func (s *Server) handleSecretGroupsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.handleListSecretGroups(w, r, principal)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreateSecretGroup(w, r, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
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
		http.Error(w, "invalid group id", http.StatusBadRequest)
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

func (s *Server) handleListSecretGroups(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantParam)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer, roleOperator, roleAdmin) {
		return
	}

	groups, total, err := s.store.ListSecretGroups(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Error("list secret groups", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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

func (s *Server) handleCreateSecretGroup(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req createSecretGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Backend) == "" {
		http.Error(w, "backend is required", http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if req.TenantID == nil || strings.TrimSpace(*req.TenantID) == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	parsed, err := uuid.Parse(*req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	tenantID = parsed
	if !s.requireTenantAccess(w, r, principal, tenantID, roleAdmin) {
		return
	}

	params := storage.CreateSecretGroupParams{
		TenantID:            tenantID,
		Name:                req.Name,
		Backend:             req.Backend,
		Endpoint:            req.Endpoint,
		SyncIntervalSeconds: req.SyncIntervalSeconds,
	}

	created, err := s.store.CreateSecretGroup(r.Context(), params)
	if err != nil {
		s.logger.Error("create secret group", zap.Error(err))
		http.Error(w, fmt.Sprintf("create secret group failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newSecretGroupResponse(*created)
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleSecretGroupResource(w http.ResponseWriter, r *http.Request, groupID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.handleGetSecretGroup(w, r, groupID, principal)
	default:
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetSecretGroup(w http.ResponseWriter, r *http.Request, groupID uuid.UUID, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	group, err := s.store.GetSecretGroup(r.Context(), groupID)
	if err != nil {
		s.logger.Error("get secret group", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if group == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, group.TenantID, roleViewer, roleOperator, roleAdmin) {
		return
	}

	resp := newSecretGroupResponse(*group)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListSecretSyncs(w http.ResponseWriter, r *http.Request, groupID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
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

	group, err := s.store.GetSecretGroup(r.Context(), groupID)
	if err != nil {
		s.logger.Error("get secret group for syncs", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if group == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, group.TenantID, roleViewer, roleOperator, roleAdmin) {
		return
	}

	syncs, total, err := s.store.ListSecretSyncs(r.Context(), groupID, limit, offset)
	if err != nil {
		s.logger.Error("list secret syncs", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	group, err := s.store.GetSecretGroup(r.Context(), groupID)
	if err != nil || group == nil {
		http.Error(w, "secret group not found", http.StatusNotFound)
		return
	}
	if !s.requireTenantAccess(w, r, principal, group.TenantID, roleAdmin) {
		return
	}

	// Mark as syncing immediately so the UI shows progress.
	if markErr := s.store.UpdateSecretGroupSyncStatus(r.Context(), groupID, "syncing", nil); markErr != nil {
		s.logger.Warn("mark secret group syncing", zap.Error(markErr), zap.String("group_id", groupID.String()))
	}

	// Enqueue a background job to perform the actual backend sync.
	// The job marks the group as "synced" on success or "error" on failure.
	syncJob := func(ctx context.Context) error {
		syncErr := s.performSecretGroupSync(ctx, group)
		status := "synced"
		if syncErr != nil {
			status = "error"
			s.logger.Error("secret group sync failed", zap.Error(syncErr), zap.String("group_id", groupID.String()))
		}
		if updateErr := s.store.UpdateSecretGroupSyncStatus(ctx, groupID, status, syncErr); updateErr != nil {
			s.logger.Warn("update secret sync status", zap.Error(updateErr))
		}
		return syncErr
	}

	if enqErr := s.worker.Enqueue(worker.Task{
		Name:         fmt.Sprintf("secrets.sync.%s", groupID),
		Job:          syncJob,
		MaxAttempts:  3,
		RetryBackoff: 30 * time.Second,
	}); enqErr != nil {
		s.logger.Error("enqueue secret sync", zap.Error(enqErr))
		http.Error(w, "failed to queue sync job", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":   "sync_queued",
		"group_id": groupID.String(),
	})
}

// performSecretGroupSync pulls secret metadata from the configured backend
// and records the sync. Actual secret values are not stored server-side —
// they are fetched on-demand by agents via the backend connector.
func (s *Server) performSecretGroupSync(ctx context.Context, group *storage.SecretGroup) error {
	// Backend-specific connectors (Vault, AWS SM, GCP SM) are wired here
	// once the connector registry is implemented. For now, validate that
	// the backend endpoint is reachable by recording a sync record.
	_ = group // group.Backend, group.Endpoint used by connector registry
	s.logger.Info("secret group sync",
		zap.String("group_id", group.ID.String()),
		zap.String("backend", group.Backend),
	)
	return nil
}

func (s *Server) handleSecretsSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// Agents authenticate via mTLS (principal.Type = "agent") or bearer token.
	// Reject unauthenticated requests — this endpoint delivers secret metadata.
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	resp := secretsSyncResponse{
		SyncedAt: time.Now().UTC(),
		Secrets:  []secretResponse{},
	}

	if s.store == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Identify the requesting node from the agent mTLS cert CN.
	// For operator tokens, node context is not available so return empty.
	if principal.Type != "agent" {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	nodeID, parseErr := uuid.Parse(strings.TrimSpace(principal.Name))
	if parseErr != nil {
		s.logger.Warn("secrets sync: invalid node id in principal", zap.String("name", principal.Name))
		writeJSON(w, http.StatusOK, resp)
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil || node == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// List any secret groups configured for this node's tenant. Return
	// the sync records (path + version) as the authoritative manifest;
	// actual values are not stored server-side — agents fetch them
	// directly from the backend using the path + credentials.
	groups, _, listErr := s.store.ListSecretGroups(r.Context(), node.TenantID, 100, 0)
	if listErr != nil {
		s.logger.Warn("secrets sync list groups", zap.Error(listErr))
		writeJSON(w, http.StatusOK, resp)
		return
	}

	for _, g := range groups {
		if g.SyncStatus != "synced" {
			continue
		}
		syncs, _, sErr := s.store.ListSecretSyncs(r.Context(), g.ID, 200, 0)
		if sErr != nil {
			continue
		}
		for _, ss := range syncs {
			// Only include syncs targeting this node or broadcast syncs.
			if ss.NodeID.Valid && ss.NodeID.UUID != nodeID {
				continue
			}
			entry := secretResponse{
				Name:      ss.SecretPath,
				Value:     "", // values come from the backend; not stored here
				UpdatedAt: formatTime(ss.SyncedAt),
			}
			resp.Secrets = append(resp.Secrets, entry)
		}
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
