package server

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// RetentionScheduler walks tenants on a cron and prunes telemetry rows whose
// age exceeds the retention policy in storage.telemetry_retention_policies.
// Doris partitions handle their own TTL via dynamic_partition; this scheduler
// only addresses Postgres event tables that haven't migrated to Doris yet
// (the dual-write window) and the legacy `telemetry_logs` /
// `telemetry_metrics` tables.
type RetentionScheduler struct {
	cron   *cron.Cron
	server *Server
	logger *zap.Logger
}

const eventIngestJournalRetention = 24 * time.Hour

// NewRetentionScheduler creates a scheduler bound to the given server.
func NewRetentionScheduler(s *Server) *RetentionScheduler {
	return &RetentionScheduler{
		cron:   cron.New(),
		server: s,
		logger: s.logger.Named("retention-scheduler"),
	}
}

// Start registers the cron expression and begins ticking. Default once every
// six hours. Returns the scheduler so the caller can Stop later.
func (rs *RetentionScheduler) Start(schedule string) error {
	if schedule == "" {
		schedule = "0 */6 * * *"
	}
	_, err := rs.cron.AddFunc(schedule, func() {
		rs.runOnce()
	})
	if err != nil {
		return fmt.Errorf("register retention schedule %q: %w", schedule, err)
	}
	rs.cron.Start()
	rs.logger.Info("retention scheduler started", zap.String("schedule", schedule))
	// Kick once at startup so freshly-deployed clusters don't wait six
	// hours for the first sweep.
	go rs.runOnce()
	return nil
}

// Stop halts the cron scheduler.
func (rs *RetentionScheduler) Stop() {
	ctx := rs.cron.Stop()
	<-ctx.Done()
	rs.logger.Info("retention scheduler stopped")
}

// runOnce performs one full pass: per-tenant logs + metrics retention,
// then a global sweep for policies with NULL tenant_id.
func (rs *RetentionScheduler) runOnce() {
	if rs.server == nil || rs.server.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	tenants, _, err := rs.server.store.ListTenants(ctx, "", 1000, 0)
	if err != nil {
		rs.logger.Error("list tenants for retention", zap.Error(err))
		return
	}
	totalLogs := int64(0)
	totalMetrics := int64(0)
	totalEventIngestBatches := int64(0)
	for _, t := range tenants {
		n, err := rs.server.store.DeleteExpiredTelemetry(ctx, t.ID, "logs")
		if err != nil {
			rs.logger.Warn("delete expired logs", zap.String("tenant_id", t.ID.String()), zap.Error(err))
		}
		totalLogs += n
		m, err := rs.server.store.DeleteExpiredTelemetry(ctx, t.ID, "metrics")
		if err != nil {
			rs.logger.Warn("delete expired metrics", zap.String("tenant_id", t.ID.String()), zap.Error(err))
		}
		totalMetrics += m
	}
	// Global / unowned policies (tenant_id NULL).
	if n, err := rs.server.store.DeleteExpiredTelemetry(ctx, uuid.Nil, "logs"); err == nil {
		totalLogs += n
	}
	if m, err := rs.server.store.DeleteExpiredTelemetry(ctx, uuid.Nil, "metrics"); err == nil {
		totalMetrics += m
	}
	if n, err := rs.server.store.PruneAcceptedEventIngestBatches(ctx, eventIngestJournalRetention); err != nil {
		rs.logger.Warn("prune event ingest replay journal", zap.Duration("retention", eventIngestJournalRetention), zap.Error(err))
	} else {
		totalEventIngestBatches = n
	}
	rs.logger.Info("retention sweep complete",
		zap.Int64("rows_logs", totalLogs),
		zap.Int64("rows_metrics", totalMetrics),
		zap.Int64("event_ingest_batches", totalEventIngestBatches),
		zap.Int("tenants", len(tenants)),
	)
}
