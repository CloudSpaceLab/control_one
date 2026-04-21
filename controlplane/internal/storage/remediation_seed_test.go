package storage_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	testcontainers "github.com/testcontainers/testcontainers-go"
	postgrestc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/CloudSpaceLab/control_one/internal/remediation"
)

// TestRemediationSeeder_PersistsCatalogAndIsIdempotent boots a real postgres,
// applies every migration, runs the starter-pack seeder twice, and asserts
// the table ends up with exactly the catalog size. This is the integration
// gate for Gap 2.7 — the /api/v1/remediation/scripts endpoint will return
// these rows on every fresh deploy once main.go wires the seeder in.
//
// Lives under controlplane/internal/storage_test because the embedded migrate
// package is internal to the controlplane module — external packages can't
// import it directly, but sibling packages under the same internal boundary
// can.
func TestRemediationSeeder_PersistsCatalogAndIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
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

	catalog, err := remediation.LoadSeedCatalog()
	require.NoError(t, err)
	expectedCount := len(catalog)
	require.Equal(t, 25, expectedCount, "catalog size regressed")

	seeder := remediation.NewSeeder(db)

	// First pass: should insert every catalog entry.
	stats, err := seeder.Seed(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedCount, stats.Total)
	require.Equal(t, expectedCount, stats.Inserted, "first run should insert all rows")
	require.Equal(t, 0, stats.Skipped, "first run should skip nothing")

	// The table should now hold exactly expectedCount seeded rows.
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM remediation_scripts`).Scan(&count))
	require.Equal(t, expectedCount, count, "remediation_scripts row count mismatch")

	// Every seeded row must carry a rollback body and matching checksum — the
	// Gap 2.2 rollback pipeline assumes those columns exist.
	var rowsMissingRollback int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM remediation_scripts
		WHERE rollback_content IS NULL
		   OR rollback_checksum IS NULL
		   OR LENGTH(rollback_content) = 0
	`).Scan(&rowsMissingRollback))
	require.Equal(t, 0, rowsMissingRollback, "seeded rows must have rollback content + checksum")

	// Second pass: should be a complete no-op.
	stats2, err := seeder.Seed(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedCount, stats2.Total)
	require.Equal(t, 0, stats2.Inserted, "second run should insert nothing (idempotent)")
	require.Equal(t, expectedCount, stats2.Skipped, "second run should skip every entry")

	// Row count must not change.
	var recount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM remediation_scripts`).Scan(&recount))
	require.Equal(t, expectedCount, recount, "idempotent seed duplicated rows")

	// Per-platform breakdown sanity check.
	var linuxCount, windowsCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM remediation_scripts WHERE platform = 'linux'`).Scan(&linuxCount))
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM remediation_scripts WHERE platform = 'windows'`).Scan(&windowsCount))
	require.Equal(t, 15, linuxCount, "expected 15 linux seeds")
	require.Equal(t, 10, windowsCount, "expected 10 windows seeds")

	// Every row must have the enabled flag set and version=1 — the engine
	// filters on these columns when dispatching a remediation.
	var disabledCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM remediation_scripts WHERE enabled IS NOT TRUE OR version <> 1`).
		Scan(&disabledCount))
	require.Equal(t, 0, disabledCount, "seeded rows must be enabled at version 1")
}
