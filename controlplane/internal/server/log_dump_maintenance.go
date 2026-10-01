package server

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const (
	logDumpMaintenanceInterval = time.Hour
	logDumpCaptureTimeout       = 30 * time.Minute
	logDumpMaintenanceBatch     = 100
	logDumpCleanupRetryMax      = 30 * time.Minute
)

type logDumpMaintenanceStore interface {
	ListTimedOutLogDumps(context.Context, time.Time, time.Time, int) ([]storage.LogDump, error)
	FailLogDumpAndJob(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string, *bool, string, time.Time) error
	ListLogDumpCleanupCandidates(context.Context, time.Time, int) ([]storage.LogDump, error)
	ExpireLogDump(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error)
	MarkLogDumpDeleting(context.Context, uuid.UUID) error
	MarkLogDumpCleanupRetry(context.Context, uuid.UUID, string, time.Time) error
	DeleteLogDump(context.Context, uuid.UUID) error
}

func (s *Server) startLogDumpMaintenance() {
	if s == nil || s.store == nil || s.logDumpMaintenanceCancel != nil {
		return
	}
	store, ok := s.store.(logDumpMaintenanceStore)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.logDumpMaintenanceCancel = cancel
	s.logDumpMaintenanceWG.Add(1)
	go func() {
		defer s.logDumpMaintenanceWG.Done()
		runLogDumpMaintenanceOnce(ctx, s.logger, store, s.logDumpNow())
		ticker := time.NewTicker(logDumpMaintenanceInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runLogDumpMaintenanceOnce(ctx, s.logger, store, s.logDumpNow())
			}
		}
	}()
}

func (s *Server) stopLogDumpMaintenance() {
	if s == nil || s.logDumpMaintenanceCancel == nil {
		return
	}
	s.logDumpMaintenanceCancel()
	s.logDumpMaintenanceWG.Wait()
	s.logDumpMaintenanceCancel = nil
}

func runLogDumpMaintenanceOnce(ctx context.Context, logger *zap.Logger, store logDumpMaintenanceStore, now time.Time) {
	if store == nil {
		return
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	timedOut, err := store.ListTimedOutLogDumps(ctx, now, now.Add(-logDumpCaptureTimeout), logDumpMaintenanceBatch)
	if err != nil {
		logger.Warn("list timed out log dumps", zap.Error(err))
	} else {
		for i := range timedOut {
			d := timedOut[i]
			if !d.JobID.Valid {
				continue
			}
			if err := store.FailLogDumpAndJob(ctx, d.TenantID, d.NodeID, d.ID, d.JobID.UUID,
				"raw log dump capture timed out", d.SourceAvailable, d.SourceReason, now); err != nil {
				logger.Warn("fail timed out log dump", zap.String("dump_id", d.ID.String()), zap.Error(err))
			}
		}
	}

	candidates, err := store.ListLogDumpCleanupCandidates(ctx, now, logDumpMaintenanceBatch)
	if err != nil {
		logger.Warn("list log dump cleanup candidates", zap.Error(err))
		return
	}
	for i := range candidates {
		if ctx.Err() != nil {
			return
		}
		d := candidates[i]
		if d.Status != storage.LogDumpStatusExpired && d.Status != storage.LogDumpStatusDeleting {
			if _, err := store.ExpireLogDump(ctx, d.TenantID, d.ID, now); err != nil {
				logger.Warn("expire log dump", zap.String("dump_id", d.ID.String()), zap.Error(err))
				continue
			}
		}
		if err := store.MarkLogDumpDeleting(ctx, d.ID); err != nil {
			logger.Warn("mark log dump deleting", zap.String("dump_id", d.ID.String()), zap.Error(err))
			continue
		}
		if err := removeScopedLogDumpFile(d.ArtifactPath); err != nil {
			scheduleLogDumpCleanupRetry(ctx, logger, store, d, now, fmt.Errorf("remove artifact: %w", err))
			continue
		}
		if err := removeLogDumpChunkFiles(d.ID); err != nil {
			scheduleLogDumpCleanupRetry(ctx, logger, store, d, now, err)
			continue
		}
		if err := store.DeleteLogDump(ctx, d.ID); err != nil {
			scheduleLogDumpCleanupRetry(ctx, logger, store, d, now, fmt.Errorf("delete metadata: %w", err))
		}
	}
}

func scheduleLogDumpCleanupRetry(ctx context.Context, logger *zap.Logger, store logDumpMaintenanceStore, d storage.LogDump, now time.Time, cause error) {
	delay := time.Minute
	for i := 0; i < d.CleanupAttempts && delay < logDumpCleanupRetryMax; i++ {
		delay *= 2
	}
	if delay > logDumpCleanupRetryMax {
		delay = logDumpCleanupRetryMax
	}
	if err := store.MarkLogDumpCleanupRetry(ctx, d.ID, cause.Error(), now.Add(delay)); err != nil {
		logger.Warn("schedule log dump cleanup retry", zap.String("dump_id", d.ID.String()), zap.Error(err))
	}
}
