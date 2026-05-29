package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func (s *Server) attachAILogFixerActionPlan(ctx context.Context, tenantID, nodeID uuid.UUID, runID *uuid.UUID, jobID uuid.UUID, payload aiLogFixerJobPayload) map[string]any {
	policy := copyActionPolicy(payload.Policy)
	if s == nil || s.store == nil {
		return policy
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return policy
	}
	scope := map[string]any{
		"tenant_id":   tenantID.String(),
		"node_id":     nodeID.String(),
		"service_key": payload.ServiceKey,
		"action":      payload.Action,
	}
	sourceRef := map[string]any{
		"domain_table": "ai_logfixer_actions",
		"job_id":       jobID.String(),
	}
	if runID != nil && *runID != uuid.Nil {
		scope["run_id"] = runID.String()
		sourceRef["run_id"] = runID.String()
	}
	plan, err := store.CreateActionPlan(ctx, storage.CreateActionPlanParams{
		TenantID:   tenantID,
		NodeID:     &nodeID,
		Domain:     "ai_logfixer",
		ActionKind: payload.Action,
		State:      storage.ActionPlanStateQueued,
		Risk:       aiLogFixerActionRisk(payload.Action, policy),
		Scope:      scope,
		Diff: map[string]any{
			"summary":          "AI LogFixer node-local action",
			"policy":           policy,
			"service_key":      payload.ServiceKey,
			"diagnosis":        rawJSONForActionPlan(payload.Diagnosis),
			"remediation_plan": rawJSONForActionPlan(payload.RemediationPlan),
			"receipt_required": aiLogFixerActionRequiresReceipt(payload.Action),
		},
		RequiredApprovals: map[string]any{
			"approved": policyBool(policy, "approved"),
			"required": payload.Action == JobTypeAILogFixerApply || payload.Action == JobTypeAILogFixerRollback,
		},
		RollbackPlan: map[string]any{
			"type":          "ai_logfixer_contract",
			"rollback_job":  JobTypeAILogFixerRollback,
			"receipt_field": "receipt",
		},
		VerificationPlan: map[string]any{
			"ai_logfixer_action": "succeeded_or_failed",
			"receipt_required":   aiLogFixerActionRequiresReceipt(payload.Action),
			"run_status":         "mirrored_when_run_id_present",
		},
		IdempotencyKey: "ai_logfixer_job:" + jobID.String(),
		SourceRef:      sourceRef,
	})
	if err != nil {
		s.logger.Warn("create ai logfixer action plan",
			zap.Error(err),
			zap.String("job_id", jobID.String()),
			zap.String("action", payload.Action),
		)
		return policy
	}
	policy["action_plan_id"] = plan.ID.String()
	return policy
}

func (s *Server) recordAILogFixerActionReceipt(ctx context.Context, action *storage.AILogFixerAction, jobID uuid.UUID, status string, errMsg string, metadata map[string]any) {
	if action == nil || s == nil || s.store == nil {
		return
	}
	planID := aiLogFixerActionPlanID(action)
	if planID == uuid.Nil {
		return
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return
	}
	state := storage.ActionPlanStateSucceeded
	if status != "succeeded" {
		state = storage.ActionPlanStateFailed
	}
	receipt := map[string]any{
		"job_id":                jobID.String(),
		"ai_logfixer_action":    action.Action,
		"ai_logfixer_action_id": action.ID.String(),
		"status":                status,
	}
	if action.RunID.Valid {
		receipt["run_id"] = action.RunID.UUID.String()
	}
	if metadata != nil {
		receipt["metadata"] = metadata
	}
	verification := map[string]any{
		"ai_logfixer_action": status,
		"receipt_present":    len(metadataMap(metadata["receipt"])) > 0,
		"receipt_required":   aiLogFixerActionRequiresReceipt(action.Action),
	}
	if _, err := store.CreateActionReceipt(ctx, storage.CreateActionReceiptParams{
		ActionPlanID: planID,
		TenantID:     action.TenantID,
		NodeID:       &action.NodeID,
		JobID:        &jobID,
		State:        state,
		Receipt:      receipt,
		Verification: verification,
		Error:        errMsg,
	}); err != nil {
		s.logger.Warn("create ai logfixer action receipt",
			zap.Error(err),
			zap.String("action_plan_id", planID.String()),
			zap.String("job_id", jobID.String()),
		)
	}
}

func aiLogFixerActionPlanID(action *storage.AILogFixerAction) uuid.UUID {
	if action == nil {
		return uuid.Nil
	}
	raw := detailsString(action.Policy, "action_plan_id", "")
	parsed, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

func aiLogFixerActionRisk(action string, policy map[string]any) string {
	if risk := strings.TrimSpace(detailsString(policy, "risk", "")); risk != "" {
		return risk
	}
	switch strings.TrimSpace(action) {
	case JobTypeAILogFixerApply, JobTypeAILogFixerRollback:
		return "high"
	default:
		return "medium"
	}
}

func rawJSONForActionPlan(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return string(raw)
	}
	return decoded
}
