package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ApprovalStatus represents the lifecycle state of a remediation approval request.
type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusDenied   ApprovalStatus = "denied"
	ApprovalStatusExpired  ApprovalStatus = "expired"
)

// RemediationApproval is a pending/resolved approval request the operator must
// green-light before a high-severity auto-remediation can enqueue.
type RemediationApproval struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	NodeID      uuid.UUID
	RuleID      string
	ScriptID    uuid.UUID
	Severity    string
	TaskPayload []byte
	Status      ApprovalStatus
	ApprovedBy  *uuid.UUID
	ApprovedAt  *time.Time
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// CreateRemediationApprovalParams collects the input to CreateRemediationApproval.
type CreateRemediationApprovalParams struct {
	TenantID    uuid.UUID
	NodeID      uuid.UUID
	RuleID      string
	ScriptID    uuid.UUID
	Severity    string
	TaskPayload []byte
	ExpiresAt   time.Time
}

// ListRemediationApprovalsFilter narrows a list query.
type ListRemediationApprovalsFilter struct {
	TenantID uuid.UUID
	Status   ApprovalStatus // empty = any
	NodeID   uuid.UUID      // uuid.Nil = any
}

// CreateRemediationApproval inserts a new pending approval row.
func (s *Store) CreateRemediationApproval(ctx context.Context, params CreateRemediationApprovalParams) (*RemediationApproval, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if params.NodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	if strings.TrimSpace(params.RuleID) == "" {
		return nil, errors.New("rule id is required")
	}
	if params.ScriptID == uuid.Nil {
		return nil, errors.New("script id is required")
	}
	if len(params.TaskPayload) == 0 {
		return nil, errors.New("task payload is required")
	}
	if params.ExpiresAt.IsZero() {
		return nil, errors.New("expires_at is required")
	}

	severity := strings.ToLower(strings.TrimSpace(params.Severity))
	if severity == "" {
		severity = "high"
	}

	id := uuid.New()
	now := s.clock().UTC()

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO remediation_approvals (
			id, tenant_id, node_id, rule_id, script_id, severity,
			task_payload, status, created_at, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, tenant_id, node_id, rule_id, script_id, severity,
		          task_payload, status, approved_by, approved_at, created_at, expires_at
	`,
		id,
		params.TenantID,
		params.NodeID,
		strings.TrimSpace(params.RuleID),
		params.ScriptID,
		severity,
		params.TaskPayload,
		string(ApprovalStatusPending),
		now,
		params.ExpiresAt.UTC(),
	)

	return scanRemediationApproval(row)
}

// GetRemediationApproval fetches a single approval row by ID.
func (s *Store) GetRemediationApproval(ctx context.Context, id uuid.UUID) (*RemediationApproval, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, rule_id, script_id, severity,
		       task_payload, status, approved_by, approved_at, created_at, expires_at
		FROM remediation_approvals
		WHERE id = $1
	`, id)
	a, err := scanRemediationApproval(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

// ListRemediationApprovals returns approvals sorted by creation time descending.
func (s *Store) ListRemediationApprovals(ctx context.Context, filter ListRemediationApprovalsFilter, limit, offset int) ([]RemediationApproval, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(string(filter.Status)); status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM remediation_approvals WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count remediation approvals: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, node_id, rule_id, script_id, severity,
		       task_payload, status, approved_by, approved_at, created_at, expires_at
		FROM remediation_approvals
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(clauses, " AND "))

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
		return nil, 0, fmt.Errorf("query remediation approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var approvals []RemediationApproval
	for rows.Next() {
		var (
			a          RemediationApproval
			approvedBy sql.NullString
			approvedAt sql.NullTime
			status     string
		)
		if err := rows.Scan(
			&a.ID,
			&a.TenantID,
			&a.NodeID,
			&a.RuleID,
			&a.ScriptID,
			&a.Severity,
			&a.TaskPayload,
			&status,
			&approvedBy,
			&approvedAt,
			&a.CreatedAt,
			&a.ExpiresAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan remediation approval: %w", err)
		}
		a.Status = ApprovalStatus(status)
		if approvedBy.Valid {
			if parsed, err := uuid.Parse(approvedBy.String); err == nil {
				a.ApprovedBy = &parsed
			}
		}
		if approvedAt.Valid {
			t := approvedAt.Time.UTC()
			a.ApprovedAt = &t
		}
		approvals = append(approvals, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate remediation approvals: %w", err)
	}

	return approvals, total, nil
}

// ResolveRemediationApproval transitions a pending row to approved|denied. Any
// other status transition returns sql.ErrNoRows so the caller can 404/409.
func (s *Store) ResolveRemediationApproval(ctx context.Context, id uuid.UUID, status ApprovalStatus, approverID uuid.UUID) (*RemediationApproval, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	if status != ApprovalStatusApproved && status != ApprovalStatusDenied {
		return nil, fmt.Errorf("invalid target status %q", status)
	}

	var approverArg any
	if approverID != uuid.Nil {
		approverArg = approverID
	}

	now := s.clock().UTC()

	row := s.db.QueryRowContext(ctx, `
		UPDATE remediation_approvals
		SET status       = $2,
		    approved_by  = $3,
		    approved_at  = $4
		WHERE id = $1 AND status = 'pending'
		RETURNING id, tenant_id, node_id, rule_id, script_id, severity,
		          task_payload, status, approved_by, approved_at, created_at, expires_at
	`, id, string(status), approverArg, now)

	a, err := scanRemediationApproval(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return a, nil
}

// ExpireRemediationApprovals marks any pending rows past their expires_at as
// expired. Returns the number of rows transitioned.
func (s *Store) ExpireRemediationApprovals(ctx context.Context, now time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if now.IsZero() {
		now = s.clock().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE remediation_approvals
		SET status = 'expired'
		WHERE status = 'pending' AND expires_at <= $1
	`, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("expire remediation approvals: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire remediation approvals rows affected: %w", err)
	}
	return int(rows), nil
}

func scanRemediationApproval(row *sql.Row) (*RemediationApproval, error) {
	var (
		a          RemediationApproval
		approvedBy sql.NullString
		approvedAt sql.NullTime
		status     string
	)
	if err := row.Scan(
		&a.ID,
		&a.TenantID,
		&a.NodeID,
		&a.RuleID,
		&a.ScriptID,
		&a.Severity,
		&a.TaskPayload,
		&status,
		&approvedBy,
		&approvedAt,
		&a.CreatedAt,
		&a.ExpiresAt,
	); err != nil {
		return nil, err
	}
	a.Status = ApprovalStatus(status)
	if approvedBy.Valid {
		if parsed, err := uuid.Parse(approvedBy.String); err == nil {
			a.ApprovedBy = &parsed
		}
	}
	if approvedAt.Valid {
		t := approvedAt.Time.UTC()
		a.ApprovedAt = &t
	}
	return &a, nil
}
