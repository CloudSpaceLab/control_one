package migrate

import (
	"context"
	"database/sql"
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

func TestMigrationsUpAndDown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if _, _, err := tc.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pgContainer, err := tcpostgres.Run(ctx, "docker.io/postgres:16-alpine",
		tcpostgres.WithDatabase("control_one"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tc.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pgContainer.Terminate(ctx)
	})

	uri, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

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
