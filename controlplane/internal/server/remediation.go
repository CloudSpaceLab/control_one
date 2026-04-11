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

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/remediation"
)

type remediationScriptResponse struct {
	ID            string         `json:"id"`
	RuleID        string         `json:"rule_id"`
	Platform      string         `json:"platform"`
	ScriptType    string         `json:"script_type"`
	ScriptContent string         `json:"script_content"`
	Checksum      *string        `json:"checksum,omitempty"`
	Version       int            `json:"version"`
	Enabled       bool           `json:"enabled"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedBy     *string        `json:"created_by,omitempty"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

type createRemediationScriptRequest struct {
	RuleID        string         `json:"rule_id"`
	Platform      string         `json:"platform"`
	ScriptType    string         `json:"script_type"`
	ScriptContent string         `json:"script_content"`
	Version       *int           `json:"version,omitempty"`
	Enabled       *bool          `json:"enabled,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type updateRemediationScriptRequest struct {
	ScriptContent *string         `json:"script_content,omitempty"`
	Enabled       *bool           `json:"enabled,omitempty"`
	Metadata      *map[string]any `json:"metadata,omitempty"`
}

func (s *Server) handleRemediationScriptsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListRemediationScripts(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateRemediationScript(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRemediationScriptSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/remediation/scripts/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	segments := strings.Split(trimmed, "/")
	if len(segments) == 1 {
		scriptID, err := uuid.Parse(segments[0])
		if err != nil {
			http.Error(w, "invalid script id", http.StatusBadRequest)
			return
		}
		s.handleRemediationScriptResource(w, r, scriptID)
		return
	}

	if len(segments) == 2 && segments[1] == "execute" {
		scriptID, err := uuid.Parse(segments[0])
		if err != nil {
			http.Error(w, "invalid script id", http.StatusBadRequest)
			return
		}
		s.handleExecuteRemediationScript(w, r, scriptID)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleListRemediationScripts(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ruleID := strings.TrimSpace(r.URL.Query().Get("rule_id"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))

	scripts, total, err := s.store.ListRemediationScripts(r.Context(), ruleID, platform, limit, offset)
	if err != nil {
		s.logger.Error("list remediation scripts", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]remediationScriptResponse, 0, len(scripts))
	for _, script := range scripts {
		respItems = append(respItems, newRemediationScriptResponse(script))
	}

	resp := paginatedResponse[remediationScriptResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateRemediationScript(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req createRemediationScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.RuleID) == "" {
		http.Error(w, "rule_id is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ScriptType) == "" {
		http.Error(w, "script_type is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ScriptContent) == "" {
		http.Error(w, "script_content is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Platform) == "" {
		req.Platform = "all"
	}

	var createdBy *uuid.UUID
	if principal.Subject != "" {
		if id, err := uuid.Parse(principal.Subject); err == nil {
			createdBy = &id
		}
	}

	params := storage.CreateRemediationScriptParams{
		RuleID:        req.RuleID,
		Platform:      req.Platform,
		ScriptType:    req.ScriptType,
		ScriptContent: req.ScriptContent,
		Version:       req.Version,
		Enabled:       req.Enabled,
		Metadata:      req.Metadata,
		CreatedBy:     createdBy,
	}
	if params.Metadata == nil {
		params.Metadata = make(map[string]any)
	}

	created, err := s.store.CreateRemediationScript(r.Context(), params)
	if err != nil {
		s.logger.Error("create remediation script", zap.Error(err))
		http.Error(w, fmt.Sprintf("create remediation script failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newRemediationScriptResponse(*created)
	writeJSON(w, http.StatusCreated, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "remediation_script.created", "remediation_script", created.ID.String(), map[string]any{
		"rule_id":  req.RuleID,
		"platform": req.Platform,
	})
}

func (s *Server) handleRemediationScriptResource(w http.ResponseWriter, r *http.Request, scriptID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetRemediationScript(w, r, scriptID)
	case http.MethodPatch:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleUpdateRemediationScript(w, r, scriptID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetRemediationScript(w http.ResponseWriter, r *http.Request, scriptID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	ruleID := strings.TrimSpace(r.URL.Query().Get("rule_id"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))

	var script *storage.RemediationScript
	var err error

	if ruleID != "" {
		script, err = s.store.GetRemediationScript(r.Context(), ruleID, platform)
	} else {
		scripts, _, err := s.store.ListRemediationScripts(r.Context(), "", "", 1, 0)
		if err == nil && len(scripts) > 0 {
			for _, s := range scripts {
				if s.ID == scriptID {
					script = &s
					break
				}
			}
		}
	}

	if err != nil {
		s.logger.Error("get remediation script", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if script == nil {
		http.NotFound(w, r)
		return
	}

	resp := newRemediationScriptResponse(*script)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdateRemediationScript(w http.ResponseWriter, r *http.Request, scriptID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req updateRemediationScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	params := storage.UpdateRemediationScriptParams{
		ScriptContent: req.ScriptContent,
		Enabled:       req.Enabled,
		Metadata:      req.Metadata,
	}

	updated, err := s.store.UpdateRemediationScript(r.Context(), scriptID, params)
	if err != nil {
		s.logger.Error("update remediation script", zap.Error(err))
		http.Error(w, fmt.Sprintf("update remediation script failed: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	resp := newRemediationScriptResponse(*updated)
	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "remediation_script.updated", "remediation_script", scriptID.String(), map[string]any{})
}

func newRemediationScriptResponse(script storage.RemediationScript) remediationScriptResponse {
	resp := remediationScriptResponse{
		ID:            script.ID.String(),
		RuleID:        script.RuleID,
		Platform:      script.Platform,
		ScriptType:    script.ScriptType,
		ScriptContent: script.ScriptContent,
		Version:       script.Version,
		Enabled:       script.Enabled,
		Metadata:      script.Metadata,
		CreatedAt:     formatTime(script.CreatedAt),
		UpdatedAt:     formatTime(script.UpdatedAt),
	}
	if script.Checksum.Valid {
		checksum := script.Checksum.String
		resp.Checksum = &checksum
	}
	if script.CreatedBy != nil {
		createdBy := script.CreatedBy.String()
		resp.CreatedBy = &createdBy
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	return resp
}

type executeRemediationScriptRequest struct {
	NodeID string            `json:"node_id"`
	RuleID string            `json:"rule_id"`
	Env    map[string]string `json:"env,omitempty"`
}

type executeRemediationScriptResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (s *Server) handleExecuteRemediationScript(w http.ResponseWriter, r *http.Request, scriptID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleOperator)
	if !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	if s.worker == nil {
		http.Error(w, "worker queue unavailable", http.StatusServiceUnavailable)
		return
	}

	var req executeRemediationScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.NodeID) == "" {
		http.Error(w, "node_id is required", http.StatusBadRequest)
		return
	}

	nodeID, err := uuid.Parse(req.NodeID)
	if err != nil {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}

	script, err := s.store.GetRemediationScriptByID(r.Context(), scriptID)
	if err != nil {
		s.logger.Error("get remediation script", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if script == nil {
		http.NotFound(w, r)
		return
	}

	if !script.Enabled {
		http.Error(w, "remediation script is disabled", http.StatusBadRequest)
		return
	}

	ruleID := req.RuleID
	if ruleID == "" {
		ruleID = script.RuleID
	}

	jobPayload := map[string]any{
		"script_id":      scriptID.String(),
		"rule_id":        ruleID,
		"node_id":        nodeID.String(),
		"platform":       script.Platform,
		"script_type":    script.ScriptType,
		"script_content": script.ScriptContent,
	}
	if len(req.Env) > 0 {
		jobPayload["env"] = req.Env
	}

	payloadBytes, err := json.Marshal(jobPayload)
	if err != nil {
		s.logger.Error("marshal remediation job payload", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	job := &storage.Job{
		Type:     "remediation.execute",
		TenantID: uuid.Nil,
		Payload:  payloadBytes,
		Status:   storage.JobStatusQueued,
	}
	job, err = s.store.CreateJob(r.Context(), job, nil)
	if err != nil {
		s.logger.Error("create remediation job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	task := worker.Task{
		Name:         fmt.Sprintf("remediation-%s", job.ID),
		Job:          s.buildRemediationJobExecution(job.ID, scriptID, nodeID, ruleID, script),
		MaxAttempts:  3,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		s.logger.Error("enqueue remediation job", zap.Error(err))
		_ = s.store.UpdateJobStatus(r.Context(), job.ID, storage.JobStatusFailed, "failed to enqueue job", nil)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := executeRemediationScriptResponse{
		JobID:   job.ID.String(),
		Status:  string(job.Status),
		Message: "remediation job created and queued",
	}
	writeJSON(w, http.StatusAccepted, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "remediation.execute", "remediation_script", scriptID.String(), map[string]any{
		"job_id":  job.ID.String(),
		"node_id": nodeID.String(),
		"rule_id": ruleID,
	})
}

func (s *Server) buildRemediationJobExecution(jobID uuid.UUID, scriptID uuid.UUID, nodeID uuid.UUID, ruleID string, script *storage.RemediationScript) func(context.Context) error {
	return func(ctx context.Context) error {
		principal := s.systemActor()
		job, err := s.store.GetJob(ctx, jobID)
		if err != nil {
			return fmt.Errorf("load job: %w", err)
		}
		if job == nil {
			return fmt.Errorf("job %s not found", jobID)
		}

		if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "executing remediation script", nil); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}

		remediationEngine := remediation.NewEngine(s.logger, remediation.Options{
			Timeout: 5 * time.Minute,
		})

		remediationScript := remediation.Script{
			RuleID:        ruleID,
			Platform:      script.Platform,
			ScriptType:    script.ScriptType,
			ScriptContent: script.ScriptContent,
		}

		result, err := remediationEngine.Execute(ctx, remediationScript)
		if err != nil {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, fmt.Sprintf("remediation execution failed: %v", err), map[string]any{
				"error": err.Error(),
			})
			return err
		}

		status := storage.JobStatusSucceeded
		message := "remediation script executed successfully"
		if !result.Success {
			status = storage.JobStatusFailed
			message = fmt.Sprintf("remediation script failed: %s", result.Error)
		}

		if err := s.store.UpdateJobStatus(ctx, jobID, status, message, map[string]any{
			"output":      result.Output,
			"error":       result.Error,
			"executed_at": result.ExecutedAt,
			"duration":    result.Duration.String(),
		}); err != nil {
			return fmt.Errorf("update job status: %w", err)
		}

		s.recordAudit(ctx, principal, job.TenantID, "remediation.completed", "job", jobID.String(), map[string]any{
			"success": result.Success,
			"rule_id": ruleID,
			"node_id": nodeID.String(),
		})

		return nil
	}
}
