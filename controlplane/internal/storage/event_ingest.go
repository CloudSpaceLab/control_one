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
	ReplayKey     sql.NullString
	DorisStatus   sql.NullString
	LastAttemptAt sql.NullTime
	RetryCount    int
	NextAttemptAt sql.NullTime
	LastErrorAt   sql.NullTime
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
	ReplayKey string
	Payload   []byte
}

// DuplicateEventIngestReplayError reports that RecordEventIngest found an
// existing journal row for the tenant/node replay key instead of creating a
// fresh batch. Callers should return the stored receipt and skip local side
// effects.
type DuplicateEventIngestReplayError struct {
	Batch EventIngestBatch
}

func (e *DuplicateEventIngestReplayError) Error() string {
	if e == nil {
		return "duplicate event ingest replay"
	}
	return fmt.Sprintf("duplicate event ingest replay: %s", e.Batch.ID)
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
	replayKey := strings.TrimSpace(p.ReplayKey)
	if replayKey != "" {
		id, err := s.recordEventIngestWithReplayKey(ctx, id, tArg, nArg, p, status, replayKey)
		return id, err
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

func (s *Store) recordEventIngestWithReplayKey(ctx context.Context, id uuid.UUID, tenantArg, nodeArg any, p CreateEventIngestBatchParams, status, replayKey string) (uuid.UUID, error) {
	row := s.db.QueryRowContext(ctx, `
		WITH inserted AS (
			INSERT INTO event_ingest_batches (id, tenant_id, node_id, received_at, size_bytes, rows, status, replay_key, payload)
			VALUES ($1, $2, $3, NOW(), $4, $5, $6, $7, $8)
			ON CONFLICT DO NOTHING
			RETURNING id, tenant_id, node_id, received_at, size_bytes, rows, status,
			          replay_key, doris_status, last_attempt_at, retry_count,
			          next_attempt_at, last_error_at, payload, error_message
		)
		SELECT id, tenant_id, node_id, received_at, size_bytes, rows, status,
		       replay_key, doris_status, last_attempt_at, retry_count,
		       next_attempt_at, last_error_at, payload, error_message,
		       FALSE AS duplicate
		FROM inserted
		UNION ALL
		SELECT id, tenant_id, node_id, received_at, size_bytes, rows, status,
		       replay_key, doris_status, last_attempt_at, retry_count,
		       next_attempt_at, last_error_at, payload, error_message,
		       TRUE AS duplicate
		FROM event_ingest_batches
		WHERE tenant_id IS NOT DISTINCT FROM $2
		  AND node_id IS NOT DISTINCT FROM $3
		  AND replay_key = $7
		ORDER BY duplicate
		LIMIT 1
	`, id, tenantArg, nodeArg, p.SizeBytes, p.Rows, status, replayKey, p.Payload)
	var batch EventIngestBatch
	var duplicate bool
	if err := row.Scan(
		&batch.ID, &batch.TenantID, &batch.NodeID, &batch.ReceivedAt,
		&batch.SizeBytes, &batch.Rows, &batch.Status, &batch.ReplayKey,
		&batch.DorisStatus, &batch.LastAttemptAt, &batch.RetryCount,
		&batch.NextAttemptAt, &batch.LastErrorAt, &batch.Payload,
		&batch.ErrorMessage, &duplicate,
	); err != nil {
		return uuid.Nil, fmt.Errorf("insert event_ingest_batches: %w", err)
	}
	if duplicate {
		return batch.ID, &DuplicateEventIngestReplayError{Batch: batch}
	}
	return batch.ID, nil
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
		       error_message  = CASE
		                          WHEN $2 = 'accepted' THEN NULL
		                          ELSE COALESCE(NULLIF($4, ''), error_message)
		                        END,
		       retry_count    = CASE
		                          WHEN $2 = 'pending_doris' THEN retry_count + 1
		                          ELSE retry_count
		                        END,
		       next_attempt_at = CASE
		                          WHEN $2 = 'pending_doris'
		                            THEN NOW() + (LEAST(1800, (30 * POWER(2, LEAST(retry_count, 6)))::int) * INTERVAL '1 second')
		                          ELSE NULL
		                        END,
		       last_error_at   = CASE
		                          WHEN $2 = 'accepted' THEN NULL
		                          WHEN NULLIF($4, '') IS NOT NULL THEN NOW()
		                          ELSE last_error_at
		                        END,
		       last_attempt_at = NOW()
		 WHERE id = $1
	`, id, status, dorisStatus, errMsg)
	return err
}

// MarkEventIngestLocalComplete records that Postgres-side side effects and
// eventbus publication completed for a batch. The payload is replaced with the
// normalized fan-out event set so a later Doris-only replay has the exact rows
// it must flush and does not re-run local side effects.
func (s *Store) MarkEventIngestLocalComplete(ctx context.Context, id uuid.UUID, payload []byte, rows int) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	var payloadArg any
	if len(payload) > 0 {
		payloadArg = payload
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE event_ingest_batches
		   SET status          = 'local_completed',
		       payload         = COALESCE($2, payload),
		       rows            = CASE WHEN $3 > 0 THEN $3 ELSE rows END,
		       error_message   = NULL,
		       next_attempt_at = NULL,
		       last_attempt_at = NOW()
		 WHERE id = $1
	`, id, payloadArg, rows)
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
		       replay_key, doris_status, last_attempt_at, retry_count, next_attempt_at, last_error_at, payload, error_message
		FROM event_ingest_batches
		WHERE status IN ('received','local_completed','pending_doris')
		  AND received_at <= $1
		  AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
		ORDER BY COALESCE(next_attempt_at, received_at), received_at
		LIMIT $2
	`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EventIngestBatch
	for rows.Next() {
		var b EventIngestBatch
		if err := rows.Scan(&b.ID, &b.TenantID, &b.NodeID, &b.ReceivedAt, &b.SizeBytes, &b.Rows, &b.Status, &b.ReplayKey, &b.DorisStatus, &b.LastAttemptAt, &b.RetryCount, &b.NextAttemptAt, &b.LastErrorAt, &b.Payload, &b.ErrorMessage); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// EventIngestBacklogSummary is an operator-facing rollup of the durable ingest
// journal. It separates queued work from retrying work so Doris outages are
// visible before they become data-loss risks.
type EventIngestBacklogSummary struct {
	PendingBatches   int64
	PendingRows      int64
	DueBatches       int64
	RetryingBatches  int64
	FailedBatches    int64
	MaxRetryCount    int
	OldestPendingAt  sql.NullTime
	NextAttemptAt    sql.NullTime
	LastErrorAt      sql.NullTime
	LastErrorMessage sql.NullString
}

// EventIngestBacklog summarizes pending, retrying, and failed journal rows.
func (s *Store) EventIngestBacklog(ctx context.Context) (EventIngestBacklogSummary, error) {
	return s.eventIngestBacklog(ctx, "", nil)
}

// EventIngestBacklogForTenant summarizes pending, retrying, and failed journal
// rows for one tenant. It is the tenant-scoped form used by investigation
// tools and must not leak global ingest state.
func (s *Store) EventIngestBacklogForTenant(ctx context.Context, tenantID uuid.UUID) (EventIngestBacklogSummary, error) {
	if tenantID == uuid.Nil {
		return EventIngestBacklogSummary{}, errors.New("tenant_id required")
	}
	return s.eventIngestBacklog(ctx, "WHERE tenant_id = $1", []any{tenantID})
}

func (s *Store) eventIngestBacklog(ctx context.Context, where string, args []any) (EventIngestBacklogSummary, error) {
	var out EventIngestBacklogSummary
	if s.db == nil {
		return out, errors.New("store database not initialized")
	}
	query := `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('received','local_completed','pending_doris')) AS pending_batches,
			COALESCE(SUM(rows) FILTER (WHERE status IN ('received','local_completed','pending_doris')), 0) AS pending_rows,
			COUNT(*) FILTER (
				WHERE status IN ('received','local_completed','pending_doris')
				  AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
			) AS due_batches,
			COUNT(*) FILTER (WHERE status = 'pending_doris') AS retrying_batches,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed_batches,
			COALESCE(MAX(retry_count) FILTER (WHERE status IN ('received','local_completed','pending_doris','failed')), 0) AS max_retry_count,
			MIN(received_at) FILTER (WHERE status IN ('received','local_completed','pending_doris')) AS oldest_pending_at,
			MIN(next_attempt_at) FILTER (WHERE status IN ('received','local_completed','pending_doris') AND next_attempt_at IS NOT NULL) AS next_attempt_at,
			MAX(last_error_at) FILTER (WHERE status IN ('pending_doris','failed')) AS last_error_at
		FROM event_ingest_batches
		` + where
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&out.PendingBatches,
		&out.PendingRows,
		&out.DueBatches,
		&out.RetryingBatches,
		&out.FailedBatches,
		&out.MaxRetryCount,
		&out.OldestPendingAt,
		&out.NextAttemptAt,
		&out.LastErrorAt,
	)
	if err != nil {
		return out, err
	}
	lastErrorWhere := "WHERE error_message IS NOT NULL AND status IN ('pending_doris','failed')"
	lastErrorArgs := args
	if where != "" {
		lastErrorWhere = "WHERE error_message IS NOT NULL AND status IN ('pending_doris','failed') AND tenant_id = $1"
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT error_message
		FROM event_ingest_batches
		`+lastErrorWhere+`
		ORDER BY COALESCE(last_error_at, last_attempt_at, received_at) DESC
		LIMIT 1
	`, lastErrorArgs...).Scan(&out.LastErrorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return out, err
}

// ListEventIngestBacklogBatches returns bounded batch metadata for cited
// operator/AI evidence. It deliberately does not read the payload column.
func (s *Store) ListEventIngestBacklogBatches(ctx context.Context, tenantID uuid.UUID, limit int) ([]EventIngestBatch, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, received_at, size_bytes, rows, status,
		       replay_key, doris_status, last_attempt_at, retry_count, next_attempt_at, last_error_at,
		       NULL::bytea AS payload, error_message
		FROM event_ingest_batches
		WHERE tenant_id = $1
		  AND status IN ('received','local_completed','pending_doris','failed')
		ORDER BY COALESCE(last_error_at, next_attempt_at, last_attempt_at, received_at) DESC, received_at DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EventIngestBatch
	for rows.Next() {
		var b EventIngestBatch
		if err := rows.Scan(&b.ID, &b.TenantID, &b.NodeID, &b.ReceivedAt, &b.SizeBytes, &b.Rows, &b.Status, &b.ReplayKey, &b.DorisStatus, &b.LastAttemptAt, &b.RetryCount, &b.NextAttemptAt, &b.LastErrorAt, &b.Payload, &b.ErrorMessage); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// PruneAcceptedEventIngestBatches deletes terminal replay-journal rows older
// than retain. Accepted rows already completed fan-out; archived rows were
// explicitly taken out of the retry path by an operator/reset workflow.
func (s *Store) PruneAcceptedEventIngestBatches(ctx context.Context, retain time.Duration) (int64, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	cutoff := time.Now().UTC().Add(-retain)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM event_ingest_batches
		WHERE status IN ('accepted', 'archived') AND received_at < $1
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
