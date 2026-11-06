package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"sort"
	"strings"
)

// A migration is an explicit administrator authorization, never inferred from
// an email match or a provider claim. Local password/LDAP accounts are excluded.
func (s *Store) AuthenticationMigrationEligible(ctx context.Context, user uuid.UUID) (bool, error) {
	var eligible bool
	err := s.db.QueryRowContext(ctx, `SELECT disabled_at IS NULL AND password_hash IS NULL AND auth_provider<>'ldap' AND external_id NOT LIKE 'local:%' AND external_id NOT LIKE 'ldap:%' AND (external_id NOT LIKE 'auth:v1:%' OR EXISTS(SELECT 1 FROM user_roles ur LEFT JOIN global_role_sources g ON g.assignment_id=ur.id WHERE ur.user_id=users.id AND ur.tenant_id IS NULL AND (g.source IS NULL OR g.source IN ('legacy_unknown','provisioning')))) FROM users WHERE id=$1`, user).Scan(&eligible)
	return eligible, err
}

func (s *Store) MigrateAuthenticationIdentity(ctx context.Context, actor, user uuid.UUID, expected, authority, subject, reason string) error {
	reason = strings.TrimSpace(reason)
	if actor == user {
		return ErrGlobalRoleAuthority
	}
	if actor == uuid.Nil || user == uuid.Nil || actor == user || expected == "" || subject == "" || len(subject) > 512 || len(reason) < 10 || len(reason) > 2000 || (authority != "static" && !strings.HasPrefix(authority, "oidc:")) {
		return errors.New("reviewed authentication migration required")
	}
	key, _ := json.Marshal([]string{authority, subject})
	digest := sha256.Sum256(key)
	external := "auth:v1:" + hex.EncodeToString(digest[:])
	ad := sha256.Sum256([]byte(authority))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, external); err != nil {
		return err
	}
	ids := []string{actor.String(), user.String()}
	sort.Strings(ids)
	for _, id := range ids {
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
	var old string
	var eligible bool
	if err = tx.QueryRowContext(ctx, `SELECT external_id,password_hash IS NULL AND auth_provider<>'ldap' AND external_id NOT LIKE 'local:%' AND external_id NOT LIKE 'ldap:%' FROM users WHERE id=$1`, user).Scan(&old, &eligible); err != nil {
		return err
	}
	if !eligible || (old != subject && old != external) {
		return ErrRoleSourceReviewRequired
	}
	if old == external {
		var ambiguous bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur LEFT JOIN global_role_sources g ON g.assignment_id=ur.id WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND (g.source IS NULL OR g.source IN ('legacy_unknown','provisioning')))`, user).Scan(&ambiguous); err != nil {
			return err
		}
		if !ambiguous {
			return ErrGovernanceConflict
		}
	}
	var collision bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE external_id=$1 AND id<>$2)`, external, user).Scan(&collision); err != nil {
		return err
	}
	if collision {
		return ErrGovernanceConflict
	}
	// Never merge differently-owned grants implicitly. The operator must review
	// those individual origins before retrying the migration.
	var mixed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND (SELECT count(*) FROM global_role_sources g WHERE g.assignment_id=ur.id)>1 AND EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id AND g.source IN ('legacy_unknown','provisioning')))`, user).Scan(&mixed); err != nil {
		return err
	}
	if mixed {
		return ErrRoleSourceReviewRequired
	}
	before, err := globalRoleAuditSnapshot(ctx, tx, user)
	if err != nil {
		return err
	}
	// Preserve unknown pre-source assignments and each existing source deadline.
	if _, err = tx.ExecContext(ctx, `INSERT INTO global_role_sources(assignment_id,source,expires_at) SELECT id,'legacy_unknown',expires_at FROM user_roles ur WHERE user_id=$1 AND tenant_id IS NULL AND NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id)`, user); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE global_role_sources g SET source='administrator',grant_id=gen_random_uuid() FROM user_roles ur WHERE g.assignment_id=ur.id AND ur.user_id=$1 AND ur.tenant_id IS NULL AND g.source IN ('legacy_unknown','provisioning')`, user); err != nil {
		return err
	}
	provider := "oidc"
	if authority == "static" {
		provider = "static"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET external_id=$2,auth_provider=$3 WHERE id=$1`, user, external, provider); err != nil {
		return err
	}
	if err = bindAuthenticationAuthority(ctx, tx, user, external, authority, subject); err != nil {
		return err
	}
	if err = projectGlobalRoleSources(ctx, tx, user); err != nil {
		return err
	}
	after, err := globalRoleAuditSnapshot(ctx, tx, user)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO authentication_identity_migrations(actor_id,user_id,old_external_id,new_external_id,authority_sha256,reason,previous_grants,current_grants) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb)`, actor, user, old, external, hex.EncodeToString(ad[:]), reason, before, after); err != nil {
		return err
	}
	return tx.Commit()
}
