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

// TelemetryRetentionPolicy represents a retention policy for telemetry data.
type TelemetryRetentionPolicy struct {
	ID            uuid.UUID
	TenantID      uuid.NullUUID
	PolicyName    string
	DataType      string
	RetentionDays int
	Enabled       bool
	Metadata      map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateRetentionPolicyParams defines input for creating a retention policy.
type CreateRetentionPolicyParams struct {
	TenantID      *uuid.UUID
	PolicyName    string
	DataType      string
	RetentionDays int
	Enabled       *bool
	Metadata      map[string]any
}

// UpdateRetentionPolicyParams captures patchable fields on a retention policy.
type UpdateRetentionPolicyParams struct {
	RetentionDays *int
	Enabled       *bool
	Metadata      *map[string]any
}

// GetRetentionPolicy returns a retention policy by tenant and data type.
func (s *Store) GetRetentionPolicy(ctx context.Context, tenantID uuid.UUID, dataType string) (*TelemetryRetentionPolicy, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, policy_name, data_type, retention_days, enabled, metadata, created_at, updated_at
		FROM telemetry_retention_policies
		WHERE (tenant_id = $1 OR tenant_id IS NULL) AND data_type IN ($2, 'both') AND enabled = true
		ORDER BY CASE WHEN tenant_id = $1 THEN 0 ELSE 1 END
		LIMIT 1
	`, tenantID, dataType)

	var policy TelemetryRetentionPolicy
	var tenantIDNull sql.NullString
	var metadataRaw []byte

	if err := row.Scan(
		&policy.ID,
		&tenantIDNull,
		&policy.PolicyName,
		&policy.DataType,
		&policy.RetentionDays,
		&policy.Enabled,
		&metadataRaw,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get retention policy: %w", err)
	}

	if tenantIDNull.Valid {
		if id, err := uuid.Parse(tenantIDNull.String); err == nil {
			policy.TenantID = uuid.NullUUID{UUID: id, Valid: true}
		}
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &policy.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if policy.Metadata == nil {
		policy.Metadata = make(map[string]any)
	}

	return &policy, nil
}

// ListRetentionPolicies returns retention policies with filtering.
func (s *Store) ListRetentionPolicies(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]TelemetryRetentionPolicy, int, error) {
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
		clauses = append(clauses, fmt.Sprintf("(tenant_id = $%d OR tenant_id IS NULL)", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM telemetry_retention_policies WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count retention policies: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, policy_name, data_type, retention_days, enabled, metadata, created_at, updated_at
		FROM telemetry_retention_policies
		WHERE %s
		ORDER BY tenant_id NULLS LAST, policy_name
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
		return nil, 0, fmt.Errorf("query retention policies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var policies []TelemetryRetentionPolicy
	for rows.Next() {
		var policy TelemetryRetentionPolicy
		var tenantIDNull sql.NullString
		var metadataRaw []byte

		if err := rows.Scan(
			&policy.ID,
			&tenantIDNull,
			&policy.PolicyName,
			&policy.DataType,
			&policy.RetentionDays,
			&policy.Enabled,
			&metadataRaw,
			&policy.CreatedAt,
			&policy.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan retention policy: %w", err)
		}

		if tenantIDNull.Valid {
			if id, err := uuid.Parse(tenantIDNull.String); err == nil {
				policy.TenantID = uuid.NullUUID{UUID: id, Valid: true}
			}
		}

		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &policy.Metadata); err != nil {
				return nil, 0, fmt.Errorf("decode metadata: %w", err)
			}
		}
		if policy.Metadata == nil {
			policy.Metadata = make(map[string]any)
		}

		policies = append(policies, policy)
	}

	return policies, total, nil
}

// CreateRetentionPolicy creates a new retention policy.
func (s *Store) CreateRetentionPolicy(ctx context.Context, params CreateRetentionPolicyParams) (*TelemetryRetentionPolicy, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if strings.TrimSpace(params.PolicyName) == "" {
		return nil, errors.New("policy name is required")
	}
	if params.RetentionDays <= 0 {
		return nil, errors.New("retention days must be positive")
	}
	dataType := strings.ToLower(strings.TrimSpace(params.DataType))
	if dataType != "metrics" && dataType != "logs" && dataType != "both" {
		return nil, errors.New("data type must be metrics, logs, or both")
	}

	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	id := uuid.New()
	now := s.clock()
	tenantIDNull := sql.NullString{}
	if params.TenantID != nil {
		tenantIDNull = sql.NullString{String: params.TenantID.String(), Valid: true}
	}

	enabled := true
	if params.Enabled != nil {
		enabled = *params.Enabled
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO telemetry_retention_policies (
			id, tenant_id, policy_name, data_type, retention_days, enabled, metadata, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, policy_name, data_type, retention_days, enabled, metadata, created_at, updated_at
	`, id, tenantIDNull, params.PolicyName, dataType, params.RetentionDays, enabled, metadataJSON, now, now)

	var policy TelemetryRetentionPolicy
	var tenantIDNullOut sql.NullString
	var metadataRaw []byte

	if err := row.Scan(
		&policy.ID,
		&tenantIDNullOut,
		&policy.PolicyName,
		&policy.DataType,
		&policy.RetentionDays,
		&policy.Enabled,
		&metadataRaw,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create retention policy: %w", err)
	}

	if tenantIDNullOut.Valid {
		if id, err := uuid.Parse(tenantIDNullOut.String); err == nil {
			policy.TenantID = uuid.NullUUID{UUID: id, Valid: true}
		}
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &policy.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if policy.Metadata == nil {
		policy.Metadata = make(map[string]any)
	}

	return &policy, nil
}

// DeleteExpiredTelemetry deletes expired telemetry data based on retention policies.
func (s *Store) DeleteExpiredTelemetry(ctx context.Context, tenantID uuid.UUID, dataType string) (int64, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}

	policy, err := s.GetRetentionPolicy(ctx, tenantID, dataType)
	if err != nil {
		return 0, fmt.Errorf("get retention policy: %w", err)
	}
	if policy == nil {
		return 0, nil
	}

	cutoffDate := time.Now().AddDate(0, 0, -policy.RetentionDays)

	var result sql.Result
	if dataType == "metrics" || dataType == "both" {
		result, err = s.db.ExecContext(ctx, `
			DELETE FROM telemetry_metrics
			WHERE (tenant_id = $1 OR $1 IS NULL) AND timestamp < $2
		`, tenantID, cutoffDate)
		if err != nil {
			return 0, fmt.Errorf("delete expired metrics: %w", err)
		}
	}

	if dataType == "logs" || dataType == "both" {
		result, err = s.db.ExecContext(ctx, `
			DELETE FROM telemetry_logs
			WHERE (tenant_id = $1 OR $1 IS NULL) AND timestamp < $2
		`, tenantID, cutoffDate)
		if err != nil {
			return 0, fmt.Errorf("delete expired logs: %w", err)
		}
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}

	return rowsAffected, nil
}
