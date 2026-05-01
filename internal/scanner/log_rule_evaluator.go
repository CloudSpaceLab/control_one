package scanner

import (
	"regexp"
	"sync"
	"time"
)

// LogRule mirrors the control-plane log_monitoring_rules row.
type LogRule struct {
	ID            string
	Name          string
	Source        string
	Pattern       string
	Severity      string
	WindowSeconds int
	Threshold     int
	compiled      *regexp.Regexp
	compileErr    error
}

// LogTrigger is emitted when a rule's threshold is exceeded within the window.
type LogTrigger struct {
	RuleID   string
	Severity string
	Hits     int
	Window   time.Duration
	FiredAt  time.Time
	Evidence []string
}

// LogRuleEvaluator holds sliding-window counters per rule and returns triggers
// when thresholds are exceeded. It is safe for concurrent use.
type LogRuleEvaluator struct {
	mu       sync.Mutex
	rules    []LogRule
	hits     map[string][]time.Time
	evidence map[string][]string
	now      func() time.Time
}

// NewLogRuleEvaluator compiles the patterns up front. Rules with invalid
// patterns are skipped (compile error surfaced on the rule itself).
func NewLogRuleEvaluator(rules []LogRule) *LogRuleEvaluator {
	eval := &LogRuleEvaluator{
		hits:     make(map[string][]time.Time),
		evidence: make(map[string][]string),
		now:      func() time.Time { return time.Now() },
	}
	for i, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		rules[i].compiled = re
		rules[i].compileErr = err
	}
	eval.rules = rules
	return eval
}

// Evaluate processes one log line and returns any triggers the line caused.
// Call per line; cheap to invoke.
func (e *LogRuleEvaluator) Evaluate(source, line string) []LogTrigger {
	if line == "" {
		return nil
	}
	now := e.now()
	var triggers []LogTrigger

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.rules {
		if r.compileErr != nil || r.compiled == nil {
			continue
		}
		if r.Source != "" && r.Source != source {
			continue
		}
		if !r.compiled.MatchString(line) {
			continue
		}
		window := time.Duration(r.WindowSeconds) * time.Second
		if window <= 0 {
			window = 60 * time.Second
		}
		cutoff := now.Add(-window)

		hits := append(e.hits[r.ID], now)
		// Trim old
		trimmed := hits[:0]
		for _, t := range hits {
			if t.After(cutoff) {
				trimmed = append(trimmed, t)
			}
		}
		e.hits[r.ID] = trimmed

		evidence := append(e.evidence[r.ID], line)
		if len(evidence) > 5 {
			evidence = evidence[len(evidence)-5:]
		}
		e.evidence[r.ID] = evidence

		threshold := r.Threshold
		if threshold <= 0 {
			threshold = 1
		}
		if len(trimmed) >= threshold {
			triggers = append(triggers, LogTrigger{
				RuleID:   r.ID,
				Severity: r.Severity,
				Hits:     len(trimmed),
				Window:   window,
				FiredAt:  now,
				Evidence: append([]string(nil), evidence...),
			})
			// Reset after firing so we emit once per window.
			e.hits[r.ID] = nil
			e.evidence[r.ID] = nil
		}
	}
	return triggers
}
