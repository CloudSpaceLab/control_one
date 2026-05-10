package server

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// nodePackageResponse is the wire representation of a single node_packages row.
// Field shapes mirror the underlying storage type — installed_at and arch are
// optional in the schema, so they round-trip as omitempty pointers.
type nodePackageResponse struct {
	NodeID      string  `json:"node_id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Source      string  `json:"source"`
	Arch        *string `json:"arch,omitempty"`
	InstalledAt *string `json:"installed_at,omitempty"`
}

func newNodePackageResponse(p storage.NodePackage) nodePackageResponse {
	out := nodePackageResponse{
		NodeID:  p.NodeID.String(),
		Name:    p.Name,
		Version: p.Version,
		Source:  p.Source,
		Arch:    p.Arch,
	}
	if p.InstalledAt != nil {
		s := p.InstalledAt.UTC().Format(time.RFC3339)
		out.InstalledAt = &s
	}
	return out
}

// handleNodePackages implements GET /api/v1/nodes/:id/packages — surfaces the
// `node_packages` inventory row set for the operator UI Packages tab. The
// table is populated by the heartbeat ingest path; this is a read-only view.
//
// Routed from handleNodeResource, mirroring the handleNodeServices pattern.
// Auth: viewer-or-better. Returns 404 when the node id is unknown so a stale
// UI link doesn't masquerade as an empty inventory.
func (s *Server) handleNodePackages(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for packages", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	pkgs, err := s.store.ListNodePackages(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("list node packages", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]nodePackageResponse, 0, len(pkgs))
	for _, p := range pkgs {
		out = append(out, newNodePackageResponse(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}
