package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

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

	task := worker.Task{
		Name:         fmt.Sprintf("auto-remediation-%s", job.ID),
		Job:          s.buildRemediationJobExecution(job.ID, script.ID, nodeID, result.RuleID, script),
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
