package server

import "testing"

func TestValidateCorrelationRuleRequest(t *testing.T) {
	req := createCorrelationRuleRequest{
		Name: "SSH brute force", EventTypes: []string{"security.event"},
		EventType: "ssh.authentication_failure", WindowSeconds: 20, Threshold: 4,
		GroupBy: []string{"src_ip", "node_id"}, Severity: "high", SuppressionSeconds: 300,
	}
	if err := validateCorrelationRuleRequest(&req); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if req.Dimension != "src_ip" {
		t.Fatalf("legacy dimension = %q, want first grouping field", req.Dimension)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCorrelationRuleRequest(&tt.req); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}
