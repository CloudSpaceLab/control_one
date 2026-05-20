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

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
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
		s.handleApproveRemediationApproval(w, r, approvalID, principal)
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
		s.handleDenyRemediationApproval(w, r, approvalID, principal)
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

	tenantID, ok := requiredTenantIDQuery(w, r)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer, roleOperator, roleAdmin) {
		return
	}
	filter := storage.ListRemediationApprovalsFilter{TenantID: tenantID}
	if v := strings.TrimSpace(r.URL.Query().Get("status")); v != "" {
		filter.Status = storage.ApprovalStatus(strings.ToLower(v))
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
	if !remediationApprovalMatchesTenantQuery(w, r, approval) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	if !s.requireTenantAccess(w, r, principal, approval.TenantID, roleViewer, roleOperator, roleAdmin) {
		return
	}
	writeJSON(w, http.StatusOK, approvalToResponse(approval))
}

func (s *Server) handleApproveRemediationApproval(w http.ResponseWriter, r *http.Request, id uuid.UUID, principal *auth.Principal) {
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
	if !remediationApprovalMatchesTenantQuery(w, r, approval) {
		return
	}
	if !s.requireTenantAccess(w, r, principal, approval.TenantID, roleOperator, roleAdmin) {
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

	script, err := s.store.GetRemediationScriptByID(r.Context(), approval.ScriptID)
	if err != nil {
		s.logger.Error("load script for approved remediation", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if err := validateRemediationApprovalScriptBinding(approval, script); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err := s.validateRemediationApprovalDispatchGates(r.Context(), approval); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	approverID := uuid.Nil
	if principal != nil {
		approverID, _ = uuid.Parse(strings.TrimSpace(principal.Subject))
	}

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

	s.recordAudit(r.Context(), principal, updated.TenantID, "remediation.approval_approved", "remediation_approval", updated.ID.String(), map[string]any{
		"rule_id":  updated.RuleID,
		"node_id":  updated.NodeID.String(),
		"severity": updated.Severity,
	})
}

func requiredTenantIDQuery(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if raw == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return uuid.Nil, false
	}
	tenantID, err := uuid.Parse(raw)
	if err != nil || tenantID == uuid.Nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return tenantID, true
}

func remediationApprovalMatchesTenantQuery(w http.ResponseWriter, r *http.Request, approval *storage.RemediationApproval) bool {
	tenantID, ok := requiredTenantIDQuery(w, r)
	if !ok {
		return false
	}
	if approval == nil || approval.TenantID != tenantID {
		http.NotFound(w, r)
		return false
	}
	return true
}

func validateRemediationApprovalScriptBinding(approval *storage.RemediationApproval, script *storage.RemediationScript) error {
	if approval == nil {
		return errors.New("approval unavailable")
	}
	if script == nil {
		return errors.New("remediation script no longer exists")
	}
	if !script.Enabled {
		return errors.New("remediation script is disabled")
	}
	var payload map[string]any
	if err := json.Unmarshal(approval.TaskPayload, &payload); err != nil {
		return errors.New("task payload corrupted")
	}
	if got := strings.TrimSpace(detailsString(payload, "script_checksum", "")); got == "" || got != remediationScriptArtifactChecksum(*script) {
		return errors.New("approved remediation script checksum changed")
	}
	if got, ok := intFromPayload(payload, "script_version"); !ok || got != script.Version {
		return errors.New("approved remediation script version changed")
	}
	currentDescriptor := remediationDescriptorFromScript(*script)
	if got := strings.TrimSpace(detailsString(payload, "safety_class", "")); got == "" || !strings.EqualFold(got, currentDescriptor.SafetyClass) {
		return errors.New("approved remediation safety class changed")
	}
	return nil
}

func (s *Server) validateRemediationApprovalDispatchGates(ctx context.Context, approval *storage.RemediationApproval) error {
	if s == nil || s.store == nil {
		return errors.New("remediation dispatch gates unavailable")
	}
	if approval == nil {
		return errors.New("approval unavailable")
	}
	now := time.Now().UTC()
	if approval.NodeID != uuid.Nil {
		node, err := s.store.GetNode(ctx, approval.NodeID)
		if err != nil {
			s.logger.Warn("load node for approved remediation gates",
				zap.String("node_id", approval.NodeID.String()),
				zap.Error(err),
			)
			return errors.New("remediation target gate unavailable")
		}
		if node == nil {
			return errors.New("remediation target node no longer exists")
		}
		if node.TenantID != approval.TenantID {
			return errors.New("remediation target node is outside approval tenant")
		}
		posture := nodeIsolationPostureFromNode(*node, now)
		if posture.Active && posture.Mode == isolationModeAirgapped {
			return errors.New("remediation target is airgapped")
		}
		if posture.Active && posture.Mode == isolationModeWhitelist && !stringSliceContainsFold(posture.AllowedApplications, "remediation") {
			return errors.New("remediation target is whitelist-only and remediation is not allowlisted")
		}
		if node.Labels != nil {
			if val, ok := node.Labels["remediation"]; ok {
				if str, ok := val.(string); ok && strings.EqualFold(strings.TrimSpace(str), "manual-only") {
					return errors.New("remediation target is labelled remediation=manual-only")
				}
			}
		}
	}

	cfg, err := s.store.GetTenantRemediationConfig(ctx, approval.TenantID)
	if err != nil {
		s.logger.Warn("load tenant remediation config for approval dispatch",
			zap.String("tenant_id", approval.TenantID.String()),
			zap.Error(err),
		)
		return errors.New("tenant remediation policy gate unavailable")
	}
	if cfg == nil {
		defaults := storage.DefaultTenantRemediationConfig(approval.TenantID)
		cfg = &defaults
	}
	severity := strings.ToLower(strings.TrimSpace(approval.Severity))
	insideChangeWindow := storage.IsInsideChangeWindow(cfg.ChangeWindows, now)
	criticalOverrideAllowed := cfg.CriticalOverride && severity == "critical"
	if !insideChangeWindow && !criticalOverrideAllowed {
		return errors.New("remediation approval cannot dispatch outside an active change window")
	}

	breaker, err := s.store.GetCircuitBreakerState(ctx, approval.TenantID, approval.RuleID)
	if err != nil {
		s.logger.Warn("load circuit breaker for approval dispatch",
			zap.String("tenant_id", approval.TenantID.String()),
			zap.String("rule_id", approval.RuleID),
			zap.Error(err),
		)
		return errors.New("remediation circuit breaker gate unavailable")
	}
	if breaker != nil && breaker.AckedAt == nil {
		return errors.New("remediation circuit breaker is tripped")
	}
	return nil
}

func intFromPayload(payload map[string]any, key string) (int, bool) {
	if payload == nil {
		return 0, false
	}
	switch value := payload[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		n, err := value.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func (s *Server) handleDenyRemediationApproval(w http.ResponseWriter, r *http.Request, id uuid.UUID, principal *auth.Principal) {
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
	if !remediationApprovalMatchesTenantQuery(w, r, approval) {
		return
	}
	if !s.requireTenantAccess(w, r, principal, approval.TenantID, roleOperator, roleAdmin) {
		return
	}
	if approval.Status != storage.ApprovalStatusPending {
		http.Error(w, "approval is not pending", http.StatusConflict)
		return
	}

	approverID := uuid.Nil
	if principal != nil {
		approverID, _ = uuid.Parse(strings.TrimSpace(principal.Subject))
	}

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

	s.recordAudit(r.Context(), principal, updated.TenantID, "remediation.approval_denied", "remediation_approval", updated.ID.String(), map[string]any{
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
			delete(payload, "script_content")
			delete(payload, "env")
			resp.TaskPayload = payload
		}
	}
	return resp
}
