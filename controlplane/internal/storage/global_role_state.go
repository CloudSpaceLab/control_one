package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrGlobalRoleAuthority = errors.New("current global administrator required")

type GlobalRoleState struct {
	Roles    []string `json:"roles"`
	Revision string   `json:"revision"`
}

type roleStateQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// One SQL snapshot binds displayed current roles to all global assignment
// identities and expiries. Replacing identical names still changes the token.
func readGlobalRoleState(ctx context.Context, q roleStateQuerier, user uuid.UUID) (GlobalRoleState, error) {
	var out GlobalRoleState
	var roles pq.StringArray
	var assignments string
	var disabled bool
	err := q.QueryRowContext(ctx, `SELECT u.disabled_at IS NOT NULL,
 ARRAY(SELECT DISTINCT r.name FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND ur.tenant_id IS NULL AND u.disabled_at IS NULL AND (ur.expires_at IS NULL OR ur.expires_at>clock_timestamp()) ORDER BY r.name),
 COALESCE((SELECT string_agg(ur.id::text || ':' || ur.role_id::text || ':' || COALESCE(EXTRACT(EPOCH FROM ur.expires_at)::text,'permanent') || ':' || COALESCE((SELECT string_agg(g.grant_id::text || ':' || g.source || ':' || COALESCE(EXTRACT(EPOCH FROM g.expires_at)::text,'permanent'),',' ORDER BY g.source) FROM global_role_sources g WHERE g.assignment_id=ur.id),'unsourced'),';' ORDER BY ur.id) FROM user_roles ur WHERE ur.user_id=u.id AND ur.tenant_id IS NULL),'')
 FROM users u WHERE u.id=$1`, user).Scan(&disabled, &roles, &assignments)
	if err != nil {
		return out, err
	}
	out.Roles = append([]string{}, roles...)
	raw, err := json.Marshal([]any{disabled, out.Roles, assignments})
	if err != nil {
		return out, err
	}
	digest := sha256.Sum256(raw)
	out.Revision = hex.EncodeToString(digest[:])
	return out, nil
}

func (s *Store) GetGlobalRoleState(ctx context.Context, user uuid.UUID) (GlobalRoleState, error) {
	return readGlobalRoleState(ctx, s.db, user)
}

// Locks both identities in stable order before checking current authority and
// the reviewed snapshot. Shared SetUserRoles writes lock the same account row.
func (s *Store) ReplaceGlobalRolesAuthorized(ctx context.Context, actor, target uuid.UUID, expected string, roles []string) error {
	if actor == uuid.Nil || target == uuid.Nil || expected == "" {
		return ErrGlobalRoleAuthority
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := []string{actor.String(), target.String()}
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
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND r.name='admin' AND (ur.expires_at IS NULL OR ur.expires_at>clock_timestamp()))`, actor).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrGlobalRoleAuthority
	}
	state, err := readGlobalRoleState(ctx, tx, target)
	if err != nil {
		return err
	}
	if state.Revision != expected {
		return ErrGovernanceConflict
	}
	// This editor submits an undifferentiated role list. Replacing an LDAP
	// identity's global grants here would erase directory provenance or turn a
	// directory grant into a permanent administrator grant. Keep it blocked
	// until the editor can explicitly manage source-specific grants.
	var targetProvider string
	if err = tx.QueryRowContext(ctx, `SELECT auth_provider FROM users WHERE id=$1`, target).Scan(&targetProvider); err != nil {
		return err
	}
	if targetProvider == AuthProviderLDAP {
		return ErrRoleSourceReviewRequired
	}
	var managed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND (NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id) OR EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id AND g.source<>'administrator')))`, target).Scan(&managed); err != nil {
		return err
	}
	if managed {
		return ErrRoleSourceReviewRequired
	}
	// Re-saving a role list is not authorization to make retained access permanent.
	selected := append([]string{}, sanitizeRoles(roles)...)
	before, err := globalRoleAuditSnapshot(ctx, tx, target)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_roles ur USING roles r WHERE ur.role_id=r.id AND ur.user_id=$1 AND ur.tenant_id IS NULL AND NOT(r.name=ANY($2))`, target, pq.Array(selected)); err != nil {
		return err
	}
	for _, name := range selected {
		var role uuid.UUID
		if err = tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name=$1`, name).Scan(&role); err != nil {
			return fmt.Errorf("existing global role required: %w", err)
		}
		if err = s.attributeGlobalRole(ctx, tx, target, role, "administrator", actor); err != nil {
			return err
		}
	}
	// Invalidate other drafts without changing retained assignment actors/deadlines.
	if _, err = tx.ExecContext(ctx, `UPDATE global_role_sources g SET grant_id=gen_random_uuid() FROM user_roles ur WHERE g.assignment_id=ur.id AND ur.user_id=$1 AND ur.tenant_id IS NULL AND g.source='administrator'`, target); err != nil {
		return err
	}
	if err = projectGlobalRoleSources(ctx, tx, target); err != nil {
		return err
	}
	after, err := globalRoleAuditSnapshot(ctx, tx, target)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO global_role_change_events(actor_id,user_id,reviewed_revision,previous_grants,current_grants) VALUES($1,$2,$3,$4::jsonb,$5::jsonb)`, actor, target, expected, before, after); err != nil {
		return err
	}

	return tx.Commit()
}

// Capture source evidence within the locked role-change transaction.
func globalRoleAuditSnapshot(ctx context.Context, tx *sql.Tx, user uuid.UUID) (string, error) {
	var snapshot string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('assignment_id',ur.id,'role',r.name,'source',COALESCE(g.source,'legacy_unknown'),'expires_at',CASE WHEN g.source IS NULL THEN ur.expires_at ELSE g.expires_at END,'assigned_by',ur.assigned_by) ORDER BY r.name,g.source),'[]'::jsonb)::text FROM user_roles ur JOIN roles r ON r.id=ur.role_id LEFT JOIN global_role_sources g ON g.assignment_id=ur.id WHERE ur.user_id=$1 AND ur.tenant_id IS NULL`, user).Scan(&snapshot)
	return snapshot, err
}
