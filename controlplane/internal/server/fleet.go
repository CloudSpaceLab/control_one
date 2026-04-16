package server

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/fleet"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

// ─── Request / Response types ───────────────────────────────────────

type fleetEnrollRequest struct {
	Targets     []fleet.Target    `json:"targets"`
	SSHUser     string            `json:"ssh_user"`
	SSHKey      string            `json:"ssh_key"`      // base64-encoded PEM
	SSHPassword string            `json:"ssh_password"`  // optional
	Token       string            `json:"token"`
	Parallel    int               `json:"parallel"`
	Labels      map[string]string `json:"labels"`
}

func (r fleetEnrollRequest) validate() error {
	if len(r.Targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	for i, t := range r.Targets {
		if strings.TrimSpace(t.Host) == "" {
			return fmt.Errorf("target[%d].host is required", i)
		}
	}
	if strings.TrimSpace(r.Token) == "" {
		return fmt.Errorf("token is required")
	}
	if strings.TrimSpace(r.SSHUser) == "" && !hasPerTargetUsers(r.Targets) {
		return fmt.Errorf("ssh_user is required when targets lack per-host users")
	}
	if strings.TrimSpace(r.SSHKey) == "" && strings.TrimSpace(r.SSHPassword) == "" {
		return fmt.Errorf("ssh_key or ssh_password is required")
	}
	return nil
}

func hasPerTargetUsers(targets []fleet.Target) bool {
	for _, t := range targets {
		if strings.TrimSpace(t.User) == "" {
			return false
		}
	}
	return true
}

type fleetEnrollResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type fleetEnrollStatusResponse struct {
	JobID   string                       `json:"job_id"`
	Status  string                       `json:"status"`
	Results []fleetEnrollResultResponse  `json:"results"`
}

type fleetEnrollResultResponse struct {
	ID           string  `json:"id"`
	Host         string  `json:"host"`
	Port         int     `json:"port"`
	Success      bool    `json:"success"`
	NodeID       *string `json:"node_id,omitempty"`
	ErrorMessage *string `json:"error_message,omitempty"`
	SSHOutput    *string `json:"ssh_output,omitempty"`
	DurationMs   *int32  `json:"duration_ms,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// ─── Handlers ───────────────────────────────────────────────────────

func (s *Server) handleFleetEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req fleetEnrollRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	// Create a job record.
	payloadBytes, _ := json.Marshal(req)
	job := &storage.Job{
		Type:       "fleet.enroll",
		Status:     storage.JobStatusQueued,
		Payload:    payloadBytes,
		MaxRetries: 1,
	}
	initialEvent := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: fmt.Sprintf("fleet enrollment queued for %d targets", len(req.Targets)),
	}

	ctx := r.Context()
	created, err := s.store.CreateJob(ctx, job, initialEvent)
	if err != nil {
		s.logger.Error("create fleet enroll job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.recordAudit(ctx, principal, uuid.Nil, "fleet.enroll.created", "job", created.ID.String(), map[string]any{
		"targets": len(req.Targets),
	})

	// Enqueue work.
	jobFn := s.buildFleetEnrollJob(created.ID, req)

	if s.worker != nil {
		task := worker.Task{
			Name:         fmt.Sprintf("fleet-enroll-%s", created.ID),
			Job:          jobFn,
			MaxAttempts:  1,
			RetryBackoff: s.cfg.Worker.RetryBackoff,
		}
		if err := s.worker.Enqueue(task); err != nil {
			s.logger.Error("enqueue fleet enroll job", zap.Error(err))
			_ = s.store.UpdateJobStatus(ctx, created.ID, storage.JobStatusFailed,
				fmt.Sprintf("enqueue failed: %v", err), map[string]any{"finished_at": time.Now()})
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
	} else {
		go func(fn func(context.Context) error) {
			_ = fn(context.Background())
		}(jobFn)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(fleetEnrollResponse{
		JobID:   created.ID.String(),
		Status:  string(created.Status),
		Message: fmt.Sprintf("fleet enrollment started for %d targets", len(req.Targets)),
	})
}

func (s *Server) buildFleetEnrollJob(jobID uuid.UUID, req fleetEnrollRequest) func(context.Context) error {
	return func(ctx context.Context) error {
		_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "provisioning targets", map[string]any{
			"started_at": time.Now(),
		})

		provisioner := fleet.NewProvisioner(s.logger)

		var sshKey []byte
		if strings.TrimSpace(req.SSHKey) != "" {
			decoded, err := base64.StdEncoding.DecodeString(req.SSHKey)
			if err != nil {
				errMsg := fmt.Sprintf("decode ssh_key: %v", err)
				_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, errMsg, map[string]any{
					"finished_at": time.Now(),
				})
				return fmt.Errorf("%s", errMsg)
			}
			sshKey = decoded
		}

		cpURL := strings.TrimSpace(s.cfg.HTTP.Address)
		if !strings.HasPrefix(cpURL, "http") {
			cpURL = fmt.Sprintf("https://localhost%s", cpURL)
		}

		provReq := fleet.ProvisionRequest{
			Targets:     req.Targets,
			SSHUser:     req.SSHUser,
			SSHKey:      sshKey,
			SSHPassword: req.SSHPassword,
			TokenRaw:    req.Token,
			CPURL:       cpURL,
			Parallel:    req.Parallel,
			Labels:      req.Labels,
		}

		results := provisioner.Provision(ctx, provReq)

		// Persist each result.
		var successCount, failCount int
		for _, r := range results {
			record := &storage.FleetEnrollmentResult{
				JobID:   jobID,
				Host:    r.Host,
				Port:    r.Port,
				Success: r.Success,
			}
			if r.Error != "" {
				record.ErrorMessage = sql.NullString{String: r.Error, Valid: true}
			}
			if r.Output != "" {
				record.SSHOutput = sql.NullString{String: r.Output, Valid: true}
			}
			if r.DurationMs > 0 {
				record.DurationMs = sql.NullInt32{Int32: int32(r.DurationMs), Valid: true}
			}

			if err := s.store.CreateFleetEnrollmentResult(ctx, record); err != nil {
				s.logger.Error("save fleet enrollment result", zap.Error(err), zap.String("host", r.Host))
			}

			if r.Success {
				successCount++
			} else {
				failCount++
			}
		}

		finalStatus := storage.JobStatusSucceeded
		msg := fmt.Sprintf("completed: %d succeeded, %d failed", successCount, failCount)
		if failCount > 0 && successCount == 0 {
			finalStatus = storage.JobStatusFailed
		}

		_ = s.store.UpdateJobStatus(ctx, jobID, finalStatus, msg, map[string]any{
			"finished_at": time.Now(),
			"succeeded":   successCount,
			"failed":      failCount,
		})

		return nil
	}
}

func (s *Server) handleFleetEnrollStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	// Parse job_id from /api/v1/fleet/enroll/{job_id}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/fleet/enroll/")
	idStr = strings.TrimSuffix(idStr, "/")
	jobID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid job_id", http.StatusBadRequest)
		return
	}

	job, err := s.store.GetJob(r.Context(), jobID)
	if err != nil {
		s.logger.Error("get fleet enroll job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if job == nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	results, err := s.store.ListFleetEnrollmentResults(r.Context(), jobID)
	if err != nil {
		s.logger.Error("list fleet enrollment results", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := fleetEnrollStatusResponse{
		JobID:   job.ID.String(),
		Status:  string(job.Status),
		Results: make([]fleetEnrollResultResponse, 0, len(results)),
	}

	for _, r := range results {
		item := fleetEnrollResultResponse{
			ID:        r.ID.String(),
			Host:      r.Host,
			Port:      r.Port,
			Success:   r.Success,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		}
		if r.NodeID != nil {
			s := r.NodeID.String()
			item.NodeID = &s
		}
		if r.ErrorMessage.Valid {
			item.ErrorMessage = &r.ErrorMessage.String
		}
		if r.SSHOutput.Valid {
			item.SSHOutput = &r.SSHOutput.String
		}
		if r.DurationMs.Valid {
			v := r.DurationMs.Int32
			item.DurationMs = &v
		}
		resp.Results = append(resp.Results, item)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode fleet enroll status", zap.Error(err))
	}
}
