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

// RemediationScript represents a remediation script for a compliance rule.
type RemediationScript struct {
	ID            uuid.UUID
	RuleID        string
	Platform      string
	ScriptType    string
	ScriptContent string
	Checksum      sql.NullString
	Version       int
	Enabled       bool
	Metadata      map[string]any
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateRemediationScriptParams defines input for creating a remediation script.
type CreateRemediationScriptParams struct {
	RuleID        string
	Platform      string
	ScriptType    string
	ScriptContent string
	Version       *int
	Enabled       *bool
	Metadata      map[string]any
	CreatedBy     *uuid.UUID
}

// UpdateRemediationScriptParams captures patchable fields on a remediation script.
type UpdateRemediationScriptParams struct {
	ScriptContent *string
	Enabled       *bool
	Metadata      *map[string]any
}

// GetRemediationScriptByID returns a remediation script by its ID.
func (s *Store) GetRemediationScriptByID(ctx context.Context, scriptID uuid.UUID) (*RemediationScript, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if scriptID == uuid.Nil {
		return nil, errors.New("script id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, rule_id, platform, script_type, script_content, checksum, version,
		       enabled, metadata, created_by, created_at, updated_at
		FROM remediation_scripts
		WHERE id = $1
	`, scriptID)

	return scanRemediationScript(row)
}

// GetRemediationScript returns a remediation script for a rule and platform.
func (s *Store) GetRemediationScript(ctx context.Context, ruleID, platform string) (*RemediationScript, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if strings.TrimSpace(ruleID) == "" {
		return nil, errors.New("rule id is required")
	}
	if strings.TrimSpace(platform) == "" {
		platform = "all"
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, rule_id, platform, script_type, script_content, checksum, version,
		       enabled, metadata, created_by, created_at, updated_at
		FROM remediation_scripts
		WHERE rule_id = $1 AND platform IN ($2, 'all') AND enabled = true
		ORDER BY CASE WHEN platform = $2 THEN 0 ELSE 1 END, version DESC
		LIMIT 1
	`, ruleID, platform)

	var script RemediationScript
	var checksum sql.NullString
	var metadataRaw []byte
	var createdBy sql.NullString

	if err := row.Scan(
		&script.ID,
		&script.RuleID,
		&script.Platform,
		&script.ScriptType,
		&script.ScriptContent,
		&checksum,
		&script.Version,
		&script.Enabled,
		&metadataRaw,
		&createdBy,
		&script.CreatedAt,
		&script.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get remediation script: %w", err)
	}

	if checksum.Valid {
		script.Checksum = checksum
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &script.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if script.Metadata == nil {
		script.Metadata = make(map[string]any)
	}
	if createdBy.Valid {
		if id, err := uuid.Parse(createdBy.String); err == nil {
			script.CreatedBy = &id
		}
	}

	return &script, nil
}

// ListRemediationScripts returns remediation scripts with filtering.
func (s *Store) ListRemediationScripts(ctx context.Context, ruleID, platform string, limit, offset int) ([]RemediationScript, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if strings.TrimSpace(ruleID) != "" {
		args = append(args, strings.TrimSpace(ruleID))
		clauses = append(clauses, fmt.Sprintf("rule_id = $%d", len(args)))
	}
	if strings.TrimSpace(platform) != "" {
		args = append(args, strings.TrimSpace(platform))
		clauses = append(clauses, fmt.Sprintf("platform = $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM remediation_scripts WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count remediation scripts: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, rule_id, platform, script_type, script_content, checksum, version,
		       enabled, metadata, created_by, created_at, updated_at
		FROM remediation_scripts
		WHERE %s
		ORDER BY rule_id, platform, version DESC
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
		return nil, 0, fmt.Errorf("query remediation scripts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scripts []RemediationScript
	for rows.Next() {
		var script RemediationScript
		var checksum sql.NullString
		var metadataRaw []byte
		var createdBy sql.NullString

		if err := rows.Scan(
			&script.ID,
			&script.RuleID,
			&script.Platform,
			&script.ScriptType,
			&script.ScriptContent,
			&checksum,
			&script.Version,
			&script.Enabled,
			&metadataRaw,
			&createdBy,
			&script.CreatedAt,
			&script.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan remediation script: %w", err)
		}

		if checksum.Valid {
			script.Checksum = checksum
		}
		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &script.Metadata); err != nil {
				return nil, 0, fmt.Errorf("decode metadata: %w", err)
			}
		}
		if script.Metadata == nil {
			script.Metadata = make(map[string]any)
		}
		if createdBy.Valid {
			if id, err := uuid.Parse(createdBy.String); err == nil {
				script.CreatedBy = &id
			}
		}

		scripts = append(scripts, script)
	}

	return scripts, total, nil
}

// CreateRemediationScript creates a new remediation script.
func (s *Store) CreateRemediationScript(ctx context.Context, params CreateRemediationScriptParams) (*RemediationScript, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if strings.TrimSpace(params.RuleID) == "" {
		return nil, errors.New("rule id is required")
	}
	if strings.TrimSpace(params.Platform) == "" {
		params.Platform = "all"
	}
	if strings.TrimSpace(params.ScriptType) == "" {
		return nil, errors.New("script type is required")
	}
	if strings.TrimSpace(params.ScriptContent) == "" {
		return nil, errors.New("script content is required")
	}

	version := 1
	if params.Version != nil {
		version = *params.Version
	}
	enabled := true
	if params.Enabled != nil {
		enabled = *params.Enabled
	}

	checksum := computeChecksum(params.ScriptContent)
	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	id := uuid.New()
	now := s.clock()
	createdBy := sql.NullString{}
	if params.CreatedBy != nil {
		createdBy = sql.NullString{String: params.CreatedBy.String(), Valid: true}
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO remediation_scripts (
			id, rule_id, platform, script_type, script_content, checksum, version,
			enabled, metadata, created_by, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, rule_id, platform, script_type, script_content, checksum, version,
		          enabled, metadata, created_by, created_at, updated_at
	`, id, params.RuleID, params.Platform, params.ScriptType, params.ScriptContent, checksum, version,
		enabled, metadataJSON, createdBy, now, now)

	var script RemediationScript
	var checksumNull sql.NullString
	var metadataRaw []byte
	var createdByNull sql.NullString

	if err := row.Scan(
		&script.ID,
		&script.RuleID,
		&script.Platform,
		&script.ScriptType,
		&script.ScriptContent,
		&checksumNull,
		&script.Version,
		&script.Enabled,
		&metadataRaw,
		&createdByNull,
		&script.CreatedAt,
		&script.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create remediation script: %w", err)
	}

	if checksumNull.Valid {
		script.Checksum = checksumNull
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &script.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if script.Metadata == nil {
		script.Metadata = make(map[string]any)
	}
	if createdByNull.Valid {
		if id, err := uuid.Parse(createdByNull.String); err == nil {
			script.CreatedBy = &id
		}
	}

	return &script, nil
}

// UpdateRemediationScript updates a remediation script.
func (s *Store) UpdateRemediationScript(ctx context.Context, scriptID uuid.UUID, params UpdateRemediationScriptParams) (*RemediationScript, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if scriptID == uuid.Nil {
		return nil, errors.New("script id is required")
	}

	updates := []string{}
	args := []any{scriptID}
	argPos := 2

	if params.ScriptContent != nil {
		content := strings.TrimSpace(*params.ScriptContent)
		if content == "" {
			return nil, errors.New("script content cannot be empty")
		}
		checksum := computeChecksum(content)
		args = append(args, content, checksum)
		updates = append(updates, fmt.Sprintf("script_content = $%d", argPos), fmt.Sprintf("checksum = $%d", argPos+1))
		argPos += 2
	}
	if params.Enabled != nil {
		args = append(args, *params.Enabled)
		updates = append(updates, fmt.Sprintf("enabled = $%d", argPos))
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
		row := s.db.QueryRowContext(ctx, `
			SELECT id, rule_id, platform, script_type, script_content, checksum, version,
			       enabled, metadata, created_by, created_at, updated_at
			FROM remediation_scripts
			WHERE id = $1
		`, scriptID)
		return scanRemediationScript(row)
	}

	updates = append(updates, fmt.Sprintf("updated_at = $%d", argPos))
	args = append(args, s.clock())

	query := fmt.Sprintf(`
		UPDATE remediation_scripts
		SET %s
		WHERE id = $1
		RETURNING id, rule_id, platform, script_type, script_content, checksum, version,
		          enabled, metadata, created_by, created_at, updated_at
	`, strings.Join(updates, ", "))

	row := s.db.QueryRowContext(ctx, query, args...)
	return scanRemediationScript(row)
}

func scanRemediationScript(row *sql.Row) (*RemediationScript, error) {
	var script RemediationScript
	var checksum sql.NullString
	var metadataRaw []byte
	var createdBy sql.NullString

	if err := row.Scan(
		&script.ID,
		&script.RuleID,
		&script.Platform,
		&script.ScriptType,
		&script.ScriptContent,
		&checksum,
		&script.Version,
		&script.Enabled,
		&metadataRaw,
		&createdBy,
		&script.CreatedAt,
		&script.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan remediation script: %w", err)
	}

	if checksum.Valid {
		script.Checksum = checksum
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &script.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if script.Metadata == nil {
		script.Metadata = make(map[string]any)
	}
	if createdBy.Valid {
		if id, err := uuid.Parse(createdBy.String); err == nil {
			script.CreatedBy = &id
		}
	}

	return &script, nil
}

func computeChecksum(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}
