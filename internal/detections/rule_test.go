package detections

import (
	"testing"
	"time"
)

func TestRuleEvaluateNestedPredicates(t *testing.T) {
	rule := Rule{
		ID:       "windows.powershell.encoded",
		Title:    "Suspicious PowerShell Encoded Command",
		Severity: "high",
		LogSource: LogSource{
			Product:  "windows",
			Category: "process_creation",
		},
		Expression: All(
			Field("event.category", OpEquals, "process"),
			Field("process.executable", OpEndsWith, `\powershell.exe`),
			Any(
				Field("process.command_line", OpContains, "-enc"),
				Field("process.command_line", OpContains, "downloadstring"),
			),
			Not(Field("user.name", OpEquals, "maintenance")),
		),
	}

	match := rule.Evaluate(Event{Fields: map[string]any{
		"event": map[string]any{
			"category": "process",
		},
		"process": map[string]any{
			"executable":   `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
			"command_line": "powershell.exe -NoProfile -enc SQBFAFgA",
		},
		"user": map[string]any{"name": "alice"},
	}})
	if !match.Matched || match.RuleID != rule.ID || match.Severity != "high" {
		t.Fatalf("match = %#v", match)
	}

	suppressed := rule.Evaluate(Event{Fields: map[string]any{
		"event.category":         "process",
		"process.executable":     `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"process.command_line":   "powershell.exe -enc SQBFAFgA",
		"user.name":              "maintenance",
		"event.provider":         "Microsoft-Windows-Sysmon",
		"control_one.parser_id":  "windows.sysmon",
		"control_one.source_ref": "fixture",
	}})
	if suppressed.Matched {
		t.Fatalf("suppressed match = %#v, want not matched", suppressed)
	}
}

func TestRuleEvaluateNumericPredicate(t *testing.T) {
	rule := Rule{
		ID:         "network.large_transfer",
		Title:      "Large Transfer",
		Expression: Field("network.bytes", OpGTE, 1000000),
	}
	if !rule.Evaluate(Event{Fields: map[string]any{"network.bytes": "1000000"}}).Matched {
		t.Fatal("expected numeric string to match gte threshold")
	}
	if rule.Evaluate(Event{Fields: map[string]any{"network.bytes": 999999}}).Matched {
		t.Fatal("expected value below threshold not to match")
	}
}

func TestRuleEvaluateRiskScore(t *testing.T) {
	explicit := Rule{
		ID:         "explicit.risk",
		Title:      "Explicit Risk",
		Severity:   "low",
		RiskScore:  72,
		Expression: Field("event.kind", OpEquals, "event"),
	}
	if match := explicit.Evaluate(Event{Fields: map[string]any{"event.kind": "event"}}); !match.Matched || match.RiskScore != 72 {
		t.Fatalf("explicit match = %#v, want risk score 72", match)
	}
	derived := Rule{
		ID:         "derived.risk",
		Title:      "Derived Risk",
		Severity:   "high",
		Expression: Field("event.kind", OpEquals, "event"),
	}
	if match := derived.Evaluate(Event{Fields: map[string]any{"event.kind": "event"}}); !match.Matched || match.RiskScore != 80 {
		t.Fatalf("derived match = %#v, want high severity risk score 80", match)
	}
}

func TestStatefulEvaluatorThresholdWindowAndSuppression(t *testing.T) {
	rule := TemporalRule{
		Rule: Rule{
			ID:         "auth.failed_login_burst",
			Title:      "Failed Login Burst",
			Expression: Field("event.action", OpEquals, "logon_failure"),
		},
		Temporal: Temporal{
			Kind:               TemporalKindThreshold,
			WindowSeconds:      60,
			Threshold:          3,
			GroupBy:            []string{"source.ip", "user.name"},
			SuppressForSeconds: 120,
		},
	}
	evaluator := NewStatefulEvaluator()
	base := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	event := Event{Fields: map[string]any{
		"event.action": "logon_failure",
		"source.ip":    "10.10.1.25",
		"user.name":    "alice",
	}}
	if match := evaluator.EvaluateAt(rule, event, base); match.Matched || match.Count != 1 {
		t.Fatalf("first match = %#v, want threshold not reached count 1", match)
	}
	if match := evaluator.EvaluateAt(rule, event, base.Add(10*time.Second)); match.Matched || match.Count != 2 {
		t.Fatalf("second match = %#v, want threshold not reached count 2", match)
	}
	match := evaluator.EvaluateAt(rule, event, base.Add(20*time.Second))
	if !match.Matched || match.Count != 3 || match.Threshold != 3 || match.WindowSeconds != 60 {
		t.Fatalf("third match = %#v, want threshold fire", match)
	}
	suppressed := evaluator.EvaluateAt(rule, event, base.Add(30*time.Second))
	if suppressed.Matched || !suppressed.Suppressed {
		t.Fatalf("suppressed match = %#v, want suppressed", suppressed)
	}
	afterSuppress := evaluator.EvaluateAt(rule, event, base.Add(3*time.Minute))
	if afterSuppress.Matched || afterSuppress.Count != 1 {
		t.Fatalf("after suppression = %#v, want new window count 1", afterSuppress)
	}
}

func TestStatefulEvaluatorSequenceWindowAndSuppression(t *testing.T) {
	rule := TemporalRule{
		Scope: "tenant-a",
		Rule: Rule{
			ID:         "iam.privilege_after_login",
			Title:      "Privilege Change After Login",
			Expression: Field("event.kind", OpEquals, "event"),
		},
		Temporal: Temporal{
			Kind:               TemporalKindSequence,
			WindowSeconds:      120,
			GroupBy:            []string{"user.name"},
			SuppressForSeconds: 300,
			Sequence: []TemporalStep{
				{Field: "event.action", Op: OpEquals, Values: []any{"login_success"}},
				{Field: "event.action", Op: OpContains, Values: []any{"admin_grant"}},
			},
		},
	}
	evaluator := NewStatefulEvaluator()
	base := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	login := Event{Fields: map[string]any{
		"event.kind":   "event",
		"event.action": "login_success",
		"user.name":    "alice",
	}}
	grant := Event{Fields: map[string]any{
		"event.kind":   "event",
		"event.action": "admin_grant.role",
		"user.name":    "alice",
	}}
	if match := evaluator.EvaluateAt(rule, login, base); match.Matched || match.Count != 1 || match.Threshold != 2 {
		t.Fatalf("login match = %#v, want sequence started", match)
	}
	match := evaluator.EvaluateAt(rule, grant, base.Add(30*time.Second))
	if !match.Matched || match.Count != 2 || match.GroupKey != "user.name=alice" {
		t.Fatalf("grant match = %#v, want sequence fire", match)
	}
	suppressed := evaluator.EvaluateAt(rule, login, base.Add(60*time.Second))
	if suppressed.Matched || !suppressed.Suppressed {
		t.Fatalf("suppressed match = %#v, want sequence suppression", suppressed)
	}

	lateEvaluator := NewStatefulEvaluator()
	if match := lateEvaluator.EvaluateAt(rule, login, base); match.Matched {
		t.Fatalf("late login match = %#v, want start only", match)
	}
	late := lateEvaluator.EvaluateAt(rule, grant, base.Add(3*time.Minute))
	if late.Matched || late.Count != 0 {
		t.Fatalf("late grant = %#v, want expired sequence", late)
	}
}

func TestStatefulEvaluatorJoinWindowAndSuppression(t *testing.T) {
	rule := TemporalRule{
		Scope: "tenant-a",
		Rule: Rule{
			ID:         "iam.suspicious_admin_join",
			Title:      "Suspicious Admin Join",
			Expression: Field("event.kind", OpEquals, "event"),
		},
		Temporal: Temporal{
			Kind:               TemporalKindJoin,
			WindowSeconds:      120,
			GroupBy:            []string{"user.name"},
			SuppressForSeconds: 300,
			Join: []TemporalStep{
				{Field: "event.action", Op: OpEquals, Values: []any{"mfa_reset"}},
				{Field: "event.action", Op: OpContains, Values: []any{"admin_grant"}},
			},
		},
	}
	evaluator := NewStatefulEvaluator()
	base := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	grant := Event{Fields: map[string]any{
		"event.kind":   "event",
		"event.action": "admin_grant.role",
		"user.name":    "alice",
	}}
	reset := Event{Fields: map[string]any{
		"event.kind":   "event",
		"event.action": "mfa_reset",
		"user.name":    "alice",
	}}
	if match := evaluator.EvaluateAt(rule, grant, base); match.Matched || match.Count != 1 || match.Threshold != 2 {
		t.Fatalf("grant match = %#v, want join started", match)
	}
	match := evaluator.EvaluateAt(rule, reset, base.Add(30*time.Second))
	if !match.Matched || match.Count != 2 || match.GroupKey != "user.name=alice" {
		t.Fatalf("reset match = %#v, want join fire", match)
	}
	suppressed := evaluator.EvaluateAt(rule, grant, base.Add(60*time.Second))
	if suppressed.Matched || !suppressed.Suppressed {
		t.Fatalf("suppressed match = %#v, want join suppression", suppressed)
	}

	lateEvaluator := NewStatefulEvaluator()
	if match := lateEvaluator.EvaluateAt(rule, grant, base); match.Matched {
		t.Fatalf("late grant match = %#v, want start only", match)
	}
	late := lateEvaluator.EvaluateAt(rule, reset, base.Add(3*time.Minute))
	if late.Matched || late.Count != 1 {
		t.Fatalf("late reset = %#v, want expired join restarted with one step", late)
	}
}

func TestImportSigmaFieldSelectionsAndCondition(t *testing.T) {
	raw := []byte(`
title: Suspicious PowerShell Encoded Command
id: 11111111-1111-4111-8111-111111111111
status: test
description: Encoded PowerShell command line outside approved maintenance.
tags:
  - attack.execution
  - attack.t1059.001
logsource:
  product: windows
  category: process_creation
detection:
  selection:
    Image|endswith:
      - '\powershell.exe'
      - '\pwsh.exe'
    CommandLine|contains: '-enc'
  filter:
    CommandLine|contains: 'approved-maintenance'
  condition: selection and not filter
level: high
`)
	rule, err := ImportSigma(raw, SigmaImportOptions{FieldMap: map[string]string{
		"Image":       "process.executable",
		"CommandLine": "process.command_line",
	}})
	if err != nil {
		t.Fatalf("ImportSigma() error = %v", err)
	}
	if rule.ID != "11111111-1111-4111-8111-111111111111" || rule.LogSource.Product != "windows" {
		t.Fatalf("rule = %#v", rule)
	}
	match := rule.Evaluate(Event{Fields: map[string]any{
		"process.executable":   `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"process.command_line": "powershell.exe -NoProfile -enc SQBFAFgA",
	}})
	if !match.Matched {
		t.Fatalf("match = %#v, want matched", match)
	}
	filtered := rule.Evaluate(Event{Fields: map[string]any{
		"process.executable":   `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"process.command_line": "powershell.exe -enc approved-maintenance",
	}})
	if filtered.Matched {
		t.Fatalf("filtered = %#v, want not matched", filtered)
	}
}

func TestImportSigmaKeywordSelectionSearchesRawEvent(t *testing.T) {
	raw := []byte(`
title: Shell History Clear
id: 22222222-2222-4222-8222-222222222222
detection:
  keywords:
    - 'history -c'
    - 'rm ~/.bash_history'
  condition: keywords
level: medium
`)
	rule, err := ImportSigma(raw, SigmaImportOptions{})
	if err != nil {
		t.Fatalf("ImportSigma() error = %v", err)
	}
	if !rule.Evaluate(Event{Raw: "alice ran history -c"}).Matched {
		t.Fatal("expected raw keyword match")
	}
	if rule.Evaluate(Event{Raw: "alice ran whoami"}).Matched {
		t.Fatal("did not expect unrelated raw event to match")
	}
}
