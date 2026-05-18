// Package correlation implements a sliding-window correlation engine that
// subscribes to the control-plane event bus and opens Alerts when multiple
// events matching a correlation rule happen in the same window on the same
// dimension (e.g. same node_id).
package correlation

import (
	"context"
	"encoding/json"
	"fmt"
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
	store    AlertCreator
	log      *zap.Logger
	bus      *eventbus.Bus
	mu       sync.Mutex
	windows  map[windowKey][]time.Time
	cache    sync.Map // tenantID -> []storage.CorrelationRule
	cacheTTL time.Duration
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
	for key, value := range eventContext(ev) {
		ctxPayload[key] = value
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

func eventContext(ev eventbus.Event) map[string]any {
	out := map[string]any{
		"event_topic": ev.Topic,
	}
	if ev.Timestamp.IsZero() {
		out["event_timestamp"] = time.Now().UTC().Format(time.RFC3339)
	} else {
		out["event_timestamp"] = ev.Timestamp.UTC().Format(time.RFC3339)
	}
	if len(ev.Payload) == 0 {
		return out
	}
	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		out["event_payload_error"] = err.Error()
		return out
	}
	copyStringKeys(out, payload, map[string]string{
		"type":           "event_type",
		"message":        "event_message",
		"severity":       "event_severity",
		"correlation_id": "correlation_id",
		"dedup_key":      "event_dedup_key",
		"src_ip":         "src_ip",
		"dst_ip":         "dst_ip",
		"process_name":   "process_name",
		"user_name":      "user_name",
		"protocol":       "protocol",
	})
	copyNumberKeys(out, payload, map[string]string{
		"src_port":     "src_port",
		"dst_port":     "dst_port",
		"bytes_in":     "bytes_in",
		"bytes_out":    "bytes_out",
		"duration_ms":  "duration_ms",
		"threat_score": "threat_score",
	})
	details, _ := payload["details"].(map[string]any)
	copyStringKeys(out, details, map[string]string{
		"parser_profile":       "parser_profile",
		"source_file":          "source_file",
		"program":              "program",
		"collector_type":       "collector_type",
		"app":                  "app",
		"vhost":                "vhost",
		"server_group":         "server_group",
		"webserver_kind":       "webserver_kind",
		"country_code":         "country_code",
		"country":              "country",
		"asn":                  "asn",
		"application_type":     "application_type",
		"application_name":     "application_name",
		"application_category": "application_category",
		"application_root":     "application_root",
		"coverage_state":       "coverage_state",
		"request_id":           "request_id",
		"traceparent":          "traceparent",
	})
	copyNumberKeys(out, details, map[string]string{
		"score":       "score",
		"status_code": "status_code",
	})
	for _, key := range []string{"reasons", "status_counts", "top_paths", "evidence_refs", "host_correlation", "baselines"} {
		if value, ok := details[key]; ok {
			out[key] = value
		}
	}
	return out
}

func copyStringKeys(dst map[string]any, src map[string]any, keys map[string]string) {
	for from, to := range keys {
		if value, ok := stringFromAny(src[from]); ok {
			dst[to] = value
		}
	}
}

func copyNumberKeys(dst map[string]any, src map[string]any, keys map[string]string) {
	for from, to := range keys {
		if value, ok := src[from]; ok {
			switch n := value.(type) {
			case float64, float32, int, int64, int32, json.Number:
				dst[to] = n
			}
		}
	}
}

func stringFromAny(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		if v != "" {
			return v, true
		}
	case fmt.Stringer:
		if s := v.String(); s != "" {
			return s, true
		}
	}
	return "", false
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
