package scanner

import (
	"testing"
	"time"
)

func TestLogRuleFiresAtThreshold(t *testing.T) {
	ev := NewLogRuleEvaluator([]LogRule{{
		ID:            "r1",
		Source:        "auth",
		Pattern:       `failed login`,
		Severity:      "high",
		WindowSeconds: 60,
		Threshold:     3,
	}})
	if trigs := ev.Evaluate("auth", "failed login attempt"); len(trigs) != 0 {
		t.Fatalf("should not fire yet, got %d", len(trigs))
	}
	if trigs := ev.Evaluate("auth", "failed login attempt"); len(trigs) != 0 {
		t.Fatalf("should not fire yet, got %d", len(trigs))
	}
	trigs := ev.Evaluate("auth", "failed login attempt")
	if len(trigs) != 1 {
		t.Fatalf("expected one trigger, got %d", len(trigs))
	}
	if trigs[0].Hits != 3 || len(trigs[0].Evidence) != 3 {
		t.Fatalf("unexpected trigger %+v", trigs[0])
	}
}

func TestLogRuleWindowExpires(t *testing.T) {
	base := time.Now()
	clock := base
	ev := NewLogRuleEvaluator([]LogRule{{
		ID: "r1", Source: "app", Pattern: "boom", WindowSeconds: 5, Threshold: 2,
	}})
	ev.now = func() time.Time { return clock }
	if trigs := ev.Evaluate("app", "boom!"); len(trigs) != 0 {
		t.Fatalf("expected no trigger yet")
	}
	clock = base.Add(10 * time.Second)
	if trigs := ev.Evaluate("app", "boom!"); len(trigs) != 0 {
		t.Fatalf("first hit outside window, still no trigger, got %d", len(trigs))
	}
}

func TestLogRuleSourceFilter(t *testing.T) {
	ev := NewLogRuleEvaluator([]LogRule{{ID: "r1", Source: "auth", Pattern: "x", WindowSeconds: 60, Threshold: 1}})
	if trigs := ev.Evaluate("other", "x"); len(trigs) != 0 {
		t.Fatal("source filter should reject")
	}
	if trigs := ev.Evaluate("auth", "x"); len(trigs) != 1 {
		t.Fatal("should fire")
	}
}
