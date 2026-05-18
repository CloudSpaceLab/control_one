// Package dbquery polls database stat tables and publishes per-query events.
// Backends:
//
//   - Postgres — pg_stat_activity + pg_stat_statements (extension required;
//     deploy step enables it).
//   - MySQL    — performance_schema.events_statements_history.
//
// Each tracked target is described by a Target struct (DSN + engine +
// scrape interval). The Manager spawns one goroutine per target; failures on
// one don't take down the rest.
//
// Event shape carries (engine, database, user, src_ip, query_hash,
// truncated query_text, rows, exec_time_ms, started_at, ended_at,
// tables_touched). The agent's correlator joins by (server_pid, src_ip).
package dbquery

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/eventstream"
)

// Engine identifies the supported DB family.
type Engine string

const (
	EnginePostgres Engine = "postgres"
	EngineMySQL    Engine = "mysql"
	EngineMSSQL    Engine = "mssql"
	EngineMongo    Engine = "mongo" // unsupported_v1
)

// Target is one database the agent polls.
type Target struct {
	Name           string // human-friendly label
	Engine         Engine
	DSN            string        // database/sql DSN
	ScrapeInterval time.Duration // default 5s
	Disabled       bool
}

// Options for the manager.
type Options struct {
	NodeID   string
	TenantID string
	Targets  []Target
}

// Manager spawns one collector per configured target.
type Manager struct {
	stream      *eventstream.Stream
	log         *zap.Logger
	opts        Options
	captureText atomic.Bool
}

// NewManager wires the manager. Run() spawns collectors.
func NewManager(stream *eventstream.Stream, log *zap.Logger, opts Options) *Manager {
	m := &Manager{stream: stream, log: log, opts: opts}
	m.captureText.Store(false)
	return m
}

// SetCaptureQueryText hot-toggles whether the publish path includes the
// truncated query text. Privacy / regulated tenants flip this to false
// while keeping query_hash + tables_touched for behavioural analysis.
func (m *Manager) SetCaptureQueryText(on bool) {
	if m == nil {
		return
	}
	m.captureText.Store(on)
}

// Run spawns collectors and blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	if m == nil || m.stream == nil {
		return
	}
	if len(m.opts.Targets) == 0 {
		if m.log != nil {
			m.log.Info("dbquery: no targets configured; skipping")
		}
		<-ctx.Done()
		return
	}
	wg := sync.WaitGroup{}
	for _, target := range m.opts.Targets {
		if target.Disabled {
			continue
		}
		t := target
		if t.ScrapeInterval <= 0 {
			t.ScrapeInterval = 5 * time.Second
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.runTarget(ctx, t)
		}()
	}
	wg.Wait()
}

func (m *Manager) runTarget(ctx context.Context, t Target) {
	db, err := sql.Open(driverFor(t.Engine), t.DSN)
	if err != nil {
		if m.log != nil {
			m.log.Warn("dbquery open", zap.String("target", t.Name), zap.Error(err))
		}
		return
	}
	defer func() { _ = db.Close() }()

	// MSSQL gets a parallel long-running-query scrape every 10 s. Hung
	// queries during exfil show up well before they land in
	// dm_exec_query_stats (which only updates at completion).
	if t.Engine == EngineMSSQL {
		go m.runMSSQLLongRunning(ctx, t, db)
	}

	tick := time.NewTicker(t.ScrapeInterval)
	defer tick.Stop()
	prev := map[string]queryState{}
	for {
		now := time.Now().UTC()
		curr, err := scrape(ctx, db, t)
		if err != nil {
			if m.log != nil {
				m.log.Debug("dbquery scrape", zap.String("target", t.Name), zap.Error(err))
			}
		} else {
			for _, q := range curr {
				p, ok := prev[q.queryHash]
				if !ok {
					m.publish(t, q, now)
					continue
				}
				// Emit a delta event so dashboards can show traffic shape.
				delta := q
				delta.calls -= p.calls
				delta.totalTimeMS -= p.totalTimeMS
				delta.rows -= p.rows
				if delta.calls > 0 {
					m.publish(t, delta, now)
				}
			}
		}
		nextPrev := make(map[string]queryState, len(curr))
		for _, q := range curr {
			nextPrev[q.queryHash] = q
		}
		prev = nextPrev
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (m *Manager) runMSSQLLongRunning(ctx context.Context, t Target, db *sql.DB) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		rows, err := scrapeMSSQLLongRunning(ctx, db)
		if err != nil {
			if m.log != nil {
				m.log.Debug("mssql long-running scrape", zap.String("target", t.Name), zap.Error(err))
			}
			continue
		}
		now := time.Now().UTC()
		for _, q := range rows {
			m.publishLongRunning(t, q, now)
		}
	}
}

func (m *Manager) publishLongRunning(t Target, q queryState, now time.Time) {
	if m.stream == nil {
		return
	}
	msg := ""
	if m.captureText.Load() {
		msg = truncString(q.queryText, 512)
	}
	m.stream.Publish(eventstream.Event{
		Type:       "db.query.long_running",
		TS:         now,
		NodeID:     m.opts.NodeID,
		TenantID:   m.opts.TenantID,
		Severity:   "medium",
		Message:    msg,
		DurationMS: int64(q.totalTimeMS),
		DedupKey:   fmt.Sprintf("db.long:%s:%s:%d", t.Name, q.queryHash, now.Unix()/60),
		Details: map[string]any{
			"engine":         string(t.Engine),
			"database_name":  q.dbName,
			"user_name":      q.userName,
			"src_ip":         q.clientIP,
			"query_hash":     q.queryHash,
			"rows_affected":  int64(q.rows),
			"exec_time_ms":   int64(q.totalTimeMS),
			"tables_touched": strings.Join(q.tables, ","),
			"target":         t.Name,
		},
	})
}

func (m *Manager) publish(t Target, q queryState, now time.Time) {
	if m.stream == nil {
		return
	}
	msg := ""
	if m.captureText.Load() {
		msg = truncString(q.queryText, 512)
	}
	m.stream.Publish(eventstream.Event{
		Type:       "db.query",
		TS:         now,
		NodeID:     m.opts.NodeID,
		TenantID:   m.opts.TenantID,
		Severity:   "info",
		Message:    msg,
		DurationMS: int64(q.totalTimeMS),
		DedupKey:   fmt.Sprintf("db.query:%s:%s:%d", t.Name, q.queryHash, now.Unix()),
		Details: map[string]any{
			"engine":         string(t.Engine),
			"database_name":  q.dbName,
			"user_name":      q.userName,
			"src_ip":         q.clientIP,
			"query_hash":     q.queryHash,
			"calls":          int64(q.calls),
			"rows_affected":  int64(q.rows),
			"exec_time_ms":   int64(q.totalTimeMS),
			"tables_touched": strings.Join(q.tables, ","),
			"target":         t.Name,
		},
	})
}

func driverFor(e Engine) string {
	switch e {
	case EnginePostgres:
		return "postgres" // lib/pq driver — already in go.mod
	case EngineMySQL:
		return "mysql"
	case EngineMSSQL:
		return "sqlserver" // microsoft/go-mssqldb
	}
	return string(e)
}

// queryState is the per-poll snapshot row.
type queryState struct {
	queryHash   string
	queryText   string
	dbName      string
	userName    string
	clientIP    string
	calls       int64
	rows        int64
	totalTimeMS float64
	tables      []string
}

func hashQueryText(text string) string {
	h := md5.Sum([]byte(text))
	return hex.EncodeToString(h[:])[:16]
}

func truncString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
