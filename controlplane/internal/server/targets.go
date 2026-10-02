package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Optional interface keeps existing node-specific stores and enrollment APIs
// compatible while target identity is introduced alongside them.
type targetIdentityStore interface {
	ListTargets(context.Context, storage.TargetFilter, storage.TargetAccess) ([]storage.Target, int, error)
	GetTarget(context.Context, uuid.UUID, storage.TargetAccess) (*storage.Target, error)
	CreateNetworkTarget(context.Context, storage.NetworkTargetParams, storage.TargetAccess) (*storage.Target, error)
}

func (s *Server) targetAccess(w http.ResponseWriter, r *http.Request, permission string) (storage.TargetAccess, bool) {
	roles := []string{roleViewer}
	if permission == "targets.write" {
		roles = []string{roleOperator, roleAdmin}
	}
	principal, ok := s.authorizePermission(w, r, permission, roles...)
	if !ok {
		return storage.TargetAccess{}, false
	}
	// No agent principal or unresolvable identity may perform estate-wide reads.
	if principal.Type != "user" {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return storage.TargetAccess{}, false
	}
	userID := principalStorageUserID(s, r.Context(), principal)
	if userID == uuid.Nil {
		http.Error(w, "target access requires a persisted user", http.StatusForbidden)
		return storage.TargetAccess{}, false
	}
	return storage.TargetAccess{UserID: userID, Permission: permission}, true
}

func (s *Server) handleTargetsCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	store, ok := s.store.(targetIdentityStore)
	if !ok {
		http.Error(w, "target store unavailable", http.StatusServiceUnavailable)
		return
	}
	permission := "targets.read"
	if r.Method == http.MethodPost {
		permission = "targets.write"
	}
	access, ok := s.targetAccess(w, r, permission)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		var p storage.NetworkTargetParams
		if err := decodeStrictJSONDocument(r, &p, 16*1024); err != nil {
			http.Error(w, "invalid target payload", http.StatusBadRequest)
			return
		}
		if err := p.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		target, err := store.CreateNetworkTarget(r.Context(), p, access)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if err != nil {
			s.logger.Error("create network target", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		principal, _ := s.authorize(w, r)
		s.recordAudit(r.Context(), principal, target.TenantID, "target.create", "target", target.ID.String(), map[string]any{"family": target.Family, "type": target.Type})
		w.Header().Set("Location", "/api/v1/targets/"+target.ID.String())
		writeJSON(w, http.StatusCreated, target)
		return
	}
	query := r.URL.Query()
	limit, offset, err := parseLimitOffset(query)
	if err != nil || limit < 1 || limit > 500 {
		http.Error(w, "limit must be 1 to 500 and offset non-negative", http.StatusBadRequest)
		return
	}
	var tenantID uuid.UUID
	if raw := strings.TrimSpace(query.Get("tenant_id")); raw != "" {
		tenantID, err = uuid.Parse(raw)
		if err != nil || tenantID == uuid.Nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
	}
	f := storage.TargetFilter{
		TenantID: tenantID, Limit: limit, Offset: offset,
		Family: strings.TrimSpace(query.Get("family")), Type: strings.TrimSpace(query.Get("type")),
		Site: strings.TrimSpace(query.Get("site")), Group: strings.TrimSpace(query.Get("group")),
		Vendor: strings.TrimSpace(query.Get("vendor")), Model: strings.TrimSpace(query.Get("model")),
		Platform: strings.TrimSpace(query.Get("platform")), Firmware: strings.TrimSpace(query.Get("firmware")),
		ReachabilityState: strings.TrimSpace(query.Get("reachability_state")), CollectionState: strings.TrimSpace(query.Get("collection_state")),
		LifecycleState: strings.TrimSpace(query.Get("lifecycle_state")), Search: strings.TrimSpace(query.Get("search")),
	}
	targets, total, err := store.ListTargets(r.Context(), f, access)
	if err != nil {
		s.logger.Error("list targets", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if targets == nil {
		targets = []storage.Target{}
	}
	writeJSON(w, http.StatusOK, paginatedResponse[storage.Target]{Data: targets, Pagination: newPaginationMeta(total, limit, offset, len(targets))})
}

func (s *Server) handleTargetResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	store, ok := s.store.(targetIdentityStore)
	if !ok {
		http.Error(w, "target store unavailable", http.StatusServiceUnavailable)
		return
	}
	access, ok := s.targetAccess(w, r, "targets.read")
	if !ok {
		return
	}
	id, err := uuid.Parse(strings.TrimPrefix(r.URL.Path, "/api/v1/targets/"))
	if err != nil || id == uuid.Nil {
		http.Error(w, "invalid target id", http.StatusBadRequest)
		return
	}
	target, err := store.GetTarget(r.Context(), id, access)
	if err != nil {
		s.logger.Error("get target", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	// Inaccessible IDs look identical to nonexistent IDs.
	if target == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, target)
}
