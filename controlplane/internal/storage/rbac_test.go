package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

func TestRBACAssignmentsWithPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
		postgres.WithInitScripts("../migrate/sql/0001_init.up.sql", "../migrate/sql/0003_auth.up.sql"),
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pg.Terminate(ctx))
	})

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	store, err := New(zap.NewNop(), config.DatabaseConfig{URL: connStr}, Options{Clock: func() time.Time { return time.Unix(1700000000, 0) }})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	user, err := store.EnsureUser(ctx, "external-user", "user@example.com", "Example User")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, "external-user", user.ExternalID)

	// Assign overlapping roles to validate sanitization/uniqueness.
	err = store.AssignRolesToUser(ctx, user.ID, []string{"Admin", "viewer", "admin", "operator "})
	require.NoError(t, err)

	roles, err := store.ListUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"Admin", "viewer", "operator"}, roles)

	// Ensure assigning an existing role does not duplicate entries.
	err = store.AssignRolesToUser(ctx, user.ID, []string{"viewer"})
	require.NoError(t, err)

	roles, err = store.ListUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"Admin", "viewer", "operator"}, roles)

	// Fetch user by external ID and verify ID matches.
	fetched, err := store.GetUserByExternalID(ctx, "external-user")
	require.NoError(t, err)
	require.NotNil(t, fetched)
	require.Equal(t, user.ID, fetched.ID)
}
