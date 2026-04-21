package migrate

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// createThrowawayDatabase carves out a unique database inside an externally
// provided postgres cluster and drops it after the test. Used when the
// CONTROL_ONE_TEST_DB_URL env var is set; see storage.setupPostgresStoreFull
// for the sibling helper.
func createThrowawayDatabase(t *testing.T, ctx context.Context, baseDSN string) string {
	t.Helper()

	admin, err := sql.Open("postgres", baseDSN)
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()

	dbName := "co_mig_" + randomDBSuffix()
	_, err = admin.ExecContext(ctx, `CREATE DATABASE `+dbName)
	require.NoError(t, err)
	t.Cleanup(func() {
		drop, err := sql.Open("postgres", baseDSN)
		if err == nil {
			_, _ = drop.Exec(`DROP DATABASE IF EXISTS ` + dbName + ` WITH (FORCE)`)
			_ = drop.Close()
		}
	})

	// Rewrite the DSN's path component with the new database name.
	qIdx := -1
	for i := 0; i < len(baseDSN); i++ {
		if baseDSN[i] == '?' {
			qIdx = i
			break
		}
	}
	head := baseDSN
	query := ""
	if qIdx >= 0 {
		head = baseDSN[:qIdx]
		query = baseDSN[qIdx:]
	}
	slashIdx := -1
	for i := len(head) - 1; i >= 0; i-- {
		if head[i] == '/' {
			slashIdx = i
			break
		}
	}
	require.Greater(t, slashIdx, 0, "DSN must contain /dbname")
	return head[:slashIdx+1] + dbName + query
}

func randomDBSuffix() string {
	// time-derived suffix; unique enough for a single go-test invocation and
	// avoids pulling in uuid to keep this file's import list minimal.
	return time.Now().UTC().Format("20060102150405") + "_" + time.Now().UTC().Format("000000000")
}

func TestMigrationsUpAndDown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()

	var uri string

	if envDSN := os.Getenv("CONTROL_ONE_TEST_DB_URL"); envDSN != "" {
		// Local dev bypass: use a dedicated throwaway database when docker
		// credential helpers break the testcontainers image-auth check.
		uri = createThrowawayDatabase(t, ctx, envDSN)
	} else {
		if _, _, err := tc.DockerImageAuth(ctx, "postgres:latest"); err != nil {
			t.Skipf("skipping: docker daemon unavailable: %v", err)
		}

		pgContainer, err := tcpostgres.Run(ctx, "docker.io/postgres:16-alpine",
			tcpostgres.WithDatabase("control_one"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
			tc.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(60*time.Second),
			),
		)
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = pgContainer.Terminate(ctx)
		})

		uri, err = pgContainer.ConnectionString(ctx, "sslmode=disable")
		require.NoError(t, err)
	}

	db, err := sql.Open("postgres", uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	applyCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	require.NoError(t, Apply(applyCtx, db))

	// Verify role seeds and provisioning tables exist.
	var roleCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles`).Scan(&roleCount))
	require.GreaterOrEqual(t, roleCount, 3)

	var tableExists bool
	require.NoError(t, db.QueryRowContext(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_name = 'provisioning_templates'
        )`).Scan(&tableExists))
	require.True(t, tableExists, "provisioning_templates table should exist after up migrations")

	// Run down migrations to ensure reversibility (including 0004).
	src, err := iofs.New(migrationsFS, "sql")
	require.NoError(t, err)
	driverDown, err := migratepg.WithInstance(db, &migratepg.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driverDown)
	require.NoError(t, err)

	downCtx, downCancel := context.WithTimeout(ctx, time.Minute)
	defer downCancel()

	done := make(chan error, 1)
	go func() {
		done <- m.Down()
	}()

	select {
	case <-downCtx.Done():
		t.Fatalf("down migration timed out: %v", downCtx.Err())
	case err := <-done:
		if err != nil && err != migrate.ErrNoChange {
			t.Fatalf("down migration failed: %v", err)
		}
	}
}
