package eventstream

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Correlator stamps a shared correlation_id onto events that belong to the
// same logical "thing happening" on the host:
//
//   * proc.exec lands within ±2s of conn.open from the same PID → same id
//   * file.* with same PID and within an active conn window → same id
//   * db.query with the same conn_id → same id
//   * log.spike with a matched _PID inside the conn window → same id
//
// The correlator runs on the agent before events leave the box so the
// controlplane gets a ready-to-JOIN event stream without server-side
// reconstruction. It is best-effort — the join window is a real-time
// approximation, not a strict invariant. Server-side correlation engine
// keeps doing its windowed job for cross-node patterns.
type Correlator struct {
	mu        sync.Mutex
	byPID     map[int64]*correlation
	byConn    map[string]*correlation
	cleanupAt time.Time
	window    time.Duration
}

// correlation tracks one ongoing causal cluster.
type correlation struct {
	id       string
	startedAt time.Time
	expiresAt time.Time
	conns    map[string]struct{} // conn_ids belonging to this cluster
}

// NewCorrelator returns a correlator with the given join window.
// 2s is the default that experimentally captures the proc.exec → conn.open
// causality without merging unrelated activity on busy hosts.
func NewCorrelator(window time.Duration) *Correlator {
	if window <= 0 {
		window = 2 * time.Second
	}
	return &Correlator{
		byPID:  make(map[int64]*correlation),
		byConn: make(map[string]*correlation),
		window: window,
	}
}

// Stamp mutates ev.CorrelationID in place. Idempotent: if the event already
// has a correlation_id, it's preserved (collector overrides win).
func (c *Correlator) Stamp(ev *Event) {
	if ev == nil {
		return
	}
	if ev.CorrelationID != "" {
		// Track the conn so file/db events on this conn join later.
		if ev.ConnID != "" {
			c.touchConn(ev.ConnID, ev.CorrelationID, ev.TS)
		}
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maybeCleanup()

	now := ev.TS
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// 1. If the event references an existing connection, inherit its id.
	if ev.ConnID != "" {
		if cor, ok := c.byConn[ev.ConnID]; ok {
			ev.CorrelationID = cor.id
			cor.expiresAt = now.Add(c.window)
			return
		}
	}

	// 2. PID-based join window.
	if ev.PID != 0 {
		if cor, ok := c.byPID[ev.PID]; ok && now.Before(cor.expiresAt) {
			ev.CorrelationID = cor.id
			cor.expiresAt = now.Add(c.window)
			if ev.ConnID != "" {
				cor.conns[ev.ConnID] = struct{}{}
				c.byConn[ev.ConnID] = cor
			}
			return
		}
	}

	// 3. New cluster.
	cor := &correlation{
		id:        uuid.New().String(),
		startedAt: now,
		expiresAt: now.Add(c.window),
		conns:     make(map[string]struct{}),
	}
	ev.CorrelationID = cor.id
	if ev.PID != 0 {
		c.byPID[ev.PID] = cor
	}
	if ev.ConnID != "" {
		cor.conns[ev.ConnID] = struct{}{}
		c.byConn[ev.ConnID] = cor
	}
}

// touchConn records that a connection belongs to a correlation cluster.
func (c *Correlator) touchConn(connID, correlationID string, ts time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cor, ok := c.byConn[connID]; ok {
		cor.expiresAt = ts.Add(c.window)
		return
	}
	cor := &correlation{id: correlationID, startedAt: ts, expiresAt: ts.Add(c.window), conns: map[string]struct{}{connID: {}}}
	c.byConn[connID] = cor
}

// maybeCleanup is called under c.mu. Drops expired entries every 30s so the
// correlator's memory footprint stays bounded.
func (c *Correlator) maybeCleanup() {
	if time.Since(c.cleanupAt) < 30*time.Second {
		return
	}
	c.cleanupAt = time.Now()
	now := time.Now()
	for pid, cor := range c.byPID {
		if now.After(cor.expiresAt) {
			delete(c.byPID, pid)
		}
	}
	for cid, cor := range c.byConn {
		if now.After(cor.expiresAt) {
			delete(c.byConn, cid)
		}
	}
}

// SnapshotSize returns current map cardinalities. For tests / metrics.
func (c *Correlator) SnapshotSize() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.byPID), len(c.byConn)
}
