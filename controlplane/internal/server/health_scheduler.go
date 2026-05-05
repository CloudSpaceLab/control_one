package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

// HealthScheduler runs the hourly predictive-health pipeline:
//  1. health.compute_baselines — EWMA over telemetry_metrics per node
//  2. health.predict          — score each node and upsert node_health_scores
//
// Both jobs are fleet-wide (no tenant scope in payload). The scheduler
// enqueues them sequentially so baselines are always fresh before scoring.
type HealthScheduler struct {
	cron   *cron.Cron
	server *Server
	logger *zap.Logger
}

// NewHealthScheduler creates a scheduler bound to the given server.
func NewHealthScheduler(s *Server) *HealthScheduler {
	return &HealthScheduler{
		cron:   cron.New(),
		server: s,
		logger: s.logger.Named("health-scheduler"),
	}
}

// Start registers the cron schedule and begins ticking.
func (hs *HealthScheduler) Start(schedule string) error {
	_, err := hs.cron.AddFunc(schedule, hs.run)
	if err != nil {
		return fmt.Errorf("register health schedule %q: %w", schedule, err)
	}
	hs.cron.Start()
	hs.logger.Info("health scheduler started", zap.String("schedule", schedule))
	return nil
}

// Stop halts the cron scheduler gracefully.
func (hs *HealthScheduler) Stop() {
	ctx := hs.cron.Stop()
	<-ctx.Done()
	hs.logger.Info("health scheduler stopped")
}

// run fires on each cron tick. It enqueues baselines first, then predict.
// Both jobs are fleet-wide; the handlers iterate all tenants internally.
func (hs *HealthScheduler) run() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Baselines must finish before predict reads from behavioral_baselines.
	// With the in-memory worker these run sequentially inside the same goroutine
	// pool; enqueueing baselines first is the best we can do without a DAG.
	hs.enqueueJob(ctx, JobTypeHealthBaselines)
	hs.enqueueJob(ctx, JobTypeHealthPredict)
}

func (hs *HealthScheduler) enqueueJob(ctx context.Context, jobType string) {
	if hs.server.store == nil {
		hs.logger.Warn("store unavailable, skipping health job", zap.String("type", jobType))
		return
	}

	payload, _ := json.Marshal(map[string]any{})
	job := &storage.Job{
		Type:    jobType,
		Status:  storage.JobStatusQueued,
		Payload: payload,
	}
	event := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: "scheduled " + jobType,
	}
	created, err := hs.server.store.CreateJob(ctx, job, event)
	if err != nil {
		hs.logger.Error("create health job", zap.String("type", jobType), zap.Error(err))
		return
	}

	if hs.server.worker == nil {
		return
	}
	handler, ok := hs.server.jobHandlers[jobType]
	if !ok {
		hs.logger.Warn("no handler for health job type", zap.String("type", jobType))
		return
	}
	jobCopy := *created
	task := worker.Task{
		Name: fmt.Sprintf("%s:%s", jobType, created.ID.String()),
		Job: func(taskCtx context.Context) error {
			return handler(taskCtx, &jobCopy)
		},
	}
	if err := hs.server.worker.Enqueue(task); err != nil {
		hs.logger.Warn("enqueue health job", zap.String("type", jobType), zap.Error(err))
	}
}
