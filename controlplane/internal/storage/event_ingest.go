package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EventIngestBatch is one row in event_ingest_batches.
type EventIngestBatch struct {
	ID            uuid.UUID
	TenantID      uuid.NullUUID
	NodeID        uuid.NullUUID
	ReceivedAt    time.Time
	SizeBytes     int64
	Rows          int
	Status        string
	DorisStatus   sql.NullString
	LastAttemptAt sql.NullTime
	Payload       []byte
	ErrorMessage  sql.NullString
}

// CreateEventIngestBatchParams captures the inputs to RecordEventIngest.
type CreateEventIngestBatchParams struct {
	TenantID  *uuid.UUID
	NodeID    *uuid.UUID
	SizeBytes int64
	Rows      int
	Status    string
	Payload   []byte
}

// RecordEventIngest writes a journal row in 'received' (or caller-chosen)
// status. Returns the generated batch id.
func (s *Store) RecordEventIngest(ctx context.Context, p CreateEventIngestBatchParams) (uuid.UUID, error) {
	if s.db == nil {
		return uuid.Nil, errors.New("store database not initialized")
	}
	status := strings.TrimSpace(p.Status)
	if status == "" {
		status = "received"
	}
	id := uuid.New()
	var tArg, nArg any
	if p.TenantID != nil && *p.TenantID != uuid.Nil {
		tArg = *p.TenantID
	}
	if p.NodeID != nil && *p.NodeID != uuid.Nil {
		nArg = *p.NodeID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO event_ingest_batches (id, tenant_id, node_id, received_at, size_bytes, rows, status, payload)
		VALUES ($1, $2, $3, NOW(), $4, $5, $6, $7)
	`, id, tArg, nArg, p.SizeBytes, p.Rows, status, p.Payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert event_ingest_batches: %w", err)
	}
	return id, nil
}

// MarkEventIngestStatus updates the batch row after fan-out completes (or
// fails). When dorisStatus is empty no change to that column.
func (s *Store) MarkEventIngestStatus(ctx context.Context, id uuid.UUID, status, dorisStatus, errMsg string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE event_ingest_batches
		   SET status         = $2,
		       doris_status   = COALESCE(NULLIF($3, ''), doris_status),
		       error_message  = COALESCE(NULLIF($4, ''), error_message),
		       last_attempt_at = NOW()
		 WHERE id = $1
	`, id, status, dorisStatus, errMsg)
	return err
}

// PendingEventIngestBatches returns batches stuck in 'pending_doris' or
// 'received' for more than `olderThan`. Drainer uses this to retry.
func (s *Store) PendingEventIngestBatches(ctx context.Context, olderThan time.Duration, limit int) ([]EventIngestBatch, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 100
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, received_at, size_bytes, rows, status,
		       doris_status, last_attempt_at, payload, error_message
		FROM event_ingest_batches
		WHERE status IN ('received','pending_doris')
		  AND received_at <= $1
		ORDER BY received_at
		LIMIT $2
	`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EventIngestBatch
	for rows.Next() {
		var b EventIngestBatch
		if err := rows.Scan(&b.ID, &b.TenantID, &b.NodeID, &b.ReceivedAt, &b.SizeBytes, &b.Rows, &b.Status, &b.DorisStatus, &b.LastAttemptAt, &b.Payload, &b.ErrorMessage); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// PruneAcceptedEventIngestBatches deletes batch rows older than retain whose
// status is 'accepted'. Returns the number of rows deleted.
func (s *Store) PruneAcceptedEventIngestBatches(ctx context.Context, retain time.Duration) (int64, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	cutoff := time.Now().UTC().Add(-retain)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM event_ingest_batches
		WHERE status = 'accepted' AND received_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// IncrementHourlyRollup adds counts/bytes to the per-(tenant, node, type, hour)
// rollup. Called from the ingest fast-path so dashboards have a Postgres-side
// number even when Doris is degraded.
func (s *Store) IncrementHourlyRollup(ctx context.Context, tenantID uuid.UUID, nodeID *uuid.UUID, eventType string, hourTS time.Time, cnt, bytesIn, bytesOut int64, sevMax string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || strings.TrimSpace(eventType) == "" {
		return errors.New("tenant_id and event_type required")
	}
	var nArg any
	if nodeID != nil && *nodeID != uuid.Nil {
		nArg = *nodeID
	}
	hour := hourTS.UTC().Truncate(time.Hour)
	var sevArg any
	if strings.TrimSpace(sevMax) != "" {
		sevArg = sevMax
	}
	// Postgres ON CONFLICT requires the PK columns; tenant+node+type+hour
	// are the natural composite key. NULL node_id collides cleanly when
	// using IS NOT DISTINCT FROM, so we use a synthetic empty UUID for the
	// PK-targetable INSERT and a separate update path for null nodes.
	if nArg == nil {
		// Postgres treats NULL as distinct in unique indexes, so the PK on
		// (tenant, node_id, type, hour) cannot dedupe NULL-node rows. The
		// migration in 0091 adds a partial unique index covering exactly
		// (tenant, type, hour) WHERE node_id IS NULL; ON CONFLICT can now
		// target it via the WHERE predicate inference.
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO event_rollups_hourly (tenant_id, node_id, event_type, hour_ts, cnt, bytes_in, bytes_out, sev_max)
			VALUES ($1, NULL, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (tenant_id, event_type, hour_ts) WHERE node_id IS NULL DO UPDATE
			   SET cnt       = event_rollups_hourly.cnt + EXCLUDED.cnt,
			       bytes_in  = event_rollups_hourly.bytes_in + EXCLUDED.bytes_in,
			       bytes_out = event_rollups_hourly.bytes_out + EXCLUDED.bytes_out,
			       sev_max   = COALESCE(EXCLUDED.sev_max, event_rollups_hourly.sev_max)
		`, tenantID, eventType, hour, cnt, bytesIn, bytesOut, sevArg)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO event_rollups_hourly (tenant_id, node_id, event_type, hour_ts, cnt, bytes_in, bytes_out, sev_max)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, node_id, event_type, hour_ts) DO UPDATE
		   SET cnt       = event_rollups_hourly.cnt + EXCLUDED.cnt,
		       bytes_in  = event_rollups_hourly.bytes_in + EXCLUDED.bytes_in,
		       bytes_out = event_rollups_hourly.bytes_out + EXCLUDED.bytes_out,
		       sev_max   = COALESCE(EXCLUDED.sev_max, event_rollups_hourly.sev_max)
	`, tenantID, nArg, eventType, hour, cnt, bytesIn, bytesOut, sevArg)
	return err
}

// HourlyRollupRow is one entry in event_rollups_hourly returned to dashboards.
type HourlyRollupRow struct {
	TenantID  uuid.UUID
	NodeID    uuid.NullUUID
	EventType string
	HourTS    time.Time
	Count     int64
	BytesIn   int64
	BytesOut  int64
	SevMax    sql.NullString
}

// QueryHourlyRollup returns rows for a tenant + window. Used by the dashboard
// fast-view when Doris is degraded.
func (s *Store) QueryHourlyRollup(ctx context.Context, tenantID uuid.UUID, since, until time.Time) ([]HourlyRollupRow, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, node_id, event_type, hour_ts, cnt, bytes_in, bytes_out, sev_max
		FROM event_rollups_hourly
		WHERE tenant_id = $1 AND hour_ts >= $2 AND hour_ts <= $3
		ORDER BY hour_ts
	`, tenantID, since, until)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []HourlyRollupRow
	for rows.Next() {
		var r HourlyRollupRow
		if err := rows.Scan(&r.TenantID, &r.NodeID, &r.EventType, &r.HourTS, &r.Count, &r.BytesIn, &r.BytesOut, &r.SevMax); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
