package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrRoleSourceReviewRequired means an existing global assignment has no
// trustworthy origin. Never infer that it is safe to remove or adopt it.
var ErrRoleSourceReviewRequired = errors.New("global role source requires administrator review")

// ListDirectoryGlobalRoles reads attributed directory grants only. Ambiguous
// assignments block verification rather than being adopted as manual grants.
func (s *Store) ListDirectoryGlobalRoles(ctx context.Context, user uuid.UUID) ([]string, error) {
	var ambiguous bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND (NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id) OR EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id AND g.source='legacy_unknown')))`, user).Scan(&ambiguous); err != nil {
		return nil, err
	}
	if ambiguous {
		return nil, ErrRoleSourceReviewRequired
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT r.name FROM user_roles ur JOIN roles r ON r.id=ur.role_id JOIN global_role_sources g ON g.assignment_id=ur.id WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND g.source='directory' AND (g.expires_at IS NULL OR g.expires_at>clock_timestamp()) ORDER BY r.name`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

// ReconcileDirectoryRoleSources changes directory attribution while preserving
// independent grants. The user lock serializes global role replacements.
func (s *Store) ReconcileDirectoryRoleSources(ctx context.Context, user uuid.UUID, roles []string) error {
	if user == uuid.Nil {
		return errors.New("user id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND auth_provider='ldap' AND disabled_at IS NULL FOR UPDATE`, user).Scan(&id); err != nil {
		return fmt.Errorf("active directory identity required: %w", err)
	}
	var ambiguous bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND (NOT EXISTS (SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id) OR EXISTS (SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id AND g.source='legacy_unknown')))`, user).Scan(&ambiguous); err != nil {
		return err
	}
	if ambiguous {
		return ErrRoleSourceReviewRequired
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM global_role_sources g USING user_roles ur WHERE g.assignment_id=ur.id AND ur.user_id=$1 AND ur.tenant_id IS NULL AND g.source='directory'`, user); err != nil {
		return err
	}
	for _, name := range sanitizeRoles(roles) {
		roleID, err := s.ensureRole(ctx, tx, name)
		if err != nil {
			return err
		}
		var assignment uuid.UUID
		if err = tx.QueryRowContext(ctx, `INSERT INTO user_roles(user_id,role_id,tenant_id,assigned_by,expires_at) VALUES($1,$2,NULL,NULL,NULL) ON CONFLICT (user_id,role_id) WHERE tenant_id IS NULL DO UPDATE SET role_id=EXCLUDED.role_id RETURNING id`, user, roleID).Scan(&assignment); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO global_role_sources(assignment_id,source) VALUES($1,'directory') ON CONFLICT DO NOTHING`, assignment); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_roles ur WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id)`, user); err != nil {
		return err
	}
	// Existing authorization queries read this projection. A permanent source
	// keeps the role active; otherwise the last independent deadline applies.
	if _, err = tx.ExecContext(ctx, `UPDATE user_roles ur SET expires_at=p.expires_at FROM (SELECT g.assignment_id, CASE WHEN bool_or(g.expires_at IS NULL) THEN NULL ELSE max(g.expires_at) END AS expires_at FROM global_role_sources g JOIN user_roles a ON a.id=g.assignment_id WHERE a.user_id=$1 AND a.tenant_id IS NULL GROUP BY g.assignment_id) p WHERE ur.id=p.assignment_id`, user); err != nil {
		return err
	}
	return tx.Commit()
}
