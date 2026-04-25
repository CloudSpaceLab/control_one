package correlation

import (
	"context"
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
		EventTypes: []string{eventbus.TopicSecurityEvent},
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
		EventTypes: []string{eventbus.TopicSecurityEvent},
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
		EventTypes: []string{eventbus.TopicSecurityEvent},
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
