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

// PatchApproval is the patch-domain twin of RemediationApproval. It captures
// per-(deployment, node) operator gating before runPatchSafetyGates re-
// dispatches the underlying patch.deploy_* job. Mirrors the lifecycle of
// remediation_approvals: pending → approved | denied | expired.
type PatchApproval struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	DeploymentID uuid.UUID
	NodeID       uuid.UUID
	Mode         string
	ProxyID      *uuid.UUID
	WindowID     *uuid.UUID
	Status       ApprovalStatus
	ApprovedBy   *uuid.UUID
	ApprovedAt   *time.Time
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// CreatePatchApprovalParams collects the input to CreatePatchApproval.
type CreatePatchApprovalParams struct {
	TenantID     uuid.UUID
	DeploymentID uuid.UUID
	NodeID       uuid.UUID
	Mode         string
	ProxyID      *uuid.UUID
	WindowID     *uuid.UUID
	ExpiresAt    time.Time
}

// ListPatchApprovalsFilter narrows a list query.
type ListPatchApprovalsFilter struct {
	TenantID     uuid.UUID
	DeploymentID uuid.UUID
	NodeID       uuid.UUID
	Status       ApprovalStatus // empty = any
}

// CreatePatchApproval inserts a new pending approval row. The (deployment_id,
// node_id) uniqueness constraint protects against double-dispatch race
// conditions: a second CreatePatchApproval call for the same pair is a no-op
// when one is already pending — callers should treat ErrPatchApprovalExists
// as benign.
func (s *Store) CreatePatchApproval(ctx context.Context, params CreatePatchApprovalParams) (*PatchApproval, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if params.DeploymentID == uuid.Nil {
		return nil, errors.New("deployment id is required")
	}
	if params.NodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	mode := strings.ToLower(strings.TrimSpace(params.Mode))
	switch mode {
	case "direct", "proxy", "airgapped":
	default:
		return nil, fmt.Errorf("invalid mode %q (must be direct|proxy|airgapped)", params.Mode)
	}
	if params.ExpiresAt.IsZero() {
		return nil, errors.New("expires_at is required")
	}

	id := uuid.New()
	now := s.clock().UTC()

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO patch_approvals (
			id, tenant_id, deployment_id, node_id, mode, proxy_id, window_id,
			status, created_at, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, tenant_id, deployment_id, node_id, mode, proxy_id, window_id,
		          status, approved_by, approved_at, created_at, expires_at
	`,
		id,
		params.TenantID,
		params.DeploymentID,
		params.NodeID,
		mode,
		nullableUUIDPtr(params.ProxyID),
		nullableUUIDPtr(params.WindowID),
		string(ApprovalStatusPending),
		now,
		params.ExpiresAt.UTC(),
	)

	return scanPatchApproval(row)
}

// GetPatchApproval fetches a single approval by ID.
func (s *Store) GetPatchApproval(ctx context.Context, id uuid.UUID) (*PatchApproval, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, deployment_id, node_id, mode, proxy_id, window_id,
		       status, approved_by, approved_at, created_at, expires_at
		FROM patch_approvals
		WHERE id = $1
	`, id)
	a, err := scanPatchApproval(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

// ListPatchApprovals returns approvals filtered and sorted by creation time DESC.
func (s *Store) ListPatchApprovals(ctx context.Context, filter ListPatchApprovalsFilter, limit, offset int) ([]PatchApproval, int, error) {
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
	if filter.DeploymentID != uuid.Nil {
		args = append(args, filter.DeploymentID)
		clauses = append(clauses, fmt.Sprintf("deployment_id = $%d", len(args)))
	}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(string(filter.Status)); status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM patch_approvals WHERE %s`, strings.Join(clauses, " AND "))
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count patch approvals: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, deployment_id, node_id, mode, proxy_id, window_id,
		       status, approved_by, approved_at, created_at, expires_at
		FROM patch_approvals
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
		return nil, 0, fmt.Errorf("query patch approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var approvals []PatchApproval
	for rows.Next() {
		a, err := scanPatchApprovalRows(rows)
		if err != nil {
			return nil, 0, err
		}
		approvals = append(approvals, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate patch approvals: %w", err)
	}
	return approvals, total, nil
}

// ResolvePatchApproval transitions a pending row to approved|denied. Any other
// status transition returns sql.ErrNoRows so the caller can map to 409.
func (s *Store) ResolvePatchApproval(ctx context.Context, id uuid.UUID, status ApprovalStatus, approverID uuid.UUID) (*PatchApproval, error) {
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
		UPDATE patch_approvals
		SET status      = $2,
		    approved_by = $3,
		    approved_at = $4
		WHERE id = $1 AND status = 'pending'
		RETURNING id, tenant_id, deployment_id, node_id, mode, proxy_id, window_id,
		          status, approved_by, approved_at, created_at, expires_at
	`, id, string(status), approverArg, now)

	a, err := scanPatchApproval(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return a, nil
}

// ExpirePatchApprovals flips any pending rows past their expires_at to
// expired. Returns the number of rows transitioned.
func (s *Store) ExpirePatchApprovals(ctx context.Context, now time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if now.IsZero() {
		now = s.clock().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE patch_approvals
		SET status = 'expired'
		WHERE status = 'pending' AND expires_at <= $1
	`, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("expire patch approvals: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire patch approvals rows affected: %w", err)
	}
	return int(rows), nil
}

func scanPatchApproval(row *sql.Row) (*PatchApproval, error) {
	var (
		a          PatchApproval
		proxyID    sql.NullString
		windowID   sql.NullString
		approvedBy sql.NullString
		approvedAt sql.NullTime
		status     string
	)
	if err := row.Scan(
		&a.ID,
		&a.TenantID,
		&a.DeploymentID,
		&a.NodeID,
		&a.Mode,
		&proxyID,
		&windowID,
		&status,
		&approvedBy,
		&approvedAt,
		&a.CreatedAt,
		&a.ExpiresAt,
	); err != nil {
		return nil, err
	}
	a.Status = ApprovalStatus(status)
	if proxyID.Valid {
		if parsed, err := uuid.Parse(proxyID.String); err == nil {
			a.ProxyID = &parsed
		}
	}
	if windowID.Valid {
		if parsed, err := uuid.Parse(windowID.String); err == nil {
			a.WindowID = &parsed
		}
	}
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

func scanPatchApprovalRows(rows *sql.Rows) (*PatchApproval, error) {
	var (
		a          PatchApproval
		proxyID    sql.NullString
		windowID   sql.NullString
		approvedBy sql.NullString
		approvedAt sql.NullTime
		status     string
	)
	if err := rows.Scan(
		&a.ID,
		&a.TenantID,
		&a.DeploymentID,
		&a.NodeID,
		&a.Mode,
		&proxyID,
		&windowID,
		&status,
		&approvedBy,
		&approvedAt,
		&a.CreatedAt,
		&a.ExpiresAt,
	); err != nil {
		return nil, fmt.Errorf("scan patch approval: %w", err)
	}
	a.Status = ApprovalStatus(status)
	if proxyID.Valid {
		if parsed, err := uuid.Parse(proxyID.String); err == nil {
			a.ProxyID = &parsed
		}
	}
	if windowID.Valid {
		if parsed, err := uuid.Parse(windowID.String); err == nil {
			a.WindowID = &parsed
		}
	}
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
