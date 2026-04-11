package hooks

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/config"
)

var (
	errHooksDisabled  = errors.New("hooks disabled")
	errQueueSaturated = errors.New("hooks queue saturated")
	errUnknownMode    = errors.New("unknown subscription mode")
	errUnknownHandler = errors.New("unknown handler type")
	errDuplicateSub   = errors.New("subscription already exists")
)

// Service coordinates event publications, subscription management, and job queueing.
type Service struct {
	log   *zap.Logger
	cfg   config.HooksConfig
	mu    sync.RWMutex
	subs  map[string]*Subscription
	queue chan *ScriptRun
}

// NewService constructs a hook service using the provided configuration.
func NewService(log *zap.Logger, cfg config.HooksConfig) *Service {
	s := &Service{
		log:   log,
		cfg:   cfg,
		subs:  make(map[string]*Subscription),
		queue: make(chan *ScriptRun, cfg.MaxQueueSize),
	}

	if !cfg.Enabled {
		log.Info("hooks disabled via configuration")
		return s
	}

	for _, raw := range cfg.BootstrapSubscriptions {
		sub, err := convertBootstrap(raw)
		if err != nil {
			log.Warn("skip bootstrap subscription", zap.Error(err))
			continue
		}
		if err := s.RegisterSubscription(sub); err != nil {
			log.Warn("bootstrap subscription registration failed", zap.String("id", sub.ID), zap.Error(err))
		}
	}

	return s
}

// RegisterSubscription registers a new subscription for event processing.
func (s *Service) RegisterSubscription(sub *Subscription) error {
	if !s.cfg.Enabled {
		return errHooksDisabled
	}
	if sub == nil {
		return errors.New("subscription cannot be nil")
	}
	if sub.ID == "" {
		sub.ID = uuid.NewString()
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	if sub.LastModifiedAt.IsZero() {
		sub.LastModifiedAt = sub.CreatedAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.subs[sub.ID]; exists {
		return errDuplicateSub
	}
	s.subs[sub.ID] = sub
	return nil
}

// PublishEvent evaluates subscriptions and enqueues matching script runs.
func (s *Service) PublishEvent(ctx context.Context, evt *Event) error {
	if !s.cfg.Enabled {
		return errHooksDisabled
	}
	if evt == nil {
		return errors.New("event cannot be nil")
	}
	now := time.Now().UTC()
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = now
	}
	evt.ReceivedAt = now

	subs := s.snapshotSubscriptions()
	for _, sub := range subs {
		if !sub.Matches(evt) {
			continue
		}
		run := s.createRunFromSubscription(sub, evt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case s.queue <- run:
		default:
			s.log.Warn("hook queue saturated", zap.String("subscription_id", sub.ID), zap.String("event_id", evt.ID))
			return errQueueSaturated
		}
	}

	return nil
}

// NextRun blocks until a script run is available or context is cancelled.
func (s *Service) NextRun(ctx context.Context) (*ScriptRun, error) {
	if !s.cfg.Enabled {
		return nil, errHooksDisabled
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case run := <-s.queue:
		return run, nil
	}
}

// SnapshotQueueLength returns the number of queued script runs.
func (s *Service) SnapshotQueueLength() int {
	return len(s.queue)
}

// Subscriptions returns a copy of registered subscriptions.
func (s *Service) Subscriptions() []*Subscription {
	return s.snapshotSubscriptions()
}

func (s *Service) snapshotSubscriptions() []*Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		list = append(list, sub)
	}
	return list
}

func (s *Service) createRunFromSubscription(sub *Subscription, evt *Event) *ScriptRun {
	run := &ScriptRun{
		RunID:          uuid.NewString(),
		SubscriptionID: sub.ID,
		EventID:        evt.ID,
		TenantID:       sub.TenantID,
		NodeID:         evt.Subject,
		Status:         ScriptRunStatusQueued,
		Mode:           sub.Mode,
		Priority:       100,
		QueuedAt:       time.Now().UTC(),
		RunPolicy:      sub.RunPolicy,
		Metadata:       map[string]any{"auto": sub.Mode == ModeAuto},
	}
	return run
}

func convertBootstrap(raw config.HookSubscriptionConfig) (*Subscription, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(raw.Mode)))
	if mode == "" {
		mode = ModeAuto
	}
	if mode != ModeAuto && mode != ModeManual {
		return nil, errUnknownMode
	}

	handlerType := HandlerType(strings.ToLower(strings.TrimSpace(raw.Handler.Type)))
	switch handlerType {
	case HandlerTypeWASM, HandlerTypeBash, HandlerTypeLua, HandlerTypeWebhook:
	default:
		return nil, errUnknownHandler
	}

	sub := &Subscription{
		ID:       raw.ID,
		TenantID: "tenant-default",
		EventID:  raw.EventID,
		Filter:   raw.Filter,
		Mode:     mode,
		Handler: Handler{
			Type:     handlerType,
			Language: raw.Handler.Language,
			Inline:   raw.Handler.Inline,
			Source:   raw.Handler.Source,
		},
		RunPolicy: RunPolicy{
			Timeout:     raw.RunPolicy.Timeout,
			MemoryMB:    raw.RunPolicy.MemoryMB,
			Concurrency: raw.RunPolicy.Concurrency,
			MaxRetries:  raw.RunPolicy.MaxRetries,
		},
		Remediate: raw.RemediateAllowed,
		RBACRoles: raw.RBACRoles,
		CreatedAt: time.Now().UTC(),
	}

	return sub, nil
}
