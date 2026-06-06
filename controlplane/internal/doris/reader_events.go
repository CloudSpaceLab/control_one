package doris

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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

const connectionSelectColumns = `conn_id, correlation_id, started_at, ended_at, duration_ms, direction,
		       pid, process_name, cmdline, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, packets_in, packets_out,
		       threat_match, threat_feed, closed_reason, bastion_session_id, node_id`

const connectionPeerNumberExpr = `INET_ATON(CASE
		           WHEN direction = 'inbound' THEN src_ip
		           WHEN direction = 'outbound' THEN dst_ip
		           WHEN dst_ip IS NOT NULL AND dst_ip != '' THEN dst_ip
		           ELSE src_ip
		       END)`

// ListConnectionsForIP returns recent connections involving an IP.
func (c *Client) ListConnectionsForIP(ctx context.Context, tenantID, ip string, since, until time.Time, limit int) ([]ConnectionRow, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()

	out := make([]ConnectionRow, 0, limit)
	seen := map[string]struct{}{}
	for _, day := range connectionEventDays(since, until, 14) {
		for _, peerColumn := range []string{"src_ip", "dst_ip"} {
			remaining := limit - len(out)
			if remaining <= 0 {
				break
			}
			q := buildListConnectionsForIPDayQuery(peerColumn, remaining)
			rows, err := queryConnectionRows(qctx, c.db, q, remaining, day.Format("2006-01-02"), tenantID, ip, until, since)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				key := connectionDedupeKey(row)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, row)
			}
		}
		if len(out) >= limit {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func buildListConnectionsForIPDayQuery(peerColumn string, limit int) string {
	if peerColumn != "dst_ip" {
		peerColumn = "src_ip"
	}
	return withLimit(fmt.Sprintf(`
		SELECT %s
		FROM process_connections
		WHERE event_date = ?
		  AND tenant_id = ?
		  AND `+peerColumn+` = ?
		  AND started_at <= ?
		  AND (ended_at IS NULL OR ended_at >= ?)
	`, connectionSelectColumns), limit)
}

// buildListConnectionsForNodeDayQuery composes the SQL for ListConnectionsForNode.
//
// It is deliberately *single-layer*: the only filtering applied is on
// (tenant_id, node_id, time window overlap, optional ended_at IS NULL). The peer-IP
// classification (RFC1918 vs external) lives ONE layer further out — at the
// agent's `internal/netflow/filter.go` capture-policy boundary — and is NOT
// not re-applied on the full-scope path. Re-applying it there would
// double-strip internal flows on dev/internal nodes where most peers are
// private (bugs §1.2). Callers that
// externalOnly is explicit and reserved for default UI views; full-scope
// callers still receive private peers for operator drilldown.
func buildListConnectionsForNodeDayQuery(limit int, openOnly, externalOnly bool) string {
	where := `
		WHERE event_date = ?
		  AND tenant_id = ?
		  AND node_id = ?
		  AND started_at <= ?`
	if openOnly {
		where += `
		  AND ended_at IS NULL`
	} else {
		where += `
		  AND (ended_at IS NULL OR ended_at >= ?)`
	}
	if externalOnly {
		return withLimit(fmt.Sprintf(`
		SELECT %s
		FROM (
			SELECT %s,
			       %s AS peer_num
			FROM process_connections
			%s
		) pc
		WHERE %s
		ORDER BY started_at DESC
	`, connectionSelectColumns, connectionSelectColumns, connectionPeerNumberExpr, where, dorisPublicPeerPredicate("peer_num")), limit)
	}
	return withLimit(fmt.Sprintf(`
		SELECT %s
		FROM process_connections
		%s
		ORDER BY started_at DESC
	`, connectionSelectColumns, where), limit)
}

func buildListConnectionsForNodeQuery(limit int, openOnly bool) string {
	return buildListConnectionsForNodeDayQuery(limit, openOnly, false)
}

// ListConnectionsForNode returns recent or currently-open connections for one node.
func (c *Client) ListConnectionsForNode(ctx context.Context, tenantID, nodeID string, since, until time.Time, limit int, openOnly, externalOnly bool) ([]ConnectionRow, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()

	out := make([]ConnectionRow, 0, limit)
	seen := map[string]struct{}{}
	for _, day := range connectionEventDays(since, until, 14) {
		remaining := limit - len(out)
		if remaining <= 0 {
			break
		}
		q := buildListConnectionsForNodeDayQuery(remaining, openOnly, externalOnly)
		args := []any{day.Format("2006-01-02"), tenantID, nodeID, until}
		if !openOnly {
			args = append(args, since)
		}
		rows, err := queryConnectionRows(qctx, c.db, q, remaining, args...)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			key := connectionDedupeKey(row)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListConnectionsForTenant returns recent fleet-wide connections for a tenant.
// The caller decides whether to display internal/listener rows; this method
// keeps the analytic query broad enough for operator drilldown and UI filters.
func (c *Client) ListConnectionsForTenant(ctx context.Context, tenantID string, since, until time.Time, limit int, externalOnly bool) ([]ConnectionRow, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()

	out := make([]ConnectionRow, 0, limit)
	seen := map[string]struct{}{}
	for _, day := range connectionEventDays(since, until, 14) {
		remaining := limit - len(out)
		if remaining <= 0 {
			break
		}
		q := buildListConnectionsForTenantDayQuery(remaining, externalOnly)
		rows, err := queryConnectionRows(qctx, c.db, q, remaining, day.Format("2006-01-02"), tenantID, until, since)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			key := connectionDedupeKey(row)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func buildListConnectionsForTenantDayQuery(limit int, externalOnly bool) string {
	where := `
		WHERE event_date = ?
		  AND tenant_id = ?
		  AND started_at <= ?
		  AND (ended_at IS NULL OR ended_at >= ?)`
	if externalOnly {
		return withLimit(fmt.Sprintf(`
		SELECT %s
		FROM (
			SELECT %s,
			       %s AS peer_num
			FROM process_connections
			%s
		) pc
		WHERE %s
		ORDER BY started_at DESC
	`, connectionSelectColumns, connectionSelectColumns, connectionPeerNumberExpr, where, dorisPublicPeerPredicate("peer_num")), limit)
	}
	return withLimit(fmt.Sprintf(`
		SELECT %s
		FROM process_connections
		%s
		ORDER BY started_at DESC
	`, connectionSelectColumns, where), limit)
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
	var connID, correlationID, direction, processName, cmdline, userName sql.NullString
	var srcIP, dstIP, protocol, threatFeed, closedReason, bastionSession, nodeID sql.NullString
	var durationMS, pid, srcPort, dstPort, bytesIn, bytesOut, packetsIn, packetsOut sql.NullInt64
	var threatMatch sql.NullBool
	if err := s.Scan(&connID, &correlationID, &startedAt, &endedAt, &durationMS, &direction,
		&pid, &processName, &cmdline, &userName,
		&srcIP, &srcPort, &dstIP, &dstPort, &protocol,
		&bytesIn, &bytesOut, &packetsIn, &packetsOut,
		&threatMatch, &threatFeed, &closedReason, &bastionSession, &nodeID); err != nil {
		return r, err
	}
	r.ConnID = connID.String
	r.CorrelationID = correlationID.String
	if startedAt != nil {
		r.StartedAt = *startedAt
	}
	if endedAt != nil {
		r.EndedAt = *endedAt
	}
	if durationMS.Valid {
		r.DurationMS = durationMS.Int64
	}
	r.Direction = direction.String
	if pid.Valid {
		r.PID = pid.Int64
	}
	r.ProcessName = processName.String
	r.Cmdline = cmdline.String
	r.UserName = userName.String
	r.SrcIP = srcIP.String
	if srcPort.Valid {
		r.SrcPort = int(srcPort.Int64)
	}
	r.DstIP = dstIP.String
	if dstPort.Valid {
		r.DstPort = int(dstPort.Int64)
	}
	r.Protocol = protocol.String
	if bytesIn.Valid {
		r.BytesIn = bytesIn.Int64
	}
	if bytesOut.Valid {
		r.BytesOut = bytesOut.Int64
	}
	if packetsIn.Valid {
		r.PacketsIn = packetsIn.Int64
	}
	if packetsOut.Valid {
		r.PacketsOut = packetsOut.Int64
	}
	if threatMatch.Valid {
		r.ThreatMatch = threatMatch.Bool
	}
	r.ThreatFeed = threatFeed.String
	r.ClosedReason = closedReason.String
	r.BastionSession = bastionSession.String
	r.NodeID = nodeID.String
	return r, nil
}

func queryConnectionRows(ctx context.Context, db *sql.DB, query string, limit int, args ...any) ([]ConnectionRow, error) {
	rows, err := db.QueryContext(ctx, query, args...)
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

func dorisPublicPeerPredicate(peerNum string) string {
	return fmt.Sprintf(`%s BETWEEN INET_ATON('1.0.0.0') AND INET_ATON('223.255.255.255')
		  AND NOT (%s BETWEEN INET_ATON('10.0.0.0') AND INET_ATON('10.255.255.255'))
		  AND NOT (%s BETWEEN INET_ATON('100.64.0.0') AND INET_ATON('100.127.255.255'))
		  AND NOT (%s BETWEEN INET_ATON('127.0.0.0') AND INET_ATON('127.255.255.255'))
		  AND NOT (%s BETWEEN INET_ATON('169.254.0.0') AND INET_ATON('169.254.255.255'))
		  AND NOT (%s BETWEEN INET_ATON('172.16.0.0') AND INET_ATON('172.31.255.255'))
		  AND NOT (%s BETWEEN INET_ATON('192.168.0.0') AND INET_ATON('192.168.255.255'))
		  AND NOT (%s BETWEEN INET_ATON('192.0.2.0') AND INET_ATON('192.0.2.255'))
		  AND NOT (%s BETWEEN INET_ATON('198.18.0.0') AND INET_ATON('198.19.255.255'))
		  AND NOT (%s BETWEEN INET_ATON('198.51.100.0') AND INET_ATON('198.51.100.255'))
		  AND NOT (%s BETWEEN INET_ATON('203.0.113.0') AND INET_ATON('203.0.113.255'))`,
		peerNum, peerNum, peerNum, peerNum, peerNum, peerNum, peerNum, peerNum, peerNum, peerNum, peerNum)
}

func connectionEventDays(since, until time.Time, maxDays int) []time.Time {
	if until.IsZero() {
		until = time.Now().UTC()
	}
	if since.IsZero() {
		since = until.Add(-24 * time.Hour)
	}
	if since.After(until) {
		since, until = until, since
	}
	if maxDays <= 0 {
		maxDays = 14
	}
	minSince := until.AddDate(0, 0, -(maxDays - 1))
	if since.Before(minSince) {
		since = minSince
	}
	start := dateOnlyUTC(since)
	end := dateOnlyUTC(until)
	days := []time.Time{}
	for day := end; !day.Before(start); day = day.AddDate(0, 0, -1) {
		days = append(days, day)
	}
	return days
}

func dateOnlyUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func connectionDedupeKey(row ConnectionRow) string {
	if row.ConnID != "" {
		return row.ConnID + "|" + row.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + row.SrcIP + "|" + row.DstIP
	}
	return row.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + row.SrcIP + "|" + row.DstIP + "|" + row.ProcessName
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

// EventQueryParams narrows a normalized event query against the Doris events
// table. TenantID is mandatory so callers cannot accidentally scan or leak
// cross-tenant data.
type EventQueryParams struct {
	TenantID      string
	NodeID        string
	CorrelationID string
	ConnID        string
	EventID       string
	RawRef        string
	EventTypes    []string
	Severity      string
	ParserStatus  string
	Search        string
	Since         time.Time
	Until         time.Time
	Limit         int
	Offset        int
}

// EventRow is the normalized event projection used by investigation APIs and
// AI citations.
type EventRow struct {
	SchemaVersion int
	EventID       string
	RawRef        string
	Collector     string
	Parser        string
	ParserStatus  string
	TenantID      string
	TS            time.Time
	NodeID        string
	EventType     string
	Severity      string
	CorrelationID string
	ConnID        string
	BastionSessID string
	PID           int64
	ProcessName   string
	UserName      string
	SrcIP         string
	SrcPort       int
	DstIP         string
	DstPort       int
	Protocol      string
	BytesIn       int64
	BytesOut      int64
	DurationMS    int64
	RuleID        string
	ThreatFeed    string
	ThreatScore   int
	Message       string
	DetailsJSON   string
	DedupKey      string
}

// QueryEvents returns normalized rows from the generic signal table plus
// canonical typed fact tables, ordered newest first.
func (c *Client) QueryEvents(ctx context.Context, p EventQueryParams) ([]EventRow, int, error) {
	if c == nil || c.db == nil {
		return nil, 0, fmt.Errorf("doris client unavailable")
	}
	query, countQuery, args, err := buildEventQuerySQL(p)
	if err != nil {
		return nil, 0, err
	}
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()

	var total int
	if err := c.db.QueryRowContext(qctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count events: %w", err)
	}
	rows, err := c.db.QueryContext(qctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]EventRow, 0, clampEventQueryLimit(p.Limit))
	for rows.Next() {
		row, err := scanEventRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

func buildEventQuerySQL(p EventQueryParams) (string, string, []any, error) {
	where, args, err := eventWhereClause(p, "ts")
	if err != nil {
		return "", "", nil, err
	}
	unionSQL := eventQueryUnionSQL()
	selectSQL := `
		SELECT schema_version, event_id, raw_ref, collector, parser, parser_status,
		       tenant_id, ts, node_id, event_type, severity, correlation_id, conn_id,
		       bastion_session_id, pid, process_name, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, duration_ms, rule_id, threat_feed, threat_score,
		       message, details_json, dedup_key
		FROM (` + unionSQL + `) normalized_events
		WHERE ` + where + `
		ORDER BY ts DESC`
	query := withLimitOffset(selectSQL, clampEventQueryLimit(p.Limit), maxInt(p.Offset, 0))
	countQuery := "SELECT COUNT(*) FROM (" + unionSQL + ") normalized_events WHERE " + where
	return query, countQuery, args, nil
}

func eventQueryUnionSQL() string {
	return `
		SELECT schema_version, event_id, raw_ref, collector, parser, parser_status,
		       tenant_id, ts, node_id, event_type, severity, correlation_id, conn_id,
		       bastion_session_id, pid, process_name, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, duration_ms, rule_id, threat_feed, threat_score,
		       message, details_json, dedup_key
		FROM events
		UNION ALL
		SELECT 1 AS schema_version, '' AS event_id, '' AS raw_ref, '' AS collector,
		       '' AS parser, '' AS parser_status,
		       tenant_id, started_at AS ts, node_id,
		       CONCAT('conn.', COALESCE(NULLIF(direction, ''), 'flow')) AS event_type,
		       CASE WHEN threat_match THEN 'high' ELSE 'info' END AS severity,
		       correlation_id, conn_id, bastion_session_id, pid, process_name, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, duration_ms, '' AS rule_id, threat_feed, threat_score,
		       CONCAT(COALESCE(process_name, ''), ' ', COALESCE(src_ip, ''), ' -> ', COALESCE(dst_ip, '')) AS message,
		       '' AS details_json, conn_id AS dedup_key
		FROM process_connections
		UNION ALL
		SELECT 1 AS schema_version, '' AS event_id, '' AS raw_ref, '' AS collector,
		       '' AS parser, '' AS parser_status,
		       tenant_id, observed_at AS ts, node_id,
		       CASE WHEN exited_at IS NULL THEN 'proc.exec' ELSE 'proc.exit' END AS event_type,
		       'info' AS severity,
		       '' AS correlation_id, '' AS conn_id, '' AS bastion_session_id,
		       pid, process_name, user_name,
		       '' AS src_ip, 0 AS src_port, '' AS dst_ip, 0 AS dst_port, '' AS protocol,
		       0 AS bytes_in, 0 AS bytes_out, 0 AS duration_ms, '' AS rule_id, '' AS threat_feed, 0 AS threat_score,
		       CONCAT(COALESCE(process_name, ''), ' ', COALESCE(cmdline, '')) AS message,
		       '' AS details_json, '' AS dedup_key
		FROM process_lineage
		UNION ALL
		SELECT 1 AS schema_version, '' AS event_id, '' AS raw_ref, '' AS collector,
		       '' AS parser, '' AS parser_status,
		       tenant_id, ts, node_id, CONCAT('file.', op) AS event_type, 'info' AS severity,
		       correlation_id, conn_id, '' AS bastion_session_id, pid, process_name, user_name,
		       '' AS src_ip, 0 AS src_port, '' AS dst_ip, 0 AS dst_port, '' AS protocol,
		       bytes AS bytes_in, 0 AS bytes_out, 0 AS duration_ms, '' AS rule_id, '' AS threat_feed, 0 AS threat_score,
		       path AS message, '' AS details_json, '' AS dedup_key
		FROM file_accesses
		UNION ALL
		SELECT 1 AS schema_version, '' AS event_id, '' AS raw_ref, '' AS collector,
		       '' AS parser, '' AS parser_status,
		       tenant_id, ts, node_id, 'db.query' AS event_type, 'info' AS severity,
		       correlation_id, conn_id, '' AS bastion_session_id, pid, '' AS process_name, user_name,
		       src_ip, 0 AS src_port, '' AS dst_ip, 0 AS dst_port, '' AS protocol,
		       0 AS bytes_in, exec_time_ms AS bytes_out, exec_time_ms AS duration_ms,
		       '' AS rule_id, '' AS threat_feed, 0 AS threat_score,
		       query_text AS message, '' AS details_json, query_hash AS dedup_key
		FROM db_queries
		UNION ALL
		SELECT schema_version, event_id, raw_ref, collector, parser, parser_status,
		       tenant_id, ts, node_id,
		       CASE WHEN status_code >= 500 THEN 'web.error' ELSE 'web.request' END AS event_type,
		       CASE WHEN status_code >= 500 THEN 'high' WHEN status_code >= 400 THEN 'medium' ELSE 'info' END AS severity,
		       correlation_id, '' AS conn_id, '' AS bastion_session_id, 0 AS pid, webserver_kind AS process_name, '' AS user_name,
		       src_ip, 0 AS src_port, socket_ip AS dst_ip, 0 AS dst_port, '' AS protocol,
		       bytes_in, bytes_out, duration_ms, '' AS rule_id, '' AS threat_feed, reputation_score AS threat_score,
		       message, details_json, event_id AS dedup_key
		FROM web_requests`
}

func scanEventRow(s rowScanner) (EventRow, error) {
	var r EventRow
	var schemaVersion sql.NullInt64
	var srcPort, dstPort, threatScore sql.NullInt64
	var pid, bytesIn, bytesOut, durationMS sql.NullInt64
	var ts *time.Time
	var eventID, rawRef, collector, parser, parserStatus sql.NullString
	var tenantID, nodeID, eventType, severity, correlationID, connID sql.NullString
	var bastion, processName, userName, srcIP, dstIP, protocol sql.NullString
	var ruleID, threatFeed, message, detailsJSON, dedupKey sql.NullString
	if err := s.Scan(
		&schemaVersion, &eventID, &rawRef, &collector, &parser, &parserStatus,
		&tenantID, &ts, &nodeID, &eventType, &severity, &correlationID, &connID,
		&bastion, &pid, &processName, &userName,
		&srcIP, &srcPort, &dstIP, &dstPort, &protocol,
		&bytesIn, &bytesOut, &durationMS, &ruleID, &threatFeed, &threatScore,
		&message, &detailsJSON, &dedupKey,
	); err != nil {
		return r, err
	}
	if schemaVersion.Valid {
		r.SchemaVersion = int(schemaVersion.Int64)
	}
	if r.SchemaVersion <= 0 {
		r.SchemaVersion = 1
	}
	r.EventID = eventID.String
	r.RawRef = rawRef.String
	r.Collector = collector.String
	r.Parser = parser.String
	r.ParserStatus = parserStatus.String
	r.TenantID = tenantID.String
	if ts != nil {
		r.TS = *ts
	}
	r.NodeID = nodeID.String
	r.EventType = eventType.String
	r.Severity = severity.String
	r.CorrelationID = correlationID.String
	r.ConnID = connID.String
	r.BastionSessID = bastion.String
	if pid.Valid {
		r.PID = pid.Int64
	}
	r.ProcessName = processName.String
	r.UserName = userName.String
	r.SrcIP = srcIP.String
	if srcPort.Valid {
		r.SrcPort = int(srcPort.Int64)
	}
	r.DstIP = dstIP.String
	if dstPort.Valid {
		r.DstPort = int(dstPort.Int64)
	}
	r.Protocol = protocol.String
	if bytesIn.Valid {
		r.BytesIn = bytesIn.Int64
	}
	if bytesOut.Valid {
		r.BytesOut = bytesOut.Int64
	}
	if durationMS.Valid {
		r.DurationMS = durationMS.Int64
	}
	r.RuleID = ruleID.String
	r.ThreatFeed = threatFeed.String
	if threatScore.Valid {
		r.ThreatScore = int(threatScore.Int64)
	}
	r.Message = message.String
	r.DetailsJSON = detailsJSON.String
	r.DedupKey = dedupKey.String
	return r, nil
}

// TimelineBuildParams describes the bounded timeline query used by the
// investigation API. At least one pivot must be supplied by the server layer
// before calling BuildTimeline.
type TimelineBuildParams struct {
	TenantID      string
	CorrelationID string
	NodeID        string
	ConnID        string
	EntityType    string
	EntityID      string
	Since         time.Time
	Until         time.Time
	Limit         int
}

// TimelineItem is one normalized row in the multi-table investigation
// timeline. SourceTable identifies where the cited row came from.
type TimelineItem struct {
	SourceTable   string
	SchemaVersion int
	EventID       string
	RawRef        string
	Collector     string
	Parser        string
	ParserStatus  string
	TenantID      string
	TS            time.Time
	NodeID        string
	EventType     string
	Severity      string
	Message       string
	CorrelationID string
	ConnID        string
	PID           int64
	ProcessName   string
	UserName      string
	SrcIP         string
	DstIP         string
	DstPort       int
	Path          string
	BytesIn       int64
	BytesOut      int64
	DetailsJSON   string
}

func (c *Client) BuildTimeline(ctx context.Context, p TimelineBuildParams) ([]TimelineItem, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	query, args, err := buildTimelineSQL(p)
	if err != nil {
		return nil, err
	}
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("build timeline: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]TimelineItem, 0, clampTimelineLimit(p.Limit))
	for rows.Next() {
		item, err := scanTimelineItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func buildTimelineSQL(p TimelineBuildParams) (string, []any, error) {
	eventsWhere, eventsArgs, err := eventWhereClause(EventQueryParams{
		TenantID:      p.TenantID,
		NodeID:        p.NodeID,
		CorrelationID: p.CorrelationID,
		ConnID:        p.ConnID,
		Since:         p.Since,
		Until:         p.Until,
	}, "ts")
	if err != nil {
		return "", nil, err
	}
	eventsWhere, eventsArgs = appendEntityPredicate(eventsWhere, eventsArgs, p.EntityType, p.EntityID, eventEntityColumns())

	fileWhere, fileArgs, err := timelineSideTableWhere(p, "ts", fileEntityColumns())
	if err != nil {
		return "", nil, err
	}
	connWhere, connArgs, err := timelineConnectionWhere(p)
	if err != nil {
		return "", nil, err
	}
	lineageWhere, lineageArgs, err := timelineLineageWhere(p)
	if err != nil {
		return "", nil, err
	}
	dbWhere, dbArgs, err := timelineSideTableWhere(p, "ts", dbEntityColumns())
	if err != nil {
		return "", nil, err
	}
	webWhere, webArgs, err := timelineWebWhere(p)
	if err != nil {
		return "", nil, err
	}

	query := `
		SELECT * FROM (
			SELECT 'events' AS source_table, schema_version, event_id, raw_ref, collector, parser, parser_status,
			       tenant_id, ts, node_id, event_type, severity, message, correlation_id, conn_id,
			       pid, process_name, user_name, src_ip, dst_ip, dst_port, '' AS path,
			       bytes_in, bytes_out, details_json
			FROM events
			WHERE ` + eventsWhere + `
			UNION ALL
			SELECT 'process_connections' AS source_table, 1 AS schema_version, '' AS event_id, '' AS raw_ref,
			       '' AS collector, '' AS parser, '' AS parser_status,
			       tenant_id, started_at AS ts, node_id,
			       CONCAT('conn.', COALESCE(NULLIF(direction, ''), 'flow')) AS event_type,
			       CASE WHEN threat_match THEN 'high' ELSE 'info' END AS severity,
			       CONCAT(COALESCE(process_name, ''), ' ', COALESCE(src_ip, ''), ' -> ', COALESCE(dst_ip, '')) AS message,
			       correlation_id, conn_id, pid, process_name, user_name,
			       src_ip, dst_ip, dst_port, '' AS path, bytes_in, bytes_out, '' AS details_json
			FROM process_connections
			WHERE ` + connWhere + `
			UNION ALL
			SELECT 'process_lineage' AS source_table, 1 AS schema_version, '' AS event_id, '' AS raw_ref,
			       '' AS collector, '' AS parser, '' AS parser_status,
			       tenant_id, observed_at AS ts, node_id,
			       CASE WHEN exited_at IS NULL THEN 'proc.exec' ELSE 'proc.exit' END AS event_type,
			       'info' AS severity, CONCAT(COALESCE(process_name, ''), ' ', COALESCE(cmdline, '')) AS message,
			       '' AS correlation_id, '' AS conn_id, pid, process_name, user_name,
			       '' AS src_ip, '' AS dst_ip, 0 AS dst_port, exe_path AS path, 0 AS bytes_in, 0 AS bytes_out,
			       '' AS details_json
			FROM process_lineage
			WHERE ` + lineageWhere + `
			UNION ALL
			SELECT 'file_accesses' AS source_table, 1 AS schema_version, '' AS event_id, '' AS raw_ref,
			       '' AS collector, '' AS parser, '' AS parser_status,
			       tenant_id, ts, node_id, CONCAT('file.', op) AS event_type, 'info' AS severity,
			       path AS message, correlation_id, conn_id, pid, process_name, user_name,
			       '' AS src_ip, '' AS dst_ip, 0 AS dst_port, path, bytes AS bytes_in, 0 AS bytes_out,
			       '' AS details_json
			FROM file_accesses
			WHERE ` + fileWhere + `
			UNION ALL
			SELECT 'db_queries' AS source_table, 1 AS schema_version, '' AS event_id, '' AS raw_ref,
			       '' AS collector, '' AS parser, '' AS parser_status,
			       tenant_id, ts, node_id, 'db.query' AS event_type, 'info' AS severity,
			       query_text AS message, correlation_id, conn_id, pid, '' AS process_name, user_name,
			       src_ip, '' AS dst_ip, 0 AS dst_port, '' AS path, 0 AS bytes_in, exec_time_ms AS bytes_out,
			       '' AS details_json
			FROM db_queries
			WHERE ` + dbWhere + `
			UNION ALL
			SELECT 'web_requests' AS source_table, schema_version, event_id, raw_ref, collector, parser, parser_status,
			       tenant_id, ts, node_id,
			       CASE WHEN status_code >= 500 THEN 'web.error' ELSE 'web.request' END AS event_type,
			       CASE WHEN status_code >= 500 THEN 'high' WHEN status_code >= 400 THEN 'medium' ELSE 'info' END AS severity,
			       message, correlation_id, '' AS conn_id, 0 AS pid, webserver_kind AS process_name, '' AS user_name,
			       src_ip, socket_ip AS dst_ip, 0 AS dst_port, path_template AS path, bytes_in, bytes_out, details_json
			FROM web_requests
			WHERE ` + webWhere + `
		) timeline
		ORDER BY ts DESC`
	args := append(append(append(append(append(eventsArgs, connArgs...), lineageArgs...), fileArgs...), dbArgs...), webArgs...)
	return withLimit(query, clampTimelineLimit(p.Limit)), args, nil
}

func scanTimelineItem(s rowScanner) (TimelineItem, error) {
	var r TimelineItem
	var schemaVersion, dstPort, pid, bytesIn, bytesOut sql.NullInt64
	var ts *time.Time
	var sourceTable, eventID, rawRef, collector, parser, parserStatus sql.NullString
	var tenantID, nodeID, eventType, severity, message, correlationID, connID sql.NullString
	var processName, userName, srcIP, dstIP, path, detailsJSON sql.NullString
	if err := s.Scan(
		&sourceTable, &schemaVersion, &eventID, &rawRef, &collector, &parser, &parserStatus,
		&tenantID, &ts, &nodeID, &eventType, &severity, &message, &correlationID, &connID,
		&pid, &processName, &userName, &srcIP, &dstIP, &dstPort, &path, &bytesIn, &bytesOut, &detailsJSON,
	); err != nil {
		return r, err
	}
	r.SourceTable = sourceTable.String
	if schemaVersion.Valid {
		r.SchemaVersion = int(schemaVersion.Int64)
	}
	if r.SchemaVersion <= 0 {
		r.SchemaVersion = 1
	}
	r.EventID = eventID.String
	r.RawRef = rawRef.String
	r.Collector = collector.String
	r.Parser = parser.String
	r.ParserStatus = parserStatus.String
	r.TenantID = tenantID.String
	if ts != nil {
		r.TS = *ts
	}
	r.NodeID = nodeID.String
	r.EventType = eventType.String
	r.Severity = severity.String
	r.Message = message.String
	r.CorrelationID = correlationID.String
	r.ConnID = connID.String
	if pid.Valid {
		r.PID = pid.Int64
	}
	r.ProcessName = processName.String
	r.UserName = userName.String
	r.SrcIP = srcIP.String
	r.DstIP = dstIP.String
	if dstPort.Valid {
		r.DstPort = int(dstPort.Int64)
	}
	r.Path = path.String
	if bytesIn.Valid {
		r.BytesIn = bytesIn.Int64
	}
	if bytesOut.Valid {
		r.BytesOut = bytesOut.Int64
	}
	r.DetailsJSON = detailsJSON.String
	return r, nil
}

type entityColumns struct {
	NodeID      string
	SrcIP       string
	DstIP       string
	UserName    string
	ProcessName string
	Path        string
	ConnID      string
	EventID     string
	RawRef      string
}

func eventEntityColumns() entityColumns {
	return entityColumns{
		NodeID:      "node_id",
		SrcIP:       "src_ip",
		DstIP:       "dst_ip",
		UserName:    "user_name",
		ProcessName: "process_name",
		ConnID:      "conn_id",
		EventID:     "event_id",
		RawRef:      "raw_ref",
	}
}

func fileEntityColumns() entityColumns {
	return entityColumns{
		NodeID:      "node_id",
		UserName:    "user_name",
		ProcessName: "process_name",
		Path:        "path",
		ConnID:      "conn_id",
	}
}

func dbEntityColumns() entityColumns {
	return entityColumns{
		NodeID:   "node_id",
		SrcIP:    "src_ip",
		UserName: "user_name",
		ConnID:   "conn_id",
	}
}

func connectionEntityColumns() entityColumns {
	return entityColumns{
		NodeID:      "node_id",
		SrcIP:       "src_ip",
		DstIP:       "dst_ip",
		UserName:    "user_name",
		ProcessName: "process_name",
		ConnID:      "conn_id",
	}
}

func lineageEntityColumns() entityColumns {
	return entityColumns{
		NodeID:      "node_id",
		UserName:    "user_name",
		ProcessName: "process_name",
		Path:        "exe_path",
	}
}

func webEntityColumns() entityColumns {
	return entityColumns{
		NodeID:      "node_id",
		SrcIP:       "src_ip",
		DstIP:       "socket_ip",
		ProcessName: "webserver_kind",
		Path:        "path_template",
		EventID:     "event_id",
		RawRef:      "raw_ref",
	}
}

func eventWhereClause(p EventQueryParams, timeColumn string) (string, []any, error) {
	if strings.TrimSpace(p.TenantID) == "" {
		return "", nil, fmt.Errorf("tenant_id required")
	}
	if timeColumn == "" {
		timeColumn = "ts"
	}
	where := []string{"tenant_id = ?"}
	args := []any{strings.TrimSpace(p.TenantID)}
	if !p.Since.IsZero() {
		where = append(where, timeColumn+" >= ?")
		args = append(args, p.Since)
	}
	if !p.Until.IsZero() {
		where = append(where, timeColumn+" <= ?")
		args = append(args, p.Until)
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
		where = append(where, "parser_status = ?")
		args = append(args, v)
	}
	if types := cleanEventTypes(p.EventTypes); len(types) > 0 {
		placeholders := make([]string, len(types))
		for i, typ := range types {
			placeholders[i] = "?"
			args = append(args, typ)
		}
		where = append(where, "event_type IN ("+strings.Join(placeholders, ", ")+")")
	}
	if v := strings.TrimSpace(p.Search); v != "" {
		where = append(where, "LOWER(message) LIKE ?")
		args = append(args, "%"+strings.ToLower(v)+"%")
	}
	return strings.Join(where, " AND "), args, nil
}

func timelineSideTableWhere(p TimelineBuildParams, timeColumn string, cols entityColumns) (string, []any, error) {
	base, args, err := eventWhereClause(EventQueryParams{
		TenantID:      p.TenantID,
		NodeID:        p.NodeID,
		CorrelationID: p.CorrelationID,
		ConnID:        p.ConnID,
		Since:         p.Since,
		Until:         p.Until,
	}, timeColumn)
	if err != nil {
		return "", nil, err
	}
	where, args := appendEntityPredicate(base, args, p.EntityType, p.EntityID, cols)
	return where, args, nil
}

func timelineConnectionWhere(p TimelineBuildParams) (string, []any, error) {
	if strings.TrimSpace(p.TenantID) == "" {
		return "", nil, fmt.Errorf("tenant_id required")
	}
	where := []string{"tenant_id = ?"}
	args := []any{strings.TrimSpace(p.TenantID)}
	switch {
	case !p.Since.IsZero() && !p.Until.IsZero():
		where = append(where, "started_at <= ?")
		args = append(args, p.Until)
		where = append(where, "(ended_at IS NULL OR ended_at >= ?)")
		args = append(args, p.Since)
	case !p.Since.IsZero():
		where = append(where, "(ended_at IS NULL OR ended_at >= ?)")
		args = append(args, p.Since)
	case !p.Until.IsZero():
		where = append(where, "started_at <= ?")
		args = append(args, p.Until)
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
	base := strings.Join(where, " AND ")
	base, args = appendEntityPredicate(base, args, p.EntityType, p.EntityID, connectionEntityColumns())
	return base, args, nil
}

func timelineLineageWhere(p TimelineBuildParams) (string, []any, error) {
	if strings.TrimSpace(p.TenantID) == "" {
		return "", nil, fmt.Errorf("tenant_id required")
	}
	where := []string{"tenant_id = ?"}
	args := []any{strings.TrimSpace(p.TenantID)}
	if strings.TrimSpace(p.CorrelationID) != "" || strings.TrimSpace(p.ConnID) != "" {
		return strings.Join(append(where, "1 = 0"), " AND "), args, nil
	}
	if !p.Since.IsZero() {
		where = append(where, "observed_at >= ?")
		args = append(args, p.Since)
	}
	if !p.Until.IsZero() {
		where = append(where, "observed_at <= ?")
		args = append(args, p.Until)
	}
	if v := strings.TrimSpace(p.NodeID); v != "" {
		where = append(where, "node_id = ?")
		args = append(args, v)
	}
	base := strings.Join(where, " AND ")
	base, args = appendEntityPredicate(base, args, p.EntityType, p.EntityID, lineageEntityColumns())
	return base, args, nil
}

func timelineWebWhere(p TimelineBuildParams) (string, []any, error) {
	if strings.TrimSpace(p.TenantID) == "" {
		return "", nil, fmt.Errorf("tenant_id required")
	}
	where := []string{"tenant_id = ?"}
	args := []any{strings.TrimSpace(p.TenantID)}
	if strings.TrimSpace(p.ConnID) != "" {
		return strings.Join(append(where, "1 = 0"), " AND "), args, nil
	}
	if !p.Since.IsZero() {
		where = append(where, "ts >= ?")
		args = append(args, p.Since)
	}
	if !p.Until.IsZero() {
		where = append(where, "ts <= ?")
		args = append(args, p.Until)
	}
	if v := strings.TrimSpace(p.NodeID); v != "" {
		where = append(where, "node_id = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(p.CorrelationID); v != "" {
		where = append(where, "correlation_id = ?")
		args = append(args, v)
	}
	base := strings.Join(where, " AND ")
	base, args = appendEntityPredicate(base, args, p.EntityType, p.EntityID, webEntityColumns())
	return base, args, nil
}

func appendEntityPredicate(where string, args []any, entityType, entityID string, cols entityColumns) (string, []any) {
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	entityID = strings.TrimSpace(entityID)
	if entityType == "" || entityID == "" {
		return where, args
	}
	switch entityType {
	case "ip":
		parts := []string{}
		if cols.SrcIP != "" {
			parts = append(parts, cols.SrcIP+" = ?")
			args = append(args, entityID)
		}
		if cols.DstIP != "" {
			parts = append(parts, cols.DstIP+" = ?")
			args = append(args, entityID)
		}
		if len(parts) == 0 {
			return where + " AND 1 = 0", args
		}
		return where + " AND (" + strings.Join(parts, " OR ") + ")", args
	case "user":
		if cols.UserName == "" {
			return where + " AND 1 = 0", args
		}
		return where + " AND " + cols.UserName + " = ?", append(args, entityID)
	case "process":
		if cols.ProcessName == "" {
			return where + " AND 1 = 0", args
		}
		return where + " AND " + cols.ProcessName + " = ?", append(args, entityID)
	case "file":
		if cols.Path == "" {
			return where + " AND 1 = 0", args
		}
		return where + " AND " + cols.Path + " = ?", append(args, entityID)
	case "host", "node":
		if cols.NodeID == "" {
			return where + " AND 1 = 0", args
		}
		return where + " AND " + cols.NodeID + " = ?", append(args, entityID)
	case "connection":
		if cols.ConnID == "" {
			return where + " AND 1 = 0", args
		}
		return where + " AND " + cols.ConnID + " = ?", append(args, entityID)
	case "event":
		if cols.EventID == "" {
			return where + " AND 1 = 0", args
		}
		return where + " AND " + cols.EventID + " = ?", append(args, entityID)
	case "raw_ref":
		if cols.RawRef == "" {
			return where + " AND 1 = 0", args
		}
		return where + " AND " + cols.RawRef + " = ?", append(args, entityID)
	default:
		return where + " AND 1 = 0", args
	}
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

func clampEventQueryLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func clampTimelineLimit(limit int) int {
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
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, fleetHealthSnapshotSQL(), tenantID, since)
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

func fleetHealthSnapshotSQL() string {
	return `
		SELECT IFNULL(node_id, '') AS node_id,
		       CASE
		         WHEN SUM(CASE WHEN event_type = 'conn.open' THEN 1 WHEN event_type = 'conn.close' THEN -1 ELSE 0 END) < 0 THEN 0
		         ELSE SUM(CASE WHEN event_type = 'conn.open' THEN 1 WHEN event_type = 'conn.close' THEN -1 ELSE 0 END)
		       END AS conns_active,
		       IFNULL(SUM(IFNULL(bytes_in, 0)), 0) AS bytes_in,
		       IFNULL(SUM(IFNULL(bytes_out, 0)), 0) AS bytes_out,
		       SUM(CASE WHEN IFNULL(threat_feed, '') != '' THEN 1 ELSE 0 END) AS threat_hits,
		       MAX(ts) AS last_event_at,
		       IFNULL(MAX(severity), '') AS sev_max
		FROM events
		WHERE tenant_id = ? AND ts >= ?
		GROUP BY IFNULL(node_id, '')
		ORDER BY threat_hits DESC, conns_active DESC
		LIMIT 1000
	`
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
