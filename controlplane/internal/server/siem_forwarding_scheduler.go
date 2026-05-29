package server

import (
	"context"
	"errors"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/logforward"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/vault"
)

type SIEMForwardingScheduler struct {
	runner  *logforward.Runner
	logger  *zap.Logger
	stopCh  chan struct{}
	doneCh  chan struct{}
	stopMux sync.Once
}

func (s *Server) startSIEMForwardingScheduler() error {
	if s == nil || s.store == nil {
		return errors.New("SIEM forwarding scheduler missing store")
	}
	forwardingStore, ok := s.store.(logforward.Store)
	if !ok {
		return errors.New("store does not support SIEM forwarding")
	}
	resolver, err := s.siemForwardingCredentialResolver()
	if err != nil {
		return err
	}
	runner, err := logforward.NewRunner(forwardingStore, resolver, s.logger.Named("siem-forwarding"), logforward.RunnerOptions{
		Interval:                 s.cfg.SIEMForwarding.Interval,
		RunTimeout:               s.cfg.SIEMForwarding.RunTimeout,
		InitialLookback:          s.cfg.SIEMForwarding.InitialLookback,
		MaxBatchSize:             s.cfg.SIEMForwarding.MaxBatchSize,
		MaxTenantsPerPass:        s.cfg.SIEMForwarding.MaxTenantsPerPass,
		MaxDestinationsPerTenant: s.cfg.SIEMForwarding.MaxDestinationsPerTenant,
	})
	if err != nil {
		return err
	}
	s.siemForwardingScheduler = NewSIEMForwardingScheduler(runner, s.logger.Named("siem-forwarding-scheduler"))
	s.siemForwardingScheduler.Start()
	return nil
}

func (s *Server) siemForwardingCredentialResolver() (logforward.CredentialResolver, error) {
	resolvers := logforward.MultiCredentialResolver{logforward.EnvCredentialResolver{}}
	if s != nil && s.cfg != nil && strings.TrimSpace(s.cfg.Vault.Address) != "" {
		client, err := vault.NewClient(s.logger.Named("vault"), vault.Config{
			Address:    s.cfg.Vault.Address,
			Token:      s.cfg.Vault.Token,
			Timeout:    s.cfg.Vault.Timeout,
			SkipVerify: s.cfg.Vault.SkipVerify,
		})
		if err != nil {
			return nil, err
		}
		resolvers = append(resolvers, logforward.VaultCredentialResolver{Client: client})
	}
	return resolvers, nil
}

func NewSIEMForwardingScheduler(runner *logforward.Runner, logger *zap.Logger) *SIEMForwardingScheduler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &SIEMForwardingScheduler{
		runner: runner,
		logger: logger,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (s *SIEMForwardingScheduler) Start() {
	if s == nil || s.runner == nil {
		return
	}
	go s.loop()
	s.logger.Info("SIEM forwarding scheduler started")
}

func (s *SIEMForwardingScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopMux.Do(func() {
		close(s.stopCh)
		<-s.doneCh
		s.logger.Info("SIEM forwarding scheduler stopped")
	})
}

func (s *SIEMForwardingScheduler) loop() {
	defer close(s.doneCh)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runner.Run(ctx)
	}()
	select {
	case <-s.stopCh:
		cancel()
		<-done
	case <-done:
	}
}
