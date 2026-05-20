package doris

import (
	"context"
	"database/sql"
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
// (tenant_id, node_id, time window overlap, optional ended_at IS NULL). The peer-IP
// classification (RFC1918 vs external) lives ONE layer further out — at the
// agent's `internal/netflow/filter.go` capture-policy boundary — and is NOT
// re-applied here. Re-applying it would double-strip internal flows on
// dev/internal nodes where most peers are private (bugs §1.2). Callers that
// want "external only" should pass an explicit predicate via a future
// parameter instead of post-filtering rows in the UI.
func buildListConnectionsForNodeQuery(limit int, openOnly bool) string {
	if openOnly {
		return withLimit(`
		SELECT conn_id, correlation_id, started_at, ended_at, duration_ms, direction,
		       pid, process_name, cmdline, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, packets_in, packets_out,
		       threat_match, threat_feed, closed_reason, bastion_session_id, node_id
		FROM process_connections
		WHERE tenant_id = ?
		  AND node_id = ?
		  AND started_at <= ?
		  AND ended_at IS NULL
		ORDER BY threat_match DESC, started_at DESC
	`, limit)
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
		  AND started_at <= ?
		  AND (ended_at IS NULL OR ended_at >= ?)
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
	var rows *sql.Rows
	var err error
	if openOnly {
		rows, err = c.db.QueryContext(qctx, q, tenantID, nodeID, until)
	} else {
		rows, err = c.db.QueryContext(qctx, q, tenantID, nodeID, until, since)
	}
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

// ListConnectionsForTenant returns recent fleet-wide connections for a tenant.
// The caller decides whether to display internal/listener rows; this method
// keeps the analytic query broad enough for operator drilldown and UI filters.
func (c *Client) ListConnectionsForTenant(ctx context.Context, tenantID string, since, until time.Time, limit int) ([]ConnectionRow, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := withLimit(`
		SELECT conn_id, correlation_id, started_at, ended_at, duration_ms, direction,
		       pid, process_name, cmdline, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, packets_in, packets_out,
		       threat_match, threat_feed, closed_reason, bastion_session_id, node_id
		FROM process_connections
		WHERE tenant_id = ?
		  AND started_at <= ?
		  AND (ended_at IS NULL OR ended_at >= ?)
		ORDER BY threat_match DESC, started_at DESC
	`, limit)
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, q, tenantID, until, since)
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

// QueryEvents returns normalized rows from events ordered newest first.
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
	selectSQL := `
		SELECT schema_version, event_id, raw_ref, collector, parser, parser_status,
		       tenant_id, ts, node_id, event_type, severity, correlation_id, conn_id,
		       bastion_session_id, pid, process_name, user_name,
		       src_ip, src_port, dst_ip, dst_port, protocol,
		       bytes_in, bytes_out, duration_ms, rule_id, threat_feed, threat_score,
		       message, details_json, dedup_key
		FROM events
		WHERE ` + where + `
		ORDER BY ts DESC`
	query := withLimitOffset(selectSQL, clampEventQueryLimit(p.Limit), maxInt(p.Offset, 0))
	countQuery := "SELECT COUNT(*) FROM events WHERE " + where
	return query, countQuery, args, nil
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
	dbWhere, dbArgs, err := timelineSideTableWhere(p, "ts", dbEntityColumns())
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
		) timeline
		ORDER BY ts ASC`
	args := append(append(eventsArgs, fileArgs...), dbArgs...)
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
		where = append(where, "message MATCH_ANY ?")
		args = append(args, v)
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
