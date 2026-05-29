package detections

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	OpEquals     = "equals"
	OpContains   = "contains"
	OpStartsWith = "starts_with"
	OpEndsWith   = "ends_with"
	OpExists     = "exists"
	OpIn         = "in"
	OpGT         = "gt"
	OpGTE        = "gte"
	OpLT         = "lt"
	OpLTE        = "lte"

	TemporalKindThreshold = "threshold"
	TemporalKindSequence  = "sequence"
	TemporalKindJoin      = "join"
)

type Rule struct {
	ID          string
	Title       string
	Description string
	Status      string
	Severity    string
	RiskScore   int
	Tags        []string
	LogSource   LogSource
	Expression  Expression
}

type LogSource struct {
	Product  string
	Service  string
	Category string
}

type Event struct {
	Raw       string
	Fields    map[string]any
	Timestamp time.Time
}

type Match struct {
	RuleID        string
	Title         string
	Severity      string
	RiskScore     int
	Matched       bool
	Reason        string
	LogSource     LogSource
	Tags          []string
	Count         int
	Threshold     int
	WindowSeconds int
	GroupKey      string
	Suppressed    bool
}

type Expression struct {
	All       []Expression
	Any       []Expression
	Not       *Expression
	Predicate *Predicate
}

type Predicate struct {
	Field         string
	Op            string
	Values        []any
	CaseSensitive bool
}

type Temporal struct {
	Kind               string
	WindowSeconds      int
	Threshold          int
	GroupBy            []string
	SuppressForSeconds int
	Sequence           []TemporalStep
	Join               []TemporalStep
}

type TemporalStep struct {
	Field         string
	Op            string
	Values        []any
	CaseSensitive bool
}

type TemporalRule struct {
	Rule     Rule
	Temporal Temporal
	Scope    string
}

type StatefulEvaluator struct {
	mu         sync.Mutex
	windows    map[string][]time.Time
	sequences  map[string]sequenceState
	joins      map[string]joinState
	suppressed map[string]time.Time
	Now        func() time.Time
}

type sequenceState struct {
	Index     int
	StartedAt time.Time
	LastAt    time.Time
}

type joinState struct {
	Seen      []bool
	StartedAt time.Time
	LastAt    time.Time
}

func (r Rule) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("rule id is required")
	}
	if strings.TrimSpace(r.Title) == "" {
		return errors.New("rule title is required")
	}
	if !r.Expression.valid() {
		return errors.New("rule expression is required")
	}
	return nil
}

func (r Rule) Evaluate(event Event) Match {
	matched := false
	if err := r.Validate(); err == nil {
		matched = r.Expression.Evaluate(event)
	}
	reason := "not matched"
	if matched {
		reason = "matched"
	}
	return Match{
		RuleID:    strings.TrimSpace(r.ID),
		Title:     strings.TrimSpace(r.Title),
		Severity:  strings.TrimSpace(r.Severity),
		RiskScore: RiskScoreForSeverity(r.RiskScore, r.Severity),
		Matched:   matched,
		Reason:    reason,
		LogSource: r.LogSource,
		Tags:      append([]string(nil), r.Tags...),
	}
}

func RiskScoreForSeverity(score int, severity string) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	if score > 0 {
		return score
	}
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 95
	case "high":
		return 80
	case "medium":
		return 55
	case "low":
		return 25
	case "informational", "info":
		return 10
	default:
		return 0
	}
}

func NewStatefulEvaluator() *StatefulEvaluator {
	return &StatefulEvaluator{
		windows:    map[string][]time.Time{},
		sequences:  map[string]sequenceState{},
		joins:      map[string]joinState{},
		suppressed: map[string]time.Time{},
	}
}

func (e *StatefulEvaluator) Evaluate(rule TemporalRule, event Event) Match {
	at := event.Timestamp
	if at.IsZero() {
		if e != nil && e.Now != nil {
			at = e.Now()
		} else {
			at = time.Now().UTC()
		}
	}
	return e.EvaluateAt(rule, event, at)
}

func (e *StatefulEvaluator) EvaluateAt(rule TemporalRule, event Event, at time.Time) Match {
	base := rule.Rule.Evaluate(event)
	if !base.Matched {
		return base
	}
	temporal := rule.Temporal
	if !temporal.Enabled() {
		return base
	}
	if err := temporal.Validate(); err != nil {
		base.Matched = false
		base.Reason = err.Error()
		return base
	}
	if e == nil {
		e = NewStatefulEvaluator()
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	switch strings.ToLower(strings.TrimSpace(temporal.Kind)) {
	case TemporalKindThreshold:
		return e.evaluateThreshold(base, rule, event, at)
	case TemporalKindSequence:
		return e.evaluateSequence(base, rule, event, at)
	case TemporalKindJoin:
		return e.evaluateJoin(base, rule, event, at)
	default:
		base.Matched = false
		base.Reason = fmt.Sprintf("unsupported temporal kind %q", strings.TrimSpace(temporal.Kind))
		return base
	}
}

func (e *StatefulEvaluator) evaluateThreshold(base Match, rule TemporalRule, event Event, at time.Time) Match {
	temporal := rule.Temporal
	groupKey, ok := temporalGroupKey(event, temporal.GroupBy)
	if !ok {
		base.Matched = false
		base.Reason = "missing temporal group field"
		return base
	}
	key := strings.TrimSpace(rule.Scope) + "\x00" + strings.TrimSpace(rule.Rule.ID) + "\x00" + groupKey
	cutoff := at.Add(-time.Duration(temporal.WindowSeconds) * time.Second)

	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureStateLocked()
	if until, ok := e.suppressed[key]; ok && until.After(at) {
		base.Matched = false
		base.Reason = "suppressed"
		base.Suppressed = true
		base.GroupKey = groupKey
		base.Threshold = temporal.Threshold
		base.WindowSeconds = temporal.WindowSeconds
		return base
	}
	hits := append(e.windows[key], at)
	trimmed := hits[:0]
	for _, hit := range hits {
		if !hit.Before(cutoff) {
			trimmed = append(trimmed, hit)
		}
	}
	e.windows[key] = trimmed
	base.Count = len(trimmed)
	base.Threshold = temporal.Threshold
	base.WindowSeconds = temporal.WindowSeconds
	base.GroupKey = groupKey
	if len(trimmed) < temporal.Threshold {
		base.Matched = false
		base.Reason = "threshold not reached"
		return base
	}
	base.Reason = "threshold matched"
	e.windows[key] = nil
	if temporal.SuppressForSeconds > 0 {
		e.suppressed[key] = at.Add(time.Duration(temporal.SuppressForSeconds) * time.Second)
	}
	return base
}

func (e *StatefulEvaluator) evaluateSequence(base Match, rule TemporalRule, event Event, at time.Time) Match {
	temporal := rule.Temporal
	groupKey, ok := temporalGroupKey(event, temporal.GroupBy)
	if !ok {
		base.Matched = false
		base.Reason = "missing temporal group field"
		return base
	}
	key := strings.TrimSpace(rule.Scope) + "\x00" + strings.TrimSpace(rule.Rule.ID) + "\x00" + groupKey
	cutoff := at.Add(-time.Duration(temporal.WindowSeconds) * time.Second)

	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureStateLocked()
	base.Threshold = len(temporal.Sequence)
	base.WindowSeconds = temporal.WindowSeconds
	base.GroupKey = groupKey
	if until, ok := e.suppressed[key]; ok && until.After(at) {
		base.Matched = false
		base.Reason = "suppressed"
		base.Suppressed = true
		return base
	}
	state := e.sequences[key]
	if state.Index < 0 || state.Index >= len(temporal.Sequence) || (!state.StartedAt.IsZero() && state.StartedAt.Before(cutoff)) {
		state = sequenceState{}
	}
	expected := state.Index
	if expected >= len(temporal.Sequence) {
		expected = 0
	}
	if !temporalStepMatches(event, temporal.Sequence[expected]) {
		if expected > 0 && temporalStepMatches(event, temporal.Sequence[0]) {
			state = sequenceState{Index: 1, StartedAt: at, LastAt: at}
			e.sequences[key] = state
			base.Count = state.Index
			base.Matched = false
			base.Reason = "sequence restarted"
			return base
		}
		if state.Index > 0 {
			e.sequences[key] = state
		}
		base.Count = state.Index
		base.Matched = false
		base.Reason = "sequence not advanced"
		return base
	}
	if state.Index == 0 {
		state.StartedAt = at
	}
	state.Index++
	state.LastAt = at
	base.Count = state.Index
	if state.Index < len(temporal.Sequence) {
		e.sequences[key] = state
		base.Matched = false
		base.Reason = "sequence not complete"
		return base
	}
	delete(e.sequences, key)
	base.Reason = "sequence matched"
	if temporal.SuppressForSeconds > 0 {
		e.suppressed[key] = at.Add(time.Duration(temporal.SuppressForSeconds) * time.Second)
	}
	return base
}

func (e *StatefulEvaluator) evaluateJoin(base Match, rule TemporalRule, event Event, at time.Time) Match {
	temporal := rule.Temporal
	groupKey, ok := temporalGroupKey(event, temporal.GroupBy)
	if !ok {
		base.Matched = false
		base.Reason = "missing temporal group field"
		return base
	}
	key := strings.TrimSpace(rule.Scope) + "\x00" + strings.TrimSpace(rule.Rule.ID) + "\x00" + groupKey
	cutoff := at.Add(-time.Duration(temporal.WindowSeconds) * time.Second)

	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureStateLocked()
	base.Threshold = len(temporal.Join)
	base.WindowSeconds = temporal.WindowSeconds
	base.GroupKey = groupKey
	if until, ok := e.suppressed[key]; ok && until.After(at) {
		base.Matched = false
		base.Reason = "suppressed"
		base.Suppressed = true
		return base
	}
	state := e.joins[key]
	if len(state.Seen) != len(temporal.Join) || (!state.StartedAt.IsZero() && state.StartedAt.Before(cutoff)) {
		state = joinState{Seen: make([]bool, len(temporal.Join))}
	}
	matchedStep := false
	for i, step := range temporal.Join {
		if state.Seen[i] {
			continue
		}
		if temporalStepMatches(event, step) {
			state.Seen[i] = true
			matchedStep = true
			if state.StartedAt.IsZero() {
				state.StartedAt = at
			}
			state.LastAt = at
		}
	}
	base.Count = countJoinSeen(state.Seen)
	if base.Count < len(temporal.Join) {
		if base.Count > 0 {
			e.joins[key] = state
		}
		base.Matched = false
		if matchedStep {
			base.Reason = "join not complete"
		} else {
			base.Reason = "join not advanced"
		}
		return base
	}
	delete(e.joins, key)
	base.Reason = "join matched"
	if temporal.SuppressForSeconds > 0 {
		e.suppressed[key] = at.Add(time.Duration(temporal.SuppressForSeconds) * time.Second)
	}
	return base
}

func (e *StatefulEvaluator) ensureStateLocked() {
	if e.windows == nil {
		e.windows = map[string][]time.Time{}
	}
	if e.sequences == nil {
		e.sequences = map[string]sequenceState{}
	}
	if e.joins == nil {
		e.joins = map[string]joinState{}
	}
	if e.suppressed == nil {
		e.suppressed = map[string]time.Time{}
	}
}

func (t Temporal) Enabled() bool {
	return strings.TrimSpace(t.Kind) != ""
}

func (t Temporal) Validate() error {
	switch strings.ToLower(strings.TrimSpace(t.Kind)) {
	case "":
		return nil
	case TemporalKindThreshold:
		if t.WindowSeconds <= 0 {
			return errors.New("temporal threshold window_seconds must be positive")
		}
		if t.Threshold <= 0 {
			return errors.New("temporal threshold must be positive")
		}
		if t.SuppressForSeconds < 0 {
			return errors.New("temporal suppress_for_seconds must be non-negative")
		}
		for _, field := range t.GroupBy {
			if strings.TrimSpace(field) == "" {
				return errors.New("temporal group_by fields must be non-empty")
			}
		}
		return nil
	case TemporalKindSequence:
		if t.WindowSeconds <= 0 {
			return errors.New("temporal sequence window_seconds must be positive")
		}
		if t.SuppressForSeconds < 0 {
			return errors.New("temporal suppress_for_seconds must be non-negative")
		}
		if len(t.Sequence) < 2 {
			return errors.New("temporal sequence requires at least two steps")
		}
		if err := validateTemporalSteps("sequence", t.Sequence); err != nil {
			return err
		}
		for _, field := range t.GroupBy {
			if strings.TrimSpace(field) == "" {
				return errors.New("temporal group_by fields must be non-empty")
			}
		}
		return nil
	case TemporalKindJoin:
		if t.WindowSeconds <= 0 {
			return errors.New("temporal join window_seconds must be positive")
		}
		if t.SuppressForSeconds < 0 {
			return errors.New("temporal suppress_for_seconds must be non-negative")
		}
		if len(t.Join) < 2 {
			return errors.New("temporal join requires at least two steps")
		}
		if err := validateTemporalSteps("join", t.Join); err != nil {
			return err
		}
		for _, field := range t.GroupBy {
			if strings.TrimSpace(field) == "" {
				return errors.New("temporal group_by fields must be non-empty")
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported temporal kind %q", strings.TrimSpace(t.Kind))
	}
}

func validateTemporalSteps(kind string, steps []TemporalStep) error {
	for i, step := range steps {
		if strings.TrimSpace(step.Field) == "" {
			return fmt.Errorf("temporal %s step %d field is required", kind, i)
		}
		op := strings.TrimSpace(step.Op)
		if op == "" {
			op = OpEquals
		}
		if !validPredicateOp(op) {
			return fmt.Errorf("temporal %s step %d op %q is unsupported", kind, i, step.Op)
		}
		if op != OpExists && len(step.Values) == 0 {
			return fmt.Errorf("temporal %s step %d values are required", kind, i)
		}
	}
	return nil
}

func validPredicateOp(op string) bool {
	switch strings.TrimSpace(op) {
	case OpEquals, OpContains, OpStartsWith, OpEndsWith, OpExists, OpIn, OpGT, OpGTE, OpLT, OpLTE:
		return true
	default:
		return false
	}
}

func (e Expression) Evaluate(event Event) bool {
	if e.Predicate != nil {
		return e.Predicate.Evaluate(event)
	}
	if e.Not != nil {
		return !e.Not.Evaluate(event)
	}
	if len(e.All) > 0 {
		for _, child := range e.All {
			if !child.Evaluate(event) {
				return false
			}
		}
		return true
	}
	if len(e.Any) > 0 {
		for _, child := range e.Any {
			if child.Evaluate(event) {
				return true
			}
		}
		return false
	}
	return false
}

func (e Expression) valid() bool {
	count := 0
	if e.Predicate != nil {
		count++
	}
	if e.Not != nil {
		count++
	}
	if len(e.All) > 0 {
		count++
	}
	if len(e.Any) > 0 {
		count++
	}
	return count == 1
}

func (p Predicate) Evaluate(event Event) bool {
	op := strings.TrimSpace(p.Op)
	if op == "" {
		op = OpEquals
	}
	if op == OpExists {
		_, ok := eventValue(event, p.Field)
		return ok
	}
	value, ok := eventValue(event, p.Field)
	if !ok {
		return false
	}
	switch op {
	case OpEquals:
		return anyValueMatches(value, p.Values, p.CaseSensitive, func(got, want string) bool { return got == want })
	case OpContains:
		return anyValueMatches(value, p.Values, p.CaseSensitive, strings.Contains)
	case OpStartsWith:
		return anyValueMatches(value, p.Values, p.CaseSensitive, strings.HasPrefix)
	case OpEndsWith:
		return anyValueMatches(value, p.Values, p.CaseSensitive, strings.HasSuffix)
	case OpIn:
		return anyValueMatches(value, p.Values, p.CaseSensitive, func(got, want string) bool { return got == want })
	case OpGT, OpGTE, OpLT, OpLTE:
		return compareNumeric(value, p.Values, op)
	default:
		return false
	}
}

func Field(field, op string, values ...any) Expression {
	return Expression{Predicate: &Predicate{Field: field, Op: op, Values: values}}
}

func All(children ...Expression) Expression {
	return Expression{All: children}
}

func Any(children ...Expression) Expression {
	return Expression{Any: children}
}

func Not(child Expression) Expression {
	return Expression{Not: &child}
}

func temporalGroupKey(event Event, fields []string) (string, bool) {
	if len(fields) == 0 {
		return "__global__", true
	}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		value, ok := eventValue(event, field)
		if !ok {
			return "", false
		}
		parts = append(parts, field+"="+strings.Join(flattenStrings(value), ","))
	}
	return strings.Join(parts, "|"), true
}

func temporalStepMatches(event Event, step TemporalStep) bool {
	op := strings.TrimSpace(step.Op)
	if op == "" {
		op = OpEquals
	}
	return Predicate{
		Field:         strings.TrimSpace(step.Field),
		Op:            op,
		Values:        append([]any(nil), step.Values...),
		CaseSensitive: step.CaseSensitive,
	}.Evaluate(event)
}

func countJoinSeen(seen []bool) int {
	count := 0
	for _, value := range seen {
		if value {
			count++
		}
	}
	return count
}

func eventValue(event Event, field string) (any, bool) {
	field = strings.TrimSpace(field)
	if field == "" || field == "__raw__" {
		if strings.TrimSpace(event.Raw) != "" {
			return event.Raw, true
		}
		return event.Fields["message"], event.Fields["message"] != nil
	}
	if value, ok := event.Fields[field]; ok {
		return value, true
	}
	parts := strings.Split(field, ".")
	var current any = event.Fields
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := obj[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

func anyValueMatches(value any, wants []any, caseSensitive bool, match func(string, string) bool) bool {
	gotValues := flattenStrings(value)
	wantValues := flattenStrings(wants)
	if len(wantValues) == 0 {
		wantValues = []string{""}
	}
	for _, got := range gotValues {
		for _, want := range wantValues {
			left, right := got, want
			if !caseSensitive {
				left = strings.ToLower(left)
				right = strings.ToLower(right)
			}
			if match(left, right) {
				return true
			}
		}
	}
	return false
}

func flattenStrings(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, flattenStrings(item)...)
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(item))
		}
		return out
	default:
		return []string{strings.TrimSpace(fmt.Sprint(value))}
	}
}

func compareNumeric(value any, wants []any, op string) bool {
	got, ok := numberValue(value)
	if !ok {
		return false
	}
	for _, want := range wants {
		threshold, ok := numberValue(want)
		if !ok {
			continue
		}
		switch op {
		case OpGT:
			if got > threshold {
				return true
			}
		case OpGTE:
			if got >= threshold {
				return true
			}
		case OpLT:
			if got < threshold {
				return true
			}
		case OpLTE:
			if got <= threshold {
				return true
			}
		}
	}
	return false
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case json.Number:
		f, err := typed.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
