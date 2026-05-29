package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func (s *Server) createPatchNodeActionPlan(ctx context.Context, tenantID, deploymentID, patchStateID, nodeID uuid.UUID, mode string, policy patchPackagePolicy) uuid.UUID {
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
		Domain:     "patch",
		ActionKind: "patch.deploy",
		State:      storage.ActionPlanStateQueued,
		Risk:       "high",
		Scope: map[string]any{
			"tenant_id":           tenantID.String(),
			"node_id":             nodeID.String(),
			"deployment_id":       deploymentID.String(),
			"node_patch_state_id": patchStateID.String(),
			"mode":                mode,
		},
		Diff: map[string]any{
			"summary":             "OS package patch execution on node",
			"package_allowlist":   policy.Allowlist,
			"package_denylist":    policy.Denylist,
			"post_patch_rescan":   policy.PostPatchRescan,
			"operator_readable":   true,
			"expected_job_status": storage.JobStatusQueued,
		},
		RequiredApprovals: map[string]any{
			"gate":   "patch_safety_gates",
			"status": "completed_or_not_required",
		},
		RollbackPlan: map[string]any{
			"type":     "package_manager",
			"strategy": "use repository cache, package pinning, or maintenance rollback policy where available",
		},
		VerificationPlan: map[string]any{
			"node_patch_state":     "applied_or_failed",
			"job_status":           "heartbeat_reported",
			"post_patch_inventory": policy.PostPatchRescan,
		},
		IdempotencyKey: "patch_node_state:" + patchStateID.String(),
		SourceRef: map[string]any{
			"domain_table":        "node_patch_state",
			"deployment_id":       deploymentID.String(),
			"node_patch_state_id": patchStateID.String(),
		},
	})
	if err != nil {
		s.logger.Warn("create patch action plan",
			zap.Error(err),
			zap.String("deployment_id", deploymentID.String()),
			zap.String("node_id", nodeID.String()),
		)
		return uuid.Nil
	}
	return plan.ID
}

func (s *Server) markActionPlanFailed(ctx context.Context, rawPlanID string) {
	planID, ok := parseOptionalActionPlanID(rawPlanID)
	if !ok || s == nil || s.store == nil {
		return
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return
	}
	if _, err := store.UpdateActionPlanState(ctx, planID, storage.ActionPlanStateFailed); err != nil {
		s.logger.Warn("mark action plan failed", zap.Error(err), zap.String("action_plan_id", planID.String()))
	}
}

func (s *Server) patchActionPlanIDForJob(ctx context.Context, jobID uuid.UUID) uuid.UUID {
	if s == nil || s.store == nil || jobID == uuid.Nil {
		return uuid.Nil
	}
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		s.logger.Warn("get patch job for action plan receipt", zap.Error(err), zap.String("job_id", jobID.String()))
		return uuid.Nil
	}
	if job == nil || len(job.Payload) == 0 {
		return uuid.Nil
	}
	var payload patchJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return uuid.Nil
	}
	planID, ok := parseOptionalActionPlanID(payload.ActionPlanID)
	if !ok {
		return uuid.Nil
	}
	return planID
}

func (s *Server) recordPatchActionReceipt(ctx context.Context, planID uuid.UUID, ps *storage.NodePatchState, jobID uuid.UUID, status storage.ActionPlanState, packagesUpgraded int, logTail string, errMsg string, postPatchRescan bool) {
	if planID == uuid.Nil || ps == nil || s == nil || s.store == nil {
		return
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		return
	}
	receipt := map[string]any{
		"job_id":              jobID.String(),
		"node_patch_state_id": ps.ID.String(),
		"deployment_id":       ps.DeploymentID.String(),
		"status":              string(status),
	}
	if packagesUpgraded >= 0 {
		receipt["packages_upgraded"] = packagesUpgraded
	}
	if strings.TrimSpace(logTail) != "" {
		receipt["log_tail"] = logTail
	}
	verification := map[string]any{
		"node_patch_state":     string(status),
		"job_status":           string(status),
		"post_patch_inventory": postPatchRescan,
	}
	_, err := store.CreateActionReceipt(ctx, storage.CreateActionReceiptParams{
		ActionPlanID: planID,
		TenantID:     ps.TenantID,
		NodeID:       &ps.NodeID,
		JobID:        &jobID,
		State:        status,
		Receipt:      receipt,
		Verification: verification,
		Error:        errMsg,
	})
	if err != nil {
		s.logger.Warn("create patch action receipt",
			zap.Error(err),
			zap.String("action_plan_id", planID.String()),
			zap.String("job_id", jobID.String()),
		)
	}
}

func parseOptionalActionPlanID(raw string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, false
	}
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, false
	}
	return parsed, true
}
