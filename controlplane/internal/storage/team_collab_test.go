package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

func TestListTenantUsersMatchingQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Parallel()

	ctx := context.Background()
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
		postgres.WithInitScripts("../migrate/sql/0001_init.up.sql",
			"../migrate/sql/0003_auth.up.sql",
			"../migrate/sql/0005_seed_roles.up.sql"),
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pg.Terminate(ctx)) })

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	store, err := New(zap.NewNop(), config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	tenantID := uuid.New()

	// Create user A: "Ada CISO"
	ada, err := store.EnsureUser(ctx, "ada-external", "ada@example.com", "Ada CISO")
	require.NoError(t, err)

	// Create user B: "Bob Ops"
	bob, err := store.EnsureUser(ctx, "bob-external", "bob@example.com", "Bob Ops")
	require.NoError(t, err)

	// Create user C: viewer only (should be excluded)
	carol, err := store.EnsureUser(ctx, "carol-external", "carol@example.com", "Carol Viewer")
	require.NoError(t, err)

	// Assign roles via AssignRolesToUser (creates roles via ensureRole if
	// not already seeded, and inserts user_roles with tenant_id NULL).
	err = store.AssignRolesToUser(ctx, ada.ID, []string{"investigator"})
	require.NoError(t, err)
	err = store.AssignRolesToUser(ctx, bob.ID, []string{"operator"})
	require.NoError(t, err)
	err = store.AssignRolesToUser(ctx, carol.ID, []string{"viewer"})
	require.NoError(t, err)

	// Resolve role IDs after assignment (investigator may have been created by ensureRole).
	allRoles, err := store.ListRoles(ctx)
	require.NoError(t, err)
	roleByName := make(map[string]uuid.UUID, len(allRoles))
	for _, r := range allRoles {
		roleByName[r.Name] = r.ID
	}

	// Scoped tenant-role assignments: replicate the rows with a specific tenant_id
	// by inserting directly (AssignRolesToUser sets tenant_id NULL).
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, ada.ID, roleByName["investigator"], tenantID)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, bob.ID, roleByName["operator"], tenantID)
	require.NoError(t, err)

	// Query: search "ada" — should return only Ada.
	results, err := store.ListTenantUsers(ctx, tenantID, "ada", 50)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ada.ID, results[0].ID)
	require.Equal(t, "Ada CISO", results[0].Name)
	require.Equal(t, "ada@example.com", results[0].Email)

	// Query: search "bob" — should return only Bob.
	results, err = store.ListTenantUsers(ctx, tenantID, "bob", 50)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, bob.ID, results[0].ID)

	// Query: empty query — should return both Ada and Bob (investigator/operator), no Carol.
	results, err = store.ListTenantUsers(ctx, tenantID, "", 50)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Query: limit 1.
	results, err = store.ListTenantUsers(ctx, tenantID, "", 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
}
