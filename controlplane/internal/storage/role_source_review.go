package storage

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type GlobalRoleSource struct {
	AssignmentID uuid.UUID  `json:"assignment_id"`
	Role         string     `json:"role"`
	Source       string     `json:"source"`
	ExpiresAt    *time.Time `json:"expires_at"`
}

func (s *Store) ListGlobalRoleSources(ctx context.Context, user uuid.UUID) ([]GlobalRoleSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ur.id,r.name,COALESCE(g.source,'legacy_unknown'),CASE WHEN g.source IS NULL THEN ur.expires_at ELSE g.expires_at END FROM user_roles ur JOIN roles r ON r.id=ur.role_id LEFT JOIN global_role_sources g ON g.assignment_id=ur.id WHERE ur.user_id=$1 AND ur.tenant_id IS NULL ORDER BY r.name,g.source`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GlobalRoleSource{}
	for rows.Next() {
		var g GlobalRoleSource
		if err = rows.Scan(&g.AssignmentID, &g.Role, &g.Source, &g.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ReviewLegacyRoleSource records an explicit administrator decision to own an
// existing global grant. It preserves the role and deadline, and never invents
// directory membership. User locks serialize this with reconciliation/editing.
func (s *Store) ReviewLegacyRoleSource(ctx context.Context, actor, user, assignment uuid.UUID, expected, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) < 10 || len(reason) > 2000 || expected == "" || actor == uuid.Nil || user == uuid.Nil || assignment == uuid.Nil {
		return errors.New("reviewed assignment, revision and reason of 10 to 2000 characters required")
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
	var expiry sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT r.name,ur.expires_at FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.id=$1 AND ur.user_id=$2 AND ur.tenant_id IS NULL AND (NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id) OR EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id AND g.source='legacy_unknown'))`, assignment, user).Scan(&role, &expiry); err != nil {
		return ErrGovernanceConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO global_role_sources(assignment_id,source,expires_at) VALUES($1,'administrator',$2) ON CONFLICT DO NOTHING`, assignment, expiry); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM global_role_sources WHERE assignment_id=$1 AND source='legacy_unknown'`, assignment); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO global_role_source_reviews(actor_id,user_id,assignment_id,role_name,reason) VALUES($1,$2,$3,$4,$5)`, actor, user, assignment, role, reason); err != nil {
		return err
	}
	return tx.Commit()
}
