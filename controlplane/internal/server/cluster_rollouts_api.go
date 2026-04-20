package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

// Cluster rollout job types for the async control-plane queue.
const (
	JobTypeClusterRolloutAdvance   = "cluster.rollout.advance"
	JobTypeClusterRolloutGateCheck = "cluster.rollout.gate_check"
	JobTypeClusterRolloutPrefix    = "cluster.rollout."
)

// Cluster rollout lifecycle states persisted in cluster_rollouts.state.
const (
	RolloutStatePending   = "pending"
	RolloutStateRunning   = "running"
	RolloutStateHalted    = "halted"
	RolloutStateCompleted = "completed"
	RolloutStateAborted   = "aborted"
)

// Webhook event types emitted during a cluster rollout lifecycle.
const (
	EventClusterRolloutStarted       = "cluster.rollout.started"
	EventClusterRolloutWaveHealthy   = "cluster.rollout.wave_healthy"
	EventClusterRolloutWaveUnhealthy = "cluster.rollout.wave_unhealthy"
	EventClusterRolloutHalted        = "cluster.rollout.halted"
	EventClusterRolloutCompleted     = "cluster.rollout.completed"
	EventClusterRolloutAborted       = "cluster.rollout.aborted"
)

// Gate-type discriminator values understood by the gate_check handler.
const (
	rolloutGateTypeHeartbeat  = "heartbeat"
	rolloutGateTypeCompliance = "compliance"
	rolloutGateTypeHTTP       = "http"
)

type createClusterRolloutRequest struct {
	TemplateVersionID string         `json:"template_version_id"`
	WaveSize          int            `json:"wave_size"`
	WaveStrategy      string         `json:"wave_strategy,omitempty"`
	HealthGate        map[string]any `json:"health_gate,omitempty"`
}

type clusterRolloutWaveResponse struct {
	ID          string         `json:"id"`
	WaveNumber  int            `json:"wave_number"`
	MemberIDs   []string       `json:"member_ids"`
	State       string         `json:"state"`
	StartedAt   string         `json:"started_at"`
	CompletedAt *string        `json:"completed_at,omitempty"`
	GateResult  map[string]any `json:"gate_result,omitempty"`
}

type clusterRolloutDetailResponse struct {
	clusterRolloutResponse
	ClusterID string                       `json:"cluster_id"`
	Waves     []clusterRolloutWaveResponse `json:"waves"`
}

type clusterRolloutAcceptedResponse struct {
	ClusterID string `json:"cluster_id"`
	RolloutID string `json:"rollout_id"`
	JobID     string `json:"job_id"`
	State     string `json:"state"`
}

// handleClusterRolloutsRoute dispatches the /api/v1/clusters/{id}/rollouts...
// subtree. Segments start AFTER the "rollouts" literal.
func (s *Server) handleClusterRolloutsRoute(w http.ResponseWriter, r *http.Request, clusterID uuid.UUID, rest []string) {
	// /api/v1/clusters/{id}/rollouts
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodPost:
			principal, ok := s.authorize(w, r, roleAdmin)
			if !ok {
				return
			}
			s.handleCreateClusterRollout(w, r, clusterID, principal)
		default:
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/v1/clusters/{id}/rollouts/{rolloutID}[/abort|/resume]
	rolloutID, err := uuid.Parse(rest[0])
	if err != nil {
		http.Error(w, "invalid rollout id", http.StatusBadRequest)
		return
	}

	if len(rest) == 1 {
		switch r.Method {
		case http.MethodGet:
			if _, ok := s.authorize(w, r, roleViewer); !ok {
				return
			}
			s.handleGetClusterRollout(w, r, clusterID, rolloutID)
		default:
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}

	if len(rest) != 2 {
		http.NotFound(w, r)
		return
	}

	switch rest[1] {
	case "abort":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleAbortClusterRollout(w, r, clusterID, rolloutID, principal)
	case "resume":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleResumeClusterRollout(w, r, clusterID, rolloutID, principal)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleCreateClusterRollout(w http.ResponseWriter, r *http.Request, clusterID uuid.UUID, principal *auth.Principal) {
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster for rollout create", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}

	var req createClusterRolloutRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.TemplateVersionID) == "" {
		http.Error(w, "template_version_id is required", http.StatusBadRequest)
		return
	}
	templateVersionID, err := uuid.Parse(strings.TrimSpace(req.TemplateVersionID))
	if err != nil {
		http.Error(w, "invalid template_version_id", http.StatusBadRequest)
		return
	}
	if req.WaveSize < 1 {
		http.Error(w, "wave_size must be >= 1", http.StatusBadRequest)
		return
	}
	strategy := strings.TrimSpace(req.WaveStrategy)
	if strategy == "" {
		strategy = "rolling"
	}
	if err := validateRolloutHealthGate(req.HealthGate); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rollout, err := s.store.CreateClusterRollout(r.Context(), storage.CreateClusterRolloutParams{
		ClusterID:         clusterID,
		TemplateVersionID: templateVersionID,
		WaveSize:          req.WaveSize,
		WaveStrategy:      strategy,
		HealthGate:        req.HealthGate,
		State:             RolloutStatePending,
		CurrentWave:       0,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create cluster rollout: %v", err), http.StatusBadRequest)
		return
	}

	jobID, enqueueErr := s.enqueueClusterRolloutAdvance(r, cluster.TenantID, clusterID, rollout.ID, 0)
	if enqueueErr != nil {
		s.logger.Error("enqueue cluster rollout advance", zap.Error(enqueueErr))
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	s.recordAudit(r.Context(), principal, cluster.TenantID, "cluster.rollout.created", "cluster_rollout", rollout.ID.String(), map[string]any{
		"cluster_id":          clusterID.String(),
		"template_version_id": templateVersionID.String(),
		"wave_size":           req.WaveSize,
		"wave_strategy":       strategy,
		"job_id":              jobID.String(),
	})

	s.emitRolloutEvent(r.Context(), cluster.TenantID, EventClusterRolloutStarted, map[string]any{
		"cluster_id":          clusterID.String(),
		"rollout_id":          rollout.ID.String(),
		"template_version_id": templateVersionID.String(),
		"wave_size":           req.WaveSize,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(clusterRolloutAcceptedResponse{
		ClusterID: clusterID.String(),
		RolloutID: rollout.ID.String(),
		JobID:     jobID.String(),
		State:     rollout.State,
	})
}

func (s *Server) handleGetClusterRollout(w http.ResponseWriter, r *http.Request, clusterID, rolloutID uuid.UUID) {
	rollout, err := s.store.GetClusterRolloutByID(r.Context(), rolloutID)
	if err != nil {
		s.logger.Error("get cluster rollout", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if rollout == nil || rollout.ClusterID != clusterID {
		http.NotFound(w, r)
		return
	}

	waves, err := s.store.ListClusterRolloutWaves(r.Context(), rolloutID)
	if err != nil {
		s.logger.Warn("list cluster rollout waves", zap.Error(err))
	}

	resp := clusterRolloutDetailResponse{
		clusterRolloutResponse: clusterRolloutResponse{
			ID:                rollout.ID.String(),
			TemplateVersionID: rollout.TemplateVersionID.String(),
			WaveSize:          rollout.WaveSize,
			WaveStrategy:      rollout.WaveStrategy,
			HealthGate:        rollout.HealthGate,
			State:             rollout.State,
			CurrentWave:       rollout.CurrentWave,
			CreatedAt:         formatTime(rollout.CreatedAt),
			UpdatedAt:         formatTime(rollout.UpdatedAt),
		},
		ClusterID: rollout.ClusterID.String(),
	}
	if resp.HealthGate == nil {
		resp.HealthGate = map[string]any{}
	}
	resp.Waves = make([]clusterRolloutWaveResponse, 0, len(waves))
	for _, wv := range waves {
		resp.Waves = append(resp.Waves, newClusterRolloutWaveResponse(wv))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAbortClusterRollout(w http.ResponseWriter, r *http.Request, clusterID, rolloutID uuid.UUID, principal *auth.Principal) {
	rollout, err := s.store.GetClusterRolloutByID(r.Context(), rolloutID)
	if err != nil {
		s.logger.Error("get cluster rollout for abort", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if rollout == nil || rollout.ClusterID != clusterID {
		http.NotFound(w, r)
		return
	}
	if rollout.State == RolloutStateAborted || rollout.State == RolloutStateCompleted {
		http.Error(w, fmt.Sprintf("rollout is already %s", rollout.State), http.StatusConflict)
		return
	}

	abortedState := RolloutStateAborted
	if _, err := s.store.UpdateClusterRollout(r.Context(), rolloutID, storage.UpdateClusterRolloutParams{State: &abortedState}); err != nil {
		s.logger.Error("update cluster rollout state=aborted", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Best-effort abort current wave.
	if wave, err := s.store.GetClusterRolloutWaveByNumber(r.Context(), rolloutID, rollout.CurrentWave); err == nil && wave != nil && wave.State == storage.ClusterRolloutWaveStateRunning {
		waveState := storage.ClusterRolloutWaveStateAborted
		if _, err := s.store.UpdateClusterRolloutWave(r.Context(), wave.ID, storage.UpdateClusterRolloutWaveParams{State: &waveState}); err != nil {
			s.logger.Warn("update rollout wave state=aborted", zap.Error(err))
		}
	}

	cluster, _ := s.store.GetClusterByID(r.Context(), clusterID)
	var tenantID uuid.UUID
	if cluster != nil {
		tenantID = cluster.TenantID
	}
	s.recordAudit(r.Context(), principal, tenantID, "cluster.rollout.aborted", "cluster_rollout", rolloutID.String(), map[string]any{
		"cluster_id":   clusterID.String(),
		"current_wave": rollout.CurrentWave,
	})
	s.emitRolloutEvent(r.Context(), tenantID, EventClusterRolloutAborted, map[string]any{
		"cluster_id":   clusterID.String(),
		"rollout_id":   rolloutID.String(),
		"current_wave": rollout.CurrentWave,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResumeClusterRollout(w http.ResponseWriter, r *http.Request, clusterID, rolloutID uuid.UUID, principal *auth.Principal) {
	rollout, err := s.store.GetClusterRolloutByID(r.Context(), rolloutID)
	if err != nil {
		s.logger.Error("get cluster rollout for resume", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if rollout == nil || rollout.ClusterID != clusterID {
		http.NotFound(w, r)
		return
	}
	if rollout.State != RolloutStateHalted {
		http.Error(w, fmt.Sprintf("rollout is not halted (state=%s)", rollout.State), http.StatusConflict)
		return
	}

	// Rewind to the unhealthy wave, clear its state so advance treats it as fresh.
	runningState := RolloutStateRunning
	if _, err := s.store.UpdateClusterRollout(r.Context(), rolloutID, storage.UpdateClusterRolloutParams{State: &runningState}); err != nil {
		s.logger.Error("update cluster rollout state=running", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	cluster, _ := s.store.GetClusterByID(r.Context(), clusterID)
	var tenantID uuid.UUID
	if cluster != nil {
		tenantID = cluster.TenantID
	}

	jobID, enqueueErr := s.enqueueClusterRolloutAdvance(r, tenantID, clusterID, rolloutID, rollout.CurrentWave)
	if enqueueErr != nil {
		s.logger.Error("enqueue cluster rollout advance on resume", zap.Error(enqueueErr))
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	s.recordAudit(r.Context(), principal, tenantID, "cluster.rollout.resumed", "cluster_rollout", rolloutID.String(), map[string]any{
		"cluster_id":   clusterID.String(),
		"current_wave": rollout.CurrentWave,
		"job_id":       jobID.String(),
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(clusterRolloutAcceptedResponse{
		ClusterID: clusterID.String(),
		RolloutID: rolloutID.String(),
		JobID:     jobID.String(),
		State:     runningState,
	})
}

// enqueueClusterRolloutAdvance creates a cluster.rollout.advance job + task pair.
func (s *Server) enqueueClusterRolloutAdvance(r *http.Request, tenantID, clusterID, rolloutID uuid.UUID, waveNumber int) (uuid.UUID, error) {
	return s.enqueueClusterRolloutJob(r, tenantID, clusterRolloutAdvanceOptions{
		ClusterID:  clusterID,
		RolloutID:  rolloutID,
		WaveNumber: waveNumber,
	})
}

type clusterRolloutAdvanceOptions struct {
	ClusterID  uuid.UUID
	RolloutID  uuid.UUID
	WaveNumber int
}

func (s *Server) enqueueClusterRolloutJob(r *http.Request, tenantID uuid.UUID, opts clusterRolloutAdvanceOptions) (uuid.UUID, error) {
	payload := clusterRolloutAdvancePayload{
		ClusterID:  opts.ClusterID.String(),
		RolloutID:  opts.RolloutID.String(),
		TenantID:   tenantID.String(),
		WaveNumber: opts.WaveNumber,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal cluster.rollout.advance payload: %w", err)
	}

	job := &storage.Job{
		TenantID: tenantID,
		Type:     JobTypeClusterRolloutAdvance,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	event := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: "cluster.rollout.advance queued",
	}

	ctx := r.Context()
	created, err := s.store.CreateJob(ctx, job, event)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create cluster.rollout.advance job: %w", err)
	}

	if s.worker == nil {
		return uuid.Nil, errors.New("worker unavailable")
	}

	task := worker.Task{
		Name:         fmt.Sprintf("cluster-rollout-advance-%s", created.ID),
		Job:          s.buildClusterRolloutAdvanceJob(created.ID, tenantID, opts),
		MaxAttempts:  1,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, created.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), nil)
		return uuid.Nil, fmt.Errorf("enqueue cluster.rollout.advance task: %w", err)
	}
	return created.ID, nil
}

func newClusterRolloutWaveResponse(wv storage.ClusterRolloutWave) clusterRolloutWaveResponse {
	members := make([]string, 0, len(wv.MemberIDs))
	for _, m := range wv.MemberIDs {
		members = append(members, m.String())
	}
	resp := clusterRolloutWaveResponse{
		ID:         wv.ID.String(),
		WaveNumber: wv.WaveNumber,
		MemberIDs:  members,
		State:      wv.State,
		StartedAt:  formatTime(wv.StartedAt),
		GateResult: wv.GateResult,
	}
	if wv.CompletedAt != nil {
		formatted := formatTime(*wv.CompletedAt)
		resp.CompletedAt = &formatted
	}
	return resp
}

func validateRolloutHealthGate(gate map[string]any) error {
	if gate == nil {
		return nil
	}
	raw, ok := gate["type"]
	if !ok {
		return errors.New("health_gate.type is required")
	}
	typeStr, ok := raw.(string)
	if !ok {
		return errors.New("health_gate.type must be a string")
	}
	switch strings.ToLower(strings.TrimSpace(typeStr)) {
	case rolloutGateTypeHeartbeat, rolloutGateTypeCompliance, rolloutGateTypeHTTP:
		return nil
	default:
		return fmt.Errorf("unsupported health_gate.type %q", typeStr)
	}
}

// ─── Webhook emission ───────────────────────────────────────────────

// emitRolloutEvent dispatches a rollout webhook to every enabled subscriber.
// Deliveries happen in goroutines with bounded concurrency (matching the
// compliance-event emitter) so the caller doesn't block on HTTP.
func (s *Server) emitRolloutEvent(ctx context.Context, tenantID uuid.UUID, eventType string, payload map[string]any) {
	if s == nil || s.store == nil {
		return
	}

	var webhooks []storage.Webhook
	var err error
	if tenantID != uuid.Nil {
		webhooks, err = s.store.ListWebhooksByEvent(ctx, tenantID, eventType)
	} else {
		webhooks, err = s.store.GetEnabledWebhooksForEvent(ctx, eventType)
	}
	if err != nil {
		s.logger.Warn("list webhooks for rollout event",
			zap.String("event_type", eventType),
			zap.Error(err),
		)
		return
	}
	if len(webhooks) == 0 {
		return
	}

	enriched := make(map[string]any, len(payload)+2)
	for k, v := range payload {
		enriched[k] = v
	}
	enriched["event_type"] = eventType
	if _, ok := enriched["timestamp"]; !ok {
		enriched["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	}

	sem := make(chan struct{}, maxWebhookConcurrency)
	var wg sync.WaitGroup

	for _, wh := range webhooks {
		wg.Add(1)
		sem <- struct{}{}
		go func(hook storage.Webhook) {
			defer wg.Done()
			defer func() { <-sem }()
			s.deliverAndRecordRollout(ctx, &hook, eventType, enriched)
		}(wh)
	}
	wg.Wait()
}

func (s *Server) deliverAndRecordRollout(ctx context.Context, webhook *storage.Webhook, eventType string, payload map[string]any) {
	success, statusCode, responseBody, err := s.deliverWebhook(webhook, eventType, payload)

	deliveryStatus := "success"
	if !success {
		deliveryStatus = "failed"
	}

	delivery := storage.WebhookDelivery{
		ID:            uuid.New(),
		WebhookID:     webhook.ID,
		EventType:     eventType,
		Status:        deliveryStatus,
		AttemptNumber: 1,
		RequestBody:   payload,
		CreatedAt:     time.Now().UTC(),
	}
	if statusCode > 0 {
		delivery.HTTPStatusCode = sql.NullInt64{Int64: int64(statusCode), Valid: true}
	}
	if responseBody != "" {
		delivery.ResponseBody = sql.NullString{String: responseBody, Valid: true}
	}
	if err != nil {
		delivery.ErrorMessage = sql.NullString{String: err.Error(), Valid: true}
		s.logger.Warn("rollout webhook delivery failed",
			zap.String("webhook_id", webhook.ID.String()),
			zap.String("event_type", eventType),
			zap.Error(err),
		)
	}
	now := time.Now().UTC()
	delivery.DeliveredAt = sql.NullTime{Time: now, Valid: true}

	if recordErr := s.store.RecordWebhookDelivery(ctx, delivery); recordErr != nil {
		s.logger.Warn("record rollout webhook delivery",
			zap.String("webhook_id", webhook.ID.String()),
			zap.Error(recordErr),
		)
	}
}
