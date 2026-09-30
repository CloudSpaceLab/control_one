package server

import (
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

func TestValidateCorrelationNotificationPolicy(t *testing.T) {
	webhookID := uuid.New()
	tests := []struct {
		name    string
		policy  storage.CorrelationNotificationPolicy
		wantErr bool
	}{
		{
			name: "normalizes supported channels",
			policy: storage.CorrelationNotificationPolicy{
				EmailRecipients: []string{"SOC@Example.test", "soc@example.test"},
				WebhookIDs:      []uuid.UUID{webhookID, webhookID},
				MinimumSeverity: "HIGH",
			},
		},
		{name: "rejects invalid email", policy: storage.CorrelationNotificationPolicy{EmailRecipients: []string{"not-an-email"}}, wantErr: true},
		{name: "rejects invalid severity", policy: storage.CorrelationNotificationPolicy{MinimumSeverity: "urgent"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := validateCorrelationNotificationPolicy(tt.policy)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("validate policy: %v", err)
			}
			if policy.MinimumSeverity != "high" {
				t.Fatalf("minimum severity = %q, want high", policy.MinimumSeverity)
			}
			if len(policy.EmailRecipients) != 1 || policy.EmailRecipients[0] != "soc@example.test" {
				t.Fatalf("email recipients = %#v", policy.EmailRecipients)
			}
			if len(policy.WebhookIDs) != 1 || policy.WebhookIDs[0] != webhookID {
				t.Fatalf("webhook ids = %#v", policy.WebhookIDs)
			}
		})
	}
}

func TestValidateCorrelationRuleRequest(t *testing.T) {
	req := createCorrelationRuleRequest{
		Name: "SSH brute force", EventTypes: []string{"security.event"},
		EventType: "ssh.authentication_failure", WindowSeconds: 20, Threshold: 4,
		GroupBy: []string{"src_ip", "node_id"}, Severity: "high", SuppressionSeconds: 300,
		Conditions:        []storage.CorrelationCondition{{Field: "dst_port", Operator: "eq", Value: "22"}},
		ConditionGroups:   [][]storage.CorrelationCondition{{{Field: "dst_port", Operator: "eq", Value: "80"}}, {{Field: "dst_port", Operator: "eq", Value: "443"}}},
		DistinctField:     "user_name",
		SequenceEventType: "authentication.failure", SequenceThreshold: 4,
		SequenceConditions: []storage.CorrelationCondition{{Field: "auth_result", Operator: "eq", Value: "failure"}},
	}
	if err := validateCorrelationRuleRequest(&req); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if req.Dimension != "src_ip" {
		t.Fatalf("legacy dimension = %q, want first grouping field", req.Dimension)
	}
}

func TestValidateCorrelationRuleRequestAcceptsExpandedLogFields(t *testing.T) {
	fields := []string{
		"node_id", "tenant_id", "event_type", "message", "correlation_id", "dedup_key",
		"src_ip", "dst_ip", "src_port", "dst_port", "protocol", "direction", "process_name", "user_name",
		"auth_result", "status_code", "http_method", "path", "source", "severity", "bytes_in", "bytes_out",
		"duration_ms", "threat_score", "parser_profile", "source_file", "program", "collector_type", "app", "vhost",
		"server_group", "webserver_kind", "country_code", "country", "asn", "application_type", "application_name",
		"application_category", "application_root", "coverage_state", "request_id", "traceparent", "score",
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			req := createCorrelationRuleRequest{
				Name: "Expanded log field", EventTypes: []string{"security.event"},
				WindowSeconds: 60, Threshold: 1, GroupBy: []string{field}, Severity: "high",
				Conditions:    []storage.CorrelationCondition{{Field: field, Operator: "contains", Value: "value"}},
				DistinctField: field,
			}
			if err := validateCorrelationRuleRequest(&req); err != nil {
				t.Fatalf("field %q rejected: %v", field, err)
			}
		})
	}
	for _, field := range []string{"bytes_in", "bytes_out", "duration_ms", "threat_score", "score"} {
		t.Run("aggregate_"+field, func(t *testing.T) {
			req := createCorrelationRuleRequest{
				Name: "Numeric aggregation", EventTypes: []string{"security.event"},
				WindowSeconds: 60, Threshold: 1, GroupBy: []string{"node_id"}, Severity: "high",
				AggregateField: field, AggregateThreshold: 1,
			}
			if err := validateCorrelationRuleRequest(&req); err != nil {
				t.Fatalf("aggregate field %q rejected: %v", field, err)
			}
		})
	}
}

func TestValidateCorrelationRuleRequestRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		req  createCorrelationRuleRequest
	}{
		{"missing name", createCorrelationRuleRequest{EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 4, GroupBy: []string{"node_id"}, Severity: "high"}},
		{"unknown category", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"events.web"}, WindowSeconds: 20, Threshold: 4, GroupBy: []string{"node_id"}, Severity: "high"}},
		{"empty grouping", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 4, Severity: "high"}},
		{"duplicate grouping", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 4, GroupBy: []string{"node_id", "node_id"}, Severity: "high"}},
		{"bad severity", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 4, GroupBy: []string{"node_id"}, Severity: "urgent"}},
		{"bad condition", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 4, GroupBy: []string{"node_id"}, Severity: "high", Conditions: []storage.CorrelationCondition{{Field: "password", Operator: "eq", Value: "x"}}}},
		{"empty condition group", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 4, GroupBy: []string{"node_id"}, Severity: "high", ConditionGroups: [][]storage.CorrelationCondition{{}}}},
		{"bad grouped condition", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 4, GroupBy: []string{"node_id"}, Severity: "high", ConditionGroups: [][]storage.CorrelationCondition{{{Field: "password", Operator: "eq", Value: "x"}}}}},
		{"bad distinct field", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 4, GroupBy: []string{"node_id"}, Severity: "high", DistinctField: "password"}},
		{"sequence missing threshold", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 1, GroupBy: []string{"node_id"}, Severity: "high", SequenceEventType: "authentication.failure"}},
		{"sequence missing type", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 1, GroupBy: []string{"node_id"}, Severity: "high", SequenceThreshold: 4}},
		{"bad aggregate field", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 1, GroupBy: []string{"node_id"}, Severity: "high", AggregateField: "password", AggregateThreshold: 100}},
		{"aggregate missing threshold", createCorrelationRuleRequest{Name: "x", EventTypes: []string{"security.event"}, WindowSeconds: 20, Threshold: 1, GroupBy: []string{"node_id"}, Severity: "high", AggregateField: "bytes_out"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCorrelationRuleRequest(&tt.req); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}
