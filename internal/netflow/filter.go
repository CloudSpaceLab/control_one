package netflow

import (
	"net"
	"sync"
	"time"
)

// FilterVerdict drives Manager.handle.
type FilterVerdict int

const (
	// FilterEmit publishes the event as-is.
	FilterEmit FilterVerdict = iota
	// FilterSummary feeds the event into the rolling aggregator instead of
	// publishing a row per syscall.
	FilterSummary
	// FilterDrop drops the event silently (e.g., excluded internal flows).
	FilterDrop
)

// FilterConfig is the per-tenant capture policy. Bound to
// `tenant_event_filters` in the controlplane DB; pushed to the agent via
// the heartbeat response and applied with Manager.SetFilter.
type FilterConfig struct {
	// CaptureExternal: emit full lifecycle for non-RFC1918/loopback peers.
	CaptureExternal bool
	// CaptureInternalSummary: aggregate internal flows per (pid, dst_port,
	// minute) instead of dropping them entirely.
	CaptureInternalSummary bool
	// CaptureListeningChanges: always emit when a socket transitions in or
	// out of LISTEN.
	CaptureListeningChanges bool
	// AlwaysCaptureThreat: never drop / summarise a connection whose IP hits
	// the threat-intel snapshot.
	AlwaysCaptureThreat bool
	// AllowlistCIDRs: peers in this set are dropped (treated as fully
	// internal even if outside RFC1918 — useful for partner VPCs).
	AllowlistCIDRs []*net.IPNet
	// DenylistCIDRs: peers in this set are always emitted at full detail
	// regardless of other rules.
	DenylistCIDRs []*net.IPNet
}

// DefaultFilterConfig returns the safe default — full external capture,
// internal-summary on, listening changes captured, threats always full.
func DefaultFilterConfig() FilterConfig {
	return FilterConfig{
		CaptureExternal:         true,
		CaptureInternalSummary:  true,
		CaptureListeningChanges: true,
		AlwaysCaptureThreat:     true,
	}
}

// Filter implements smart-filter for the netflow Manager.
type Filter struct {
	cfg        FilterConfig
	mu         sync.Mutex
	roll       map[summaryKey]*summaryBucket
	maxBuckets int
	evicted    uint64
}

type summaryKey struct {
	netns   string
	pid     int
	dstPort uint16
	minute  int64
}

type summaryBucket struct {
	bytesIn  uint64
	bytesOut uint64
	count    int
	dst      net.IP
	process  string
	user     string
	first    time.Time
	last     time.Time
	protocol string
}

// NewFilter constructs a Filter from a config.
func NewFilter(cfg FilterConfig) *Filter {
	return NewFilterWithLimit(cfg, 4096)
}

// NewFilterWithLimit constructs a Filter with a hard cap on summary buckets.
func NewFilterWithLimit(cfg FilterConfig, maxBuckets int) *Filter {
	if maxBuckets <= 0 {
		maxBuckets = 4096
	}
	return &Filter{cfg: cfg, roll: make(map[summaryKey]*summaryBucket), maxBuckets: maxBuckets}
}

// Decide returns the verdict for a single event. May mutate the event (e.g.
// set ThreatMatch=true if the event matches threat intel — but threat intel
// resolution belongs in the collector itself, not here, since the threat
// snapshot lives outside the filter).
func (f *Filter) Decide(ev *ConnectionEvent) FilterVerdict {
	if f == nil {
		return FilterEmit
	}
	// Listening-state changes always go through (when enabled).
	if f.cfg.CaptureListeningChanges && ev.State == "LISTEN" {
		return FilterEmit
	}
	// Threat hits override everything else.
	if f.cfg.AlwaysCaptureThreat && ev.ThreatMatch {
		return FilterEmit
	}
	// Operator-set denylist forces full capture.
	for _, n := range f.cfg.DenylistCIDRs {
		if n != nil && (n.Contains(ev.SrcIP) || n.Contains(ev.DstIP)) {
			return FilterEmit
		}
	}
	// Operator-set allowlist drops entirely.
	for _, n := range f.cfg.AllowlistCIDRs {
		if n != nil && (n.Contains(ev.SrcIP) || n.Contains(ev.DstIP)) {
			return FilterDrop
		}
	}
	// External (non-RFC1918) peer → full emit.
	if f.cfg.CaptureExternal && (isExternal(ev.SrcIP) || isExternal(ev.DstIP)) {
		return FilterEmit
	}
	// Internal-only flow.
	if f.cfg.CaptureInternalSummary {
		f.fold(ev)
		return FilterSummary
	}
	return FilterDrop
}

func (f *Filter) fold(ev *ConnectionEvent) {
	bucketTS := ev.StartedAt
	if bucketTS.IsZero() {
		bucketTS = time.Now().UTC()
	}
	key := summaryKey{
		netns:   ev.NetNS,
		pid:     ev.PID,
		dstPort: ev.DstPort,
		minute:  bucketTS.Truncate(time.Minute).Unix(),
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.roll[key]
	if !ok {
		if len(f.roll) >= f.maxBuckets {
			f.evictOldestLocked()
		}
		b = &summaryBucket{
			dst:      ev.DstIP,
			process:  ev.Process,
			user:     ev.User,
			first:    bucketTS,
			last:     bucketTS,
			protocol: ev.Protocol,
		}
		f.roll[key] = b
	}
	b.bytesIn += ev.BytesIn
	b.bytesOut += ev.BytesOut
	b.count++
	if ev.LastDataAt.After(b.last) {
		b.last = ev.LastDataAt
	}
}

func (f *Filter) evictOldestLocked() {
	var oldest summaryKey
	found := false
	for k := range f.roll {
		if !found || k.minute < oldest.minute {
			oldest = k
			found = true
		}
	}
	if found {
		delete(f.roll, oldest)
		f.evicted++
	}
}

// Drain returns the buffered summary buckets older than `age` and removes
// them from the rolling map. Manager.Run calls this on a 30s timer.
func (f *Filter) Drain(age time.Duration) []ConnectionEvent {
	if f == nil {
		return nil
	}
	cutoff := time.Now().UTC().Add(-age).Truncate(time.Minute).Unix()
	out := []ConnectionEvent{}
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, b := range f.roll {
		if k.minute > cutoff {
			continue
		}
		out = append(out, ConnectionEvent{
			Kind:       "summary",
			NetNS:      k.netns,
			PID:        k.pid,
			Process:    b.process,
			User:       b.user,
			DstIP:      b.dst,
			DstPort:    k.dstPort,
			Protocol:   b.protocol,
			BytesIn:    b.bytesIn,
			BytesOut:   b.bytesOut,
			StartedAt:  b.first,
			EndedAt:    b.last,
			LastDataAt: b.last,
			State:      "summary",
		})
		delete(f.roll, k)
	}
	return out
}

type FilterStats struct {
	SummaryBuckets  int    `json:"summary_buckets"`
	SummaryEvicted  uint64 `json:"summary_evicted"`
	MaxSummarySlots int    `json:"max_summary_slots"`
}

func (f *Filter) Stats() FilterStats {
	if f == nil {
		return FilterStats{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return FilterStats{
		SummaryBuckets:  len(f.roll),
		SummaryEvicted:  f.evicted,
		MaxSummarySlots: f.maxBuckets,
	}
}

// isExternal returns true when ip is not in private / loopback / link-local
// ranges. IPv6 unique-local (fc00::/7) and link-local (fe80::/10) are
// treated as internal.
func isExternal(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsPrivate() {
		return false
	}
	return true
}
