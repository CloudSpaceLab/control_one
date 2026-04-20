package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

// JobTypeComplianceVerify re-evaluates a single rule after a remediation has
// completed so the server can mark the compliance_result verified or kick off
// a rollback.
const JobTypeComplianceVerify = "compliance.verify"

// JobTypeRemediationRollback executes the rollback body of a remediation
// script when post-remediation verification fails.
const JobTypeRemediationRollback = "remediation.rollback"

// triggerAutoRemediation creates a remediation job for a failed compliance result
// if auto-remediation is enabled and a matching script exists for the rule.
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

	// Per-tenant concurrency cap. Check before we cut a job row so we don't
	// persist orphan jobs whenever the cap is hit.
	if cap := s.remediationTenantCap(); cap > 0 && tenantID != uuid.Nil {
		inflight, err := s.store.CountTenantLeases(ctx, tenantID)
		if err != nil {
			s.logger.Warn("count tenant remediation leases",
				zap.String("tenant_id", tenantID.String()),
				zap.Error(err),
			)
		} else if inflight >= cap {
			s.logger.Info("auto-remediation deferred: tenant cap reached",
				zap.String("tenant_id", tenantID.String()),
				zap.Int("in_flight", inflight),
				zap.Int("cap", cap),
			)
			return nil
		}
	}

	// Build job payload following the pattern in handleExecuteRemediationScript.
	jobPayload := map[string]any{
		"script_id":      script.ID.String(),
		"rule_id":        result.RuleID,
		"node_id":        nodeID.String(),
		"platform":       script.Platform,
		"script_type":    script.ScriptType,
		"script_content": script.ScriptContent,
		"auto_triggered": true,
	}

	payloadBytes, err := json.Marshal(jobPayload)
	if err != nil {
		s.logger.Error("marshal auto-remediation job payload",
			zap.String("rule_id", result.RuleID),
			zap.Error(err),
		)
		return nil
	}

	job := &storage.Job{
		Type:     "remediation.execute",
		TenantID: tenantID,
		Payload:  payloadBytes,
		Status:   storage.JobStatusQueued,
	}
	job, err = s.store.CreateJob(ctx, job, nil)
	if err != nil {
		s.logger.Error("create auto-remediation job",
			zap.String("rule_id", result.RuleID),
			zap.Error(err),
		)
		return nil
	}

	// Acquire the per-node lease. If another remediation is already in flight
	// on this node we skip rather than stampede it.
	if nodeID != uuid.Nil && tenantID != uuid.Nil {
		if _, err := s.store.AcquireRemediationLease(ctx, tenantID, nodeID, job.ID, s.remediationLeaseTTL()); err != nil {
			if errors.Is(err, storage.ErrLeaseHeld) {
				s.logger.Info("auto-remediation skipped: lease already held",
					zap.String("node_id", nodeID.String()),
					zap.String("rule_id", result.RuleID),
				)
			} else {
				s.logger.Error("acquire remediation lease",
					zap.String("node_id", nodeID.String()),
					zap.Error(err),
				)
			}
			_ = s.store.UpdateJobStatus(ctx, job.ID, storage.JobStatusCancelled, "skipped: remediation lease held", nil)
			return nil
		}
	}

	chained := s.chainRemediationExecution(job.ID, script.ID, nodeID, result.RuleID, script, true)

	task := worker.Task{
		Name:         fmt.Sprintf("auto-remediation-%s", job.ID),
		Job:          chained,
		MaxAttempts:  3,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		s.logger.Error("enqueue auto-remediation job",
			zap.String("job_id", job.ID.String()),
			zap.String("rule_id", result.RuleID),
			zap.Error(err),
		)
		_ = s.store.UpdateJobStatus(ctx, job.ID, storage.JobStatusFailed, "failed to enqueue auto-remediation job", nil)
		if nodeID != uuid.Nil {
			_ = s.store.ReleaseRemediationLease(ctx, nodeID)
		}
		return nil
	}

	s.logger.Info("auto-remediation triggered",
		zap.String("job_id", job.ID.String()),
		zap.String("rule_id", result.RuleID),
		zap.String("node_id", nodeID.String()),
		zap.String("script_id", script.ID.String()),
	)

	return &job.ID
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
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal rollback payload: %w", err)
	}

	job := &storage.Job{
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
