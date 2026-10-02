package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// Patch-domain webhook event names. Reusing the EventRemediationApproval*
// constants would cross domains and confuse downstream consumers, so the
// patch pipeline emits its own.
const (
	EventPatchApprovalRequested = "patch.approval_requested"
	EventPatchApprovalApproved  = "patch.approval_approved"
	EventPatchApprovalDenied    = "patch.approval_denied"
)

// patchApprovalResponse is the wire shape for a single patch_approvals row.
type patchApprovalResponse struct {
	ID           string  `json:"id"`
	TenantID     string  `json:"tenant_id"`
	DeploymentID string  `json:"deployment_id"`
	NodeID       string  `json:"node_id"`
	Mode         string  `json:"mode"`
	ProxyID      *string `json:"proxy_id,omitempty"`
	WindowID     *string `json:"window_id,omitempty"`
	Status       string  `json:"status"`
	ApprovedBy   *string `json:"approved_by,omitempty"`
	ApprovedAt   *string `json:"approved_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
	ExpiresAt    string  `json:"expires_at"`
	JobID        *string `json:"job_id,omitempty"` // populated on approve
}

// handlePatchApprovalsCollection routes /api/v1/patch/approvals.
func (s *Server) handlePatchApprovalsCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	s.expirePatchApprovals(r.Context())
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		s.handleListPatchApprovals(w, r, principal)
	default:
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// handlePatchApprovalSubroutes handles /api/v1/patch/approvals/:id and
// /:id/approve|deny. Approve re-runs dispatchPatchModeToNode using the data
// snapshotted on the approval row (mode + optional proxy/window IDs).
func (s *Server) handlePatchApprovalSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	s.expirePatchApprovals(r.Context())
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/patch/approvals/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(trimmed, "/")
	approvalID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid approval id", http.StatusBadRequest)
		return
	}

	switch {
	case len(segments) == 1:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		if !s.requirePatchApprovalTenantAccess(w, r, principal, approvalID, roleViewer, roleOperator, roleAdmin) {
			return
		}
		s.handleGetPatchApproval(w, r, approvalID)
	case len(segments) == 2 && segments[1] == "approve":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		if !s.requirePatchApprovalTenantAccess(w, r, principal, approvalID, roleOperator, roleAdmin) {
			return
		}
		s.handleApprovePatchApproval(w, r, approvalID, principal.Subject)
	case len(segments) == 2 && segments[1] == "deny":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		if !s.requirePatchApprovalTenantAccess(w, r, principal, approvalID, roleOperator, roleAdmin) {
			return
		}
		s.handleDenyPatchApproval(w, r, approvalID, principal.Subject)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleListPatchApprovals(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filter := storage.ListPatchApprovalsFilter{}
	if v := strings.TrimSpace(r.URL.Query().Get("status")); v != "" {
		filter.Status = storage.ApprovalStatus(strings.ToLower(v))
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		if !s.requireTenantAccess(w, r, principal, parsed, roleViewer, roleOperator, roleAdmin) {
			return
		}
		filter.TenantID = parsed
	} else {
		tenantIDs, err := s.accessibleTenantIDs(r.Context(), principal, roleViewer, roleOperator, roleAdmin)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		if len(tenantIDs) == 0 {
			writeJSON(w, http.StatusOK, paginatedResponse[patchApprovalResponse]{
				Data:       []patchApprovalResponse{},
				Pagination: newPaginationMeta(0, limit, offset, 0),
			})
			return
		}
		filter.TenantIDs = tenantIDs
	}
	if v := strings.TrimSpace(r.URL.Query().Get("deployment_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid deployment_id", http.StatusBadRequest)
			return
		}
		filter.DeploymentID = parsed
	}
	if v := strings.TrimSpace(r.URL.Query().Get("node_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}

	approvals, total, err := s.store.ListPatchApprovals(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list patch approvals", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	items := make([]patchApprovalResponse, 0, len(approvals))
	for i := range approvals {
		items = append(items, patchApprovalToResponse(&approvals[i], nil))
	}

	writeJSON(w, http.StatusOK, paginatedResponse[patchApprovalResponse]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

func (s *Server) requirePatchApprovalTenantAccess(
	w http.ResponseWriter,
	r *http.Request,
	principal *auth.Principal,
	id uuid.UUID,
	roles ...string,
) bool {
	approval, err := s.store.GetPatchApproval(r.Context(), id)
	if err != nil {
		s.logger.Error("get patch approval for tenant access", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return false
	}
	if approval == nil {
		http.NotFound(w, r)
		return false
	}
	return s.requireTenantAccess(w, r, principal, approval.TenantID, roles...)
}

func (s *Server) handleGetPatchApproval(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	approval, err := s.store.GetPatchApproval(r.Context(), id)
	if err != nil {
		s.logger.Error("get patch approval", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if approval == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, patchApprovalToResponse(approval, nil))
}

func (s *Server) handleApprovePatchApproval(w http.ResponseWriter, r *http.Request, id uuid.UUID, approverSubject string) {
	approval, err := s.store.GetPatchApproval(r.Context(), id)
	if err != nil {
		s.logger.Error("get patch approval", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if approval == nil {
		http.NotFound(w, r)
		return
	}
	if approval.Status != storage.ApprovalStatusPending {
		http.Error(w, "approval is not pending", http.StatusConflict)
		return
	}
	if !approval.ExpiresAt.IsZero() && time.Now().UTC().After(approval.ExpiresAt) {
		http.Error(w, "approval has expired", http.StatusConflict)
		return
	}

	approverID, _ := uuid.Parse(strings.TrimSpace(approverSubject))

	updated, err := s.store.ResolvePatchApproval(r.Context(), id, storage.ApprovalStatusApproved, approverID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "approval is not pending", http.StatusConflict)
			return
		}
		s.logger.Error("resolve patch approval (approve)", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Re-dispatch with the snapshot stored on the approval. We bypass the
	// gate here intentionally — the operator has already greenlit this
	// (deployment, node) pair. A future hardening pass could re-run the
	// non-approval gates (change window / circuit breaker) before
	// dispatching, but for now we stay aligned with the compliance approve
	// loop which also bypasses on approve.
	policy := patchPackagePolicy{}
	if deployment, derr := s.store.GetPatchDeployment(r.Context(), updated.DeploymentID); derr == nil && deployment != nil {
		policy = patchPackagePolicyFromSummary(deployment.Summary)
	}
	state, dispErr := s.dispatchPatchModeToNode(
		r.Context(),
		updated.TenantID,
		updated.DeploymentID,
		updated.NodeID,
		updated.Mode,
		updated.ProxyID,
		updated.WindowID,
		policy,
	)
	var jobID *uuid.UUID
	if dispErr != nil {
		s.logger.Error("dispatch on patch approve",
			zap.Error(dispErr),
			zap.String("approval_id", updated.ID.String()),
			zap.String("deployment_id", updated.DeploymentID.String()),
			zap.String("node_id", updated.NodeID.String()),
		)
		if state != nil {
			if markErr := s.store.MarkNodePatchFailed(r.Context(), state.ID, dispErr.Error(), ""); markErr != nil {
				s.logger.Warn("mark approved patch dispatch failed", zap.Error(markErr), zap.String("node_id", updated.NodeID.String()))
			}
		}
		s.maybeRollupPatchDeployment(r.Context(), updated.DeploymentID)
	} else {
		if state != nil && state.JobID != nil && *state.JobID != uuid.Nil {
			id := *state.JobID
			jobID = &id
		}
		_ = s.store.UpdatePatchDeploymentStatus(r.Context(), updated.DeploymentID, "in_progress", false)
	}

	s.emitRemediationSafetyEvent(r.Context(), updated.TenantID, EventPatchApprovalApproved, map[string]any{
		"approval_id":   updated.ID.String(),
		"tenant_id":     updated.TenantID.String(),
		"deployment_id": updated.DeploymentID.String(),
		"node_id":       updated.NodeID.String(),
		"mode":          updated.Mode,
		"approver_id":   approverID.String(),
		"job_id": func() string {
			if jobID == nil {
				return ""
			}
			return jobID.String()
		}(),
	})

	writeJSON(w, http.StatusOK, patchApprovalToResponse(updated, jobID))

	s.recordAudit(r.Context(), s.systemActor(), updated.TenantID, "patch.approval_approved", "patch_approval", updated.ID.String(), map[string]any{
		"deployment_id": updated.DeploymentID.String(),
		"node_id":       updated.NodeID.String(),
		"mode":          updated.Mode,
	})
}

func (s *Server) handleDenyPatchApproval(w http.ResponseWriter, r *http.Request, id uuid.UUID, approverSubject string) {
	approval, err := s.store.GetPatchApproval(r.Context(), id)
	if err != nil {
		s.logger.Error("get patch approval", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if approval == nil {
		http.NotFound(w, r)
		return
	}
	if approval.Status != storage.ApprovalStatusPending {
		http.Error(w, "approval is not pending", http.StatusConflict)
		return
	}

	approverID, _ := uuid.Parse(strings.TrimSpace(approverSubject))

	updated, err := s.store.ResolvePatchApproval(r.Context(), id, storage.ApprovalStatusDenied, approverID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "approval is not pending", http.StatusConflict)
			return
		}
		s.logger.Error("resolve patch approval (deny)", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if state, stateErr := s.store.CreateNodePatchState(r.Context(), storage.NodePatchState{
		DeploymentID: updated.DeploymentID,
		NodeID:       updated.NodeID,
		TenantID:     updated.TenantID,
	}); stateErr != nil {
		s.logger.Warn("create denied patch state", zap.Error(stateErr), zap.String("node_id", updated.NodeID.String()))
	} else if state != nil {
		if markErr := s.store.MarkNodePatchFailed(r.Context(), state.ID, "patch approval denied", ""); markErr != nil {
			s.logger.Warn("mark denied patch state failed", zap.Error(markErr), zap.String("node_id", updated.NodeID.String()))
		}
	}
	s.maybeRollupPatchDeployment(r.Context(), updated.DeploymentID)

	s.emitRemediationSafetyEvent(r.Context(), updated.TenantID, EventPatchApprovalDenied, map[string]any{
		"approval_id":   updated.ID.String(),
		"tenant_id":     updated.TenantID.String(),
		"deployment_id": updated.DeploymentID.String(),
		"node_id":       updated.NodeID.String(),
		"mode":          updated.Mode,
		"approver_id":   approverID.String(),
	})

	writeJSON(w, http.StatusOK, patchApprovalToResponse(updated, nil))

	s.recordAudit(r.Context(), s.systemActor(), updated.TenantID, "patch.approval_denied", "patch_approval", updated.ID.String(), map[string]any{
		"deployment_id": updated.DeploymentID.String(),
		"node_id":       updated.NodeID.String(),
		"mode":          updated.Mode,
	})
}

func (s *Server) expirePatchApprovals(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	if _, err := s.store.ExpirePatchApprovals(ctx, time.Now().UTC()); err != nil {
		s.logger.Warn("expire patch approvals", zap.Error(err))
	}
}

func patchApprovalToResponse(a *storage.PatchApproval, jobID *uuid.UUID) patchApprovalResponse {
	resp := patchApprovalResponse{
		ID:           a.ID.String(),
		TenantID:     a.TenantID.String(),
		DeploymentID: a.DeploymentID.String(),
		NodeID:       a.NodeID.String(),
		Mode:         a.Mode,
		Status:       string(a.Status),
		CreatedAt:    a.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:    a.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if a.ProxyID != nil {
		s := a.ProxyID.String()
		resp.ProxyID = &s
	}
	if a.WindowID != nil {
		s := a.WindowID.String()
		resp.WindowID = &s
	}
	if a.ApprovedBy != nil {
		s := a.ApprovedBy.String()
		resp.ApprovedBy = &s
	}
	if a.ApprovedAt != nil {
		t := a.ApprovedAt.UTC().Format(time.RFC3339)
		resp.ApprovedAt = &t
	}
	if jobID != nil {
		s := jobID.String()
		resp.JobID = &s
	}
	return resp
}
