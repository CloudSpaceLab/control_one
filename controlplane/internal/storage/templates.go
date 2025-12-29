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

// ProvisioningTemplate represents a stored provisioning template definition.
type ProvisioningTemplate struct {
	ID                uuid.UUID
	Name              string
	Provider          string
	Description       sql.NullString
	Labels            map[string]string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        sql.NullTime
	PromotedVersionID *uuid.UUID
}

// ProvisioningTemplateVersion represents a versioned template payload.
type ProvisioningTemplateVersion struct {
	ID             uuid.UUID
	TemplateID     uuid.UUID
	Version        int
	Checksum       sql.NullString
	Body           string
	MetadataSchema json.RawMessage
	RolloutNotes   sql.NullString
	CreatedBy      *uuid.UUID
	CreatedAt      time.Time
	PromotedAt     sql.NullTime
}

// ProvisioningTemplateFilter captures filters for listing templates.
type ProvisioningTemplateFilter struct {
	Provider        string
	NamePrefix      string
	IncludeArchived bool
}

// CreateTemplateVersionParams defines input for creating a template version.
type CreateTemplateVersionParams struct {
	TemplateID     uuid.UUID
	Body           string
	Checksum       *string
	MetadataSchema json.RawMessage
	RolloutNotes   *string
	CreatedBy      *uuid.UUID
}

// ListProvisioningTemplates returns templates matching the provided filter.
func (s *Store) ListProvisioningTemplates(ctx context.Context, filter ProvisioningTemplateFilter, limit, offset int) ([]ProvisioningTemplate, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if strings.TrimSpace(filter.Provider) != "" {
		args = append(args, strings.TrimSpace(filter.Provider))
		clauses = append(clauses, fmt.Sprintf("provider = $%d", len(args)))
	}
	if strings.TrimSpace(filter.NamePrefix) != "" {
		args = append(args, strings.TrimSpace(filter.NamePrefix)+"%")
		clauses = append(clauses, fmt.Sprintf("name ILIKE $%d", len(args)))
	}
	if !filter.IncludeArchived {
		clauses = append(clauses, "archived_at IS NULL")
	}

	query := fmt.Sprintf(`
        SELECT id, name, provider, description, labels, created_at, updated_at, archived_at, promoted_version_id
        FROM provisioning_templates
        WHERE %s
        ORDER BY created_at DESC
    `, strings.Join(clauses, " AND "))

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM provisioning_templates WHERE %s`, strings.Join(clauses, " AND "))

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
		return nil, 0, fmt.Errorf("count provisioning templates: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query provisioning templates: %w", err)
	}
	defer rows.Close()

	var templates []ProvisioningTemplate
	for rows.Next() {
		var tpl ProvisioningTemplate
		var labelsRaw []byte
		var promoted sql.NullString
		if err := rows.Scan(
			&tpl.ID,
			&tpl.Name,
			&tpl.Provider,
			&tpl.Description,
			&labelsRaw,
			&tpl.CreatedAt,
			&tpl.UpdatedAt,
			&tpl.ArchivedAt,
			&promoted,
		); err != nil {
			return nil, 0, fmt.Errorf("scan provisioning template: %w", err)
		}
		labels, err := decodeStringMap(labelsRaw)
		if err != nil {
			return nil, 0, err
		}
		tpl.Labels = labels
		if promoted.Valid {
			if id, err := uuid.Parse(promoted.String); err == nil {
				tpl.PromotedVersionID = &id
			}
		}
		templates = append(templates, tpl)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate provisioning templates: %w", err)
	}

	return templates, total, nil
}

// CreateProvisioningTemplate inserts a new provisioning template shell record.
func (s *Store) CreateProvisioningTemplate(ctx context.Context, tpl *ProvisioningTemplate) (*ProvisioningTemplate, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tpl == nil {
		return nil, errors.New("template cannot be nil")
	}
	tpl.Name = strings.TrimSpace(tpl.Name)
	if tpl.Name == "" {
		return nil, errors.New("template name is required")
	}
	if tpl.ID == uuid.Nil {
		tpl.ID = uuid.New()
	}
	now := s.clock()
	tpl.CreatedAt = now
	tpl.UpdatedAt = now

	labelsRaw, err := encodeStringMap(tpl.Labels)
	if err != nil {
		return nil, err
	}

	_, err = s.db.ExecContext(ctx, `
        INSERT INTO provisioning_templates (id, name, provider, description, labels, created_at, updated_at, archived_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `, tpl.ID, tpl.Name, strings.TrimSpace(tpl.Provider), tpl.Description, labelsRaw, tpl.CreatedAt, tpl.UpdatedAt, tpl.ArchivedAt)
	if err != nil {
		return nil, fmt.Errorf("insert provisioning template: %w", err)
	}
	return tpl, nil
}

// GetProvisioningTemplate returns a template by ID.
func (s *Store) GetProvisioningTemplate(ctx context.Context, id uuid.UUID) (*ProvisioningTemplate, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("template id is required")
	}

	row := s.db.QueryRowContext(ctx, `
        SELECT id, name, provider, description, labels, created_at, updated_at, archived_at, promoted_version_id
        FROM provisioning_templates
        WHERE id = $1
    `, id)

	var tpl ProvisioningTemplate
	var labelsRaw []byte
	var promoted sql.NullString
	if err := row.Scan(&tpl.ID, &tpl.Name, &tpl.Provider, &tpl.Description, &labelsRaw, &tpl.CreatedAt, &tpl.UpdatedAt, &tpl.ArchivedAt, &promoted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get provisioning template: %w", err)
	}
	labels, err := decodeStringMap(labelsRaw)
	if err != nil {
		return nil, err
	}
	tpl.Labels = labels
	if promoted.Valid {
		if id, err := uuid.Parse(promoted.String); err == nil {
			tpl.PromotedVersionID = &id
		}
	}
	return &tpl, nil
}

// CreateProvisioningTemplateVersion creates a new version for the template.
func (s *Store) CreateProvisioningTemplateVersion(ctx context.Context, params CreateTemplateVersionParams) (*ProvisioningTemplateVersion, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TemplateID == uuid.Nil {
		return nil, errors.New("template id is required")
	}
	if strings.TrimSpace(params.Body) == "" {
		return nil, errors.New("template body is required")
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

	// lock template to ensure consistent numbering
	if err = s.ensureTemplateExistsForUpdate(ctx, tx, params.TemplateID); err != nil {
		return nil, err
	}

	var currentVersion sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT MAX(version) FROM provisioning_template_versions WHERE template_id = $1`, params.TemplateID).Scan(&currentVersion); err != nil {
		return nil, fmt.Errorf("select template version: %w", err)
	}
	nextVersion := int(currentVersion.Int64) + 1
	version := &ProvisioningTemplateVersion{
		ID:         uuid.New(),
		TemplateID: params.TemplateID,
		Version:    nextVersion,
		Body:       params.Body,
		CreatedAt:  s.clock(),
	}
	if params.Checksum != nil {
		version.Checksum = sql.NullString{String: strings.TrimSpace(*params.Checksum), Valid: strings.TrimSpace(*params.Checksum) != ""}
	}
	if len(params.MetadataSchema) > 0 {
		version.MetadataSchema = params.MetadataSchema
	}
	if params.RolloutNotes != nil {
		note := strings.TrimSpace(*params.RolloutNotes)
		if note != "" {
			version.RolloutNotes = sql.NullString{String: note, Valid: true}
		}
	}
	if params.CreatedBy != nil && *params.CreatedBy != uuid.Nil {
		version.CreatedBy = params.CreatedBy
	}

	_, err = tx.ExecContext(ctx, `
        INSERT INTO provisioning_template_versions (id, template_id, version, checksum, body, metadata_schema, rollout_notes, created_by, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `,
		version.ID,
		version.TemplateID,
		version.Version,
		version.Checksum,
		version.Body,
		nullJSON(version.MetadataSchema),
		version.RolloutNotes,
		nullableUUIDPtr(version.CreatedBy),
		version.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert template version: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit template version: %w", err)
	}
	return version, nil
}

// PromoteProvisioningTemplateVersion marks the specified version as the promoted one for the template.
func (s *Store) PromoteProvisioningTemplateVersion(ctx context.Context, templateID uuid.UUID, versionNumber int) (*ProvisioningTemplateVersion, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if templateID == uuid.Nil {
		return nil, errors.New("template id is required")
	}
	if versionNumber <= 0 {
		return nil, errors.New("version must be positive")
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

	var version ProvisioningTemplateVersion
	var checksum sql.NullString
	var notes sql.NullString
	var metadata []byte
	var createdBy sql.NullString
	var promoted sql.NullTime
	if err = tx.QueryRowContext(ctx, `
        SELECT id, template_id, version, checksum, body, metadata_schema, rollout_notes, created_by, created_at, promoted_at
        FROM provisioning_template_versions
        WHERE template_id = $1 AND version = $2
        FOR UPDATE
    `, templateID, versionNumber).Scan(
		&version.ID,
		&version.TemplateID,
		&version.Version,
		&checksum,
		&version.Body,
		&metadata,
		&notes,
		&createdBy,
		&version.CreatedAt,
		&promoted,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("template version not found")
		}
		return nil, fmt.Errorf("select template version: %w", err)
	}
	if checksum.Valid {
		version.Checksum = checksum
	}
	if len(metadata) > 0 {
		version.MetadataSchema = append([]byte(nil), metadata...)
	}
	if notes.Valid {
		version.RolloutNotes = notes
	}
	if createdBy.Valid {
		if id, parseErr := uuid.Parse(createdBy.String); parseErr == nil {
			version.CreatedBy = &id
		}
	}
	version.PromotedAt = promoted

	now := s.clock()
	if _, err = tx.ExecContext(ctx, `
        UPDATE provisioning_template_versions
        SET promoted_at = NULL
        WHERE template_id = $1
    `, templateID); err != nil {
		return nil, fmt.Errorf("clear prior promotions: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
        UPDATE provisioning_template_versions
        SET promoted_at = $1
        WHERE id = $2
    `, now, version.ID); err != nil {
		return nil, fmt.Errorf("promote template version: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
        UPDATE provisioning_templates
        SET promoted_version_id = $1, updated_at = $2
        WHERE id = $3
    `, version.ID, now, templateID); err != nil {
		return nil, fmt.Errorf("update template promotion pointer: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit promote version: %w", err)
	}

	version.PromotedAt = sql.NullTime{Time: now, Valid: true}
	return &version, nil
}

// GetProvisioningTemplateVersion fetches a version by template ID and version number.
func (s *Store) GetProvisioningTemplateVersion(ctx context.Context, templateID uuid.UUID, versionNumber int) (*ProvisioningTemplateVersion, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if templateID == uuid.Nil {
		return nil, errors.New("template id is required")
	}
	if versionNumber <= 0 {
		return nil, errors.New("version must be positive")
	}

	row := s.db.QueryRowContext(ctx, `
        SELECT id, template_id, version, checksum, body, metadata_schema, rollout_notes, created_by, created_at, promoted_at
        FROM provisioning_template_versions
        WHERE template_id = $1 AND version = $2
    `, templateID, versionNumber)

	return scanTemplateVersion(row)
}

// GetPromotedProvisioningTemplateVersion returns the currently promoted version for the template.
func (s *Store) GetPromotedProvisioningTemplateVersion(ctx context.Context, templateID uuid.UUID) (*ProvisioningTemplateVersion, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if templateID == uuid.Nil {
		return nil, errors.New("template id is required")
	}

	row := s.db.QueryRowContext(ctx, `
        SELECT v.id, v.template_id, v.version, v.checksum, v.body, v.metadata_schema, v.rollout_notes, v.created_by, v.created_at, v.promoted_at
        FROM provisioning_templates t
        JOIN provisioning_template_versions v ON t.promoted_version_id = v.id
        WHERE t.id = $1
    `, templateID)

	result, err := scanTemplateVersion(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}

func (s *Store) ensureTemplateExistsForUpdate(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
	row := tx.QueryRowContext(ctx, `SELECT id FROM provisioning_templates WHERE id = $1 FOR UPDATE`, id)
	var tmp uuid.UUID
	if err := row.Scan(&tmp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("template not found")
		}
		return fmt.Errorf("lock template: %w", err)
	}
	return nil
}

func scanTemplateVersion(row *sql.Row) (*ProvisioningTemplateVersion, error) {
	var version ProvisioningTemplateVersion
	var checksum sql.NullString
	var notes sql.NullString
	var metadata []byte
	var createdBy sql.NullString
	if err := row.Scan(
		&version.ID,
		&version.TemplateID,
		&version.Version,
		&checksum,
		&version.Body,
		&metadata,
		&notes,
		&createdBy,
		&version.CreatedAt,
		&version.PromotedAt,
	); err != nil {
		return nil, err
	}
	if checksum.Valid {
		version.Checksum = checksum
	}
	if len(metadata) > 0 {
		version.MetadataSchema = append([]byte(nil), metadata...)
	}
	if notes.Valid {
		version.RolloutNotes = notes
	}
	if createdBy.Valid {
		if id, err := uuid.Parse(createdBy.String); err == nil {
			version.CreatedBy = &id
		}
	}
	return &version, nil
}

func encodeStringMap(input map[string]string) ([]byte, error) {
	if len(input) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(input)
}

func decodeStringMap(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode string map: %w", err)
	}
	if out == nil {
		out = map[string]string{}
	}
	return out, nil
}

func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func nullableUUIDPtr(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	if *id == uuid.Nil {
		return nil
	}
	return *id
}
