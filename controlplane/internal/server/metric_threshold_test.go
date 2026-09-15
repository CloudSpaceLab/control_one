package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// testMetricStore extends fakeStore with metric-threshold-specific test doubles.
type testMetricStore struct {
	fakeStore
	rules              []storage.MetricThresholdRule
	metricCountResults map[string]int // keyed by "metricName:operator:threshold:window"
}

func (f *testMetricStore) ListEnabledMetricThresholdRules(_ context.Context, tenantID uuid.UUID) ([]storage.MetricThresholdRule, error) {
	var out []storage.MetricThresholdRule
	for _, r := range f.rules {
		if r.TenantID == tenantID && r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *testMetricStore) CountMetricValueInWindow(_ context.Context, _, _ uuid.UUID, metricName, operator string, threshold, windowSeconds float64) (int, error) {
	if f.metricCountResults != nil {
		key := metricName + ":" + operator + ":" + floatToStr(threshold) + ":" + floatToStr(windowSeconds)
		if count, ok := f.metricCountResults[key]; ok {
			return count, nil
		}
	}
	return 0, nil
}

func floatToStr(f float64) string {
	return time.Duration(f * float64(time.Second)).String()
}

func TestMetricThresholdAboveThresholdFiresAlert(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()

	store := &testMetricStore{
		rules: []storage.MetricThresholdRule{
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				Name:          "high memory",
				MetricName:    "memory_used_percent",
				Operator:      "gt",
				Threshold:     80.0,
				WindowSeconds: 300,
				Severity:      "high",
				Action:        "alert",
				Enabled:       true,
			},
		},
		metricCountResults: map[string]int{
			"memory_used_percent:gt:" + floatToStr(80.0) + ":" + floatToStr(300.0): 3,
		},
	}

	bus := eventbus.New(16)
	srv := &Server{
		store:       store,
		logger:      zap.NewNop(),
		eventBus:    bus,
	}

	sub := bus.Subscribe(tenantID, []string{eventbus.TopicAlertOpened}, nil)
	defer sub.Close()

	metrics := []storage.CreateTelemetryMetricParams{
		{TenantID: tenantID, NodeID: nodeID, MetricName: "memory_used_percent", MetricValue: 92.5},
	}
	srv.evaluateMetricThresholds(context.Background(), tenantID, nodeID, metrics)

	select {
	case ev := <-sub.Ch:
		if ev.Topic != eventbus.TopicAlertOpened {
			t.Fatalf("expected alert.opened topic, got %s", ev.Topic)
		}
	case <-time.After(time.Second):
		t.Fatal("expected alert event but got none")
	}
}

func TestMetricThresholdBelowThresholdDoesNotFire(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()

	store := &testMetricStore{
		rules: []storage.MetricThresholdRule{
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				Name:          "high cpu",
				MetricName:    "cpu_used_percent",
				Operator:      "gt",
				Threshold:     80.0,
				WindowSeconds: 300,
				Severity:      "high",
				Action:        "alert",
				Enabled:       true,
			},
		},
	}

	bus := eventbus.New(16)
	srv := &Server{
		store:    store,
		logger:   zap.NewNop(),
		eventBus: bus,
	}

	sub := bus.Subscribe(tenantID, []string{eventbus.TopicAlertOpened}, nil)
	defer sub.Close()

	metrics := []storage.CreateTelemetryMetricParams{
		{TenantID: tenantID, NodeID: nodeID, MetricName: "cpu_used_percent", MetricValue: 50.0},
	}
	srv.evaluateMetricThresholds(context.Background(), tenantID, nodeID, metrics)

	select {
	case <-sub.Ch:
		t.Fatal("did not expect alert event for value below threshold")
	case <-time.After(200 * time.Millisecond):
		// expected: no alert
	}
}

func TestMetricThresholdCooldownPreventsAlertStorm(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()

	store := &testMetricStore{
		rules: []storage.MetricThresholdRule{
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				Name:          "high memory",
				MetricName:    "memory_used_percent",
				Operator:      "gt",
				Threshold:     80.0,
				WindowSeconds: 300,
				Severity:      "high",
				Action:        "alert",
				Enabled:       true,
			},
		},
		metricCountResults: map[string]int{
			"memory_used_percent:gt:" + floatToStr(80.0) + ":" + floatToStr(300.0): 3,
		},
	}

	bus := eventbus.New(16)
	srv := &Server{
		store:    store,
		logger:   zap.NewNop(),
		eventBus: bus,
	}

	sub := bus.Subscribe(tenantID, []string{eventbus.TopicAlertOpened}, nil)
	defer sub.Close()

	metrics := []storage.CreateTelemetryMetricParams{
		{TenantID: tenantID, NodeID: nodeID, MetricName: "memory_used_percent", MetricValue: 92.5},
	}

	// First call should fire.
	srv.evaluateMetricThresholds(context.Background(), tenantID, nodeID, metrics)
	select {
	case <-sub.Ch:
	case <-time.After(time.Second):
		t.Fatal("expected first alert event")
	}

	// Second call immediately should be suppressed by cooldown.
	srv.evaluateMetricThresholds(context.Background(), tenantID, nodeID, metrics)
	select {
	case <-sub.Ch:
		t.Fatal("cooldown should prevent second alert")
	case <-time.After(200 * time.Millisecond):
		// expected: no second alert
	}
}

func TestMetricThresholdDisabledRuleIgnored(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()

	store := &testMetricStore{
		rules: []storage.MetricThresholdRule{
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				Name:          "disabled rule",
				MetricName:    "memory_used_percent",
				Operator:      "gt",
				Threshold:     80.0,
				WindowSeconds: 300,
				Severity:      "high",
				Action:        "alert",
				Enabled:       false, // disabled
			},
		},
		metricCountResults: map[string]int{
			"memory_used_percent:gt:" + floatToStr(80.0) + ":" + floatToStr(300.0): 3,
		},
	}

	bus := eventbus.New(16)
	srv := &Server{
		store:    store,
		logger:   zap.NewNop(),
		eventBus: bus,
	}

	sub := bus.Subscribe(tenantID, []string{eventbus.TopicAlertOpened}, nil)
	defer sub.Close()

	metrics := []storage.CreateTelemetryMetricParams{
		{TenantID: tenantID, NodeID: nodeID, MetricName: "memory_used_percent", MetricValue: 92.5},
	}
	srv.evaluateMetricThresholds(context.Background(), tenantID, nodeID, metrics)

	select {
	case <-sub.Ch:
		t.Fatal("disabled rule should not fire alert")
	case <-time.After(200 * time.Millisecond):
		// expected: no alert
	}
}

func TestMetricThresholdTargetNodeMismatchIgnored(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	otherNodeID := uuid.New()

	store := &testMetricStore{
		rules: []storage.MetricThresholdRule{
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				Name:          "targeted rule",
				MetricName:    "cpu_used_percent",
				Operator:      "gt",
				Threshold:     90.0,
				WindowSeconds: 300,
				Severity:      "critical",
				Action:        "alert",
				TargetNodeID:  &otherNodeID,
				Enabled:       true,
			},
		},
		metricCountResults: map[string]int{
			"cpu_used_percent:gt:" + floatToStr(90.0) + ":" + floatToStr(300.0): 5,
		},
	}

	bus := eventbus.New(16)
	srv := &Server{
		store:    store,
		logger:   zap.NewNop(),
		eventBus: bus,
	}

	sub := bus.Subscribe(tenantID, []string{eventbus.TopicAlertOpened}, nil)
	defer sub.Close()

	metrics := []storage.CreateTelemetryMetricParams{
		{TenantID: tenantID, NodeID: nodeID, MetricName: "cpu_used_percent", MetricValue: 95.0},
	}
	srv.evaluateMetricThresholds(context.Background(), tenantID, nodeID, metrics)

	select {
	case <-sub.Ch:
		t.Fatal("rule targeting a different node should not fire")
	case <-time.After(200 * time.Millisecond):
		// expected: no alert
	}
}

func TestThresholdExceeded(t *testing.T) {
	tests := []struct {
		value     float64
		operator  string
		threshold float64
		expected  bool
	}{
		{90.0, "gt", 80.0, true},
		{80.0, "gt", 80.0, false},
		{80.0, "gte", 80.0, true},
		{79.0, "gte", 80.0, false},
		{70.0, "lt", 80.0, true},
		{80.0, "lt", 80.0, false},
		{80.0, "lte", 80.0, true},
		{81.0, "lte", 80.0, false},
		{80.0, "eq", 80.0, true},
		{80.1, "eq", 80.0, false},
	}

	for _, tt := range tests {
		got := thresholdExceeded(tt.value, tt.operator, tt.threshold)
		if got != tt.expected {
			t.Errorf("thresholdExceeded(%v, %q, %v) = %v, want %v", tt.value, tt.operator, tt.threshold, got, tt.expected)
		}
	}
}
