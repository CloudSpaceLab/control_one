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
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=$1 AND tenant_id IS NULL`, target); err != nil {
		return err
	}
	for _, name := range sanitizeRoles(roles) {
		var role uuid.UUID
		if err = tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name=$1`, name).Scan(&role); err != nil {
			return fmt.Errorf("existing global role required: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id,tenant_id,assigned_by,expires_at) VALUES($1,$2,NULL,$3,NULL)`, target, role, actor); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO global_role_sources(assignment_id,source) SELECT id,'administrator' FROM user_roles WHERE user_id=$1 AND role_id=$2 AND tenant_id IS NULL`, target, role); err != nil {
			return err
		}
	}
	return tx.Commit()
}
