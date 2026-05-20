package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// EventIngestReplayScheduler drains durable event ingest journal rows that did
// not complete live fan-out, typically because the process restarted or Doris
// briefly rejected a Stream Load enqueue.
type EventIngestReplayScheduler struct {
	server  *Server
	logger  *zap.Logger
	stopCh  chan struct{}
	doneCh  chan struct{}
	stopMux sync.Once
}

func NewEventIngestReplayScheduler(s *Server) *EventIngestReplayScheduler {
	logger := zap.NewNop()
	if s != nil && s.logger != nil {
		logger = s.logger.Named("event-ingest-replay")
	}
	return &EventIngestReplayScheduler{
		server: s,
		logger: logger,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (rs *EventIngestReplayScheduler) Start(interval, olderThan time.Duration, limit int) error {
	if rs == nil || rs.server == nil {
		return errors.New("event ingest replay scheduler missing server")
	}
	if interval <= 0 {
		return fmt.Errorf("invalid event ingest replay interval %s", interval)
	}
	if olderThan < 0 {
		return fmt.Errorf("invalid event ingest replay olderThan %s", olderThan)
	}
	if limit <= 0 {
		limit = 100
	}
	go rs.loop(interval, olderThan, limit)
	rs.logger.Info("event ingest replay scheduler started",
		zap.Duration("interval", interval),
		zap.Duration("older_than", olderThan),
		zap.Int("limit", limit),
	)
	go rs.runOnce(olderThan, limit)
	return nil
}

func (rs *EventIngestReplayScheduler) Stop() {
	if rs == nil {
		return
	}
	rs.stopMux.Do(func() {
		close(rs.stopCh)
		<-rs.doneCh
		rs.logger.Info("event ingest replay scheduler stopped")
	})
}

func (rs *EventIngestReplayScheduler) loop(interval, olderThan time.Duration, limit int) {
	defer close(rs.doneCh)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rs.runOnce(olderThan, limit)
		case <-rs.stopCh:
			return
		}
	}
}

func (rs *EventIngestReplayScheduler) runOnce(olderThan time.Duration, limit int) {
	if rs == nil || rs.server == nil || rs.server.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	drained, failed, err := rs.server.drainPendingEventIngestBatches(ctx, olderThan, limit)
	if err != nil {
		rs.logger.Warn("event ingest replay sweep incomplete",
			zap.Int("drained", drained),
			zap.Int("failed", failed),
			zap.Error(err),
		)
		return
	}
	if drained > 0 || failed > 0 {
		rs.logger.Info("event ingest replay sweep complete",
			zap.Int("drained", drained),
			zap.Int("failed", failed),
		)
	}
}
