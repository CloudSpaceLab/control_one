package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Permission is one entry in the catalog. Names are dotted strings like
// "tenants.read"; categories group them in the admin UI.
type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// RolePermissions extends Role with the granted-permissions list. Kept
// separate so legacy callers of Role aren't disrupted.
type RolePermissions struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	BuiltIn     bool      `json:"built_in"`
}

var builtInRoleNames = map[string]struct{}{
	"admin":        {},
	"ciso":         {},
	"investigator": {},
	"operator":     {},
	"viewer":       {},
}

func IsBuiltInRoleName(name string) bool {
	_, ok := builtInRoleNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// ListPermissions returns the canonical permission catalog. Sorted by
// category then name so the admin UI renders them grouped.
func (s *Store) ListPermissions(ctx context.Context) ([]Permission, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, COALESCE(description,''), COALESCE(category,'general')
FROM permissions ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Permission, 0, 32)
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.Name, &p.Description, &p.Category); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListRolesWithPermissions joins roles + role_permissions in one query.
func (s *Store) ListRolesWithPermissions(ctx context.Context) ([]RolePermissions, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id, r.name, COALESCE(r.description,''),
       COALESCE(ARRAY_AGG(rp.permission_name) FILTER (WHERE rp.permission_name IS NOT NULL), ARRAY[]::text[])
FROM roles r
LEFT JOIN role_permissions rp ON rp.role_id = r.id
GROUP BY r.id, r.name, r.description
ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]RolePermissions, 0, 8)
	for rows.Next() {
		var r RolePermissions
		var perms pq.StringArray
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &perms); err != nil {
			return nil, err
		}
		r.Permissions = []string(perms)
		r.BuiltIn = IsBuiltInRoleName(r.Name)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetRolePermissions replaces the permission set for a role atomically.
func (s *Store) SetRolePermissions(ctx context.Context, roleID uuid.UUID, perms []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	for _, p := range perms {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO role_permissions (role_id, permission_name) VALUES ($1, $2)
              ON CONFLICT DO NOTHING`,
			roleID, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateCustomRole adds a tenant-defined role beyond the built-ins.
func (s *Store) CreateCustomRole(ctx context.Context, name, description string, permissions []string) (*RolePermissions, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("role name required")
	}
	if IsBuiltInRoleName(name) {
		return nil, errors.New("role name is reserved")
	}
	id := uuid.New()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO roles (id, name, description) VALUES ($1, $2, $3)`,
		id, name, description); err != nil {
		return nil, err
	}
	if len(permissions) > 0 {
		if err := s.SetRolePermissions(ctx, id, permissions); err != nil {
			return nil, err
		}
	}
	return &RolePermissions{ID: id, Name: name, Description: description, Permissions: permissions, BuiltIn: false}, nil
}

// DeleteRoleByID removes a custom role. Built-ins are identified by canonical
// role name because older migrations intentionally kept legacy UUIDs.
func (s *Store) DeleteRoleByID(ctx context.Context, roleID uuid.UUID) error {
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM roles WHERE id = $1`, roleID).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if IsBuiltInRoleName(name) {
		return errors.New("cannot delete built-in role")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM roles WHERE id = $1`, roleID)
	return err
}

// GetUserPermissions returns the union of permission names for every role
// the user holds. Cached per-request by the auth middleware.
func (s *Store) GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT rp.permission_name
FROM user_roles ur
JOIN role_permissions rp ON rp.role_id = ur.role_id
WHERE ur.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0, 16)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
