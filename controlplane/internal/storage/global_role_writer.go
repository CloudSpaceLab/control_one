package storage

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
)

// Caller holds the user row lock. An old unknown grant must be reviewed before
// any writer can adopt it. Repeated requests never renew source deadlines.
func (s *Store) attributeGlobalRole(ctx context.Context, tx *sql.Tx, user, role uuid.UUID, source string, actor any) error {
	var assignment uuid.UUID
	err := tx.QueryRowContext(ctx, `SELECT id FROM user_roles WHERE user_id=$1 AND role_id=$2 AND tenant_id IS NULL`, user, role).Scan(&assignment)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `INSERT INTO user_roles(user_id,role_id,tenant_id,assigned_by,expires_at) VALUES($1,$2,NULL,$3,NULL) RETURNING id`, user, role, actor).Scan(&assignment)
	} else if err == nil {
		var unknown bool
		err = tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM global_role_sources WHERE assignment_id=$1) OR EXISTS(SELECT 1 FROM global_role_sources WHERE assignment_id=$1 AND source='legacy_unknown')`, assignment).Scan(&unknown)
		if err == nil && unknown {
			return ErrRoleSourceReviewRequired
		}
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO global_role_sources(assignment_id,source) VALUES($1,$2) ON CONFLICT DO NOTHING`, assignment, source)
	return err
}

func projectGlobalRoleSources(ctx context.Context, tx *sql.Tx, user uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM user_roles ur WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id)`, user)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE user_roles ur SET expires_at=p.expiry FROM (SELECT g.assignment_id,CASE WHEN bool_or(g.expires_at IS NULL) THEN NULL ELSE max(g.expires_at) END AS expiry FROM global_role_sources g JOIN user_roles a ON a.id=g.assignment_id WHERE a.user_id=$1 AND a.tenant_id IS NULL GROUP BY g.assignment_id) p WHERE ur.id=p.assignment_id`, user)
	return err
}
