package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// Service wraps robfig/cron with structured logging.
type Service struct {
	runner *cron.Cron
	log    *zap.Logger
}

// New creates a scheduler with second-level precision.
func New(log *zap.Logger) *Service {
	return &Service{
		runner: cron.New(cron.WithSeconds()),
		log:    log,
	}
}

// AddInterval schedules a job to run at a fixed interval using cron's @every expression.
func (s *Service) AddInterval(name string, interval time.Duration, job func()) (cron.EntryID, error) {
	if interval <= 0 {
		return 0, fmt.Errorf("interval must be greater than zero")
	}

	spec := "@every " + interval.String()
	wrap := s.wrapJob(name, job)

	id, err := s.runner.AddFunc(spec, wrap)
	if err != nil {
		return 0, err
	}

	s.log.Info("job scheduled", zap.String("job", name), zap.String("spec", spec))
	return id, nil
}

func (s *Service) wrapJob(name string, job func()) func() {
	var running atomic.Bool
	return func() {
		if !running.CompareAndSwap(false, true) {
			s.log.Warn("job skipped: previous run still active", zap.String("job", name))
			return
		}
		defer running.Store(false)
		s.log.Debug("job start", zap.String("job", name))
		start := time.Now()
		job()
		s.log.Debug("job done", zap.String("job", name), zap.Duration("duration", time.Since(start)))
	}
}

// Start asynchronously starts the scheduler.
func (s *Service) Start() {
	s.runner.Start()
}

// Stop stops the scheduler and waits for running jobs to finish.
func (s *Service) Stop(ctx context.Context) {
	done := s.runner.Stop().Done()
	select {
	case <-done:
	case <-ctx.Done():
		return
	}
}
