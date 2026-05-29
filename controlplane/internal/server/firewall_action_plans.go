package server

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func (s *Server) createFirewallActionPlan(ctx context.Context, tenantID, nodeID, entityActionID, ruleID, jobID uuid.UUID, payload firewallJobPayload) uuid.UUID {
	if s == nil || s.store == nil {
		return uuid.Nil
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return uuid.Nil
	}
	plan, err := store.CreateActionPlan(ctx, storage.CreateActionPlanParams{
		TenantID:   tenantID,
		NodeID:     &nodeID,
		Domain:     "firewall",
		ActionKind: payload.Action,
		State:      storage.ActionPlanStateQueued,
		Risk:       "high",
		Scope: map[string]any{
			"tenant_id":             tenantID.String(),
			"node_id":               nodeID.String(),
			"entity_action_id":      entityActionID.String(),
			"node_firewall_rule_id": ruleID.String(),
			"direction":             payload.Direction,
			"source":                payload.Source,
			"dest":                  payload.Dest,
			"port":                  payload.Port,
			"protocol":              payload.Protocol,
			"tag":                   payload.Tag,
		},
		Diff: map[string]any{
			"summary":     "host firewall rule change",
			"action":      payload.Action,
			"direction":   payload.Direction,
			"source":      payload.Source,
			"dest":        payload.Dest,
			"port":        payload.Port,
			"protocol":    payload.Protocol,
			"ttl_seconds": payload.TTLSeconds,
			"reason":      payload.Reason,
		},
		RequiredApprovals: map[string]any{
			"gate":   "network_security_operator_action",
			"status": "completed_or_not_required",
		},
		RollbackPlan: map[string]any{
			"type":          "firewall_rule_inverse",
			"rollback_job":  JobTypeFirewallRuleDelete,
			"rollback_note": "remove Control One tag " + payload.Tag,
		},
		VerificationPlan: map[string]any{
			"node_firewall_rule": "applied_failed_or_removed",
			"job_status":         "heartbeat_reported",
		},
		IdempotencyKey: "firewall_rule:" + ruleID.String(),
		SourceRef: map[string]any{
			"domain_table":          "node_firewall_rules",
			"entity_action_id":      entityActionID.String(),
			"node_firewall_rule_id": ruleID.String(),
			"job_id":                jobID.String(),
		},
	})
	if err != nil {
		s.logger.Warn("create firewall action plan",
			zap.Error(err),
			zap.String("node_firewall_rule_id", ruleID.String()),
			zap.String("job_id", jobID.String()),
		)
		return uuid.Nil
	}
	return plan.ID
}

func (s *Server) firewallActionPlanIDForJob(ctx context.Context, jobID uuid.UUID) uuid.UUID {
	if s == nil || s.store == nil || jobID == uuid.Nil {
		return uuid.Nil
	}
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		s.logger.Warn("get firewall job for action plan receipt", zap.Error(err), zap.String("job_id", jobID.String()))
		return uuid.Nil
	}
	if job == nil || len(job.Payload) == 0 {
		return uuid.Nil
	}
	var payload firewallJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return uuid.Nil
	}
	planID, ok := parseOptionalActionPlanID(payload.ActionPlanID)
	if !ok {
		return uuid.Nil
	}
	return planID
}

func (s *Server) recordFirewallActionReceipt(ctx context.Context, planID uuid.UUID, rule *storage.NodeFirewallRule, jobID uuid.UUID, status storage.ActionPlanState, errMsg string) {
	if planID == uuid.Nil || rule == nil || s == nil || s.store == nil {
		return
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return
	}
	receipt := map[string]any{
		"job_id":                jobID.String(),
		"node_firewall_rule_id": rule.ID.String(),
		"entity_action_id":      rule.EntityActionID.String(),
		"action":                rule.Action,
		"status":                string(status),
	}
	verification := map[string]any{
		"node_firewall_rule": string(status),
		"job_status":         string(status),
	}
	_, err := store.CreateActionReceipt(ctx, storage.CreateActionReceiptParams{
		ActionPlanID: planID,
		TenantID:     rule.TenantID,
		NodeID:       &rule.NodeID,
		JobID:        &jobID,
		State:        status,
		Receipt:      receipt,
		Verification: verification,
		Error:        errMsg,
	})
	if err != nil {
		s.logger.Warn("create firewall action receipt",
			zap.Error(err),
			zap.String("action_plan_id", planID.String()),
			zap.String("job_id", jobID.String()),
		)
	}
}
