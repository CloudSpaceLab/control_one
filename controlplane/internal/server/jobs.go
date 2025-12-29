package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
)

const (
	// JobTypeProvisionApply represents a provisioning plan execution.
	JobTypeProvisionApply = "provision.apply"
	// JobTypeComplianceScan represents a compliance scan across nodes.
	JobTypeComplianceScan = "compliance.scan"
)

type jobHandler func(ctx context.Context, job *storage.Job) error

func isValidJobStatus(status storage.JobStatus) bool {
	switch status {
	case storage.JobStatusQueued,
		storage.JobStatusRunning,
		storage.JobStatusSucceeded,
		storage.JobStatusFailed,
		storage.JobStatusCancelled:
		return true
	default:
		return false
	}
}

func (s *Server) enrichProvisioningMetadata(ctx context.Context, planID string, metadata map[string]string) {
	if s == nil || s.store == nil {
		return
	}
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return
	}
	tplID, err := uuid.Parse(planID)
	if err != nil {
		return
	}

	var version *storage.ProvisioningTemplateVersion
	if current, ok := metadata["template_version"]; ok {
		if verNum, err := strconv.Atoi(strings.TrimSpace(current)); err == nil && verNum > 0 {
			version, err = s.store.GetProvisioningTemplateVersion(ctx, tplID, verNum)
			if err != nil {
				s.logger.Warn("fetch template version",
					zap.Error(err),
					zap.String("template_id", tplID.String()),
					zap.Int("version", verNum),
				)
				return
			}
		}
	}

	if version == nil {
		var err error
		version, err = s.store.GetPromotedProvisioningTemplateVersion(ctx, tplID)
		if err != nil {
			s.logger.Warn("fetch promoted template version", zap.Error(err), zap.String("template_id", tplID.String()))
			return
		}
		if version == nil {
			s.logger.Warn("template version unavailable", zap.String("template_id", tplID.String()))
			return
		}
	}

	if metadata == nil {
		return
	}

	metadata["template_id"] = tplID.String()
	metadata["template_version"] = strconv.Itoa(version.Version)
	if version.Checksum.Valid {
		metadata["template_checksum"] = version.Checksum.String
	}
	if len(version.MetadataSchema) > 0 {
		metadata["template_schema"] = string(version.MetadataSchema)
	}
}

func defaultJobHandlers() map[string]jobHandler {
	return map[string]jobHandler{}
}

func jobRequiresTenant(jobType string) bool {
	switch jobType {
	case JobTypeProvisionApply, JobTypeComplianceScan:
		return true
	default:
		return false
	}
}

func (s *Server) configureJobIntegrations() {
	if s.jobHandlers == nil {
		s.jobHandlers = defaultJobHandlers()
	}

	if _, exists := s.jobHandlers[JobTypeProvisionApply]; !exists {
		client, err := newAPIClient(s.cfg.Jobs.Provisioning.APIBaseURL, s.cfg.Jobs.Provisioning.TLS, s.cfg.Jobs.Provisioning.Token)
		if err != nil {
			s.logger.Warn("initialize provisioning client", zap.Error(err))
		}
		templateSet := strings.TrimSpace(s.cfg.Jobs.Provisioning.Template) != ""
		if client != nil || templateSet || len(s.cfg.Jobs.Provisioning.Baselines) > 0 {
			opts := provisioning.Options{
				Template:        s.cfg.Jobs.Provisioning.Template,
				Provider:        s.cfg.Jobs.Provisioning.Provider,
				Baselines:       s.cfg.Jobs.Provisioning.Baselines,
				AutoRemediation: s.cfg.Jobs.Provisioning.AutoRemediation,
			}
			s.provisioningEngine = provisioning.NewEngine(s.logger.Named("provisioning-engine"), client, opts)
			s.jobHandlers[JobTypeProvisionApply] = s.handleProvisionApply
		}
		if s.provisioningEngine == nil {
			opts := provisioning.Options{
				Template:        "demo-template",
				Provider:        "mock",
				Baselines:       []string{"cis-aws-foundations"},
				AutoRemediation: s.cfg.Jobs.Provisioning.AutoRemediation,
			}
			s.logger.Warn("provisioning client unavailable; using mock engine")
			s.provisioningEngine = provisioning.NewEngine(s.logger.Named("provisioning-engine"), nil, opts)
			s.jobHandlers[JobTypeProvisionApply] = s.handleProvisionApply
		}
	}

	if _, exists := s.jobHandlers[JobTypeComplianceScan]; !exists {
		client, err := newAPIClient(s.cfg.Jobs.Compliance.APIBaseURL, s.cfg.Jobs.Compliance.TLS, s.cfg.Jobs.Compliance.Token)
		if err != nil {
			s.logger.Warn("initialize compliance client", zap.Error(err))
		}
		ruleSetConfigured := len(s.cfg.Jobs.Compliance.RuleSets) > 0 || len(s.cfg.Jobs.Compliance.Certifications) > 0
		if client != nil || ruleSetConfigured {
			opts := compliance.Options{
				Region:         s.cfg.Jobs.Compliance.Region,
				RuleSets:       s.cfg.Jobs.Compliance.RuleSets,
				Certifications: s.cfg.Jobs.Compliance.Certifications,
				AutoApply:      s.cfg.Jobs.Compliance.AutoApply,
			}
			s.complianceEngine = compliance.NewEngine(s.logger.Named("compliance-engine"), client, opts)
			s.jobHandlers[JobTypeComplianceScan] = s.handleComplianceScan
		}
		if s.complianceEngine == nil {
			opts := compliance.Options{
				Region:         "us-mock-1",
				RuleSets:       []string{"mock-best-practices"},
				Certifications: []string{"soc2"},
				AutoApply:      s.cfg.Jobs.Compliance.AutoApply,
			}
			s.logger.Warn("compliance client unavailable; using mock engine")
			s.complianceEngine = compliance.NewEngine(s.logger.Named("compliance-engine"), nil, opts)
			s.jobHandlers[JobTypeComplianceScan] = s.handleComplianceScan
		}
	}
}

type provisionPayload struct {
	PlanID   string            `json:"plan_id"`
	TenantID string            `json:"tenant_id"`
	NodeID   string            `json:"node_id"`
	Metadata map[string]string `json:"metadata"`
}

type compliancePayload struct {
	ScanID   string            `json:"scan_id"`
	TenantID string            `json:"tenant_id"`
	NodeID   string            `json:"node_id"`
	Policies map[string]string `json:"policies"`
}

func (s *Server) handleProvisionApply(ctx context.Context, job *storage.Job) error {
	payload, err := decodeProvisionPayload(job.Payload)
	if err != nil {
		return err
	}
	if s.provisioningEngine == nil {
		return fmt.Errorf("provisioning engine not configured")
	}

	metadata := map[string]string{
		"job_id":    job.ID.String(),
		"plan_id":   payload.PlanID,
		"tenant_id": payload.TenantID,
	}
	for k, v := range payload.Metadata {
		metadata[k] = v
	}

	s.enrichProvisioningMetadata(ctx, payload.PlanID, metadata)

	s.logger.Info("provisioning job starting",
		zap.String("job_id", job.ID.String()),
		zap.String("node_id", payload.NodeID),
		zap.String("plan_id", payload.PlanID),
	)

	if err := s.provisioningEngine.ApplyTemplate(ctx, payload.NodeID, metadata); err != nil {
		return fmt.Errorf("apply template: %w", err)
	}
	if err := s.provisioningEngine.RunBaselines(ctx, payload.NodeID); err != nil {
		s.logger.Warn("provisioning baselines failed", zap.Error(err), zap.String("node_id", payload.NodeID))
	}

	s.logger.Info("provisioning job completed",
		zap.String("job_id", job.ID.String()),
		zap.String("node_id", payload.NodeID),
	)

	return nil
}

func (s *Server) handleComplianceScan(ctx context.Context, job *storage.Job) error {
	payload, err := decodeCompliancePayload(job.Payload)
	if err != nil {
		return err
	}
	if s.complianceEngine == nil {
		return fmt.Errorf("compliance engine not configured")
	}

	s.logger.Info("compliance job starting",
		zap.String("job_id", job.ID.String()),
		zap.String("node_id", payload.NodeID),
		zap.String("scan_id", payload.ScanID),
	)

	results, err := s.complianceEngine.Evaluate(ctx, payload.NodeID, payload.Policies)
	if err != nil {
		return fmt.Errorf("compliance evaluate: %w", err)
	}
	s.logger.Info("compliance job completed",
		zap.String("job_id", job.ID.String()),
		zap.String("node_id", payload.NodeID),
		zap.String("scan_id", payload.ScanID),
		zap.Int("results", len(results)),
	)
	return nil
}

func decodeProvisionPayload(data []byte) (*provisionPayload, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("provision payload required")
	}
	var payload provisionPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode provision payload: %w", err)
	}
	if strings.TrimSpace(payload.PlanID) == "" {
		return nil, fmt.Errorf("plan_id is required")
	}
	if strings.TrimSpace(payload.TenantID) == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(payload.NodeID) == "" {
		return nil, fmt.Errorf("node_id is required")
	}
	return &payload, nil
}

func decodeCompliancePayload(data []byte) (*compliancePayload, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("compliance payload required")
	}
	var payload compliancePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode compliance payload: %w", err)
	}
	if strings.TrimSpace(payload.ScanID) == "" {
		return nil, fmt.Errorf("scan_id is required")
	}
	if strings.TrimSpace(payload.TenantID) == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(payload.NodeID) == "" {
		return nil, fmt.Errorf("node_id is required")
	}
	return &payload, nil
}

func validateJobPayload(jobType string, payload json.RawMessage) (any, error) {
	switch jobType {
	case JobTypeProvisionApply:
		val, err := decodeProvisionPayload(payload)
		return val, err
	case JobTypeComplianceScan:
		val, err := decodeCompliancePayload(payload)
		return val, err
	default:
		return nil, nil
	}
}

type createJobRequest struct {
	Type       string          `json:"type"`
	TenantID   *string         `json:"tenant_id"`
	Payload    json.RawMessage `json:"payload"`
	MaxRetries int             `json:"max_retries"`
}

func (r createJobRequest) validate() error {
	if strings.TrimSpace(r.Type) == "" {
		return fmt.Errorf("type is required")
	}
	if r.TenantID != nil {
		if _, err := uuid.Parse(*r.TenantID); err != nil {
			return fmt.Errorf("invalid tenant_id: %w", err)
		}
	}
	if r.MaxRetries < 0 {
		return fmt.Errorf("max_retries must be non-negative")
	}
	return nil
}

func (s *Server) handleJobsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListJobs(w, r)
	case http.MethodPost:
		s.handleCreateJob(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "job store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	jobType := strings.TrimSpace(r.URL.Query().Get("type"))
	statusParam := strings.TrimSpace(r.URL.Query().Get("status"))
	var status storage.JobStatus
	if statusParam != "" {
		candidate := storage.JobStatus(statusParam)
		if !isValidJobStatus(candidate) {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		status = candidate
	}

	jobs, total, err := s.store.ListJobs(r.Context(), tenantID, jobType, status, limit, offset)
	if err != nil {
		s.logger.Error("list jobs", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := make([]jobResponse, 0, len(jobs))
	for i := range jobs {
		resp = append(resp, jobResponseFromModel(&jobs[i], nil))
	}

	payload := paginatedResponse[jobResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Warn("encode jobs response", zap.Error(err))
	}
}

func (s *Server) handleJobResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := auth.PrincipalFromContext(r.Context()); !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/")
	jobID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}

	job, err := s.store.GetJob(r.Context(), jobID)
	if err != nil {
		s.logger.Error("get job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if job == nil {
		http.NotFound(w, r)
		return
	}

	events, err := s.store.ListJobEvents(r.Context(), jobID)
	if err != nil {
		s.logger.Error("list job events", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jobResponseFromModel(job, events)); err != nil {
		s.logger.Warn("encode job response", zap.Error(err))
	}
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "job store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleOperator, roleAdmin); !ok {
		return
	}

	var req createJobRequest
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

	s.configureJobIntegrations()
	if _, ok := s.jobHandlers[req.Type]; !ok {
		http.Error(w, "unsupported job type", http.StatusBadRequest)
		return
	}

	payloadDetails, err := validateJobPayload(req.Type, req.Payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if req.TenantID != nil {
		parsed, _ := uuid.Parse(*req.TenantID)
		tenantID = parsed
		tenant, err := s.store.GetTenant(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("get tenant", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if tenant == nil {
			http.Error(w, "tenant not found", http.StatusBadRequest)
			return
		}
	}

	if jobRequiresTenant(req.Type) {
		if req.TenantID == nil {
			http.Error(w, "tenant_id is required for this job type", http.StatusBadRequest)
			return
		}
		if tenantID == uuid.Nil {
			http.Error(w, "tenant_id is invalid", http.StatusBadRequest)
			return
		}
		switch v := payloadDetails.(type) {
		case *provisionPayload:
			if v != nil && v.TenantID != "" && !strings.EqualFold(v.TenantID, tenantID.String()) {
				http.Error(w, "tenant mismatch between payload and request", http.StatusBadRequest)
				return
			}
		case *compliancePayload:
			if v != nil && v.TenantID != "" && !strings.EqualFold(v.TenantID, tenantID.String()) {
				http.Error(w, "tenant mismatch between payload and request", http.StatusBadRequest)
				return
			}
		}
	}

	job := &storage.Job{
		TenantID:   tenantID,
		Type:       strings.TrimSpace(req.Type),
		Status:     storage.JobStatusQueued,
		Payload:    append(json.RawMessage(nil), req.Payload...),
		MaxRetries: req.MaxRetries,
	}
	if job.MaxRetries == 0 {
		job.MaxRetries = 3
	}
	initialEvent := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: fmt.Sprintf("job queued: %s", job.Type),
	}

	created, err := s.store.CreateJob(r.Context(), job, initialEvent)
	if err != nil {
		s.logger.Error("create job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if s.worker != nil {
		task := worker.Task{
			Name:         fmt.Sprintf("job-%s", created.ID),
			Job:          s.buildJobExecution(created.ID, created.Type, created.MaxRetries),
			MaxAttempts:  created.MaxRetries,
			RetryBackoff: s.cfg.Worker.RetryBackoff,
		}
		if err := s.worker.Enqueue(task); err != nil {
			s.logger.Error("enqueue job", zap.Error(err))
			_ = s.store.UpdateJobStatus(r.Context(), created.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), map[string]any{"finished_at": time.Now()})
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
	} else {
		go func(jobID uuid.UUID, jobType string, attempts int) {
			exec := s.buildJobExecution(jobID, jobType, attempts)
			for attempt := 1; attempt <= attempts; attempt++ {
				if err := exec(context.Background()); err == nil {
					return
				}
				time.Sleep(s.cfg.Worker.RetryBackoff)
			}
		}(created.ID, created.Type, created.MaxRetries)
	}

	events := []storage.JobEvent{*initialEvent}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(jobResponseFromModel(created, events)); err != nil {
		s.logger.Warn("encode job response", zap.Error(err))
	}
}

type jobResponse struct {
	ID         string            `json:"id"`
	TenantID   *string           `json:"tenant_id,omitempty"`
	Type       string            `json:"type"`
	Status     string            `json:"status"`
	Payload    json.RawMessage   `json:"payload"`
	Retries    int               `json:"retries"`
	MaxRetries int               `json:"max_retries"`
	Scheduled  *string           `json:"scheduled_at,omitempty"`
	Started    *string           `json:"started_at,omitempty"`
	Finished   *string           `json:"finished_at,omitempty"`
	Created    string            `json:"created_at"`
	Updated    string            `json:"updated_at"`
	Events     []jobEventPayload `json:"events"`
}

type jobEventPayload struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	Message   *string `json:"message,omitempty"`
	CreatedAt string  `json:"created_at"`
}

func jobResponseFromModel(job *storage.Job, events []storage.JobEvent) jobResponse {
	resp := jobResponse{
		ID:         job.ID.String(),
		Type:       job.Type,
		Status:     string(job.Status),
		Payload:    json.RawMessage(job.Payload),
		Retries:    job.Retries,
		MaxRetries: job.MaxRetries,
		Created:    job.CreatedAt.UTC().Format(time.RFC3339),
		Updated:    job.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if job.TenantID != uuid.Nil {
		tid := job.TenantID.String()
		resp.TenantID = &tid
	}
	if job.ScheduledAt != nil {
		s := job.ScheduledAt.UTC().Format(time.RFC3339)
		resp.Scheduled = &s
	}
	if job.StartedAt != nil {
		s := job.StartedAt.UTC().Format(time.RFC3339)
		resp.Started = &s
	}
	if job.FinishedAt != nil {
		s := job.FinishedAt.UTC().Format(time.RFC3339)
		resp.Finished = &s
	}
	if len(events) > 0 {
		resp.Events = make([]jobEventPayload, 0, len(events))
		for _, evt := range events {
			payload := jobEventPayload{
				ID:        evt.ID.String(),
				Status:    string(evt.Status),
				CreatedAt: evt.CreatedAt.UTC().Format(time.RFC3339),
			}
			if strings.TrimSpace(evt.Message) != "" {
				msg := evt.Message
				payload.Message = &msg
			}
			resp.Events = append(resp.Events, payload)
		}
	}
	return resp
}

func newAPIClient(baseURL string, tlsCfg config.ClientTLSConfig, token string) (*api.Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, nil
	}

	certFile := strings.TrimSpace(tlsCfg.CertFile)
	keyFile := strings.TrimSpace(tlsCfg.KeyFile)
	caFile := strings.TrimSpace(tlsCfg.CACertFile)

	client, err := api.NewClient(baseURL, certFile, keyFile, caFile, strings.TrimSpace(token))
	if err != nil {
		return nil, fmt.Errorf("new api client: %w", err)
	}
	return client, nil
}
