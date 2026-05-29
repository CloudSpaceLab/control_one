package server

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func (s *Server) attachWebserverActionPlan(ctx context.Context, tenantID, nodeID uuid.UUID, instanceID *uuid.UUID, jobID uuid.UUID, action string, policy map[string]any, instance map[string]any) (map[string]any, uuid.UUID) {
	policy = copyActionPolicy(policy)
	if s == nil || s.store == nil {
		return policy, uuid.Nil
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return policy, uuid.Nil
	}
	scope := map[string]any{
		"tenant_id": tenantID.String(),
		"node_id":   nodeID.String(),
		"action":    action,
	}
	sourceRef := map[string]any{
		"domain_table": "webserver_config_actions",
		"job_id":       jobID.String(),
	}
	if instanceID != nil && *instanceID != uuid.Nil {
		scope["webserver_instance_id"] = instanceID.String()
		sourceRef["webserver_instance_id"] = instanceID.String()
	}
	plan, err := store.CreateActionPlan(ctx, storage.CreateActionPlanParams{
		TenantID:   tenantID,
		NodeID:     &nodeID,
		Domain:     "webserver",
		ActionKind: action,
		State:      storage.ActionPlanStateQueued,
		Risk:       webserverActionRisk(action),
		Scope:      scope,
		Diff: map[string]any{
			"summary":           "webserver configuration action",
			"policy":            policy,
			"instance":          instance,
			"receipt_required":  webserverActionRequiresReceipt(action),
			"operator_readable": true,
		},
		RequiredApprovals: map[string]any{
			"gate":   "webserver_restart_sensitive_approval",
			"status": "completed_or_not_required",
		},
		RollbackPlan: map[string]any{
			"type":          "webserver_config_restore",
			"rollback_ref":  "expected_in_webserver_config_receipt",
			"reload_safety": "validate_then_reload",
		},
		VerificationPlan: map[string]any{
			"webserver_config_action": "succeeded_or_failed",
			"config_receipt":          webserverActionRequiresReceipt(action),
			"validation_status":       "ok",
			"reload_status":           "ok",
		},
		IdempotencyKey: "webserver_job:" + jobID.String(),
		SourceRef:      sourceRef,
	})
	if err != nil {
		s.logger.Warn("create webserver action plan",
			zap.Error(err),
			zap.String("job_id", jobID.String()),
			zap.String("action", action),
		)
		return policy, uuid.Nil
	}
	policy["action_plan_id"] = plan.ID.String()
	return policy, plan.ID
}

func (s *Server) recordWebserverActionReceipt(ctx context.Context, action *storage.WebserverConfigAction, jobID uuid.UUID, status string, errMsg string, metadata map[string]any, receiptPersisted bool) {
	if action == nil || s == nil || s.store == nil {
		return
	}
	planID := webserverActionPlanID(action)
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
		"job_id":                      jobID.String(),
		"webserver_config_action_id":  action.ID.String(),
		"webserver_action":            action.Action,
		"status":                      status,
		"webserver_receipt_persisted": receiptPersisted,
		"webserver_receipt_required":  webserverActionRequiresReceipt(action.Action),
	}
	if metadata != nil {
		receipt["metadata"] = metadata
	}
	verification := map[string]any{
		"webserver_config_action": status,
		"config_receipt":          receiptPersisted,
		"receipt_required":        webserverActionRequiresReceipt(action.Action),
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
		s.logger.Warn("create webserver action receipt",
			zap.Error(err),
			zap.String("action_plan_id", planID.String()),
			zap.String("job_id", jobID.String()),
		)
	}
}

func webserverActionPlanID(action *storage.WebserverConfigAction) uuid.UUID {
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

func webserverActionRisk(action string) string {
	switch strings.TrimSpace(action) {
	case JobTypeWebserverConfigApply, JobTypeWebserverConfigRollback, JobTypeWebserverBlocklistUpdate:
		return "high"
	case JobTypeWebserverConfigPlan:
		return "medium"
	default:
		return "low"
	}
}

func copyActionPolicy(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
