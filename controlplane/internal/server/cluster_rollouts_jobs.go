package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
)

// Default timings for the rollout gate check loop.
const (
	defaultGateStartDelay = 30 * time.Second
	defaultGateInterval   = 30 * time.Second
	defaultGateTimeout    = 10 * time.Minute
	defaultGateGrace      = 5 * time.Minute
	defaultHTTPGatePort   = 80
	defaultHTTPGatePath   = "/healthz"
)

// clusterRolloutAdvancePayload is persisted on the advance job record.
type clusterRolloutAdvancePayload struct {
	ClusterID  string `json:"cluster_id"`
	RolloutID  string `json:"rollout_id"`
	TenantID   string `json:"tenant_id"`
	WaveNumber int    `json:"wave_number"`
}

// clusterRolloutGateCheckPayload is persisted on the gate_check job record.
type clusterRolloutGateCheckPayload struct {
	ClusterID  string `json:"cluster_id"`
	RolloutID  string `json:"rollout_id"`
	TenantID   string `json:"tenant_id"`
	WaveID     string `json:"wave_id"`
	WaveNumber int    `json:"wave_number"`
	Attempt    int    `json:"attempt"`
}

// buildClusterRolloutAdvanceJob returns a worker.Task-compatible function that
// computes the next wave's members, runs adapter.Apply for each of them, records
// the wave row, and enqueues the post-wave gate_check.
//
// If the wave has already been recorded (idempotent re-run) the handler updates
// that wave's state and enqueues the gate check directly.
func (s *Server) buildClusterRolloutAdvanceJob(jobID uuid.UUID, tenantID uuid.UUID, opts clusterRolloutAdvanceOptions) func(context.Context) error {
	return func(ctx context.Context) error {
		_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "cluster.rollout.advance running", map[string]any{
			"started_at": time.Now(),
		})

		rollout, err := s.store.GetClusterRolloutByID(ctx, opts.RolloutID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("load rollout: %w", err))
		}
		if rollout == nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("rollout %s not found", opts.RolloutID))
		}

		switch rollout.State {
		case RolloutStateAborted, RolloutStateCompleted:
			return s.failClusterJob(ctx, jobID, fmt.Errorf("rollout is %s", rollout.State))
		}

		cluster, err := s.store.GetClusterByID(ctx, opts.ClusterID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("load cluster: %w", err))
		}
		if cluster == nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("cluster %s not found", opts.ClusterID))
		}

		members, err := s.store.ListClusterMembers(ctx, opts.ClusterID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("list members: %w", err))
		}

		// members are returned ordered by (role, position); our rollouts walk
		// the flat list so wave N gets members[N*waveSize : (N+1)*waveSize].
		waveSize := rollout.WaveSize
		if waveSize < 1 {
			waveSize = 1
		}

		start := opts.WaveNumber * waveSize
		if start >= len(members) {
			// Nothing left to roll — mark completed.
			completedState := RolloutStateCompleted
			if _, err := s.store.UpdateClusterRollout(ctx, rollout.ID, storage.UpdateClusterRolloutParams{State: &completedState}); err != nil {
				s.logger.Warn("mark rollout completed", zap.Error(err))
			}
			s.emitRolloutEvent(ctx, tenantID, EventClusterRolloutCompleted, map[string]any{
				"cluster_id":  opts.ClusterID.String(),
				"rollout_id":  opts.RolloutID.String(),
				"total_waves": opts.WaveNumber,
			})
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "rollout completed", map[string]any{
				"finished_at": time.Now(),
			})
			return nil
		}

		end := start + waveSize
		if end > len(members) {
			end = len(members)
		}
		waveMembers := members[start:end]
		memberIDs := make([]uuid.UUID, 0, len(waveMembers))
		for _, m := range waveMembers {
			memberIDs = append(memberIDs, m.NodeID)
		}

		// Flip rollout state to running, persist current_wave pointer.
		runningState := RolloutStateRunning
		currentWave := opts.WaveNumber
		if _, err := s.store.UpdateClusterRollout(ctx, rollout.ID, storage.UpdateClusterRolloutParams{
			State:       &runningState,
			CurrentWave: &currentWave,
		}); err != nil {
			s.logger.Warn("update rollout state=running", zap.Error(err))
		}

		// Re-entrant: if a wave with this number already exists, reuse it.
		// Lets resume handle the unhealthy-then-retry path without dup-insert.
		existing, err := s.store.GetClusterRolloutWaveByNumber(ctx, rollout.ID, opts.WaveNumber)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("lookup wave: %w", err))
		}

		var wave *storage.ClusterRolloutWave
		if existing != nil {
			// Reset the existing wave to running and clear any previous gate result.
			waveState := storage.ClusterRolloutWaveStateRunning
			emptyGate := map[string]any{}
			updated, err := s.store.UpdateClusterRolloutWave(ctx, existing.ID, storage.UpdateClusterRolloutWaveParams{
				State:      &waveState,
				GateResult: &emptyGate,
			})
			if err != nil {
				return s.failClusterJob(ctx, jobID, fmt.Errorf("reset wave: %w", err))
			}
			wave = updated
		} else {
			created, err := s.store.CreateClusterRolloutWave(ctx, storage.CreateClusterRolloutWaveParams{
				RolloutID:  rollout.ID,
				WaveNumber: opts.WaveNumber,
				MemberIDs:  memberIDs,
				State:      storage.ClusterRolloutWaveStateRunning,
				StartedAt:  time.Now().UTC(),
			})
			if err != nil {
				return s.failClusterJob(ctx, jobID, fmt.Errorf("create wave: %w", err))
			}
			wave = created
		}

		// Apply the new template version across the wave via the adapter. We
		// record failures but keep going — partial failure still surfaces via
		// the gate result.
		adapter := provisioning.NewAdapter(cluster.Provider, s.logger.Named("cluster-rollout"), nil)
		applyOpts := provisioning.Options{
			Provider: cluster.Provider,
			Template: rollout.TemplateVersionID.String(),
		}
		applyFailures := 0
		for _, m := range waveMembers {
			metadata := map[string]string{
				"cluster_id":          opts.ClusterID.String(),
				"tenant_id":           tenantID.String(),
				"rollout_id":          rollout.ID.String(),
				"wave_number":         fmt.Sprintf("%d", opts.WaveNumber),
				"role":                m.Role,
				"position":            fmt.Sprintf("%d", m.Position),
				"template_version_id": rollout.TemplateVersionID.String(),
			}
			if _, err := adapter.Apply(ctx, m.NodeID.String(), applyOpts, metadata); err != nil {
				applyFailures++
				s.logger.Warn("rollout adapter apply failed",
					zap.String("cluster_id", opts.ClusterID.String()),
					zap.String("rollout_id", rollout.ID.String()),
					zap.String("node_id", m.NodeID.String()),
					zap.Int("wave_number", opts.WaveNumber),
					zap.Error(err),
				)
			}
		}

		// Schedule the gate check after the configured start_delay (default 30s).
		gate := normalizeHealthGate(rollout.HealthGate)
		startDelay := parseDuration(gate["start_delay"], defaultGateStartDelay)

		gateJobID, enqueueErr := s.enqueueClusterRolloutGateCheck(ctx, clusterRolloutGateCheckPayload{
			ClusterID:  opts.ClusterID.String(),
			RolloutID:  rollout.ID.String(),
			TenantID:   tenantID.String(),
			WaveID:     wave.ID.String(),
			WaveNumber: opts.WaveNumber,
			Attempt:    1,
		}, startDelay)
		if enqueueErr != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("enqueue gate check: %w", enqueueErr))
		}

		_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, fmt.Sprintf("wave %d advanced (apply_failures=%d, gate_job=%s)", opts.WaveNumber, applyFailures, gateJobID.String()), map[string]any{
			"finished_at": time.Now(),
		})
		return nil
	}
}

// buildClusterRolloutGateCheckJob returns a worker.Task function that evaluates
// the rollout's health gate for the current wave. Pass → advance; fail past
// gate.timeout → halted wave + halted rollout; still pending → re-enqueue.
func (s *Server) buildClusterRolloutGateCheckJob(jobID uuid.UUID, payload clusterRolloutGateCheckPayload) func(context.Context) error {
	return func(ctx context.Context) error {
		_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "cluster.rollout.gate_check running", map[string]any{
			"started_at": time.Now(),
		})

		rolloutID, err := uuid.Parse(payload.RolloutID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("invalid rollout id: %w", err))
		}
		waveID, err := uuid.Parse(payload.WaveID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("invalid wave id: %w", err))
		}
		clusterID, err := uuid.Parse(payload.ClusterID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("invalid cluster id: %w", err))
		}
		// Rollout jobs are dispatched by the controlplane and always carry a
		// tenant. Reject the payload outright when it's missing so we can't
		// silently process a cluster rollout under "no tenant scope".
		if payload.TenantID == "" {
			return s.failClusterJob(ctx, jobID, errors.New("payload missing tenant_id"))
		}
		tenantID, err := uuid.Parse(payload.TenantID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("invalid tenant_id: %w", err))
		}

		rollout, err := s.store.GetClusterRolloutByID(ctx, rolloutID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("load rollout: %w", err))
		}
		if rollout == nil {
			return s.failClusterJob(ctx, jobID, errors.New("rollout not found"))
		}
		if rollout.State == RolloutStateAborted {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "rollout aborted; skipping gate", map[string]any{
				"finished_at": time.Now(),
			})
			return nil
		}

		wave, err := s.store.GetClusterRolloutWave(ctx, waveID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("load wave: %w", err))
		}
		if wave == nil {
			return s.failClusterJob(ctx, jobID, errors.New("wave not found"))
		}
		if wave.State != storage.ClusterRolloutWaveStateRunning {
			// Wave already resolved (probably by a parallel run). Short-circuit.
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, fmt.Sprintf("wave %d already %s", wave.WaveNumber, wave.State), map[string]any{
				"finished_at": time.Now(),
			})
			return nil
		}

		gate := normalizeHealthGate(rollout.HealthGate)
		result := s.evaluateRolloutGate(ctx, gate, wave)

		timeout := parseDuration(gate["timeout"], defaultGateTimeout)
		interval := parseDuration(gate["interval"], defaultGateInterval)

		switch {
		case result.passed:
			now := time.Now().UTC()
			waveState := storage.ClusterRolloutWaveStateHealthy
			if _, err := s.store.UpdateClusterRolloutWave(ctx, wave.ID, storage.UpdateClusterRolloutWaveParams{
				State:       &waveState,
				GateResult:  &result.details,
				CompletedAt: &now,
			}); err != nil {
				s.logger.Warn("mark wave healthy", zap.Error(err))
			}
			nextWave := wave.WaveNumber + 1
			if _, err := s.store.UpdateClusterRollout(ctx, rollout.ID, storage.UpdateClusterRolloutParams{CurrentWave: &nextWave}); err != nil {
				s.logger.Warn("advance rollout wave pointer", zap.Error(err))
			}
			s.emitRolloutEvent(ctx, tenantID, EventClusterRolloutWaveHealthy, map[string]any{
				"cluster_id":  clusterID.String(),
				"rollout_id":  rolloutID.String(),
				"wave_number": wave.WaveNumber,
				"member_ids":  uuidsToStrings(wave.MemberIDs),
			})

			// Enqueue next advance — run inline path: no delay. We build a job
			// record and task inline without a request object.
			advanceJobID, enqueueErr := s.enqueueClusterRolloutAdvanceFromCtx(ctx, tenantID, clusterID, rolloutID, nextWave)
			if enqueueErr != nil {
				return s.failClusterJob(ctx, jobID, fmt.Errorf("enqueue next advance: %w", enqueueErr))
			}
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, fmt.Sprintf("wave %d healthy; next advance=%s", wave.WaveNumber, advanceJobID.String()), map[string]any{
				"finished_at": time.Now(),
			})
			return nil

		case time.Since(wave.StartedAt) >= timeout:
			// Gate didn't pass in time — halt the rollout.
			waveState := storage.ClusterRolloutWaveStateUnhealthy
			now := time.Now().UTC()
			failDetails := map[string]any{}
			for k, v := range result.details {
				failDetails[k] = v
			}
			failDetails["reason"] = result.reason
			failDetails["timeout"] = timeout.String()
			if _, err := s.store.UpdateClusterRolloutWave(ctx, wave.ID, storage.UpdateClusterRolloutWaveParams{
				State:       &waveState,
				GateResult:  &failDetails,
				CompletedAt: &now,
			}); err != nil {
				s.logger.Warn("mark wave unhealthy", zap.Error(err))
			}
			haltedState := RolloutStateHalted
			if _, err := s.store.UpdateClusterRollout(ctx, rollout.ID, storage.UpdateClusterRolloutParams{State: &haltedState}); err != nil {
				s.logger.Warn("mark rollout halted", zap.Error(err))
			}
			s.emitRolloutEvent(ctx, tenantID, EventClusterRolloutWaveUnhealthy, map[string]any{
				"cluster_id":  clusterID.String(),
				"rollout_id":  rolloutID.String(),
				"wave_number": wave.WaveNumber,
				"reason":      result.reason,
			})
			s.emitRolloutEvent(ctx, tenantID, EventClusterRolloutHalted, map[string]any{
				"cluster_id":  clusterID.String(),
				"rollout_id":  rolloutID.String(),
				"wave_number": wave.WaveNumber,
				"reason":      result.reason,
			})
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, fmt.Sprintf("wave %d unhealthy: %s", wave.WaveNumber, result.reason), map[string]any{
				"finished_at": time.Now(),
			})
			return nil

		default:
			// Re-enqueue gate check after the interval.
			nextAttempt := payload.Attempt + 1
			nextPayload := payload
			nextPayload.Attempt = nextAttempt
			nextJobID, enqueueErr := s.enqueueClusterRolloutGateCheck(ctx, nextPayload, interval)
			if enqueueErr != nil {
				return s.failClusterJob(ctx, jobID, fmt.Errorf("re-enqueue gate check: %w", enqueueErr))
			}
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, fmt.Sprintf("gate pending (%s); next check=%s", result.reason, nextJobID.String()), map[string]any{
				"finished_at": time.Now(),
			})
			return nil
		}
	}
}

// enqueueClusterRolloutGateCheck creates a cluster.rollout.gate_check job +
// task pair with an optional scheduled delay. Uses the worker's EnqueueAt
// hook when available (Worktree C adds it), otherwise falls back to a
// lightweight sleeping goroutine that delegates to Enqueue.
func (s *Server) enqueueClusterRolloutGateCheck(ctx context.Context, payload clusterRolloutGateCheckPayload, delay time.Duration) (uuid.UUID, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal cluster.rollout.gate_check payload: %w", err)
	}
	var tenantID uuid.UUID
	if parsed, err := uuid.Parse(payload.TenantID); err == nil {
		tenantID = parsed
	}

	job := &storage.Job{
		TenantID: tenantID,
		Type:     JobTypeClusterRolloutGateCheck,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	event := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: fmt.Sprintf("cluster.rollout.gate_check queued (wave=%d attempt=%d)", payload.WaveNumber, payload.Attempt),
	}

	created, err := s.store.CreateJob(ctx, job, event)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create cluster.rollout.gate_check job: %w", err)
	}

	if s.worker == nil {
		return uuid.Nil, errors.New("worker unavailable")
	}
	task := worker.Task{
		Name:         fmt.Sprintf("cluster-rollout-gate-check-%s", created.ID),
		Job:          s.buildClusterRolloutGateCheckJob(created.ID, payload),
		MaxAttempts:  1,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}

	if err := s.enqueueAtOrDelay(task, delay); err != nil {
		_ = s.store.UpdateJobStatus(ctx, created.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), nil)
		return uuid.Nil, fmt.Errorf("enqueue cluster.rollout.gate_check task: %w", err)
	}
	return created.ID, nil
}

// enqueueClusterRolloutAdvanceFromCtx is the ctx-scoped twin of
// enqueueClusterRolloutAdvance used from inside a job handler where no
// *http.Request is available.
func (s *Server) enqueueClusterRolloutAdvanceFromCtx(ctx context.Context, tenantID, clusterID, rolloutID uuid.UUID, waveNumber int) (uuid.UUID, error) {
	payload := clusterRolloutAdvancePayload{
		ClusterID:  clusterID.String(),
		RolloutID:  rolloutID.String(),
		TenantID:   tenantID.String(),
		WaveNumber: waveNumber,
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
	created, err := s.store.CreateJob(ctx, job, event)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create cluster.rollout.advance job: %w", err)
	}
	if s.worker == nil {
		return uuid.Nil, errors.New("worker unavailable")
	}
	task := worker.Task{
		Name: fmt.Sprintf("cluster-rollout-advance-%s", created.ID),
		Job: s.buildClusterRolloutAdvanceJob(created.ID, tenantID, clusterRolloutAdvanceOptions{
			ClusterID:  clusterID,
			RolloutID:  rolloutID,
			WaveNumber: waveNumber,
		}),
		MaxAttempts:  1,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, created.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), nil)
		return uuid.Nil, fmt.Errorf("enqueue cluster.rollout.advance task: %w", err)
	}
	return created.ID, nil
}

// enqueueAtOrDelay schedules the task with the worker's delayed-enqueue hook
// if the worker exposes one, otherwise uses a short-lived goroutine that
// sleeps and then calls Enqueue.
//
// TEMPORARY: Worktree C is adding a proper EnqueueAt(task, processAt) method
// on the worker.Manager that uses asynq.ProcessAt under the hood. When that
// lands in `seigha`, swap the fallback branch for a direct call. For tests
// with zero delay we skip the goroutine so behaviour stays synchronous.
func (s *Server) enqueueAtOrDelay(task worker.Task, delay time.Duration) error {
	if s.worker == nil {
		return errors.New("worker unavailable")
	}
	type delayedEnqueuer interface {
		EnqueueAt(worker.Task, time.Time) error
	}
	if dq, ok := s.worker.(delayedEnqueuer); ok && delay > 0 {
		return dq.EnqueueAt(task, time.Now().Add(delay))
	}
	if delay <= 0 {
		return s.worker.Enqueue(task)
	}
	go func(t worker.Task, d time.Duration) {
		timer := time.NewTimer(d)
		defer timer.Stop()
		<-timer.C
		if err := s.worker.Enqueue(t); err != nil {
			s.logger.Warn("delayed enqueue failed",
				zap.String("task", t.Name),
				zap.Duration("delay", d),
				zap.Error(err),
			)
		}
	}(task, delay)
	return nil
}

// gateEvaluation captures a single gate_check evaluation.
type gateEvaluation struct {
	passed  bool
	reason  string
	details map[string]any
}

// evaluateRolloutGate dispatches to the right per-type evaluator.
func (s *Server) evaluateRolloutGate(ctx context.Context, gate map[string]any, wave *storage.ClusterRolloutWave) gateEvaluation {
	gateType := stringField(gate, "type", rolloutGateTypeHeartbeat)
	switch strings.ToLower(gateType) {
	case rolloutGateTypeHeartbeat:
		return s.evaluateHeartbeatGate(ctx, gate, wave)
	case rolloutGateTypeCompliance:
		return s.evaluateComplianceGate(ctx, gate, wave)
	case rolloutGateTypeHTTP:
		return s.evaluateHTTPGate(ctx, gate, wave)
	default:
		return gateEvaluation{passed: false, reason: fmt.Sprintf("unknown gate type %q", gateType), details: map[string]any{}}
	}
}

// evaluateHeartbeatGate passes when every wave member has a last_seen_at
// after the wave started and the delta is inside the grace window.
//
// relies on nodes.last_seen_at from migration 0028 (Worktree A)
func (s *Server) evaluateHeartbeatGate(ctx context.Context, gate map[string]any, wave *storage.ClusterRolloutWave) gateEvaluation {
	grace := parseDuration(gate["grace"], defaultGateGrace)
	pending := make([]string, 0, len(wave.MemberIDs))
	now := time.Now().UTC()

	for _, nodeID := range wave.MemberIDs {
		node, err := s.store.GetNode(ctx, nodeID)
		if err != nil {
			pending = append(pending, nodeID.String())
			s.logger.Warn("heartbeat gate: load node failed",
				zap.String("node_id", nodeID.String()),
				zap.Error(err),
			)
			continue
		}
		if node == nil {
			pending = append(pending, nodeID.String())
			continue
		}
		lastSeen := nodeLastSeenAt(node)
		if lastSeen == nil || lastSeen.IsZero() {
			// Treated as "never heartbeated" — unhealthy for the purposes of
			// this gate. Still pending until timeout.
			pending = append(pending, nodeID.String())
			continue
		}
		if lastSeen.Before(wave.StartedAt) {
			pending = append(pending, nodeID.String())
			continue
		}
		// Grace controls how stale a heartbeat may be relative to now.
		if now.Sub(*lastSeen) > grace {
			pending = append(pending, nodeID.String())
			continue
		}
	}

	details := map[string]any{
		"type":        rolloutGateTypeHeartbeat,
		"grace":       grace.String(),
		"checked_at":  now.Format(time.RFC3339),
		"total":       len(wave.MemberIDs),
		"pending":     pending,
		"pending_len": len(pending),
	}
	if len(pending) == 0 {
		return gateEvaluation{passed: true, reason: "all members heartbeated", details: details}
	}
	return gateEvaluation{passed: false, reason: fmt.Sprintf("%d/%d members pending heartbeat", len(pending), len(wave.MemberIDs)), details: details}
}

// evaluateComplianceGate passes when every listed rule has a verified=true
// compliance_result for every wave member since the wave started.
func (s *Server) evaluateComplianceGate(ctx context.Context, gate map[string]any, wave *storage.ClusterRolloutWave) gateEvaluation {
	rules := stringListField(gate, "rules")
	if len(rules) == 0 {
		return gateEvaluation{passed: false, reason: "compliance gate missing rules", details: map[string]any{
			"type":  rolloutGateTypeCompliance,
			"rules": rules,
		}}
	}

	pending := make(map[string][]string)
	now := time.Now().UTC()
	startedAt := wave.StartedAt
	for _, nodeID := range wave.MemberIDs {
		for _, rule := range rules {
			results, _, err := s.store.ListComplianceResultsFiltered(ctx, storage.ComplianceResultFilter{
				NodeID: nodeID,
				RuleID: rule,
				Since:  &startedAt,
			}, 0, 0)
			if err != nil {
				pending[rule] = append(pending[rule], nodeID.String())
				continue
			}
			passed := false
			for _, res := range results {
				if res.Verified {
					passed = true
					break
				}
			}
			if !passed {
				pending[rule] = append(pending[rule], nodeID.String())
			}
		}
	}

	details := map[string]any{
		"type":       rolloutGateTypeCompliance,
		"rules":      rules,
		"checked_at": now.Format(time.RFC3339),
		"pending":    pending,
	}
	if len(pending) == 0 {
		return gateEvaluation{passed: true, reason: "all members verified", details: details}
	}
	return gateEvaluation{passed: false, reason: fmt.Sprintf("%d rule(s) still pending", len(pending)), details: details}
}

// evaluateHTTPGate passes when every wave member's public_ip responds to the
// probe with the expected status.
func (s *Server) evaluateHTTPGate(ctx context.Context, gate map[string]any, wave *storage.ClusterRolloutWave) gateEvaluation {
	path := stringField(gate, "path", defaultHTTPGatePath)
	expect := intField(gate, "expect", http.StatusOK)
	port := intField(gate, "port", defaultHTTPGatePort)
	probeTimeout := parseDuration(gate["probe_timeout"], 5*time.Second)

	client := &http.Client{Timeout: probeTimeout}

	failures := make(map[string]string)
	checked := 0
	for _, nodeID := range wave.MemberIDs {
		node, err := s.store.GetNode(ctx, nodeID)
		if err != nil || node == nil {
			failures[nodeID.String()] = "node not found"
			continue
		}
		if !node.PublicIP.Valid || strings.TrimSpace(node.PublicIP.String) == "" {
			failures[nodeID.String()] = "node has no public_ip"
			continue
		}

		url := fmt.Sprintf("http://%s:%d%s", node.PublicIP.String, port, path)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			failures[nodeID.String()] = fmt.Sprintf("build probe: %v", err)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			failures[nodeID.String()] = fmt.Sprintf("probe failed: %v", err)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		checked++
		if resp.StatusCode != expect {
			failures[nodeID.String()] = fmt.Sprintf("status=%d want=%d", resp.StatusCode, expect)
		}
	}

	details := map[string]any{
		"type":       rolloutGateTypeHTTP,
		"path":       path,
		"port":       port,
		"expect":     expect,
		"checked":    checked,
		"total":      len(wave.MemberIDs),
		"failures":   failures,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	}
	if len(failures) == 0 {
		return gateEvaluation{passed: true, reason: "all probes passed", details: details}
	}
	return gateEvaluation{passed: false, reason: fmt.Sprintf("%d probe(s) failed", len(failures)), details: details}
}

// normalizeHealthGate returns a non-nil copy of the gate map. Callers then
// read keys without nil-guards scattered through the code.
func normalizeHealthGate(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw)+1)
	for k, v := range raw {
		out[k] = v
	}
	return out
}

// parseDuration pulls a duration value out of a JSON map. Accepts strings
// like "5m", floats (seconds), or ints (seconds). Explicit zero values are
// honoured so callers can disable delays in tests. Negative values or bad
// input fall back to the default.
func parseDuration(value any, fallback time.Duration) time.Duration {
	switch v := value.(type) {
	case nil:
		return fallback
	case string:
		if strings.TrimSpace(v) == "" {
			return fallback
		}
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil && d >= 0 {
			return d
		}
		return fallback
	case float64:
		if v >= 0 {
			return time.Duration(v * float64(time.Second))
		}
	case int:
		if v >= 0 {
			return time.Duration(v) * time.Second
		}
	case int64:
		if v >= 0 {
			return time.Duration(v) * time.Second
		}
	}
	return fallback
}

func stringField(raw map[string]any, key, fallback string) string {
	if raw == nil {
		return fallback
	}
	if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func intField(raw map[string]any, key string, fallback int) int {
	if raw == nil {
		return fallback
	}
	switch v := raw[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return fallback
}

func stringListField(raw map[string]any, key string) []string {
	if raw == nil {
		return nil
	}
	switch v := raw[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

func uuidsToStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// nodeLastSeenAt is a compatibility shim that returns the node's heartbeat
// timestamp. Worktree A is adding `storage.Node.LastSeenAt *time.Time` in
// migration 0028; until that column lands on `seigha`, this worktree has no
// field to read and every node effectively looks "never heartbeated", which
// the heartbeat health gate already treats as unhealthy.
//
// relies on nodes.last_seen_at from migration 0028 (Worktree A)
//
// When A merges, replace this body with:
//
//	return node.LastSeenAt
//
// Tests inject per-node timestamps via testLookupNodeLastSeen (see
// cluster_rollouts_jobs_test.go) so the heartbeat gate can be exercised now.
func nodeLastSeenAt(node *storage.Node) *time.Time {
	if node == nil {
		return nil
	}
	return testLookupNodeLastSeen(node.ID)
}
