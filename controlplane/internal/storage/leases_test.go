package storage

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	testcontainers "github.com/testcontainers/testcontainers-go"
	postgrestc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
)

// setupPostgresStoreFull spins up a postgres container, applies ALL embedded
// migrations (not just 0001-0006), and returns a Store. Needed for anything
// that touches remediation_scripts, remediation_leases, or
// compliance_results.verified.
func setupPostgresStoreFull(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgrestc.Run(ctx, "docker.io/postgres:16-alpine",
		postgrestc.WithDatabase("control_one"),
		postgrestc.WithUsername("postgres"),
		postgrestc.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pg.Terminate(ctx)
	})

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	applyCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	require.NoError(t, migrate.Apply(applyCtx, db))

	logger := zap.NewNop()
	store, err := New(logger, config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

// seedLeasePrereqs inserts a tenant, node, and job so foreign keys on
// remediation_leases are satisfied.
func seedLeasePrereqs(t *testing.T, ctx context.Context, store *Store) (tenantID, nodeID, jobID uuid.UUID) {
	t.Helper()
	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "lease-tenant-" + uuid.NewString()[:8]})
	require.NoError(t, err)

	node, err := store.CreateNode(ctx, &Node{
		ID:       uuid.New(),
		TenantID: tenant.ID,
		Hostname: "lease-node-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	job, err := store.CreateJob(ctx, &Job{
		TenantID: tenant.ID,
		Type:     "remediation.execute",
		Status:   JobStatusQueued,
		Payload:  []byte(`{}`),
	}, nil)
	require.NoError(t, err)

	return tenant.ID, node.ID, job.ID
}

// TestRemediationLeases exercises the whole lease surface under a single
// shared postgres container. Each subtest uses fresh tenants/nodes/jobs so
// they don't collide. Running under one container avoids overwhelming the
// local docker engine with 5+ simultaneous postgres spins.
func TestRemediationLeases(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	t.Run("contention returns ErrLeaseHeld", func(t *testing.T) {
		tenantID, nodeID, jobID := seedLeasePrereqs(t, ctx, store)

		lease, err := store.AcquireRemediationLease(ctx, tenantID, nodeID, jobID, time.Minute)
		require.NoError(t, err)
		require.NotNil(t, lease)
		require.Equal(t, nodeID, lease.NodeID)
		require.True(t, lease.ExpiresAt.After(lease.AcquiredAt))

		// Second acquire on same node with a different job should return ErrLeaseHeld.
		_, _, jobID2 := seedLeasePrereqs(t, ctx, store)
		_, err = store.AcquireRemediationLease(ctx, tenantID, nodeID, jobID2, time.Minute)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrLeaseHeld), "expected ErrLeaseHeld, got %v", err)

		// Release and acquire again succeeds.
		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeID))
		lease2, err := store.AcquireRemediationLease(ctx, tenantID, nodeID, jobID, time.Minute)
		require.NoError(t, err)
		require.NotNil(t, lease2)

		// Clean up for the next subtest.
		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeID))
	})

	t.Run("parallel goroutines, exactly one wins", func(t *testing.T) {
		tenantID, nodeID, jobID := seedLeasePrereqs(t, ctx, store)
		_, _, jobID2 := seedLeasePrereqs(t, ctx, store)

		var wg sync.WaitGroup
		results := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := store.AcquireRemediationLease(ctx, tenantID, nodeID, jobID, time.Minute)
			results[0] = err
		}()
		go func() {
			defer wg.Done()
			_, err := store.AcquireRemediationLease(ctx, tenantID, nodeID, jobID2, time.Minute)
			results[1] = err
		}()
		wg.Wait()

		winCount := 0
		for _, err := range results {
			if err == nil {
				winCount++
			} else {
				require.True(t, errors.Is(err, ErrLeaseHeld), "expected ErrLeaseHeld for loser, got %v", err)
			}
		}
		require.Equal(t, 1, winCount, "exactly one goroutine should win the lease")

		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeID))
	})

	t.Run("expired lease is swept by next acquire", func(t *testing.T) {
		tenantID, nodeID, jobID := seedLeasePrereqs(t, ctx, store)
		_, _, jobID2 := seedLeasePrereqs(t, ctx, store)

		// Acquire with a very short TTL.
		lease, err := store.AcquireRemediationLease(ctx, tenantID, nodeID, jobID, 10*time.Millisecond)
		require.NoError(t, err)
		require.NotNil(t, lease)

		time.Sleep(80 * time.Millisecond)

		// Count should now reflect that the lease is expired.
		count, err := store.CountTenantLeases(ctx, tenantID)
		require.NoError(t, err)
		require.Equal(t, 0, count, "expired lease should not count as in-flight")

		// New acquire reclaims the expired slot.
		lease2, err := store.AcquireRemediationLease(ctx, tenantID, nodeID, jobID2, time.Minute)
		require.NoError(t, err, "expected expired lease to be swept and replaced")
		require.Equal(t, jobID2, lease2.JobID, "new lease should reference the new job")

		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeID))
	})

	t.Run("release is idempotent", func(t *testing.T) {
		tenantID, nodeID, jobID := seedLeasePrereqs(t, ctx, store)

		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeID))

		_, err := store.AcquireRemediationLease(ctx, tenantID, nodeID, jobID, time.Minute)
		require.NoError(t, err)
		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeID))
		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeID))
	})

	t.Run("count is per-tenant", func(t *testing.T) {
		tenantA, nodeA, jobA := seedLeasePrereqs(t, ctx, store)
		tenantB, nodeB, jobB := seedLeasePrereqs(t, ctx, store)

		_, err := store.AcquireRemediationLease(ctx, tenantA, nodeA, jobA, time.Minute)
		require.NoError(t, err)
		_, err = store.AcquireRemediationLease(ctx, tenantB, nodeB, jobB, time.Minute)
		require.NoError(t, err)

		countA, err := store.CountTenantLeases(ctx, tenantA)
		require.NoError(t, err)
		require.Equal(t, 1, countA)

		countB, err := store.CountTenantLeases(ctx, tenantB)
		require.NoError(t, err)
		require.Equal(t, 1, countB)

		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeA))
		require.NoError(t, store.ReleaseRemediationLease(ctx, nodeB))
	})

	t.Run("input validation", func(t *testing.T) {
		_, err := store.AcquireRemediationLease(ctx, uuid.Nil, uuid.New(), uuid.New(), time.Minute)
		require.Error(t, err)

		_, err = store.AcquireRemediationLease(ctx, uuid.New(), uuid.Nil, uuid.New(), time.Minute)
		require.Error(t, err)

		_, err = store.AcquireRemediationLease(ctx, uuid.New(), uuid.New(), uuid.Nil, time.Minute)
		require.Error(t, err)

		_, err = store.AcquireRemediationLease(ctx, uuid.New(), uuid.New(), uuid.New(), 0)
		require.Error(t, err)

		require.Error(t, store.ReleaseRemediationLease(ctx, uuid.Nil))

		_, err = store.CountTenantLeases(ctx, uuid.Nil)
		require.Error(t, err)
	})
}
