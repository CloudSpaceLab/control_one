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
	"github.com/lib/pq"
)

// EnrollmentToken represents a token used for single-command node enrollment.
type EnrollmentToken struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Name          string
	TokenHash     string
	MaxNodes      int
	NodesEnrolled int
	Labels        map[string]string
	Capabilities  []string
	ExpiresAt     time.Time
	RevokedAt     sql.NullTime
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
}

// CreateEnrollmentTokenParams captures the fields required to insert a new enrollment token.
type CreateEnrollmentTokenParams struct {
	TenantID     uuid.UUID
	Name         string
	TokenHash    string
	MaxNodes     int
	Labels       map[string]string
	Capabilities []string
	ExpiresAt    time.Time
	CreatedBy    *uuid.UUID
}

// CreateEnrollmentToken inserts a new enrollment token record.
func (s *Store) CreateEnrollmentToken(ctx context.Context, params CreateEnrollmentTokenParams) (*EnrollmentToken, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, errors.New("name is required")
	}
	if strings.TrimSpace(params.TokenHash) == "" {
		return nil, errors.New("token_hash is required")
	}

	labels := params.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}

	caps := params.Capabilities
	if caps == nil {
		caps = []string{}
	}

	var token EnrollmentToken
	var revokedAt sql.NullTime
	var createdBy sql.NullString
	var labelsRaw []byte
	var capsArray pq.StringArray

	cbVal := nullableUUIDPtr(params.CreatedBy)

	err = s.db.QueryRowContext(ctx, `
		INSERT INTO enrollment_tokens (tenant_id, name, token_hash, max_nodes, labels, capabilities, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, tenant_id, name, token_hash, max_nodes, nodes_enrolled, labels, capabilities, expires_at, revoked_at, created_by, created_at
	`, params.TenantID, strings.TrimSpace(params.Name), params.TokenHash, params.MaxNodes, labelsJSON, pq.Array(caps), params.ExpiresAt, cbVal).Scan(
		&token.ID, &token.TenantID, &token.Name, &token.TokenHash,
		&token.MaxNodes, &token.NodesEnrolled, &labelsRaw, &capsArray,
		&token.ExpiresAt, &revokedAt, &createdBy, &token.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert enrollment token: %w", err)
	}

	token.RevokedAt = revokedAt
	if createdBy.Valid {
		if id, err := uuid.Parse(createdBy.String); err == nil {
			token.CreatedBy = &id
		}
	}
	if err := json.Unmarshal(labelsRaw, &token.Labels); err != nil {
		token.Labels = make(map[string]string)
	}
	token.Capabilities = []string(capsArray)

	return &token, nil
}

// GetEnrollmentTokenByHash returns an enrollment token by its SHA-256 hash.
func (s *Store) GetEnrollmentTokenByHash(ctx context.Context, hash string) (*EnrollmentToken, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, errors.New("token hash is required")
	}

	var token EnrollmentToken
	var revokedAt sql.NullTime
	var createdBy sql.NullString
	var labelsRaw []byte
	var capsArray pq.StringArray

	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, token_hash, max_nodes, nodes_enrolled, labels, capabilities, expires_at, revoked_at, created_by, created_at
		FROM enrollment_tokens
		WHERE token_hash = $1
	`, hash).Scan(
		&token.ID, &token.TenantID, &token.Name, &token.TokenHash,
		&token.MaxNodes, &token.NodesEnrolled, &labelsRaw, &capsArray,
		&token.ExpiresAt, &revokedAt, &createdBy, &token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get enrollment token by hash: %w", err)
	}

	token.RevokedAt = revokedAt
	if createdBy.Valid {
		if id, err := uuid.Parse(createdBy.String); err == nil {
			token.CreatedBy = &id
		}
	}
	if err := json.Unmarshal(labelsRaw, &token.Labels); err != nil {
		token.Labels = make(map[string]string)
	}
	token.Capabilities = []string(capsArray)

	return &token, nil
}

// GetEnrollmentToken returns an enrollment token by ID.
func (s *Store) GetEnrollmentToken(ctx context.Context, id uuid.UUID) (*EnrollmentToken, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("token id is required")
	}

	var token EnrollmentToken
	var revokedAt sql.NullTime
	var createdBy sql.NullString
	var labelsRaw []byte
	var capsArray pq.StringArray

	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, token_hash, max_nodes, nodes_enrolled, labels, capabilities, expires_at, revoked_at, created_by, created_at
		FROM enrollment_tokens
		WHERE id = $1
	`, id).Scan(
		&token.ID, &token.TenantID, &token.Name, &token.TokenHash,
		&token.MaxNodes, &token.NodesEnrolled, &labelsRaw, &capsArray,
		&token.ExpiresAt, &revokedAt, &createdBy, &token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get enrollment token: %w", err)
	}

	token.RevokedAt = revokedAt
	if createdBy.Valid {
		if id, err := uuid.Parse(createdBy.String); err == nil {
			token.CreatedBy = &id
		}
	}
	if err := json.Unmarshal(labelsRaw, &token.Labels); err != nil {
		token.Labels = make(map[string]string)
	}
	token.Capabilities = []string(capsArray)

	return &token, nil
}

// ListEnrollmentTokens returns a paginated list of enrollment tokens for a tenant.
func (s *Store) ListEnrollmentTokens(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]EnrollmentToken, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant_id is required")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	countRow := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM enrollment_tokens WHERE tenant_id = $1`, tenantID)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count enrollment tokens: %w", err)
	}

	query := `
		SELECT id, tenant_id, name, token_hash, max_nodes, nodes_enrolled, labels, capabilities, expires_at, revoked_at, created_by, created_at
		FROM enrollment_tokens
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`
	args := []any{tenantID}

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
		return nil, 0, fmt.Errorf("query enrollment tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tokens []EnrollmentToken
	for rows.Next() {
		var token EnrollmentToken
		var revokedAt sql.NullTime
		var createdBy sql.NullString
		var labelsRaw []byte
		var capsArray pq.StringArray

		if err := rows.Scan(
			&token.ID, &token.TenantID, &token.Name, &token.TokenHash,
			&token.MaxNodes, &token.NodesEnrolled, &labelsRaw, &capsArray,
			&token.ExpiresAt, &revokedAt, &createdBy, &token.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan enrollment token: %w", err)
		}

		token.RevokedAt = revokedAt
		if createdBy.Valid {
			if id, err := uuid.Parse(createdBy.String); err == nil {
				token.CreatedBy = &id
			}
		}
		if err := json.Unmarshal(labelsRaw, &token.Labels); err != nil {
			token.Labels = make(map[string]string)
		}
		token.Capabilities = []string(capsArray)

		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate enrollment tokens: %w", err)
	}

	return tokens, total, nil
}

// RevokeEnrollmentToken marks a token as revoked by setting revoked_at to NOW().
func (s *Store) RevokeEnrollmentToken(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("token id is required")
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE enrollment_tokens SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL
	`, id)
	if err != nil {
		return fmt.Errorf("revoke enrollment token: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return errors.New("token not found or already revoked")
	}

	return nil
}

// IncrementEnrollmentCount increments nodes_enrolled by 1 for the given token.
func (s *Store) IncrementEnrollmentCount(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("token id is required")
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE enrollment_tokens SET nodes_enrolled = nodes_enrolled + 1 WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("increment enrollment count: %w", err)
	}

	return nil
}
