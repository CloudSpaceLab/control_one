package server

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type agentUpdateRequest struct {
	TargetVersion string `json:"target_version"` // empty → latest
}

type agentUpdateResponse struct {
	NodeID  string `json:"node_id"`
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

// handleNodeAgentUpdate queues an agent.update job for a specific node.
// On the next heartbeat from that node the server embeds "agent_update" in
// pending_actions, signalling the agent to kick off a self-update goroutine.
func (s *Server) handleNodeAgentUpdate(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for agent update", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	var req agentUpdateRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
	}

	payload, err := json.Marshal(map[string]string{
		"node_id":        nodeID.String(),
		"target_version": req.TargetVersion,
	})
	if err != nil {
		http.Error(w, "marshal payload", http.StatusInternalServerError)
		return
	}

	job, err := s.store.CreateJob(r.Context(), &storage.Job{
		TenantID: node.TenantID,
		Type:     JobTypeAgentUpdate,
		Status:   storage.JobStatusQueued,
		Payload:  payload,
	}, nil)
	if err != nil {
		s.logger.Error("create agent update job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.recordAudit(r.Context(), principal, node.TenantID, "node.agent_update.queued", "node", nodeID.String(), map[string]any{
		"hostname":       node.Hostname,
		"target_version": req.TargetVersion,
		"job_id":         job.ID.String(),
	})

	writeJSON(w, http.StatusAccepted, agentUpdateResponse{
		NodeID:  nodeID.String(),
		JobID:   job.ID.String(),
		Message: "agent update queued; will apply on next heartbeat",
	})
}
