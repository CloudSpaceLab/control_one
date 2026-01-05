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

// SecretGroup represents a secret group configuration.
type SecretGroup struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Name               string
	Backend            string
	Endpoint           sql.NullString
	SyncIntervalSeconds sql.NullInt64
	LastSyncAt         sql.NullTime
	SyncStatus         string
	SyncError          sql.NullString
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// SecretSync tracks individual secret synchronization.
type SecretSync struct {
	ID            uuid.UUID
	SecretGroupID uuid.UUID
	NodeID        uuid.NullUUID
	SecretPath    string
	SecretVersion sql.NullString
	SyncedAt      time.Time
	SyncStatus    string
	SyncError     sql.NullString
	Metadata      map[string]any
}

// CreateSecretGroupParams defines input for creating a secret group.
type CreateSecretGroupParams struct {
	TenantID           uuid.UUID
	Name               string
	Backend            string
	Endpoint           *string
	SyncIntervalSeconds  *int
}

// UpdateSecretGroupParams captures patchable fields on a secret group.
type UpdateSecretGroupParams struct {
	Endpoint          *string
	SyncIntervalSeconds *int
}

// ListSecretGroups returns secret groups with filtering.
func (s *Store) ListSecretGroups(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]SecretGroup, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if tenantID != uuid.Nil {
		args = append(args, tenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM secret_groups WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count secret groups: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, backend, endpoint, sync_interval_seconds, last_sync_at, sync_status, sync_error, created_at, updated_at
		FROM secret_groups
		WHERE %s
		ORDER BY name
	`, strings.Join(clauses, " AND "))

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query secret groups: %w", err)
	}
	defer rows.Close()

	var groups []SecretGroup
	for rows.Next() {
		var group SecretGroup

		if err := rows.Scan(
			&group.ID,
			&group.TenantID,
			&group.Name,
			&group.Backend,
			&group.Endpoint,
			&group.SyncIntervalSeconds,
			&group.LastSyncAt,
			&group.SyncStatus,
			&group.SyncError,
			&group.CreatedAt,
			&group.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan secret group: %w", err)
		}

		groups = append(groups, group)
	}

	return groups, total, nil
}

// GetSecretGroup returns a secret group by ID.
func (s *Store) GetSecretGroup(ctx context.Context, groupID uuid.UUID) (*SecretGroup, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if groupID == uuid.Nil {
		return nil, errors.New("group id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, backend, endpoint, sync_interval_seconds, last_sync_at, sync_status, sync_error, created_at, updated_at
		FROM secret_groups
		WHERE id = $1
	`, groupID)

	var group SecretGroup

	if err := row.Scan(
		&group.ID,
		&group.TenantID,
		&group.Name,
		&group.Backend,
		&group.Endpoint,
		&group.SyncIntervalSeconds,
		&group.LastSyncAt,
		&group.SyncStatus,
		&group.SyncError,
		&group.CreatedAt,
		&group.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get secret group: %w", err)
	}

	return &group, nil
}

// CreateSecretGroup creates a new secret group.
func (s *Store) CreateSecretGroup(ctx context.Context, params CreateSecretGroupParams) (*SecretGroup, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, errors.New("name is required")
	}
	if strings.TrimSpace(params.Backend) == "" {
		return nil, errors.New("backend is required")
	}

	id := uuid.New()
	now := s.clock()

	endpoint := sql.NullString{}
	if params.Endpoint != nil {
		endpoint = sql.NullString{String: strings.TrimSpace(*params.Endpoint), Valid: true}
	}

	syncInterval := sql.NullInt64{}
	if params.SyncIntervalSeconds != nil {
		syncInterval = sql.NullInt64{Int64: int64(*params.SyncIntervalSeconds), Valid: true}
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO secret_groups (
			id, tenant_id, name, backend, endpoint, sync_interval_seconds, sync_status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, name, backend, endpoint, sync_interval_seconds, last_sync_at, sync_status, sync_error, created_at, updated_at
	`, id, params.TenantID, params.Name, params.Backend, endpoint, syncInterval, "pending", now, now)

	var group SecretGroup

	if err := row.Scan(
		&group.ID,
		&group.TenantID,
		&group.Name,
		&group.Backend,
		&group.Endpoint,
		&group.SyncIntervalSeconds,
		&group.LastSyncAt,
		&group.SyncStatus,
		&group.SyncError,
		&group.CreatedAt,
		&group.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create secret group: %w", err)
	}

	return &group, nil
}

// UpdateSecretGroupSyncStatus updates the sync status and last synced time.
func (s *Store) UpdateSecretGroupSyncStatus(ctx context.Context, groupID uuid.UUID, status string, syncErr error) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if groupID == uuid.Nil {
		return errors.New("group id is required")
	}

	now := s.clock()
	var errMsg sql.NullString
	if syncErr != nil {
		errMsg = sql.NullString{String: syncErr.Error(), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE secret_groups
		SET last_sync_at = $1, sync_status = $2, sync_error = $3, updated_at = $4
		WHERE id = $5
	`, now, status, errMsg, now, groupID)
	if err != nil {
		return fmt.Errorf("update secret group sync status: %w", err)
	}

	return nil
}

// ListSecretSyncs returns sync records for a secret group.
func (s *Store) ListSecretSyncs(ctx context.Context, groupID uuid.UUID, limit, offset int) ([]SecretSync, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	countQuery := `SELECT COUNT(*) FROM secret_syncs WHERE secret_group_id = $1`
	countRow := s.db.QueryRowContext(ctx, countQuery, groupID)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count secret syncs: %w", err)
	}

	query := `
		SELECT id, secret_group_id, node_id, secret_path, secret_version, synced_at, sync_status, sync_error, metadata
		FROM secret_syncs
		WHERE secret_group_id = $1
		ORDER BY synced_at DESC
	`

	args := []any{groupID}
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query secret syncs: %w", err)
	}
	defer rows.Close()

	var syncs []SecretSync
	for rows.Next() {
		var sync SecretSync
		var metadataRaw []byte

		if err := rows.Scan(
			&sync.ID,
			&sync.SecretGroupID,
			&sync.NodeID,
			&sync.SecretPath,
			&sync.SecretVersion,
			&sync.SyncedAt,
			&sync.SyncStatus,
			&sync.SyncError,
			&metadataRaw,
		); err != nil {
			return nil, 0, fmt.Errorf("scan secret sync: %w", err)
		}

		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &sync.Metadata); err != nil {
				return nil, 0, fmt.Errorf("decode metadata: %w", err)
			}
		}
		if sync.Metadata == nil {
			sync.Metadata = make(map[string]any)
		}

		syncs = append(syncs, sync)
	}

	return syncs, total, nil
}

