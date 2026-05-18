package server

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

type IPBlockExpiryScheduler struct {
	cron   *cron.Cron
	server *Server
	logger *zap.Logger
}

func NewIPBlockExpiryScheduler(s *Server) *IPBlockExpiryScheduler {
	return &IPBlockExpiryScheduler{
		cron:   cron.New(),
		server: s,
		logger: s.logger.Named("ip-block-expiry"),
	}
}

func (bs *IPBlockExpiryScheduler) Start(schedule string) error {
	if schedule == "" {
		schedule = "* * * * *"
	}
	_, err := bs.cron.AddFunc(schedule, bs.runOnce)
	if err != nil {
		return fmt.Errorf("register ip block expiry schedule %q: %w", schedule, err)
	}
	bs.cron.Start()
	bs.logger.Info("ip block expiry scheduler started", zap.String("schedule", schedule))
	go bs.runOnce()
	return nil
}

func (bs *IPBlockExpiryScheduler) Stop() {
	ctx := bs.cron.Stop()
	<-ctx.Done()
	bs.logger.Info("ip block expiry scheduler stopped")
}

func (bs *IPBlockExpiryScheduler) runOnce() {
	if bs.server == nil || bs.server.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := bs.server.expireIPBlocklistEntries(ctx, time.Now().UTC(), 200)
	if err != nil {
		bs.logger.Warn("expire ip blocklist entries", zap.Error(err))
		return
	}
	if n > 0 {
		bs.logger.Info("ip block expiry sweep complete", zap.Int("expired", n))
	}
}
