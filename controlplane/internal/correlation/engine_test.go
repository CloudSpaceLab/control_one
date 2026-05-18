package correlation

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type fakeStore struct {
	mu     sync.Mutex
	rules  []storage.CorrelationRule
	alerts []storage.CreateAlertParams
}

func (f *fakeStore) ListCorrelationRules(_ context.Context, _ uuid.UUID) ([]storage.CorrelationRule, error) {
	return f.rules, nil
}

func (f *fakeStore) CreateAlert(_ context.Context, p storage.CreateAlertParams) (*storage.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, p)
	return &storage.Alert{ID: uuid.New(), TenantID: p.TenantID, Severity: p.Severity, Title: p.Title}, nil
}

func TestEngineFiresAtThreshold(t *testing.T) {
	tenant := uuid.New()
	node := uuid.New()
	rule := storage.CorrelationRule{
		ID: uuid.New(), TenantID: tenant, Name: "3 sec events on same node",
		EventTypes:    []string{eventbus.TopicSecurityEvent},
		WindowSeconds: 300, Threshold: 3, Dimension: "node_id", Severity: "high", Enabled: true,
	}
	store := &fakeStore{rules: []storage.CorrelationRule{rule}}
	eng := New(store, eventbus.New(16), nil)

	now := time.Now()
	for i := 0; i < 3; i++ {
		eng.handle(context.Background(), eventbus.Event{
			Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.alerts) != 1 {
		t.Fatalf("want 1 alert, got %d", len(store.alerts))
	}
	if store.alerts[0].DedupKey != rule.ID.String()+"/"+node.String() {
		t.Fatalf("unexpected dedup key %s", store.alerts[0].DedupKey)
	}
}

func TestEngineWindowExpiresBeforeFiring(t *testing.T) {
	tenant := uuid.New()
	node := uuid.New()
	rule := storage.CorrelationRule{
		ID: uuid.New(), TenantID: tenant, Name: "window",
		EventTypes:    []string{eventbus.TopicSecurityEvent},
		WindowSeconds: 5, Threshold: 2, Dimension: "node_id", Severity: "high", Enabled: true,
	}
	store := &fakeStore{rules: []storage.CorrelationRule{rule}}
	eng := New(store, eventbus.New(16), nil)

	base := time.Now()
	eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: base})
	eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: base.Add(10 * time.Second)})

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.alerts) != 0 {
		t.Fatalf("expected no alerts (outside window), got %d", len(store.alerts))
	}
}

func TestEngineFiltersByEventType(t *testing.T) {
	tenant := uuid.New()
	node := uuid.New()
	rule := storage.CorrelationRule{
		ID: uuid.New(), TenantID: tenant, Name: "only security",
		EventTypes:    []string{eventbus.TopicSecurityEvent},
		WindowSeconds: 60, Threshold: 1, Dimension: "node_id", Severity: "high", Enabled: true,
	}
	store := &fakeStore{rules: []storage.CorrelationRule{rule}}
	eng := New(store, eventbus.New(16), nil)

	eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicHealthIncident, TenantID: tenant, NodeID: &node, Timestamp: time.Now()})
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.alerts) != 0 {
		t.Fatal("health event should not match security-only rule")
	}
}

func TestEngineCopiesEventEvidenceIntoAlertContext(t *testing.T) {
	tenant := uuid.New()
	node := uuid.New()
	rule := storage.CorrelationRule{
		ID: uuid.New(), TenantID: tenant, Name: "ip behavior finding",
		EventTypes:    []string{"events.anomaly"},
		WindowSeconds: 60, Threshold: 1, Dimension: "node_id", Severity: "high", Enabled: true,
	}
	store := &fakeStore{rules: []storage.CorrelationRule{rule}}
	eng := New(store, eventbus.New(16), nil)

	payload, err := json.Marshal(map[string]any{
		"type":           "anomaly.ip_behavior",
		"message":        "credential stuffing behavior from 203.0.113.10 scored 82",
		"severity":       "high",
		"src_ip":         "203.0.113.10",
		"correlation_id": "corr-1",
		"details": map[string]any{
			"parser_profile": "temenos-t24",
			"source_file":    "/opt/temenos/logs/access.log",
			"app":            "Core Banking API",
			"server_group":   "Core Banking",
			"country_code":   "NG",
			"status_counts":  map[string]any{"401": 42},
			"evidence_refs": []map[string]any{{
				"type":           "web.request",
				"parser_profile": "temenos-t24",
				"source_file":    "/opt/temenos/logs/access.log",
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	eng.handle(context.Background(), eventbus.Event{
		Topic: "events.anomaly", TenantID: tenant, NodeID: &node, Timestamp: time.Now(), Payload: payload,
	})

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.alerts) != 1 {
		t.Fatalf("want 1 alert, got %d", len(store.alerts))
	}
	ctx := store.alerts[0].Context
	for key, want := range map[string]string{
		"event_type":     "anomaly.ip_behavior",
		"event_message":  "credential stuffing behavior from 203.0.113.10 scored 82",
		"src_ip":         "203.0.113.10",
		"parser_profile": "temenos-t24",
		"source_file":    "/opt/temenos/logs/access.log",
		"app":            "Core Banking API",
		"server_group":   "Core Banking",
		"country_code":   "NG",
	} {
		if got, _ := ctx[key].(string); got != want {
			t.Fatalf("context[%s] = %#v, want %q", key, ctx[key], want)
		}
	}
	if _, ok := ctx["status_counts"].(map[string]any); !ok {
		t.Fatalf("status_counts missing from context: %#v", ctx["status_counts"])
	}
	if refs, ok := ctx["evidence_refs"].([]any); !ok || len(refs) != 1 {
		t.Fatalf("evidence_refs missing from context: %#v", ctx["evidence_refs"])
	}
}
