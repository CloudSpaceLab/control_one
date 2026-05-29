package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

// Remediation safety event names. These are emitted as webhooks when the
// dispatch-path safety gates halt or defer a remediation.
const (
	EventRemediationManualOnlySkipped    = "remediation.manual_only_skipped"
	EventRemediationIsolationSkipped     = "remediation.isolation_skipped"
	EventRemediationChangeWindowDeferred = "remediation.change_window_deferred"
	EventRemediationCircuitBreakerTrip   = "remediation.circuit_breaker_tripped"
	EventRemediationCircuitBreakerAcked  = "remediation.circuit_breaker_acked"
	EventRemediationApprovalRequested    = "remediation.approval_requested"
	EventRemediationApprovalApproved     = "remediation.approval_approved"
	EventRemediationApprovalDenied       = "remediation.approval_denied"
	EventRemediationApprovalExpired      = "remediation.approval_expired"
)

// approvalTTL is how long a pending approval stays actionable before the
// reaper flips it to expired.
const approvalTTL = 24 * time.Hour

// JobTypeComplianceVerify re-evaluates a single rule after a remediation has
// completed so the server can mark the compliance_result verified or kick off
// a rollback.
const JobTypeComplianceVerify = "compliance.verify"

// JobTypeRemediationRollback executes the rollback body of a remediation
// script when post-remediation verification fails.
const JobTypeRemediationRollback = "remediation.rollback"

// triggerAutoRemediation creates a remediation job for a failed compliance result
// if auto-remediation is enabled and a matching script exists for the rule.
// Four safety gates run immediately before the worker enqueue:
//  1. Opt-out label: `nodes.labels["remediation"] == "manual-only"` short-circuits.
//  2. Change window: outside the tenant's configured windows the job is deferred
//     to the next window (critical severity can override when configured).
//  3. Circuit breaker: unacked trips short-circuit; fail-rate threshold breaches
//     trip a fresh breaker and short-circuit.
//  4. Approval gate: severities at or above min_approval_severity create a
//     pending approval row and emit a webhook instead of enqueueing.
//
// Returns the job ID if a job was created, nil otherwise.
func (s *Server) triggerAutoRemediation(ctx context.Context, tenantID, nodeID uuid.UUID, result compliance.Result, autoRemediate bool) *uuid.UUID {
	if !autoRemediate {
		return nil
	}

	if result.Passed {
		return nil
	}

	if s.store == nil {
		s.logger.Warn("auto-remediation skipped: store unavailable")
		return nil
	}

	if s.worker == nil {
		s.logger.Warn("auto-remediation skipped: worker queue unavailable")
		return nil
	}

	// Look up a remediation script by rule_id. Pass empty platform to match any/all.
	script, err := s.store.GetRemediationScript(ctx, result.RuleID, "")
	if err != nil {
		s.logger.Error("lookup remediation script for auto-remediation",
			zap.String("rule_id", result.RuleID),
			zap.Error(err),
		)
		return nil
	}
	if script == nil {
		s.logger.Debug("no remediation script found for rule",
			zap.String("rule_id", result.RuleID),
		)
		return nil
	}

	if !script.Enabled {
		s.logger.Debug("remediation script disabled",
			zap.String("rule_id", result.RuleID),
			zap.String("script_id", script.ID.String()),
		)
		return nil
	}
	now := time.Now().UTC()

	// ---------------------------------------------------------------------
	// Gate 1: opt-out label. Reads from node.Labels which is populated by
	// migration 0028 (Worktree A). If A has not merged yet Labels is nil and
	// this gate is effectively a no-op.
	// relies on nodes.labels JSONB column from migration 0028 (Worktree A)
	// ---------------------------------------------------------------------
	if nodeID != uuid.Nil {
		node, err := s.store.GetNode(ctx, nodeID)
		if err != nil {
			s.logger.Warn("load node for safety gates",
				zap.String("node_id", nodeID.String()),
				zap.Error(err),
			)
		} else if node != nil && node.Labels != nil {
			posture := nodeIsolationPostureFromNode(*node, now)
			if posture.Active && posture.Mode == isolationModeAirgapped {
				s.logger.Info("auto-remediation skipped: node is airgapped",
					zap.String("node_id", nodeID.String()),
					zap.String("rule_id", result.RuleID),
				)
				s.emitRemediationSafetyEvent(ctx, tenantID, EventRemediationIsolationSkipped, map[string]any{
					"tenant_id": tenantID.String(),
					"node_id":   nodeID.String(),
					"rule_id":   result.RuleID,
					"severity":  result.Severity,
					"mode":      posture.Mode,
					"reason":    "node is airgapped",
				})
				return nil
			}
			if posture.Active && posture.Mode == isolationModeWhitelist && !stringSliceContainsFold(posture.AllowedApplications, "remediation") {
				s.logger.Info("auto-remediation skipped: node is whitelist-only",
					zap.String("node_id", nodeID.String()),
					zap.String("rule_id", result.RuleID),
				)
				s.emitRemediationSafetyEvent(ctx, tenantID, EventRemediationIsolationSkipped, map[string]any{
					"tenant_id": tenantID.String(),
					"node_id":   nodeID.String(),
					"rule_id":   result.RuleID,
					"severity":  result.Severity,
					"mode":      posture.Mode,
					"reason":    "node is whitelist-only; remediation application is not allowlisted",
				})
				return nil
			}
			if val, ok := node.Labels["remediation"]; ok {
				if str, ok := val.(string); ok && strings.EqualFold(strings.TrimSpace(str), "manual-only") {
					s.logger.Info("auto-remediation skipped: node labelled manual-only",
						zap.String("node_id", nodeID.String()),
						zap.String("rule_id", result.RuleID),
					)
					s.emitRemediationSafetyEvent(ctx, tenantID, EventRemediationManualOnlySkipped, map[string]any{
						"tenant_id": tenantID.String(),
						"node_id":   nodeID.String(),
						"rule_id":   result.RuleID,
						"severity":  result.Severity,
						"reason":    "node labelled remediation=manual-only",
					})
					return nil
				}
			}
		}
	}

	// Load tenant safety config (synthesises defaults when tenant has no row).
	cfg, err := s.store.GetTenantRemediationConfig(ctx, tenantID)
	if err != nil {
		s.logger.Warn("load tenant remediation config (using defaults)",
			zap.String("tenant_id", tenantID.String()),
			zap.Error(err),
		)
		defaults := storage.DefaultTenantRemediationConfig(tenantID)
		cfg = &defaults
	}
	if cfg == nil {
		defaults := storage.DefaultTenantRemediationConfig(tenantID)
		cfg = &defaults
	}

	// ---------------------------------------------------------------------
	// Gate 2: change window. Outside the configured windows the job is
	// deferred to the next opening unless critical_override + severity=critical
	// lets it run immediately.
	// ---------------------------------------------------------------------
	// enqueueAt remains zero (immediate) unless we explicitly defer below.
	// Storing zero lets dispatchRemediationTask call worker.Enqueue() instead
	// of EnqueueAt(now) — semantically "run as soon as a worker picks it up"
	// rather than "schedule for this exact instant".
	var enqueueAt time.Time
	severity := strings.ToLower(strings.TrimSpace(result.Severity))
	if !storage.IsInsideChangeWindow(cfg.ChangeWindows, now) {
		if cfg.CriticalOverride && severity == "critical" {
			s.logger.Info("auto-remediation: critical severity overrides change window",
				zap.String("tenant_id", tenantID.String()),
				zap.String("rule_id", result.RuleID),
			)
		} else {
			next := storage.NextChangeWindowStart(cfg.ChangeWindows, now)
			if !next.IsZero() && next.After(now) {
				enqueueAt = next
				s.logger.Info("auto-remediation deferred to next change window",
					zap.String("tenant_id", tenantID.String()),
					zap.String("rule_id", result.RuleID),
					zap.Time("process_at", enqueueAt),
				)
				s.emitRemediationSafetyEvent(ctx, tenantID, EventRemediationChangeWindowDeferred, map[string]any{
					"tenant_id":  tenantID.String(),
					"node_id":    nodeID.String(),
					"rule_id":    result.RuleID,
					"severity":   severity,
					"process_at": enqueueAt.Format(time.RFC3339),
				})
			}
		}
	}

	// ---------------------------------------------------------------------
	// Gate 3: circuit breaker. Either an unacked trip or a fail-rate breach
	// short-circuits. A fresh breach trips the breaker so subsequent triggers
	// short-circuit without re-computing the fail rate.
	// ---------------------------------------------------------------------
	if breaker, err := s.store.GetCircuitBreakerState(ctx, tenantID, result.RuleID); err != nil {
		s.logger.Warn("load circuit breaker state",
			zap.String("tenant_id", tenantID.String()),
			zap.String("rule_id", result.RuleID),
			zap.Error(err),
		)
	} else if breaker != nil && breaker.AckedAt == nil {
		s.logger.Info("auto-remediation short-circuited: breaker tripped",
			zap.String("tenant_id", tenantID.String()),
			zap.String("rule_id", result.RuleID),
			zap.Time("tripped_at", breaker.TrippedAt),
		)
		s.emitRemediationSafetyEvent(ctx, tenantID, EventRemediationCircuitBreakerTrip, map[string]any{
			"tenant_id":     tenantID.String(),
			"rule_id":       result.RuleID,
			"reason":        breaker.TrippedReason,
			"short_circuit": true,
		})
		return nil
	}

	if cfg.CircuitBreakerWindowMin > 0 && cfg.CircuitBreakerMinSamples > 0 {
		window := time.Duration(cfg.CircuitBreakerWindowMin) * time.Minute
		rate, err := s.store.RemediationFailRate(ctx, tenantID, result.RuleID, window)
		if err != nil {
			s.logger.Warn("compute remediation fail rate",
				zap.String("tenant_id", tenantID.String()),
				zap.String("rule_id", result.RuleID),
				zap.Error(err),
			)
		} else if rate != nil &&
			rate.Samples >= cfg.CircuitBreakerMinSamples &&
			rate.Pct >= cfg.CircuitBreakerFailPct {

			reason := fmt.Sprintf("fail rate %d%% over last %dm (samples=%d, failed=%d, threshold=%d%%)",
				rate.Pct, cfg.CircuitBreakerWindowMin, rate.Samples, rate.Failed, cfg.CircuitBreakerFailPct)
			if _, tripErr := s.store.TripCircuitBreaker(ctx, tenantID, result.RuleID, reason); tripErr != nil {
				s.logger.Error("trip circuit breaker",
					zap.String("tenant_id", tenantID.String()),
					zap.String("rule_id", result.RuleID),
					zap.Error(tripErr),
				)
			}
			s.logger.Warn("auto-remediation tripped circuit breaker",
				zap.String("tenant_id", tenantID.String()),
				zap.String("rule_id", result.RuleID),
				zap.Int("fail_pct", rate.Pct),
				zap.Int("samples", rate.Samples),
			)
			s.emitRemediationSafetyEvent(ctx, tenantID, EventRemediationCircuitBreakerTrip, map[string]any{
				"tenant_id": tenantID.String(),
				"rule_id":   result.RuleID,
				"reason":    reason,
				"fail_pct":  rate.Pct,
				"samples":   rate.Samples,
				"failed":    rate.Failed,
			})
			return nil
		}
	}

	// ---------------------------------------------------------------------
	// Gate 4: approval gate. At or above the configured min severity the job
	// is held in remediation_approvals until an operator approves it; the
	// approval endpoint re-dispatches via dispatchRemediationTask below.
	// ---------------------------------------------------------------------
	if storage.SeverityAtLeast(severity, cfg.MinApprovalSeverity) {
		descriptor := remediationDescriptorFromScript(*script)
		taskPayload := map[string]any{
			"script_id":       script.ID.String(),
			"script_checksum": remediationScriptArtifactChecksum(*script),
			"script_version":  script.Version,
			"rule_id":         result.RuleID,
			"tenant_id":       tenantID.String(),
			"node_id":         nodeID.String(),
			"severity":        severity,
			"platform":        script.Platform,
			"script_type":     script.ScriptType,
			"auto_triggered":  true,
			"action_type":     descriptor.ActionType,
			"safety_class":    descriptor.SafetyClass,
		}
		payloadBytes, err := json.Marshal(taskPayload)
		if err != nil {
			s.logger.Error("marshal approval task payload",
				zap.String("rule_id", result.RuleID),
				zap.Error(err),
			)
			return nil
		}
		approval, err := s.store.CreateRemediationApproval(ctx, storage.CreateRemediationApprovalParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			RuleID:      result.RuleID,
			ScriptID:    script.ID,
			Severity:    severity,
			TaskPayload: payloadBytes,
			ExpiresAt:   now.Add(approvalTTL),
		})
		if err != nil {
			s.logger.Error("create remediation approval",
				zap.String("rule_id", result.RuleID),
				zap.Error(err),
			)
			return nil
		}
		s.logger.Info("auto-remediation awaiting approval",
			zap.String("approval_id", approval.ID.String()),
			zap.String("tenant_id", tenantID.String()),
			zap.String("rule_id", result.RuleID),
			zap.String("severity", severity),
		)
		s.emitRemediationSafetyEvent(ctx, tenantID, EventRemediationApprovalRequested, map[string]any{
			"approval_id": approval.ID.String(),
			"tenant_id":   tenantID.String(),
			"node_id":     nodeID.String(),
			"rule_id":     result.RuleID,
			"severity":    severity,
			"expires_at":  approval.ExpiresAt.Format(time.RFC3339),
		})
		return nil
	}

	// All four gates passed — fall through to the Sprint 1 lease + tenant cap
	// path and enqueue (possibly deferred via enqueueAt).
	return s.dispatchRemediationTask(ctx, dispatchRemediationTaskParams{
		TenantID:  tenantID,
		NodeID:    nodeID,
		RuleID:    result.RuleID,
		Script:    script,
		EnqueueAt: enqueueAt,
	})
}

// dispatchRemediationTaskParams bundles the inputs to dispatchRemediationTask.
// The struct keeps the argument list honest when the approval-approve API
// re-dispatches after a manual override.
type dispatchRemediationTaskParams struct {
	TenantID  uuid.UUID
	NodeID    uuid.UUID
	RuleID    string
	Script    *storage.RemediationScript
	EnqueueAt time.Time
}

// dispatchRemediationTask performs the Sprint 1 lease + tenant cap checks,
// creates the storage.Job row, and enqueues the worker task. It runs AFTER the
// four safety gates in triggerAutoRemediation; callers that bypass the gates
// (the approval-approve handler) invoke this directly.
func (s *Server) dispatchRemediationTask(ctx context.Context, p dispatchRemediationTaskParams) *uuid.UUID {
	if s.store == nil || s.worker == nil {
		s.logger.Warn("dispatch remediation skipped: store or worker unavailable")
		return nil
	}
	if p.Script == nil {
		s.logger.Error("dispatch remediation called with nil script")
		return nil
	}

	// Per-tenant concurrency cap. Check before we cut a job row so we don't
	// persist orphan jobs whenever the cap is hit.
	if cap := s.remediationTenantCap(); cap > 0 && p.TenantID != uuid.Nil {
		inflight, err := s.store.CountTenantLeases(ctx, p.TenantID)
		if err != nil {
			s.logger.Warn("count tenant remediation leases",
				zap.String("tenant_id", p.TenantID.String()),
				zap.Error(err),
			)
		} else if inflight >= cap {
			s.logger.Info("auto-remediation deferred: tenant cap reached",
				zap.String("tenant_id", p.TenantID.String()),
				zap.Int("in_flight", inflight),
				zap.Int("cap", cap),
			)
			return nil
		}
	}

	jobPayload := map[string]any{
		"script_id":      p.Script.ID.String(),
		"rule_id":        p.RuleID,
		"node_id":        p.NodeID.String(),
		"platform":       p.Script.Platform,
		"script_type":    p.Script.ScriptType,
		"script_content": p.Script.ScriptContent,
		"auto_triggered": true,
	}
	jobID := uuid.New()
	if actionPlanID := s.createRemediationScriptActionPlan(ctx, p.TenantID, p.NodeID, jobID, p.RuleID, p.Script, "execute", true); actionPlanID != uuid.Nil {
		jobPayload["action_plan_id"] = actionPlanID.String()
	}

	payloadBytes, err := json.Marshal(jobPayload)
	if err != nil {
		s.logger.Error("marshal auto-remediation job payload",
			zap.String("rule_id", p.RuleID),
			zap.Error(err),
		)
		return nil
	}

	job := &storage.Job{
		ID:       jobID,
		Type:     "remediation.execute",
		TenantID: p.TenantID,
		Payload:  payloadBytes,
		Status:   storage.JobStatusQueued,
	}
	job, err = s.store.CreateJob(ctx, job, nil)
	if err != nil {
		s.logger.Error("create auto-remediation job",
			zap.String("rule_id", p.RuleID),
			zap.Error(err),
		)
		return nil
	}

	// Acquire the per-node lease. If another remediation is already in flight
	// on this node we skip rather than stampede it.
	if p.NodeID != uuid.Nil && p.TenantID != uuid.Nil {
		if _, err := s.store.AcquireRemediationLease(ctx, p.TenantID, p.NodeID, job.ID, s.remediationLeaseTTL()); err != nil {
			if errors.Is(err, storage.ErrLeaseHeld) {
				s.logger.Info("auto-remediation skipped: lease already held",
					zap.String("node_id", p.NodeID.String()),
					zap.String("rule_id", p.RuleID),
				)
			} else {
				s.logger.Error("acquire remediation lease",
					zap.String("node_id", p.NodeID.String()),
					zap.Error(err),
				)
			}
			_ = s.store.UpdateJobStatus(ctx, job.ID, storage.JobStatusCancelled, "skipped: remediation lease held", nil)
			return nil
		}
	}

	chained := s.chainRemediationExecution(job.ID, p.Script.ID, p.NodeID, p.RuleID, p.Script, true)

	task := worker.Task{
		Name:         fmt.Sprintf("auto-remediation-%s", job.ID),
		Job:          chained,
		MaxAttempts:  3,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}

	enqueueErr := s.worker.EnqueueAt(task, p.EnqueueAt)
	if enqueueErr != nil {
		s.logger.Error("enqueue auto-remediation job",
			zap.String("job_id", job.ID.String()),
			zap.String("rule_id", p.RuleID),
			zap.Error(enqueueErr),
		)
		_ = s.store.UpdateJobStatus(ctx, job.ID, storage.JobStatusFailed, "failed to enqueue auto-remediation job", nil)
		if p.NodeID != uuid.Nil {
			_ = s.store.ReleaseRemediationLease(ctx, p.NodeID)
		}
		return nil
	}

	s.logger.Info("auto-remediation triggered",
		zap.String("job_id", job.ID.String()),
		zap.String("rule_id", p.RuleID),
		zap.String("node_id", p.NodeID.String()),
		zap.String("script_id", p.Script.ID.String()),
		zap.Time("process_at", p.EnqueueAt),
	)

	return &job.ID
}

// emitRemediationSafetyEvent fires a webhook event for tenant-scoped
// subscribers. Deliveries run in a detached goroutine so the dispatch path is
// never blocked on slow downstreams. The function is intentionally best-effort:
// storage errors are logged and swallowed.
func (s *Server) emitRemediationSafetyEvent(ctx context.Context, tenantID uuid.UUID, eventType string, payload map[string]any) {
	if s.store == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if _, exists := payload["event_type"]; !exists {
		payload["event_type"] = eventType
	}
	if _, exists := payload["timestamp"]; !exists {
		payload["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	}

	// Run delivery in a detached goroutine with a fresh context so slow
	// webhook endpoints never wedge the compliance request path.
	go func(tenantID uuid.UUID, eventType string, payload map[string]any) {
		deliverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		webhooks, err := s.store.ListWebhooksByEvent(deliverCtx, tenantID, eventType)
		if err != nil {
			s.logger.Warn("list webhooks for remediation event",
				zap.String("event_type", eventType),
				zap.Error(err),
			)
			return
		}
		for _, wh := range webhooks {
			func(wh storage.Webhook) {
				success, statusCode, responseBody, err := s.deliverWebhook(&wh, eventType, payload)
				deliveryStatus := "success"
				if !success {
					deliveryStatus = "failed"
				}
				delivery := storage.WebhookDelivery{
					ID:            uuid.New(),
					WebhookID:     wh.ID,
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
					s.logger.Warn("remediation webhook delivery failed",
						zap.String("webhook_id", wh.ID.String()),
						zap.String("event_type", eventType),
						zap.Error(err),
					)
				}
				delivery.DeliveredAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
				if recErr := s.store.RecordWebhookDelivery(deliverCtx, delivery); recErr != nil {
					s.logger.Warn("record remediation webhook delivery",
						zap.String("webhook_id", wh.ID.String()),
						zap.Error(recErr),
					)
				}
			}(wh)
		}
	}(tenantID, eventType, payload)
	_ = ctx
}

// chainRemediationExecution wraps buildRemediationJobExecution with the
// post-remediation verify/rollback chain and lease release. The wrapped job
// runs the normal remediation; on success it enqueues a verify job; on
// failure with rollback content it enqueues a rollback job. In all cases the
// per-node lease is released so the next scan can act on the node.
func (s *Server) chainRemediationExecution(jobID, scriptID, nodeID uuid.UUID, ruleID string, script *storage.RemediationScript, autoTriggered bool) func(context.Context) error {
	inner := s.buildRemediationJobExecution(jobID, scriptID, nodeID, ruleID, script)

	return func(ctx context.Context) error {
		execErr := inner(ctx)

		tenantID := uuid.Nil
		if j, err := s.store.GetJob(ctx, jobID); err == nil && j != nil {
			tenantID = j.TenantID
		}

		// Always release the lease regardless of success or failure. The next
		// scan for this node can then kick off a fresh remediation.
		if nodeID != uuid.Nil {
			if err := s.store.ReleaseRemediationLease(ctx, nodeID); err != nil {
				s.logger.Warn("release remediation lease",
					zap.String("node_id", nodeID.String()),
					zap.Error(err),
				)
			}
		}

		// Only auto-triggered remediations (from a failing compliance result)
		// chain into verify/rollback. Manually-invoked remediations return the
		// original error unchanged so operators can see what they asked for.
		if !autoTriggered {
			return execErr
		}

		if execErr == nil {
			if err := s.enqueueComplianceVerify(ctx, tenantID, nodeID, ruleID, jobID, script); err != nil {
				s.logger.Warn("enqueue compliance verify",
					zap.String("rule_id", ruleID),
					zap.String("node_id", nodeID.String()),
					zap.Error(err),
				)
			}
			return nil
		}

		// Remediation execution failed. If the script has a rollback body
		// queue it now; otherwise propagate the error.
		if script != nil && script.RollbackContent.Valid && script.RollbackContent.String != "" {
			if err := s.enqueueRemediationRollback(ctx, tenantID, nodeID, ruleID, jobID, script); err != nil {
				s.logger.Warn("enqueue remediation rollback after failure",
					zap.String("rule_id", ruleID),
					zap.String("node_id", nodeID.String()),
					zap.Error(err),
				)
			}
		}

		return execErr
	}
}

// enqueueComplianceVerify creates a compliance.verify job that re-evaluates
// the single rule that originally failed. The job handler updates
// compliance_results.verified on pass or kicks off a rollback on fail.
func (s *Server) enqueueComplianceVerify(ctx context.Context, tenantID, nodeID uuid.UUID, ruleID string, remediationJobID uuid.UUID, script *storage.RemediationScript) error {
	if s.store == nil || s.worker == nil {
		return errors.New("store or worker unavailable")
	}

	payload := map[string]any{
		"rule_id":            ruleID,
		"node_id":            nodeID.String(),
		"remediation_job_id": remediationJobID.String(),
	}
	if script != nil {
		payload["script_id"] = script.ID.String()
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal verify payload: %w", err)
	}

	job := &storage.Job{
		Type:     JobTypeComplianceVerify,
		TenantID: tenantID,
		Payload:  payloadBytes,
		Status:   storage.JobStatusQueued,
	}
	job, err = s.store.CreateJob(ctx, job, nil)
	if err != nil {
		return fmt.Errorf("create verify job: %w", err)
	}

	// Best-effort: attach the verification_job_id to the latest compliance
	// result for this rule/node so the API surface tracks it before the job
	// finishes.
	if nodeID != uuid.Nil {
		if result, err := s.store.GetLatestComplianceResultForRule(ctx, nodeID, ruleID); err == nil && result != nil {
			verifJob := job.ID
			if err := s.store.UpdateComplianceResultVerification(ctx, result.ID, false, &verifJob); err != nil {
				s.logger.Warn("set pending verification_job_id",
					zap.String("result_id", result.ID.String()),
					zap.Error(err),
				)
			}
		}
	}

	handler := s.buildComplianceVerifyJob(job.ID, tenantID, nodeID, ruleID, script)
	task := worker.Task{
		Name:         fmt.Sprintf("compliance-verify-%s", job.ID),
		Job:          handler,
		MaxAttempts:  3,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, job.ID, storage.JobStatusFailed, "failed to enqueue verify job", nil)
		return fmt.Errorf("enqueue verify: %w", err)
	}

	s.logger.Info("compliance verify enqueued",
		zap.String("job_id", job.ID.String()),
		zap.String("rule_id", ruleID),
		zap.String("node_id", nodeID.String()),
	)
	return nil
}

// buildComplianceVerifyJob returns the job.Job closure that performs the
// post-remediation re-evaluation. It reuses evaluateWithPolicies for the
// single rule and flips compliance_results.verified accordingly.
func (s *Server) buildComplianceVerifyJob(jobID, tenantID, nodeID uuid.UUID, ruleID string, script *storage.RemediationScript) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "verifying remediation", nil); err != nil {
			return fmt.Errorf("update verify job status: %w", err)
		}

		results, evalErr := s.verifyRuleOnNode(ctx, tenantID, nodeID, ruleID)

		// Locate the original compliance_result row so we can flip verified.
		existing, err := s.store.GetLatestComplianceResultForRule(ctx, nodeID, ruleID)
		if err != nil {
			s.logger.Warn("load compliance result for verification",
				zap.String("rule_id", ruleID),
				zap.String("node_id", nodeID.String()),
				zap.Error(err),
			)
		}

		if evalErr != nil {
			msg := fmt.Sprintf("verify evaluation failed: %v", evalErr)
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, msg, nil)
			return evalErr
		}

		passed := verifyResultsPassed(results, ruleID)

		if existing != nil {
			verifJob := jobID
			if err := s.store.UpdateComplianceResultVerification(ctx, existing.ID, passed, &verifJob); err != nil {
				s.logger.Warn("update compliance verification",
					zap.String("result_id", existing.ID.String()),
					zap.Error(err),
				)
			}
		}

		finalStatus := storage.JobStatusSucceeded
		finalMsg := "verification passed"
		if !passed {
			finalMsg = "verification failed"
		}
		if err := s.store.UpdateJobStatus(ctx, jobID, finalStatus, finalMsg, nil); err != nil {
			return fmt.Errorf("update verify job status: %w", err)
		}

		s.recordAudit(ctx, s.systemActor(), tenantID, "remediation.verified", "job", jobID.String(), map[string]any{
			"rule_id": ruleID,
			"node_id": nodeID.String(),
			"passed":  passed,
		})

		if !passed && script != nil && script.RollbackContent.Valid && script.RollbackContent.String != "" {
			if err := s.enqueueRemediationRollback(ctx, tenantID, nodeID, ruleID, jobID, script); err != nil {
				s.logger.Warn("enqueue rollback after failed verification",
					zap.String("rule_id", ruleID),
					zap.String("node_id", nodeID.String()),
					zap.Error(err),
				)
			}
		}

		return nil
	}
}

// verifyRuleOnNode reuses evaluateWithPolicies to re-scan only the single rule
// that originally produced a failing result. When no policies match, it
// returns a synthetic passing result so absence of policy does not
// artificially keep compliance_results.verified stuck at false.
func (s *Server) verifyRuleOnNode(ctx context.Context, tenantID, nodeID uuid.UUID, ruleID string) ([]compliance.Result, error) {
	if s.store == nil {
		return nil, errors.New("store unavailable")
	}

	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("load node: %w", err)
	}

	allPolicies, err := s.store.GetEffectivePolicies(ctx, tenantID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("load policies: %w", err)
	}

	policies := make([]storage.PolicyWithVersion, 0, 1)
	for _, p := range allPolicies {
		if p.ID.String() == ruleID {
			policies = append(policies, p)
			break
		}
	}

	req := complianceEvaluateRequest{
		NodeID:   nodeID.String(),
		Region:   "verify",
		RuleSets: []string{ruleID},
	}

	if len(policies) == 0 {
		// No policy to evaluate against — the rule may have been deleted
		// between scan and verify. Synthesise a passing result so the failing
		// row is marked verified instead of stuck.
		return []compliance.Result{{
			RuleID:    ruleID,
			Passed:    true,
			Severity:  "info",
			Details:   "no policy available for verification; assuming fixed",
			CheckedAt: time.Now().UTC(),
		}}, nil
	}

	return s.evaluateWithPolicies(ctx, req, policies, node)
}

// verifyResultsPassed checks whether the rule we care about passed in the
// results slice. If the rule is missing entirely (policy was deleted) we
// treat that as passed so we don't permanently leave verified=false.
func verifyResultsPassed(results []compliance.Result, ruleID string) bool {
	for _, r := range results {
		if r.RuleID == ruleID {
			return r.Passed
		}
	}
	return true
}

// enqueueRemediationRollback fires a remediation.rollback job that executes
// the inverse script body.
func (s *Server) enqueueRemediationRollback(ctx context.Context, tenantID, nodeID uuid.UUID, ruleID string, triggeringJobID uuid.UUID, script *storage.RemediationScript) error {
	if s.store == nil || s.worker == nil {
		return errors.New("store or worker unavailable")
	}
	if script == nil || !script.RollbackContent.Valid || script.RollbackContent.String == "" {
		return errors.New("no rollback content available")
	}

	payload := map[string]any{
		"script_id":         script.ID.String(),
		"rule_id":           ruleID,
		"node_id":           nodeID.String(),
		"platform":          script.Platform,
		"script_type":       script.ScriptType,
		"script_content":    script.RollbackContent.String,
		"mode":              "rollback",
		"triggering_job_id": triggeringJobID.String(),
	}
	jobID := uuid.New()
	if actionPlanID := s.createRemediationScriptActionPlan(ctx, tenantID, nodeID, jobID, ruleID, script, "rollback", true); actionPlanID != uuid.Nil {
		payload["action_plan_id"] = actionPlanID.String()
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal rollback payload: %w", err)
	}

	job := &storage.Job{
		ID:       jobID,
		Type:     JobTypeRemediationRollback,
		TenantID: tenantID,
		Payload:  payloadBytes,
		Status:   storage.JobStatusQueued,
	}
	job, err = s.store.CreateJob(ctx, job, nil)
	if err != nil {
		return fmt.Errorf("create rollback job: %w", err)
	}

	if nodeID != uuid.Nil {
		if result, err := s.store.GetLatestComplianceResultForRule(ctx, nodeID, ruleID); err == nil && result != nil {
			if err := s.store.UpdateComplianceResultRollback(ctx, result.ID, job.ID); err != nil {
				s.logger.Warn("attach rollback_job_id",
					zap.String("result_id", result.ID.String()),
					zap.Error(err),
				)
			}
		}
	}

	rollbackScript := *script
	rollbackScript.ScriptContent = script.RollbackContent.String

	handler := s.buildRemediationJobExecution(job.ID, script.ID, nodeID, ruleID, &rollbackScript)
	task := worker.Task{
		Name:         fmt.Sprintf("remediation-rollback-%s", job.ID),
		Job:          handler,
		MaxAttempts:  3,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, job.ID, storage.JobStatusFailed, "failed to enqueue rollback job", nil)
		return fmt.Errorf("enqueue rollback: %w", err)
	}

	s.logger.Info("remediation rollback enqueued",
		zap.String("job_id", job.ID.String()),
		zap.String("rule_id", ruleID),
		zap.String("node_id", nodeID.String()),
	)
	return nil
}

// remediationTenantCap returns the per-tenant concurrency cap, defaulting to 10.
func (s *Server) remediationTenantCap() int {
	if s.cfg == nil {
		return 10
	}
	if s.cfg.Remediation.MaxConcurrentPerTenant > 0 {
		return s.cfg.Remediation.MaxConcurrentPerTenant
	}
	return 10
}

// remediationLeaseTTL returns the configured lease TTL, defaulting to 10m.
func (s *Server) remediationLeaseTTL() time.Duration {
	if s.cfg == nil {
		return 10 * time.Minute
	}
	if s.cfg.Remediation.LeaseTTL > 0 {
		return s.cfg.Remediation.LeaseTTL
	}
	return 10 * time.Minute
}
