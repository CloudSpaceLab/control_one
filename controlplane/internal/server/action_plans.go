package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type actionPlanStore interface {
	CreateActionPlan(context.Context, storage.CreateActionPlanParams) (*storage.ActionPlan, error)
	GetActionPlan(context.Context, uuid.UUID) (*storage.ActionPlan, error)
	ListActionPlans(context.Context, storage.ListActionPlansFilter, int, int) ([]storage.ActionPlan, int, error)
	UpdateActionPlanState(context.Context, uuid.UUID, storage.ActionPlanState) (*storage.ActionPlan, error)
	CreateActionPlanApproval(context.Context, storage.CreateActionPlanApprovalParams) (*storage.ActionPlanApproval, error)
	ListActionPlanApprovals(context.Context, uuid.UUID) ([]storage.ActionPlanApproval, error)
	CreateActionReceipt(context.Context, storage.CreateActionReceiptParams) (*storage.ActionReceipt, error)
	ListActionReceipts(context.Context, uuid.UUID) ([]storage.ActionReceipt, error)
}

type createActionPlanRequest struct {
	TenantID          string         `json:"tenant_id"`
	NodeID            *string        `json:"node_id,omitempty"`
	Domain            string         `json:"domain"`
	ActionKind        string         `json:"action_kind"`
	State             string         `json:"state,omitempty"`
	Risk              string         `json:"risk,omitempty"`
	Scope             map[string]any `json:"scope,omitempty"`
	Diff              map[string]any `json:"diff,omitempty"`
	RequiredApprovals map[string]any `json:"required_approvals,omitempty"`
	MaintenanceWindow map[string]any `json:"maintenance_window,omitempty"`
	RollbackPlan      map[string]any `json:"rollback_plan,omitempty"`
	VerificationPlan  map[string]any `json:"verification_plan,omitempty"`
	IdempotencyKey    string         `json:"idempotency_key,omitempty"`
	SourceRef         map[string]any `json:"source_ref,omitempty"`
}

type createActionReceiptRequest struct {
	NodeID       *string        `json:"node_id,omitempty"`
	JobID        *string        `json:"job_id,omitempty"`
	State        string         `json:"state"`
	Receipt      map[string]any `json:"receipt,omitempty"`
	Verification map[string]any `json:"verification,omitempty"`
	RollbackRef  string         `json:"rollback_ref,omitempty"`
	Error        string         `json:"error,omitempty"`
}

type createActionPlanApprovalRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}

type actionPlanResponse struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenant_id"`
	NodeID            *string        `json:"node_id,omitempty"`
	Domain            string         `json:"domain"`
	ActionKind        string         `json:"action_kind"`
	State             string         `json:"state"`
	Risk              string         `json:"risk"`
	Scope             map[string]any `json:"scope"`
	Diff              map[string]any `json:"diff"`
	RequiredApprovals map[string]any `json:"required_approvals"`
	MaintenanceWindow map[string]any `json:"maintenance_window"`
	RollbackPlan      map[string]any `json:"rollback_plan"`
	VerificationPlan  map[string]any `json:"verification_plan"`
	IdempotencyKey    *string        `json:"idempotency_key,omitempty"`
	CreatedBy         *string        `json:"created_by,omitempty"`
	SourceRef         map[string]any `json:"source_ref"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type actionPlanApprovalResponse struct {
	ID           string   `json:"id"`
	ActionPlanID string   `json:"action_plan_id"`
	TenantID     string   `json:"tenant_id"`
	Decision     string   `json:"decision"`
	ActorID      *string  `json:"actor_id,omitempty"`
	ActorSubject string   `json:"actor_subject,omitempty"`
	ActorRoles   []string `json:"actor_roles"`
	Note         string   `json:"note,omitempty"`
	CreatedAt    string   `json:"created_at"`
}

type actionPlanApprovalResultResponse struct {
	Approval       actionPlanApprovalResponse   `json:"approval"`
	ActionPlan     actionPlanResponse           `json:"action_plan"`
	Approvals      []actionPlanApprovalResponse `json:"approvals"`
	ApprovedCount  int                          `json:"approved_count"`
	RequiredCount  int                          `json:"required_count"`
	StateChanged   bool                         `json:"state_changed"`
	NextActionHint string                       `json:"next_action_hint,omitempty"`
}

type actionReceiptResponse struct {
	ID           string         `json:"id"`
	ActionPlanID string         `json:"action_plan_id"`
	TenantID     string         `json:"tenant_id"`
	NodeID       *string        `json:"node_id,omitempty"`
	JobID        *string        `json:"job_id,omitempty"`
	State        string         `json:"state"`
	Receipt      map[string]any `json:"receipt"`
	Verification map[string]any `json:"verification"`
	RollbackRef  string         `json:"rollback_ref,omitempty"`
	Error        string         `json:"error,omitempty"`
	CreatedAt    string         `json:"created_at"`
}

func (s *Server) handleActionPlansCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleInvestigator, roleAdmin)
		if !ok {
			return
		}
		s.handleListActionPlans(w, r, principal)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		s.handleCreateActionPlan(w, r, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleActionPlanSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/action-plans/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(trimmed, "/")
	planID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid action plan id", http.StatusBadRequest)
		return
	}
	if len(segments) == 1 {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleInvestigator, roleAdmin)
		if !ok {
			return
		}
		s.handleGetActionPlan(w, r, principal, planID)
		return
	}
	if len(segments) == 2 && segments[1] == "receipts" {
		s.handleActionPlanReceipts(w, r, planID)
		return
	}
	if len(segments) == 2 && segments[1] == "approvals" {
		s.handleActionPlanApprovals(w, r, planID)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleListActionPlans(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	store, ok := s.actionPlanStore(w)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleInvestigator, roleAdmin)
	if !ok {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.ListActionPlansFilter{
		TenantID:   tenantID,
		Domain:     strings.TrimSpace(r.URL.Query().Get("domain")),
		ActionKind: strings.TrimSpace(r.URL.Query().Get("action_kind")),
		State:      storage.ActionPlanState(strings.TrimSpace(r.URL.Query().Get("state"))),
	}
	if rawNodeID := strings.TrimSpace(r.URL.Query().Get("node_id")); rawNodeID != "" {
		nodeID, err := uuid.Parse(rawNodeID)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = nodeID
	}
	rows, total, err := store.ListActionPlans(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list action plans", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]actionPlanResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, newActionPlanResponse(row))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[actionPlanResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	})
}

func (s *Server) handleCreateActionPlan(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	store, ok := s.actionPlanStore(w)
	if !ok {
		return
	}
	var req createActionPlanRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleOperator, roleAdmin) {
		return
	}
	nodeID, err := optionalUUIDString(req.NodeID, "node_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if nodeID != nil {
		node, err := s.store.GetNode(r.Context(), *nodeID)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if node == nil || node.TenantID != tenantID {
			http.Error(w, "node does not belong to tenant", http.StatusBadRequest)
			return
		}
	}
	if err := enforceActionPlanCreatePolicy(&req, principal); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var createdBy *uuid.UUID
	if parsed, err := uuid.Parse(strings.TrimSpace(principal.Subject)); err == nil && parsed != uuid.Nil {
		createdBy = &parsed
	}
	created, err := store.CreateActionPlan(r.Context(), storage.CreateActionPlanParams{
		TenantID:          tenantID,
		NodeID:            nodeID,
		Domain:            req.Domain,
		ActionKind:        req.ActionKind,
		State:             storage.ActionPlanState(req.State),
		Risk:              req.Risk,
		Scope:             req.Scope,
		Diff:              req.Diff,
		RequiredApprovals: req.RequiredApprovals,
		MaintenanceWindow: req.MaintenanceWindow,
		RollbackPlan:      req.RollbackPlan,
		VerificationPlan:  req.VerificationPlan,
		IdempotencyKey:    req.IdempotencyKey,
		CreatedBy:         createdBy,
		SourceRef:         req.SourceRef,
	})
	if err != nil {
		s.logger.Error("create action plan", zap.Error(err))
		http.Error(w, fmt.Sprintf("create action plan failed: %v", err), http.StatusBadRequest)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "action_plan.created", "action_plan", created.ID.String(), map[string]any{
		"domain":      created.Domain,
		"action_kind": created.ActionKind,
		"state":       string(created.State),
	})
	writeJSON(w, http.StatusCreated, newActionPlanResponse(*created))
}

func (s *Server) handleGetActionPlan(w http.ResponseWriter, r *http.Request, principal *auth.Principal, planID uuid.UUID) {
	plan, store, ok := s.loadAuthorizedActionPlan(w, r, principal, planID, roleViewer, roleOperator, roleInvestigator, roleAdmin)
	if !ok {
		return
	}
	_ = store
	writeJSON(w, http.StatusOK, newActionPlanResponse(*plan))
}

func (s *Server) handleActionPlanReceipts(w http.ResponseWriter, r *http.Request, planID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleInvestigator, roleAdmin)
		if !ok {
			return
		}
		s.handleListActionReceipts(w, r, principal, planID)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		s.handleCreateActionReceipt(w, r, principal, planID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleActionPlanApprovals(w http.ResponseWriter, r *http.Request, planID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleInvestigator, roleAdmin)
		if !ok {
			return
		}
		s.handleListActionPlanApprovals(w, r, principal, planID)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		s.handleCreateActionPlanApproval(w, r, principal, planID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListActionPlanApprovals(w http.ResponseWriter, r *http.Request, principal *auth.Principal, planID uuid.UUID) {
	_, store, ok := s.loadAuthorizedActionPlan(w, r, principal, planID, roleViewer, roleOperator, roleInvestigator, roleAdmin)
	if !ok {
		return
	}
	rows, err := store.ListActionPlanApprovals(r.Context(), planID)
	if err != nil {
		s.logger.Error("list action plan approvals", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]actionPlanApprovalResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, newActionPlanApprovalResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (s *Server) handleCreateActionPlanApproval(w http.ResponseWriter, r *http.Request, principal *auth.Principal, planID uuid.UUID) {
	plan, store, ok := s.loadAuthorizedActionPlan(w, r, principal, planID, roleOperator, roleAdmin)
	if !ok {
		return
	}
	var req createActionPlanApprovalRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	decision, err := normalizeActionPlanDecision(req.Decision)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := enforceActionPlanApprovalPolicy(*plan, principal, decision); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, errActionPlanApprovalConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	approval, err := store.CreateActionPlanApproval(r.Context(), storage.CreateActionPlanApprovalParams{
		ActionPlanID: plan.ID,
		TenantID:     plan.TenantID,
		Decision:     decision,
		ActorID:      principalUUID(principal),
		ActorSubject: actionPlanPrincipalSubject(principal),
		ActorRoles:   normalizedPrincipalRoles(principal),
		Note:         req.Note,
	})
	if err != nil {
		s.logger.Error("create action plan approval", zap.Error(err))
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already recorded") {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	approvals, err := store.ListActionPlanApprovals(r.Context(), planID)
	if err != nil {
		s.logger.Error("list action plan approvals after create", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	required := requiredActionPlanApprovalCount(*plan)
	approvedCount := distinctActionPlanApprovalCount(approvals)
	stateChanged := false
	current := *plan
	if decision == "denied" {
		if updated, err := store.UpdateActionPlanState(r.Context(), planID, storage.ActionPlanStateCancelled); err != nil {
			s.logger.Error("cancel denied action plan", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if updated != nil {
			current = *updated
			stateChanged = true
		}
	} else if approvedCount >= required {
		if updated, err := store.UpdateActionPlanState(r.Context(), planID, storage.ActionPlanStateApproved); err != nil {
			s.logger.Error("approve action plan", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if updated != nil {
			current = *updated
			stateChanged = true
		}
	}
	s.recordAudit(r.Context(), principal, plan.TenantID, "action_plan.approval."+decision, "action_plan", planID.String(), map[string]any{
		"approval_id":    approval.ID.String(),
		"approved_count": approvedCount,
		"required_count": required,
		"state":          string(current.State),
	})
	approvalResponses := make([]actionPlanApprovalResponse, 0, len(approvals))
	for _, row := range approvals {
		approvalResponses = append(approvalResponses, newActionPlanApprovalResponse(row))
	}
	result := actionPlanApprovalResultResponse{
		Approval:      newActionPlanApprovalResponse(*approval),
		ActionPlan:    newActionPlanResponse(current),
		Approvals:     approvalResponses,
		ApprovedCount: approvedCount,
		RequiredCount: required,
		StateChanged:  stateChanged,
	}
	if decision == "approved" && approvedCount < required {
		result.NextActionHint = "additional_approval_required"
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleListActionReceipts(w http.ResponseWriter, r *http.Request, principal *auth.Principal, planID uuid.UUID) {
	_, store, ok := s.loadAuthorizedActionPlan(w, r, principal, planID, roleViewer, roleOperator, roleInvestigator, roleAdmin)
	if !ok {
		return
	}
	rows, err := store.ListActionReceipts(r.Context(), planID)
	if err != nil {
		s.logger.Error("list action receipts", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]actionReceiptResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, newActionReceiptResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (s *Server) handleCreateActionReceipt(w http.ResponseWriter, r *http.Request, principal *auth.Principal, planID uuid.UUID) {
	plan, store, ok := s.loadAuthorizedActionPlan(w, r, principal, planID, roleOperator, roleAdmin)
	if !ok {
		return
	}
	var req createActionReceiptRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	nodeID, err := optionalUUIDString(req.NodeID, "node_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if nodeID != nil {
		node, err := s.store.GetNode(r.Context(), *nodeID)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if node == nil || node.TenantID != plan.TenantID {
			http.Error(w, "node does not belong to tenant", http.StatusBadRequest)
			return
		}
	}
	jobID, err := optionalUUIDString(req.JobID, "job_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	receipt, err := store.CreateActionReceipt(r.Context(), storage.CreateActionReceiptParams{
		ActionPlanID: planID,
		TenantID:     plan.TenantID,
		NodeID:       nodeID,
		JobID:        jobID,
		State:        storage.ActionPlanState(req.State),
		Receipt:      req.Receipt,
		Verification: req.Verification,
		RollbackRef:  req.RollbackRef,
		Error:        req.Error,
	})
	if err != nil {
		s.logger.Error("create action receipt", zap.Error(err))
		http.Error(w, fmt.Sprintf("create action receipt failed: %v", err), http.StatusBadRequest)
		return
	}
	s.recordAudit(r.Context(), principal, plan.TenantID, "action_receipt.created", "action_plan", planID.String(), map[string]any{
		"receipt_id": receipt.ID.String(),
		"state":      string(receipt.State),
	})
	writeJSON(w, http.StatusCreated, newActionReceiptResponse(*receipt))
}

var errActionPlanApprovalConflict = errors.New("action plan approval conflict")

func normalizeActionPlanDecision(decision string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", "approved":
		return "approved", nil
	case "deny", "denied", "reject", "rejected":
		return "denied", nil
	default:
		return "", fmt.Errorf("invalid action plan approval decision %q", decision)
	}
}

func enforceActionPlanApprovalPolicy(plan storage.ActionPlan, principal *auth.Principal, decision string) error {
	switch plan.State {
	case storage.ActionPlanStateProposed, storage.ActionPlanStateNeedsApproval:
	default:
		return fmt.Errorf("%w: action plan is %s", errActionPlanApprovalConflict, plan.State)
	}
	if decision != "approved" {
		return nil
	}
	if !actionPlanPrincipalHasApproverRole(principal, plan.RequiredApprovals) {
		return errors.New("principal does not satisfy action plan approver role requirements")
	}
	if actionPlanRequiresSeparationOfDuties(plan.RequiredApprovals) && actionPlanPrincipalCreatedPlan(plan, principal) {
		return errors.New("separation of duties requires an approver different from the plan creator")
	}
	return nil
}

func actionPlanPrincipalHasApproverRole(principal *auth.Principal, required map[string]any) bool {
	if hasRole(principal, roleAdmin) {
		return true
	}
	roles := actionPlanApprovalRoles(required)
	if len(roles) == 0 {
		return principalHasAnyRole(principal, roleOperator, roleAdmin)
	}
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" {
			continue
		}
		if hasRole(principal, role) {
			return true
		}
	}
	return false
}

func actionPlanApprovalRoles(required map[string]any) []string {
	for _, key := range []string{"roles", "required_roles", "approver_roles"} {
		if roles := actionPlanStringList(required[key]); len(roles) > 0 {
			return roles
		}
	}
	return nil
}

func actionPlanStringList(value any) []string {
	var out []string
	switch v := value.(type) {
	case string:
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	case []string:
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	case []any:
		for _, item := range v {
			out = append(out, actionPlanStringList(item)...)
		}
	}
	return out
}

func actionPlanRequiresSeparationOfDuties(required map[string]any) bool {
	return actionPlanTruthy(required["separation_of_duties"])
}

func actionPlanPrincipalCreatedPlan(plan storage.ActionPlan, principal *auth.Principal) bool {
	if principal == nil {
		return false
	}
	if plan.CreatedBy.Valid {
		if id := principalUUID(principal); id != nil && *id == plan.CreatedBy.UUID {
			return true
		}
	}
	subject := strings.ToLower(actionPlanPrincipalSubject(principal))
	if subject == "" {
		return false
	}
	for _, raw := range []any{
		plan.RequiredApprovals["created_by_subject"],
		plan.SourceRef["created_by_subject"],
		plan.SourceRef["creator_subject"],
	} {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(raw)), subject) {
			return true
		}
	}
	return false
}

func requiredActionPlanApprovalCount(plan storage.ActionPlan) int {
	if n := actionPlanInt(plan.RequiredApprovals["min_approvers"]); n > 0 {
		return n
	}
	if actionPlanRiskRequiresDualApproval(plan.Risk) {
		return 2
	}
	return 1
}

func distinctActionPlanApprovalCount(approvals []storage.ActionPlanApproval) int {
	seen := map[string]struct{}{}
	for _, approval := range approvals {
		if approval.Decision != "approved" {
			continue
		}
		key := strings.TrimSpace(approval.ActorKey)
		if key == "" {
			key = approval.ActorSubject
		}
		if key == "" && approval.ActorID.Valid {
			key = approval.ActorID.UUID.String()
		}
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
	}
	return len(seen)
}

func actionPlanPrincipalSubject(principal *auth.Principal) string {
	if subject := principalSubject(principal); subject != "" {
		return subject
	}
	if principal != nil {
		return strings.TrimSpace(principal.Name)
	}
	return ""
}

func normalizedPrincipalRoles(principal *auth.Principal) []string {
	if principal == nil {
		return nil
	}
	out := make([]string, 0, len(principal.Roles))
	seen := map[string]struct{}{}
	for _, role := range principal.Roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	return out
}

func newActionPlanApprovalResponse(approval storage.ActionPlanApproval) actionPlanApprovalResponse {
	return actionPlanApprovalResponse{
		ID:           approval.ID.String(),
		ActionPlanID: approval.ActionPlanID.String(),
		TenantID:     approval.TenantID.String(),
		Decision:     approval.Decision,
		ActorID:      uuidNullString(approval.ActorID),
		ActorSubject: approval.ActorSubject,
		ActorRoles:   append([]string{}, approval.ActorRoles...),
		Note:         approval.Note,
		CreatedAt:    approval.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) loadAuthorizedActionPlan(w http.ResponseWriter, r *http.Request, principal *auth.Principal, planID uuid.UUID, roles ...string) (*storage.ActionPlan, actionPlanStore, bool) {
	store, ok := s.actionPlanStore(w)
	if !ok {
		return nil, nil, false
	}
	plan, err := store.GetActionPlan(r.Context(), planID)
	if err != nil {
		s.logger.Error("get action plan", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, nil, false
	}
	if plan == nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	if !s.requireTenantAccess(w, r, principal, plan.TenantID, roles...) {
		return nil, nil, false
	}
	return plan, store, true
}

func enforceActionPlanCreatePolicy(req *createActionPlanRequest, principal *auth.Principal) error {
	state := storage.ActionPlanState(strings.TrimSpace(req.State))
	if state == "" {
		state = storage.ActionPlanStateProposed
	}
	switch state {
	case storage.ActionPlanStateDraft, storage.ActionPlanStateProposed, storage.ActionPlanStateNeedsApproval:
	default:
		return fmt.Errorf("action plan state %q cannot be created directly; use the approval and receipt workflow", state)
	}
	risk := strings.ToLower(strings.TrimSpace(req.Risk))
	if risk == "" {
		risk = "medium"
	}
	if actionPlanRiskRequiresDualApproval(risk) {
		if state != storage.ActionPlanStateNeedsApproval {
			return fmt.Errorf("%s risk action plans must start in needs_approval", risk)
		}
		if err := validateActionPlanRequiredApprovals(req.RequiredApprovals); err != nil {
			return err
		}
		if value, ok := req.RequiredApprovals["separation_of_duties"]; ok && !actionPlanTruthy(value) {
			return errors.New("high risk action plans require separation_of_duties=true")
		}
		req.RequiredApprovals["separation_of_duties"] = true
		if subject := principalSubject(principal); subject != "" {
			req.RequiredApprovals["created_by_subject"] = subject
		}
	}
	if subject := principalSubject(principal); subject != "" {
		if req.SourceRef == nil {
			req.SourceRef = map[string]any{}
		}
		if _, exists := req.SourceRef["created_by_subject"]; !exists {
			req.SourceRef["created_by_subject"] = subject
		}
	}
	return nil
}

func actionPlanRiskRequiresDualApproval(risk string) bool {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "high", "critical":
		return true
	default:
		return false
	}
}

func validateActionPlanRequiredApprovals(required map[string]any) error {
	if len(required) == 0 {
		return errors.New("high risk action plans require required_approvals")
	}
	if !actionPlanApprovalRolesPresent(required) {
		return errors.New("high risk action plans require approver roles")
	}
	if actionPlanInt(required["min_approvers"]) < 2 {
		return errors.New("high risk action plans require min_approvers >= 2")
	}
	return nil
}

func actionPlanApprovalRolesPresent(required map[string]any) bool {
	for _, key := range []string{"roles", "required_roles", "approver_roles"} {
		if actionPlanValuePresent(required[key]) {
			return true
		}
	}
	return false
}

func actionPlanValuePresent(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []string:
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if actionPlanValuePresent(item) {
				return true
			}
		}
	}
	return false
}

func actionPlanInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(v))
		return parsed
	}
	return 0
}

func actionPlanTruthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "true" || v == "yes" || v == "1"
	case int:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}

func (s *Server) actionPlanStore(w http.ResponseWriter) (actionPlanStore, bool) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	store, ok := s.store.(actionPlanStore)
	if !ok {
		http.Error(w, "action plan store unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	return store, true
}

func newActionPlanResponse(plan storage.ActionPlan) actionPlanResponse {
	return actionPlanResponse{
		ID:                plan.ID.String(),
		TenantID:          plan.TenantID.String(),
		NodeID:            uuidNullString(plan.NodeID),
		Domain:            plan.Domain,
		ActionKind:        plan.ActionKind,
		State:             string(plan.State),
		Risk:              plan.Risk,
		Scope:             nonNilMap(plan.Scope),
		Diff:              nonNilMap(plan.Diff),
		RequiredApprovals: nonNilMap(plan.RequiredApprovals),
		MaintenanceWindow: nonNilMap(plan.MaintenanceWindow),
		RollbackPlan:      nonNilMap(plan.RollbackPlan),
		VerificationPlan:  nonNilMap(plan.VerificationPlan),
		IdempotencyKey:    nullStringPtrFromSQL(plan.IdempotencyKey),
		CreatedBy:         uuidNullString(plan.CreatedBy),
		SourceRef:         nonNilMap(plan.SourceRef),
		CreatedAt:         plan.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         plan.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func newActionReceiptResponse(receipt storage.ActionReceipt) actionReceiptResponse {
	return actionReceiptResponse{
		ID:           receipt.ID.String(),
		ActionPlanID: receipt.ActionPlanID.String(),
		TenantID:     receipt.TenantID.String(),
		NodeID:       uuidNullString(receipt.NodeID),
		JobID:        uuidNullString(receipt.JobID),
		State:        string(receipt.State),
		Receipt:      nonNilMap(receipt.Receipt),
		Verification: nonNilMap(receipt.Verification),
		RollbackRef:  receipt.RollbackRef,
		Error:        receipt.Error,
		CreatedAt:    receipt.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func optionalUUIDString(raw *string, field string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, fmt.Errorf("invalid %s", field)
	}
	return &parsed, nil
}

func uuidNullString(id uuid.NullUUID) *string {
	if !id.Valid {
		return nil
	}
	out := id.UUID.String()
	return &out
}

func nullStringPtrFromSQL(value sql.NullString) *string {
	if value.Valid {
		out := value.String
		return &out
	}
	return nil
}

func nonNilMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}
