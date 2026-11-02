package server

import (
	"context"
	"fmt"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"net/http"
	"time"
)

type patchAutomationStore interface {
	ListPatchAutomationRuns(context.Context, uuid.UUID) ([]storage.PatchAutomationRun, error)
	DuePatchAutomationRuns(context.Context) ([]storage.PatchAutomationRun, error)
	QueuePatchAutomationRun(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int64, time.Time, string) (storage.PatchAutomationRun, error)
	CancelPatchAutomationRun(context.Context, uuid.UUID, uuid.UUID, string) error
	SetPatchAutomationBlocked(context.Context, uuid.UUID, string) error
	CheckPatchAutomationRun(context.Context, uuid.UUID, uuid.UUID) (storage.PatchAutomationRun, error)
	PatchAutomationReadiness(context.Context, uuid.UUID, uuid.UUID) error
	DiscoverPatchAutomationRuns(context.Context) error
}

func (s *Server) handlePatchAutomation(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if principal.Type == "agent" {
		http.Error(w, "operator access required", 403)
		return
	}
	tenant, e := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if e != nil || tenant == uuid.Nil {
		http.Error(w, "tenant_id required", 400)
		return
	}
	roles := []string{roleViewer, roleOperator, roleAdmin}
	if r.Method != "GET" {
		roles = []string{roleAdmin}
	}
	if !s.requireTenantAccess(w, r, principal, tenant, roles...) {
		return
	}
	store, ok := s.store.(patchAutomationStore)
	if !ok {
		http.Error(w, "automation storage unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		runs, err := store.ListPatchAutomationRuns(r.Context(), tenant)
		if err != nil {
			http.Error(w, "cannot load automation requests", 503)
			return
		}
		writeJSON(w, 200, map[string]any{"runs": runs, "limit": 100, "scope": "latest_retained_requests", "supported": "exact Ubuntu APT direct/proxy; complete health and verified canary required"})
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		ApprovalID uuid.UUID `json:"approval_id"`
		RunID      uuid.UUID `json:"run_id"`
		NodeID     uuid.UUID `json:"node_id"`
		Revision   int64     `json:"revision"`
		At         time.Time `json:"not_before"`
	}
	if !decodeGovernance(w, r, &req) {
		return
	}
	switch r.URL.Query().Get("action") {
	case "readiness":
		node, err := s.store.GetNode(r.Context(), req.NodeID)
		if err != nil || node == nil || node.TenantID != tenant {
			http.Error(w, "tenant node required", 404)
			return
		}
		err = store.PatchAutomationReadiness(r.Context(), tenant, req.NodeID)
		reason := "Current host observations pass pre-install readiness; exact authorization and canary gates are checked separately"
		if err != nil {
			reason = err.Error()
		}
		writeJSON(w, 200, map[string]any{"ready": err == nil, "reason": reason, "installation_authorized": false})
		return
	case "schedule":
		actor := principalStorageUserID(s, r.Context(), principal)
		run, err := store.QueuePatchAutomationRun(r.Context(), tenant, req.ApprovalID, actor, req.Revision, req.At, "individual_approval")
		if err != nil {
			http.Error(w, "schedule rejected: "+err.Error(), 409)
			return
		}
		s.recordAudit(r.Context(), principal, tenant, "patch.automation_scheduled", "patch_automation_run", run.ID.String(), map[string]any{"approval_id": run.ApprovalID, "policy_revision": run.PolicyRevision, "not_before": run.NotBefore})
		writeJSON(w, 201, run)
		return
	case "cancel":
		if e = store.CancelPatchAutomationRun(r.Context(), tenant, req.RunID, principal.Subject); e != nil {
			http.Error(w, "cannot cancel undispatched schedule", 409)
			return
		}
		writeJSON(w, 200, map[string]bool{"cancelled": true})
		return
	default:
		http.Error(w, "unknown automation action", 400)
	}
}

func (s *Server) runPatchAutomationCycle(ctx context.Context) {
	store, ok := s.store.(patchAutomationStore)
	if !ok {
		return
	}
	if err := store.DiscoverPatchAutomationRuns(ctx); err != nil {
		s.logger.Warn("automation discovery unavailable")
		return
	}
	runs, err := store.DuePatchAutomationRuns(ctx)
	if err != nil {
		s.logger.Warn("automation queue unavailable")
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		if err = s.dispatchPatchAutomationRun(ctx, store, run); err != nil {
			_ = store.SetPatchAutomationBlocked(ctx, run.ID, err.Error())
		}
	}
}
func (s *Server) dispatchPatchAutomationRun(ctx context.Context, store patchAutomationStore, run storage.PatchAutomationRun) error {
	checked, e := store.CheckPatchAutomationRun(ctx, run.TenantID, run.ID)
	if e != nil {
		return e
	}
	if checked.Status == "dispatched" {
		return nil
	}
	approval, e := s.store.GetPatchApproval(ctx, run.ApprovalID)
	if e != nil || approval == nil || approval.TenantID != run.TenantID || approval.Status != storage.ApprovalStatusPending || approval.PolicyHash != run.PolicyHash {
		return fmt.Errorf("scheduled approval is no longer current")
	}
	dep, e := s.store.GetPatchDeployment(ctx, approval.DeploymentID)
	if e != nil || dep == nil || dep.TenantID != run.TenantID {
		return fmt.Errorf("scheduled deployment unavailable")
	}
	policy := patchPackagePolicyFromSummary(dep.Summary)
	if policy.OnlinePlan == nil {
		return fmt.Errorf("exact online plan required")
	}
	hash, proxy, e := s.patchApprovalPolicyHash(ctx, approval.TenantID, approval.NodeID, approval.Mode, approval.ProxyID, approval.WindowID, policy)
	if e != nil || hash != approval.PolicyHash {
		return fmt.Errorf("scheduled exact plan or transport changed")
	}
	gate := s.previewPatchSafetyGates(ctx, approval.TenantID, approval.NodeID, approval.Mode, approval.WindowID, policy)
	if !gate.Allowed && !gate.AwaitingApproval {
		return fmt.Errorf("scheduled execution blocked: %s", gate.Reason)
	}
	policy.approvalID, policy.approvalHash, policy.approverID = approval.ID, hash, run.ActorID
	policy.approvedProxyURL = proxy
	policy.automationRunID = run.ID
	_, e = s.dispatchPatchModeToNode(ctx, approval.TenantID, approval.DeploymentID, approval.NodeID, approval.Mode, approval.ProxyID, approval.WindowID, policy)
	if e != nil {
		return e
	}
	s.maybeRollupPatchDeployment(ctx, approval.DeploymentID)
	return nil
}
