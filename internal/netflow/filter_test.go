package netflow

import (
	"net"
	"testing"
	"time"
)

func TestFilterEmitsExternal(t *testing.T) {
	f := NewFilter(DefaultFilterConfig())
	ev := ConnectionEvent{SrcIP: net.ParseIP("10.0.0.5"), DstIP: net.ParseIP("203.0.113.10"), DstPort: 443}
	if got := f.Decide(&ev); got != FilterEmit {
		t.Fatalf("external should emit; got %v", got)
	}
}

func TestFilterSummariesInternal(t *testing.T) {
	f := NewFilter(DefaultFilterConfig())
	ev := ConnectionEvent{
		SrcIP:     net.ParseIP("10.0.0.5"),
		DstIP:     net.ParseIP("10.0.0.7"),
		DstPort:   5432,
		PID:       42,
		StartedAt: time.Now().UTC(),
	}
	if got := f.Decide(&ev); got != FilterSummary {
		t.Fatalf("internal should summarise; got %v", got)
	}
}

func TestFilterDropsAllowlisted(t *testing.T) {
	_, n, _ := net.ParseCIDR("10.0.0.0/8")
	cfg := DefaultFilterConfig()
	cfg.AllowlistCIDRs = []*net.IPNet{n}
	f := NewFilter(cfg)
	ev := ConnectionEvent{SrcIP: net.ParseIP("203.0.113.10"), DstIP: net.ParseIP("10.0.0.50"), DstPort: 443}
	if got := f.Decide(&ev); got != FilterDrop {
		t.Fatalf("allowlist should drop; got %v", got)
	}
}

func TestFilterAlwaysCapturesThreat(t *testing.T) {
	cfg := DefaultFilterConfig()
	f := NewFilter(cfg)
	ev := ConnectionEvent{SrcIP: net.ParseIP("10.0.0.5"), DstIP: net.ParseIP("10.0.0.7"), DstPort: 5432, ThreatMatch: true, ThreatFeed: "test"}
	if got := f.Decide(&ev); got != FilterEmit {
		t.Fatalf("threat hit should emit; got %v", got)
	}
}

func TestFilterAlwaysCapturesListening(t *testing.T) {
	f := NewFilter(DefaultFilterConfig())
	ev := ConnectionEvent{State: "LISTEN", SrcIP: net.ParseIP("0.0.0.0"), DstIP: nil}
	if got := f.Decide(&ev); got != FilterEmit {
		t.Fatalf("LISTEN should emit; got %v", got)
	}
}
