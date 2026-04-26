// Package netflow observes IP-connection lifecycles per host and publishes
// `conn.open` / `conn.close` / `conn.summary` events into the agent's
// eventstream. The collector picks the strongest backend available at
// runtime:
//
//   * Linux ≥5.4 with CAP_BPF: cilium/ebpf tcplife (collector_linux_ebpf.go)
//   * Linux fallback: /proc/net/tcp{,6} polling (collector_linux_proc.go)
//   * Windows: PowerShell Get-NetTCPConnection polling (collector_windows.go)
//   * Darwin: lsof + nettop polling (collector_darwin.go)
//
// Smart filter (filter.go) decides per-event whether full lifecycle data is
// emitted (non-RFC1918 / threat-intel match / listening-port change) or
// folded into a per-(pid, dst_port, minute) `conn.summary` row.
package netflow

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/eventstream"
)

// ConnectionEvent is the platform-agnostic shape every backend emits.
type ConnectionEvent struct {
	Kind          string    // "open" | "close" | "state_change" | "summary"
	PID           int
	Process       string
	User          string
	Cmdline       string
	SrcIP         net.IP
	SrcPort       uint16
	DstIP         net.IP
	DstPort       uint16
	Protocol      string
	State         string
	BytesIn       uint64
	BytesOut      uint64
	PacketsIn     uint64
	PacketsOut    uint64
	BytesInDelta  uint64
	BytesOutDelta uint64
	StartedAt     time.Time
	EndedAt       time.Time
	LastDataAt    time.Time
	Direction     string // "inbound" | "outbound"
	ThreatMatch   bool
	ThreatFeed    string
	ThreatScore   int
}

// Collector is the abstraction every backend implements.
type Collector interface {
	Name() string
	Run(ctx context.Context, out chan<- ConnectionEvent) error
}

// Options configure the manager + filter + emit path.
type Options struct {
	NodeID    string
	TenantID  string
	// FilterCfg drives smart-filter decisions. Hot-reload by replacing the
	// pointer atomically (Manager.SetFilter).
	FilterCfg FilterConfig
	// PollInterval drives polling backends. eBPF backends ignore this.
	PollInterval time.Duration
}

// Manager picks the best available collector and forwards filtered events
// into the supplied eventstream.
type Manager struct {
	stream    *eventstream.Stream
	log       *zap.Logger
	opts      Options
	mu        sync.RWMutex
	filter    *Filter
	chosen    Collector
	stopCh    chan struct{}
}

// NewManager wires the manager. Run() picks a backend lazily; if no backend
// is available the manager logs a warning and idles.
func NewManager(stream *eventstream.Stream, log *zap.Logger, opts Options) *Manager {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 5 * time.Second
	}
	return &Manager{
		stream: stream,
		log:    log,
		opts:   opts,
		filter: NewFilter(opts.FilterCfg),
		stopCh: make(chan struct{}),
	}
}

// SetFilter swaps the filter atomically. Used by the heartbeat policy
// channel to hot-reload tenant settings without restarting the agent.
func (m *Manager) SetFilter(cfg FilterConfig) {
	f := NewFilter(cfg)
	m.mu.Lock()
	m.filter = f
	m.mu.Unlock()
}

// Filter returns the current filter (read-only consumer).
func (m *Manager) Filter() *Filter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.filter
}

// Run picks a collector and forwards events until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	col := pickCollector(m.log, m.opts)
	if col == nil {
		if m.log != nil {
			m.log.Warn("netflow: no collector available on this platform; skipping connection observability")
		}
		return
	}
	m.mu.Lock()
	m.chosen = col
	m.mu.Unlock()

	out := make(chan ConnectionEvent, 1024)
	go func() {
		if err := col.Run(ctx, out); err != nil && m.log != nil {
			m.log.Warn("netflow collector exited", zap.String("backend", col.Name()), zap.Error(err))
		}
		close(out)
	}()
	if m.log != nil {
		m.log.Info("netflow collector running", zap.String("backend", col.Name()))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-out:
			if !ok {
				return
			}
			m.handle(ev)
		}
	}
}

// Name returns the active backend name (or "none").
func (m *Manager) Name() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.chosen == nil {
		return "none"
	}
	return m.chosen.Name()
}

func (m *Manager) handle(ev ConnectionEvent) {
	if m.stream == nil {
		return
	}
	verdict := m.Filter().Decide(&ev)
	switch verdict {
	case FilterDrop:
		return
	case FilterSummary:
		// Aggregator inside Filter buffers + flushes summary events itself
		// via Tick(); nothing to publish here.
		return
	}
	connID := makeConnID(ev)
	stream := m.stream

	streamEv := eventstream.Event{
		TS:          ev.StartedAt,
		NodeID:      m.opts.NodeID,
		TenantID:    m.opts.TenantID,
		ConnID:      connID,
		PID:         int64(ev.PID),
		ProcessName: ev.Process,
		UserName:    ev.User,
		SrcIP:       ipString(ev.SrcIP),
		SrcPort:     int(ev.SrcPort),
		DstIP:       ipString(ev.DstIP),
		DstPort:     int(ev.DstPort),
		Protocol:    ev.Protocol,
		BytesIn:     int64(ev.BytesIn),
		BytesOut:    int64(ev.BytesOut),
		Severity:    "info",
		ThreatFeed:  ev.ThreatFeed,
		ThreatScore: ev.ThreatScore,
		Details: map[string]any{
			"state":          ev.State,
			"direction":      ev.Direction,
			"cmdline":        ev.Cmdline,
			"packets_in":     int64(ev.PacketsIn),
			"packets_out":    int64(ev.PacketsOut),
			"bytes_in_delta": int64(ev.BytesInDelta),
			"bytes_out_delta": int64(ev.BytesOutDelta),
		},
	}
	if !ev.EndedAt.IsZero() {
		streamEv.Details["ended_at"] = ev.EndedAt.Format("2006-01-02 15:04:05.000")
		streamEv.DurationMS = ev.EndedAt.Sub(ev.StartedAt).Milliseconds()
	}
	if !ev.LastDataAt.IsZero() {
		streamEv.Details["last_data_at"] = ev.LastDataAt.Format("2006-01-02 15:04:05.000")
	}
	if !ev.StartedAt.IsZero() {
		streamEv.Details["started_at"] = ev.StartedAt.Format("2006-01-02 15:04:05.000")
	}
	switch ev.Kind {
	case "open":
		streamEv.Type = "conn.open"
		streamEv.DedupKey = fmt.Sprintf("conn.open:%s", connID)
	case "close":
		streamEv.Type = "conn.close"
		streamEv.DedupKey = fmt.Sprintf("conn.close:%s", connID)
		if ev.ThreatMatch {
			streamEv.Severity = "high"
		}
	case "state_change":
		streamEv.Type = "conn.state_change"
		streamEv.DedupKey = fmt.Sprintf("conn.state:%s:%s:%d", connID, ev.State, ev.LastDataAt.UnixNano())
	default:
		streamEv.Type = "conn.summary"
		streamEv.DedupKey = fmt.Sprintf("conn.summary:%d:%d:%d", ev.PID, ev.DstPort, ev.StartedAt.Unix())
	}
	stream.Publish(streamEv)
}

// makeConnID hashes the 4-tuple + start-time so a single connection has the
// same id across open / state_change / close events.
func makeConnID(ev ConnectionEvent) string {
	src := ipString(ev.SrcIP)
	dst := ipString(ev.DstIP)
	h := sha1.New()
	fmt.Fprintf(h, "%s:%d|%s:%d|%s|%d", src, ev.SrcPort, dst, ev.DstPort, ev.Protocol, ev.StartedAt.UnixNano())
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}
