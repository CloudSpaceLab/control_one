package netflow

import (
	"net"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/eventstream"
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

func TestFilterDrainEmitsAndRemovesOldSummaryBuckets(t *testing.T) {
	f := NewFilter(DefaultFilterConfig())
	ev := ConnectionEvent{
		SrcIP:      net.ParseIP("10.0.0.5"),
		DstIP:      net.ParseIP("10.0.0.7"),
		DstPort:    5432,
		PID:        42,
		Process:    "postgres",
		Protocol:   "tcp",
		BytesIn:    10,
		BytesOut:   20,
		StartedAt:  time.Now().UTC().Add(-3 * time.Minute),
		LastDataAt: time.Now().UTC().Add(-3 * time.Minute),
	}
	if got := f.Decide(&ev); got != FilterSummary {
		t.Fatalf("internal should summarise; got %v", got)
	}
	drained := f.Drain(time.Minute)
	if len(drained) != 1 {
		t.Fatalf("drained summaries = %d, want 1", len(drained))
	}
	if drained[0].Kind != "summary" || drained[0].BytesIn != 10 || drained[0].BytesOut != 20 {
		t.Fatalf("unexpected summary event: %#v", drained[0])
	}
	if stats := f.Stats(); stats.SummaryBuckets != 0 {
		t.Fatalf("summary buckets after drain = %d", stats.SummaryBuckets)
	}
}

func TestFilterSummaryBucketsAreBounded(t *testing.T) {
	f := NewFilterWithLimit(DefaultFilterConfig(), 2)
	for i := 0; i < 3; i++ {
		ev := ConnectionEvent{
			SrcIP:     net.ParseIP("10.0.0.5"),
			DstIP:     net.ParseIP("10.0.0.7"),
			DstPort:   uint16(5000 + i),
			PID:       i,
			Protocol:  "tcp",
			StartedAt: time.Now().UTC().Add(time.Duration(i) * time.Minute),
		}
		if got := f.Decide(&ev); got != FilterSummary {
			t.Fatalf("event %d verdict = %v", i, got)
		}
	}
	stats := f.Stats()
	if stats.SummaryBuckets != 2 {
		t.Fatalf("summary bucket count = %d, want 2", stats.SummaryBuckets)
	}
	if stats.SummaryEvicted != 1 {
		t.Fatalf("summary evictions = %d, want 1", stats.SummaryEvicted)
	}
}

func TestManagerPublishesDrainedSummaries(t *testing.T) {
	stream := eventstream.NewStream(4)
	m := NewManager(stream, nil, Options{
		NodeID:   "node-1",
		TenantID: "tenant-1",
		FilterCfg: FilterConfig{
			CaptureExternal:        false,
			CaptureInternalSummary: true,
		},
	})
	now := time.Now().UTC().Add(-3 * time.Minute)
	m.handle(ConnectionEvent{
		Kind:       "open",
		NetNS:      "net:[1]",
		SrcIP:      net.ParseIP("10.0.0.5"),
		SrcPort:    43000,
		DstIP:      net.ParseIP("10.0.0.7"),
		DstPort:    5432,
		PID:        42,
		Process:    "postgres",
		Protocol:   "tcp",
		BytesIn:    10,
		BytesOut:   20,
		StartedAt:  now,
		LastDataAt: now,
	})
	select {
	case ev := <-stream.Out():
		t.Fatalf("summary source event should have been buffered, got published event %#v", ev)
	default:
	}

	m.drainSummaries(time.Minute)
	select {
	case ev := <-stream.Out():
		if ev.Type != "conn.summary" {
			t.Fatalf("published event type = %q, want conn.summary", ev.Type)
		}
		if ev.ConnID == "" || ev.DedupKey == "" {
			t.Fatalf("summary missing identifiers: %#v", ev)
		}
		if got := ev.Details["netns"]; got != "net:[1]" {
			t.Fatalf("summary netns detail = %#v, want net:[1]", got)
		}
		if ev.BytesIn != 10 || ev.BytesOut != 20 {
			t.Fatalf("summary counters = %d/%d, want 10/20", ev.BytesIn, ev.BytesOut)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for drained summary event")
	}
	if stats := m.Stats(); stats.SummaryBuckets != 0 {
		t.Fatalf("summary buckets after manager drain = %d, want 0", stats.SummaryBuckets)
	}
}
