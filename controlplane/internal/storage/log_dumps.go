package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	LogDumpSourceControlPlane = "control_plane"
	LogDumpSourceNodeAgent    = "node_agent"

	LogDumpStatusRequested = "requested"
	LogDumpStatusCapturing = "capturing"
	LogDumpStatusCaptured  = "captured"
	LogDumpStatusFailed    = "failed"
	LogDumpStatusExpired   = "expired"
	LogDumpStatusDeleting  = "deleting"
)

var (
	ErrLogDumpClaimHeld     = errors.New("log dump claim is active")
	ErrLogDumpClaimInvalid  = errors.New("log dump claim is invalid or expired")
	ErrLogDumpChunkConflict = errors.New("log dump chunk conflicts with existing chunk")
	ErrLogDumpExpired       = errors.New("log dump has expired")
)

// LogDump is the control-plane record for one bounded raw-log export.
// Raw payload bytes are never persisted in this structure.
type LogDump struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	NodeID           uuid.UUID
	JobID            uuid.NullUUID
	Source           string
	EntityFilter     map[string]any
	WindowStart      time.Time
	WindowEnd        time.Time
	RetentionDays    int
	Status           string
	ArtifactPath     string
	ArtifactSHA256   string
	RowCount         int64
	SizeBytes        int64
	Truncated        bool
	SourceAvailable  *bool
	SourceReason     string
	Error            string
	RequestedBy      uuid.NullUUID
	ClaimTokenSHA256 string
	ClaimGeneration  int64
	ClaimedAt        *time.Time
	ClaimExpiresAt   *time.Time
	CaptureStartedAt *time.Time
	CapturedAt       *time.Time
	CleanupAttempts  int
	CleanupError     string
	NextCleanupAt    *time.Time
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

func (d LogDump) Expired(now time.Time) bool {
	return !now.Before(d.ExpiresAt)
}

type LogDumpFilter struct {
	TenantID uuid.UUID
	NodeID   uuid.UUID
	Source   string
	Status   string
}

type LogDumpChunk struct {
	DumpID          uuid.UUID
	ClaimGeneration int64
	TenantID  uuid.UUID
	NodeID    uuid.UUID
	JobID     uuid.NullUUID
	Ordinal   int
	SHA256    string
	SizeBytes int64
	TempPath  string
	CreatedAt time.Time
}

const logDumpSelect = `id, tenant_id, node_id, job_id, source, entity_filter,
	window_start, window_end, retention_days, status, artifact_path,
	artifact_sha256, row_count, size_bytes, truncated, source_available,
	source_reason, error, requested_by, claim_token_sha256, claim_generation,
	claimed_at, claim_expires_at, capture_started_at, captured_at,
	cleanup_attempts, cleanup_error, next_cleanup_at, created_at, expires_at`

func scanLogDump(row interface{ Scan(...any) error }) (LogDump, error) {
	var d LogDump
	var jobID, requestedBy sql.NullString
	var entityRaw []byte
	var artifactPath, artifactSHA, sourceReason, errMsg, claimSHA, cleanupErr sql.NullString
	var sourceAvailable sql.NullBool
	var claimedAt, claimExpiresAt, captureStartedAt, capturedAt, nextCleanupAt sql.NullTime
	if err := row.Scan(
		&d.ID, &d.TenantID, &d.NodeID, &jobID, &d.Source, &entityRaw,
		&d.WindowStart, &d.WindowEnd, &d.RetentionDays, &d.Status, &artifactPath,
		&artifactSHA, &d.RowCount, &d.SizeBytes, &d.Truncated, &sourceAvailable,
		&sourceReason, &errMsg, &requestedBy, &claimSHA, &d.ClaimGeneration,
		&claimedAt, &claimExpiresAt, &captureStartedAt, &capturedAt,
		&d.CleanupAttempts, &cleanupErr, &nextCleanupAt, &d.CreatedAt, &d.ExpiresAt,
	); err != nil {
		return d, err
	}
	if jobID.Valid {
		id, err := uuid.Parse(jobID.String)
		if err != nil {
			return d, fmt.Errorf("decode log dump job id: %w", err)
		}
		d.JobID = uuid.NullUUID{UUID: id, Valid: true}
	}
	if requestedBy.Valid {
		id, err := uuid.Parse(requestedBy.String)
		if err != nil {
			return d, fmt.Errorf("decode log dump requested_by: %w", err)
		}
		d.RequestedBy = uuid.NullUUID{UUID: id, Valid: true}
	}
	if len(entityRaw) > 0 && string(entityRaw) != "null" {
		if err := json.Unmarshal(entityRaw, &d.EntityFilter); err != nil {
			return d, fmt.Errorf("decode log dump entity filter: %w", err)
		}
	}
	if d.EntityFilter == nil {
		d.EntityFilter = map[string]any{}
	}
	d.ArtifactPath = artifactPath.String
	d.ArtifactSHA256 = artifactSHA.String
	d.SourceReason = sourceReason.String
	d.Error = errMsg.String
	d.ClaimTokenSHA256 = claimSHA.String
	d.CleanupError = cleanupErr.String
	if sourceAvailable.Valid {
		v := sourceAvailable.Bool
		d.SourceAvailable = &v
	}
	if claimedAt.Valid {
		t := claimedAt.Time
		d.ClaimedAt = &t
	}
	if claimExpiresAt.Valid {
		t := claimExpiresAt.Time
		d.ClaimExpiresAt = &t
	}
	if captureStartedAt.Valid {
		t := captureStartedAt.Time
		d.CaptureStartedAt = &t
	}
	if capturedAt.Valid {
		t := capturedAt.Time
		d.CapturedAt = &t
	}
	if nextCleanupAt.Valid {
		t := nextCleanupAt.Time
		d.NextCleanupAt = &t
	}
	return d, nil
}

func marshalLogDumpEntity(entity map[string]any) ([]byte, error) {
	if entity == nil {
		entity = map[string]any{}
	}
	b, err := json.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("encode log dump entity filter: %w", err)
	}
	return b, nil
}

func (s *Store) CreateLogDump(ctx context.Context, d LogDump) (*LogDump, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.TenantID == uuid.Nil || d.NodeID == uuid.Nil {
		return nil, errors.New("log dump tenant and node are required")
	}
	if d.RetentionDays == 0 {
		d.RetentionDays = 7
	}
	if d.Status == "" {
		d.Status = LogDumpStatusRequested
	}
	now := s.clock().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if d.ExpiresAt.IsZero() {
		d.ExpiresAt = now.Add(time.Duration(d.RetentionDays) * 24 * time.Hour)
	}
	entity, err := marshalLogDumpEntity(d.EntityFilter)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO agent_log_dumps (
			id, tenant_id, node_id, job_id, source, entity_filter, window_start,
			window_end, retention_days, status, requested_by, created_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+logDumpSelect,
		d.ID, d.TenantID, d.NodeID, nullableUUID(d.JobID.UUID), d.Source, entity,
		d.WindowStart, d.WindowEnd, d.RetentionDays, d.Status,
		nullableUUID(d.RequestedBy.UUID), d.CreatedAt, d.ExpiresAt,
	)
	created, err := scanLogDump(row)
	if err != nil {
		return nil, fmt.Errorf("create log dump: %w", err)
	}
	return &created, nil
}

func (s *Store) GetLogDump(ctx context.Context, tenantID, dumpID uuid.UUID) (*LogDump, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+logDumpSelect+`
		FROM agent_log_dumps WHERE tenant_id=$1 AND id=$2`, tenantID, dumpID)
	d, err := scanLogDump(row)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) GetLogDumpByScope(ctx context.Context, tenantID, nodeID, dumpID uuid.UUID) (*LogDump, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+logDumpSelect+`
		FROM agent_log_dumps WHERE tenant_id=$1 AND node_id=$2 AND id=$3`, tenantID, nodeID, dumpID)
	d, err := scanLogDump(row)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) ListLogDumps(ctx context.Context, f LogDumpFilter, limit, offset int) ([]LogDump, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if f.TenantID == uuid.Nil {
		return nil, 0, errors.New("log dump tenant filter is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		return nil, 0, errors.New("log dump offset must be non-negative")
	}
	clauses := []string{"tenant_id=$1"}
	args := []any{f.TenantID}
	if f.NodeID != uuid.Nil {
		args = append(args, f.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id=$%d", len(args)))
	}
	if v := strings.TrimSpace(f.Source); v != "" {
		args = append(args, v)
		clauses = append(clauses, fmt.Sprintf("source=$%d", len(args)))
	}
	if v := strings.TrimSpace(f.Status); v != "" {
		args = append(args, v)
		clauses = append(clauses, fmt.Sprintf("status=$%d", len(args)))
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_log_dumps WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count log dumps: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM agent_log_dumps
		WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, logDumpSelect, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list log dumps: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]LogDump, 0, limit)
	for rows.Next() {
		d, err := scanLogDump(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan log dump: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate log dumps: %w", err)
	}
	return out, total, nil
}

// ListPendingNodeLogDumps returns only non-expired agent requests. Older
// agents never call this path because the server gates dispatch on log_dump.v1.
func (s *Store) ListPendingNodeLogDumps(ctx context.Context, nodeID uuid.UUID, now time.Time, limit int) ([]LogDump, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+logDumpSelect+`
		FROM agent_log_dumps
		WHERE node_id=$1 AND source='node_agent' AND status='requested' AND expires_at>$2
		ORDER BY created_at ASC LIMIT $3`, nodeID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending node log dumps: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []LogDump
	for rows.Next() {
		d, err := scanLogDump(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClaimLogDump atomically starts or takes over an expired capture lease. The
// opaque upload token itself is never stored, only its SHA-256 digest.
func (s *Store) ClaimLogDump(ctx context.Context, tenantID, nodeID, dumpID, jobID uuid.UUID, tokenSHA string, now, leaseUntil time.Time) (*LogDump, error) {
	if !validSHA256Hex(tokenSHA) || !leaseUntil.After(now) {
		return nil, errors.New("invalid log dump claim")
	}
	row := s.db.QueryRowContext(ctx, `UPDATE agent_log_dumps SET
		status='capturing', claim_token_sha256=$5, claim_generation=claim_generation+1,
		claimed_at=$6, claim_expires_at=$7, capture_started_at=COALESCE(capture_started_at,$6),
		error=NULL
		WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4
		  AND source='node_agent' AND expires_at>$6
		  AND (status='requested' OR (status='capturing' AND claim_expires_at<=$6))
		RETURNING `+logDumpSelect,
		tenantID, nodeID, dumpID, jobID, tokenSHA, now, leaseUntil)
	d, err := scanLogDump(row)
	if err == nil {
		return &d, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("claim log dump: %w", err)
	}
	var status string
	var expiresAt time.Time
	var claimExpires sql.NullTime
	lookupErr := s.db.QueryRowContext(ctx, `SELECT status, expires_at, claim_expires_at
		FROM agent_log_dumps WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4`,
		tenantID, nodeID, dumpID, jobID).Scan(&status, &expiresAt, &claimExpires)
	if lookupErr != nil {
		return nil, lookupErr
	}
	if !now.Before(expiresAt) || status == LogDumpStatusExpired {
		return nil, ErrLogDumpExpired
	}
	if status == LogDumpStatusCapturing && claimExpires.Valid && now.Before(claimExpires.Time) {
		return nil, ErrLogDumpClaimHeld
	}
	return nil, ErrLogDumpClaimInvalid
}

func (s *Store) RenewLogDumpClaim(ctx context.Context, tenantID, nodeID, dumpID, jobID uuid.UUID, tokenSHA string, now, leaseUntil time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_log_dumps SET claim_expires_at=$7
		WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4
		  AND status='capturing' AND claim_token_sha256=$5 AND expires_at>$6 AND claim_expires_at>$6`,
		tenantID, nodeID, dumpID, jobID, tokenSHA, now, leaseUntil)
	if err != nil {
		return fmt.Errorf("renew log dump claim: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrLogDumpClaimInvalid
	}
	return nil
}

func (s *Store) PutLogDumpChunk(ctx context.Context, c LogDumpChunk, tokenSHA string, now time.Time) (bool, error) {
	if c.DumpID == uuid.Nil || c.TenantID == uuid.Nil || c.NodeID == uuid.Nil || !c.JobID.Valid || c.ClaimGeneration < 1 || c.Ordinal < 0 || c.SizeBytes < 0 || !validSHA256Hex(c.SHA256) || strings.TrimSpace(c.TempPath) == "" {
		return false, errors.New("invalid log dump chunk")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin log dump chunk tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var expiresAt time.Time
	var claimExpires sql.NullTime
	var status string
	var storedToken sql.NullString
	var claimGeneration int64
	if err := tx.QueryRowContext(ctx, `SELECT status, expires_at, claim_expires_at, claim_token_sha256, claim_generation
		FROM agent_log_dumps WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4 FOR UPDATE`,
		c.TenantID, c.NodeID, c.DumpID, c.JobID.UUID).Scan(&status, &expiresAt, &claimExpires, &storedToken, &claimGeneration); err != nil {
		return false, err
	}
	if !now.Before(expiresAt) {
		return false, ErrLogDumpExpired
	}
	if status != LogDumpStatusCapturing || !storedToken.Valid || storedToken.String != tokenSHA ||
		!claimExpires.Valid || !now.Before(claimExpires.Time) || claimGeneration != c.ClaimGeneration {
		return false, ErrLogDumpClaimInvalid
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO agent_log_dump_chunks
		(dump_id, tenant_id, node_id, job_id, claim_generation, ordinal, sha256, size_bytes, temp_path, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (dump_id, claim_generation, ordinal) DO NOTHING`,
		c.DumpID, c.TenantID, c.NodeID, c.JobID.UUID, c.ClaimGeneration, c.Ordinal, c.SHA256, c.SizeBytes, c.TempPath, now)
	if err != nil {
		return false, fmt.Errorf("insert log dump chunk: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	var existingSHA string
	var existingSize int64
	if err := tx.QueryRowContext(ctx, `SELECT sha256, size_bytes FROM agent_log_dump_chunks WHERE dump_id=$1 AND claim_generation=$2 AND ordinal=$3`, c.DumpID, c.ClaimGeneration, c.Ordinal).Scan(&existingSHA, &existingSize); err != nil {
		return false, err
	}
	if existingSHA != c.SHA256 || existingSize != c.SizeBytes {
		return false, ErrLogDumpChunkConflict
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ListLogDumpChunks(ctx context.Context, tenantID, nodeID, dumpID, jobID uuid.UUID) ([]LogDumpChunk, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.dump_id,c.tenant_id,c.node_id,c.job_id,c.claim_generation,c.ordinal,c.sha256,c.size_bytes,c.temp_path,c.created_at
		FROM agent_log_dump_chunks c JOIN agent_log_dumps d ON d.id=c.dump_id
		WHERE c.tenant_id=$1 AND c.node_id=$2 AND c.dump_id=$3 AND c.job_id=$4
		  AND d.tenant_id=$1 AND d.node_id=$2 AND d.job_id=$4
		  AND c.claim_generation=d.claim_generation
		ORDER BY c.ordinal`, tenantID, nodeID, dumpID, jobID)
	if err != nil {
		return nil, fmt.Errorf("list log dump chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []LogDumpChunk
	for rows.Next() {
		var c LogDumpChunk
		var rawJob sql.NullString
		if err := rows.Scan(&c.DumpID, &c.TenantID, &c.NodeID, &rawJob, &c.ClaimGeneration, &c.Ordinal, &c.SHA256, &c.SizeBytes, &c.TempPath, &c.CreatedAt); err != nil {
			return nil, err
		}
		if rawJob.Valid {
			id, err := uuid.Parse(rawJob.String)
			if err != nil {
				return nil, err
			}
			c.JobID = uuid.NullUUID{UUID: id, Valid: true}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CompleteLogDumpAndJob commits the artifact metadata and terminal job event
// in one transaction. This prevents a captured artifact with a still-running
// job (or a succeeded job without a captured artifact) after restart.
func (s *Store) CompleteLogDumpAndJob(ctx context.Context, tenantID, nodeID, dumpID, jobID uuid.UUID, tokenSHA, artifactPath, artifactSHA string, rowCount, sizeBytes int64, truncated bool, sourceAvailable *bool, sourceReason string, now time.Time) error {
	if strings.TrimSpace(artifactPath) == "" || !validSHA256Hex(artifactSHA) || rowCount < 0 || sizeBytes < 0 {
		return errors.New("invalid completed log dump metadata")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	var expiresAt time.Time
	var claimExpires sql.NullTime
	var storedToken sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT status, expires_at, claim_expires_at, claim_token_sha256
		FROM agent_log_dumps WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4 FOR UPDATE`, tenantID, nodeID, dumpID, jobID).Scan(&status, &expiresAt, &claimExpires, &storedToken); err != nil {
		return err
	}
	if !now.Before(expiresAt) {
		return ErrLogDumpExpired
	}
	if status == LogDumpStatusCaptured {
		return nil
	}
	if status != LogDumpStatusCapturing || !storedToken.Valid || storedToken.String != tokenSHA ||
		!claimExpires.Valid || !now.Before(claimExpires.Time) {
		return ErrLogDumpClaimInvalid
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_log_dumps SET status='captured',artifact_path=$5,artifact_sha256=$6,row_count=$7,size_bytes=$8,truncated=$9,
		source_available=$10,source_reason=$11,error=NULL,captured_at=$12,claim_token_sha256=NULL,claim_expires_at=NULL
		WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4`, tenantID, nodeID, dumpID, jobID, artifactPath, artifactSHA, rowCount, sizeBytes, truncated, sourceAvailable, nullableText(sourceReason), now); err != nil {
		return fmt.Errorf("complete log dump: %w", err)
	}
	if err := setTerminalLogDumpJobTx(ctx, tx, jobID, JobStatusSucceeded, "raw log dump captured", now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkControlPlaneLogDumpCaptured(ctx context.Context, tenantID, nodeID, dumpID uuid.UUID, artifactPath, artifactSHA string, rowCount, sizeBytes int64, truncated bool, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_log_dumps SET status='captured',artifact_path=$4,artifact_sha256=$5,row_count=$6,size_bytes=$7,truncated=$8,
		source_available=TRUE,source_reason=NULL,error=NULL,captured_at=$9
		WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND source='control_plane' AND status='requested' AND expires_at>$9`,
		tenantID, nodeID, dumpID, artifactPath, artifactSHA, rowCount, sizeBytes, truncated, now)
	if err != nil {
		return fmt.Errorf("capture control-plane log dump: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrLogDumpExpired
	}
	return nil
}

func (s *Store) FailLogDumpAndJob(ctx context.Context, tenantID, nodeID, dumpID, jobID uuid.UUID, message string, sourceAvailable *bool, sourceReason string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE agent_log_dumps SET status='failed',error=$5,source_available=$6,source_reason=$7,
		claim_token_sha256=NULL,claim_expires_at=NULL WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4 AND status IN ('requested','capturing')`,
		tenantID, nodeID, dumpID, jobID, nullableText(message), sourceAvailable, nullableText(sourceReason))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	if err := setTerminalLogDumpJobTx(ctx, tx, jobID, JobStatusFailed, message, now); err != nil {
		return err
	}
	return tx.Commit()
}

func setTerminalLogDumpJobTx(ctx context.Context, tx *sql.Tx, jobID uuid.UUID, status JobStatus, message string, now time.Time) error {
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET status=$2,updated_at=$3,finished_at=COALESCE(finished_at,$3)
		WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled')`, jobID, status, now)
	if err != nil {
		return fmt.Errorf("update log dump job terminal status: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO job_events (id,job_id,status,message,created_at) VALUES ($1,$2,$3,$4,$5)`, uuid.New(), jobID, status, nullString(message), now)
	if err != nil {
		return fmt.Errorf("insert log dump terminal job event: %w", err)
	}
	return nil
}

func (s *Store) MarkLogDumpSourceAvailability(ctx context.Context, tenantID, nodeID, dumpID uuid.UUID, available bool, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_log_dumps SET source_available=$4,source_reason=$5 WHERE tenant_id=$1 AND node_id=$2 AND id=$3`, tenantID, nodeID, dumpID, available, nullableText(reason))
	return err
}

func (s *Store) ExpireLogDump(ctx context.Context, tenantID, dumpID uuid.UUID, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_log_dumps SET status='expired',claim_token_sha256=NULL,claim_expires_at=NULL
		WHERE tenant_id=$1 AND id=$2 AND expires_at<=$3 AND status NOT IN ('expired','deleting')`, tenantID, dumpID, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ListLogDumpCleanupCandidates(ctx context.Context, now time.Time, limit int) ([]LogDump, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+logDumpSelect+` FROM agent_log_dumps
		WHERE (expires_at<=$1 OR status IN ('expired','deleting')) AND (next_cleanup_at IS NULL OR next_cleanup_at<=$1)
		ORDER BY expires_at ASC LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []LogDump
	for rows.Next() {
		d, err := scanLogDump(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) MarkLogDumpDeleting(ctx context.Context, dumpID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_log_dumps SET status='deleting' WHERE id=$1`, dumpID)
	return err
}

func (s *Store) MarkLogDumpCleanupRetry(ctx context.Context, dumpID uuid.UUID, message string, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_log_dumps SET status='deleting',cleanup_attempts=cleanup_attempts+1,cleanup_error=$2,next_cleanup_at=$3 WHERE id=$1`, dumpID, nullableText(message), next)
	return err
}

func (s *Store) DeleteLogDump(ctx context.Context, dumpID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_log_dumps WHERE id=$1`, dumpID)
	return err
}

func (s *Store) ListTimedOutLogDumps(ctx context.Context, now, startedBefore time.Time, limit int) ([]LogDump, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+logDumpSelect+` FROM agent_log_dumps
		WHERE source='node_agent' AND expires_at>$1 AND status IN ('requested','capturing')
		AND COALESCE(capture_started_at,created_at)<=$2 ORDER BY created_at ASC LIMIT $3`, now, startedBefore, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []LogDump
	for rows.Next() {
		d, err := scanLogDump(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func validSHA256Hex(v string) bool {
	if len(v) != sha256.Size*2 {
		return false
	}
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == sha256.Size && v == strings.ToLower(v)
}

func nullableText(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}
