package main

import (
	"context"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/server"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

func main() {
	configPath := pflag.StringP("config", "c", "config/controlplane.yaml", "path to control plane config")
	pflag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Errorf("init logger: %w", err))
	}
	defer logger.Sync() //nolint:errcheck

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("load config", zap.Error(err))
	}

	store, err := storage.New(logger, cfg.Database, storage.Options{})
	if err != nil {
		logger.Fatal("init database", zap.Error(err))
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Warn("close database", zap.Error(err))
		}
	}()

	if err := store.Ping(context.Background()); err != nil {
		logger.Fatal("database ping failed", zap.Error(err))
	}
	logger.Info("database connected")

	if cfg.Database.ApplyMigrations {
		logger.Info("applying database migrations")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := migrate.Apply(ctx, store.DB()); err != nil {
			cancel()
			logger.Fatal("migrate database", zap.Error(err))
		}
		cancel()
		logger.Info("database migrations complete")
	}

	workerMgr := worker.New(logger, cfg.Worker)
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	if err := workerMgr.Start(appCtx); err != nil {
		logger.Fatal("start worker manager", zap.Error(err))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := workerMgr.Stop(shutdownCtx); err != nil {
			logger.Warn("worker stop", zap.Error(err))
		}
	}()

	srv := server.New(logger, cfg, store, workerMgr)

	ctx, cancel := signal.NotifyContext(appCtx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server start", zap.Error(err))
		}
	}()
	logger.Info("control plane started", zap.String("addr", cfg.HTTP.Address))

	<-ctx.Done()
	appCancel()
	logger.Info("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Stop(shutdownCtx); err != nil {
		logger.Error("server shutdown", zap.Error(err))
	}
}
