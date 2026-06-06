package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
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

type rolloutWaveListItemResponse struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	Name       string  `json:"name"`
	Order      int     `json:"order"`
	Status     string  `json:"status"`
	NodeCount  int     `json:"node_count"`
	DoneCount  int     `json:"done_count"`
	StartedAt  *string `json:"started_at,omitempty"`
	FinishedAt *string `json:"finished_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
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

type updateRolloutWaveRequest struct {
	Status string `json:"status"`
}

// handleRolloutWavesCollection is a compatibility surface used by the
// Templates page for a fleet-wide rollout wave table.
func (s *Server) handleRolloutWavesCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Path != "/api/v1/rollout/waves" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	s.handleListRolloutWaves(w, r, principal)
}

func (s *Server) handleRolloutWaveSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rawID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/rollout/waves/"), "/")
	if rawID == "" || strings.Contains(rawID, "/") {
		http.NotFound(w, r)
		return
	}
	waveID, err := uuid.Parse(rawID)
	if err != nil {
		http.Error(w, "invalid wave id", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodPatch {
		w.Header().Set("Allow", http.MethodPatch)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	s.handleUpdateRolloutWave(w, r, waveID, principal)
}

type rolloutWaveAggregate struct {
	cluster storage.Cluster
	rollout storage.ClusterRollout
	wave    storage.ClusterRolloutWave
}

func (s *Server) handleListRolloutWaves(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	rawTenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if rawTenantID != "" {
		parsed, err := uuid.Parse(rawTenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		if !s.requireTenantAccess(w, r, principal, parsed, roleViewer, roleOperator, roleAdmin) {
			return
		}
		tenantID = parsed
	} else if !hasRole(principal, roleAdmin) {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}

	rows, err := s.collectRolloutWaves(r, tenantID)
	if err != nil {
		s.logger.Error("list rollout waves", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].wave.StartedAt.Equal(rows[j].wave.StartedAt) {
			return rows[i].wave.StartedAt.After(rows[j].wave.StartedAt)
		}
		if rows[i].rollout.CreatedAt != rows[j].rollout.CreatedAt {
			return rows[i].rollout.CreatedAt.After(rows[j].rollout.CreatedAt)
		}
		return rows[i].wave.WaveNumber < rows[j].wave.WaveNumber
	})

	total := len(rows)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	items := make([]rolloutWaveListItemResponse, 0, end-offset)
	for _, row := range rows[offset:end] {
		items = append(items, newRolloutWaveListItemResponse(row.cluster, row.rollout, row.wave))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[rolloutWaveListItemResponse]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

func (s *Server) collectRolloutWaves(r *http.Request, tenantID uuid.UUID) ([]rolloutWaveAggregate, error) {
	clusters, _, err := s.store.ListClusters(r.Context(), tenantID, 0, 0)
	if err != nil {
		return nil, err
	}
	rows := make([]rolloutWaveAggregate, 0)
	for _, cluster := range clusters {
		rollouts, _, err := s.store.ListClusterRollouts(r.Context(), cluster.ID, 0, 0)
		if err != nil {
			return nil, err
		}
		for _, rollout := range rollouts {
			waves, err := s.store.ListClusterRolloutWaves(r.Context(), rollout.ID)
			if err != nil {
				return nil, err
			}
			for _, wave := range waves {
				rows = append(rows, rolloutWaveAggregate{
					cluster: cluster,
					rollout: rollout,
					wave:    wave,
				})
			}
		}
	}
	return rows, nil
}

func (s *Server) handleUpdateRolloutWave(w http.ResponseWriter, r *http.Request, waveID uuid.UUID, principal *auth.Principal) {
	wave, err := s.store.GetClusterRolloutWave(r.Context(), waveID)
	if err != nil {
		s.logger.Error("get rollout wave", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if wave == nil {
		http.NotFound(w, r)
		return
	}
	rollout, err := s.store.GetClusterRolloutByID(r.Context(), wave.RolloutID)
	if err != nil {
		s.logger.Error("get rollout for wave", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if rollout == nil {
		http.NotFound(w, r)
		return
	}
	cluster, err := s.store.GetClusterByID(r.Context(), rollout.ClusterID)
	if err != nil {
		s.logger.Error("get cluster for rollout wave", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, cluster.TenantID, roleOperator, roleAdmin) {
		return
	}

	var req updateRolloutWaveRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	state, rolloutState, err := rolloutWaveUpdateStates(req.Status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params := storage.UpdateClusterRolloutWaveParams{State: &state}
	if state == storage.ClusterRolloutWaveStateAborted || state == storage.ClusterRolloutWaveStateUnhealthy {
		now := time.Now().UTC()
		params.CompletedAt = &now
	}
	updatedWave, err := s.store.UpdateClusterRolloutWave(r.Context(), waveID, params)
	if err != nil {
		s.logger.Error("update rollout wave", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if updatedWave == nil {
		http.NotFound(w, r)
		return
	}
	if rolloutState != "" {
		if updatedRollout, err := s.store.UpdateClusterRollout(r.Context(), rollout.ID, storage.UpdateClusterRolloutParams{State: &rolloutState}); err != nil {
			s.logger.Error("update rollout for wave action", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if updatedRollout != nil {
			rollout = updatedRollout
		}
	}

	s.recordAudit(r.Context(), principal, cluster.TenantID, "cluster.rollout.wave.update", "cluster_rollout_wave", waveID.String(), map[string]any{
		"cluster_id":  cluster.ID.String(),
		"rollout_id":  rollout.ID.String(),
		"wave_number": updatedWave.WaveNumber,
		"status":      req.Status,
	})

	writeJSON(w, http.StatusOK, newRolloutWaveListItemResponse(*cluster, *rollout, *updatedWave))
}

func rolloutWaveUpdateStates(status string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return storage.ClusterRolloutWaveStateRunning, RolloutStateRunning, nil
	case "paused":
		return storage.ClusterRolloutWaveStateUnhealthy, RolloutStateHalted, nil
	case "aborted":
		return storage.ClusterRolloutWaveStateAborted, RolloutStateAborted, nil
	default:
		return "", "", fmt.Errorf("unsupported wave status %q", status)
	}
}

func newRolloutWaveListItemResponse(cluster storage.Cluster, rollout storage.ClusterRollout, wave storage.ClusterRolloutWave) rolloutWaveListItemResponse {
	startedAt := formatRolloutOptionalTime(wave.StartedAt)
	finishedAt := formatRolloutOptionalTimePtr(wave.CompletedAt)
	updatedAt := wave.StartedAt
	if wave.CompletedAt != nil {
		updatedAt = *wave.CompletedAt
	}
	status := rolloutWavePresentationStatus(wave.State)
	nodeCount := len(wave.MemberIDs)
	doneCount := 0
	if status == "done" {
		doneCount = nodeCount
	}
	return rolloutWaveListItemResponse{
		ID:         wave.ID.String(),
		TenantID:   cluster.TenantID.String(),
		Name:       fmt.Sprintf("%s wave %d", cluster.Name, wave.WaveNumber+1),
		Order:      wave.WaveNumber + 1,
		Status:     status,
		NodeCount:  nodeCount,
		DoneCount:  doneCount,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		CreatedAt:  formatTime(wave.StartedAt),
		UpdatedAt:  formatTime(updatedAt),
	}
}

func rolloutWavePresentationStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case storage.ClusterRolloutWaveStateRunning:
		return "running"
	case storage.ClusterRolloutWaveStateHealthy, "done", RolloutStateCompleted:
		return "done"
	case storage.ClusterRolloutWaveStateUnhealthy, "paused", RolloutStateHalted:
		return "paused"
	case storage.ClusterRolloutWaveStateAborted:
		return "aborted"
	default:
		return "pending"
	}
}

func formatRolloutOptionalTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	formatted := formatTime(t)
	return &formatted
}

func formatRolloutOptionalTimePtr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	formatted := formatTime(*t)
	return &formatted
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
			principal, ok := s.authorize(w, r, roleViewer)
			if !ok {
				return
			}
			s.handleGetClusterRollout(w, r, clusterID, rolloutID, principal)
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
	if !s.requireTenantAccess(w, r, principal, cluster.TenantID, roleAdmin) {
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
	if !s.validateProvisioningTemplateVersionForProvider(w, r, templateVersionID, cluster.TenantID, cluster.Provider) {
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

func (s *Server) handleGetClusterRollout(w http.ResponseWriter, r *http.Request, clusterID, rolloutID uuid.UUID, principal *auth.Principal) {
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
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster for rollout", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, cluster.TenantID, roleViewer, roleOperator, roleAdmin) {
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
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster for rollout abort", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, cluster.TenantID, roleAdmin) {
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

	tenantID := cluster.TenantID
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
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster for rollout resume", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, cluster.TenantID, roleAdmin) {
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

	tenantID := cluster.TenantID

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
