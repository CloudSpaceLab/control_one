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

// Policy represents a policy definition.
type Policy struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Name        string
	Description sql.NullString
	RuleType    string
	Enabled     bool
	Labels      map[string]string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  sql.NullTime
}

// PolicyVersion represents a versioned policy rule definition.
type PolicyVersion struct {
	ID             uuid.UUID
	PolicyID       uuid.UUID
	Version        int
	RuleDefinition string
	Checksum       sql.NullString
	Metadata       map[string]any
	CreatedBy      *uuid.UUID
	CreatedAt      time.Time
	PromotedAt     sql.NullTime
}

// PolicyAssignment represents a policy assigned to a tenant or node.
type PolicyAssignment struct {
	ID         uuid.UUID
	PolicyID   uuid.UUID
	TenantID   uuid.UUID
	NodeID     uuid.UUID
	AssignedAt time.Time
	AssignedBy *uuid.UUID
	ExpiresAt  sql.NullTime
}

// PolicyFilter captures filters for listing policies.
type PolicyFilter struct {
	TenantID        uuid.UUID
	NamePrefix      string
	RuleType        string
	Enabled         *bool
	IncludeArchived bool
}

// CreatePolicyParams defines input for creating a policy.
type CreatePolicyParams struct {
	TenantID    uuid.UUID
	Name        string
	Description *string
	RuleType    string
	Enabled     bool
	Labels      map[string]string
}

// UpdatePolicyParams captures patchable fields on a policy.
type UpdatePolicyParams struct {
	Name        *string
	Description *string
	RuleType    *string
	Enabled     *bool
	Labels      *map[string]string
	Archived    *bool
}

// CreatePolicyVersionParams defines input for creating a policy version.
type CreatePolicyVersionParams struct {
	PolicyID       uuid.UUID
	RuleDefinition string
	Checksum       *string
	Metadata       map[string]any
	CreatedBy      *uuid.UUID
}

// ListPolicies returns policies matching the provided filter.
func (s *Store) ListPolicies(ctx context.Context, filter PolicyFilter, limit, offset int) ([]Policy, int, error) {
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
	if strings.TrimSpace(filter.NamePrefix) != "" {
		args = append(args, strings.TrimSpace(filter.NamePrefix)+"%")
		clauses = append(clauses, fmt.Sprintf("name ILIKE $%d", len(args)))
	}
	if strings.TrimSpace(filter.RuleType) != "" {
		args = append(args, strings.TrimSpace(filter.RuleType))
		clauses = append(clauses, fmt.Sprintf("rule_type = $%d", len(args)))
	}
	if filter.Enabled != nil {
		args = append(args, *filter.Enabled)
		clauses = append(clauses, fmt.Sprintf("enabled = $%d", len(args)))
	}
	if !filter.IncludeArchived {
		clauses = append(clauses, "archived_at IS NULL")
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, description, rule_type, enabled, labels, created_at, updated_at, archived_at
		FROM policies
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(clauses, " AND "))

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM policies WHERE %s`, strings.Join(clauses, " AND "))

	argsForCount := make([]any, len(args))
	copy(argsForCount, args)

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	countRow := s.db.QueryRowContext(ctx, countQuery, argsForCount...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count policies: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query policies: %w", err)
	}
	defer rows.Close()

	var policies []Policy
	for rows.Next() {
		var p Policy
		var labelsRaw []byte
		if err := rows.Scan(
			&p.ID,
			&p.TenantID,
			&p.Name,
			&p.Description,
			&p.RuleType,
			&p.Enabled,
			&labelsRaw,
			&p.CreatedAt,
			&p.UpdatedAt,
			&p.ArchivedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan policy: %w", err)
		}
		labels, err := decodeStringMap(labelsRaw)
		if err != nil {
			return nil, 0, err
		}
		p.Labels = labels
		policies = append(policies, p)
	}

	return policies, total, nil
}

// GetPolicy returns a policy by ID.
func (s *Store) GetPolicy(ctx context.Context, id uuid.UUID) (*Policy, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("policy id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, description, rule_type, enabled, labels, created_at, updated_at, archived_at
		FROM policies
		WHERE id = $1
	`, id)

	var p Policy
	var labelsRaw []byte
	if err := row.Scan(
		&p.ID,
		&p.TenantID,
		&p.Name,
		&p.Description,
		&p.RuleType,
		&p.Enabled,
		&labelsRaw,
		&p.CreatedAt,
		&p.UpdatedAt,
		&p.ArchivedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get policy: %w", err)
	}

	labels, err := decodeStringMap(labelsRaw)
	if err != nil {
		return nil, err
	}
	p.Labels = labels

	return &p, nil
}

// CreatePolicy creates a new policy.
func (s *Store) CreatePolicy(ctx context.Context, params CreatePolicyParams) (*Policy, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		return nil, errors.New("policy name is required")
	}
	params.RuleType = strings.TrimSpace(params.RuleType)
	if params.RuleType == "" {
		return nil, errors.New("rule type is required")
	}

	labelsJSON, err := encodeStringMap(params.Labels)
	if err != nil {
		return nil, fmt.Errorf("encode labels: %w", err)
	}

	id := uuid.New()
	now := s.clock()
	desc := sql.NullString{}
	if params.Description != nil {
		desc = sql.NullString{String: strings.TrimSpace(*params.Description), Valid: true}
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO policies (id, tenant_id, name, description, rule_type, enabled, labels, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, name, description, rule_type, enabled, labels, created_at, updated_at, archived_at
	`, id, params.TenantID, params.Name, desc, params.RuleType, params.Enabled, labelsJSON, now, now)

	var p Policy
	var labelsRaw []byte
	if err := row.Scan(
		&p.ID,
		&p.TenantID,
		&p.Name,
		&p.Description,
		&p.RuleType,
		&p.Enabled,
		&labelsRaw,
		&p.CreatedAt,
		&p.UpdatedAt,
		&p.ArchivedAt,
	); err != nil {
		return nil, fmt.Errorf("create policy: %w", err)
	}

	labels, err := decodeStringMap(labelsRaw)
	if err != nil {
		return nil, err
	}
	p.Labels = labels

	return &p, nil
}

// UpdatePolicy updates a policy.
func (s *Store) UpdatePolicy(ctx context.Context, id uuid.UUID, params UpdatePolicyParams) (*Policy, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("policy id is required")
	}

	updates := []string{}
	args := []any{id}
	argPos := 2

	if params.Name != nil {
		name := strings.TrimSpace(*params.Name)
		if name == "" {
			return nil, errors.New("name cannot be empty")
		}
		args = append(args, name)
		updates = append(updates, fmt.Sprintf("name = $%d", argPos))
		argPos++
	}
	if params.Description != nil {
		args = append(args, *params.Description)
		updates = append(updates, fmt.Sprintf("description = $%d", argPos))
		argPos++
	}
	if params.RuleType != nil {
		ruleType := strings.TrimSpace(*params.RuleType)
		if ruleType == "" {
			return nil, errors.New("rule type cannot be empty")
		}
		args = append(args, ruleType)
		updates = append(updates, fmt.Sprintf("rule_type = $%d", argPos))
		argPos++
	}
	if params.Enabled != nil {
		args = append(args, *params.Enabled)
		updates = append(updates, fmt.Sprintf("enabled = $%d", argPos))
		argPos++
	}
	if params.Labels != nil {
		labelsJSON, err := encodeStringMap(*params.Labels)
		if err != nil {
			return nil, fmt.Errorf("encode labels: %w", err)
		}
		args = append(args, labelsJSON)
		updates = append(updates, fmt.Sprintf("labels = $%d", argPos))
		argPos++
	}
	if params.Archived != nil {
		now := s.clock()
		if *params.Archived {
			args = append(args, now)
			updates = append(updates, fmt.Sprintf("archived_at = $%d", argPos))
		} else {
			updates = append(updates, "archived_at = NULL")
		}
		argPos++
	}

	if len(updates) == 0 {
		return s.GetPolicy(ctx, id)
	}

	updates = append(updates, fmt.Sprintf("updated_at = $%d", argPos))
	args = append(args, s.clock())

	query := fmt.Sprintf(`
		UPDATE policies
		SET %s
		WHERE id = $1
		RETURNING id, tenant_id, name, description, rule_type, enabled, labels, created_at, updated_at, archived_at
	`, strings.Join(updates, ", "))

	row := s.db.QueryRowContext(ctx, query, args...)

	var p Policy
	var labelsRaw []byte
	if err := row.Scan(
		&p.ID,
		&p.TenantID,
		&p.Name,
		&p.Description,
		&p.RuleType,
		&p.Enabled,
		&labelsRaw,
		&p.CreatedAt,
		&p.UpdatedAt,
		&p.ArchivedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update policy: %w", err)
	}

	labels, err := decodeStringMap(labelsRaw)
	if err != nil {
		return nil, err
	}
	p.Labels = labels

	return &p, nil
}

// DeletePolicy removes a policy by ID.
func (s *Store) DeletePolicy(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("policy id is required")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM policies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete policy rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// ListPolicyVersions returns versions for a policy.
func (s *Store) ListPolicyVersions(ctx context.Context, policyID uuid.UUID, limit, offset int) ([]PolicyVersion, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if policyID == uuid.Nil {
		return nil, 0, errors.New("policy id is required")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	countRow := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM policy_versions WHERE policy_id = $1`, policyID)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count policy versions: %w", err)
	}

	query := `
		SELECT id, policy_id, version, rule_definition, checksum, metadata, created_by, created_at, promoted_at
		FROM policy_versions
		WHERE policy_id = $1
		ORDER BY version DESC
	`
	args := []any{policyID}
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
		return nil, 0, fmt.Errorf("query policy versions: %w", err)
	}
	defer rows.Close()

	var versions []PolicyVersion
	for rows.Next() {
		var v PolicyVersion
		var metadataRaw []byte
		if err := rows.Scan(
			&v.ID,
			&v.PolicyID,
			&v.Version,
			&v.RuleDefinition,
			&v.Checksum,
			&metadataRaw,
			&v.CreatedBy,
			&v.CreatedAt,
			&v.PromotedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan policy version: %w", err)
		}
		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &v.Metadata); err != nil {
				return nil, 0, fmt.Errorf("decode metadata: %w", err)
			}
		}
		if v.Metadata == nil {
			v.Metadata = make(map[string]any)
		}
		versions = append(versions, v)
	}

	return versions, total, nil
}

// GetPolicyVersion returns a specific policy version.
func (s *Store) GetPolicyVersion(ctx context.Context, policyID uuid.UUID, version int) (*PolicyVersion, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if policyID == uuid.Nil {
		return nil, errors.New("policy id is required")
	}
	if version <= 0 {
		return nil, errors.New("version must be positive")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, policy_id, version, rule_definition, checksum, metadata, created_by, created_at, promoted_at
		FROM policy_versions
		WHERE policy_id = $1 AND version = $2
	`, policyID, version)

	var v PolicyVersion
	var metadataRaw []byte
	if err := row.Scan(
		&v.ID,
		&v.PolicyID,
		&v.Version,
		&v.RuleDefinition,
		&v.Checksum,
		&metadataRaw,
		&v.CreatedBy,
		&v.CreatedAt,
		&v.PromotedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get policy version: %w", err)
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &v.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if v.Metadata == nil {
		v.Metadata = make(map[string]any)
	}

	return &v, nil
}

// GetPromotedPolicyVersion returns the promoted version for a policy.
func (s *Store) GetPromotedPolicyVersion(ctx context.Context, policyID uuid.UUID) (*PolicyVersion, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if policyID == uuid.Nil {
		return nil, errors.New("policy id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, policy_id, version, rule_definition, checksum, metadata, created_by, created_at, promoted_at
		FROM policy_versions
		WHERE policy_id = $1 AND promoted_at IS NOT NULL
		ORDER BY promoted_at DESC
		LIMIT 1
	`, policyID)

	var v PolicyVersion
	var metadataRaw []byte
	if err := row.Scan(
		&v.ID,
		&v.PolicyID,
		&v.Version,
		&v.RuleDefinition,
		&v.Checksum,
		&metadataRaw,
		&v.CreatedBy,
		&v.CreatedAt,
		&v.PromotedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get promoted policy version: %w", err)
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &v.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if v.Metadata == nil {
		v.Metadata = make(map[string]any)
	}

	return &v, nil
}

// CreatePolicyVersion creates a new policy version.
func (s *Store) CreatePolicyVersion(ctx context.Context, params CreatePolicyVersionParams) (*PolicyVersion, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.PolicyID == uuid.Nil {
		return nil, errors.New("policy id is required")
	}
	params.RuleDefinition = strings.TrimSpace(params.RuleDefinition)
	if params.RuleDefinition == "" {
		return nil, errors.New("rule definition is required")
	}

	var nextVersion int
	row := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM policy_versions
		WHERE policy_id = $1
	`, params.PolicyID)
	if err := row.Scan(&nextVersion); err != nil {
		return nil, fmt.Errorf("get next version: %w", err)
	}

	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	id := uuid.New()
	now := s.clock()
	checksum := sql.NullString{}
	if params.Checksum != nil {
		checksum = sql.NullString{String: strings.TrimSpace(*params.Checksum), Valid: true}
	}

	row = s.db.QueryRowContext(ctx, `
		INSERT INTO policy_versions (id, policy_id, version, rule_definition, checksum, metadata, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, policy_id, version, rule_definition, checksum, metadata, created_by, created_at, promoted_at
	`, id, params.PolicyID, nextVersion, params.RuleDefinition, checksum, metadataJSON, params.CreatedBy, now)

	var v PolicyVersion
	var metadataRaw []byte
	if err := row.Scan(
		&v.ID,
		&v.PolicyID,
		&v.Version,
		&v.RuleDefinition,
		&v.Checksum,
		&metadataRaw,
		&v.CreatedBy,
		&v.CreatedAt,
		&v.PromotedAt,
	); err != nil {
		return nil, fmt.Errorf("create policy version: %w", err)
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &v.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if v.Metadata == nil {
		v.Metadata = make(map[string]any)
	}

	return &v, nil
}

// PromotePolicyVersion promotes a policy version.
func (s *Store) PromotePolicyVersion(ctx context.Context, policyID uuid.UUID, version int) (*PolicyVersion, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if policyID == uuid.Nil {
		return nil, errors.New("policy id is required")
	}
	if version <= 0 {
		return nil, errors.New("version must be positive")
	}

	now := s.clock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE policy_versions
		SET promoted_at = NULL
		WHERE policy_id = $1 AND promoted_at IS NOT NULL
	`, policyID)
	if err != nil {
		return nil, fmt.Errorf("unpromote existing versions: %w", err)
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE policy_versions
		SET promoted_at = $3
		WHERE policy_id = $1 AND version = $2
		RETURNING id, policy_id, version, rule_definition, checksum, metadata, created_by, created_at, promoted_at
	`, policyID, version, now)

	var v PolicyVersion
	var metadataRaw []byte
	if err := row.Scan(
		&v.ID,
		&v.PolicyID,
		&v.Version,
		&v.RuleDefinition,
		&v.Checksum,
		&metadataRaw,
		&v.CreatedBy,
		&v.CreatedAt,
		&v.PromotedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("promote policy version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &v.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if v.Metadata == nil {
		v.Metadata = make(map[string]any)
	}

	return &v, nil
}
