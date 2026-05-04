package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

// nodeRepairRequest carries one-shot SSH credentials the operator supplied
// via the Repair-agent dialog. Credentials are NEVER persisted — they live
// only in the in-flight job payload, which is purged from memory once the
// worker finishes. A future enhancement stores them encrypted in the
// secrets infra so reflexive repairs don't re-prompt.
type nodeRepairRequest struct {
	SSHUser      string `json:"ssh_user"`
	SSHKey       string `json:"ssh_key"`      // base64 PEM, optional
	SSHPassword  string `json:"ssh_password"` // optional
	HostOverride string `json:"host_override"`
	Port         int    `json:"port"`
}

type nodeRepairResponse struct {
	JobID    string `json:"job_id"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	ExpireAt string `json:"expire_at"`
	Message  string `json:"message"`
}

// handleNodeRepair issues a fresh enrollment token and dispatches a
// fleet-enroll job over SSH using operator-supplied credentials. Routed
// from handleNodeResource (segments[1]=="repair").
func (s *Server) handleNodeRepair(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var body nodeRepairRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.SSHUser) == "" {
		http.Error(w, "ssh_user is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.SSHKey) == "" && strings.TrimSpace(body.SSHPassword) == "" {
		http.Error(w, "ssh_key or ssh_password is required", http.StatusBadRequest)
		return
	}
	if body.SSHKey != "" {
		// Validate base64 up-front so the worker fails fast with a clean error
		// instead of hanging on an SSH dial that uses garbage key material.
		if _, err := base64.StdEncoding.DecodeString(body.SSHKey); err != nil {
			http.Error(w, "ssh_key must be base64-encoded PEM", http.StatusBadRequest)
			return
		}
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for repair", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	host := strings.TrimSpace(body.HostOverride)
	if host == "" {
		if v := node.PublicIP.String; v != "" {
			host = v
		} else {
			host = node.Hostname
		}
	}
	if host == "" {
		http.Error(w, "node has no public_ip or hostname; provide host_override", http.StatusBadRequest)
		return
	}
	port := body.Port
	if port <= 0 {
		port = 22
	}

	// Issue a fresh enrollment token (24h TTL). Same generate-then-hash
	// flow as handleCreateEnrollmentToken; we reproduce inline to avoid
	// exposing the token-creation surface as an internal helper.
	rawBytes := make([]byte, 16)
	if _, err := rand.Read(rawBytes); err != nil {
		s.logger.Error("generate repair token", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	rawToken := "cot_" + hex.EncodeToString(rawBytes)
	h := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(h[:])

	var createdBy *uuid.UUID
	if principal.Subject != "" {
		if id, err := uuid.Parse(principal.Subject); err == nil {
			createdBy = &id
		}
	}

	tokenParams := storage.CreateEnrollmentTokenParams{
		TenantID:  node.TenantID,
		Name:      fmt.Sprintf("repair · %s", node.Hostname),
		TokenHash: tokenHash,
		MaxNodes:  1,
		Labels: map[string]string{
			"node_id":  node.ID.String(),
			"hostname": node.Hostname,
			"source":   "repair-via-ssh",
		},
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedBy: createdBy,
	}
	tokenRow, err := s.store.CreateEnrollmentToken(r.Context(), tokenParams)
	if err != nil {
		s.logger.Error("create repair enrollment token", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	req := fleetEnrollRequest{
		Targets:     []fleet.Target{{Host: host, Port: port, User: body.SSHUser}},
		SSHUser:     body.SSHUser,
		SSHKey:      body.SSHKey,
		SSHPassword: body.SSHPassword,
		Token:       rawToken,
		Parallel:    1,
		Labels: map[string]string{
			"node_id":  node.ID.String(),
			"hostname": node.Hostname,
			"source":   "repair-via-ssh",
		},
	}
	payloadBytes, _ := json.Marshal(req)
	job := &storage.Job{
		Type:       "fleet.enroll",
		Status:     storage.JobStatusQueued,
		Payload:    payloadBytes,
		MaxRetries: 1,
	}
	created, err := s.store.CreateJob(r.Context(), job, &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: fmt.Sprintf("repair-via-ssh queued for node %s", node.Hostname),
	})
	if err != nil {
		s.logger.Error("create repair job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.recordAudit(r.Context(), principal, node.TenantID, "node.repair.dispatched",
		"node", node.ID.String(), map[string]any{
			"job_id": created.ID.String(),
			"host":   host,
		})

	jobFn := s.buildFleetEnrollJob(created.ID, req, s.deriveControlPlaneURL(r))
	if s.worker != nil {
		task := worker.Task{
			Name:         fmt.Sprintf("node-repair-%s", created.ID),
			Job:          jobFn,
			MaxAttempts:  1,
			RetryBackoff: s.cfg.Worker.RetryBackoff,
		}
		if err := s.worker.Enqueue(task); err != nil {
			s.logger.Error("enqueue repair job", zap.Error(err))
			_ = s.store.UpdateJobStatus(r.Context(), created.ID, storage.JobStatusFailed,
				fmt.Sprintf("enqueue failed: %v", err), map[string]any{"finished_at": time.Now()})
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
	} else {
		go func(fn func(context.Context) error) {
			_ = fn(context.Background())
		}(jobFn)
	}

	writeJSON(w, http.StatusAccepted, nodeRepairResponse{
		JobID:    created.ID.String(),
		Host:     host,
		Port:     port,
		ExpireAt: tokenRow.ExpiresAt.UTC().Format(time.RFC3339),
		Message:  fmt.Sprintf("repair dispatched to %s:%d", host, port),
	})
}
