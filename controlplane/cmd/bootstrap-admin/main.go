package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func main() {
	configPath := flag.String("config", "config/controlplane.yaml", "path to control plane config file")
	subject := flag.String("subject", "", "external subject/ID for the bootstrap admin user")
	name := flag.String("name", "", "display name for the bootstrap admin user")
	email := flag.String("email", "", "email address for the bootstrap admin user")
	applyMigrations := flag.Bool("apply-migrations", false, "run database migrations before bootstrapping")
	flag.Parse()

	if stringsTrim(*subject) == "" {
		fmt.Fprintln(os.Stderr, "subject is required")
		os.Exit(1)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("load config", zap.Error(err))
	}

	store, err := storage.New(logger, cfg.Database, storage.Options{})
	if err != nil {
		logger.Fatal("connect database", zap.Error(err))
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Warn("close database", zap.Error(err))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if *applyMigrations {
		logger.Info("applying database migrations")
		if err := migrate.Apply(ctx, store.DB()); err != nil {
			logger.Fatal("apply migrations", zap.Error(err))
		}
		logger.Info("database migrations complete")
	}

	user, err := store.EnsureUser(ctx, stringsTrim(*subject), stringsTrim(*email), stringsTrim(*name))
	if err != nil {
		logger.Fatal("ensure user", zap.Error(err))
	}

	if err := store.AssignRolesToUser(ctx, user.ID, []string{"admin"}); err != nil {
		logger.Fatal("assign admin role", zap.Error(err))
	}

	logger.Info("bootstrap admin ensured",
		zap.String("subject", user.ExternalID),
		zap.String("user_id", user.ID.String()),
	)
	fmt.Printf("Bootstrap admin ensured (subject=%s, id=%s)\n", user.ExternalID, user.ID)
}

func stringsTrim(val string) string {
	return strings.TrimSpace(val)
}
