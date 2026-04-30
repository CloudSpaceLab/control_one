package autoblock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/firewall"
)

type fakeBackend struct {
	mu      sync.Mutex
	applied []firewall.Rule
}

func (f *fakeBackend) Name() string    { return "fake" }
func (f *fakeBackend) Available() bool { return true }
func (f *fakeBackend) Apply(_ context.Context, r firewall.Rule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, r)
	return nil
}
func (f *fakeBackend) Remove(_ context.Context, _ firewall.Rule) error           { return nil }
func (f *fakeBackend) List(_ context.Context, _ string) ([]firewall.Rule, error) { return nil, nil }

func newPipeline(t *testing.T, p Policy) (*Pipeline, *fakeBackend) {
	t.Helper()
	fb := &fakeBackend{}
	mgr := (&firewall.Manager{}).WithBackends(fb)
	if err := mgr.Detect(); err != nil {
		t.Fatal(err)
	}
	return New(p, mgr, nil), fb
}

func TestBlocksAboveThreshold(t *testing.T) {
	p, fb := newPipeline(t, Policy{MinScore: 75})
	out := p.Process(context.Background(), Candidate{IP: "1.2.3.4", Score: 90, Source: "feed"})
	if out.Decision != "blocked" {
		t.Fatalf("want blocked, got %s (%s)", out.Decision, out.Reason)
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if len(fb.applied) != 1 {
		t.Fatalf("want 1 rule, got %d", len(fb.applied))
	}
}

func TestSkipsBelowThreshold(t *testing.T) {
	p, _ := newPipeline(t, Policy{MinScore: 90})
	out := p.Process(context.Background(), Candidate{IP: "1.2.3.4", Score: 50, Source: "feed"})
	if out.Decision != "skipped" {
		t.Fatalf("want skipped, got %s", out.Decision)
	}
}

func TestAllowlistOverrides(t *testing.T) {
	p, _ := newPipeline(t, Policy{MinScore: 50, Allowlist: []string{"10.0.0.0/8"}})
	out := p.Process(context.Background(), Candidate{IP: "10.5.5.5", Score: 99, Source: "feed"})
	if out.Decision != "skipped" || out.Reason != "allowlisted" {
		t.Fatalf("expected allowlist suppression, got %+v", out)
	}
}

func TestCooldownDebounces(t *testing.T) {
	p, fb := newPipeline(t, Policy{MinScore: 50, CooldownSeconds: 60})
	c := Candidate{IP: "9.9.9.9", Score: 80, Source: "feed"}
	if out := p.Process(context.Background(), c); out.Decision != "blocked" {
		t.Fatalf("first call should block: %s", out.Decision)
	}
	if out := p.Process(context.Background(), c); out.Decision != "suppressed" {
		t.Fatalf("second call should suppress: %s", out.Decision)
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if len(fb.applied) != 1 {
		t.Fatalf("only 1 rule should hit backend, got %d", len(fb.applied))
	}
}

func TestRateLimit(t *testing.T) {
	p, _ := newPipeline(t, Policy{MinScore: 50, MaxBlocksPerHour: 2})
	for i, ip := range []string{"1.0.0.1", "1.0.0.2", "1.0.0.3"} {
		out := p.Process(context.Background(), Candidate{IP: ip, Score: 80, Source: "feed"})
		if i < 2 && out.Decision != "blocked" {
			t.Fatalf("request %d should block, got %s", i, out.Decision)
		}
		if i == 2 && out.Decision != "rate-limited" {
			t.Fatalf("request 3 should rate-limit, got %s", out.Decision)
		}
	}
}

func TestAuditCaptured(t *testing.T) {
	p, _ := newPipeline(t, Policy{MinScore: 50})
	p.Process(context.Background(), Candidate{IP: "1.1.1.1", Score: 90, Source: "feed"})
	if len(p.Audit()) != 1 {
		t.Fatal("audit should record decision")
	}
}

func TestAuditTrimsAt1000(t *testing.T) {
	p, _ := newPipeline(t, Policy{MinScore: 0})
	for i := 0; i < 1100; i++ {
		p.Process(context.Background(), Candidate{IP: "0.0.0.0", Score: 100, Source: "feed", Observed: time.Now()})
	}
	if len(p.Audit()) > 1000 {
		t.Fatalf("audit not trimmed: %d", len(p.Audit()))
	}
}
