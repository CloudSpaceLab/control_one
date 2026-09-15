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

func TestEngineMatchesSpecificTypeGroupsFieldsAndSuppressesDuplicates(t *testing.T) {
	tenant := uuid.New()
	node := uuid.New()
	rule := storage.CorrelationRule{
		ID: uuid.New(), TenantID: tenant, Name: "SSH brute force",
		EventTypes: []string{eventbus.TopicSecurityEvent}, EventType: "ssh.authentication_failure",
		WindowSeconds: 20, Threshold: 3, GroupBy: []string{"src_ip", "node_id"},
		SuppressionSeconds: 300, Severity: "high", Enabled: true,
		Conditions: []storage.CorrelationCondition{{Field: "dst_port", Operator: "eq", Value: "22"}, {Field: "auth_result", Operator: "eq", Value: "failure"}},
	}
	store := &fakeStore{rules: []storage.CorrelationRule{rule}}
	eng := New(store, eventbus.New(16), nil)
	base := time.Now()
	payload := func(eventType, authResult string) []byte {
		blob, _ := json.Marshal(map[string]any{"event_type": eventType, "src_ip": "203.0.113.8", "details": map[string]any{"dst_port": 22, "auth_result": authResult}})
		return blob
	}

	eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: base, Payload: payload("malware.detected", "failure")})
	eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: base, Payload: payload("ssh.authentication_failure", "success")})
	for i := 0; i < 6; i++ {
		eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: base.Add(time.Duration(i+1) * time.Second), Payload: payload("ssh.authentication_failure", "failure")})
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.alerts) != 1 {
		t.Fatalf("want exactly 1 suppressed alert, got %d", len(store.alerts))
	}
	wantKey := rule.ID.String() + "/src_ip=203.0.113.8|node_id=" + node.String()
	if store.alerts[0].DedupKey != wantKey {
		t.Fatalf("dedup key = %q, want %q", store.alerts[0].DedupKey, wantKey)
	}
}

func TestEnginePhase3AThresholdTemplatesMatchAndRejectEvents(t *testing.T) {
	tests := []struct {
		name       string
		eventType  string
		threshold  int
		window     int
		groupBy    []string
		conditions []storage.CorrelationCondition
		matching   map[string]any
		nonmatch   map[string]any
	}{
		{
			name: "SSH brute force", eventType: "ssh.authentication_failure", threshold: 4, window: 20,
			groupBy:    []string{"src_ip", "node_id"},
			conditions: []storage.CorrelationCondition{{Field: "dst_port", Operator: "eq", Value: "22"}, {Field: "protocol", Operator: "eq", Value: "tcp"}, {Field: "auth_result", Operator: "eq", Value: "failure"}},
			matching:   map[string]any{"src_ip": "203.0.113.20", "dst_port": 22, "protocol": "tcp", "auth_result": "failure"},
			nonmatch:   map[string]any{"src_ip": "203.0.113.20", "dst_port": 23, "protocol": "tcp", "auth_result": "failure"},
		},
		{
			name: "Windows repeated login failures", eventType: "windows.authentication_failure", threshold: 5, window: 60,
			groupBy:    []string{"user_name", "node_id"},
			conditions: []storage.CorrelationCondition{{Field: "auth_result", Operator: "eq", Value: "failure"}},
			matching:   map[string]any{"user_name": "administrator", "auth_result": "failure"},
			nonmatch:   map[string]any{"user_name": "administrator", "auth_result": "success"},
		},
		{
			name: "Repeated web-server errors", eventType: "web.request", threshold: 10, window: 60,
			groupBy:    []string{"node_id"},
			conditions: []storage.CorrelationCondition{{Field: "status_code", Operator: "gte", Value: "500"}},
			matching:   map[string]any{"status_code": 503},
			nonmatch:   map[string]any{"status_code": 404},
		},
		{
			name: "Database authentication failures", eventType: "database.authentication_failure", threshold: 5, window: 60,
			groupBy:    []string{"user_name", "node_id"},
			conditions: []storage.CorrelationCondition{{Field: "auth_result", Operator: "eq", Value: "failure"}},
			matching:   map[string]any{"user_name": "dbadmin", "auth_result": "failure"},
			nonmatch:   map[string]any{"user_name": "dbadmin", "auth_result": "success"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenant, node := uuid.New(), uuid.New()
			rule := storage.CorrelationRule{
				ID: uuid.New(), TenantID: tenant, Name: tt.name,
				EventTypes: []string{eventbus.TopicSecurityEvent}, EventType: tt.eventType,
				WindowSeconds: tt.window, Threshold: tt.threshold, GroupBy: tt.groupBy,
				SuppressionSeconds: 300, Severity: "high", Enabled: true, Conditions: tt.conditions,
			}
			store := &fakeStore{rules: []storage.CorrelationRule{rule}}
			eng := New(store, eventbus.New(tt.threshold+2), nil)
			base := time.Now()
			payload := func(fields map[string]any) []byte {
				body := map[string]any{"event_type": tt.eventType, "details": fields}
				for key, value := range fields {
					body[key] = value
				}
				blob, err := json.Marshal(body)
				if err != nil {
					t.Fatalf("marshal event: %v", err)
				}
				return blob
			}

			eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: base, Payload: payload(tt.nonmatch)})
			for i := 0; i < tt.threshold-1; i++ {
				eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: base.Add(time.Duration(i+1) * time.Second), Payload: payload(tt.matching)})
			}
			if len(store.alerts) != 0 {
				t.Fatalf("nonmatching event counted toward threshold: got %d alerts", len(store.alerts))
			}
			eng.handle(context.Background(), eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: base.Add(time.Duration(tt.threshold) * time.Second), Payload: payload(tt.matching)})
			if len(store.alerts) != 1 {
				t.Fatalf("matching events should create one alert, got %d", len(store.alerts))
			}
		})
	}
}

func TestEngineMatchesAnyConditionGroup(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	rule := storage.CorrelationRule{
		ID: uuid.New(), TenantID: tenant, Name: "Web request flood", EventTypes: []string{eventbus.TopicSecurityEvent},
		EventType: "web.request", WindowSeconds: 60, Threshold: 2, GroupBy: []string{"src_ip", "node_id"}, Severity: "high", Enabled: true,
		ConditionGroups: [][]storage.CorrelationCondition{{{Field: "dst_port", Operator: "eq", Value: "80"}}, {{Field: "dst_port", Operator: "eq", Value: "443"}}},
	}
	store := &fakeStore{rules: []storage.CorrelationRule{rule}}
	eng := New(store, eventbus.New(4), nil)
	base := time.Now()
	event := func(port int, at time.Time) eventbus.Event {
		payload, _ := json.Marshal(map[string]any{"event_type": "web.request", "src_ip": "203.0.113.30", "dst_port": port})
		return eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: at, Payload: payload}
	}
	eng.handle(context.Background(), event(22, base))
	eng.handle(context.Background(), event(80, base.Add(time.Second)))
	eng.handle(context.Background(), event(443, base.Add(2*time.Second)))
	if len(store.alerts) != 1 {
		t.Fatalf("ports 80 and 443 should satisfy the OR groups, got %d alerts", len(store.alerts))
	}
}

func TestEngineCountsDistinctFieldValues(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	rule := storage.CorrelationRule{
		ID: uuid.New(), TenantID: tenant, Name: "Credential stuffing", EventTypes: []string{eventbus.TopicSecurityEvent},
		EventType: "authentication.failure", WindowSeconds: 60, Threshold: 3, GroupBy: []string{"src_ip", "node_id"}, Severity: "critical", Enabled: true,
		Conditions: []storage.CorrelationCondition{{Field: "auth_result", Operator: "eq", Value: "failure"}}, DistinctField: "user_name",
	}
	store := &fakeStore{rules: []storage.CorrelationRule{rule}}
	eng := New(store, eventbus.New(8), nil)
	base := time.Now()
	event := func(user string, at time.Time) eventbus.Event {
		payload, _ := json.Marshal(map[string]any{"event_type": "authentication.failure", "src_ip": "203.0.113.40", "user_name": user, "auth_result": "failure"})
		return eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: at, Payload: payload}
	}
	eng.handle(context.Background(), event("alice", base))
	eng.handle(context.Background(), event("alice", base.Add(time.Second)))
	eng.handle(context.Background(), event("bob", base.Add(2*time.Second)))
	if len(store.alerts) != 0 {
		t.Fatal("a repeated username must count once")
	}
	eng.handle(context.Background(), event("carol", base.Add(3*time.Second)))
	if len(store.alerts) != 1 {
		t.Fatalf("three distinct usernames should create one alert, got %d", len(store.alerts))
	}
	if got := store.alerts[0].Context["hits"]; got != 3 {
		t.Fatalf("alert hits = %#v, want 3 distinct values", got)
	}
}

func TestEnginePhase3BTemplatesRejectNonmatchingEvents(t *testing.T) {
	tests := []struct {
		name            string
		eventType       string
		conditions      []storage.CorrelationCondition
		conditionGroups [][]storage.CorrelationCondition
		distinctField   string
		matching        map[string]any
		nonmatching     map[string]any
	}{
		{"Web request flood", "web.request", []storage.CorrelationCondition{{Field: "protocol", Operator: "eq", Value: "tcp"}}, [][]storage.CorrelationCondition{{{Field: "dst_port", Operator: "eq", Value: "80"}}, {{Field: "dst_port", Operator: "eq", Value: "443"}}}, "", map[string]any{"protocol": "tcp", "dst_port": 443}, map[string]any{"protocol": "tcp", "dst_port": 22}},
		{"Web path scanner", "web.request", nil, [][]storage.CorrelationCondition{{{Field: "path", Operator: "contains", Value: "/.env"}}, {{Field: "path", Operator: "contains", Value: "/wp-admin"}}, {{Field: "path", Operator: "contains", Value: "/phpmyadmin"}}}, "", map[string]any{"path": "/phpmyadmin/index.php"}, map[string]any{"path": "/health"}},
		{"Credential stuffing", "authentication.failure", []storage.CorrelationCondition{{Field: "auth_result", Operator: "eq", Value: "failure"}}, nil, "user_name", map[string]any{"auth_result": "failure", "user_name": "alice"}, map[string]any{"auth_result": "success", "user_name": "alice"}},
		{"Port scanning", "network.connection", []storage.CorrelationCondition{{Field: "protocol", Operator: "eq", Value: "tcp"}}, nil, "dst_port", map[string]any{"protocol": "tcp", "dst_port": 443}, map[string]any{"protocol": "udp", "dst_port": 443}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenant, node := uuid.New(), uuid.New()
			rule := storage.CorrelationRule{ID: uuid.New(), TenantID: tenant, Name: tt.name, EventTypes: []string{eventbus.TopicSecurityEvent}, EventType: tt.eventType, WindowSeconds: 60, Threshold: 1, GroupBy: []string{"src_ip", "node_id"}, Severity: "high", Enabled: true, Conditions: tt.conditions, ConditionGroups: tt.conditionGroups, DistinctField: tt.distinctField}
			store := &fakeStore{rules: []storage.CorrelationRule{rule}}
			eng := New(store, eventbus.New(4), nil)
			event := func(fields map[string]any, at time.Time) eventbus.Event {
				body := map[string]any{"event_type": tt.eventType, "src_ip": "203.0.113.50"}
				for key, value := range fields {
					body[key] = value
				}
				payload, _ := json.Marshal(body)
				return eventbus.Event{Topic: eventbus.TopicSecurityEvent, TenantID: tenant, NodeID: &node, Timestamp: at, Payload: payload}
			}
			base := time.Now()
			eng.handle(context.Background(), event(tt.nonmatching, base))
			if len(store.alerts) != 0 {
				t.Fatal("nonmatching event created an alert")
			}
			eng.handle(context.Background(), event(tt.matching, base.Add(time.Second)))
			if len(store.alerts) != 1 {
				t.Fatalf("matching event should create one alert, got %d", len(store.alerts))
			}
		})
	}
}
