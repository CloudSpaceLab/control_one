package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ActionPlanState captures the shared lifecycle for operator-visible actions.
type ActionPlanState string

const (
	ActionPlanStateDraft         ActionPlanState = "draft"
	ActionPlanStateProposed      ActionPlanState = "proposed"
	ActionPlanStateNeedsApproval ActionPlanState = "needs_approval"
	ActionPlanStateApproved      ActionPlanState = "approved"
	ActionPlanStateQueued        ActionPlanState = "queued"
	ActionPlanStateRunning       ActionPlanState = "running"
	ActionPlanStateSucceeded     ActionPlanState = "succeeded"
	ActionPlanStateFailed        ActionPlanState = "failed"
	ActionPlanStateVerified      ActionPlanState = "verified"
	ActionPlanStateRolledBack    ActionPlanState = "rolled_back"
	ActionPlanStateCancelled     ActionPlanState = "cancelled"
)

// ActionPlan is the provider-neutral plan contract used by remediation surfaces.
type ActionPlan struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	NodeID            uuid.NullUUID
	Domain            string
	ActionKind        string
	State             ActionPlanState
	Risk              string
	Scope             map[string]any
	Diff              map[string]any
	RequiredApprovals map[string]any
	MaintenanceWindow map[string]any
	RollbackPlan      map[string]any
	VerificationPlan  map[string]any
	IdempotencyKey    sql.NullString
	CreatedBy         uuid.NullUUID
	SourceRef         map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateActionPlanParams contains the JSONB contract fragments for a plan.
type CreateActionPlanParams struct {
	TenantID          uuid.UUID
	NodeID            *uuid.UUID
	Domain            string
	ActionKind        string
	State             ActionPlanState
	Risk              string
	Scope             map[string]any
	Diff              map[string]any
	RequiredApprovals map[string]any
	MaintenanceWindow map[string]any
	RollbackPlan      map[string]any
	VerificationPlan  map[string]any
	IdempotencyKey    string
	CreatedBy         *uuid.UUID
	SourceRef         map[string]any
}

// ListActionPlansFilter narrows action plan list queries.
type ListActionPlansFilter struct {
	TenantID   uuid.UUID
	NodeID     uuid.UUID
	Domain     string
	ActionKind string
	State      ActionPlanState
}

// ActionReceipt is an append-only execution or verification record.
type ActionReceipt struct {
	ID           uuid.UUID
	ActionPlanID uuid.UUID
	TenantID     uuid.UUID
	NodeID       uuid.NullUUID
	JobID        uuid.NullUUID
	State        ActionPlanState
	Receipt      map[string]any
	Verification map[string]any
	RollbackRef  string
	Error        string
	CreatedAt    time.Time
}

// ActionPlanApproval is an append-only approval/denial decision for a plan.
type ActionPlanApproval struct {
	ID           uuid.UUID
	ActionPlanID uuid.UUID
	TenantID     uuid.UUID
	Decision     string
	ActorID      uuid.NullUUID
	ActorSubject string
	ActorKey     string
	ActorRoles   []string
	Note         string
	CreatedAt    time.Time
}

// CreateActionReceiptParams records execution or verification evidence.
type CreateActionReceiptParams struct {
	ActionPlanID uuid.UUID
	TenantID     uuid.UUID
	NodeID       *uuid.UUID
	JobID        *uuid.UUID
	State        ActionPlanState
	Receipt      map[string]any
	Verification map[string]any
	RollbackRef  string
	Error        string
}

// CreateActionPlanApprovalParams records one operator approval decision.
type CreateActionPlanApprovalParams struct {
	ActionPlanID uuid.UUID
	TenantID     uuid.UUID
	Decision     string
	ActorID      *uuid.UUID
	ActorSubject string
	ActorRoles   []string
	Note         string
}

// CreateActionPlan inserts a durable action plan. A non-empty idempotency key
// returns the existing plan for the same tenant without mutating the contract.
func (s *Store) CreateActionPlan(ctx context.Context, p CreateActionPlanParams) (*ActionPlan, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	domain := strings.TrimSpace(p.Domain)
	if domain == "" {
		return nil, errors.New("domain is required")
	}
	actionKind := strings.TrimSpace(p.ActionKind)
	if actionKind == "" {
		return nil, errors.New("action kind is required")
	}
	state, err := normalizeActionPlanState(p.State, ActionPlanStateProposed)
	if err != nil {
		return nil, err
	}
	risk := strings.TrimSpace(p.Risk)
	if risk == "" {
		risk = "medium"
	}

	scope, err := marshalJSONBMap(p.Scope)
	if err != nil {
		return nil, fmt.Errorf("encode scope: %w", err)
	}
	diff, err := marshalJSONBMap(p.Diff)
	if err != nil {
		return nil, fmt.Errorf("encode diff: %w", err)
	}
	requiredApprovals, err := marshalJSONBMap(p.RequiredApprovals)
	if err != nil {
		return nil, fmt.Errorf("encode required approvals: %w", err)
	}
	maintenanceWindow, err := marshalJSONBMap(p.MaintenanceWindow)
	if err != nil {
		return nil, fmt.Errorf("encode maintenance window: %w", err)
	}
	rollbackPlan, err := marshalJSONBMap(p.RollbackPlan)
	if err != nil {
		return nil, fmt.Errorf("encode rollback plan: %w", err)
	}
	verificationPlan, err := marshalJSONBMap(p.VerificationPlan)
	if err != nil {
		return nil, fmt.Errorf("encode verification plan: %w", err)
	}
	sourceRef, err := marshalJSONBMap(p.SourceRef)
	if err != nil {
		return nil, fmt.Errorf("encode source ref: %w", err)
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO action_plans (
			tenant_id, node_id, domain, action_kind, state, risk,
			scope, diff, required_approvals, maintenance_window,
			rollback_plan, verification_plan, idempotency_key, created_by, source_ref
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (tenant_id, idempotency_key)
		DO UPDATE SET updated_at = action_plans.updated_at
		RETURNING id, tenant_id, node_id, domain, action_kind, state, risk,
		          scope, diff, required_approvals, maintenance_window,
		          rollback_plan, verification_plan, idempotency_key, created_by,
		          source_ref, created_at, updated_at
	`,
		p.TenantID,
		uuidPtrArg(p.NodeID),
		domain,
		actionKind,
		string(state),
		risk,
		scope,
		diff,
		requiredApprovals,
		maintenanceWindow,
		rollbackPlan,
		verificationPlan,
		nullString(p.IdempotencyKey),
		uuidPtrArg(p.CreatedBy),
		sourceRef,
	)
	return scanActionPlan(row)
}

// GetActionPlan fetches a single plan by ID.
func (s *Store) GetActionPlan(ctx context.Context, id uuid.UUID) (*ActionPlan, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, domain, action_kind, state, risk,
		       scope, diff, required_approvals, maintenance_window,
		       rollback_plan, verification_plan, idempotency_key, created_by,
		       source_ref, created_at, updated_at
		FROM action_plans
		WHERE id = $1
	`, id)
	plan, err := scanActionPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return plan, err
}

// ListActionPlans returns tenant-scoped plans in reverse creation order.
func (s *Store) ListActionPlans(ctx context.Context, filter ListActionPlansFilter, limit, offset int) ([]ActionPlan, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if filter.TenantID == uuid.Nil {
		return nil, 0, errors.New("tenant id is required")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"tenant_id = $1"}
	args := []any{filter.TenantID}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.Domain) != "" {
		args = append(args, strings.TrimSpace(filter.Domain))
		clauses = append(clauses, fmt.Sprintf("domain = $%d", len(args)))
	}
	if strings.TrimSpace(filter.ActionKind) != "" {
		args = append(args, strings.TrimSpace(filter.ActionKind))
		clauses = append(clauses, fmt.Sprintf("action_kind = $%d", len(args)))
	}
	if strings.TrimSpace(string(filter.State)) != "" {
		state, err := normalizeActionPlanState(filter.State, "")
		if err != nil {
			return nil, 0, err
		}
		args = append(args, string(state))
		clauses = append(clauses, fmt.Sprintf("state = $%d", len(args)))
	}
	where := strings.Join(clauses, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM action_plans WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count action plans: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, node_id, domain, action_kind, state, risk,
		       scope, diff, required_approvals, maintenance_window,
		       rollback_plan, verification_plan, idempotency_key, created_by,
		       source_ref, created_at, updated_at
		FROM action_plans
		WHERE %s
		ORDER BY created_at DESC
	`, where)
	pagedArgs := append([]any{}, args...)
	if limit > 0 {
		pagedArgs = append(pagedArgs, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(pagedArgs))
	}
	if offset > 0 {
		pagedArgs = append(pagedArgs, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(pagedArgs))
	}

	rows, err := s.db.QueryContext(ctx, query, pagedArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query action plans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ActionPlan
	for rows.Next() {
		plan, err := scanActionPlan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *plan)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UpdateActionPlanState transitions a plan state and returns the updated row.
func (s *Store) UpdateActionPlanState(ctx context.Context, id uuid.UUID, state ActionPlanState) (*ActionPlan, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	normalized, err := normalizeActionPlanState(state, "")
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE action_plans
		SET state = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING id, tenant_id, node_id, domain, action_kind, state, risk,
		          scope, diff, required_approvals, maintenance_window,
		          rollback_plan, verification_plan, idempotency_key, created_by,
		          source_ref, created_at, updated_at
	`, id, string(normalized))
	plan, err := scanActionPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return plan, err
}

func (s *Store) CreateActionPlanApproval(ctx context.Context, p CreateActionPlanApprovalParams) (*ActionPlanApproval, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.ActionPlanID == uuid.Nil {
		return nil, errors.New("action plan id is required")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	decision, err := normalizeActionPlanApprovalDecision(p.Decision)
	if err != nil {
		return nil, err
	}
	actorSubject := strings.TrimSpace(p.ActorSubject)
	actorKey := actionPlanApprovalActorKey(p.ActorID, actorSubject)
	if actorKey == "" {
		return nil, errors.New("approval actor identity is required")
	}
	roles := normalizeActionPlanApprovalRoles(p.ActorRoles)
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO action_plan_approvals (
			action_plan_id, tenant_id, decision, actor_id, actor_subject, actor_key, actor_roles, note
		)
		SELECT p.id, p.tenant_id, $3, $4, $5, $6, $7, $8
		FROM action_plans p
		WHERE p.id = $1 AND p.tenant_id = $2
		ON CONFLICT (action_plan_id, actor_key) DO NOTHING
		RETURNING id, action_plan_id, tenant_id, decision, actor_id, actor_subject, actor_key, actor_roles, note, created_at
	`,
		p.ActionPlanID,
		p.TenantID,
		decision,
		uuidPtrArg(p.ActorID),
		actorSubject,
		actorKey,
		pq.Array(roles),
		strings.TrimSpace(p.Note),
	)
	approval, err := scanActionPlanApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		if plan, getErr := s.GetActionPlan(ctx, p.ActionPlanID); getErr != nil {
			return nil, getErr
		} else if plan == nil || plan.TenantID != p.TenantID {
			return nil, errors.New("action plan not found")
		}
		return nil, errors.New("actor has already recorded an action plan approval decision")
	}
	return approval, err
}

func (s *Store) ListActionPlanApprovals(ctx context.Context, actionPlanID uuid.UUID) ([]ActionPlanApproval, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if actionPlanID == uuid.Nil {
		return nil, errors.New("action plan id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action_plan_id, tenant_id, decision, actor_id, actor_subject, actor_key, actor_roles, note, created_at
		FROM action_plan_approvals
		WHERE action_plan_id = $1
		ORDER BY created_at ASC
	`, actionPlanID)
	if err != nil {
		return nil, fmt.Errorf("query action plan approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ActionPlanApproval
	for rows.Next() {
		approval, err := scanActionPlanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *approval)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateActionReceipt appends execution evidence and mirrors the latest state
// back to the parent plan.
func (s *Store) CreateActionReceipt(ctx context.Context, p CreateActionReceiptParams) (*ActionReceipt, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.ActionPlanID == uuid.Nil {
		return nil, errors.New("action plan id is required")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	state, err := normalizeActionReceiptState(p.State)
	if err != nil {
		return nil, err
	}
	receipt, err := marshalJSONBMap(p.Receipt)
	if err != nil {
		return nil, fmt.Errorf("encode receipt: %w", err)
	}
	verification, err := marshalJSONBMap(p.Verification)
	if err != nil {
		return nil, fmt.Errorf("encode verification: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		INSERT INTO action_receipts (
			action_plan_id, tenant_id, node_id, job_id, state,
			receipt, verification, rollback_ref, error
		)
		SELECT p.id, p.tenant_id, COALESCE($3::uuid, p.node_id), $4::uuid, $5,
		       $6, $7, $8, $9
		FROM action_plans p
		WHERE p.id = $1 AND p.tenant_id = $2
		RETURNING id, action_plan_id, tenant_id, node_id, job_id, state,
		          receipt, verification, rollback_ref, error, created_at
	`,
		p.ActionPlanID,
		p.TenantID,
		uuidPtrArg(p.NodeID),
		uuidPtrArg(p.JobID),
		string(state),
		receipt,
		verification,
		strings.TrimSpace(p.RollbackRef),
		strings.TrimSpace(p.Error),
	)
	out, err := scanActionReceipt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("action plan not found")
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE action_plans
		SET state = $2, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $3
	`, p.ActionPlanID, string(state), p.TenantID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListActionReceipts returns append-only receipts for a plan.
func (s *Store) ListActionReceipts(ctx context.Context, actionPlanID uuid.UUID) ([]ActionReceipt, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if actionPlanID == uuid.Nil {
		return nil, errors.New("action plan id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action_plan_id, tenant_id, node_id, job_id, state,
		       receipt, verification, rollback_ref, error, created_at
		FROM action_receipts
		WHERE action_plan_id = $1
		ORDER BY created_at ASC
	`, actionPlanID)
	if err != nil {
		return nil, fmt.Errorf("query action receipts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ActionReceipt
	for rows.Next() {
		receipt, err := scanActionReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *receipt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanActionPlanApproval(row interface{ Scan(dest ...any) error }) (*ActionPlanApproval, error) {
	var (
		out     ActionPlanApproval
		actorID sql.NullString
		roles   pq.StringArray
	)
	if err := row.Scan(
		&out.ID,
		&out.ActionPlanID,
		&out.TenantID,
		&out.Decision,
		&actorID,
		&out.ActorSubject,
		&out.ActorKey,
		pq.Array(&roles),
		&out.Note,
		&out.CreatedAt,
	); err != nil {
		return nil, err
	}
	out.ActorID = nullUUIDFromSQLString(actorID)
	out.ActorRoles = append([]string{}, roles...)
	return &out, nil
}

func scanActionPlan(row interface{ Scan(dest ...any) error }) (*ActionPlan, error) {
	var (
		out                                                                                ActionPlan
		nodeID, createdBy                                                                  sql.NullString
		state                                                                              string
		scopeRaw, diffRaw, requiredRaw, windowRaw, rollbackRaw, verificationRaw, sourceRaw []byte
	)
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&nodeID,
		&out.Domain,
		&out.ActionKind,
		&state,
		&out.Risk,
		&scopeRaw,
		&diffRaw,
		&requiredRaw,
		&windowRaw,
		&rollbackRaw,
		&verificationRaw,
		&out.IdempotencyKey,
		&createdBy,
		&sourceRaw,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, err
	}
	out.State = ActionPlanState(state)
	out.NodeID = nullUUIDFromSQLString(nodeID)
	out.CreatedBy = nullUUIDFromSQLString(createdBy)

	var err error
	if out.Scope, err = decodeJSONBMap(scopeRaw); err != nil {
		return nil, fmt.Errorf("decode scope: %w", err)
	}
	if out.Diff, err = decodeJSONBMap(diffRaw); err != nil {
		return nil, fmt.Errorf("decode diff: %w", err)
	}
	if out.RequiredApprovals, err = decodeJSONBMap(requiredRaw); err != nil {
		return nil, fmt.Errorf("decode required approvals: %w", err)
	}
	if out.MaintenanceWindow, err = decodeJSONBMap(windowRaw); err != nil {
		return nil, fmt.Errorf("decode maintenance window: %w", err)
	}
	if out.RollbackPlan, err = decodeJSONBMap(rollbackRaw); err != nil {
		return nil, fmt.Errorf("decode rollback plan: %w", err)
	}
	if out.VerificationPlan, err = decodeJSONBMap(verificationRaw); err != nil {
		return nil, fmt.Errorf("decode verification plan: %w", err)
	}
	if out.SourceRef, err = decodeJSONBMap(sourceRaw); err != nil {
		return nil, fmt.Errorf("decode source ref: %w", err)
	}
	return &out, nil
}

func scanActionReceipt(row interface{ Scan(dest ...any) error }) (*ActionReceipt, error) {
	var (
		out                   ActionReceipt
		nodeID, jobID         sql.NullString
		state                 string
		receiptRaw, verifyRaw []byte
	)
	if err := row.Scan(
		&out.ID,
		&out.ActionPlanID,
		&out.TenantID,
		&nodeID,
		&jobID,
		&state,
		&receiptRaw,
		&verifyRaw,
		&out.RollbackRef,
		&out.Error,
		&out.CreatedAt,
	); err != nil {
		return nil, err
	}
	out.NodeID = nullUUIDFromSQLString(nodeID)
	out.JobID = nullUUIDFromSQLString(jobID)
	out.State = ActionPlanState(state)

	var err error
	if out.Receipt, err = decodeJSONBMap(receiptRaw); err != nil {
		return nil, fmt.Errorf("decode receipt: %w", err)
	}
	if out.Verification, err = decodeJSONBMap(verifyRaw); err != nil {
		return nil, fmt.Errorf("decode verification: %w", err)
	}
	return &out, nil
}

func normalizeActionPlanState(state ActionPlanState, fallback ActionPlanState) (ActionPlanState, error) {
	normalized := ActionPlanState(strings.TrimSpace(string(state)))
	if normalized == "" {
		normalized = fallback
	}
	if normalized == "" {
		return "", errors.New("state is required")
	}
	if !validActionPlanState(normalized) {
		return "", fmt.Errorf("invalid action plan state %q", normalized)
	}
	return normalized, nil
}

func normalizeActionReceiptState(state ActionPlanState) (ActionPlanState, error) {
	normalized := ActionPlanState(strings.TrimSpace(string(state)))
	if normalized == "" {
		return "", errors.New("state is required")
	}
	switch normalized {
	case ActionPlanStateQueued,
		ActionPlanStateRunning,
		ActionPlanStateSucceeded,
		ActionPlanStateFailed,
		ActionPlanStateVerified,
		ActionPlanStateRolledBack,
		ActionPlanStateCancelled:
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid action receipt state %q", normalized)
	}
}

func normalizeActionPlanApprovalDecision(decision string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", "approved":
		return "approved", nil
	case "deny", "denied", "reject", "rejected":
		return "denied", nil
	default:
		return "", fmt.Errorf("invalid action plan approval decision %q", decision)
	}
}

func actionPlanApprovalActorKey(actorID *uuid.UUID, actorSubject string) string {
	if actorID != nil && *actorID != uuid.Nil {
		return "user:" + actorID.String()
	}
	if actorSubject = strings.TrimSpace(actorSubject); actorSubject != "" {
		return "subject:" + strings.ToLower(actorSubject)
	}
	return ""
}

func normalizeActionPlanApprovalRoles(roles []string) []string {
	out := make([]string, 0, len(roles))
	seen := map[string]struct{}{}
	for _, role := range roles {
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

func validActionPlanState(state ActionPlanState) bool {
	switch state {
	case ActionPlanStateDraft,
		ActionPlanStateProposed,
		ActionPlanStateNeedsApproval,
		ActionPlanStateApproved,
		ActionPlanStateQueued,
		ActionPlanStateRunning,
		ActionPlanStateSucceeded,
		ActionPlanStateFailed,
		ActionPlanStateVerified,
		ActionPlanStateRolledBack,
		ActionPlanStateCancelled:
		return true
	default:
		return false
	}
}

func uuidPtrArg(id *uuid.UUID) any {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	return *id
}

func nullUUIDFromSQLString(value sql.NullString) uuid.NullUUID {
	if !value.Valid {
		return uuid.NullUUID{}
	}
	parsed, err := uuid.Parse(value.String)
	if err != nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}
}
