package server

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

// webhookEventStore extends fakeStore with webhook event tracking.
type webhookEventStore struct {
	fakeStore
	mu         sync.Mutex
	webhooks   []storage.Webhook
	deliveries []storage.WebhookDelivery
}

func (w *webhookEventStore) ListWebhooksByEvent(_ context.Context, tenantID uuid.UUID, eventType string) ([]storage.Webhook, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var matched []storage.Webhook
	for _, wh := range w.webhooks {
		if !wh.Enabled {
			continue
		}
		if wh.TenantID.Valid && wh.TenantID.UUID != tenantID {
			continue
		}
		for _, evt := range wh.Events {
			if evt == eventType {
				matched = append(matched, wh)
				break
			}
		}
	}
	return matched, nil
}

func (w *webhookEventStore) RecordWebhookDelivery(_ context.Context, delivery storage.WebhookDelivery) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deliveries = append(w.deliveries, delivery)
	return nil
}

func TestEmitComplianceEvents_NoFailures(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	webhookID := uuid.New()

	store := &webhookEventStore{
		webhooks: []storage.Webhook{
			{
				ID:             webhookID,
				TenantID:       uuid.NullUUID{UUID: tenantID, Valid: true},
				Name:           "test-hook",
				URL:            "http://localhost:9999/hook", // unreachable, but we still test delivery attempt recording
				Events:         []string{EventComplianceCompleted},
				Enabled:        true,
				VerifySSL:      false,
				TimeoutSeconds: 2,
				RetryCount:     0,
				Headers:        map[string]any{},
				Metadata:       map[string]any{},
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
	}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, &stubQueue{})

	results := []compliance.Result{
		{RuleID: "rule-1", Passed: true, Severity: "low", CheckedAt: time.Now()},
		{RuleID: "rule-2", Passed: true, Severity: "medium", CheckedAt: time.Now()},
	}

	srv.emitComplianceEvents(context.Background(), tenantID, nodeID, results, "scan-001")

	store.mu.Lock()
	defer store.mu.Unlock()

	// Should have attempted delivery for "completed" event only (no failure events).
	if len(store.deliveries) != 1 {
		t.Fatalf("expected 1 delivery attempt, got %d", len(store.deliveries))
	}
	if store.deliveries[0].EventType != EventComplianceCompleted {
		t.Fatalf("expected event type %s, got %s", EventComplianceCompleted, store.deliveries[0].EventType)
	}
}

func TestEmitComplianceEvents_WithHighFailure(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	webhookID := uuid.New()

	store := &webhookEventStore{
		webhooks: []storage.Webhook{
			{
				ID:             webhookID,
				TenantID:       uuid.NullUUID{UUID: tenantID, Valid: true},
				Name:           "all-events-hook",
				URL:            "http://localhost:9999/hook",
				Events:         []string{EventComplianceCompleted, EventComplianceFailure, EventComplianceHighFail, EventComplianceCritical},
				Enabled:        true,
				VerifySSL:      false,
				TimeoutSeconds: 2,
				RetryCount:     0,
				Headers:        map[string]any{},
				Metadata:       map[string]any{},
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
	}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, &stubQueue{})

	results := []compliance.Result{
		{RuleID: "rule-1", Passed: true, Severity: "low", CheckedAt: time.Now()},
		{RuleID: "rule-2", Passed: false, Severity: "high", CheckedAt: time.Now()},
		{RuleID: "rule-3", Passed: false, Severity: "medium", CheckedAt: time.Now()},
	}

	srv.emitComplianceEvents(context.Background(), tenantID, nodeID, results, "scan-002")

	store.mu.Lock()
	defer store.mu.Unlock()

	// Should emit: completed + failure + high_fail = 3 events (no critical since max is high)
	if len(store.deliveries) != 3 {
		t.Fatalf("expected 3 delivery attempts, got %d", len(store.deliveries))
	}

	eventTypes := make(map[string]bool)
	for _, d := range store.deliveries {
		eventTypes[d.EventType] = true
	}
	for _, expected := range []string{EventComplianceCompleted, EventComplianceFailure, EventComplianceHighFail} {
		if !eventTypes[expected] {
			t.Errorf("expected event type %s to be delivered", expected)
		}
	}
	if eventTypes[EventComplianceCritical] {
		t.Errorf("did not expect critical event to be delivered")
	}
}

func TestEmitComplianceEvents_CriticalSeverity(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	webhookID := uuid.New()

	store := &webhookEventStore{
		webhooks: []storage.Webhook{
			{
				ID:             webhookID,
				TenantID:       uuid.NullUUID{UUID: tenantID, Valid: true},
				Name:           "critical-hook",
				URL:            "http://localhost:9999/hook",
				Events:         []string{EventComplianceCritical},
				Enabled:        true,
				VerifySSL:      false,
				TimeoutSeconds: 2,
				RetryCount:     0,
				Headers:        map[string]any{},
				Metadata:       map[string]any{},
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
	}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, &stubQueue{})

	results := []compliance.Result{
		{RuleID: "rule-1", Passed: false, Severity: "critical", CheckedAt: time.Now()},
	}

	srv.emitComplianceEvents(context.Background(), tenantID, nodeID, results, "scan-003")

	store.mu.Lock()
	defer store.mu.Unlock()

	// Only subscribed to critical, so should get 1 delivery
	if len(store.deliveries) != 1 {
		t.Fatalf("expected 1 delivery attempt, got %d", len(store.deliveries))
	}
	if store.deliveries[0].EventType != EventComplianceCritical {
		t.Fatalf("expected event type %s, got %s", EventComplianceCritical, store.deliveries[0].EventType)
	}
}

func TestEmitComplianceEvents_NoWebhooks(t *testing.T) {
	t.Parallel()

	store := &webhookEventStore{
		webhooks: nil, // no webhooks configured
	}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, &stubQueue{})

	results := []compliance.Result{
		{RuleID: "rule-1", Passed: false, Severity: "high", CheckedAt: time.Now()},
	}

	// Should not panic when no webhooks exist.
	srv.emitComplianceEvents(context.Background(), uuid.New(), uuid.New(), results, "scan-004")

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deliveries) != 0 {
		t.Fatalf("expected 0 deliveries, got %d", len(store.deliveries))
	}
}

func TestEmitComplianceEvents_DisabledWebhookSkipped(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()

	store := &webhookEventStore{
		webhooks: []storage.Webhook{
			{
				ID:             uuid.New(),
				TenantID:       uuid.NullUUID{UUID: tenantID, Valid: true},
				Name:           "disabled-hook",
				URL:            "http://localhost:9999/hook",
				Events:         []string{EventComplianceCompleted},
				Enabled:        false, // disabled
				VerifySSL:      false,
				TimeoutSeconds: 2,
				Headers:        map[string]any{},
				Metadata:       map[string]any{},
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
	}

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, &stubQueue{})

	results := []compliance.Result{
		{RuleID: "rule-1", Passed: true, Severity: "low", CheckedAt: time.Now()},
	}

	srv.emitComplianceEvents(context.Background(), tenantID, uuid.New(), results, "scan-005")

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deliveries) != 0 {
		t.Fatalf("expected 0 deliveries for disabled webhook, got %d", len(store.deliveries))
	}
}

func TestSeverityRankOrdering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		severity string
		rank     int
	}{
		{"low", "low", 1},
		{"medium", "medium", 2},
		{"high", "high", 3},
		{"critical", "critical", 4},
		{"unknown returns zero", "unknown", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := severityRank[tt.severity]
			if got != tt.rank {
				t.Errorf("severityRank[%q] = %d, want %d", tt.severity, got, tt.rank)
			}
		})
	}
}

// Ensure webhookEventStore satisfies the Store interface (compile-time check).
var _ Store = (*webhookEventStore)(nil)

// webhookEventStore needs to implement all the extra Store methods that fakeStore doesn't cover
// for the new interface methods. The embedded fakeStore handles most; we override the webhook ones above.
// We also need to provide the method that returns NullUUID tenant for the webhook.
func makeTestWebhook(tenantID uuid.UUID, events []string) storage.Webhook {
	return storage.Webhook{
		ID:             uuid.New(),
		TenantID:       uuid.NullUUID{UUID: tenantID, Valid: true},
		Name:           "test-webhook",
		URL:            "http://localhost:9999/hook",
		Secret:         sql.NullString{},
		Events:         events,
		Enabled:        true,
		VerifySSL:      false,
		TimeoutSeconds: 2,
		RetryCount:     0,
		Headers:        map[string]any{},
		Metadata:       map[string]any{},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}
