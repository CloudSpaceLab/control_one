package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// PersistAuthenticationRoles binds the verified authority and subject to a
// separate identity and atomically replaces only authentication-owned grants.
// Old bare-subject identities must be explicitly migrated, never auto-adopted.
func (s *Store) PersistAuthenticationRoles(ctx context.Context, authority, subject, email, name string, roles []string) (*User, []string, error) {
	if subject == "" || (authority != "static" && !strings.HasPrefix(authority, "oidc:")) || authority == "oidc:" {
		return nil, nil, errors.New("verified authentication authority and subject required")
	}
	key, _ := json.Marshal([]string{authority, subject})
	digest := sha256.Sum256(key)
	external := "auth:v1:" + hex.EncodeToString(digest[:])
	authorityDigest := sha256.Sum256([]byte(authority))
	provider := "oidc"
	if authority == "static" {
		provider = "static"
	}
	for i := range roles {
		roles[i] = strings.ToLower(strings.TrimSpace(roles[i]))
	}
	roles = sanitizeRoles(roles)
	if roles == nil {
		roles = []string{}
	}
	sort.Strings(roles)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	// First browser sign-in sends profile and tenant requests concurrently.
	// Serialize creation for this authority/subject before either unique index
	// (external identity or email) can reject a competing same-identity insert.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, external); err != nil {
		return nil, nil, err
	}
	var legacy bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE external_id=$1)`, subject).Scan(&legacy); err != nil {
		return nil, nil, err
	}
	if legacy {
		return nil, nil, fmt.Errorf("legacy authentication identity requires explicit migration: %w", ErrRoleSourceReviewRequired)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO users(id,external_id,email,display_name,auth_provider,created_at) VALUES($1,$2,$3,$4,$5,clock_timestamp()) ON CONFLICT(external_id) DO NOTHING`, uuid.New(), external, nullString(email), nullString(name), provider); err != nil {
		return nil, nil, err
	}
	var user User
	var actualProvider string
	if err = tx.QueryRowContext(ctx, `SELECT id,external_id,email,display_name,created_at,auth_provider FROM users WHERE external_id=$1 AND disabled_at IS NULL FOR UPDATE`, external).Scan(&user.ID, &user.ExternalID, &user.Email, &user.DisplayName, &user.CreatedAt, &actualProvider); err != nil {
		return nil, nil, err
	}
	if actualProvider != provider {
		return nil, nil, errors.New("authentication provider identity mismatch")
	}
	if err = bindAuthenticationAuthority(ctx, tx, user.ID, external, authority, subject); err != nil {
		return nil, nil, err
	}
	// Provisioning/legacy rows do not prove ownership by this authority.
	var ambiguous bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND (NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id) OR EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id AND g.source IN ('legacy_unknown','provisioning'))))`, user.ID).Scan(&ambiguous); err != nil {
		return nil, nil, err
	}
	if ambiguous {
		return nil, nil, ErrRoleSourceReviewRequired
	}
	previous := []string{}
	rows, err := tx.QueryContext(ctx, `SELECT r.name FROM global_role_sources g JOIN user_roles ur ON ur.id=g.assignment_id JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND g.source='authentication' ORDER BY r.name`, user.ID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			rows.Close()
			return nil, nil, err
		}
		previous = append(previous, role)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM global_role_sources g USING user_roles ur,roles r WHERE g.assignment_id=ur.id AND r.id=ur.role_id AND ur.user_id=$1 AND ur.tenant_id IS NULL AND g.source='authentication' AND NOT(r.name=ANY($2))`, user.ID, pq.Array(roles)); err != nil {
		return nil, nil, err
	}
	for _, role := range roles {
		id, e := s.ensureRole(ctx, tx, role)
		if e != nil {
			return nil, nil, e
		}
		if err = s.attributeGlobalRole(ctx, tx, user.ID, id, "authentication", nil); err != nil {
			return nil, nil, err
		}
	}
	if err = projectGlobalRoleSources(ctx, tx, user.ID); err != nil {
		return nil, nil, err
	}
	if !reflect.DeepEqual(previous, roles) {
		before, _ := json.Marshal(previous)
		after, _ := json.Marshal(roles)
		if _, err = tx.ExecContext(ctx, `INSERT INTO authentication_role_events(user_id,authority_sha256,previous_roles,current_roles) VALUES($1,$2,$3,$4)`, user.ID, hex.EncodeToString(authorityDigest[:]), before, after); err != nil {
			return nil, nil, err
		}
	}
	effective := []string{}
	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT lower(r.name) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND (ur.expires_at IS NULL OR ur.expires_at>clock_timestamp()) ORDER BY lower(r.name)`, user.ID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			rows.Close()
			return nil, nil, err
		}
		effective = append(effective, role)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &user, effective, nil
}
