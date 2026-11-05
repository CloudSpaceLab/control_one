package storage

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"
)

// ChangeAdministratorRoleSource changes an existing explicitly administrator-
// owned grant. It never edits directory or tenant grants or grants a new role.
func (s *Store) ChangeAdministratorRoleSource(ctx context.Context, actor, user, assignment uuid.UUID, expected, action, reason string, expiry *time.Time) error {
	reason = strings.TrimSpace(reason)
	if actor == uuid.Nil || user == uuid.Nil || assignment == uuid.Nil || expected == "" || len(reason) < 10 || len(reason) > 2000 {
		return errors.New("reviewed identity, assignment, revision and reason required")
	}
	if action != "revoke" && action != "set_expiry" {
		return errors.New("unsupported administrator grant action")
	}
	if (action == "revoke" && expiry != nil) || (action == "set_expiry" && (expiry == nil || !expiry.After(time.Now()))) {
		return errors.New("a future deadline is required only for set_expiry")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := []string{actor.String(), user.String()}
	sort.Strings(ids)
	for i, id := range ids {
		if i > 0 && id == ids[i-1] {
			continue
		}
		var locked uuid.UUID
		if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND disabled_at IS NULL FOR UPDATE`, id).Scan(&locked); err != nil {
			return ErrGlobalRoleAuthority
		}
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND r.name='admin' AND (ur.expires_at IS NULL OR ur.expires_at>clock_timestamp()))`, actor).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return ErrGlobalRoleAuthority
	}
	state, err := readGlobalRoleState(ctx, tx, user)
	if err != nil {
		return err
	}
	if state.Revision != expected {
		return ErrGovernanceConflict
	}
	var role string
	var before sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT r.name,g.expires_at FROM global_role_sources g JOIN user_roles ur ON ur.id=g.assignment_id JOIN roles r ON r.id=ur.role_id WHERE g.assignment_id=$1 AND ur.user_id=$2 AND ur.tenant_id IS NULL AND g.source='administrator' AND NOT EXISTS(SELECT 1 FROM global_role_sources x WHERE x.assignment_id=ur.id AND x.source='legacy_unknown')`, assignment, user).Scan(&role, &before); err != nil {
		return ErrRoleSourceReviewRequired
	}
	if action == "revoke" {
		_, err = tx.ExecContext(ctx, `DELETE FROM global_role_sources WHERE assignment_id=$1 AND source='administrator'`, assignment)
	} else {
		var future bool
		if err = tx.QueryRowContext(ctx, `SELECT $1::timestamptz>clock_timestamp()`, expiry).Scan(&future); err != nil {
			return err
		}
		if !future {
			return errors.New("deadline expired while waiting for authorization")
		}
		_, err = tx.ExecContext(ctx, `UPDATE global_role_sources SET expires_at=$2 WHERE assignment_id=$1 AND source='administrator'`, assignment, expiry)
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_roles ur SET expires_at=p.expiry FROM (SELECT assignment_id,CASE WHEN bool_or(expires_at IS NULL) THEN NULL ELSE max(expires_at) END AS expiry FROM global_role_sources WHERE assignment_id=$1 GROUP BY assignment_id) p WHERE ur.id=p.assignment_id`, assignment); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_roles ur WHERE ur.id=$1 AND NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id)`, assignment); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO global_role_source_reviews(actor_id,user_id,assignment_id,role_name,reason,action,before_expiry,after_expiry) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, actor, user, assignment, role, reason, action, before, expiry); err != nil {
		return err
	}
	return tx.Commit()
}
