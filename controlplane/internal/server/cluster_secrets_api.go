package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ─── Wire types ─────────────────────────────────────────────────────

type clusterSecretMetaResponse struct {
	ID        string `json:"id"`
	ClusterID string `json:"cluster_id"`
	Key       string `json:"key"`
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type clusterSecretValueResponse struct {
	clusterSecretMetaResponse
	Value string `json:"value"`
}

type clusterSecretUpsertRequest struct {
	Value string `json:"value"`
}

type clusterSecretListResponse struct {
	Data []clusterSecretMetaResponse `json:"data"`
}

// ─── Routing ────────────────────────────────────────────────────────

// handleClusterSecretsRoute is dispatched from handleClusterSubroutes when
// the URL path is `/api/v1/clusters/<id>/secrets` or `/api/v1/clusters/<id>/secrets/<key>`.
// `tail` is the path fragment after `secrets` (possibly empty).
func (s *Server) handleClusterSecretsRoute(w http.ResponseWriter, r *http.Request, clusterID uuid.UUID, tail []string) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	// Every secret route requires the cluster to exist + be tenant-visible.
	// We do a lightweight existence check up-front so 404s are consistent
	// across GET/PUT/DELETE.
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster for secret route", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}

	switch len(tail) {
	case 0:
		// /api/v1/clusters/:id/secrets
		s.handleClusterSecretsCollection(w, r, cluster)
	case 1:
		// /api/v1/clusters/:id/secrets/:key
		key := strings.TrimSpace(tail[0])
		if key == "" {
			http.NotFound(w, r)
			return
		}
		s.handleClusterSecretResource(w, r, cluster, key)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleClusterSecretsCollection(w http.ResponseWriter, r *http.Request, cluster *storage.Cluster) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListClusterSecrets(w, r, cluster)
	default:
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleClusterSecretResource(w http.ResponseWriter, r *http.Request, cluster *storage.Cluster, key string) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetClusterSecret(w, r, cluster, key)
	case http.MethodPut:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleUpsertClusterSecret(w, r, cluster, key, principal)
	case http.MethodDelete:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleDeleteClusterSecret(w, r, cluster, key, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPut, http.MethodDelete}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ─── Handlers ───────────────────────────────────────────────────────

func (s *Server) handleListClusterSecrets(w http.ResponseWriter, r *http.Request, cluster *storage.Cluster) {
	secrets, err := s.store.ListClusterSecrets(r.Context(), cluster.ID)
	if err != nil {
		s.logger.Error("list cluster secrets", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := clusterSecretListResponse{
		Data: make([]clusterSecretMetaResponse, 0, len(secrets)),
	}
	for _, sec := range secrets {
		resp.Data = append(resp.Data, newClusterSecretMetaResponse(sec))
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetClusterSecret returns a single secret WITH the decrypted value so
// operators can reveal a secret on demand. The equivalent "list" endpoint
// keeps values blank — per-node secret read semantics (the existing
// secrets.go pattern) allow admins to fetch values explicitly when they
// request a specific key, but the collection endpoint stays metadata-only.
func (s *Server) handleGetClusterSecret(w http.ResponseWriter, r *http.Request, cluster *storage.Cluster, key string) {
	secret, err := s.store.GetClusterSecretDecrypted(r.Context(), cluster.ID, key)
	if err != nil {
		s.logger.Error("get cluster secret", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if secret == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, newClusterSecretValueResponse(*secret))
}

func (s *Server) handleUpsertClusterSecret(w http.ResponseWriter, r *http.Request, cluster *storage.Cluster, key string, principal *auth.Principal) {
	var req clusterSecretUpsertRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	// Value is allowed to be empty string (explicit empty is a valid secret).
	// The only thing we reject is an unreadable request body — already caught
	// above.

	secret, err := s.store.UpsertClusterSecret(r.Context(), storage.UpsertClusterSecretParams{
		ClusterID: cluster.ID,
		Key:       key,
		Value:     req.Value,
	})
	if err != nil {
		s.logger.Error("upsert cluster secret", zap.Error(err))
		http.Error(w, fmt.Sprintf("upsert cluster secret failed: %v", err), http.StatusBadRequest)
		return
	}

	// Kick off fan-out to every current cluster member.
	if _, enqueueErr := s.enqueueClusterSecretFanOut(r, cluster, ClusterSecretFanOutActionUpsert, key); enqueueErr != nil {
		// Log but don't fail the write — the secret is persisted; a
		// subsequent PUT or the cluster-member-join hook will reconcile.
		s.logger.Warn("enqueue cluster secret fan-out",
			zap.String("cluster_id", cluster.ID.String()),
			zap.String("key", key),
			zap.Error(enqueueErr),
		)
	}

	s.recordAudit(r.Context(), principal, cluster.TenantID, "cluster.secret.upserted", "cluster_secret", secret.ID.String(), map[string]any{
		"cluster_id": cluster.ID.String(),
		"key":        key,
		"version":    secret.Version,
	})

	writeJSON(w, http.StatusOK, newClusterSecretMetaResponse(*secret))
}

func (s *Server) handleDeleteClusterSecret(w http.ResponseWriter, r *http.Request, cluster *storage.Cluster, key string, principal *auth.Principal) {
	if err := s.store.DeleteClusterSecret(r.Context(), cluster.ID, key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("delete cluster secret", zap.Error(err))
		http.Error(w, fmt.Sprintf("delete cluster secret failed: %v", err), http.StatusBadRequest)
		return
	}

	if _, enqueueErr := s.enqueueClusterSecretFanOut(r, cluster, ClusterSecretFanOutActionDelete, key); enqueueErr != nil {
		s.logger.Warn("enqueue cluster secret delete fan-out",
			zap.String("cluster_id", cluster.ID.String()),
			zap.String("key", key),
			zap.Error(enqueueErr),
		)
	}

	s.recordAudit(r.Context(), principal, cluster.TenantID, "cluster.secret.deleted", "cluster_secret", "", map[string]any{
		"cluster_id": cluster.ID.String(),
		"key":        key,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ─── Response mappers ───────────────────────────────────────────────

func newClusterSecretMetaResponse(sec storage.ClusterSecret) clusterSecretMetaResponse {
	return clusterSecretMetaResponse{
		ID:        sec.ID.String(),
		ClusterID: sec.ClusterID.String(),
		Key:       sec.Key,
		Version:   sec.Version,
		CreatedAt: formatTime(sec.CreatedAt),
		UpdatedAt: formatTime(sec.UpdatedAt),
	}
}

func newClusterSecretValueResponse(sec storage.ClusterSecret) clusterSecretValueResponse {
	return clusterSecretValueResponse{
		clusterSecretMetaResponse: newClusterSecretMetaResponse(sec),
		Value:                     sec.Value,
	}
}
