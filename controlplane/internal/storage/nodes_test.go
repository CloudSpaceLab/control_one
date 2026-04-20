package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
)

// setupPostgresStoreWithMigrations brings up a Postgres container and runs the
// full migration chain, including 0022. This is required for tests that touch
// the machine_id / state columns which don't exist in setupPostgresStore's
// init-scripts-only bootstrap.
func setupPostgresStoreWithMigrations(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
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
	t.Cleanup(func() {
		require.NoError(t, pg.Terminate(ctx))
	})

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	store, err := New(zap.NewNop(), config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	applyCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	require.NoError(t, migrate.Apply(applyCtx, store.DB()))

	return store
}

func TestGetNodeByMachineIDRoundtrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-machine-id"})
	require.NoError(t, err)

	machineID := "11111111-1111-1111-1111-111111111111"
	node, err := store.CreateNode(ctx, &Node{
		TenantID:  tenant.ID,
		Hostname:  "host-a",
		MachineID: sql.NullString{String: machineID, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, NodeStateActive, node.State)

	found, err := store.GetNodeByMachineID(ctx, tenant.ID, machineID)
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, node.ID, found.ID)
	require.Equal(t, machineID, found.MachineID.String)

	// Miss — different tenant.
	otherTenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-other"})
	require.NoError(t, err)
	miss, err := store.GetNodeByMachineID(ctx, otherTenant.ID, machineID)
	require.NoError(t, err)
	require.Nil(t, miss)

	// Miss — unknown machine_id.
	miss2, err := store.GetNodeByMachineID(ctx, tenant.ID, "does-not-exist")
	require.NoError(t, err)
	require.Nil(t, miss2)
}

func TestMachineIDUniqueIndexPerTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-unique"})
	require.NoError(t, err)

	machineID := "stable-machine-id-xyz"
	_, err = store.CreateNode(ctx, &Node{
		TenantID:  tenant.ID,
		Hostname:  "first",
		MachineID: sql.NullString{String: machineID, Valid: true},
	})
	require.NoError(t, err)

	_, err = store.CreateNode(ctx, &Node{
		TenantID:  tenant.ID,
		Hostname:  "duplicate",
		MachineID: sql.NullString{String: machineID, Valid: true},
	})
	require.Error(t, err, "duplicate machine_id in tenant must be rejected")

	// Another tenant with the same machine_id is fine (partial index is per tenant).
	otherTenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-unique-other"})
	require.NoError(t, err)
	_, err = store.CreateNode(ctx, &Node{
		TenantID:  otherTenant.ID,
		Hostname:  "shared-id",
		MachineID: sql.NullString{String: machineID, Valid: true},
	})
	require.NoError(t, err)
}

func TestMultipleNullMachineIDsAllowed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-null"})
	require.NoError(t, err)

	_, err = store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "legacy-1"})
	require.NoError(t, err)

	// A second legacy node (no machine_id) must not conflict on the partial index.
	_, err = store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "legacy-2"})
	require.NoError(t, err)
}

func TestRetireNodeSetsState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-retire"})
	require.NoError(t, err)

	node, err := store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "bye"})
	require.NoError(t, err)
	require.Equal(t, NodeStateActive, node.State)

	require.NoError(t, store.RetireNode(ctx, node.ID))

	fetched, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	require.Equal(t, NodeStateRetired, fetched.State)

	// Retire a missing id returns ErrNoRows.
	err = store.RetireNode(ctx, uuid.New())
	require.ErrorIs(t, err, sql.ErrNoRows)
}
