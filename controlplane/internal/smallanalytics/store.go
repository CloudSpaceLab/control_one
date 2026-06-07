package smallanalytics

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	_ "modernc.org/sqlite"
)

type Config struct {
	Dir          string
	QueryTimeout time.Duration
	CacheMB      int
}

type Store struct {
	db           *sql.DB
	queryTimeout time.Duration
	writeMu      sync.Mutex
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		return nil, fmt.Errorf("small analytics sqlite_dir is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create small analytics dir: %w", err)
	}
	queryTimeout := cfg.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = 5 * time.Second
	}
	db, err := sql.Open("sqlite", sqliteDSN(filepath.Join(dir, "controlone-small-analytics.db"), cfg.CacheMB, queryTimeout))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	store := &Store{
		db:           db,
		queryTimeout: queryTimeout,
	}
	if err := store.migrate(ctx, cfg.CacheMB); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func sqliteDSN(path string, cacheMB int, busyTimeout time.Duration) string {
	if cacheMB <= 0 {
		cacheMB = 32
	}
	busyMillis := int(busyTimeout.Round(time.Millisecond) / time.Millisecond)
	if busyMillis < 1000 {
		busyMillis = 1000
	}
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyMillis))
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "temp_store(FILE)")
	q.Add("_pragma", fmt.Sprintf("cache_size(-%d)", cacheMB*1024))
	q.Set("_txlock", "immediate")
	return path + "?" + q.Encode()
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Healthy(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("small analytics unavailable")
	}
	qctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	var status string
	if err := s.db.QueryRowContext(qctx, "PRAGMA quick_check").Scan(&status); err != nil {
		return err
	}
	if !strings.EqualFold(status, "ok") {
		return fmt.Errorf("sqlite quick_check: %s", status)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context, cacheMB int) error {
	if cacheMB <= 0 {
		cacheMB = 32
	}
	cacheKiB := cacheMB * 1024
	stmts := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA temp_store = FILE",
		fmt.Sprintf("PRAGMA cache_size = -%d", cacheKiB),
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS process_connections (
			tenant_id TEXT NOT NULL,
			row_key TEXT NOT NULL,
			conn_id TEXT,
			correlation_id TEXT,
			started_at_ms INTEGER NOT NULL,
			ended_at_ms INTEGER,
			last_seen_ms INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			direction TEXT,
			pid INTEGER NOT NULL DEFAULT 0,
			process_name TEXT,
			cmdline TEXT,
			user_name TEXT,
			src_ip TEXT,
			src_port INTEGER NOT NULL DEFAULT 0,
			dst_ip TEXT,
			dst_port INTEGER NOT NULL DEFAULT 0,
			protocol TEXT,
			bytes_in INTEGER NOT NULL DEFAULT 0,
			bytes_out INTEGER NOT NULL DEFAULT 0,
			packets_in INTEGER NOT NULL DEFAULT 0,
			packets_out INTEGER NOT NULL DEFAULT 0,
			threat_match INTEGER NOT NULL DEFAULT 0,
			threat_feed TEXT,
			closed_reason TEXT,
			bastion_session_id TEXT,
			node_id TEXT,
			PRIMARY KEY (tenant_id, row_key)
		)`,
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_started_idx ON process_connections(tenant_id, started_at_ms DESC)",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_node_started_idx ON process_connections(tenant_id, node_id, started_at_ms DESC)",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_src_started_idx ON process_connections(tenant_id, src_ip, started_at_ms DESC)",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_dst_started_idx ON process_connections(tenant_id, dst_ip, started_at_ms DESC)",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_conn_idx ON process_connections(tenant_id, conn_id)",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_corr_started_idx ON process_connections(tenant_id, correlation_id, started_at_ms DESC)",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_ended_idx ON process_connections(tenant_id, ended_at_ms DESC) WHERE ended_at_ms IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_node_ended_idx ON process_connections(tenant_id, node_id, ended_at_ms DESC) WHERE ended_at_ms IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_src_ended_idx ON process_connections(tenant_id, src_ip, ended_at_ms DESC) WHERE ended_at_ms IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_dst_ended_idx ON process_connections(tenant_id, dst_ip, ended_at_ms DESC) WHERE ended_at_ms IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS process_connections_tenant_corr_ended_idx ON process_connections(tenant_id, correlation_id, ended_at_ms DESC) WHERE ended_at_ms IS NOT NULL",
		"INSERT OR IGNORE INTO schema_migrations(version, applied_at_ms) VALUES (1, ?)",
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt, "?") {
			if _, err := s.db.ExecContext(ctx, stmt, time.Now().UTC().UnixMilli()); err != nil {
				return fmt.Errorf("small analytics migration: %w", err)
			}
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("small analytics migration: %w", err)
		}
	}
	return nil
}

func (s *Store) AppendConnectionRows(ctx context.Context, rows []map[string]any) error {
	if s == nil || s.db == nil || len(rows) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	qctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(qctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(qctx, upsertConnectionSQL)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, raw := range rows {
		rec := parseConnectionMap(raw)
		if rec.TenantID == "" || rec.RowKey == "" || rec.StartedAt.IsZero() {
			continue
		}
		if _, err = stmt.ExecContext(qctx,
			rec.TenantID, rec.RowKey, rec.ConnID, rec.CorrelationID,
			rec.StartedAt.UTC().UnixMilli(), nullableTimeMillis(rec.EndedAt), rec.LastSeen.UTC().UnixMilli(),
			rec.DurationMS, rec.Direction, rec.PID, rec.ProcessName, rec.Cmdline, rec.UserName,
			rec.SrcIP, rec.SrcPort, rec.DstIP, rec.DstPort, rec.Protocol,
			rec.BytesIn, rec.BytesOut, rec.PacketsIn, rec.PacketsOut,
			boolInt(rec.ThreatMatch), rec.ThreatFeed, rec.ClosedReason, rec.BastionSession, rec.NodeID,
		); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

const upsertConnectionSQL = `
INSERT INTO process_connections (
	tenant_id, row_key, conn_id, correlation_id, started_at_ms, ended_at_ms, last_seen_ms,
	duration_ms, direction, pid, process_name, cmdline, user_name,
	src_ip, src_port, dst_ip, dst_port, protocol,
	bytes_in, bytes_out, packets_in, packets_out,
	threat_match, threat_feed, closed_reason, bastion_session_id, node_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, row_key) DO UPDATE SET
	conn_id = CASE WHEN excluded.conn_id != '' THEN excluded.conn_id ELSE process_connections.conn_id END,
	correlation_id = CASE WHEN excluded.correlation_id != '' THEN excluded.correlation_id ELSE process_connections.correlation_id END,
	started_at_ms = CASE WHEN excluded.started_at_ms < process_connections.started_at_ms THEN excluded.started_at_ms ELSE process_connections.started_at_ms END,
	ended_at_ms = CASE
		WHEN excluded.ended_at_ms IS NULL THEN process_connections.ended_at_ms
		WHEN process_connections.ended_at_ms IS NULL THEN excluded.ended_at_ms
		WHEN excluded.ended_at_ms > process_connections.ended_at_ms THEN excluded.ended_at_ms
		ELSE process_connections.ended_at_ms
	END,
	last_seen_ms = max(process_connections.last_seen_ms, excluded.last_seen_ms),
	duration_ms = max(process_connections.duration_ms, excluded.duration_ms),
	direction = CASE WHEN excluded.direction != '' THEN excluded.direction ELSE process_connections.direction END,
	pid = CASE WHEN excluded.pid != 0 THEN excluded.pid ELSE process_connections.pid END,
	process_name = CASE WHEN excluded.process_name != '' THEN excluded.process_name ELSE process_connections.process_name END,
	cmdline = CASE WHEN excluded.cmdline != '' THEN excluded.cmdline ELSE process_connections.cmdline END,
	user_name = CASE WHEN excluded.user_name != '' THEN excluded.user_name ELSE process_connections.user_name END,
	src_ip = CASE WHEN excluded.src_ip != '' THEN excluded.src_ip ELSE process_connections.src_ip END,
	src_port = CASE WHEN excluded.src_port != 0 THEN excluded.src_port ELSE process_connections.src_port END,
	dst_ip = CASE WHEN excluded.dst_ip != '' THEN excluded.dst_ip ELSE process_connections.dst_ip END,
	dst_port = CASE WHEN excluded.dst_port != 0 THEN excluded.dst_port ELSE process_connections.dst_port END,
	protocol = CASE WHEN excluded.protocol != '' THEN excluded.protocol ELSE process_connections.protocol END,
	bytes_in = max(process_connections.bytes_in, excluded.bytes_in),
	bytes_out = max(process_connections.bytes_out, excluded.bytes_out),
	packets_in = max(process_connections.packets_in, excluded.packets_in),
	packets_out = max(process_connections.packets_out, excluded.packets_out),
	threat_match = max(process_connections.threat_match, excluded.threat_match),
	threat_feed = CASE WHEN excluded.threat_feed != '' THEN excluded.threat_feed ELSE process_connections.threat_feed END,
	closed_reason = CASE WHEN excluded.closed_reason != '' THEN excluded.closed_reason ELSE process_connections.closed_reason END,
	bastion_session_id = CASE WHEN excluded.bastion_session_id != '' THEN excluded.bastion_session_id ELSE process_connections.bastion_session_id END,
	node_id = CASE WHEN excluded.node_id != '' THEN excluded.node_id ELSE process_connections.node_id END
`

func (s *Store) ListConnectionsForNode(ctx context.Context, tenantID, nodeID string, since, until time.Time, limit int, openOnly, externalOnly bool) ([]doris.ConnectionRow, error) {
	where := "tenant_id = ? AND node_id = ? AND started_at_ms <= ? AND (ended_at_ms IS NULL OR ended_at_ms >= ?)"
	args := []any{tenantID, nodeID, timeMillis(until), timeMillis(since)}
	if openOnly {
		where += " AND ended_at_ms IS NULL"
	}
	return s.queryConnections(ctx, where, args, limit, externalOnly)
}

func (s *Store) ListConnectionsForIP(ctx context.Context, tenantID, ip string, since, until time.Time, limit int) ([]doris.ConnectionRow, error) {
	where := "tenant_id = ? AND (src_ip = ? OR dst_ip = ?) AND started_at_ms <= ? AND (ended_at_ms IS NULL OR ended_at_ms >= ?)"
	args := []any{tenantID, ip, ip, timeMillis(until), timeMillis(since)}
	return s.queryConnections(ctx, where, args, limit, false)
}

func (s *Store) ListConnectionsForTenant(ctx context.Context, tenantID string, since, until time.Time, limit int, externalOnly bool) ([]doris.ConnectionRow, error) {
	where := "tenant_id = ? AND started_at_ms <= ? AND (ended_at_ms IS NULL OR ended_at_ms >= ?)"
	args := []any{tenantID, timeMillis(until), timeMillis(since)}
	return s.queryConnections(ctx, where, args, limit, externalOnly)
}

func (s *Store) ConnectionLifetime(ctx context.Context, tenantID, connID string) (*doris.ConnectionRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("small analytics unavailable")
	}
	qctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	row := s.db.QueryRowContext(qctx, selectConnectionColumns+`
		FROM process_connections
		WHERE tenant_id = ? AND conn_id = ?
		ORDER BY started_at_ms DESC
		LIMIT 1`, tenantID, connID)
	out, err := scanConnection(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// QueryEvents projects SQLite connection facts into the normalized
// investigation event shape. The small backend starts with connection evidence
// and keeps the same API contract as the Doris event query.
func (s *Store) QueryEvents(ctx context.Context, p doris.EventQueryParams) ([]doris.EventRow, int, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("small analytics unavailable")
	}
	query, countQuery, args, err := buildConnectionEventQuerySQL(p)
	if err != nil {
		return nil, 0, err
	}
	qctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	var total int
	if err := s.db.QueryRowContext(qctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count small analytics events: %w", err)
	}
	rows, err := s.db.QueryContext(qctx, query, append(args, clampSmallEventLimit(p.Limit), maxInt(p.Offset, 0))...)
	if err != nil {
		return nil, 0, fmt.Errorf("query small analytics events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]doris.EventRow, 0, clampSmallEventLimit(p.Limit))
	for rows.Next() {
		ev, err := scanConnectionEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, ev.eventRow())
	}
	return out, total, rows.Err()
}

// BuildTimeline returns a bounded connection timeline from the local SQLite
// read model. Broader file/db/web/process timelines remain an OLAP feature until
// their typed facts are mirrored into SQLite.
func (s *Store) BuildTimeline(ctx context.Context, p doris.TimelineBuildParams) ([]doris.TimelineItem, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("small analytics unavailable")
	}
	query, args, err := buildConnectionTimelineSQL(p)
	if err != nil {
		return nil, err
	}
	qctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(qctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("build small analytics timeline: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]doris.TimelineItem, 0, clampSmallTimelineLimit(p.Limit))
	for rows.Next() {
		ev, err := scanConnectionEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev.timelineItem())
	}
	return out, rows.Err()
}

func (s *Store) TopTalkers(ctx context.Context, tenantID string, since time.Time, limit int) ([]doris.TopTalker, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := s.ListConnectionsForTenant(ctx, tenantID, since, time.Now().UTC(), 1000, true)
	if err != nil {
		return nil, err
	}
	byIP := map[string]*doris.TopTalker{}
	for _, row := range rows {
		ip := connectionPeer(row)
		if ip == "" || !publicRoutableIP(net.ParseIP(ip)) {
			continue
		}
		t := byIP[ip]
		if t == nil {
			t = &doris.TopTalker{IP: ip}
			byIP[ip] = t
		}
		t.Connections++
		t.BytesIn += row.BytesIn
		t.BytesOut += row.BytesOut
		if row.ThreatMatch {
			t.ThreatHits++
		}
	}
	out := make([]doris.TopTalker, 0, len(byIP))
	for _, row := range byIP {
		out = append(out, *row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Connections != out[j].Connections {
			return out[i].Connections > out[j].Connections
		}
		if out[i].BytesOut != out[j].BytesOut {
			return out[i].BytesOut > out[j].BytesOut
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

const selectConnectionColumns = `SELECT conn_id, correlation_id, started_at_ms, ended_at_ms, duration_ms, direction,
	pid, process_name, cmdline, user_name,
	src_ip, src_port, dst_ip, dst_port, protocol,
	bytes_in, bytes_out, packets_in, packets_out,
	threat_match, threat_feed, closed_reason, bastion_session_id, node_id`

func (s *Store) queryConnections(ctx context.Context, where string, args []any, limit int, externalOnly bool) ([]doris.ConnectionRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("small analytics unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	scanLimit := limit
	if externalOnly {
		scanLimit = limit * 10
		if scanLimit < 100 {
			scanLimit = 100
		}
		if scanLimit > 5000 {
			scanLimit = 5000
		}
	}
	qctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	query := selectConnectionColumns + `
		FROM process_connections
		WHERE ` + where + `
		ORDER BY started_at_ms DESC
		LIMIT ?`
	args = append(args, scanLimit)
	rows, err := s.db.QueryContext(qctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]doris.ConnectionRow, 0, limit)
	seen := map[string]struct{}{}
	for rows.Next() {
		row, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		if externalOnly && !connectionPeerIsPublic(row) {
			continue
		}
		key := connectionDedupeKey(row)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

type connectionEvent struct {
	TenantID       string
	RowKey         string
	ConnID         string
	CorrelationID  string
	StartedAt      time.Time
	EndedAt        time.Time
	TS             time.Time
	DurationMS     int64
	Direction      string
	Phase          string
	EventType      string
	Severity       string
	PID            int64
	ProcessName    string
	Cmdline        string
	UserName       string
	SrcIP          string
	SrcPort        int
	DstIP          string
	DstPort        int
	Protocol       string
	BytesIn        int64
	BytesOut       int64
	PacketsIn      int64
	PacketsOut     int64
	ThreatMatch    bool
	ThreatFeed     string
	ClosedReason   string
	BastionSession string
	NodeID         string
	EventID        string
	RawRef         string
	Message        string
}

func buildConnectionEventQuerySQL(p doris.EventQueryParams) (string, string, []any, error) {
	where, args, err := connectionEventWhereClause(p)
	if err != nil {
		return "", "", nil, err
	}
	base := connectionEventProjectionSQL()
	selectSQL := selectConnectionEventColumns + `
		FROM (` + base + `) connection_events
		WHERE ` + where + `
		ORDER BY ts_ms DESC, event_id DESC
		LIMIT ? OFFSET ?`
	countSQL := `SELECT COUNT(*) FROM (` + base + `) connection_events WHERE ` + where
	return selectSQL, countSQL, args, nil
}

func buildConnectionTimelineSQL(p doris.TimelineBuildParams) (string, []any, error) {
	where, args, err := connectionTimelineWhereClause(p)
	if err != nil {
		return "", nil, err
	}
	query := selectConnectionEventColumns + `
		FROM (` + connectionEventProjectionSQL() + `) connection_events
		WHERE ` + where + `
		ORDER BY ts_ms DESC, event_id DESC
		LIMIT ?`
	args = append(args, clampSmallTimelineLimit(p.Limit))
	return query, args, nil
}

const selectConnectionEventColumns = `SELECT tenant_id, row_key, conn_id, correlation_id,
	started_at_ms, ended_at_ms, ts_ms, duration_ms, direction, event_phase, event_type, severity,
	pid, process_name, cmdline, user_name,
	src_ip, src_port, dst_ip, dst_port, protocol,
	bytes_in, bytes_out, packets_in, packets_out,
	threat_match, threat_feed, closed_reason, bastion_session_id, node_id,
	event_id, raw_ref, message`

func connectionEventProjectionSQL() string {
	return `
		SELECT tenant_id, row_key, conn_id, correlation_id,
		       started_at_ms, ended_at_ms, started_at_ms AS ts_ms,
		       duration_ms, direction, 'open' AS event_phase, 'conn.open' AS event_type,
		       CASE WHEN threat_match != 0 THEN 'high' ELSE 'info' END AS severity,
		       pid, process_name, cmdline, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, packets_in, packets_out,
		       threat_match, threat_feed, closed_reason, bastion_session_id, node_id,
		       'conn:' || COALESCE(NULLIF(conn_id, ''), row_key) || ':open' AS event_id,
		       'smallanalytics://process_connections/' || tenant_id || '/' || COALESCE(NULLIF(conn_id, ''), row_key) || '/open' AS raw_ref,
		       TRIM(COALESCE(NULLIF(process_name, ''), 'process') || ' opened ' ||
		            COALESCE(src_ip, '') || ':' || CAST(COALESCE(src_port, 0) AS TEXT) ||
		            ' -> ' || COALESCE(dst_ip, '') || ':' || CAST(COALESCE(dst_port, 0) AS TEXT)) AS message,
		       CASE WHEN direction != '' THEN 'conn.' || direction ELSE 'conn.flow' END AS direction_event_type
		FROM process_connections
		UNION ALL
		SELECT tenant_id, row_key, conn_id, correlation_id,
		       started_at_ms, ended_at_ms, ended_at_ms AS ts_ms,
		       duration_ms, direction, 'close' AS event_phase, 'conn.close' AS event_type,
		       CASE WHEN threat_match != 0 THEN 'high' ELSE 'info' END AS severity,
		       pid, process_name, cmdline, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, packets_in, packets_out,
		       threat_match, threat_feed, closed_reason, bastion_session_id, node_id,
		       'conn:' || COALESCE(NULLIF(conn_id, ''), row_key) || ':close' AS event_id,
		       'smallanalytics://process_connections/' || tenant_id || '/' || COALESCE(NULLIF(conn_id, ''), row_key) || '/close' AS raw_ref,
		       TRIM(COALESCE(NULLIF(process_name, ''), 'process') || ' closed ' ||
		            COALESCE(src_ip, '') || ':' || CAST(COALESCE(src_port, 0) AS TEXT) ||
		            ' -> ' || COALESCE(dst_ip, '') || ':' || CAST(COALESCE(dst_port, 0) AS TEXT)) AS message,
		       CASE WHEN direction != '' THEN 'conn.' || direction ELSE 'conn.flow' END AS direction_event_type
		FROM process_connections
		WHERE ended_at_ms IS NOT NULL`
}

func connectionEventWhereClause(p doris.EventQueryParams) (string, []any, error) {
	tenantID := strings.TrimSpace(p.TenantID)
	if tenantID == "" {
		return "", nil, fmt.Errorf("tenant_id required")
	}
	where := []string{"tenant_id = ?"}
	args := []any{tenantID}
	if !p.Since.IsZero() {
		where = append(where, "ts_ms >= ?")
		args = append(args, timeMillis(p.Since))
	}
	if !p.Until.IsZero() {
		where = append(where, "ts_ms <= ?")
		args = append(args, timeMillis(p.Until))
	}
	if v := strings.TrimSpace(p.NodeID); v != "" {
		where = append(where, "node_id = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(p.CorrelationID); v != "" {
		where = append(where, "correlation_id = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(p.ConnID); v != "" {
		where = append(where, "conn_id = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(p.EventID); v != "" {
		where = append(where, "event_id = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(p.RawRef); v != "" {
		where = append(where, "raw_ref = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(p.Severity); v != "" {
		where = append(where, "severity = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(p.ParserStatus); v != "" {
		where = append(where, "? IN ('normalized', 'parsed', '')")
		args = append(args, strings.ToLower(v))
	}
	if types := cleanEventTypes(p.EventTypes); len(types) > 0 {
		placeholders := make([]string, len(types))
		directionPlaceholders := make([]string, len(types))
		for i, typ := range types {
			placeholders[i] = "?"
			directionPlaceholders[i] = "?"
			args = append(args, typ)
		}
		for _, typ := range types {
			args = append(args, typ)
		}
		where = append(where, "(event_type IN ("+strings.Join(placeholders, ", ")+") OR direction_event_type IN ("+strings.Join(directionPlaceholders, ", ")+"))")
	}
	if v := strings.TrimSpace(p.Search); v != "" {
		where = append(where, `LOWER(
			COALESCE(message, '') || ' ' || COALESCE(process_name, '') || ' ' ||
			COALESCE(cmdline, '') || ' ' || COALESCE(user_name, '') || ' ' ||
			COALESCE(src_ip, '') || ' ' || COALESCE(dst_ip, '') || ' ' ||
			COALESCE(threat_feed, '') || ' ' || COALESCE(closed_reason, '')
		) LIKE ?`)
		args = append(args, "%"+strings.ToLower(v)+"%")
	}
	return strings.Join(where, " AND "), args, nil
}

func connectionTimelineWhereClause(p doris.TimelineBuildParams) (string, []any, error) {
	eventParams := doris.EventQueryParams{
		TenantID:      p.TenantID,
		NodeID:        p.NodeID,
		CorrelationID: p.CorrelationID,
		ConnID:        p.ConnID,
		Since:         p.Since,
		Until:         p.Until,
	}
	where, args, err := connectionEventWhereClause(eventParams)
	if err != nil {
		return "", nil, err
	}
	return appendConnectionEntityPredicate(where, args, p.EntityType, p.EntityID)
}

func appendConnectionEntityPredicate(where string, args []any, entityType, entityID string) (string, []any, error) {
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	entityID = strings.TrimSpace(entityID)
	if entityType == "" || entityID == "" {
		return where, args, nil
	}
	switch entityType {
	case "ip":
		args = append(args, entityID, entityID)
		return where + " AND (src_ip = ? OR dst_ip = ?)", args, nil
	case "user":
		args = append(args, entityID)
		return where + " AND user_name = ?", args, nil
	case "process":
		args = append(args, entityID)
		return where + " AND process_name = ?", args, nil
	case "host", "node":
		args = append(args, entityID)
		return where + " AND node_id = ?", args, nil
	case "connection":
		args = append(args, entityID)
		return where + " AND conn_id = ?", args, nil
	case "event":
		args = append(args, entityID)
		return where + " AND event_id = ?", args, nil
	case "raw_ref":
		args = append(args, entityID)
		return where + " AND raw_ref = ?", args, nil
	default:
		return where + " AND 1 = 0", args, nil
	}
}

func scanConnectionEvent(s interface{ Scan(dest ...any) error }) (connectionEvent, error) {
	var ev connectionEvent
	var startedMS, tsMS int64
	var endedMS sql.NullInt64
	var connID, correlationID, direction, phase, eventType, severity sql.NullString
	var processName, cmdline, userName, srcIP, dstIP, protocol sql.NullString
	var threatFeed, closedReason, bastionSession, nodeID, eventID, rawRef, message sql.NullString
	var durationMS, pid, srcPort, dstPort, bytesIn, bytesOut, packetsIn, packetsOut sql.NullInt64
	var threatMatch sql.NullInt64
	if err := s.Scan(&ev.TenantID, &ev.RowKey, &connID, &correlationID,
		&startedMS, &endedMS, &tsMS, &durationMS, &direction, &phase, &eventType, &severity,
		&pid, &processName, &cmdline, &userName,
		&srcIP, &srcPort, &dstIP, &dstPort, &protocol,
		&bytesIn, &bytesOut, &packetsIn, &packetsOut,
		&threatMatch, &threatFeed, &closedReason, &bastionSession, &nodeID,
		&eventID, &rawRef, &message); err != nil {
		return ev, err
	}
	ev.ConnID = connID.String
	ev.CorrelationID = correlationID.String
	ev.StartedAt = millisTime(startedMS)
	if endedMS.Valid {
		ev.EndedAt = millisTime(endedMS.Int64)
	}
	ev.TS = millisTime(tsMS)
	ev.DurationMS = durationMS.Int64
	ev.Direction = direction.String
	ev.Phase = phase.String
	ev.EventType = eventType.String
	ev.Severity = severity.String
	ev.PID = pid.Int64
	ev.ProcessName = processName.String
	ev.Cmdline = cmdline.String
	ev.UserName = userName.String
	ev.SrcIP = srcIP.String
	ev.SrcPort = int(srcPort.Int64)
	ev.DstIP = dstIP.String
	ev.DstPort = int(dstPort.Int64)
	ev.Protocol = protocol.String
	ev.BytesIn = bytesIn.Int64
	ev.BytesOut = bytesOut.Int64
	ev.PacketsIn = packetsIn.Int64
	ev.PacketsOut = packetsOut.Int64
	ev.ThreatMatch = threatMatch.Int64 != 0
	ev.ThreatFeed = threatFeed.String
	ev.ClosedReason = closedReason.String
	ev.BastionSession = bastionSession.String
	ev.NodeID = nodeID.String
	ev.EventID = eventID.String
	ev.RawRef = rawRef.String
	ev.Message = message.String
	return ev, nil
}

func (ev connectionEvent) eventRow() doris.EventRow {
	threatScore := 0
	if ev.ThreatMatch {
		threatScore = 100
	}
	return doris.EventRow{
		SchemaVersion: 1,
		EventID:       ev.EventID,
		RawRef:        ev.RawRef,
		Collector:     "small-analytics",
		Parser:        "process_connections",
		ParserStatus:  "normalized",
		TenantID:      ev.TenantID,
		TS:            ev.TS,
		NodeID:        ev.NodeID,
		EventType:     ev.EventType,
		Severity:      ev.Severity,
		CorrelationID: ev.CorrelationID,
		ConnID:        ev.ConnID,
		BastionSessID: ev.BastionSession,
		PID:           ev.PID,
		ProcessName:   ev.ProcessName,
		UserName:      ev.UserName,
		SrcIP:         ev.SrcIP,
		SrcPort:       ev.SrcPort,
		DstIP:         ev.DstIP,
		DstPort:       ev.DstPort,
		Protocol:      ev.Protocol,
		BytesIn:       ev.BytesIn,
		BytesOut:      ev.BytesOut,
		DurationMS:    ev.DurationMS,
		ThreatFeed:    ev.ThreatFeed,
		ThreatScore:   threatScore,
		Message:       ev.Message,
		DetailsJSON:   ev.detailsJSON(),
		DedupKey:      ev.EventID,
	}
}

func (ev connectionEvent) timelineItem() doris.TimelineItem {
	return doris.TimelineItem{
		SourceTable:   "process_connections",
		SchemaVersion: 1,
		EventID:       ev.EventID,
		RawRef:        ev.RawRef,
		Collector:     "small-analytics",
		Parser:        "process_connections",
		ParserStatus:  "normalized",
		TenantID:      ev.TenantID,
		TS:            ev.TS,
		NodeID:        ev.NodeID,
		EventType:     ev.EventType,
		Severity:      ev.Severity,
		Message:       ev.Message,
		CorrelationID: ev.CorrelationID,
		ConnID:        ev.ConnID,
		PID:           ev.PID,
		ProcessName:   ev.ProcessName,
		UserName:      ev.UserName,
		SrcIP:         ev.SrcIP,
		DstIP:         ev.DstIP,
		DstPort:       ev.DstPort,
		BytesIn:       ev.BytesIn,
		BytesOut:      ev.BytesOut,
		DetailsJSON:   ev.detailsJSON(),
	}
}

func (ev connectionEvent) detailsJSON() string {
	details := map[string]any{
		"source_table":       "process_connections",
		"event_phase":        ev.Phase,
		"direction":          ev.Direction,
		"started_at":         ev.StartedAt.UTC().Format(time.RFC3339Nano),
		"duration_ms":        ev.DurationMS,
		"src_port":           ev.SrcPort,
		"dst_port":           ev.DstPort,
		"protocol":           ev.Protocol,
		"packets_in":         ev.PacketsIn,
		"packets_out":        ev.PacketsOut,
		"threat_match":       ev.ThreatMatch,
		"threat_feed":        ev.ThreatFeed,
		"closed_reason":      ev.ClosedReason,
		"bastion_session_id": ev.BastionSession,
		"cmdline":            ev.Cmdline,
	}
	if !ev.EndedAt.IsZero() {
		details["ended_at"] = ev.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	body, err := json.Marshal(details)
	if err != nil {
		return ""
	}
	return string(body)
}

func cleanEventTypes(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func clampSmallEventLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func clampSmallTimelineLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func scanConnection(s interface{ Scan(dest ...any) error }) (doris.ConnectionRow, error) {
	var row doris.ConnectionRow
	var startedMS int64
	var endedMS sql.NullInt64
	var connID, correlationID, direction, processName, cmdline, userName sql.NullString
	var srcIP, dstIP, protocol, threatFeed, closedReason, bastionSession, nodeID sql.NullString
	var durationMS, pid, srcPort, dstPort, bytesIn, bytesOut, packetsIn, packetsOut sql.NullInt64
	var threatMatch sql.NullInt64
	if err := s.Scan(&connID, &correlationID, &startedMS, &endedMS, &durationMS, &direction,
		&pid, &processName, &cmdline, &userName,
		&srcIP, &srcPort, &dstIP, &dstPort, &protocol,
		&bytesIn, &bytesOut, &packetsIn, &packetsOut,
		&threatMatch, &threatFeed, &closedReason, &bastionSession, &nodeID); err != nil {
		return row, err
	}
	row.ConnID = connID.String
	row.CorrelationID = correlationID.String
	row.StartedAt = millisTime(startedMS)
	if endedMS.Valid {
		row.EndedAt = millisTime(endedMS.Int64)
	}
	row.DurationMS = durationMS.Int64
	row.Direction = direction.String
	row.PID = pid.Int64
	row.ProcessName = processName.String
	row.Cmdline = cmdline.String
	row.UserName = userName.String
	row.SrcIP = srcIP.String
	row.SrcPort = int(srcPort.Int64)
	row.DstIP = dstIP.String
	row.DstPort = int(dstPort.Int64)
	row.Protocol = protocol.String
	row.BytesIn = bytesIn.Int64
	row.BytesOut = bytesOut.Int64
	row.PacketsIn = packetsIn.Int64
	row.PacketsOut = packetsOut.Int64
	row.ThreatMatch = threatMatch.Int64 != 0
	row.ThreatFeed = threatFeed.String
	row.ClosedReason = closedReason.String
	row.BastionSession = bastionSession.String
	row.NodeID = nodeID.String
	return row, nil
}

type connectionRecord struct {
	TenantID       string
	RowKey         string
	ConnID         string
	CorrelationID  string
	StartedAt      time.Time
	EndedAt        time.Time
	LastSeen       time.Time
	DurationMS     int64
	Direction      string
	PID            int64
	ProcessName    string
	Cmdline        string
	UserName       string
	SrcIP          string
	SrcPort        int
	DstIP          string
	DstPort        int
	Protocol       string
	BytesIn        int64
	BytesOut       int64
	PacketsIn      int64
	PacketsOut     int64
	ThreatMatch    bool
	ThreatFeed     string
	ClosedReason   string
	BastionSession string
	NodeID         string
}

func parseConnectionMap(raw map[string]any) connectionRecord {
	rec := connectionRecord{
		TenantID:       stringValue(raw["tenant_id"]),
		NodeID:         stringValue(raw["node_id"]),
		ConnID:         stringValue(raw["conn_id"]),
		CorrelationID:  stringValue(raw["correlation_id"]),
		StartedAt:      timeValue(raw["started_at"]),
		EndedAt:        timeValue(raw["ended_at"]),
		DurationMS:     int64Value(raw["duration_ms"]),
		Direction:      stringValue(raw["direction"]),
		PID:            int64Value(raw["pid"]),
		ProcessName:    stringValue(raw["process_name"]),
		Cmdline:        stringValue(raw["cmdline"]),
		UserName:       stringValue(raw["user_name"]),
		SrcIP:          stringValue(raw["src_ip"]),
		SrcPort:        int(int64Value(raw["src_port"])),
		DstIP:          stringValue(raw["dst_ip"]),
		DstPort:        int(int64Value(raw["dst_port"])),
		Protocol:       stringValue(raw["protocol"]),
		BytesIn:        int64Value(raw["bytes_in"]),
		BytesOut:       int64Value(raw["bytes_out"]),
		PacketsIn:      int64Value(raw["packets_in"]),
		PacketsOut:     int64Value(raw["packets_out"]),
		ThreatMatch:    boolValue(raw["threat_match"]),
		ThreatFeed:     stringValue(raw["threat_feed"]),
		ClosedReason:   stringValue(raw["closed_reason"]),
		BastionSession: stringValue(raw["bastion_session_id"]),
	}
	if !rec.EndedAt.IsZero() {
		rec.LastSeen = rec.EndedAt
	} else {
		rec.LastSeen = rec.StartedAt
	}
	rec.RowKey = rec.ConnID
	if rec.RowKey == "" {
		rec.RowKey = "anon:" + hashParts(rec.TenantID, rec.NodeID, rec.StartedAt.UTC().Format(time.RFC3339Nano), rec.SrcIP, strconv.Itoa(rec.SrcPort), rec.DstIP, strconv.Itoa(rec.DstPort), rec.Protocol, rec.ProcessName)
	}
	return rec
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func int64Value(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case int64:
		return t
	case uint:
		return int64(t)
	case uint8:
		return int64(t)
	case uint16:
		return int64(t)
	case uint32:
		return int64(t)
	case uint64:
		if t > uint64(^uint64(0)>>1) {
			return 0
		}
		return int64(t)
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	default:
		return 0
	}
}

func boolValue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int:
		return t != 0
	case int64:
		return t != 0
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true") || strings.TrimSpace(t) == "1"
	default:
		return false
	}
}

func timeValue(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t.UTC()
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return time.Time{}
		}
		for _, layout := range []string{
			time.RFC3339Nano,
			"2006-01-02 15:04:05.000",
			"2006-01-02 15:04:05",
			"2006-01-02",
		} {
			if parsed, err := time.Parse(layout, s); err == nil {
				return parsed.UTC()
			}
		}
	default:
		if ms := int64Value(t); ms > 0 {
			return millisTime(ms)
		}
	}
	return time.Time{}
}

func nullableTimeMillis(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().UnixMilli()
}

func timeMillis(t time.Time) int64 {
	if t.IsZero() {
		return time.Now().UTC().UnixMilli()
	}
	return t.UTC().UnixMilli()
}

func millisTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ms*int64(time.Millisecond)).UTC()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func hashParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func connectionPeer(row doris.ConnectionRow) string {
	switch strings.ToLower(strings.TrimSpace(row.Direction)) {
	case "inbound":
		return strings.TrimSpace(row.SrcIP)
	case "outbound":
		return strings.TrimSpace(row.DstIP)
	default:
		if strings.TrimSpace(row.DstIP) != "" {
			return strings.TrimSpace(row.DstIP)
		}
		return strings.TrimSpace(row.SrcIP)
	}
}

func connectionPeerIsPublic(row doris.ConnectionRow) bool {
	return publicRoutableIP(net.ParseIP(connectionPeer(row)))
}

func publicRoutableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip = ip.To4()
	if ip == nil {
		return false
	}
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, cidr := range []string{
		"100.64.0.0/10",
		"192.0.2.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err == nil && block.Contains(ip) {
			return false
		}
	}
	return true
}

func connectionDedupeKey(row doris.ConnectionRow) string {
	if row.ConnID != "" {
		return row.ConnID + "|" + row.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + row.SrcIP + "|" + row.DstIP
	}
	return row.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + row.SrcIP + "|" + row.DstIP + "|" + row.ProcessName
}
