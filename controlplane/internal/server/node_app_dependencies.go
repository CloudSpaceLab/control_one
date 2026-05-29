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

type nodeAppDependencyStore interface {
	GetNode(context.Context, uuid.UUID) (*storage.Node, error)
	ReplaceNodeAppDependencies(context.Context, uuid.UUID, uuid.UUID, []storage.NodeAppDependency) error
	ListNodeAppDependencies(context.Context, uuid.UUID) ([]storage.NodeAppDependency, error)
}

type nodeAppDependenciesRequest struct {
	Dependencies []nodeAppDependencyItem `json:"dependencies"`
}

type nodeAppDependencyItem struct {
	AppRoot        string         `json:"app_root,omitempty"`
	Ecosystem      string         `json:"ecosystem"`
	Name           string         `json:"name"`
	Version        string         `json:"version,omitempty"`
	PackageManager string         `json:"package_manager,omitempty"`
	ManifestPath   string         `json:"manifest_path,omitempty"`
	Scope          string         `json:"scope,omitempty"`
	License        string         `json:"license,omitempty"`
	PURL           string         `json:"purl,omitempty"`
	CPE            string         `json:"cpe,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type nodeAppDependencyResponse struct {
	ID             string         `json:"id"`
	NodeID         string         `json:"node_id"`
	TenantID       string         `json:"tenant_id"`
	AppRoot        string         `json:"app_root,omitempty"`
	Ecosystem      string         `json:"ecosystem"`
	Name           string         `json:"name"`
	Version        string         `json:"version,omitempty"`
	PackageManager string         `json:"package_manager,omitempty"`
	ManifestPath   string         `json:"manifest_path,omitempty"`
	Scope          string         `json:"scope,omitempty"`
	License        string         `json:"license,omitempty"`
	PURL           string         `json:"purl,omitempty"`
	CPE            string         `json:"cpe,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	ObservedAt     string         `json:"observed_at"`
}

func (s *Server) handleNodeAppDependencies(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	store, ok := s.store.(nodeAppDependencyStore)
	if !ok {
		http.Error(w, "app dependency store unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleNodeAppDependenciesIngest(w, r, nodeID, store)
	case http.MethodGet:
		s.handleNodeAppDependenciesList(w, r, nodeID, store)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNodeAppDependenciesIngest(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID, store nodeAppDependencyStore) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if principal.Type != "agent" {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	cn := strings.TrimSpace(principal.Name)
	if cn == "" || !strings.EqualFold(cn, nodeID.String()) {
		http.Error(w, "client cert CN does not match node id", http.StatusForbidden)
		return
	}

	var body nodeAppDependenciesRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}
	node, err := store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for app dependency ingest", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	deps := make([]storage.NodeAppDependency, 0, len(body.Dependencies))
	for _, dep := range body.Dependencies {
		if strings.TrimSpace(dep.Name) == "" || strings.TrimSpace(dep.Ecosystem) == "" {
			continue
		}
		deps = append(deps, storage.NodeAppDependency{
			NodeID:         nodeID,
			TenantID:       node.TenantID,
			AppRoot:        dep.AppRoot,
			Ecosystem:      dep.Ecosystem,
			Name:           dep.Name,
			Version:        dep.Version,
			PackageManager: dep.PackageManager,
			ManifestPath:   dep.ManifestPath,
			Scope:          dep.Scope,
			License:        dep.License,
			PURL:           dep.PURL,
			CPE:            dep.CPE,
			Metadata:       dep.Metadata,
		})
	}
	if err := store.ReplaceNodeAppDependencies(r.Context(), nodeID, node.TenantID, deps); err != nil {
		s.logger.Error("replace node app dependencies", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.refreshOfflineVulnerabilityFindingsForNode(r.Context(), node, time.Now().UTC(), "app_dependency_inventory_sync")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "dependencies": len(deps)})
}

func (s *Server) handleNodeAppDependenciesList(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID, store nodeAppDependencyStore) {
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	node, err := store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for app dependencies", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}
	deps, err := store.ListNodeAppDependencies(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("list node app dependencies", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]nodeAppDependencyResponse, 0, len(deps))
	for _, dep := range deps {
		out = append(out, newNodeAppDependencyResponse(dep))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func newNodeAppDependencyResponse(dep storage.NodeAppDependency) nodeAppDependencyResponse {
	observedAt := ""
	if !dep.ObservedAt.IsZero() {
		observedAt = dep.ObservedAt.UTC().Format(time.RFC3339)
	}
	return nodeAppDependencyResponse{
		ID:             dep.ID.String(),
		NodeID:         dep.NodeID.String(),
		TenantID:       dep.TenantID.String(),
		AppRoot:        dep.AppRoot,
		Ecosystem:      dep.Ecosystem,
		Name:           dep.Name,
		Version:        dep.Version,
		PackageManager: dep.PackageManager,
		ManifestPath:   dep.ManifestPath,
		Scope:          dep.Scope,
		License:        dep.License,
		PURL:           dep.PURL,
		CPE:            dep.CPE,
		Metadata:       dep.Metadata,
		ObservedAt:     observedAt,
	}
}
