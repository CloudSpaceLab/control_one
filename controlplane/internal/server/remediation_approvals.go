package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// remediationApprovalResponse is the wire shape for a single approval row.
type remediationApprovalResponse struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	NodeID      string         `json:"node_id"`
	RuleID      string         `json:"rule_id"`
	ScriptID    string         `json:"script_id"`
	Severity    string         `json:"severity"`
	Status      string         `json:"status"`
	ApprovedBy  *string        `json:"approved_by,omitempty"`
	ApprovedAt  *string        `json:"approved_at,omitempty"`
	CreatedAt   string         `json:"created_at"`
	ExpiresAt   string         `json:"expires_at"`
	TaskPayload map[string]any `json:"task_payload,omitempty"`
}

// handleRemediationApprovalsCollection routes /api/v1/remediation/approvals.
// GET lists approvals (admin, operator, or viewer all allowed for visibility).
func (s *Server) handleRemediationApprovalsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
			return
		}
		s.handleListRemediationApprovals(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// handleRemediationApprovalSubroutes handles /api/v1/remediation/approvals/:id
// and /:id/approve|deny.
func (s *Server) handleRemediationApprovalSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/remediation/approvals/")
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
		if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
			return
		}
		s.handleGetRemediationApproval(w, r, approvalID)
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
		s.handleApproveRemediationApproval(w, r, approvalID, principal.Subject)
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
		s.handleDenyRemediationApproval(w, r, approvalID, principal.Subject)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleListRemediationApprovals(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filter := storage.ListRemediationApprovalsFilter{}
	if v := strings.TrimSpace(r.URL.Query().Get("status")); v != "" {
		filter.Status = storage.ApprovalStatus(strings.ToLower(v))
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = parsed
	}
	if v := strings.TrimSpace(r.URL.Query().Get("node_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}

	approvals, total, err := s.store.ListRemediationApprovals(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list remediation approvals", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	items := make([]remediationApprovalResponse, 0, len(approvals))
	for i := range approvals {
		items = append(items, approvalToResponse(&approvals[i]))
	}

	writeJSON(w, http.StatusOK, paginatedResponse[remediationApprovalResponse]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

func (s *Server) handleGetRemediationApproval(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	approval, err := s.store.GetRemediationApproval(r.Context(), id)
	if err != nil {
		s.logger.Error("get remediation approval", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if approval == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, approvalToResponse(approval))
}

func (s *Server) handleApproveRemediationApproval(w http.ResponseWriter, r *http.Request, id uuid.UUID, approverSubject string) {
	approval, err := s.store.GetRemediationApproval(r.Context(), id)
	if err != nil {
		s.logger.Error("get remediation approval", zap.Error(err))
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

	updated, err := s.store.ResolveRemediationApproval(r.Context(), id, storage.ApprovalStatusApproved, approverID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "approval is not pending", http.StatusConflict)
			return
		}
		s.logger.Error("resolve remediation approval (approve)", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Re-dispatch: deserialize the stored payload, look the script back up,
	// and call the post-gate dispatch helper directly so we bypass the
	// approval gate.
	var payload map[string]any
	if err := json.Unmarshal(updated.TaskPayload, &payload); err != nil {
		s.logger.Error("decode approved task payload", zap.Error(err))
		http.Error(w, "task payload corrupted", http.StatusInternalServerError)
		return
	}

	script, err := s.store.GetRemediationScriptByID(r.Context(), updated.ScriptID)
	if err != nil {
		s.logger.Error("load script for approved remediation", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if script == nil {
		http.Error(w, "remediation script no longer exists", http.StatusGone)
		return
	}

	jobID := s.dispatchRemediationTask(r.Context(), dispatchRemediationTaskParams{
		TenantID:  updated.TenantID,
		NodeID:    updated.NodeID,
		RuleID:    updated.RuleID,
		Script:    script,
		EnqueueAt: time.Now().UTC(),
	})

	s.emitRemediationSafetyEvent(r.Context(), updated.TenantID, EventRemediationApprovalApproved, map[string]any{
		"approval_id": updated.ID.String(),
		"tenant_id":   updated.TenantID.String(),
		"node_id":     updated.NodeID.String(),
		"rule_id":     updated.RuleID,
		"severity":    updated.Severity,
		"approver_id": approverID.String(),
		"job_id": func() string {
			if jobID == nil {
				return ""
			}
			return jobID.String()
		}(),
	})

	resp := approvalToResponse(updated)
	if jobID != nil {
		jobStr := jobID.String()
		resp.TaskPayload = map[string]any{"job_id": jobStr}
	}

	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), s.systemActor(), updated.TenantID, "remediation.approval_approved", "remediation_approval", updated.ID.String(), map[string]any{
		"rule_id":  updated.RuleID,
		"node_id":  updated.NodeID.String(),
		"severity": updated.Severity,
	})
}

func (s *Server) handleDenyRemediationApproval(w http.ResponseWriter, r *http.Request, id uuid.UUID, approverSubject string) {
	approval, err := s.store.GetRemediationApproval(r.Context(), id)
	if err != nil {
		s.logger.Error("get remediation approval", zap.Error(err))
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

	updated, err := s.store.ResolveRemediationApproval(r.Context(), id, storage.ApprovalStatusDenied, approverID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "approval is not pending", http.StatusConflict)
			return
		}
		s.logger.Error("resolve remediation approval (deny)", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.emitRemediationSafetyEvent(r.Context(), updated.TenantID, EventRemediationApprovalDenied, map[string]any{
		"approval_id": updated.ID.String(),
		"tenant_id":   updated.TenantID.String(),
		"node_id":     updated.NodeID.String(),
		"rule_id":     updated.RuleID,
		"severity":    updated.Severity,
		"approver_id": approverID.String(),
	})

	writeJSON(w, http.StatusOK, approvalToResponse(updated))

	s.recordAudit(r.Context(), s.systemActor(), updated.TenantID, "remediation.approval_denied", "remediation_approval", updated.ID.String(), map[string]any{
		"rule_id":  updated.RuleID,
		"node_id":  updated.NodeID.String(),
		"severity": updated.Severity,
	})
}

// reapExpiredRemediationApprovals flips any pending approvals past their
// expires_at to expired. Intended to be called from a periodic reaper job; it
// is also safe to invoke inline from tests.
func (s *Server) reapExpiredRemediationApprovals(ctx context.Context) (int, error) {
	if s.store == nil {
		return 0, errors.New("store unavailable")
	}
	// Pull expiring rows first so we can fire webhooks per row. This is
	// best-effort — a concurrent reaper may race; both will update to the same
	// terminal state so the race is harmless.
	pending, _, err := s.store.ListRemediationApprovals(ctx, storage.ListRemediationApprovalsFilter{
		Status: storage.ApprovalStatusPending,
	}, 0, 0)
	if err != nil {
		s.logger.Warn("list pending approvals for reaper", zap.Error(err))
	}
	now := time.Now().UTC()
	n, err := s.store.ExpireRemediationApprovals(ctx, now)
	if err != nil {
		return 0, err
	}
	for i := range pending {
		a := pending[i]
		if a.ExpiresAt.After(now) {
			continue
		}
		s.emitRemediationSafetyEvent(ctx, a.TenantID, EventRemediationApprovalExpired, map[string]any{
			"approval_id": a.ID.String(),
			"tenant_id":   a.TenantID.String(),
			"node_id":     a.NodeID.String(),
			"rule_id":     a.RuleID,
			"severity":    a.Severity,
		})
	}
	return n, nil
}

func approvalToResponse(a *storage.RemediationApproval) remediationApprovalResponse {
	resp := remediationApprovalResponse{
		ID:        a.ID.String(),
		TenantID:  a.TenantID.String(),
		NodeID:    a.NodeID.String(),
		RuleID:    a.RuleID,
		ScriptID:  a.ScriptID.String(),
		Severity:  a.Severity,
		Status:    string(a.Status),
		CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: a.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if a.ApprovedBy != nil {
		s := a.ApprovedBy.String()
		resp.ApprovedBy = &s
	}
	if a.ApprovedAt != nil {
		t := a.ApprovedAt.UTC().Format(time.RFC3339)
		resp.ApprovedAt = &t
	}
	if len(a.TaskPayload) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(a.TaskPayload, &payload); err == nil {
			resp.TaskPayload = payload
		}
	}
	return resp
}
