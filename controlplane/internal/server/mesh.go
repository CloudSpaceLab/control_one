package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
)

type meshPeerResponse struct {
	ID         string    `json:"id"`
	PublicKey  string    `json:"public_key"`
	Endpoint   string    `json:"endpoint"`
	AllowedIPs []string  `json:"allowed_ips"`
	LastSeen   time.Time `json:"last_seen"`
}

type meshRotateRequest struct {
	NodeID    string `json:"node_id"`
	Namespace string `json:"namespace"`
}

func (s *Server) handleMeshPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	tenantID, nodeID, ok := s.requireMeshAgent(w, r)
	if !ok {
		return
	}
	if requested := strings.TrimSpace(r.URL.Query().Get("node_id")); requested != "" {
		parsed, err := uuid.Parse(requested)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		if parsed != nodeID {
			http.Error(w, "agent cannot sync mesh peers for another node", http.StatusForbidden)
			return
		}
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if namespace == "" {
		namespace = "default"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":  tenantID.String(),
		"node_id":    nodeID.String(),
		"namespace":  namespace,
		"peers":      []meshPeerResponse{},
		"updated_at": time.Now().UTC(),
	})
}

func (s *Server) handleMeshRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	tenantID, nodeID, ok := s.requireMeshAgent(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer func() { _ = r.Body.Close() }()
	var req meshRotateRequest
	if r.ContentLength != 0 {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}
	if strings.TrimSpace(req.NodeID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(req.NodeID))
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		if parsed != nodeID {
			http.Error(w, "agent cannot rotate mesh key for another node", http.StatusForbidden)
			return
		}
	}
	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":     tenantID.String(),
		"node_id":       nodeID.String(),
		"namespace":     namespace,
		"private_key":   "",
		"rotated_at":    time.Now().UTC(),
		"coordinator":   "controlplane-compat",
		"rotation_noop": true,
	})
}

func (s *Server) requireMeshAgent(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil || principal.Type != "agent" {
		http.Error(w, "agent principal required", http.StatusForbidden)
		return uuid.Nil, uuid.Nil, false
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return uuid.Nil, uuid.Nil, false
	}
	tenantID, nodeID, err := s.tenantNodeForAgent(r.Context(), principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, nodeID, true
}
