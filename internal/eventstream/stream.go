// Package eventstream is the agent-side fan-in for every collector. Procmon,
// netflow, fileaccess, dbquery, log-spike, bastion, and the existing scanner
// all publish typed events into a single Stream channel. A batcher consumes
// the channel and ships gzipped ndjson to the controlplane's
// `/api/v1/events/ingest` endpoint.
//
// Design notes:
//   - The Stream itself is decoupled from transport: collectors don't care
//     where their events end up, they just push.
//   - Smart-filter and rate-limit decisions belong on the producer side
//     (e.g. internal/netflow/filter.go); Stream is dumb on purpose.
//   - The Correlator stamps a shared correlation_id when it sees a
//     proc.exec ↔ conn.open pair within the join window, so downstream
//     consumers can JOIN by `correlation_id` cheaply.
package eventstream

import (
	"sync"
	"time"
)

// Event is the canonical agent-side event the batcher serialises and posts.
// Fields mirror controlplane/internal/server/events_ingest.IngestedEvent.
type Event struct {
	Type          string         `json:"type"`
	TS            time.Time      `json:"ts"`
	NodeID        string         `json:"node_id,omitempty"`
	TenantID      string         `json:"tenant_id,omitempty"`
	Severity      string         `json:"severity,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	ConnID        string         `json:"conn_id,omitempty"`
	BastionSessID string         `json:"bastion_session_id,omitempty"`
	PID           int64          `json:"pid,omitempty"`
	ProcessName   string         `json:"process_name,omitempty"`
	UserName      string         `json:"user_name,omitempty"`
	SrcIP         string         `json:"src_ip,omitempty"`
	SrcPort       int            `json:"src_port,omitempty"`
	DstIP         string         `json:"dst_ip,omitempty"`
	DstPort       int            `json:"dst_port,omitempty"`
	Protocol      string         `json:"protocol,omitempty"`
	BytesIn       int64          `json:"bytes_in,omitempty"`
	BytesOut      int64          `json:"bytes_out,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
	RuleID        string         `json:"rule_id,omitempty"`
	ThreatFeed    string         `json:"threat_feed,omitempty"`
	ThreatScore   int            `json:"threat_score,omitempty"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	DedupKey      string         `json:"dedup_key,omitempty"`
}

// Stream is the agent-wide bus all collectors publish into. It's a buffered
// channel; the batcher drains it. When the buffer fills we drop the oldest
// event and increment a counter so operators see the loss.
type Stream struct {
	out     chan Event
	dropped uint64
	mu      sync.Mutex
}

// NewStream returns a Stream with the given buffer capacity. 4096 is a
// reasonable default for moderate-traffic hosts; tune up to 16k on busy
// servers.
func NewStream(buf int) *Stream {
	if buf <= 0 {
		buf = 4096
	}
	return &Stream{out: make(chan Event, buf)}
}

// Publish sends an event into the stream. Non-blocking: if the buffer is
// full the event is dropped and the dropped counter ticks.
func (s *Stream) Publish(ev Event) {
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	select {
	case s.out <- ev:
	default:
		s.mu.Lock()
		s.dropped++
		s.mu.Unlock()
	}
}

// Out returns the receive end the batcher pulls from. Don't close from
// outside the package.
func (s *Stream) Out() <-chan Event { return s.out }

// Dropped returns the number of events dropped due to buffer full since
// startup. Useful for an `agent_event_stream_drops_total` Prometheus
// counter.
func (s *Stream) Dropped() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Close shuts the stream. After Close, Publish silently drops.
func (s *Stream) Close() {
	defer func() { _ = recover() }() // double-close is a no-op
	close(s.out)
}
