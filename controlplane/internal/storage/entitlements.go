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

// AccessEntitlement represents a user's access entitlement to a node.
type AccessEntitlement struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	UserID    uuid.UUID
	NodeID    uuid.UUID
	GroupName sql.NullString
	Role      string
	GrantedBy uuid.NullUUID
	GrantedAt time.Time
	ExpiresAt sql.NullTime
	RevokedAt sql.NullTime
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AccessSyncHistory tracks access synchronization history.
type AccessSyncHistory struct {
	ID         uuid.UUID
	Provider   string
	SyncedAt   time.Time
	Status     string
	UserCount  int
	GroupCount int
	Error      sql.NullString
	Metadata   map[string]any
}

// CreateEntitlementParams defines input for creating an entitlement.
type CreateEntitlementParams struct {
	TenantID  uuid.UUID
	UserID    uuid.UUID
	NodeID    uuid.UUID
	GroupName *string
	Role      string
	GrantedBy *uuid.UUID
	ExpiresAt *time.Time
	Metadata  map[string]any
}

// UpdateEntitlementParams captures patchable fields on an entitlement.
type UpdateEntitlementParams struct {
	Role      *string
	ExpiresAt *time.Time
	Metadata  *map[string]any
}

// ListEntitlements returns entitlements with filtering.
func (s *Store) ListEntitlements(ctx context.Context, filter EntitlementFilter, limit, offset int) ([]AccessEntitlement, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if filter.TenantID != uuid.Nil {
		args = append(args, filter.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if filter.UserID != uuid.Nil {
		args = append(args, filter.UserID)
		clauses = append(clauses, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if filter.Role != "" {
		args = append(args, strings.TrimSpace(filter.Role))
		clauses = append(clauses, fmt.Sprintf("role = $%d", len(args)))
	}
	if filter.Expired != nil {
		now := s.clock()
		if *filter.Expired {
			args = append(args, now)
			clauses = append(clauses, fmt.Sprintf("expires_at IS NOT NULL AND expires_at < $%d", len(args)))
		} else {
			args = append(args, now)
			clauses = append(clauses, fmt.Sprintf("(expires_at IS NULL OR expires_at >= $%d)", len(args)))
		}
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM access_entitlements WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count entitlements: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, user_id, node_id, group_name, role, granted_by, granted_at, expires_at, revoked_at, metadata, created_at, updated_at
		FROM access_entitlements
		WHERE %s AND revoked_at IS NULL
		ORDER BY granted_at DESC
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
		return nil, 0, fmt.Errorf("query entitlements: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entitlements []AccessEntitlement
	for rows.Next() {
		var ent AccessEntitlement
		var grantedBy sql.NullString
		var expiresAt sql.NullTime
		var revokedAt sql.NullTime
		var metadataRaw []byte

		if err := rows.Scan(
			&ent.ID,
			&ent.TenantID,
			&ent.UserID,
			&ent.NodeID,
			&ent.GroupName,
			&ent.Role,
			&grantedBy,
			&ent.GrantedAt,
			&expiresAt,
			&revokedAt,
			&metadataRaw,
			&ent.CreatedAt,
			&ent.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan entitlement: %w", err)
		}

		if grantedBy.Valid {
			if id, err := uuid.Parse(grantedBy.String); err == nil {
				ent.GrantedBy = uuid.NullUUID{UUID: id, Valid: true}
			}
		}
		if expiresAt.Valid {
			ent.ExpiresAt = expiresAt
		}
		if revokedAt.Valid {
			ent.RevokedAt = revokedAt
		}
		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &ent.Metadata); err != nil {
				return nil, 0, fmt.Errorf("decode metadata: %w", err)
			}
		}
		if ent.Metadata == nil {
			ent.Metadata = make(map[string]any)
		}

		entitlements = append(entitlements, ent)
	}

	return entitlements, total, nil
}

// EntitlementFilter filters entitlements.
type EntitlementFilter struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
	NodeID   uuid.UUID
	Role     string
	Expired  *bool
}

// GetEntitlement returns an entitlement by ID.
func (s *Store) GetEntitlement(ctx context.Context, entitlementID uuid.UUID) (*AccessEntitlement, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if entitlementID == uuid.Nil {
		return nil, errors.New("entitlement id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, user_id, node_id, group_name, role, granted_by, granted_at, expires_at, revoked_at, metadata, created_at, updated_at
		FROM access_entitlements
		WHERE id = $1
	`, entitlementID)

	var ent AccessEntitlement
	var grantedBy sql.NullString
	var expiresAt sql.NullTime
	var revokedAt sql.NullTime
	var metadataRaw []byte

	if err := row.Scan(
		&ent.ID,
		&ent.TenantID,
		&ent.UserID,
		&ent.NodeID,
		&ent.GroupName,
		&ent.Role,
		&grantedBy,
		&ent.GrantedAt,
		&expiresAt,
		&revokedAt,
		&metadataRaw,
		&ent.CreatedAt,
		&ent.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get entitlement: %w", err)
	}

	if grantedBy.Valid {
		if id, err := uuid.Parse(grantedBy.String); err == nil {
			ent.GrantedBy = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	if expiresAt.Valid {
		ent.ExpiresAt = expiresAt
	}
	if revokedAt.Valid {
		ent.RevokedAt = revokedAt
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &ent.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if ent.Metadata == nil {
		ent.Metadata = make(map[string]any)
	}

	return &ent, nil
}

// CreateEntitlement creates a new entitlement.
func (s *Store) CreateEntitlement(ctx context.Context, params CreateEntitlementParams) (*AccessEntitlement, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.UserID == uuid.Nil {
		return nil, errors.New("user id is required")
	}
	if params.NodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	if strings.TrimSpace(params.Role) == "" {
		return nil, errors.New("role is required")
	}

	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	id := uuid.New()
	now := s.clock()
	grantedBy := sql.NullString{}
	if params.GrantedBy != nil {
		grantedBy = sql.NullString{String: params.GrantedBy.String(), Valid: true}
	}

	expiresAt := sql.NullTime{}
	if params.ExpiresAt != nil {
		expiresAt = sql.NullTime{Time: *params.ExpiresAt, Valid: true}
	}

	groupName := sql.NullString{}
	if params.GroupName != nil {
		groupName = sql.NullString{String: strings.TrimSpace(*params.GroupName), Valid: true}
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO access_entitlements (
			id, tenant_id, user_id, node_id, group_name, role, granted_by, granted_at, expires_at, metadata, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, tenant_id, user_id, node_id, group_name, role, granted_by, granted_at, expires_at, revoked_at, metadata, created_at, updated_at
	`, id, params.TenantID, params.UserID, params.NodeID, groupName, params.Role, grantedBy, now, expiresAt, metadataJSON, now, now)

	var ent AccessEntitlement
	var grantedByOut sql.NullString
	var expiresAtOut sql.NullTime
	var revokedAtOut sql.NullTime
	var metadataRaw []byte

	if err := row.Scan(
		&ent.ID,
		&ent.TenantID,
		&ent.UserID,
		&ent.NodeID,
		&ent.GroupName,
		&ent.Role,
		&grantedByOut,
		&ent.GrantedAt,
		&expiresAtOut,
		&revokedAtOut,
		&metadataRaw,
		&ent.CreatedAt,
		&ent.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create entitlement: %w", err)
	}

	if grantedByOut.Valid {
		if id, err := uuid.Parse(grantedByOut.String); err == nil {
			ent.GrantedBy = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	if expiresAtOut.Valid {
		ent.ExpiresAt = expiresAtOut
	}
	if revokedAtOut.Valid {
		ent.RevokedAt = revokedAtOut
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &ent.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if ent.Metadata == nil {
		ent.Metadata = make(map[string]any)
	}

	return &ent, nil
}

// UpdateEntitlement updates an entitlement.
func (s *Store) UpdateEntitlement(ctx context.Context, entitlementID uuid.UUID, params UpdateEntitlementParams) (*AccessEntitlement, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if entitlementID == uuid.Nil {
		return nil, errors.New("entitlement id is required")
	}

	updates := []string{}
	args := []any{entitlementID}
	argPos := 2

	if params.Role != nil {
		role := strings.TrimSpace(*params.Role)
		if role == "" {
			return nil, errors.New("role cannot be empty")
		}
		args = append(args, role)
		updates = append(updates, fmt.Sprintf("role = $%d", argPos))
		argPos++
	}
	if params.ExpiresAt != nil {
		expiresAt := sql.NullTime{Time: *params.ExpiresAt, Valid: true}
		args = append(args, expiresAt)
		updates = append(updates, fmt.Sprintf("expires_at = $%d", argPos))
		argPos++
	}
	if params.Metadata != nil {
		metadataJSON, err := json.Marshal(*params.Metadata)
		if err != nil {
			return nil, fmt.Errorf("encode metadata: %w", err)
		}
		args = append(args, metadataJSON)
		updates = append(updates, fmt.Sprintf("metadata = $%d", argPos))
		argPos++
	}

	if len(updates) == 0 {
		return s.GetEntitlement(ctx, entitlementID)
	}

	updates = append(updates, fmt.Sprintf("updated_at = $%d", argPos))
	args = append(args, s.clock())

	query := fmt.Sprintf(`
		UPDATE access_entitlements
		SET %s
		WHERE id = $1
		RETURNING id, tenant_id, user_id, node_id, group_name, role, granted_by, granted_at, expires_at, revoked_at, metadata, created_at, updated_at
	`, strings.Join(updates, ", "))

	row := s.db.QueryRowContext(ctx, query, args...)

	var ent AccessEntitlement
	var grantedBy sql.NullString
	var expiresAt sql.NullTime
	var revokedAt sql.NullTime
	var metadataRaw []byte

	if err := row.Scan(
		&ent.ID,
		&ent.TenantID,
		&ent.UserID,
		&ent.NodeID,
		&ent.GroupName,
		&ent.Role,
		&grantedBy,
		&ent.GrantedAt,
		&expiresAt,
		&revokedAt,
		&metadataRaw,
		&ent.CreatedAt,
		&ent.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update entitlement: %w", err)
	}

	if grantedBy.Valid {
		if id, err := uuid.Parse(grantedBy.String); err == nil {
			ent.GrantedBy = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	if expiresAt.Valid {
		ent.ExpiresAt = expiresAt
	}
	if revokedAt.Valid {
		ent.RevokedAt = revokedAt
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &ent.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if ent.Metadata == nil {
		ent.Metadata = make(map[string]any)
	}

	return &ent, nil
}

// DeleteEntitlement deletes an entitlement.
func (s *Store) DeleteEntitlement(ctx context.Context, entitlementID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if entitlementID == uuid.Nil {
		return errors.New("entitlement id is required")
	}

	_, err := s.db.ExecContext(ctx, `DELETE FROM access_entitlements WHERE id = $1`, entitlementID)
	if err != nil {
		return fmt.Errorf("delete entitlement: %w", err)
	}

	return nil
}

// RecordAccessSync records an access synchronization event.
func (s *Store) RecordAccessSync(ctx context.Context, tenantID, nodeID uuid.UUID, provider, syncType, status string, usersSynced, groupsSynced, entitlementsSynced int, syncErr error) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}

	now := s.clock()
	var errMsg sql.NullString
	if syncErr != nil {
		errMsg = sql.NullString{String: syncErr.Error(), Valid: true}
	}

	metadataJSON, _ := json.Marshal(map[string]any{})

	id := uuid.New()
	tenantIDNull := sql.NullString{}
	if tenantID != uuid.Nil {
		tenantIDNull = sql.NullString{String: tenantID.String(), Valid: true}
	}

	nodeIDNull := sql.NullString{}
	if nodeID != uuid.Nil {
		nodeIDNull = sql.NullString{String: nodeID.String(), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO access_syncs (
			id, tenant_id, node_id, provider, sync_type, synced_at, sync_status, sync_error, users_synced, groups_synced, entitlements_synced, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, id, tenantIDNull, nodeIDNull, provider, syncType, now, status, errMsg, usersSynced, groupsSynced, entitlementsSynced, metadataJSON)

	return err
}
