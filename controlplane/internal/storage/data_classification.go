package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DataClassificationRule is a tenant-scoped regex pattern for detecting PII.
type DataClassificationRule struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Name      string
	PIIType   string
	Regex     string
	Severity  string
	Enabled   bool
	CreatedAt time.Time
}

// ColumnClassification stores the result of scanning a single DB column.
type ColumnClassification struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	NodeID         uuid.UUID
	DatabaseName   string
	SchemaName     string
	TableName      string
	ColumnName     string
	PIIType        *string
	Encrypted      *bool
	EncryptionKind *string
	MinValueLength *int
	MaxValueLength *int
	SampleCount    *int
	LastScannedAt  *time.Time
}

// PIIFinding represents a detected PII issue linked to a column classification.
type PIIFinding struct {
	ID                     uuid.UUID
	TenantID               uuid.UUID
	ColumnClassificationID *uuid.UUID
	RuleID                 *uuid.UUID
	Severity               string
	Details                *string
	ResolvedAt             *time.Time
	ResolvedBy             *uuid.UUID
	CreatedAt              time.Time
}

// ListDataClassificationRules returns all rules for the given tenant.
func (s *Store) ListDataClassificationRules(ctx context.Context, tenantID uuid.UUID) ([]DataClassificationRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, pii_type, regex, severity, enabled, created_at
		FROM data_classification_rules
		WHERE tenant_id = $1
		ORDER BY created_at ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list data classification rules: %w", err)
	}
	defer rows.Close()

	var out []DataClassificationRule
	for rows.Next() {
		var r DataClassificationRule
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.PIIType, &r.Regex, &r.Severity, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan data classification rule: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateDataClassificationRule inserts a new rule and returns it.
func (s *Store) CreateDataClassificationRule(ctx context.Context, rule *DataClassificationRule) (*DataClassificationRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if rule.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	if rule.Severity == "" {
		rule.Severity = "medium"
	}
	out := &DataClassificationRule{}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO data_classification_rules (tenant_id, name, pii_type, regex, severity, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, name, pii_type, regex, severity, enabled, created_at
	`, rule.TenantID, rule.Name, rule.PIIType, rule.Regex, rule.Severity, rule.Enabled).
		Scan(&out.ID, &out.TenantID, &out.Name, &out.PIIType, &out.Regex, &out.Severity, &out.Enabled, &out.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create data classification rule: %w", err)
	}
	return out, nil
}

// DeleteDataClassificationRule removes a rule by ID.
func (s *Store) DeleteDataClassificationRule(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM data_classification_rules WHERE id = $1`, id)
	return err
}

// UpsertColumnClassification inserts or updates a column classification record.
func (s *Store) UpsertColumnClassification(ctx context.Context, cc *ColumnClassification) (*ColumnClassification, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	out := &ColumnClassification{}
	var (
		piiType        sql.NullString
		encrypted      sql.NullBool
		encryptionKind sql.NullString
		minLen         sql.NullInt64
		maxLen         sql.NullInt64
		sampleCount    sql.NullInt64
		lastScannedAt  sql.NullTime
	)
	if cc.PIIType != nil {
		piiType = sql.NullString{String: *cc.PIIType, Valid: true}
	}
	if cc.Encrypted != nil {
		encrypted = sql.NullBool{Bool: *cc.Encrypted, Valid: true}
	}
	if cc.EncryptionKind != nil {
		encryptionKind = sql.NullString{String: *cc.EncryptionKind, Valid: true}
	}
	if cc.MinValueLength != nil {
		minLen = sql.NullInt64{Int64: int64(*cc.MinValueLength), Valid: true}
	}
	if cc.MaxValueLength != nil {
		maxLen = sql.NullInt64{Int64: int64(*cc.MaxValueLength), Valid: true}
	}
	if cc.SampleCount != nil {
		sampleCount = sql.NullInt64{Int64: int64(*cc.SampleCount), Valid: true}
	}
	if cc.LastScannedAt != nil {
		lastScannedAt = sql.NullTime{Time: *cc.LastScannedAt, Valid: true}
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO column_classifications
			(tenant_id, node_id, database_name, schema_name, table_name, column_name,
			 pii_type, encrypted, encryption_kind, min_value_length, max_value_length,
			 sample_count, last_scanned_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (tenant_id, node_id, database_name, schema_name, table_name, column_name)
		DO UPDATE SET
			pii_type        = EXCLUDED.pii_type,
			encrypted       = EXCLUDED.encrypted,
			encryption_kind = EXCLUDED.encryption_kind,
			min_value_length = EXCLUDED.min_value_length,
			max_value_length = EXCLUDED.max_value_length,
			sample_count    = EXCLUDED.sample_count,
			last_scanned_at = EXCLUDED.last_scanned_at
		RETURNING id, tenant_id, node_id, database_name, schema_name, table_name, column_name,
			pii_type, encrypted, encryption_kind, min_value_length, max_value_length,
			sample_count, last_scanned_at
	`, cc.TenantID, cc.NodeID, cc.DatabaseName, cc.SchemaName, cc.TableName, cc.ColumnName,
		piiType, encrypted, encryptionKind, minLen, maxLen, sampleCount, lastScannedAt)

	var (
		outPIIType        sql.NullString
		outEncrypted      sql.NullBool
		outEncryptionKind sql.NullString
		outMinLen         sql.NullInt64
		outMaxLen         sql.NullInt64
		outSampleCount    sql.NullInt64
		outLastScannedAt  sql.NullTime
	)
	err := row.Scan(
		&out.ID, &out.TenantID, &out.NodeID, &out.DatabaseName, &out.SchemaName,
		&out.TableName, &out.ColumnName,
		&outPIIType, &outEncrypted, &outEncryptionKind, &outMinLen, &outMaxLen,
		&outSampleCount, &outLastScannedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert column classification: %w", err)
	}
	if outPIIType.Valid {
		out.PIIType = &outPIIType.String
	}
	if outEncrypted.Valid {
		out.Encrypted = &outEncrypted.Bool
	}
	if outEncryptionKind.Valid {
		out.EncryptionKind = &outEncryptionKind.String
	}
	if outMinLen.Valid {
		v := int(outMinLen.Int64)
		out.MinValueLength = &v
	}
	if outMaxLen.Valid {
		v := int(outMaxLen.Int64)
		out.MaxValueLength = &v
	}
	if outSampleCount.Valid {
		v := int(outSampleCount.Int64)
		out.SampleCount = &v
	}
	if outLastScannedAt.Valid {
		out.LastScannedAt = &outLastScannedAt.Time
	}
	return out, nil
}

// ListColumnClassifications returns paginated column classifications for a tenant.
func (s *Store) ListColumnClassifications(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]ColumnClassification, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM column_classifications WHERE tenant_id = $1`, tenantID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count column classifications: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, database_name, schema_name, table_name, column_name,
			pii_type, encrypted, encryption_kind, min_value_length, max_value_length,
			sample_count, last_scanned_at
		FROM column_classifications
		WHERE tenant_id = $1
		ORDER BY database_name, table_name, column_name
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list column classifications: %w", err)
	}
	defer rows.Close()

	var out []ColumnClassification
	for rows.Next() {
		var cc ColumnClassification
		var (
			piiType        sql.NullString
			encrypted      sql.NullBool
			encryptionKind sql.NullString
			minLen         sql.NullInt64
			maxLen         sql.NullInt64
			sampleCount    sql.NullInt64
			lastScannedAt  sql.NullTime
		)
		if err := rows.Scan(
			&cc.ID, &cc.TenantID, &cc.NodeID, &cc.DatabaseName, &cc.SchemaName,
			&cc.TableName, &cc.ColumnName,
			&piiType, &encrypted, &encryptionKind, &minLen, &maxLen,
			&sampleCount, &lastScannedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan column classification: %w", err)
		}
		if piiType.Valid {
			cc.PIIType = &piiType.String
		}
		if encrypted.Valid {
			cc.Encrypted = &encrypted.Bool
		}
		if encryptionKind.Valid {
			cc.EncryptionKind = &encryptionKind.String
		}
		if minLen.Valid {
			v := int(minLen.Int64)
			cc.MinValueLength = &v
		}
		if maxLen.Valid {
			v := int(maxLen.Int64)
			cc.MaxValueLength = &v
		}
		if sampleCount.Valid {
			v := int(sampleCount.Int64)
			cc.SampleCount = &v
		}
		if lastScannedAt.Valid {
			cc.LastScannedAt = &lastScannedAt.Time
		}
		out = append(out, cc)
	}
	return out, total, rows.Err()
}

// ListPIIFindings returns paginated PII findings for a tenant, optionally filtered by resolved state.
func (s *Store) ListPIIFindings(ctx context.Context, tenantID uuid.UUID, resolved *bool, limit, offset int) ([]PIIFinding, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}

	whereClause := "tenant_id = $1"
	args := []any{tenantID}
	if resolved != nil {
		if *resolved {
			whereClause += " AND resolved_at IS NOT NULL"
		} else {
			whereClause += " AND resolved_at IS NULL"
		}
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM pii_findings WHERE %s`, whereClause), args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count pii findings: %w", err)
	}

	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, column_classification_id, rule_id, severity, details,
			resolved_at, resolved_by, created_at
		FROM pii_findings
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list pii findings: %w", err)
	}
	defer rows.Close()

	var out []PIIFinding
	for rows.Next() {
		var f PIIFinding
		var (
			colClassID uuid.NullUUID
			ruleID     uuid.NullUUID
			details    sql.NullString
			resolvedAt sql.NullTime
			resolvedBy uuid.NullUUID
		)
		if err := rows.Scan(
			&f.ID, &f.TenantID, &colClassID, &ruleID, &f.Severity, &details,
			&resolvedAt, &resolvedBy, &f.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan pii finding: %w", err)
		}
		if colClassID.Valid {
			f.ColumnClassificationID = &colClassID.UUID
		}
		if ruleID.Valid {
			f.RuleID = &ruleID.UUID
		}
		if details.Valid {
			f.Details = &details.String
		}
		if resolvedAt.Valid {
			f.ResolvedAt = &resolvedAt.Time
		}
		if resolvedBy.Valid {
			f.ResolvedBy = &resolvedBy.UUID
		}
		out = append(out, f)
	}
	return out, total, rows.Err()
}

// ResolvePIIFinding marks a finding as resolved.
func (s *Store) ResolvePIIFinding(ctx context.Context, id, resolvedBy uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE pii_findings
		SET resolved_at = NOW(), resolved_by = $2
		WHERE id = $1 AND resolved_at IS NULL
	`, id, resolvedBy)
	return err
}

// CreatePIIFinding inserts a new PII finding and returns it.
func (s *Store) CreatePIIFinding(ctx context.Context, f *PIIFinding) (*PIIFinding, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if f.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	if f.Severity == "" {
		f.Severity = "medium"
	}

	var (
		colClassID uuid.NullUUID
		ruleID     uuid.NullUUID
		details    sql.NullString
	)
	if f.ColumnClassificationID != nil {
		colClassID = uuid.NullUUID{UUID: *f.ColumnClassificationID, Valid: true}
	}
	if f.RuleID != nil {
		ruleID = uuid.NullUUID{UUID: *f.RuleID, Valid: true}
	}
	if f.Details != nil {
		details = sql.NullString{String: *f.Details, Valid: true}
	}

	out := &PIIFinding{}
	var (
		outColClassID uuid.NullUUID
		outRuleID     uuid.NullUUID
		outDetails    sql.NullString
		outResolvedAt sql.NullTime
		outResolvedBy uuid.NullUUID
	)
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO pii_findings (tenant_id, column_classification_id, rule_id, severity, details)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, column_classification_id, rule_id, severity, details,
			resolved_at, resolved_by, created_at
	`, f.TenantID, colClassID, ruleID, f.Severity, details).
		Scan(&out.ID, &out.TenantID, &outColClassID, &outRuleID, &out.Severity, &outDetails,
			&outResolvedAt, &outResolvedBy, &out.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create pii finding: %w", err)
	}
	if outColClassID.Valid {
		out.ColumnClassificationID = &outColClassID.UUID
	}
	if outRuleID.Valid {
		out.RuleID = &outRuleID.UUID
	}
	if outDetails.Valid {
		out.Details = &outDetails.String
	}
	if outResolvedAt.Valid {
		out.ResolvedAt = &outResolvedAt.Time
	}
	if outResolvedBy.Valid {
		out.ResolvedBy = &outResolvedBy.UUID
	}
	return out, nil
}
