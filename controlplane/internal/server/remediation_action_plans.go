package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/remediation"
)

func (s *Server) createRemediationScriptActionPlan(ctx context.Context, tenantID, nodeID, jobID uuid.UUID, ruleID string, script *storage.RemediationScript, mode string, autoTriggered bool) uuid.UUID {
	if s == nil || s.store == nil || script == nil {
		return uuid.Nil
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return uuid.Nil
	}
	descriptor := remediationDescriptorFromScript(*script)
	plan, err := store.CreateActionPlan(ctx, storage.CreateActionPlanParams{
		TenantID:   tenantID,
		NodeID:     &nodeID,
		Domain:     "remediation",
		ActionKind: "remediation." + firstNonEmpty(mode, "execute"),
		State:      storage.ActionPlanStateQueued,
		Risk:       remediationRiskFromSafetyClass(descriptor.SafetyClass),
		Scope: map[string]any{
			"tenant_id": tenantID.String(),
			"node_id":   nodeID.String(),
			"rule_id":   ruleID,
			"script_id": script.ID.String(),
			"mode":      firstNonEmpty(mode, "execute"),
		},
		Diff: map[string]any{
			"summary":          "compliance remediation script execution",
			"script_checksum":  remediationScriptArtifactChecksum(*script),
			"script_version":   script.Version,
			"script_type":      script.ScriptType,
			"platform":         script.Platform,
			"auto_triggered":   autoTriggered,
			"manual_triggered": !autoTriggered,
			"policy_gates":     descriptor.PolicyGates,
		},
		RequiredApprovals: map[string]any{
			"requires_approval": descriptor.RequiresApproval,
			"safety_class":      descriptor.SafetyClass,
		},
		RollbackPlan: map[string]any{
			"available": script.RollbackContent.Valid && strings.TrimSpace(script.RollbackContent.String) != "",
			"mode":      "remediation.rollback",
		},
		VerificationPlan: map[string]any{
			"job_status":        "worker_reported",
			"compliance_verify": autoTriggered,
		},
		IdempotencyKey: "remediation_job:" + jobID.String(),
		SourceRef: map[string]any{
			"domain_table": "jobs",
			"job_id":       jobID.String(),
			"script_id":    script.ID.String(),
			"rule_id":      ruleID,
		},
	})
	if err != nil {
		s.logger.Warn("create remediation action plan",
			zap.Error(err),
			zap.String("job_id", jobID.String()),
			zap.String("script_id", script.ID.String()),
		)
		return uuid.Nil
	}
	return plan.ID
}

func remediationActionPlanIDFromJob(job *storage.Job) uuid.UUID {
	if job == nil || len(job.Payload) == 0 {
		return uuid.Nil
	}
	var payload map[string]any
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return uuid.Nil
	}
	parsed, ok := parseOptionalActionPlanID(detailsString(payload, "action_plan_id", ""))
	if !ok {
		return uuid.Nil
	}
	return parsed
}

func (s *Server) recordRemediationActionReceipt(ctx context.Context, planID uuid.UUID, job *storage.Job, nodeID uuid.UUID, ruleID string, state storage.ActionPlanState, result *remediation.Result, errMsg string) {
	if planID == uuid.Nil || job == nil || s == nil || s.store == nil {
		return
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return
	}
	receipt := map[string]any{
		"job_id":  job.ID.String(),
		"rule_id": ruleID,
		"status":  string(state),
	}
	if nodeID != uuid.Nil {
		receipt["node_id"] = nodeID.String()
	}
	verification := map[string]any{
		"job_status": string(state),
	}
	if result != nil {
		receipt["success"] = result.Success
		receipt["executed_at"] = result.ExecutedAt.UTC().Format(time.RFC3339)
		receipt["duration"] = result.Duration.String()
		if strings.TrimSpace(result.Output) != "" {
			receipt["output"] = result.Output
		}
		if strings.TrimSpace(result.Error) != "" {
			receipt["error"] = result.Error
		}
		verification["script_success"] = result.Success
	}
	if strings.TrimSpace(errMsg) != "" {
		receipt["error"] = errMsg
	}
	_, err := store.CreateActionReceipt(ctx, storage.CreateActionReceiptParams{
		ActionPlanID: planID,
		TenantID:     job.TenantID,
		NodeID:       &nodeID,
		JobID:        &job.ID,
		State:        state,
		Receipt:      receipt,
		Verification: verification,
		Error:        errMsg,
	})
	if err != nil {
		s.logger.Warn("create remediation action receipt",
			zap.Error(err),
			zap.String("action_plan_id", planID.String()),
			zap.String("job_id", job.ID.String()),
		)
	}
}

func remediationRiskFromSafetyClass(safetyClass string) string {
	switch strings.ToLower(strings.TrimSpace(safetyClass)) {
	case "destructive":
		return "critical"
	case "privileged":
		return "high"
	case "standard":
		return "medium"
	default:
		return "low"
	}
}

func remediationActionReceiptState(jobType string, success bool) storage.ActionPlanState {
	if !success {
		return storage.ActionPlanStateFailed
	}
	if jobType == JobTypeRemediationRollback {
		return storage.ActionPlanStateRolledBack
	}
	return storage.ActionPlanStateSucceeded
}
