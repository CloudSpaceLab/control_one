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

// agentRolloutRequest is the body for PUT /api/v1/agent-rollout (PR 4a). It
// scopes a per-tenant staged rollout for the agent self-update path.
//
// Practical pattern: bump target_release_seq + target_version when a new
// release is signed; ramp rollout_pct in waves (5 → 25 → 100). paused=true
// is the emergency brake — agents see paused and refuse to self-update
// regardless of bucket math.
type agentRolloutRequest struct {
	TenantID         string `json:"tenant_id"`
	TargetReleaseSeq int    `json:"target_release_seq"`
	TargetVersion    string `json:"target_version"`
	RolloutPct       int    `json:"rollout_pct"`
	Paused           bool   `json:"paused"`
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
	if !s.requireTenantAccess(w, r, principal, node.TenantID, roleOperator, roleAdmin) {
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

// handleAgentRollout serves PUT and GET on /api/v1/agent-rollout.
//   - GET ?tenant_id=…  returns the current rollout state (or zero state).
//   - PUT writes a new rollout state; admin-only.
func (s *Server) handleAgentRollout(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAgentRolloutGet(w, r)
	case http.MethodPut:
		s.handleAgentRolloutPut(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAgentRolloutGet(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	tid, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tid, roleViewer, roleOperator, roleAdmin) {
		return
	}
	state, err := s.store.GetAgentRolloutState(r.Context(), tid)
	if err != nil {
		s.logger.Error("get agent rollout state", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if state == nil {
		// No row → return zeroed defaults. Empty rollout means no fleet
		// update is in flight; the agent gates everything off.
		writeJSON(w, http.StatusOK, storage.AgentRolloutState{TenantID: tid})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleAgentRolloutPut(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req agentRolloutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	tid, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tid, roleAdmin) {
		return
	}
	if req.RolloutPct < 0 || req.RolloutPct > 100 {
		http.Error(w, "rollout_pct must be 0..100", http.StatusBadRequest)
		return
	}
	updatedBy := principalUserID(s, r.Context(), principal)
	var by *uuid.UUID
	if updatedBy != uuid.Nil {
		id := updatedBy
		by = &id
	}
	state, err := s.store.UpsertAgentRolloutState(r.Context(), tid, storage.AgentRolloutUpdate{
		TargetReleaseSeq: req.TargetReleaseSeq,
		TargetVersion:    req.TargetVersion,
		RolloutPct:       req.RolloutPct,
		Paused:           req.Paused,
		UpdatedBy:        by,
	})
	if err != nil {
		s.logger.Error("upsert agent rollout state", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tid, "agent_rollout.update", "tenant", tid.String(), map[string]any{
		"target_release_seq": req.TargetReleaseSeq,
		"target_version":     req.TargetVersion,
		"rollout_pct":        req.RolloutPct,
		"paused":             req.Paused,
	})
	writeJSON(w, http.StatusOK, state)
}
