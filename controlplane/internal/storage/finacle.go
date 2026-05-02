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
)

// FinacleAuthMethod enumerates supported Finacle auth flavours. The CHECK
// constraint in 0085_finacle.up.sql keeps these in sync with the database.
const (
	FinacleAuthOAuth2ClientCreds = "oauth2_client_credentials"
	FinacleAuthBasic             = "basic"
)

// FinacleShiftModel enumerates the supported shift models.
const (
	FinacleShiftModel3Shift      = "3_shift"
	FinacleShiftModel2Shift      = "2_shift"
	FinacleShiftModelBranchHours = "branch_hours"
	FinacleShiftModelAlwaysOn    = "always_on"
)

// FinacleConnection is one Finacle host connection per tenant. credential_ref
// points into secret_groups (storage.SecretGroup) so OAuth client secrets and
// basic-auth passwords stay encrypted at rest.
type FinacleConnection struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Host          string
	AuthMethod    string
	CredentialRef sql.NullString
	LastSyncAt    sql.NullTime
	LastError     sql.NullString
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// FinacleShiftBand is one entry inside the FinacleShiftConfig.Shifts JSONB.
// Times are HH:MM in the branch's local time; the rotation worker compares
// time-of-day strings rather than wall-clock instants so DST is a non-issue.
type FinacleShiftBand struct {
	Name  string `json:"name"`
	Start string `json:"start"` // "HH:MM"
	End   string `json:"end"`   // "HH:MM"
}

// FinacleShiftConfig is one shift definition. branch_id is nullable so a
// tenant can have a default config that applies to every branch.
type FinacleShiftConfig struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	BranchID     sql.NullString
	Model        string
	Shifts       []FinacleShiftBand
	GraceMinutes int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// FinacleProfile is one Finacle user binding. The shift_rotate worker uses
// ListProfilesByShift to find every profile attached to a given shift.
type FinacleProfile struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	FinacleUID    string
	BranchID      sql.NullString
	Role          sql.NullString
	ShiftID       uuid.NullUUID
	Status        string
	LastRotatedAt sql.NullTime
}

// CreateFinacleConnectionParams ─ inputs for inserting a connection.
type CreateFinacleConnectionParams struct {
	TenantID      uuid.UUID
	Host          string
	AuthMethod    string
	CredentialRef *string
}

// UpdateFinacleConnectionParams ─ patchable fields. Nil fields are skipped.
type UpdateFinacleConnectionParams struct {
	Host          *string
	AuthMethod    *string
	CredentialRef *string
	LastSyncAt    *time.Time
	LastError     *string
}

// CreateFinacleShiftConfigParams ─ inputs for inserting a shift config.
type CreateFinacleShiftConfigParams struct {
	TenantID     uuid.UUID
	BranchID     *string
	Model        string
	Shifts       []FinacleShiftBand
	GraceMinutes int
}

// UpdateFinacleShiftConfigParams ─ patchable fields.
type UpdateFinacleShiftConfigParams struct {
	BranchID     *string
	Model        *string
	Shifts       []FinacleShiftBand
	GraceMinutes *int
}

// UpsertFinacleProfileParams ─ used by the sync job. (tenant_id, finacle_uid)
// is the natural key; CRUD flows reuse this for create-or-update semantics.
type UpsertFinacleProfileParams struct {
	TenantID   uuid.UUID
	FinacleUID string
	BranchID   *string
	Role       *string
	ShiftID    *uuid.UUID
	Status     string
}

// UpdateFinacleProfileParams ─ patchable fields for the profile API.
type UpdateFinacleProfileParams struct {
	BranchID *string
	Role     *string
	ShiftID  *uuid.UUID
	Status   *string
}

// CreateFinacleConnection inserts a new connection row.
func (s *Store) CreateFinacleConnection(ctx context.Context, p CreateFinacleConnectionParams) (*FinacleConnection, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	if strings.TrimSpace(p.Host) == "" {
		return nil, errors.New("host is required")
	}
	if !validFinacleAuthMethod(p.AuthMethod) {
		return nil, fmt.Errorf("invalid auth_method %q", p.AuthMethod)
	}

	id := uuid.New()
	now := s.clock()
	credRef := sql.NullString{}
	if p.CredentialRef != nil {
		trimmed := strings.TrimSpace(*p.CredentialRef)
		if trimmed != "" {
			credRef = sql.NullString{String: trimmed, Valid: true}
		}
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO finacle_connections (
			id, tenant_id, host, auth_method, credential_ref, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $6)
		RETURNING id, tenant_id, host, auth_method, credential_ref, last_sync_at, last_error, created_at, updated_at
	`, id, p.TenantID, strings.TrimSpace(p.Host), p.AuthMethod, credRef, now)

	var conn FinacleConnection
	if err := row.Scan(&conn.ID, &conn.TenantID, &conn.Host, &conn.AuthMethod, &conn.CredentialRef, &conn.LastSyncAt, &conn.LastError, &conn.CreatedAt, &conn.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create finacle connection: %w", err)
	}
	return &conn, nil
}

// GetFinacleConnection returns a connection by ID, or (nil, nil) if missing.
func (s *Store) GetFinacleConnection(ctx context.Context, id uuid.UUID) (*FinacleConnection, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, host, auth_method, credential_ref, last_sync_at, last_error, created_at, updated_at
		FROM finacle_connections WHERE id = $1
	`, id)

	var conn FinacleConnection
	if err := row.Scan(&conn.ID, &conn.TenantID, &conn.Host, &conn.AuthMethod, &conn.CredentialRef, &conn.LastSyncAt, &conn.LastError, &conn.CreatedAt, &conn.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get finacle connection: %w", err)
	}
	return &conn, nil
}

// ListFinacleConnections returns all connections for a tenant (ordered by host).
func (s *Store) ListFinacleConnections(ctx context.Context, tenantID uuid.UUID) ([]FinacleConnection, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, host, auth_method, credential_ref, last_sync_at, last_error, created_at, updated_at
		FROM finacle_connections
		WHERE tenant_id = $1
		ORDER BY host
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query finacle connections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FinacleConnection
	for rows.Next() {
		var conn FinacleConnection
		if err := rows.Scan(&conn.ID, &conn.TenantID, &conn.Host, &conn.AuthMethod, &conn.CredentialRef, &conn.LastSyncAt, &conn.LastError, &conn.CreatedAt, &conn.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan finacle connection: %w", err)
		}
		out = append(out, conn)
	}
	return out, rows.Err()
}

// UpdateFinacleConnection patches a connection row. Nil fields are skipped.
func (s *Store) UpdateFinacleConnection(ctx context.Context, id uuid.UUID, p UpdateFinacleConnectionParams) (*FinacleConnection, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	sets := []string{}
	args := []any{}

	if p.Host != nil {
		args = append(args, strings.TrimSpace(*p.Host))
		sets = append(sets, fmt.Sprintf("host = $%d", len(args)))
	}
	if p.AuthMethod != nil {
		if !validFinacleAuthMethod(*p.AuthMethod) {
			return nil, fmt.Errorf("invalid auth_method %q", *p.AuthMethod)
		}
		args = append(args, *p.AuthMethod)
		sets = append(sets, fmt.Sprintf("auth_method = $%d", len(args)))
	}
	if p.CredentialRef != nil {
		trimmed := strings.TrimSpace(*p.CredentialRef)
		if trimmed == "" {
			sets = append(sets, "credential_ref = NULL")
		} else {
			args = append(args, trimmed)
			sets = append(sets, fmt.Sprintf("credential_ref = $%d", len(args)))
		}
	}
	if p.LastSyncAt != nil {
		args = append(args, *p.LastSyncAt)
		sets = append(sets, fmt.Sprintf("last_sync_at = $%d", len(args)))
	}
	if p.LastError != nil {
		trimmed := strings.TrimSpace(*p.LastError)
		if trimmed == "" {
			sets = append(sets, "last_error = NULL")
		} else {
			args = append(args, trimmed)
			sets = append(sets, fmt.Sprintf("last_error = $%d", len(args)))
		}
	}

	args = append(args, s.clock())
	sets = append(sets, fmt.Sprintf("updated_at = $%d", len(args)))

	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE finacle_connections SET %s
		WHERE id = $%d
		RETURNING id, tenant_id, host, auth_method, credential_ref, last_sync_at, last_error, created_at, updated_at
	`, strings.Join(sets, ", "), len(args))

	row := s.db.QueryRowContext(ctx, q, args...)
	var conn FinacleConnection
	if err := row.Scan(&conn.ID, &conn.TenantID, &conn.Host, &conn.AuthMethod, &conn.CredentialRef, &conn.LastSyncAt, &conn.LastError, &conn.CreatedAt, &conn.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update finacle connection: %w", err)
	}
	return &conn, nil
}

// DeleteFinacleConnection removes a connection.
func (s *Store) DeleteFinacleConnection(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM finacle_connections WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete finacle connection: %w", err)
	}
	return nil
}

// CreateFinacleShiftConfig inserts a new shift config.
func (s *Store) CreateFinacleShiftConfig(ctx context.Context, p CreateFinacleShiftConfigParams) (*FinacleShiftConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	if !validFinacleShiftModel(p.Model) {
		return nil, fmt.Errorf("invalid shift model %q", p.Model)
	}

	if p.Shifts == nil {
		p.Shifts = []FinacleShiftBand{}
	}
	shiftsJSON, err := json.Marshal(p.Shifts)
	if err != nil {
		return nil, fmt.Errorf("marshal shifts: %w", err)
	}

	branchID := sql.NullString{}
	if p.BranchID != nil {
		trimmed := strings.TrimSpace(*p.BranchID)
		if trimmed != "" {
			branchID = sql.NullString{String: trimmed, Valid: true}
		}
	}

	grace := p.GraceMinutes
	if grace <= 0 {
		grace = 15
	}

	id := uuid.New()
	now := s.clock()
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO finacle_shift_configs (
			id, tenant_id, branch_id, model, shifts, grace_minutes, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING id, tenant_id, branch_id, model, shifts, grace_minutes, created_at, updated_at
	`, id, p.TenantID, branchID, p.Model, shiftsJSON, grace, now)

	return scanFinacleShiftConfig(row.Scan)
}

// GetFinacleShiftConfig returns one shift config by ID.
func (s *Store) GetFinacleShiftConfig(ctx context.Context, id uuid.UUID) (*FinacleShiftConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, branch_id, model, shifts, grace_minutes, created_at, updated_at
		FROM finacle_shift_configs WHERE id = $1
	`, id)
	cfg, err := scanFinacleShiftConfig(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return cfg, nil
}

// ListFinacleShiftConfigs returns all shift configs for a tenant.
func (s *Store) ListFinacleShiftConfigs(ctx context.Context, tenantID uuid.UUID) ([]FinacleShiftConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, branch_id, model, shifts, grace_minutes, created_at, updated_at
		FROM finacle_shift_configs
		WHERE tenant_id = $1
		ORDER BY branch_id NULLS FIRST, model
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query finacle shift configs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FinacleShiftConfig
	for rows.Next() {
		cfg, err := scanFinacleShiftConfig(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *cfg)
	}
	return out, rows.Err()
}

// UpdateFinacleShiftConfig patches a shift config.
func (s *Store) UpdateFinacleShiftConfig(ctx context.Context, id uuid.UUID, p UpdateFinacleShiftConfigParams) (*FinacleShiftConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	sets := []string{}
	args := []any{}

	if p.BranchID != nil {
		trimmed := strings.TrimSpace(*p.BranchID)
		if trimmed == "" {
			sets = append(sets, "branch_id = NULL")
		} else {
			args = append(args, trimmed)
			sets = append(sets, fmt.Sprintf("branch_id = $%d", len(args)))
		}
	}
	if p.Model != nil {
		if !validFinacleShiftModel(*p.Model) {
			return nil, fmt.Errorf("invalid shift model %q", *p.Model)
		}
		args = append(args, *p.Model)
		sets = append(sets, fmt.Sprintf("model = $%d", len(args)))
	}
	if p.Shifts != nil {
		shiftsJSON, err := json.Marshal(p.Shifts)
		if err != nil {
			return nil, fmt.Errorf("marshal shifts: %w", err)
		}
		args = append(args, shiftsJSON)
		sets = append(sets, fmt.Sprintf("shifts = $%d", len(args)))
	}
	if p.GraceMinutes != nil {
		args = append(args, *p.GraceMinutes)
		sets = append(sets, fmt.Sprintf("grace_minutes = $%d", len(args)))
	}

	args = append(args, s.clock())
	sets = append(sets, fmt.Sprintf("updated_at = $%d", len(args)))

	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE finacle_shift_configs SET %s
		WHERE id = $%d
		RETURNING id, tenant_id, branch_id, model, shifts, grace_minutes, created_at, updated_at
	`, strings.Join(sets, ", "), len(args))

	row := s.db.QueryRowContext(ctx, q, args...)
	cfg, err := scanFinacleShiftConfig(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return cfg, nil
}

// DeleteFinacleShiftConfig removes a shift config. ON DELETE SET NULL keeps
// any bound profiles by clearing their shift_id.
func (s *Store) DeleteFinacleShiftConfig(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM finacle_shift_configs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete finacle shift config: %w", err)
	}
	return nil
}

// UpsertFinacleProfile creates or updates a profile keyed by (tenant_id, finacle_uid).
// Used by the sync worker on every poll.
func (s *Store) UpsertFinacleProfile(ctx context.Context, p UpsertFinacleProfileParams) (*FinacleProfile, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	if strings.TrimSpace(p.FinacleUID) == "" {
		return nil, errors.New("finacle_uid is required")
	}

	branchID := sql.NullString{}
	if p.BranchID != nil {
		trimmed := strings.TrimSpace(*p.BranchID)
		if trimmed != "" {
			branchID = sql.NullString{String: trimmed, Valid: true}
		}
	}
	role := sql.NullString{}
	if p.Role != nil {
		trimmed := strings.TrimSpace(*p.Role)
		if trimmed != "" {
			role = sql.NullString{String: trimmed, Valid: true}
		}
	}
	shiftID := uuid.NullUUID{}
	if p.ShiftID != nil && *p.ShiftID != uuid.Nil {
		shiftID = uuid.NullUUID{UUID: *p.ShiftID, Valid: true}
	}
	status := strings.TrimSpace(p.Status)
	if status == "" {
		status = "unknown"
	}

	id := uuid.New()
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO finacle_profiles (
			id, tenant_id, finacle_uid, branch_id, role, shift_id, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, finacle_uid) DO UPDATE SET
			branch_id = EXCLUDED.branch_id,
			role      = EXCLUDED.role,
			shift_id  = EXCLUDED.shift_id,
			status    = EXCLUDED.status
		RETURNING id, tenant_id, finacle_uid, branch_id, role, shift_id, status, last_rotated_at
	`, id, p.TenantID, strings.TrimSpace(p.FinacleUID), branchID, role, shiftID, status)

	var prof FinacleProfile
	if err := row.Scan(&prof.ID, &prof.TenantID, &prof.FinacleUID, &prof.BranchID, &prof.Role, &prof.ShiftID, &prof.Status, &prof.LastRotatedAt); err != nil {
		return nil, fmt.Errorf("upsert finacle profile: %w", err)
	}
	return &prof, nil
}

// UpdateFinacleProfile patches a profile. Used by the admin patch endpoint.
func (s *Store) UpdateFinacleProfile(ctx context.Context, id uuid.UUID, p UpdateFinacleProfileParams) (*FinacleProfile, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	sets := []string{}
	args := []any{}

	if p.BranchID != nil {
		trimmed := strings.TrimSpace(*p.BranchID)
		if trimmed == "" {
			sets = append(sets, "branch_id = NULL")
		} else {
			args = append(args, trimmed)
			sets = append(sets, fmt.Sprintf("branch_id = $%d", len(args)))
		}
	}
	if p.Role != nil {
		trimmed := strings.TrimSpace(*p.Role)
		if trimmed == "" {
			sets = append(sets, "role = NULL")
		} else {
			args = append(args, trimmed)
			sets = append(sets, fmt.Sprintf("role = $%d", len(args)))
		}
	}
	if p.ShiftID != nil {
		if *p.ShiftID == uuid.Nil {
			sets = append(sets, "shift_id = NULL")
		} else {
			args = append(args, *p.ShiftID)
			sets = append(sets, fmt.Sprintf("shift_id = $%d", len(args)))
		}
	}
	if p.Status != nil {
		args = append(args, strings.TrimSpace(*p.Status))
		sets = append(sets, fmt.Sprintf("status = $%d", len(args)))
	}

	if len(sets) == 0 {
		return s.GetFinacleProfile(ctx, id)
	}

	args = append(args, id)
	q := fmt.Sprintf(`
		UPDATE finacle_profiles SET %s
		WHERE id = $%d
		RETURNING id, tenant_id, finacle_uid, branch_id, role, shift_id, status, last_rotated_at
	`, strings.Join(sets, ", "), len(args))

	row := s.db.QueryRowContext(ctx, q, args...)
	var prof FinacleProfile
	if err := row.Scan(&prof.ID, &prof.TenantID, &prof.FinacleUID, &prof.BranchID, &prof.Role, &prof.ShiftID, &prof.Status, &prof.LastRotatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update finacle profile: %w", err)
	}
	return &prof, nil
}

// GetFinacleProfile returns a single profile by ID.
func (s *Store) GetFinacleProfile(ctx context.Context, id uuid.UUID) (*FinacleProfile, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, finacle_uid, branch_id, role, shift_id, status, last_rotated_at
		FROM finacle_profiles WHERE id = $1
	`, id)

	var prof FinacleProfile
	if err := row.Scan(&prof.ID, &prof.TenantID, &prof.FinacleUID, &prof.BranchID, &prof.Role, &prof.ShiftID, &prof.Status, &prof.LastRotatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get finacle profile: %w", err)
	}
	return &prof, nil
}

// ListFinacleProfiles returns profiles for a tenant ordered by branch+UID.
func (s *Store) ListFinacleProfiles(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]FinacleProfile, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}
	if limit == 0 {
		limit = 100
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM finacle_profiles WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count finacle profiles: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, finacle_uid, branch_id, role, shift_id, status, last_rotated_at
		FROM finacle_profiles
		WHERE tenant_id = $1
		ORDER BY branch_id NULLS LAST, finacle_uid
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query finacle profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FinacleProfile
	for rows.Next() {
		var prof FinacleProfile
		if err := rows.Scan(&prof.ID, &prof.TenantID, &prof.FinacleUID, &prof.BranchID, &prof.Role, &prof.ShiftID, &prof.Status, &prof.LastRotatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan finacle profile: %w", err)
		}
		out = append(out, prof)
	}
	return out, total, rows.Err()
}

// ListFinacleProfilesByShift returns every profile bound to a given shift_id.
// The shift_rotate worker calls this at every shift boundary.
func (s *Store) ListFinacleProfilesByShift(ctx context.Context, shiftID uuid.UUID) ([]FinacleProfile, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, finacle_uid, branch_id, role, shift_id, status, last_rotated_at
		FROM finacle_profiles
		WHERE shift_id = $1
		ORDER BY finacle_uid
	`, shiftID)
	if err != nil {
		return nil, fmt.Errorf("query finacle profiles by shift: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FinacleProfile
	for rows.Next() {
		var prof FinacleProfile
		if err := rows.Scan(&prof.ID, &prof.TenantID, &prof.FinacleUID, &prof.BranchID, &prof.Role, &prof.ShiftID, &prof.Status, &prof.LastRotatedAt); err != nil {
			return nil, fmt.Errorf("scan finacle profile: %w", err)
		}
		out = append(out, prof)
	}
	return out, rows.Err()
}

// MarkFinacleProfileRotated stamps last_rotated_at and updates status. Called
// by the rotation worker on each successful enable/disable.
func (s *Store) MarkFinacleProfileRotated(ctx context.Context, id uuid.UUID, status string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE finacle_profiles SET status = $1, last_rotated_at = $2 WHERE id = $3
	`, strings.TrimSpace(status), s.clock(), id)
	if err != nil {
		return fmt.Errorf("mark finacle profile rotated: %w", err)
	}
	return nil
}

// DeleteFinacleProfile removes a profile.
func (s *Store) DeleteFinacleProfile(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM finacle_profiles WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete finacle profile: %w", err)
	}
	return nil
}

// scanFinacleShiftConfig is shared by GetFinacleShiftConfig, ListFinacleShiftConfigs,
// and the Update path. The scan callback isolates the row vs. rows split.
func scanFinacleShiftConfig(scan func(...any) error) (*FinacleShiftConfig, error) {
	var cfg FinacleShiftConfig
	var shiftsJSON []byte
	if err := scan(&cfg.ID, &cfg.TenantID, &cfg.BranchID, &cfg.Model, &shiftsJSON, &cfg.GraceMinutes, &cfg.CreatedAt, &cfg.UpdatedAt); err != nil {
		return nil, err
	}
	if len(shiftsJSON) > 0 {
		if err := json.Unmarshal(shiftsJSON, &cfg.Shifts); err != nil {
			return nil, fmt.Errorf("unmarshal shifts: %w", err)
		}
	}
	if cfg.Shifts == nil {
		cfg.Shifts = []FinacleShiftBand{}
	}
	return &cfg, nil
}

func validFinacleAuthMethod(method string) bool {
	switch method {
	case FinacleAuthOAuth2ClientCreds, FinacleAuthBasic:
		return true
	}
	return false
}

func validFinacleShiftModel(model string) bool {
	switch model {
	case FinacleShiftModel3Shift, FinacleShiftModel2Shift, FinacleShiftModelBranchHours, FinacleShiftModelAlwaysOn:
		return true
	}
	return false
}
