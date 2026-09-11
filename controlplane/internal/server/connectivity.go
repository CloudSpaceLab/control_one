package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// JobTypeConnectivityTest is the job type for connectivity tests.
const JobTypeConnectivityTest = "connectivity_test"

type connectivityTestRequest struct {
	TargetIP   string `json:"target_ip"`
	TargetPort int    `json:"target_port"`
	Protocol   string `json:"protocol"`
	TimeoutMs  int    `json:"timeout_ms"`
}

type connectivityTestResponse struct {
	JobID  string `json:"job_id"`
	NodeID string `json:"node_id"`
	Status string `json:"status"`
}

func (s *Server) handleConnectivityTest(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
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
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req connectivityTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.TargetIP) == "" {
		http.Error(w, "target_ip is required", http.StatusBadRequest)
		return
	}
	if req.TargetPort <= 0 || req.TargetPort > 65535 {
		http.Error(w, "target_port must be 1-65535", http.StatusBadRequest)
		return
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	if protocol != "tcp" {
		http.Error(w, "protocol must be tcp", http.StatusBadRequest)
		return
	}
	timeoutMs := req.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for connectivity test", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	payload := map[string]any{
		"target_ip":   req.TargetIP,
		"target_port": req.TargetPort,
		"protocol":    protocol,
		"timeout_ms":  timeoutMs,
	}
	payloadJSON, _ := json.Marshal(payload)

	job := &storage.Job{
		ID:       uuid.New(),
		TenantID: node.TenantID,
		Type:     JobTypeConnectivityTest,
		Status:   storage.JobStatusQueued,
		Payload:  payloadJSON,
	}
	job, err = s.store.CreateJob(r.Context(), job, &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: "connectivity test requested",
	})
	if err != nil {
		s.logger.Error("create connectivity test job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	test := storage.NodeConnectivityTest{
		NodeID:     nodeID,
		TenantID:   node.TenantID,
		JobID:      &job.ID,
		TargetIP:   req.TargetIP,
		TargetPort: req.TargetPort,
		Protocol:   protocol,
		TimeoutMs:  timeoutMs,
		Status:     "pending",
		CreatedAt:  time.Now().UTC(),
	}
	if _, cerr := s.store.CreateNodeConnectivityTest(r.Context(), test); cerr != nil {
		s.logger.Error("create connectivity test record", zap.Error(cerr))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(connectivityTestResponse{
		JobID:  job.ID.String(),
		NodeID: nodeID.String(),
		Status: "pending",
	})

	s.recordAudit(r.Context(), principal, node.TenantID, "connectivity_test.requested", "node", nodeID.String(), map[string]any{
		"target_ip":   req.TargetIP,
		"target_port": req.TargetPort,
		"protocol":    protocol,
		"job_id":      job.ID.String(),
	})
}

// processConnectivityTestCompletedAction handles the agent-reported outcome of a connectivity test.
func (s *Server) processConnectivityTestCompletedAction(ctx context.Context, jobID uuid.UUID, c heartbeatCompletedAction) {
	reachable := c.Status == "succeeded" && metadataBool(c.Metadata, "reachable")
	errMsg := strings.TrimSpace(c.Error)
	if errMsg == "" && c.Status != "succeeded" {
		errMsg = "agent reported connectivity test failure"
	}

	if c.Status == "succeeded" {
		if jerr := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "agent reported connectivity test success", c.Metadata); jerr != nil {
			s.logger.Warn("connectivity test job mark succeeded",
				zap.String("job_id", jobID.String()), zap.Error(jerr))
		}
	} else {
		if errMsg == "" {
			errMsg = "agent reported connectivity test failure"
		}
		if jerr := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, errMsg, c.Metadata); jerr != nil {
			s.logger.Warn("connectivity test job mark failed",
				zap.String("job_id", jobID.String()), zap.Error(jerr))
		}
	}

	// Mark the connectivity test record. We look it up by finding tests
	// linked to this job. The simplest path: update directly by job_id.
	if merr := s.store.MarkNodeConnectivityTestByJobCompleted(ctx, jobID, reachable, errMsg); merr != nil {
		s.logger.Warn("mark connectivity test completed",
			zap.String("job_id", jobID.String()), zap.Error(merr))
	}
}
