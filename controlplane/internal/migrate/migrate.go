package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed sql/*.sql
var migrationsFS embed.FS

// Apply runs the embedded SQL migrations against the provided database.
func Apply(ctx context.Context, db *sql.DB) error {
	src, err := iofs.New(migrationsFS, "sql")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- m.Up()
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("migration interrupted: %w", ctx.Err())
	case err := <-done:
		if err == migrate.ErrNoChange {
			return nil
		}
		return err
	}
}
