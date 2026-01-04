package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

// Store wraps database connectivity and lifecycle operations.
type Store struct {
	log   *zap.Logger
	db    *sql.DB
	cfg   config.DatabaseConfig
	clock func() time.Time
}

// GetNode returns a node by ID.
func (s *Store) GetNode(ctx context.Context, id uuid.UUID) (*Node, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("node id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, hostname, os, arch, public_ip, created_at, updated_at
		FROM nodes
		WHERE id = $1
	`, id)

	var node Node
	if err := row.Scan(&node.ID, &node.TenantID, &node.Hostname, &node.OS, &node.Arch, &node.PublicIP, &node.CreatedAt, &node.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get node: %w", err)
	}

	return &node, nil
}

// ListJobs returns jobs filtered by tenant, type, and status with pagination.
func (s *Store) ListJobs(ctx context.Context, tenantID uuid.UUID, jobType string, status JobStatus, limit, offset int) ([]Job, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}

	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	jobType = strings.TrimSpace(jobType)

	var (
		clauses = []string{"TRUE"}
		args    []any
	)

	if tenantID != uuid.Nil {
		args = append(args, tenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}

	if jobType != "" {
		args = append(args, jobType)
		clauses = append(clauses, fmt.Sprintf("type = $%d", len(args)))
	}

	if status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, type, status, payload, retries, max_retries, scheduled_at, started_at, finished_at, created_at, updated_at
		FROM jobs
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(clauses, " AND "))

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM jobs WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args[:len(args)-(func() int {
		if limit > 0 {
			return 1
		}
		return 0
	}())-(func() int {
		if offset > 0 {
			return 1
		}
		return 0
	}())]...)

	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query jobs: %w", err)
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var job Job
		var tenant sql.NullString
		var scheduled, started, finished sql.NullTime
		if err := rows.Scan(&job.ID, &tenant, &job.Type, &job.Status, &job.Payload, &job.Retries, &job.MaxRetries, &scheduled, &started, &finished, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan job: %w", err)
		}
		if tenant.Valid {
			job.TenantID, _ = uuid.Parse(tenant.String)
		}
		if scheduled.Valid {
			t := scheduled.Time
			job.ScheduledAt = &t
		}
		if started.Valid {
			t := started.Time
			job.StartedAt = &t
		}
		if finished.Valid {
			t := finished.Time
			job.FinishedAt = &t
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate jobs: %w", err)
	}

	return jobs, total, nil
}

// EnsureTenant returns the tenant or creates it if absent.
func (s *Store) EnsureTenant(ctx context.Context, id uuid.UUID, name string) (*Tenant, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}

	tenant, err := s.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	if tenant != nil {
		return tenant, nil
	}

	if strings.TrimSpace(name) == "" {
		return nil, errors.New("tenant name is required for creation")
	}

	newTenant := &Tenant{ID: id, Name: strings.TrimSpace(name)}
	created, err := s.CreateTenant(ctx, newTenant)
	if err != nil {
		return nil, err
	}
	return created, nil
}

// GetTenant returns a tenant by ID if it exists.
func (s *Store) GetTenant(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, created_at
		FROM tenants
		WHERE id = $1
	`, id)

	var tenant Tenant
	if err := row.Scan(&tenant.ID, &tenant.Name, &tenant.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tenant: %w", err)
	}

	return &tenant, nil
}

// CreateJob inserts a job and optional initial event.
func (s *Store) CreateJob(ctx context.Context, job *Job, initialEvent *JobEvent) (*Job, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if job == nil {
		return nil, errors.New("job cannot be nil")
	}
	if strings.TrimSpace(job.Type) == "" {
		return nil, errors.New("job type is required")
	}
	if job.Status == "" {
		job.Status = JobStatusQueued
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	job.CreatedAt = s.clock()
	job.UpdatedAt = job.CreatedAt

	payload := job.Payload
	if payload == nil {
		payload = []byte("null")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO jobs (id, tenant_id, type, status, payload, retries, max_retries, scheduled_at, started_at, finished_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, job.ID, nullableUUID(job.TenantID), job.Type, job.Status, payload, job.Retries, job.MaxRetries, job.ScheduledAt, job.StartedAt, job.FinishedAt, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert job: %w", err)
	}

	if initialEvent != nil {
		if initialEvent.ID == uuid.Nil {
			initialEvent.ID = uuid.New()
		}
		if initialEvent.Status == "" {
			initialEvent.Status = job.Status
		}
		initialEvent.JobID = job.ID
		initialEvent.CreatedAt = job.CreatedAt

		_, err = tx.ExecContext(ctx, `
			INSERT INTO job_events (id, job_id, status, message, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, initialEvent.ID, initialEvent.JobID, initialEvent.Status, nullString(initialEvent.Message), initialEvent.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("insert job event: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit job insert: %w", err)
	}

	return job, nil
}

// UpdateJobStatus transitions a job status and records an event.
func (s *Store) UpdateJobStatus(ctx context.Context, jobID uuid.UUID, status JobStatus, message string, updateFields map[string]any) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if jobID == uuid.Nil {
		return errors.New("job id required")
	}

	now := s.clock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	setFragments := []string{"status = $1", "updated_at = $2"}
	args := []any{status, now}
	idx := 3

	if updateFields != nil {
		if scheduled, ok := updateFields["scheduled_at"]; ok {
			setFragments = append(setFragments, fmt.Sprintf("scheduled_at = $%d", idx))
			args = append(args, scheduled)
			idx++
		}
		if started, ok := updateFields["started_at"]; ok {
			setFragments = append(setFragments, fmt.Sprintf("started_at = $%d", idx))
			args = append(args, started)
			idx++
		}
		if finished, ok := updateFields["finished_at"]; ok {
			setFragments = append(setFragments, fmt.Sprintf("finished_at = $%d", idx))
			args = append(args, finished)
			idx++
		}
		if retries, ok := updateFields["retries"]; ok {
			setFragments = append(setFragments, fmt.Sprintf("retries = $%d", idx))
			args = append(args, retries)
			idx++
		}
	}

	query := fmt.Sprintf("UPDATE jobs SET %s WHERE id = $%d", strings.Join(setFragments, ", "), idx)
	args = append(args, jobID)

	if _, err = tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	eventID := uuid.New()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO job_events (id, job_id, status, message, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, eventID, jobID, status, nullString(message), now); err != nil {
		return fmt.Errorf("insert job event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit job update: %w", err)
	}
	committed = true
	return nil
}

// GetJob retrieves a job by ID including latest persisted fields.
func (s *Store) GetJob(ctx context.Context, jobID uuid.UUID) (*Job, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if jobID == uuid.Nil {
		return nil, errors.New("job id required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, type, status, payload, retries, max_retries, scheduled_at, started_at, finished_at, created_at, updated_at
		FROM jobs
		WHERE id = $1
	`, jobID)

	var job Job
	var tenant sql.NullString
	var scheduled, started, finished sql.NullTime
	if err := row.Scan(&job.ID, &tenant, &job.Type, &job.Status, &job.Payload, &job.Retries, &job.MaxRetries, &scheduled, &started, &finished, &job.CreatedAt, &job.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan job: %w", err)
	}
	if tenant.Valid {
		job.TenantID, _ = uuid.Parse(tenant.String)
	}
	if scheduled.Valid {
		t := scheduled.Time
		job.ScheduledAt = &t
	}
	if started.Valid {
		t := started.Time
		job.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time
		job.FinishedAt = &t
	}

	return &job, nil
}

// ListJobEvents returns events for a job ordered by creation time.
func (s *Store) ListJobEvents(ctx context.Context, jobID uuid.UUID) ([]JobEvent, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if jobID == uuid.Nil {
		return nil, errors.New("job id required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, job_id, status, message, created_at
		FROM job_events
		WHERE job_id = $1
		ORDER BY created_at ASC
	`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query job events: %w", err)
	}
	defer rows.Close()

	var events []JobEvent
	for rows.Next() {
		var evt JobEvent
		var message sql.NullString
		if err := rows.Scan(&evt.ID, &evt.JobID, &evt.Status, &message, &evt.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan job event: %w", err)
		}
		if message.Valid {
			evt.Message = message.String
		}
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job events: %w", err)
	}

	return events, nil
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func nullString(val string) sql.NullString {
	if strings.TrimSpace(val) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: val, Valid: true}
}

// CreateTenant inserts a tenant record.
func (s *Store) CreateTenant(ctx context.Context, tenant *Tenant) (*Tenant, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenant == nil {
		return nil, errors.New("tenant cannot be nil")
	}
	if strings.TrimSpace(tenant.Name) == "" {
		return nil, errors.New("tenant name is required")
	}

	if tenant.ID == uuid.Nil {
		tenant.ID = uuid.New()
	}
	tenant.CreatedAt = s.clock()

	query := `
		INSERT INTO tenants (id, name, created_at)
		VALUES ($1, $2, $3)
	`

	if _, err := s.db.ExecContext(ctx, query, tenant.ID, tenant.Name, tenant.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert tenant: %w", err)
	}

	return tenant, nil
}

// ListTenants returns tenants ordered by creation time. If limit is zero, all rows are returned.
func (s *Store) ListTenants(ctx context.Context, prefix string, limit, offset int) ([]Tenant, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}

	prefix = strings.TrimSpace(prefix)
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	var (
		args    []any
		clauses []string
	)

	clauses = append(clauses, "TRUE")
	if prefix != "" {
		args = append(args, prefix+"%")
		clauses = append(clauses, fmt.Sprintf("name ILIKE $%d", len(args)))
	}

	query := fmt.Sprintf(`
		SELECT id, name, created_at
		FROM tenants
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(clauses, " AND "))

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM tenants WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args[:len(args)-(func() int {
		if limit > 0 {
			return 1
		}
		return 0
	}())-(func() int {
		if offset > 0 {
			return 1
		}
		return 0
	}())]...)

	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query tenants: %w", err)
	}
	defer rows.Close()

	var tenants []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate tenants: %w", err)
	}

	return tenants, total, nil
}

// UpdateTenant renames a tenant by ID.
func (s *Store) UpdateTenant(ctx context.Context, id uuid.UUID, name string) (*Tenant, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("tenant name is required")
	}

	row := s.db.QueryRowContext(ctx, `
		UPDATE tenants
		SET name = $2
		WHERE id = $1
		RETURNING id, name, created_at
	`, id, name)

	var tenant Tenant
	if err := row.Scan(&tenant.ID, &tenant.Name, &tenant.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update tenant: %w", err)
	}

	return &tenant, nil
}

// DeleteTenant removes a tenant by ID.
func (s *Store) DeleteTenant(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("tenant id is required")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete tenant rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Node represents a managed node record.
type Node struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Hostname  string
	OS        sql.NullString
	Arch      sql.NullString
	PublicIP  sql.NullString
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Tenant represents a tenant record.
type Tenant struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

// AuditLog captures security-relevant actions across the platform.
type AuditLog struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	ActorID      uuid.UUID
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   *string
	Metadata     map[string]any
	CreatedAt    time.Time
}

// AuditLogFilter narrows audit log queries.
type AuditLogFilter struct {
	TenantID     uuid.UUID
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   string
	Since        *time.Time
	Until        *time.Time
}

// CreateAuditLog persists an audit entry.
func (s *Store) CreateAuditLog(ctx context.Context, entry *AuditLog) (*AuditLog, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if entry == nil {
		return nil, errors.New("audit log entry cannot be nil")
	}
	entry.Action = strings.TrimSpace(entry.Action)
	entry.ResourceType = strings.TrimSpace(entry.ResourceType)
	if entry.Action == "" || entry.ResourceType == "" {
		return nil, errors.New("action and resource_type are required")
	}
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = s.clock()
	}
	actorType := strings.TrimSpace(entry.ActorType)
	if actorType == "" {
		actorType = "system"
	}
	var tenantVal any = nil
	if entry.TenantID != uuid.Nil {
		tenantVal = entry.TenantID
	}
	var actorIDVal any = nil
	if entry.ActorID != uuid.Nil {
		actorIDVal = entry.ActorID
	}
	resourceID := sql.NullString{}
	if entry.ResourceID != nil && strings.TrimSpace(*entry.ResourceID) != "" {
		resourceID = sql.NullString{String: strings.TrimSpace(*entry.ResourceID), Valid: true}
	}
	var metadataBytes []byte
	if len(entry.Metadata) > 0 {
		bytes, err := json.Marshal(entry.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		metadataBytes = bytes
	}
	if metadataBytes == nil {
		metadataBytes = []byte("{}")
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, tenant_id, actor_id, actor_type, action, resource_type, resource_id, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, entry.ID, tenantVal, actorIDVal, actorType, entry.Action, entry.ResourceType, resourceID, metadataBytes, entry.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert audit log: %w", err)
	}
	entry.ActorType = actorType
	return entry, nil
}

// ListAuditLogs returns audit entries based on the provided filter.
func (s *Store) ListAuditLogs(ctx context.Context, filter AuditLogFilter, limit, offset int) ([]AuditLog, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	var (
		clauses = []string{"TRUE"}
		args    []any
	)

	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if trimmed := strings.TrimSpace(filter.ActorType); trimmed != "" {
		args = append(args, trimmed)
		clauses = append(clauses, fmt.Sprintf("actor_type = $%d", len(args)))
	}
	if trimmed := strings.TrimSpace(filter.Action); trimmed != "" {
		args = append(args, trimmed)
		clauses = append(clauses, fmt.Sprintf("action = $%d", len(args)))
	}
	if trimmed := strings.TrimSpace(filter.ResourceType); trimmed != "" {
		args = append(args, trimmed)
		clauses = append(clauses, fmt.Sprintf("resource_type = $%d", len(args)))
	}
	if trimmed := strings.TrimSpace(filter.ResourceID); trimmed != "" {
		args = append(args, trimmed)
		clauses = append(clauses, fmt.Sprintf("resource_id = $%d", len(args)))
	}
	if filter.Since != nil {
		args = append(args, filter.Since.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if filter.Until != nil {
		args = append(args, filter.Until.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, actor_id, actor_type, action, resource_type, resource_id, metadata, created_at
		FROM audit_logs
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(clauses, " AND "))

	argsWithPagination := append([]any{}, args...)
	if limit > 0 {
		argsWithPagination = append(argsWithPagination, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(argsWithPagination))
	}
	if offset > 0 {
		argsWithPagination = append(argsWithPagination, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(argsWithPagination))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM audit_logs WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args...)

	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, argsWithPagination...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var entries []AuditLog
	for rows.Next() {
		var (
			entry      AuditLog
			tenantID   sql.NullString
			actorID    sql.NullString
			resourceID sql.NullString
			metadata   []byte
		)
		if err := rows.Scan(
			&entry.ID,
			&tenantID,
			&actorID,
			&entry.ActorType,
			&entry.Action,
			&entry.ResourceType,
			&resourceID,
			&metadata,
			&entry.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}
		if tenantID.Valid {
			entry.TenantID, _ = uuid.Parse(tenantID.String)
		}
		if actorID.Valid {
			entry.ActorID, _ = uuid.Parse(actorID.String)
		}
		if resourceID.Valid {
			val := resourceID.String
			entry.ResourceID = &val
		}
		if len(metadata) > 0 {
			var meta map[string]any
			if err := json.Unmarshal(metadata, &meta); err != nil {
				return nil, 0, fmt.Errorf("unmarshal audit metadata: %w", err)
			}
			entry.Metadata = meta
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit logs: %w", err)
	}

	return entries, total, nil
}

// JobStatus represents the state of a job.
type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// Job represents a background job persisted in storage.
type Job struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Type        string
	Status      JobStatus
	Payload     []byte
	Retries     int
	MaxRetries  int
	ScheduledAt *time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// JobEvent captures state transitions for a job.
type JobEvent struct {
	ID        uuid.UUID
	JobID     uuid.UUID
	Status    JobStatus
	Message   string
	CreatedAt time.Time
}

// Options allows injection of testing helpers.
type Options struct {
	Clock func() time.Time
}

// New creates a Store from configuration.
func New(log *zap.Logger, cfg config.DatabaseConfig, opts Options) (*Store, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database url must be provided")
	}

	db, err := sql.Open("pgx", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	s := &Store{
		log:   log,
		db:    db,
		cfg:   cfg,
		clock: opts.Clock,
	}
	if s.clock == nil {
		s.clock = time.Now
	}

	return s, nil
}

// Close releases database resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping verifies the database connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

// DB exposes the underlying sql.DB for advanced use cases (migrations/tests).
func (s *Store) DB() *sql.DB {
	return s.db
}

// GetNodeByHostname returns a node for the tenant if it exists.
func (s *Store) GetNodeByHostname(ctx context.Context, tenantID uuid.UUID, hostname string) (*Node, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	hostname = strings.TrimSpace(hostname)
	if tenantID == uuid.Nil || hostname == "" {
		return nil, errors.New("tenant id and hostname are required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, hostname, os, arch, public_ip, created_at, updated_at
		FROM nodes
		WHERE tenant_id = $1 AND hostname = $2
		LIMIT 1
	`, tenantID, hostname)

	var node Node
	if err := row.Scan(&node.ID, &node.TenantID, &node.Hostname, &node.OS, &node.Arch, &node.PublicIP, &node.CreatedAt, &node.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get node by hostname: %w", err)
	}

	return &node, nil
}

// CreateNode inserts a node record.
func (s *Store) CreateNode(ctx context.Context, node *Node) (*Node, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if node == nil {
		return nil, errors.New("node cannot be nil")
	}
	if node.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if node.ID == uuid.Nil {
		node.ID = uuid.New()
	}

	now := s.clock()
	node.CreatedAt = now
	node.UpdatedAt = now

	query := `
        INSERT INTO nodes (id, tenant_id, hostname, os, arch, public_ip, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `

	_, err := s.db.ExecContext(
		ctx,
		query,
		node.ID,
		node.TenantID,
		node.Hostname,
		node.OS,
		node.Arch,
		node.PublicIP,
		node.CreatedAt,
		node.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert node: %w", err)
	}

	return node, nil
}

// ListNodes returns nodes filtered by tenant and hostname prefix with pagination.
func (s *Store) ListNodes(ctx context.Context, tenantID uuid.UUID, hostnamePrefix string, limit, offset int) ([]Node, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}

	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	hostnamePrefix = strings.TrimSpace(hostnamePrefix)

	var (
		clauses = []string{"TRUE"}
		args    []any
	)

	if tenantID != uuid.Nil {
		args = append(args, tenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}

	if hostnamePrefix != "" {
		args = append(args, hostnamePrefix+"%")
		clauses = append(clauses, fmt.Sprintf("hostname ILIKE $%d", len(args)))
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, hostname, os, arch, public_ip, created_at, updated_at
		FROM nodes
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(clauses, " AND "))

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM nodes WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args[:len(args)-(func() int {
		if limit > 0 {
			return 1
		}
		return 0
	}())-(func() int {
		if offset > 0 {
			return 1
		}
		return 0
	}())]...)

	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count nodes: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.TenantID, &n.Hostname, &n.OS, &n.Arch, &n.PublicIP, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate nodes: %w", err)
	}

	return nodes, total, nil
}

// UpdateNode persists changes to a node record.
func (s *Store) UpdateNode(ctx context.Context, node *Node) (*Node, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if node == nil {
		return nil, errors.New("node cannot be nil")
	}
	if node.ID == uuid.Nil {
		return nil, errors.New("node id is required")
	}

	node.UpdatedAt = s.clock()

	query := `
		UPDATE nodes
		SET hostname = $2,
		    os = $3,
		    arch = $4,
		    public_ip = $5,
		    updated_at = $6
		WHERE id = $1
		RETURNING id, tenant_id, hostname, os, arch, public_ip, created_at, updated_at
	`

	row := s.db.QueryRowContext(ctx, query,
		node.ID,
		node.Hostname,
		node.OS,
		node.Arch,
		node.PublicIP,
		node.UpdatedAt,
	)

	var updated Node
	if err := row.Scan(&updated.ID, &updated.TenantID, &updated.Hostname, &updated.OS, &updated.Arch, &updated.PublicIP, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update node: %w", err)
	}

	return &updated, nil
}

// DeleteNode removes a node by ID.
func (s *Store) DeleteNode(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("node id is required")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete node rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
