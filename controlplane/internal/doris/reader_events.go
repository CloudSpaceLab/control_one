package doris

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ConnectionRow is the shape returned by ListConnectionsForIP +
// ConnectionLifetime. All bytes_in / bytes_out are cumulative for the
// connection lifetime; per-emit deltas live on the events table.
type ConnectionRow struct {
	ConnID         string
	CorrelationID  string
	StartedAt      time.Time
	EndedAt        time.Time
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

// ListConnectionsForIP returns recent connections involving an IP.
func (c *Client) ListConnectionsForIP(ctx context.Context, tenantID, ip string, since, until time.Time, limit int) ([]ConnectionRow, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := withLimit(`
		SELECT conn_id, correlation_id, started_at, ended_at, duration_ms, direction,
		       pid, process_name, cmdline, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, packets_in, packets_out,
		       threat_match, threat_feed, closed_reason, bastion_session_id, node_id
		FROM process_connections
		WHERE tenant_id = ?
		  AND (src_ip = ? OR dst_ip = ?)
		  AND started_at >= ? AND started_at <= ?
		ORDER BY started_at DESC
	`, limit)
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, q, tenantID, ip, ip, since, until)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ConnectionRow, 0, limit)
	for rows.Next() {
		r, err := scanConnectionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// buildListConnectionsForNodeQuery composes the SQL for ListConnectionsForNode.
//
// It is deliberately *single-layer*: the only filtering applied is on
// (tenant_id, node_id, time window, optional ended_at IS NULL). The peer-IP
// classification (RFC1918 vs external) lives ONE layer further out — at the
// agent's `internal/netflow/filter.go` capture-policy boundary — and is NOT
// re-applied here. Re-applying it would double-strip internal flows on
// dev/internal nodes where most peers are private (bugs §1.2). Callers that
// want "external only" should pass an explicit predicate via a future
// parameter instead of post-filtering rows in the UI.
func buildListConnectionsForNodeQuery(limit int, openOnly bool) string {
	openClause := ""
	if openOnly {
		openClause = " AND ended_at IS NULL"
	}
	return withLimit(`
		SELECT conn_id, correlation_id, started_at, ended_at, duration_ms, direction,
		       pid, process_name, cmdline, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, packets_in, packets_out,
		       threat_match, threat_feed, closed_reason, bastion_session_id, node_id
		FROM process_connections
		WHERE tenant_id = ?
		  AND node_id = ?
		  AND started_at >= ? AND started_at <= ?`+openClause+`
		ORDER BY threat_match DESC, started_at DESC
	`, limit)
}

// ListConnectionsForNode returns recent or currently-open connections for one node.
func (c *Client) ListConnectionsForNode(ctx context.Context, tenantID, nodeID string, since, until time.Time, limit int, openOnly bool) ([]ConnectionRow, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := buildListConnectionsForNodeQuery(limit, openOnly)
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, q, tenantID, nodeID, since, until)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ConnectionRow, 0, limit)
	for rows.Next() {
		r, err := scanConnectionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ConnectionLifetime returns the full record for a single conn_id (matches
// open + close + state_change rolls).
func (c *Client) ConnectionLifetime(ctx context.Context, tenantID, connID string) (*ConnectionRow, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	q := `
		SELECT conn_id, correlation_id, started_at, ended_at, duration_ms, direction,
		       pid, process_name, cmdline, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, packets_in, packets_out,
		       threat_match, threat_feed, closed_reason, bastion_session_id, node_id
		FROM process_connections
		WHERE tenant_id = ? AND conn_id = ?
		ORDER BY started_at DESC LIMIT 1
	`
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	row := c.db.QueryRowContext(qctx, q, tenantID, connID)
	out, err := scanConnectionRow(row)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanConnectionRow(s rowScanner) (ConnectionRow, error) {
	var r ConnectionRow
	var startedAt, endedAt *time.Time
	if err := s.Scan(&r.ConnID, &r.CorrelationID, &startedAt, &endedAt, &r.DurationMS, &r.Direction,
		&r.PID, &r.ProcessName, &r.Cmdline, &r.UserName,
		&r.SrcIP, &r.SrcPort, &r.DstIP, &r.DstPort, &r.Protocol,
		&r.BytesIn, &r.BytesOut, &r.PacketsIn, &r.PacketsOut,
		&r.ThreatMatch, &r.ThreatFeed, &r.ClosedReason, &r.BastionSession, &r.NodeID); err != nil {
		return r, err
	}
	if startedAt != nil {
		r.StartedAt = *startedAt
	}
	if endedAt != nil {
		r.EndedAt = *endedAt
	}
	return r, nil
}

// CorrelationEvent is one entry in the unified timeline.
type CorrelationEvent struct {
	TS        time.Time
	EventType string
	Severity  string
	Message   string
	PID       int64
	Process   string
	Path      string
	BytesIn   int64
	BytesOut  int64
	SrcIP     string
	DstIP     string
	DstPort   int
	NodeID    string
	ConnID    string
}

// ListEventsByCorrelation returns every event sharing the same
// correlation_id, in time order. Joins across events + file_accesses +
// db_queries. Used by the UI's forensic timeline.
func (c *Client) ListEventsByCorrelation(ctx context.Context, tenantID, correlationID string) ([]CorrelationEvent, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	q := `
		SELECT ts, event_type, severity, message, pid, process_name,
		       '' AS path, bytes_in, bytes_out, src_ip, dst_ip, dst_port, node_id, conn_id
		FROM events
		WHERE tenant_id = ? AND correlation_id = ?
		UNION ALL
		SELECT ts, CONCAT('file.', op) AS event_type, 'info' AS severity, path AS message, pid, process_name,
		       path, bytes, 0, '', '', 0, node_id, conn_id
		FROM file_accesses
		WHERE tenant_id = ? AND correlation_id = ?
		UNION ALL
		SELECT ts, 'db.query' AS event_type, 'info' AS severity, query_text AS message, pid, '',
		       '', 0, exec_time_ms, src_ip, '', 0, node_id, conn_id
		FROM db_queries
		WHERE tenant_id = ? AND correlation_id = ?
		ORDER BY ts
		LIMIT 5000
	`
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, q,
		tenantID, correlationID,
		tenantID, correlationID,
		tenantID, correlationID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []CorrelationEvent{}
	for rows.Next() {
		var e CorrelationEvent
		var bytesOut int64
		if err := rows.Scan(&e.TS, &e.EventType, &e.Severity, &e.Message, &e.PID, &e.Process,
			&e.Path, &e.BytesIn, &bytesOut, &e.SrcIP, &e.DstIP, &e.DstPort, &e.NodeID, &e.ConnID); err != nil {
			return nil, err
		}
		e.BytesOut = bytesOut
		out = append(out, e)
	}
	return out, rows.Err()
}

// TopTalker is one row in the per-tenant Top Talkers card.
type TopTalker struct {
	IP          string
	Connections int64
	BytesIn     int64
	BytesOut    int64
	ThreatHits  int64
}

// TopTalkers returns the busiest external peers by connection count.
func (c *Client) TopTalkers(ctx context.Context, tenantID string, since time.Time, limit int) ([]TopTalker, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	q := withLimit(`
		SELECT dst_ip,
		       COUNT(*)                                     AS conn_count,
		       SUM(bytes_in)                                AS bytes_in,
		       SUM(bytes_out)                               AS bytes_out,
		       SUM(CASE WHEN threat_match THEN 1 ELSE 0 END) AS threat_hits
		FROM process_connections
		WHERE tenant_id = ? AND started_at >= ?
		GROUP BY dst_ip
		ORDER BY conn_count DESC
	`, limit)
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, q, tenantID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []TopTalker{}
	for rows.Next() {
		var t TopTalker
		if err := rows.Scan(&t.IP, &t.Connections, &t.BytesIn, &t.BytesOut, &t.ThreatHits); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FleetSnapshotRow rolls one node's hot vitals into a single row for the
// dashboard topology grid.
type FleetSnapshotRow struct {
	NodeID        string
	ConnsActive   int64
	BytesOut24h   int64
	BytesIn24h    int64
	ThreatHits24h int64
	OpenAlerts    int64
	LastEventAt   time.Time
	SeverityMax   string
}

// FleetHealthSnapshot returns one row per node summarising the last 24h.
// Powered by the events_per_hour_mv when present, falls back to live
// aggregation otherwise.
func (c *Client) FleetHealthSnapshot(ctx context.Context, tenantID string, since time.Time) ([]FleetSnapshotRow, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	q := `
		SELECT node_id,
		       COUNT(DISTINCT conn_id)                       AS conns_active,
		       SUM(bytes_in)                                  AS bytes_in,
		       SUM(bytes_out)                                 AS bytes_out,
		       SUM(CASE WHEN threat_feed != '' THEN 1 ELSE 0 END) AS threat_hits,
		       MAX(ts)                                        AS last_event_at,
		       MAX(severity)                                  AS sev_max
		FROM events
		WHERE tenant_id = ? AND ts >= ?
		GROUP BY node_id
		ORDER BY threat_hits DESC, conns_active DESC
		LIMIT 1000
	`
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, q, tenantID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []FleetSnapshotRow{}
	for rows.Next() {
		var r FleetSnapshotRow
		var lastAt *time.Time
		if err := rows.Scan(&r.NodeID, &r.ConnsActive, &r.BytesIn24h, &r.BytesOut24h, &r.ThreatHits24h, &lastAt, &r.SeverityMax); err != nil {
			return nil, err
		}
		if lastAt != nil {
			r.LastEventAt = *lastAt
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LogVolumeBucketed returns per-bucket message counts so the dashboard can
// detect spikes. bucket is a Go duration in seconds (300 = 5 min).
func (c *Client) LogVolumeBucketed(ctx context.Context, tenantID, nodeID string, since, until time.Time, bucket time.Duration) (map[time.Time]int64, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if bucket <= 0 {
		bucket = 5 * time.Minute
	}
	bucketSec := int64(bucket.Seconds())
	where := []string{"tenant_id = ?", "ts >= ?", "ts <= ?", "event_type IN ('log.line','log.spike')"}
	args := []any{tenantID, since, until}
	if nodeID != "" {
		where = append(where, "node_id = ?")
		args = append(args, nodeID)
	}
	q := fmt.Sprintf(`
		SELECT FROM_UNIXTIME(UNIX_TIMESTAMP(ts) - UNIX_TIMESTAMP(ts) %% %d) AS bucket_ts,
		       COUNT(*) AS cnt
		FROM events
		WHERE %s
		GROUP BY bucket_ts
		ORDER BY bucket_ts
	`, bucketSec, strings.Join(where, " AND "))
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[time.Time]int64{}
	for rows.Next() {
		var ts time.Time
		var cnt int64
		if err := rows.Scan(&ts, &cnt); err != nil {
			return nil, err
		}
		out[ts] = cnt
	}
	return out, rows.Err()
}
