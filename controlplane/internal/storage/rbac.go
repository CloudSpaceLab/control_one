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

// User represents an authenticated principal persisted for RBAC purposes.
type User struct {
	ID          uuid.UUID
	ExternalID  string
	Email       sql.NullString
	DisplayName sql.NullString
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

// AssignRolesToUser ensures the provided roles exist and associates them with the user.
func (s *Store) AssignRolesToUser(ctx context.Context, userID uuid.UUID, roles []string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if userID == uuid.Nil {
		return errors.New("user id required")
	}

	roles = sanitizeRoles(roles)
	if len(roles) == 0 {
		return errors.New("roles required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, roleName := range roles {
		roleName = strings.TrimSpace(roleName)
		if roleName == "" {
			continue
		}

		roleID, roleErr := s.ensureRole(ctx, tx, roleName)
		if roleErr != nil {
			err = roleErr
			return err
		}

		if _, roleErr = tx.ExecContext(ctx, `
            INSERT INTO user_roles (user_id, role_id, tenant_id, assigned_by, expires_at)
            VALUES ($1, $2, NULL, NULL, NULL)
            ON CONFLICT (user_id, role_id, tenant_id) DO NOTHING
        `, userID, roleID); roleErr != nil {
			err = fmt.Errorf("assign role %s: %w", roleName, roleErr)
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit role assignment: %w", err)
	}
	return nil
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
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if userID == uuid.Nil {
		return nil, errors.New("user id required")
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT r.name
        FROM user_roles ur
        JOIN roles r ON ur.role_id = r.id
        WHERE ur.user_id = $1
        ORDER BY r.name
    `, userID)
	if err != nil {
		return nil, fmt.Errorf("query user roles: %w", err)
	}
	defer rows.Close()

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
