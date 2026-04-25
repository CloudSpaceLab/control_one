// Package correlation implements a sliding-window correlation engine that
// subscribes to the control-plane event bus and opens Alerts when multiple
// events matching a correlation rule happen in the same window on the same
// dimension (e.g. same node_id).
package correlation

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// AlertCreator is the narrow slice of storage.Store used by the engine. Kept
// as an interface so the engine can be tested with a fake and avoid a
// dependency on the full Store interface.
type AlertCreator interface {
	ListCorrelationRules(ctx context.Context, tenantID uuid.UUID) ([]storage.CorrelationRule, error)
	CreateAlert(ctx context.Context, p storage.CreateAlertParams) (*storage.Alert, error)
}

type windowKey struct {
	ruleID    uuid.UUID
	dimension string
}

// Engine consumes events and opens alerts when correlation rules fire.
type Engine struct {
	store     AlertCreator
	log       *zap.Logger
	bus       *eventbus.Bus
	mu        sync.Mutex
	windows   map[windowKey][]time.Time
	rulesOnce sync.Map // tenantID -> lastFetch
	cache     sync.Map // tenantID -> []storage.CorrelationRule
	cacheTTL  time.Duration
}

func New(store AlertCreator, bus *eventbus.Bus, log *zap.Logger) *Engine {
	return &Engine{
		store:    store,
		log:      log,
		bus:      bus,
		windows:  make(map[windowKey][]time.Time),
		cacheTTL: 30 * time.Second,
	}
}

// Run subscribes to the bus and processes events until ctx is cancelled.
// Tenant is discovered per-event. Pass uuid.Nil to subscribe to all tenants.
func (e *Engine) Run(ctx context.Context) {
	if e.bus == nil {
		return
	}
	sub := e.bus.Subscribe(uuid.Nil, []string{
		eventbus.TopicSecurityEvent,
		eventbus.TopicHealthIncident,
		eventbus.TopicRuleTriggered,
		eventbus.TopicComplianceFired,
		eventbus.TopicRemediationApplied,
	}, nil)
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Ch:
			if !ok {
				return
			}
			e.handle(ctx, ev)
		}
	}
}

func (e *Engine) handle(ctx context.Context, ev eventbus.Event) {
	rules := e.rulesFor(ctx, ev.TenantID)
	if len(rules) == 0 {
		return
	}
	for _, r := range rules {
		if !matchesEventType(r.EventTypes, ev.Topic) {
			continue
		}
		dim := dimensionValue(r.Dimension, ev)
		if dim == "" {
			continue
		}
		key := windowKey{ruleID: r.ID, dimension: dim}
		window := time.Duration(r.WindowSeconds) * time.Second
		if window <= 0 {
			window = 5 * time.Minute
		}
		cutoff := ev.Timestamp.Add(-window)

		e.mu.Lock()
		hits := append(e.windows[key], ev.Timestamp)
		trimmed := hits[:0]
		for _, t := range hits {
			if !t.Before(cutoff) {
				trimmed = append(trimmed, t)
			}
		}
		e.windows[key] = trimmed
		fire := len(trimmed) >= r.Threshold
		if fire {
			e.windows[key] = nil
		}
		e.mu.Unlock()

		if fire {
			e.openAlert(ctx, r, ev, dim, len(trimmed))
		}
	}
}

func (e *Engine) openAlert(ctx context.Context, r storage.CorrelationRule, ev eventbus.Event, dim string, hits int) {
	title := r.Name
	summary := "correlation rule fired"
	ctxPayload := map[string]any{
		"rule_id":   r.ID.String(),
		"dimension": r.Dimension,
		"value":     dim,
		"hits":      hits,
		"window_s":  r.WindowSeconds,
	}
	dedup := r.ID.String() + "/" + dim
	var nodeArg *uuid.UUID
	if r.Dimension == "node_id" {
		if parsed, err := uuid.Parse(dim); err == nil {
			nodeArg = &parsed
		}
	}
	_, err := e.store.CreateAlert(ctx, storage.CreateAlertParams{
		TenantID: ev.TenantID,
		NodeID:   nodeArg,
		RuleID:   &r.ID,
		Source:   "correlation",
		Severity: r.Severity,
		Title:    title,
		Summary:  summary,
		DedupKey: dedup,
		Context:  ctxPayload,
	})
	if err != nil {
		if e.log != nil {
			e.log.Warn("correlation create alert", zap.Error(err))
		}
		return
	}
	if e.bus != nil {
		payload, mErr := json.Marshal(ctxPayload)
		if mErr != nil {
			if e.log != nil {
				e.log.Warn("correlation marshal payload", zap.Error(mErr))
			}
			payload = []byte("{}")
		}
		e.bus.Publish(eventbus.Event{
			Topic:    eventbus.TopicAlertOpened,
			TenantID: ev.TenantID,
			NodeID:   nodeArg,
			Payload:  payload,
		})
	}
}

type cachedRules struct {
	at    time.Time
	rules []storage.CorrelationRule
}

func (e *Engine) rulesFor(ctx context.Context, tenantID uuid.UUID) []storage.CorrelationRule {
	if tenantID == uuid.Nil {
		return nil
	}
	if v, ok := e.cache.Load(tenantID); ok {
		cr := v.(cachedRules)
		if time.Since(cr.at) < e.cacheTTL {
			return cr.rules
		}
	}
	rules, err := e.store.ListCorrelationRules(ctx, tenantID)
	if err != nil {
		if e.log != nil {
			e.log.Debug("correlation rules fetch", zap.Error(err))
		}
		return nil
	}
	e.cache.Store(tenantID, cachedRules{at: time.Now(), rules: rules})
	return rules
}

// InvalidateCache forces the engine to refetch rules on next event.
func (e *Engine) InvalidateCache(tenantID uuid.UUID) {
	e.cache.Delete(tenantID)
}

func matchesEventType(types []string, topic string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if t == topic {
			return true
		}
	}
	return false
}

func dimensionValue(dim string, ev eventbus.Event) string {
	switch dim {
	case "", "node_id":
		if ev.NodeID != nil {
			return ev.NodeID.String()
		}
		return ""
	case "tenant_id":
		return ev.TenantID.String()
	}
	// Fallback: look inside payload for the named key.
	if len(ev.Payload) == 0 {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal(ev.Payload, &raw); err != nil {
		return ""
	}
	if v, ok := raw[dim]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
