package compliance

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestJSONDSLEvaluator_Operators(t *testing.T) {
	eval := NewJSONDSLEvaluator()
	ctx := context.Background()
	input := EvalInput{
		NodeID:   uuid.New(),
		TenantID: uuid.New(),
		NodeMeta: map[string]any{
			"os":   "linux",
			"arch": "amd64",
		},
		Facts: map[string]any{
			"ssh": map[string]any{
				"password_auth": false,
				"port":          22.0,
			},
			"firewall": map[string]any{
				"enabled": true,
			},
			"uptime_hours": 100.0,
		},
	}

	tests := []struct {
		name       string
		definition string
		wantPass   bool
		wantErr    bool
	}{
		{
			name:       "eq_pass",
			definition: `{"conditions":[{"field":"node.os","op":"eq","value":"linux"}]}`,
			wantPass:   true,
		},
		{
			name:       "eq_fail",
			definition: `{"conditions":[{"field":"node.os","op":"eq","value":"windows"}]}`,
			wantPass:   false,
		},
		{
			name:       "eq_bool",
			definition: `{"conditions":[{"field":"facts.ssh.password_auth","op":"eq","value":false}]}`,
			wantPass:   true,
		},
		{
			name:       "neq_pass",
			definition: `{"conditions":[{"field":"node.os","op":"neq","value":"windows"}]}`,
			wantPass:   true,
		},
		{
			name:       "neq_fail",
			definition: `{"conditions":[{"field":"node.os","op":"neq","value":"linux"}]}`,
			wantPass:   false,
		},
		{
			name:       "in_pass",
			definition: `{"conditions":[{"field":"node.os","op":"in","value":["linux","darwin"]}]}`,
			wantPass:   true,
		},
		{
			name:       "in_fail",
			definition: `{"conditions":[{"field":"node.os","op":"in","value":["windows","freebsd"]}]}`,
			wantPass:   false,
		},
		{
			name:       "not_in_pass",
			definition: `{"conditions":[{"field":"node.os","op":"not_in","value":["windows","freebsd"]}]}`,
			wantPass:   true,
		},
		{
			name:       "not_in_fail",
			definition: `{"conditions":[{"field":"node.os","op":"not_in","value":["linux","darwin"]}]}`,
			wantPass:   false,
		},
		{
			name:       "gt_pass",
			definition: `{"conditions":[{"field":"facts.uptime_hours","op":"gt","value":50}]}`,
			wantPass:   true,
		},
		{
			name:       "gt_fail",
			definition: `{"conditions":[{"field":"facts.uptime_hours","op":"gt","value":200}]}`,
			wantPass:   false,
		},
		{
			name:       "lt_pass",
			definition: `{"conditions":[{"field":"facts.ssh.port","op":"lt","value":1024}]}`,
			wantPass:   true,
		},
		{
			name:       "gte_pass_equal",
			definition: `{"conditions":[{"field":"facts.uptime_hours","op":"gte","value":100}]}`,
			wantPass:   true,
		},
		{
			name:       "lte_pass",
			definition: `{"conditions":[{"field":"facts.ssh.port","op":"lte","value":22}]}`,
			wantPass:   true,
		},
		{
			name:       "exists_pass",
			definition: `{"conditions":[{"field":"facts.firewall.enabled","op":"exists","value":null}]}`,
			wantPass:   true,
		},
		{
			name:       "exists_fail",
			definition: `{"conditions":[{"field":"facts.nonexistent","op":"exists","value":null}]}`,
			wantPass:   false,
		},
		{
			name:       "not_exists_pass",
			definition: `{"conditions":[{"field":"facts.nonexistent","op":"not_exists","value":null}]}`,
			wantPass:   true,
		},
		{
			name:       "not_exists_fail",
			definition: `{"conditions":[{"field":"facts.firewall.enabled","op":"not_exists","value":null}]}`,
			wantPass:   false,
		},
		{
			name:       "regex_pass",
			definition: `{"conditions":[{"field":"node.os","op":"regex","value":"^lin"}]}`,
			wantPass:   true,
		},
		{
			name:       "regex_fail",
			definition: `{"conditions":[{"field":"node.os","op":"regex","value":"^win"}]}`,
			wantPass:   false,
		},
		{
			name:       "multiple_conditions_and",
			definition: `{"conditions":[{"field":"node.os","op":"eq","value":"linux"},{"field":"facts.firewall.enabled","op":"eq","value":true}]}`,
			wantPass:   true,
		},
		{
			name:       "multiple_conditions_one_fails",
			definition: `{"conditions":[{"field":"node.os","op":"eq","value":"linux"},{"field":"facts.ssh.password_auth","op":"eq","value":true}]}`,
			wantPass:   false,
		},
		{
			name:       "empty_conditions_unsupported",
			definition: `{"conditions":[]}`,
			wantPass:   false,
		},
		{
			name:       "missing_field_eq_fails",
			definition: `{"conditions":[{"field":"facts.nonexistent","op":"eq","value":"x"}]}`,
			wantPass:   false,
		},
		{
			name:       "missing_field_neq_fails_unsupported",
			definition: `{"conditions":[{"field":"facts.nonexistent","op":"neq","value":"x"}]}`,
			wantPass:   false,
		},
		{
			name:       "missing_field_not_in_fails_unsupported",
			definition: `{"conditions":[{"field":"facts.nonexistent","op":"not_in","value":["x"]}]}`,
			wantPass:   false,
		},
		{
			name:       "unknown_op_errors",
			definition: `{"conditions":[{"field":"node.os","op":"unknown","value":"x"}]}`,
			wantErr:    true,
		},
		{
			name:       "invalid_json_errors",
			definition: `{bad json`,
			wantErr:    true,
		},
		{
			name:       "severity_from_rule",
			definition: `{"severity":"critical","description":"test","conditions":[{"field":"node.os","op":"eq","value":"linux"}]}`,
			wantPass:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := RuleDefinition{
				ID:         "test-" + tt.name,
				RuleType:   "json-dsl",
				Definition: tt.definition,
				Severity:   "medium",
			}
			result, err := eval.Evaluate(ctx, rule, input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Passed != tt.wantPass {
				t.Fatalf("expected passed=%v, got %v (details: %s)", tt.wantPass, result.Passed, result.Details)
			}
		})
	}
}

func TestJSONDSLEvaluator_MissingObservedFactIsUnsupported(t *testing.T) {
	eval := NewJSONDSLEvaluator()
	result, err := eval.Evaluate(context.Background(), RuleDefinition{
		ID:         "missing-negative",
		RuleType:   "json-dsl",
		Definition: `{"description":"SSH root login must not be enabled","conditions":[{"field":"facts.ssh.root_login","op":"neq","value":"yes"}]}`,
		Severity:   "medium",
	}, EvalInput{
		NodeID:   uuid.New(),
		TenantID: uuid.New(),
		Facts:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Passed {
		t.Fatal("missing telemetry must not pass a negative compliance assertion")
	}
	if result.Outcome != "unsupported" {
		t.Fatalf("outcome = %q, want unsupported", result.Outcome)
	}
	if result.Evidence["outcome"] != "unsupported" || result.Evidence["unsupported_condition"] == nil {
		t.Fatalf("expected unsupported evidence, got %#v", result.Evidence)
	}
}

func TestJSONDSLEvaluator_SeverityAndRemediation(t *testing.T) {
	eval := NewJSONDSLEvaluator()
	rule := RuleDefinition{
		ID:         "sev-test",
		RuleType:   "json-dsl",
		Definition: `{"severity":"critical","description":"SSH check","conditions":[{"field":"facts.ssh.password_auth","op":"eq","value":true}],"remediation":"Disable password auth"}`,
		Severity:   "low",
	}
	input := EvalInput{
		Facts: map[string]any{"ssh": map[string]any{"password_auth": false}},
	}
	result, err := eval.Evaluate(context.Background(), rule, input)
	if err != nil {
		t.Fatal(err)
	}
	// Fails because actual is false, expected is true
	if result.Passed {
		t.Fatal("expected failure")
	}
	if result.Severity != "critical" {
		t.Fatalf("expected severity critical, got %s", result.Severity)
	}
	if result.Remediation != "Disable password auth" {
		t.Fatalf("expected remediation text, got %s", result.Remediation)
	}
}

func TestResolveField(t *testing.T) {
	input := EvalInput{
		NodeMeta: map[string]any{"os": "linux"},
		Facts: map[string]any{
			"level1": map[string]any{
				"level2": map[string]any{
					"value": "deep",
				},
			},
		},
	}

	tests := []struct {
		path      string
		wantVal   any
		wantFound bool
	}{
		{"node.os", "linux", true},
		{"facts.level1.level2.value", "deep", true},
		{"facts.nonexistent", nil, false},
		{"unknown.field", nil, false},
		{"noprefix", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			val, found := resolveField(tt.path, input)
			if found != tt.wantFound {
				t.Fatalf("found=%v, want %v", found, tt.wantFound)
			}
			if found && val != tt.wantVal {
				t.Fatalf("val=%v, want %v", val, tt.wantVal)
			}
		})
	}
}
