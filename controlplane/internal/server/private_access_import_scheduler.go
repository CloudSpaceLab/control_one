package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
)

type PrivateAccessImportScheduler struct {
	server  *Server
	logger  *zap.Logger
	stopCh  chan struct{}
	doneCh  chan struct{}
	stopMux sync.Once
}

func NewPrivateAccessImportScheduler(s *Server) *PrivateAccessImportScheduler {
	logger := zap.NewNop()
	if s != nil && s.logger != nil {
		logger = s.logger.Named("private-access-import-scheduler")
	}
	return &PrivateAccessImportScheduler{
		server: s,
		logger: logger,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (ps *PrivateAccessImportScheduler) Start(interval time.Duration, limit int) error {
	if ps == nil || ps.server == nil {
		return errors.New("private access import scheduler missing server")
	}
	if interval <= 0 {
		return errors.New("private access import scheduler interval must be positive")
	}
	if limit <= 0 {
		limit = 50
	}
	go ps.loop(interval, limit)
	ps.logger.Info("private access import scheduler started", zap.Duration("interval", interval), zap.Int("limit", limit))
	return nil
}

func (ps *PrivateAccessImportScheduler) Stop() {
	if ps == nil {
		return
	}
	ps.stopMux.Do(func() {
		close(ps.stopCh)
		<-ps.doneCh
		ps.logger.Info("private access import scheduler stopped")
	})
}

func (ps *PrivateAccessImportScheduler) loop(interval time.Duration, limit int) {
	defer close(ps.doneCh)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	ps.runOnce(limit)
	for {
		select {
		case <-ticker.C:
			ps.runOnce(limit)
		case <-ps.stopCh:
			return
		}
	}
}

func (ps *PrivateAccessImportScheduler) runOnce(limit int) {
	if ps == nil || ps.server == nil || ps.server.store == nil {
		return
	}
	store, ok := ps.server.store.(privateAccessProviderAccountStore)
	if !ok {
		ps.logger.Warn("private access store does not support provider account imports")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	accounts, err := store.ListDuePrivateAccessProviderAccounts(ctx, time.Now().UTC(), limit)
	if err != nil {
		ps.logger.Warn("list due private access provider accounts", zap.Error(err))
		return
	}
	for i := range accounts {
		account := accounts[i]
		if _, _, err := ps.server.enqueuePrivateAccessImportJob(ctx, store, &account); err != nil {
			ps.logger.Warn("enqueue private access provider import",
				zap.String("tenant_id", account.TenantID.String()),
				zap.String("provider_account_id", account.ID.String()),
				zap.String("provider", string(account.Provider)),
				zap.Error(err),
			)
		}
	}
	if len(accounts) > 0 {
		ps.logger.Info("private access import scheduler sweep complete", zap.Int("queued", len(accounts)))
	}
}
