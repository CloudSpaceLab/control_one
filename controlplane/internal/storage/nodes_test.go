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
	// Post-0028 the default state is enrollment_pending. Retire still applies.
	require.Equal(t, NodeStateEnrollmentPending, node.State)

	require.NoError(t, store.RetireNode(ctx, node.ID))

	fetched, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	require.Equal(t, NodeStateRetired, fetched.State)

	// Retire a missing id returns ErrNoRows.
	err = store.RetireNode(ctx, uuid.New())
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// TestCreateNodeDefaultsToEnrollmentPending locks in the Sprint 2 state
// default. Pre-0028 this was 'active'; new rows now wait for the heartbeat
// + first-scan gate before activating.
func TestCreateNodeDefaultsToEnrollmentPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-default"})
	require.NoError(t, err)

	node, err := store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "pending"})
	require.NoError(t, err)
	require.Equal(t, NodeStateEnrollmentPending, node.State)
	require.Nil(t, node.LastSeenAt)
	require.Nil(t, node.FirstScanAt)
	require.Equal(t, map[string]any{}, node.Labels)
}

// TestNodeStateCheckRejectsInvalid verifies migration 0028 locked in the
// enumerated state list. Any value outside the set must be rejected by the
// CHECK constraint.
func TestNodeStateCheckRejectsInvalid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-check"})
	require.NoError(t, err)

	_, err = store.CreateNode(ctx, &Node{
		TenantID: tenant.ID,
		Hostname: "bogus",
		State:    "not-a-real-state",
	})
	require.Error(t, err, "invalid state must trigger CHECK failure")
}

// TestTouchNodeHeartbeatBumpsLastSeen covers the happy path and the ErrNoRows
// branch for unknown ids.
func TestTouchNodeHeartbeatBumpsLastSeen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-hb"})
	require.NoError(t, err)

	node, err := store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "hb-host"})
	require.NoError(t, err)
	require.Nil(t, node.LastSeenAt)

	refreshed, err := store.TouchNodeHeartbeat(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, refreshed.LastSeenAt)
	require.WithinDuration(t, time.Now(), *refreshed.LastSeenAt, 5*time.Second)

	// Second call bumps again — last_seen_at should move forward.
	first := *refreshed.LastSeenAt
	time.Sleep(5 * time.Millisecond)
	refreshed2, err := store.TouchNodeHeartbeat(ctx, node.ID)
	require.NoError(t, err)
	require.True(t, refreshed2.LastSeenAt.After(first) || refreshed2.LastSeenAt.Equal(first))

	// Unknown id returns ErrNoRows.
	_, err = store.TouchNodeHeartbeat(ctx, uuid.New())
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// TestMarkNodeFirstScanIsIdempotent pins the COALESCE behaviour: the first
// call stamps the timestamp, subsequent calls leave it alone.
func TestMarkNodeFirstScanIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-scan"})
	require.NoError(t, err)

	node, err := store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "scan-host"})
	require.NoError(t, err)
	require.Nil(t, node.FirstScanAt)

	first, err := store.MarkNodeFirstScan(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, first.FirstScanAt)
	firstStamp := *first.FirstScanAt

	time.Sleep(5 * time.Millisecond)
	second, err := store.MarkNodeFirstScan(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, second.FirstScanAt)
	require.True(t, second.FirstScanAt.Equal(firstStamp), "first_scan_at must not move on second call")

	_, err = store.MarkNodeFirstScan(ctx, uuid.New())
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// TestSetNodeStateTransition checks the explicit transition primitive the
// reaper + heartbeat gate both use.
func TestSetNodeStateTransition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-state"})
	require.NoError(t, err)

	node, err := store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "state-host"})
	require.NoError(t, err)
	require.Equal(t, NodeStateEnrollmentPending, node.State)

	require.NoError(t, store.SetNodeState(ctx, node.ID, NodeStateActive))
	refreshed, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, NodeStateActive, refreshed.State)

	require.NoError(t, store.SetNodeState(ctx, node.ID, NodeStateEnrollmentFailed))
	refreshed, err = store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, NodeStateEnrollmentFailed, refreshed.State)

	// Unknown state must be rejected by the CHECK constraint.
	err = store.SetNodeState(ctx, node.ID, "banana")
	require.Error(t, err)

	// Unknown id returns ErrNoRows.
	err = store.SetNodeState(ctx, uuid.New(), NodeStateActive)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// TestUpdateNodeLabelsRoundtrip asserts labels persist + retrieve as a JSONB
// object. Worktrees C and E consume these keys downstream.
func TestUpdateNodeLabelsRoundtrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-labels"})
	require.NoError(t, err)

	node, err := store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "labelled"})
	require.NoError(t, err)

	require.NoError(t, store.UpdateNodeLabels(ctx, node.ID, map[string]any{
		"remediation": "manual-only",
		"env":         "prod",
	}))

	refreshed, err := store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, "manual-only", refreshed.Labels["remediation"])
	require.Equal(t, "prod", refreshed.Labels["env"])

	// Overwriting with nil/empty should produce an empty map, never NULL.
	require.NoError(t, store.UpdateNodeLabels(ctx, node.ID, nil))
	refreshed, err = store.GetNode(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]any{}, refreshed.Labels)

	// Unknown id returns ErrNoRows.
	err = store.UpdateNodeLabels(ctx, uuid.New(), map[string]any{"k": "v"})
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// TestListEnrollmentPendingNodesOlderThan is the reaper query. Only nodes
// stuck in pending whose created_at predates the cutoff are returned.
func TestListEnrollmentPendingNodesOlderThan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreWithMigrations(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tn-reaper"})
	require.NoError(t, err)

	// A pending node created "long ago".
	oldNode, err := store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "old-pending"})
	require.NoError(t, err)

	// An active node — must not show up.
	activeNode, err := store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "active-host"})
	require.NoError(t, err)
	require.NoError(t, store.SetNodeState(ctx, activeNode.ID, NodeStateActive))

	// A pending node created in the future (relative to cutoff) — must not
	// show up either.
	_, err = store.CreateNode(ctx, &Node{TenantID: tenant.ID, Hostname: "fresh-pending"})
	require.NoError(t, err)

	// Roll the old pending node's created_at back in time so it's older
	// than the cutoff.
	_, err = store.DB().ExecContext(ctx, `UPDATE nodes SET created_at = $2 WHERE id = $1`,
		oldNode.ID, time.Now().Add(-20*time.Minute))
	require.NoError(t, err)

	pending, err := store.ListEnrollmentPendingNodesOlderThan(ctx, time.Now().Add(-10*time.Minute))
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, oldNode.ID, pending[0].ID)
}
