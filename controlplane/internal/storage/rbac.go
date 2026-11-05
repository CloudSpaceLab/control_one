package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// User represents an authenticated principal persisted for RBAC purposes.
type User struct {
	ID          uuid.UUID
	ExternalID  string
	Email       sql.NullString
	DisplayName sql.NullString
	CreatedAt   time.Time
}

// Role captures RBAC role metadata.
type Role struct {
	ID          uuid.UUID
	Name        string
	Description sql.NullString
	CreatedAt   time.Time
}

// GetUserByExternalID returns a user if it exists.
func (s *Store) GetUserByExternalID(ctx context.Context, externalID string) (*User, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return nil, errors.New("external id required")
	}

	row := s.db.QueryRowContext(ctx, `
        SELECT id, external_id, email, display_name, created_at
        FROM users
        WHERE external_id = $1
    `, externalID)

	var user User
	if err := row.Scan(&user.ID, &user.ExternalID, &user.Email, &user.DisplayName, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select user: %w", err)
	}

	return &user, nil
}

// EnsureUser upserts a user by external identifier and returns the record.
func (s *Store) EnsureUser(ctx context.Context, externalID, email, displayName string) (*User, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return nil, errors.New("external id required")
	}

	row := s.db.QueryRowContext(ctx, `
        SELECT id, external_id, email, display_name, created_at
        FROM users
        WHERE external_id = $1
    `, externalID)

	var user User
	if err := row.Scan(&user.ID, &user.ExternalID, &user.Email, &user.DisplayName, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			now := s.clock()
			user = User{
				ID:          uuid.New(),
				ExternalID:  externalID,
				Email:       nullString(email),
				DisplayName: nullString(displayName),
				CreatedAt:   now,
			}
			if _, err := s.db.ExecContext(ctx, `
                INSERT INTO users (id, external_id, email, display_name, created_at)
                VALUES ($1, $2, $3, $4, $5)
            `, user.ID, user.ExternalID, user.Email, user.DisplayName, user.CreatedAt); err != nil {
				return nil, fmt.Errorf("insert user: %w", err)
			}
			return &user, nil
		}
		return nil, fmt.Errorf("select user: %w", err)
	}

	updates := map[string]sql.NullString{}
	if email != "" {
		trimmed := nullString(email)
		if user.Email.String != trimmed.String || user.Email.Valid != trimmed.Valid {
			updates["email"] = trimmed
		}
	}
	if displayName != "" {
		trimmed := nullString(displayName)
		if user.DisplayName.String != trimmed.String || user.DisplayName.Valid != trimmed.Valid {
			updates["display_name"] = trimmed
		}
	}

	if len(updates) > 0 {
		setFragments := make([]string, 0, len(updates))
		args := []any{}
		for column, value := range updates {
			setFragments = append(setFragments, fmt.Sprintf("%s = $%d", column, len(args)+1))
			args = append(args, value)
		}
		args = append(args, user.ID)
		query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(setFragments, ", "), len(args))
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return nil, fmt.Errorf("update user: %w", err)
		}
		// Refresh record to reflect persisted values
		row = s.db.QueryRowContext(ctx, `
            SELECT id, external_id, email, display_name, created_at
            FROM users
            WHERE id = $1
        `, user.ID)
		if err := row.Scan(&user.ID, &user.ExternalID, &user.Email, &user.DisplayName, &user.CreatedAt); err != nil {
			return nil, fmt.Errorf("reload user: %w", err)
		}
	}

	return &user, nil
}

// GetUser returns a user by ID.
func (s *Store) GetUser(ctx context.Context, userID uuid.UUID) (*User, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if userID == uuid.Nil {
		return nil, errors.New("user id required")
	}

	row := s.db.QueryRowContext(ctx, `
        SELECT id, external_id, email, display_name, created_at
        FROM users
        WHERE id = $1
    `, userID)

	var user User
	if err := row.Scan(&user.ID, &user.ExternalID, &user.Email, &user.DisplayName, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select user: %w", err)
	}

	return &user, nil
}

// ListUsers returns paginated users ordered by creation date.
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]User, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	baseQuery := `
        SELECT id, external_id, email, display_name, created_at
        FROM users
        ORDER BY created_at DESC
    `
	args := []any{}
	query := baseQuery
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, limit)
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, offset)
	}

	countRow := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.ExternalID, &user.Email, &user.DisplayName, &user.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate users: %w", err)
	}

	return users, total, nil
}

// AssignRolesToUser ensures the provided roles exist and associates them with the user.
func (s *Store) AssignRolesToUser(ctx context.Context, userID uuid.UUID, roles []string) error {
	return s.assignAttributedRoles(ctx, userID, roles, "provisioning")
}

func (s *Store) assignAttributedRoles(ctx context.Context, userID uuid.UUID, roles []string, source string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if userID == uuid.Nil {
		return errors.New("user id required")
	}
	if source != "administrator" && source != "provisioning" {
		return errors.New("unsupported provisioning source")
	}
	roles = sanitizeRoles(roles)
	if len(roles) == 0 {
		return errors.New("roles required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND disabled_at IS NULL FOR UPDATE`, userID).Scan(&locked); err != nil {
		return err
	}
	for _, name := range roles {
		role, err := s.ensureRole(ctx, tx, name)
		if err != nil {
			return err
		}
		if err = s.attributeGlobalRole(ctx, tx, userID, role, source, nil); err != nil {
			return err
		}
	}
	if err = projectGlobalRoleSources(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ensureRole(ctx context.Context, tx *sql.Tx, name string) (uuid.UUID, error) {
	var roleID uuid.UUID
	row := tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = $1`, name)
	if err := row.Scan(&roleID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("lookup role: %w", err)
		}
		roleID = uuid.New()
		if _, err := tx.ExecContext(ctx, `
            INSERT INTO roles (id, name, description, created_at)
            VALUES ($1, $2, NULL, $3)
            ON CONFLICT (name) DO NOTHING
        `, roleID, name, s.clock()); err != nil {
			return uuid.Nil, fmt.Errorf("insert role: %w", err)
		}
		// Re-load to capture canonical ID if conflict triggered
		row = tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = $1`, name)
		if err := row.Scan(&roleID); err != nil {
			return uuid.Nil, fmt.Errorf("reload role: %w", err)
		}
	}
	return roleID, nil
}

func sanitizeRoles(roles []string) []string {
	seen := make(map[string]struct{})
	var normalized []string
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		key := strings.ToLower(role)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, role)
	}
	return normalized
}

// ListUserRoles returns role names for the given user.
func (s *Store) ListUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.listUserRoles(ctx, userID, false)
}

// ListGlobalUserRoles supplies the global identity editor. Tenant assignments
// must never be round-tripped into SetUserRoles, which writes global grants.
func (s *Store) ListGlobalUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.listUserRoles(ctx, userID, true)
}

// ListTenantUserRoles returns only active tenant-scoped roles for principal construction.
func (s *Store) ListTenantUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT lower(r.name) FROM user_roles ur JOIN roles r ON r.id=ur.role_id JOIN users u ON u.id=ur.user_id WHERE ur.user_id=$1 AND ur.tenant_id IS NOT NULL AND u.disabled_at IS NULL AND (ur.expires_at IS NULL OR ur.expires_at>NOW()) ORDER BY lower(r.name)`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		roles = append(roles, name)
	}
	return roles, rows.Err()
}

func (s *Store) listUserRoles(ctx context.Context, userID uuid.UUID, globalOnly bool) ([]string, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if userID == uuid.Nil {
		return nil, errors.New("user id required")
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT name
        FROM (
            SELECT LOWER(r.name) AS sort_key, MIN(r.name) AS name
            FROM user_roles ur
            JOIN roles r ON ur.role_id = r.id
            JOIN users u ON u.id=ur.user_id
            WHERE ur.user_id = $1
              AND u.disabled_at IS NULL
              AND (ur.expires_at IS NULL OR ur.expires_at>NOW())
              AND (NOT $2 OR ur.tenant_id IS NULL)
            GROUP BY LOWER(r.name)
        ) deduped
        ORDER BY sort_key
    `, userID, globalOnly)
	if err != nil {
		return nil, fmt.Errorf("query user roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var roles []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}

	return roles, nil
}

// ListAccessibleTenants returns only tenants for which the user has one of
// the supplied roles, including every tenant when a matching global role exists.
func (s *Store) ListAccessibleTenants(
	ctx context.Context,
	userID uuid.UUID,
	roles []string,
	prefix string,
	limit, offset int,
) ([]Tenant, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if userID == uuid.Nil {
		return nil, 0, errors.New("user id required")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}
	roles = sanitizeRoles(roles)
	if len(roles) == 0 {
		return []Tenant{}, 0, nil
	}
	for i := range roles {
		roles[i] = strings.ToLower(strings.TrimSpace(roles[i]))
	}

	args := []any{userID, pq.Array(roles)}
	clauses := []string{`
		EXISTS (
			SELECT 1
			FROM user_roles ur
			JOIN roles r ON r.id = ur.role_id
			JOIN users access_user ON access_user.id=ur.user_id
			WHERE ur.user_id = $1
			  AND access_user.disabled_at IS NULL
			  AND LOWER(r.name) = ANY($2)
			  AND (ur.expires_at IS NULL OR ur.expires_at > NOW())
			  AND (ur.tenant_id IS NULL OR ur.tenant_id = t.id)
		)`,
	}
	if prefix = strings.TrimSpace(prefix); prefix != "" {
		args = append(args, prefix+"%")
		clauses = append(clauses, fmt.Sprintf("t.name ILIKE $%d", len(args)))
	}
	whereSQL := strings.Join(clauses, " AND ")

	var total int
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM tenants t WHERE "+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count accessible tenants: %w", err)
	}

	query := `
		SELECT t.id, t.name, t.created_at
		FROM tenants t
		WHERE ` + whereSQL + `
		ORDER BY t.created_at DESC
	`
	queryArgs := append([]any(nil), args...)
	if limit > 0 {
		queryArgs = append(queryArgs, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(queryArgs))
	}
	if offset > 0 {
		queryArgs = append(queryArgs, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(queryArgs))
	}
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query accessible tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]Tenant, 0)
	for rows.Next() {
		var tenant Tenant
		if err := rows.Scan(&tenant.ID, &tenant.Name, &tenant.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan accessible tenant: %w", err)
		}
		out = append(out, tenant)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate accessible tenants: %w", err)
	}
	return out, total, nil
}

// UserHasTenantRole returns true when the user has one of the supplied roles
// either globally (tenant_id NULL) or scoped to the requested tenant.
func (s *Store) UserHasTenantRole(ctx context.Context, userID, tenantID uuid.UUID, roles []string) (bool, error) {
	if s.db == nil {
		return false, errors.New("store database not initialized")
	}
	if userID == uuid.Nil {
		return false, errors.New("user id required")
	}
	if tenantID == uuid.Nil {
		return false, errors.New("tenant id required")
	}
	roles = sanitizeRoles(roles)
	if len(roles) == 0 {
		return false, errors.New("roles required")
	}
	for i := range roles {
		roles[i] = strings.ToLower(strings.TrimSpace(roles[i]))
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM user_roles ur
			JOIN roles r ON r.id = ur.role_id
			WHERE ur.user_id = $1
			  AND (ur.tenant_id IS NULL OR ur.tenant_id = $2)
			  AND LOWER(r.name) = ANY($3)
			  AND EXISTS(SELECT 1 FROM users current_user_record WHERE current_user_record.id=ur.user_id AND current_user_record.disabled_at IS NULL)
			  AND (ur.expires_at IS NULL OR ur.expires_at > NOW())
		)
	`, userID, tenantID, pq.Array(roles)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("query tenant role access: %w", err)
	}
	return exists, nil
}

// SetUserRoles replaces only administrator-owned global grants, preserving
// independently managed sources. Legacy grants require explicit review.
func (s *Store) SetUserRoles(ctx context.Context, userID uuid.UUID, roles []string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if userID == uuid.Nil {
		return errors.New("user id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&locked); err != nil {
		return err
	}
	var unknown bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND (NOT EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id) OR EXISTS(SELECT 1 FROM global_role_sources g WHERE g.assignment_id=ur.id AND g.source='legacy_unknown')))`, userID).Scan(&unknown); err != nil {
		return err
	}
	if unknown {
		return ErrRoleSourceReviewRequired
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM global_role_sources g USING user_roles ur WHERE g.assignment_id=ur.id AND ur.user_id=$1 AND ur.tenant_id IS NULL AND g.source='administrator'`, userID); err != nil {
		return err
	}
	for _, name := range sanitizeRoles(roles) {
		role, err := s.ensureRole(ctx, tx, name)
		if err != nil {
			return err
		}
		// Replacing only administrator grants preserves all independently managed sources.
		var assignment uuid.UUID
		err = tx.QueryRowContext(ctx, `INSERT INTO user_roles(user_id,role_id,tenant_id,assigned_by,expires_at) VALUES($1,$2,NULL,NULL,NULL) ON CONFLICT(user_id,role_id) WHERE tenant_id IS NULL DO UPDATE SET role_id=EXCLUDED.role_id RETURNING id`, userID, role).Scan(&assignment)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO global_role_sources(assignment_id,source) VALUES($1,'administrator')`, assignment); err != nil {
			return err
		}
	}
	if err = projectGlobalRoleSources(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListRoles returns all defined roles.
func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT id, name, description, created_at
        FROM roles
        ORDER BY name ASC
    `)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var roles []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}

	return roles, nil
}
