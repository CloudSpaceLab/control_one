package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Evaluator evaluates a compliance rule against node state.
type Evaluator interface {
	Evaluate(ctx context.Context, rule RuleDefinition, input EvalInput) (*EvalResult, error)
}

// RuleDefinition describes a single compliance rule to evaluate.
type RuleDefinition struct {
	ID         string
	RuleType   string // "json-dsl" for now
	Definition string // raw JSON from policy_versions.rule_definition
	Severity   string
	Framework  string
}

// EvalInput carries the data a rule is evaluated against.
type EvalInput struct {
	NodeID   uuid.UUID
	TenantID uuid.UUID
	NodeMeta map[string]any // node OS, arch, IP, labels
	Facts    map[string]any // runtime-collected state
}

// EvalResult captures the outcome of a single rule evaluation.
type EvalResult struct {
	Passed      bool
	Severity    string
	Details     string
	Remediation string
	Evidence    map[string]any
	CheckedAt   time.Time
}

// dslRule is the parsed JSON-DSL rule format.
type dslRule struct {
	Framework   string         `json:"framework"`
	Control     string         `json:"control"`
	Severity    string         `json:"severity"`
	Description string         `json:"description"`
	Conditions  []dslCondition `json:"conditions"`
	Remediation string         `json:"remediation"`
}

// dslCondition is a single check within a rule.
type dslCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// JSONDSLEvaluator evaluates rules defined as JSON-DSL.
type JSONDSLEvaluator struct{}

// NewJSONDSLEvaluator returns a new evaluator for JSON-DSL rules.
func NewJSONDSLEvaluator() *JSONDSLEvaluator {
	return &JSONDSLEvaluator{}
}

// Evaluate parses the rule definition and checks all conditions against the input.
func (e *JSONDSLEvaluator) Evaluate(_ context.Context, rule RuleDefinition, input EvalInput) (*EvalResult, error) {
	var parsed dslRule
	if err := json.Unmarshal([]byte(rule.Definition), &parsed); err != nil {
		return nil, fmt.Errorf("parse rule definition %s: %w", rule.ID, err)
	}

	severity := parsed.Severity
	if severity == "" {
		severity = rule.Severity
	}

	now := time.Now().UTC()
	evidence := map[string]any{
		"rule_id":   rule.ID,
		"framework": parsed.Framework,
		"control":   parsed.Control,
	}

	// Empty conditions = auto-pass
	if len(parsed.Conditions) == 0 {
		return &EvalResult{
			Passed:    true,
			Severity:  severity,
			Details:   parsed.Description,
			Evidence:  evidence,
			CheckedAt: now,
		}, nil
	}

	// All conditions must pass (AND logic)
	for i, cond := range parsed.Conditions {
		fieldVal, found := resolveField(cond.Field, input)
		pass, err := evaluateCondition(cond.Op, fieldVal, found, cond.Value)
		if err != nil {
			return nil, fmt.Errorf("condition[%d] field=%s op=%s: %w", i, cond.Field, cond.Op, err)
		}
		if !pass {
			evidence["failed_condition"] = map[string]any{
				"field":    cond.Field,
				"op":       cond.Op,
				"expected": cond.Value,
				"actual":   fieldVal,
			}
			return &EvalResult{
				Passed:      false,
				Severity:    severity,
				Details:     fmt.Sprintf("%s: condition failed on %s", parsed.Description, cond.Field),
				Remediation: parsed.Remediation,
				Evidence:    evidence,
				CheckedAt:   now,
			}, nil
		}
	}

	return &EvalResult{
		Passed:    true,
		Severity:  severity,
		Details:   parsed.Description,
		Evidence:  evidence,
		CheckedAt: now,
	}, nil
}

// resolveField looks up a dot-separated field path in the EvalInput.
// "node.os" -> input.NodeMeta["os"]
// "facts.ssh.password_auth" -> input.Facts["ssh"]["password_auth"]
func resolveField(path string, input EvalInput) (any, bool) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) < 2 {
		return nil, false
	}

	var root map[string]any
	switch parts[0] {
	case "node":
		root = input.NodeMeta
	case "facts":
		root = input.Facts
	default:
		return nil, false
	}

	return resolveNestedField(parts[1], root)
}

func resolveNestedField(path string, data map[string]any) (any, bool) {
	if data == nil {
		return nil, false
	}

	// Try the full path as a flat key first — this supports agent-sent facts
	// like {"security.fail2ban.installed": "true"} without requiring nesting.
	if val, ok := data[path]; ok {
		return val, true
	}

	parts := strings.SplitN(path, ".", 2)
	val, ok := data[parts[0]]
	if !ok {
		return nil, false
	}

	if len(parts) == 1 {
		return val, true
	}

	// Recurse into nested map
	nested, ok := val.(map[string]any)
	if !ok {
		return nil, false
	}
	return resolveNestedField(parts[1], nested)
}

func evaluateCondition(op string, fieldVal any, fieldFound bool, expected any) (bool, error) {
	switch op {
	case "exists":
		return fieldFound, nil
	case "not_exists":
		return !fieldFound, nil
	case "eq":
		if !fieldFound {
			return false, nil
		}
		return compareEqual(fieldVal, expected), nil
	case "neq":
		if !fieldFound {
			return true, nil
		}
		return !compareEqual(fieldVal, expected), nil
	case "in":
		if !fieldFound {
			return false, nil
		}
		return compareIn(fieldVal, expected), nil
	case "not_in":
		if !fieldFound {
			return true, nil
		}
		return !compareIn(fieldVal, expected), nil
	case "gt":
		if !fieldFound {
			return false, nil
		}
		return compareNumeric(fieldVal, expected, func(a, b float64) bool { return a > b })
	case "lt":
		if !fieldFound {
			return false, nil
		}
		return compareNumeric(fieldVal, expected, func(a, b float64) bool { return a < b })
	case "gte":
		if !fieldFound {
			return false, nil
		}
		return compareNumeric(fieldVal, expected, func(a, b float64) bool { return a >= b })
	case "lte":
		if !fieldFound {
			return false, nil
		}
		return compareNumeric(fieldVal, expected, func(a, b float64) bool { return a <= b })
	case "regex":
		if !fieldFound {
			return false, nil
		}
		return compareRegex(fieldVal, expected)
	default:
		return false, fmt.Errorf("unknown operator %q", op)
	}
}

func compareEqual(a, b any) bool {
	// Normalize numeric types for comparison
	aNum, aOk := toFloat64(a)
	bNum, bOk := toFloat64(b)
	if aOk && bOk {
		return aNum == bNum
	}
	return reflect.DeepEqual(a, b)
}

func compareIn(val any, set any) bool {
	arr, ok := set.([]any)
	if !ok {
		return false
	}
	for _, item := range arr {
		if compareEqual(val, item) {
			return true
		}
	}
	return false
}

func compareNumeric(a, b any, cmp func(float64, float64) bool) (bool, error) {
	aNum, aOk := toFloat64(a)
	bNum, bOk := toFloat64(b)
	if !aOk || !bOk {
		return false, fmt.Errorf("numeric comparison requires numeric values, got %T and %T", a, b)
	}
	return cmp(aNum, bNum), nil
}

func compareRegex(val any, pattern any) (bool, error) {
	str := fmt.Sprintf("%v", val)
	pat, ok := pattern.(string)
	if !ok {
		return false, fmt.Errorf("regex pattern must be string, got %T", pattern)
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return false, fmt.Errorf("invalid regex %q: %w", pat, err)
	}
	return re.MatchString(str), nil
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
