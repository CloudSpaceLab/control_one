package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CreateAgentLogDumpWithJob persists the export request, its dispatch job, and
// the initial job event in one transaction. A node-agent dump must never exist
// without the job that owns its claim/upload lifecycle.
func (s *Store) CreateAgentLogDumpWithJob(ctx context.Context, d LogDump, job Job) (*LogDump, *Job, error) {
	if s.db == nil {
		return nil, nil, errors.New("store database not initialized")
	}
	if d.TenantID == uuid.Nil || d.NodeID == uuid.Nil {
		return nil, nil, errors.New("log dump tenant and node are required")
	}
	if job.Type == "" {
		return nil, nil, errors.New("job type is required")
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	now := s.clock().UTC()
	if d.RetentionDays == 0 {
		d.RetentionDays = 7
	}
	if d.Status == "" {
		d.Status = LogDumpStatusRequested
	}
	d.Source = LogDumpSourceNodeAgent
	d.JobID = uuid.NullUUID{UUID: job.ID, Valid: true}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if d.ExpiresAt.IsZero() {
		d.ExpiresAt = d.CreatedAt.Add(time.Duration(d.RetentionDays) * 24 * time.Hour)
	}
	entity, err := marshalLogDumpEntity(d.EntityFilter)
	if err != nil {
		return nil, nil, err
	}

	job.TenantID = d.TenantID
	if job.Status == "" {
		job.Status = JobStatusQueued
	}
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.Payload == nil {
		job.Payload = []byte("null")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin agent log dump request: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO jobs (id, tenant_id, type, status, payload, retries, max_retries, scheduled_at, started_at, finished_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		job.ID, job.TenantID, job.Type, job.Status, job.Payload, job.Retries, job.MaxRetries,
		job.ScheduledAt, job.StartedAt, job.FinishedAt, job.CreatedAt, job.UpdatedAt); err != nil {
		return nil, nil, fmt.Errorf("insert log dump job: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO job_events (id, job_id, status, message, created_at)
		VALUES ($1,$2,$3,$4,$5)`,
		uuid.New(), job.ID, job.Status, "raw log dump requested", now); err != nil {
		return nil, nil, fmt.Errorf("insert log dump job event: %w", err)
	}

	row := tx.QueryRowContext(ctx, `
		INSERT INTO agent_log_dumps (
			id, tenant_id, node_id, job_id, source, entity_filter, window_start,
			window_end, retention_days, status, requested_by, created_at, expires_at
		) VALUES ($1,$2,$3,$4,'node_agent',$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING `+logDumpSelect,
		d.ID, d.TenantID, d.NodeID, job.ID, entity, d.WindowStart, d.WindowEnd,
		d.RetentionDays, d.Status, nullableUUID(d.RequestedBy.UUID), d.CreatedAt, d.ExpiresAt)
	created, err := scanLogDump(row)
	if err != nil {
		return nil, nil, fmt.Errorf("insert agent log dump: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit agent log dump request: %w", err)
	}
	return &created, &job, nil
}
