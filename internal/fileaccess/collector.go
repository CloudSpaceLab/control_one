// Package fileaccess publishes per-file open/read/write/unlink/rename events
// into the agent's eventstream. Backends are selected at runtime — ebpf
// LSM (Linux ≥5.7) > auditd-tail (Linux) > ETW (Windows) > fs_usage
// (macOS).
//
// Smart filter (FilterConfig) keeps cardinality bounded:
//
//   * Only emit when path matches a watched prefix (default: /etc/, /var/lib/,
//     /var/log/, /opt/, /home/, /root/, /tmp/sensitive/).
//   * OR the process owning the access has an active external network
//     connection — captures "while attacker was connected".
//   * Aggregates per (pid, path, 5s bucket) so 1 event per file per process
//     per window, not per syscall.
package fileaccess

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/eventstream"
)

// FileEvent is the wire shape every backend emits.
type FileEvent struct {
	Op       string // "open" | "read" | "write" | "unlink" | "rename"
	Path     string
	PID      int
	Process  string
	User     string
	Bytes    int64
	OpCount  int
	StartedAt time.Time
	EndedAt  time.Time
	// CorrelationID lets the eventstream correlator skip its own join when
	// a backend already attaches it (e.g. the eBPF LSM hook can read the
	// agent's per-PID correlation map).
	CorrelationID string
}

// FilterConfig controls what's captured.
type FilterConfig struct {
	WatchedPrefixes []string
	MinBytes        int64
	// ConnAffinity: if true, the collector consults a per-PID "has active
	// external conn" oracle (set via SetConnAffinityOracle) and emits even
	// for paths not in WatchedPrefixes.
	ConnAffinity bool
	// Disabled: kill switch. When true the collector idles.
	Disabled bool
}

// DefaultFilterConfig returns sensible defaults.
func DefaultFilterConfig() FilterConfig {
	return FilterConfig{
		WatchedPrefixes: []string{
			"/etc/",
			"/var/lib/",
			"/var/log/",
			"/opt/",
			"/home/",
			"/root/",
			"/tmp/sensitive/",
		},
		ConnAffinity: true,
	}
}

// ConnAffinityOracle reports whether a PID currently owns at least one
// external (non-RFC1918) connection. Wired by the agent at startup against
// the netflow Manager.
type ConnAffinityOracle func(pid int) bool

// Collector is the abstraction every backend implements.
type Collector interface {
	Name() string
	Run(ctx context.Context, out chan<- FileEvent) error
}

// Options for the Manager.
type Options struct {
	NodeID    string
	TenantID  string
	FilterCfg FilterConfig
}

// Manager picks a backend, applies the filter, buckets per (pid, path)
// every 5 s, and forwards events into the eventstream.
type Manager struct {
	stream   *eventstream.Stream
	log      *zap.Logger
	opts     Options
	mu       sync.RWMutex
	chosen   Collector
	connOK   ConnAffinityOracle
	bucket   *bucketAggregator
	forensic bool
}

// NewManager wires the Manager.
func NewManager(stream *eventstream.Stream, log *zap.Logger, opts Options) *Manager {
	return &Manager{
		stream: stream,
		log:    log,
		opts:   opts,
		bucket: newBucketAggregator(5 * time.Second),
	}
}

// SetConnAffinityOracle plugs the netflow Manager in. Idempotent.
func (m *Manager) SetConnAffinityOracle(o ConnAffinityOracle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connOK = o
}

// UpdateFilter hot-swaps the watched-prefix list and the forensic-mode
// kill switch. Called from the heartbeat receiver every tick. When
// forensic is true the path-prefix filter is bypassed (everything emits)
// so an active incident captures full breadth.
func (m *Manager) UpdateFilter(watched []string, forensic bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if watched != nil {
		// Defensive copy so callers can't mutate live state via the slice.
		dup := make([]string, len(watched))
		copy(dup, watched)
		m.opts.FilterCfg.WatchedPrefixes = dup
	}
	m.forensic = forensic
}

// Run picks a backend and forwards events until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	if m.opts.FilterCfg.Disabled {
		if m.log != nil {
			m.log.Info("fileaccess collector disabled by config")
		}
		return
	}
	col := pickFileBackend(m.log, m.opts)
	if col == nil {
		if m.log != nil {
			m.log.Info("fileaccess: no backend on this platform; skipping")
		}
		return
	}
	m.mu.Lock()
	m.chosen = col
	m.mu.Unlock()

	out := make(chan FileEvent, 1024)
	go func() {
		if err := col.Run(ctx, out); err != nil && m.log != nil {
			m.log.Warn("fileaccess collector exited", zap.String("backend", col.Name()), zap.Error(err))
		}
		close(out)
	}()
	if m.log != nil {
		m.log.Info("fileaccess collector running", zap.String("backend", col.Name()))
	}

	flush := time.NewTicker(5 * time.Second)
	defer flush.Stop()

	for {
		select {
		case <-ctx.Done():
			m.flushAll()
			return
		case <-flush.C:
			m.flushAll()
		case ev, ok := <-out:
			if !ok {
				m.flushAll()
				return
			}
			if !m.shouldEmit(ev) {
				continue
			}
			m.bucket.add(ev)
		}
	}
}

func (m *Manager) shouldEmit(ev FileEvent) bool {
	m.mu.RLock()
	cfg := m.opts.FilterCfg
	forensic := m.forensic
	o := m.connOK
	m.mu.RUnlock()

	// Forensic mode bypasses every filter except the byte floor — operators
	// flip this during an active incident to capture full breadth.
	if forensic {
		return ev.Bytes >= cfg.MinBytes
	}
	if ev.Bytes < cfg.MinBytes {
		return false
	}
	for _, pfx := range cfg.WatchedPrefixes {
		if pfx == "" {
			continue
		}
		if hasPrefix(ev.Path, pfx) {
			return true
		}
	}
	if cfg.ConnAffinity && o != nil && o(ev.PID) {
		return true
	}
	return false
}

func (m *Manager) flushAll() {
	for _, ev := range m.bucket.drain(time.Now()) {
		m.publish(ev)
	}
}

func (m *Manager) publish(ev FileEvent) {
	if m.stream == nil {
		return
	}
	m.stream.Publish(eventstream.Event{
		Type:        "file." + ev.Op,
		TS:          ev.StartedAt,
		NodeID:      m.opts.NodeID,
		TenantID:    m.opts.TenantID,
		PID:         int64(ev.PID),
		ProcessName: ev.Process,
		UserName:    ev.User,
		BytesIn:     ev.Bytes,
		Severity:    "info",
		Message:     ev.Path,
		Details: map[string]any{
			"path":       ev.Path,
			"op":         ev.Op,
			"op_count":   int64(ev.OpCount),
			"started_at": ev.StartedAt.Format("2006-01-02 15:04:05.000"),
			"ended_at":   ev.EndedAt.Format("2006-01-02 15:04:05.000"),
		},
		CorrelationID: ev.CorrelationID,
	})
}

func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}
