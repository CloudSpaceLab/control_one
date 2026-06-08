package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestIsBuiltInRoleName(t *testing.T) {
	tests := map[string]bool{
		"admin":        true,
		" CISO ":       true,
		"investigator": true,
		"operator":     true,
		"viewer":       true,
		"soc-reviewer": false,
		"":             false,
	}

	for role, want := range tests {
		t.Run(role, func(t *testing.T) {
			if got := IsBuiltInRoleName(role); got != want {
				t.Fatalf("IsBuiltInRoleName(%q) = %v, want %v", role, got, want)
			}
		})
	}
}

func TestSetRolePermissionsRejectsBuiltInRoleName(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:rbac-permissions-test?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	_, err = db.ExecContext(ctx, `
CREATE TABLE roles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT
);
CREATE TABLE role_permissions (
  role_id TEXT NOT NULL,
  permission_name TEXT NOT NULL,
  UNIQUE(role_id, permission_name)
);`)
	require.NoError(t, err)

	adminID := uuid.New()
	customID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO roles (id, name, description) VALUES ($1, $2, $3), ($4, $5, $6)`,
		adminID.String(), "admin", "Built-in administrator",
		customID.String(), "soc-reviewer", "SOC reviewer",
	)
	require.NoError(t, err)

	store := &Store{db: db}
	require.ErrorIs(t, store.SetRolePermissions(ctx, adminID, []string{"roles.read"}), ErrBuiltInRoleImmutable)
	require.NoError(t, store.SetRolePermissions(ctx, customID, []string{"roles.read"}))

	var adminPermissionCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_permissions WHERE role_id = $1`, adminID.String()).Scan(&adminPermissionCount))
	require.Zero(t, adminPermissionCount)

	var customPermissionCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_permissions WHERE role_id = $1`, customID.String()).Scan(&customPermissionCount))
	require.Equal(t, 1, customPermissionCount)
}
