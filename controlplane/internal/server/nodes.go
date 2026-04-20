package server

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// handleRetireNode marks a node retired in response to a successful agent-side
// uninstall. The endpoint is mTLS-authenticated (agent identity) or admin/operator
// (operator-initiated retirement). We accept both because uninstall can be driven
// from either side: an operator running the uninstall one-liner, or the agent itself
// calling back with its own cert before shutting down.
func (s *Server) handleRetireNode(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for retire", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	if err := s.store.RetireNode(r.Context(), nodeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("retire node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"node_id": nodeID.String(),
		"state":   "retired",
	})

	s.recordAudit(r.Context(), principal, node.TenantID, "node.retired", "node", nodeID.String(), map[string]any{
		"hostname": node.Hostname,
	})
}
