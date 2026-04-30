package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// UpsertKnownDestinationResult reports whether a (tenant, dst_ip) row was
// inserted fresh (true → first sighting → anomaly.new_destination) or
// updated (false → already known).
type UpsertKnownDestinationResult struct {
	FirstSighting bool
	ConnCount     int64
	FirstSeenAt   time.Time
}

// UpsertKnownDestination atomically records that the tenant just observed
// dst_ip on a `conn.open` event. Returns FirstSighting=true when the
// INSERT actually happened (no prior row).
func (s *Store) UpsertKnownDestination(ctx context.Context, tenantID uuid.UUID, dstIP string) (UpsertKnownDestinationResult, error) {
	if dstIP == "" {
		return UpsertKnownDestinationResult{}, errors.New("dst_ip required")
	}
	const q = `
INSERT INTO tenant_known_destinations (tenant_id, dst_ip, first_seen_at, last_seen_at, conn_count)
VALUES ($1, $2::inet, NOW(), NOW(), 1)
ON CONFLICT (tenant_id, dst_ip)
DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at,
              conn_count   = tenant_known_destinations.conn_count + 1
RETURNING (xmax = 0) AS first_sighting, conn_count, first_seen_at;`
	var out UpsertKnownDestinationResult
	if err := s.db.QueryRowContext(ctx, q, tenantID, dstIP).
		Scan(&out.FirstSighting, &out.ConnCount, &out.FirstSeenAt); err != nil {
		return UpsertKnownDestinationResult{}, err
	}
	return out, nil
}

// UpsertKnownExeHashResult reports whether an exe_hash was first-seen
// for the tenant (→ anomaly.new_executable).
type UpsertKnownExeHashResult struct {
	FirstSighting bool
	ExecCount     int64
}

// UpsertKnownExeHash records a proc.exec. First sighting flips a fresh
// row in; subsequent calls bump exec_count + last_seen_at.
func (s *Store) UpsertKnownExeHash(ctx context.Context, tenantID uuid.UUID, hash, path string, pid int64, nodeID *uuid.UUID) (UpsertKnownExeHashResult, error) {
	if hash == "" {
		return UpsertKnownExeHashResult{}, errors.New("exe_hash required")
	}
	const q = `
INSERT INTO tenant_known_exe_hashes (tenant_id, exe_hash, first_seen_at, first_seen_pid, first_seen_path, first_seen_node, last_seen_at, exec_count)
VALUES ($1, $2, NOW(), $3, $4, $5, NOW(), 1)
ON CONFLICT (tenant_id, exe_hash)
DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at,
              exec_count   = tenant_known_exe_hashes.exec_count + 1
RETURNING (xmax = 0) AS first_sighting, exec_count;`
	var out UpsertKnownExeHashResult
	var nullableNode interface{}
	if nodeID != nil {
		nullableNode = *nodeID
	}
	if err := s.db.QueryRowContext(ctx, q, tenantID, hash, pid, path, nullableNode).
		Scan(&out.FirstSighting, &out.ExecCount); err != nil {
		return UpsertKnownExeHashResult{}, err
	}
	return out, nil
}

// ConnectionDurationBaseline holds the rolling p50/p95/p99 for a
// (tenant, dst_ip, dst_port). Returned by GetConnectionDurationBaseline;
// nil means no baseline yet (insufficient samples).
type ConnectionDurationBaseline struct {
	P50MS       int64
	P95MS       int64
	P99MS       int64
	SampleCount int64
	UpdatedAt   time.Time
}

// GetConnectionDurationBaseline looks up a baseline. Returns nil + nil
// when no row exists.
func (s *Store) GetConnectionDurationBaseline(ctx context.Context, tenantID uuid.UUID, dstIP string, dstPort int) (*ConnectionDurationBaseline, error) {
	const q = `
SELECT p50_ms, p95_ms, p99_ms, sample_count, updated_at
FROM connection_duration_baselines
WHERE tenant_id = $1 AND dst_ip = $2::inet AND dst_port = $3;`
	var b ConnectionDurationBaseline
	err := s.db.QueryRowContext(ctx, q, tenantID, dstIP, dstPort).
		Scan(&b.P50MS, &b.P95MS, &b.P99MS, &b.SampleCount, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ConnectionBytesBaseline holds rolling byte/packet percentiles for a
// (tenant, process_name, dst_port). Used by the F.4 detector at
// conn.close ingest.
type ConnectionBytesBaseline struct {
	P95BytesIn    int64
	P95BytesOut   int64
	P95PacketsIn  int64
	P95PacketsOut int64
	SampleCount   int64
	UpdatedAt     time.Time
}

// GetConnectionBytesBaseline looks up a baseline. nil + nil = no row.
func (s *Store) GetConnectionBytesBaseline(ctx context.Context, tenantID uuid.UUID, processName string, dstPort int) (*ConnectionBytesBaseline, error) {
	const q = `
SELECT p95_bytes_in, p95_bytes_out, p95_packets_in, p95_packets_out, sample_count, updated_at
FROM connection_bytes_baselines
WHERE tenant_id = $1 AND process_name = $2 AND dst_port = $3;`
	var b ConnectionBytesBaseline
	err := s.db.QueryRowContext(ctx, q, tenantID, processName, dstPort).
		Scan(&b.P95BytesIn, &b.P95BytesOut, &b.P95PacketsIn, &b.P95PacketsOut, &b.SampleCount, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// UpsertKnownQueryHashResult reports first-sighting + the running
// percentile state for the F.5 detector.
type UpsertKnownQueryHashResult struct {
	FirstSighting bool
	ExecCount     int64
	P95Rows       sql.NullInt64
	MaxRows       sql.NullInt64
}

// UpsertKnownQueryHash atomically records a db.query event keyed by the
// (tenant, engine, db, user, query_hash) tuple. First sighting →
// anomaly.new_db_query. Subsequent rows update exec_count.
func (s *Store) UpsertKnownQueryHash(ctx context.Context, tenantID uuid.UUID, engine, database, userName, hash, sample string, rows, execMS int64) (UpsertKnownQueryHashResult, error) {
	if hash == "" {
		return UpsertKnownQueryHashResult{}, errors.New("query_hash required")
	}
	const q = `
INSERT INTO db_query_known_hashes
    (tenant_id, engine, database_name, user_name, query_hash, query_sample,
     first_seen_at, last_seen_at, exec_count, max_rows, max_exec_ms)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW(), 1, $7, $8)
ON CONFLICT (tenant_id, engine, database_name, user_name, query_hash)
DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at,
              exec_count   = db_query_known_hashes.exec_count + 1,
              max_rows     = GREATEST(db_query_known_hashes.max_rows, EXCLUDED.max_rows),
              max_exec_ms  = GREATEST(db_query_known_hashes.max_exec_ms, EXCLUDED.max_exec_ms)
RETURNING (xmax = 0) AS first_sighting, exec_count, p95_rows, max_rows;`
	var out UpsertKnownQueryHashResult
	if err := s.db.QueryRowContext(ctx, q, tenantID, engine, database, userName, hash, sample, rows, execMS).
		Scan(&out.FirstSighting, &out.ExecCount, &out.P95Rows, &out.MaxRows); err != nil {
		return UpsertKnownQueryHashResult{}, err
	}
	return out, nil
}
