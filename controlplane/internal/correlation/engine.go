// Package correlation implements a sliding-window correlation engine that
// subscribes to the control-plane event bus and opens Alerts when multiple
// events matching a correlation rule happen in the same window on the same
// dimension (e.g. same node_id).
package correlation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

type alertOccurrenceUpdater interface {
	UpdateOpenAlertOccurrence(ctx context.Context, p storage.CreateAlertParams) (*storage.Alert, error)
}

type windowKey struct {
	ruleID    uuid.UUID
	dimension string
}

type windowHit struct {
	timestamp      time.Time
	distinctValue  string
	aggregateValue float64
}

// Engine consumes events and opens alerts when correlation rules fire.
type Engine struct {
	store           AlertCreator
	log             *zap.Logger
	bus             *eventbus.Bus
	mu              sync.Mutex
	windows         map[windowKey][]windowHit
	sequenceWindows map[windowKey][]time.Time
	lastFired       map[windowKey]time.Time
	cache           sync.Map // tenantID -> []storage.CorrelationRule
	cacheTTL        time.Duration
}

func New(store AlertCreator, bus *eventbus.Bus, log *zap.Logger) *Engine {
	return &Engine{
		store:           store,
		log:             log,
		bus:             bus,
		windows:         make(map[windowKey][]windowHit),
		sequenceWindows: make(map[windowKey][]time.Time),
		lastFired:       make(map[windowKey]time.Time),
		cacheTTL:        30 * time.Second,
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
		eventbus.TopicEventsAnomaly,
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
		if !r.Enabled {
			continue
		}
		if !matchesEventType(r.EventTypes, ev.Topic) {
			continue
		}
		dim := compoundDimensionValue(r.GroupBy, r.Dimension, ev)
		if dim == "" {
			continue
		}
		key := windowKey{ruleID: r.ID, dimension: dim}
		window := time.Duration(r.WindowSeconds) * time.Second
		if window <= 0 {
			window = 5 * time.Minute
		}
		cutoff := ev.Timestamp.Add(-window)
		if r.SequenceEventType != "" && matchesPayloadEventType(r.SequenceEventType, ev.Payload) && matchesConditions(r.SequenceConditions, ev) {
			e.mu.Lock()
			sequenceHits := append(e.sequenceWindows[key], ev.Timestamp)
			e.sequenceWindows[key] = trimTimes(sequenceHits, cutoff)
			e.mu.Unlock()
		}
		if !matchesPayloadEventType(r.EventType, ev.Payload) || !matchesConditions(r.Conditions, ev) || !matchesConditionGroups(r.ConditionGroups, ev) {
			continue
		}
		distinctValue := ""
		if r.DistinctField != "" {
			value, ok := eventFieldValue(r.DistinctField, ev)
			if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
				continue
			}
			distinctValue = strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
		}
		aggregateValue := float64(0)
		if r.AggregateField != "" {
			value, ok := eventFieldValue(r.AggregateField, ev)
			if !ok {
				continue
			}
			parsed, err := strconv.ParseFloat(fmt.Sprint(value), 64)
			if err != nil || parsed < 0 {
				continue
			}
			aggregateValue = parsed
		}

		e.mu.Lock()
		if r.SequenceEventType != "" {
			e.sequenceWindows[key] = trimTimes(e.sequenceWindows[key], cutoff)
			precursorCount := 0
			for _, timestamp := range e.sequenceWindows[key] {
				// A prerequisite must precede the target. This also prevents an
				// event that matches both sides of a sequence from satisfying its
				// own prerequisite.
				if timestamp.Before(ev.Timestamp) {
					precursorCount++
				}
			}
			if precursorCount < r.SequenceThreshold {
				e.mu.Unlock()
				continue
			}
		}
		hits := append(e.windows[key], windowHit{timestamp: ev.Timestamp, distinctValue: distinctValue, aggregateValue: aggregateValue})
		trimmed := hits[:0]
		for _, hit := range hits {
			if !hit.timestamp.Before(cutoff) {
				trimmed = append(trimmed, hit)
			}
		}
		e.windows[key] = trimmed
		hitCount := len(trimmed)
		if r.DistinctField != "" {
			unique := make(map[string]struct{}, len(trimmed))
			for _, hit := range trimmed {
				unique[hit.distinctValue] = struct{}{}
			}
			hitCount = len(unique)
		}
		aggregateTotal := float64(0)
		fire := hitCount >= r.Threshold
		if r.AggregateField != "" {
			for _, hit := range trimmed {
				aggregateTotal += hit.aggregateValue
			}
			fire = aggregateTotal >= float64(r.AggregateThreshold)
		}
		suppressed := false
		if fire && r.SuppressionSeconds > 0 {
			last := e.lastFired[key]
			suppressed = !last.IsZero() && ev.Timestamp.Sub(last) < time.Duration(r.SuppressionSeconds)*time.Second
			fire = !suppressed
		}
		if fire {
			e.windows[key] = nil
			e.sequenceWindows[key] = nil
			e.lastFired[key] = ev.Timestamp
		} else if suppressed {
			e.windows[key] = nil
			e.sequenceWindows[key] = nil
		}
		e.mu.Unlock()

		if fire {
			e.openAlert(ctx, r, ev, dim, hitCount, aggregateTotal, false)
		} else if suppressed {
			e.openAlert(ctx, r, ev, dim, hitCount, aggregateTotal, true)
		}
	}
}

func (e *Engine) openAlert(ctx context.Context, r storage.CorrelationRule, ev eventbus.Event, dim string, hits int, aggregateValue float64, updateOnly bool) {
	title := r.Name
	summary := "correlation rule fired"
	ctxPayload := map[string]any{
		"rule_id":             r.ID.String(),
		"dimension":           r.Dimension,
		"value":               dim,
		"hits":                hits,
		"window_s":            r.WindowSeconds,
		"event_type_filter":   r.EventType,
		"group_by":            r.GroupBy,
		"suppression_s":       r.SuppressionSeconds,
		"conditions":          r.Conditions,
		"condition_groups":    r.ConditionGroups,
		"distinct_field":      r.DistinctField,
		"sequence_event_type": r.SequenceEventType,
		"sequence_threshold":  r.SequenceThreshold,
		"sequence_conditions": r.SequenceConditions,
		"aggregate_field":     r.AggregateField,
		"aggregate_threshold": r.AggregateThreshold,
		"evidence_links":      map[string]any{"alert_inbox": "/console/alerts", "investigation": "/console/investigate"},
	}
	if r.AggregateField != "" {
		ctxPayload["aggregate_value"] = aggregateValue
	}
	for key, value := range eventContext(ev) {
		ctxPayload[key] = value
	}
	dedup := r.ID.String() + "/" + dim
	var nodeArg *uuid.UUID
	if (len(r.GroupBy) == 1 && r.GroupBy[0] == "node_id") || (len(r.GroupBy) == 0 && r.Dimension == "node_id") {
		if parsed, err := uuid.Parse(dim); err == nil {
			nodeArg = &parsed
		}
	}
	params := storage.CreateAlertParams{
		TenantID: ev.TenantID,
		NodeID:   nodeArg,
		RuleID:   &r.ID,
		Source:   "correlation",
		Severity: r.Severity,
		Title:    title,
		Summary:  summary,
		DedupKey: dedup,
		Context:  ctxPayload,
	}
	var err error
	if updateOnly {
		updater, ok := e.store.(alertOccurrenceUpdater)
		if !ok {
			return
		}
		_, err = updater.UpdateOpenAlertOccurrence(ctx, params)
	} else {
		_, err = e.store.CreateAlert(ctx, params)
	}
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

func trimTimes(times []time.Time, cutoff time.Time) []time.Time {
	trimmed := times[:0]
	for _, timestamp := range times {
		if !timestamp.Before(cutoff) {
			trimmed = append(trimmed, timestamp)
		}
	}
	return trimmed
}

func matchesConditions(conditions []storage.CorrelationCondition, ev eventbus.Event) bool {
	for _, condition := range conditions {
		got, ok := eventFieldValue(condition.Field, ev)
		if !ok || !compareCondition(got, condition.Operator, condition.Value) {
			return false
		}
	}
	return true
}

func matchesConditionGroups(groups [][]storage.CorrelationCondition, ev eventbus.Event) bool {
	if len(groups) == 0 {
		return true
	}
	for _, group := range groups {
		if matchesConditions(group, ev) {
			return true
		}
	}
	return false
}

func eventFieldValue(field string, ev eventbus.Event) (any, bool) {
	switch field {
	case "node_id":
		if ev.NodeID != nil {
			return ev.NodeID.String(), true
		}
	case "tenant_id":
		if ev.TenantID != uuid.Nil {
			return ev.TenantID.String(), true
		}
	}
	if len(ev.Payload) == 0 {
		return nil, false
	}
	var raw map[string]any
	if err := json.Unmarshal(ev.Payload, &raw); err != nil {
		return nil, false
	}
	if value, ok := raw[field]; ok {
		return value, true
	}
	if details, ok := raw["details"].(map[string]any); ok {
		value, found := details[field]
		return value, found
	}
	return nil, false
}

func compareCondition(got any, operator string, want any) bool {
	gotText := fmt.Sprint(got)
	wantText := fmt.Sprint(want)
	switch operator {
	case "eq":
		return strings.EqualFold(gotText, wantText)
	case "neq":
		return !strings.EqualFold(gotText, wantText)
	case "contains":
		return strings.Contains(strings.ToLower(gotText), strings.ToLower(wantText))
	}
	gotNumber, gotErr := strconv.ParseFloat(gotText, 64)
	wantNumber, wantErr := strconv.ParseFloat(wantText, 64)
	if gotErr != nil || wantErr != nil {
		return false
	}
	switch operator {
	case "gt":
		return gotNumber > wantNumber
	case "gte":
		return gotNumber >= wantNumber
	case "lt":
		return gotNumber < wantNumber
	case "lte":
		return gotNumber <= wantNumber
	}
	return false
}

func matchesPayloadEventType(want string, payload []byte) bool {
	if want == "" {
		return true
	}
	if len(payload) == 0 {
		return false
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return false
	}
	for _, key := range []string{"event_type", "type"} {
		if got, ok := raw[key].(string); ok && got == want {
			return true
		}
	}
	if details, ok := raw["details"].(map[string]any); ok {
		if got, ok := details["event_type"].(string); ok && got == want {
			return true
		}
	}
	return false
}

func compoundDimensionValue(groupBy []string, legacy string, ev eventbus.Event) string {
	if len(groupBy) == 0 {
		groupBy = []string{legacy}
	}
	if len(groupBy) == 1 {
		return dimensionValue(groupBy[0], ev)
	}
	parts := make([]string, 0, len(groupBy))
	for _, field := range groupBy {
		value := dimensionValue(field, ev)
		if value == "" {
			return ""
		}
		parts = append(parts, field+"="+value)
	}
	return strings.Join(parts, "|")
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
	if details, ok := raw["details"].(map[string]any); ok {
		if v, ok := details[dim]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
