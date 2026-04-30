package firewall

import (
	"context"
	"sync"
	"testing"
)

type fakeBackend struct {
	name      string
	available bool
	mu        sync.Mutex
	applied   []Rule
	removed   []Rule
}

func (f *fakeBackend) Name() string    { return f.name }
func (f *fakeBackend) Available() bool { return f.available }
func (f *fakeBackend) Apply(_ context.Context, r Rule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, r)
	return nil
}
func (f *fakeBackend) Remove(_ context.Context, r Rule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, r)
	return nil
}
func (f *fakeBackend) List(_ context.Context, _ string) ([]Rule, error) { return nil, nil }

func TestDetectPicksFirstAvailable(t *testing.T) {
	a := &fakeBackend{name: "a", available: false}
	b := &fakeBackend{name: "b", available: true}
	c := &fakeBackend{name: "c", available: true}
	m := (&Manager{}).WithBackends(a, b, c)
	if err := m.Detect(); err != nil {
		t.Fatal(err)
	}
	if m.Backend().Name() != "b" {
		t.Fatalf("want b, got %s", m.Backend().Name())
	}
}

func TestDetectErrorsWhenNoneAvailable(t *testing.T) {
	m := (&Manager{}).WithBackends(&fakeBackend{name: "a"})
	if err := m.Detect(); err != ErrNoBackend {
		t.Fatalf("want ErrNoBackend, got %v", err)
	}
}

func TestApplyValidatesShape(t *testing.T) {
	b := &fakeBackend{available: true}
	m := (&Manager{}).WithBackends(b)
	_ = m.Detect()
	if err := m.Apply(context.Background(), Rule{Action: "wrong", Source: "1.2.3.4"}); err == nil {
		t.Fatal("invalid action should error")
	}
	if err := m.Apply(context.Background(), Rule{Action: ActionBlock}); err == nil {
		t.Fatal("rule with no scope should error")
	}
	if err := m.Apply(context.Background(), Rule{Action: ActionBlock, Source: "1.2.3.4"}); err != nil {
		t.Fatalf("valid rule should apply: %v", err)
	}
	if len(b.applied) != 1 {
		t.Fatalf("backend should receive 1 rule, got %d", len(b.applied))
	}
}

func TestUFWArgs(t *testing.T) {
	r := Rule{Action: ActionBlock, Direction: DirectionIn, Source: "1.2.3.4", Port: 22, Protocol: "tcp", Tag: "controlone:ti"}
	args := buildUFWArgs(r)
	if args[0] != "block" {
		t.Fatalf("first arg should be block, got %s", args[0])
	}
	hasPort := false
	for i, a := range args {
		if a == "port" && i+1 < len(args) && args[i+1] == "22" {
			hasPort = true
		}
	}
	if !hasPort {
		t.Fatalf("expected port 22 in args: %v", args)
	}
}

func TestNftRule(t *testing.T) {
	r := Rule{Action: ActionBlock, Source: "1.2.3.0/24", Port: 22, Protocol: "tcp"}
	expr := buildNftRule(r)
	if expr == "" {
		t.Fatal("nft rule must be non-empty")
	}
	for _, want := range []string{"saddr", "1.2.3.0/24", "tcp", "dport", "22", "drop"} {
		if !contains(expr, want) {
			t.Fatalf("nft rule missing %q: %s", want, expr)
		}
	}
}

func contains(s, want string) bool {
	return len(s) >= len(want) && (s == want || (len(s) > 0 && (indexOf(s, want) >= 0)))
}

func indexOf(s, want string) int {
	for i := 0; i+len(want) <= len(s); i++ {
		if s[i:i+len(want)] == want {
			return i
		}
	}
	return -1
}
