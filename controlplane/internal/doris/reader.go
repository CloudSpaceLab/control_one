package doris

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LogSearchParams narrows a Doris-side log search. Search uses inverted index
// when available; falls back to LIKE otherwise. Time range is required to
// scope partition pruning.
type LogSearchParams struct {
	TenantID string
	NodeID   string
	Source   string
	Level    string
	Search   string // free-text, MATCH_ANY against `message`
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
}

// LogRow is the projection returned by SearchLogs.
type LogRow struct {
	Timestamp time.Time
	NodeID    string
	Level     string
	Source    string
	Program   string
	Message   string
}

// SearchLogs returns matching telemetry_logs rows, ordered newest first.
func (c *Client) SearchLogs(ctx context.Context, p LogSearchParams) ([]LogRow, int, error) {
	if c == nil || c.db == nil {
		return nil, 0, fmt.Errorf("doris client unavailable")
	}
	if p.Limit <= 0 || p.Limit > 1000 {
		p.Limit = 100
	}
	where := []string{"1=1"}
	args := []any{}
	if p.TenantID != "" {
		where = append(where, "tenant_id = ?")
		args = append(args, p.TenantID)
	}
	if p.NodeID != "" {
		where = append(where, "node_id = ?")
		args = append(args, p.NodeID)
	}
	if p.Source != "" {
		where = append(where, "log_source = ?")
		args = append(args, p.Source)
	}
	if p.Level != "" {
		where = append(where, "log_level = ?")
		args = append(args, p.Level)
	}
	if !p.Since.IsZero() {
		where = append(where, "timestamp >= ?")
		args = append(args, p.Since)
	}
	if !p.Until.IsZero() {
		where = append(where, "timestamp <= ?")
		args = append(args, p.Until)
	}
	if s := strings.TrimSpace(p.Search); s != "" {
		// Doris MATCH_ANY uses inverted index; quote the query for tokens.
		where = append(where, "message MATCH_ANY ?")
		args = append(args, s)
	}

	whereSQL := strings.Join(where, " AND ")
	countQ := "SELECT COUNT(*) FROM telemetry_logs WHERE " + whereSQL
	queryQ := "SELECT timestamp, node_id, log_level, log_source, log_program, message FROM telemetry_logs WHERE " +
		whereSQL + " ORDER BY timestamp DESC LIMIT ? OFFSET ?"

	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()

	var total int
	if err := c.db.QueryRowContext(qctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count logs: %w", err)
	}

	args = append(args, p.Limit, p.Offset)
	rows, err := c.db.QueryContext(qctx, queryQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]LogRow, 0, p.Limit)
	for rows.Next() {
		var r LogRow
		var nodeID, level, source, program *string
		if err := rows.Scan(&r.Timestamp, &nodeID, &level, &source, &program, &r.Message); err != nil {
			return nil, 0, err
		}
		if nodeID != nil {
			r.NodeID = *nodeID
		}
		if level != nil {
			r.Level = *level
		}
		if source != nil {
			r.Source = *source
		}
		if program != nil {
			r.Program = *program
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// CountUnique returns BITMAP_UNION_COUNT for a (tenant, dimension, value)
// over a time window. Cost is O(buckets) regardless of cardinality.
func (c *Client) CountUnique(ctx context.Context, tenantID, dimension, dimValue string, since, until time.Time) (int64, error) {
	if c == nil || c.db == nil {
		return 0, fmt.Errorf("doris client unavailable")
	}
	q := `
		SELECT BITMAP_UNION_COUNT(unique_set)
		FROM unique_counters
		WHERE tenant_id = ? AND dimension = ? AND dim_value = ?
		  AND bucket_ts >= ? AND bucket_ts <= ?
	`
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	var n int64
	err := c.db.QueryRowContext(qctx, q, tenantID, dimension, dimValue, since, until).Scan(&n)
	return n, err
}

// ThroughputBucket is one time bucket in a log ingest throughput series.
type ThroughputBucket struct {
	Timestamp time.Time
	Events    int64
}

// LogThroughputSeries returns bucketed log ingest counts from telemetry_logs.
// bucketDur controls bucket width; minimum 1 minute, clamped to 1 minute if smaller.
func (c *Client) LogThroughputSeries(ctx context.Context, tenantID string, since, until time.Time, bucketDur time.Duration) ([]ThroughputBucket, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if bucketDur < time.Minute {
		bucketDur = time.Minute
	}
	bucketSec := int64(bucketDur.Seconds())
	q := fmt.Sprintf(`
		SELECT FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(timestamp) / %d) * %d) AS bucket_ts,
		       COUNT(*) AS cnt
		FROM telemetry_logs
		WHERE tenant_id = ? AND timestamp >= ? AND timestamp <= ?
		GROUP BY bucket_ts
		ORDER BY bucket_ts
	`, bucketSec, bucketSec)
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	rows, err := c.db.QueryContext(qctx, q, tenantID, since, until)
	if err != nil {
		return nil, fmt.Errorf("throughput series: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ThroughputBucket{}
	for rows.Next() {
		var b ThroughputBucket
		if err := rows.Scan(&b.Timestamp, &b.Events); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// RelatedEntity is one result row from RelatedEntities.
type RelatedEntity struct {
	Type          string
	ID            string
	CoOccurrences int64
}

// RelatedEntities returns entities active in the same tenant window as
// entityID. For node-type entities it excludes the queried node; for IP-type
// it also surfaces other IPs from the same events.
func (c *Client) RelatedEntities(ctx context.Context, tenantID, entityType, entityID string, since, until time.Time, limit int) ([]RelatedEntity, error) {
	if c == nil || c.db == nil {
		return nil, fmt.Errorf("doris client unavailable")
	}
	if limit <= 0 {
		limit = 10
	}

	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()

	out := []RelatedEntity{}

	// Co-active nodes: most-active nodes in the same tenant/window.
	nodeQ := `
		SELECT node_id, COUNT(*) AS cnt
		FROM security_events
		WHERE tenant_id = ?
		  AND fired_at >= ? AND fired_at <= ?
		  AND node_id IS NOT NULL AND node_id != ''
		  AND node_id != ?
		GROUP BY node_id
		ORDER BY cnt DESC
		LIMIT ?
	`
	nodeRows, err := c.db.QueryContext(qctx, nodeQ, tenantID, since, until, entityID, limit)
	if err != nil {
		return nil, fmt.Errorf("related entities nodes: %w", err)
	}
	defer func() { _ = nodeRows.Close() }()
	for nodeRows.Next() {
		var r RelatedEntity
		r.Type = "node"
		if err := nodeRows.Scan(&r.ID, &r.CoOccurrences); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, err
	}

	// For IP-type entities: find other IPs co-appearing in the same events.
	if entityType == "ip" && len(out) < limit {
		remaining := limit - len(out)
		// src_ip side
		srcQ := `
			SELECT dst_ip, COUNT(*) AS cnt
			FROM security_events
			WHERE tenant_id = ? AND fired_at >= ? AND fired_at <= ?
			  AND src_ip = ? AND dst_ip IS NOT NULL AND dst_ip != ''
			GROUP BY dst_ip ORDER BY cnt DESC LIMIT ?
		`
		ipRows, ipErr := c.db.QueryContext(qctx, srcQ, tenantID, since, until, entityID, remaining)
		if ipErr == nil {
			defer func() { _ = ipRows.Close() }()
			for ipRows.Next() {
				var r RelatedEntity
				r.Type = "ip"
				if err := ipRows.Scan(&r.ID, &r.CoOccurrences); err != nil {
					break
				}
				out = append(out, r)
			}
		}
	}

	return out, nil
}

// EventCounts is the per-severity breakdown returned by CountSecurityEvents.
type EventCounts struct {
	Critical int64
	High     int64
	Medium   int64
	Low      int64
	Total    int64
}

// CountSecurityEvents returns severity counts over a time window. Reads from
// security_events in Doris (post-cutover) for fast aggregation.
func (c *Client) CountSecurityEvents(ctx context.Context, tenantID string, since, until time.Time) (EventCounts, error) {
	var ec EventCounts
	if c == nil || c.db == nil {
		return ec, fmt.Errorf("doris client unavailable")
	}
	q := `
		SELECT
			SUM(CASE WHEN severity = 'critical' THEN 1 ELSE 0 END) AS critical,
			SUM(CASE WHEN severity = 'high'     THEN 1 ELSE 0 END) AS high,
			SUM(CASE WHEN severity = 'medium'   THEN 1 ELSE 0 END) AS medium,
			SUM(CASE WHEN severity = 'low'      THEN 1 ELSE 0 END) AS low,
			COUNT(*)                                                 AS total
		FROM security_events
		WHERE tenant_id = ? AND fired_at >= ? AND fired_at <= ?
	`
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()
	return ec, c.db.QueryRowContext(qctx, q, tenantID, since, until).
		Scan(&ec.Critical, &ec.High, &ec.Medium, &ec.Low, &ec.Total)
}
