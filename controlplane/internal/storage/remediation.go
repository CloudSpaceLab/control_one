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

// remediationScriptColumns is the exact SELECT/RETURNING projection used by
// every query that produces a RemediationScript. Keeping one canonical list
// means adding a column only requires touching this file once — the scan
// helper below expects the exact same ordering.
const remediationScriptColumns = `id, rule_id, platform, script_type, script_content, checksum,
       signature, signature_algorithm,
       rollback_content, rollback_checksum, version,
       enabled, metadata, created_by, created_at, updated_at`

// RemediationScript represents a remediation script for a compliance rule.
type RemediationScript struct {
	ID                 uuid.UUID
	RuleID             string
	Platform           string
	ScriptType         string
	ScriptContent      string
	Checksum           sql.NullString
	Signature          sql.NullString
	SignatureAlgorithm sql.NullString
	RollbackContent    sql.NullString
	RollbackChecksum   sql.NullString
	Version            int
	Enabled            bool
	Metadata           map[string]any
	CreatedBy          *uuid.UUID
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// ScriptSignerFunc signs the canonical bytes for a remediation script and
// returns the signature and algorithm identifier. Supplied by the server
// layer so the storage package stays free of PKI dependencies; returning an
// empty signature (with nil error) means "unsigned, CP CA unavailable".
type ScriptSignerFunc func(content, platform string, version int) (signature, algorithm string, err error)

// CreateRemediationScriptParams defines input for creating a remediation script.
type CreateRemediationScriptParams struct {
	RuleID          string
	Platform        string
	ScriptType      string
	ScriptContent   string
	RollbackContent *string
	Version         *int
	Enabled         *bool
	Metadata        map[string]any
	CreatedBy       *uuid.UUID
	// Signer (optional) signs the canonical script bytes with the CP CA key
	// so the agent-side engine can verify before exec. Leaving this nil
	// writes the row unsigned — acceptable during dev but the engine refuses
	// unsigned scripts when RequireSignature is on.
	Signer ScriptSignerFunc
}

// UpdateRemediationScriptParams captures patchable fields on a remediation script.
type UpdateRemediationScriptParams struct {
	ScriptContent   *string
	RollbackContent *string
	Enabled         *bool
	Metadata        *map[string]any
	// Signer (optional) re-signs the script when ScriptContent changes so the
	// signature keeps pace with the content. When ScriptContent is nil the
	// signer is ignored.
	Signer ScriptSignerFunc
}

// GetRemediationScriptByID returns a remediation script by its ID.
func (s *Store) GetRemediationScriptByID(ctx context.Context, scriptID uuid.UUID) (*RemediationScript, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if scriptID == uuid.Nil {
		return nil, errors.New("script id is required")
	}

	query := fmt.Sprintf(`SELECT %s FROM remediation_scripts WHERE id = $1`, remediationScriptColumns)
	row := s.db.QueryRowContext(ctx, query, scriptID)
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

	query := fmt.Sprintf(`
		SELECT %s
		FROM remediation_scripts
		WHERE rule_id = $1 AND platform IN ($2, 'all') AND enabled = true
		ORDER BY CASE WHEN platform = $2 THEN 0 ELSE 1 END, version DESC
		LIMIT 1
	`, remediationScriptColumns)
	row := s.db.QueryRowContext(ctx, query, ruleID, platform)
	return scanRemediationScript(row)
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
		SELECT %s
		FROM remediation_scripts
		WHERE %s
		ORDER BY rule_id, platform, version DESC
	`, remediationScriptColumns, strings.Join(clauses, " AND "))

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
		script, err := scanRemediationScriptRow(rows)
		if err != nil {
			return nil, 0, err
		}
		scripts = append(scripts, *script)
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

	var rollbackContentArg, rollbackChecksumArg any
	if params.RollbackContent != nil && strings.TrimSpace(*params.RollbackContent) != "" {
		content := *params.RollbackContent
		rollbackContentArg = content
		rollbackChecksumArg = computeChecksum(content)
	}

	// Sign the canonical (content, platform, version) tuple with the CP CA
	// key. A missing signer (or one that returns an empty signature) writes
	// the row unsigned; the engine refuses to exec unsigned scripts when
	// RequireSignature is on.
	var signatureArg, signatureAlgArg any
	if params.Signer != nil {
		sig, alg, signErr := params.Signer(params.ScriptContent, params.Platform, version)
		if signErr != nil {
			return nil, fmt.Errorf("sign remediation script: %w", signErr)
		}
		if sig != "" {
			signatureArg = sig
			signatureAlgArg = alg
		}
	}

	id := uuid.New()
	now := s.clock()
	createdBy := sql.NullString{}
	if params.CreatedBy != nil {
		createdBy = sql.NullString{String: params.CreatedBy.String(), Valid: true}
	}

	query := fmt.Sprintf(`
		INSERT INTO remediation_scripts (
			id, rule_id, platform, script_type, script_content, checksum,
			signature, signature_algorithm,
			rollback_content, rollback_checksum, version,
			enabled, metadata, created_by, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING %s
	`, remediationScriptColumns)

	row := s.db.QueryRowContext(ctx, query,
		id, params.RuleID, params.Platform, params.ScriptType, params.ScriptContent, checksum,
		signatureArg, signatureAlgArg,
		rollbackContentArg, rollbackChecksumArg, version,
		enabled, metadataJSON, createdBy, now, now)

	return scanRemediationScript(row)
}

// UpdateRemediationScript updates a remediation script.
func (s *Store) UpdateRemediationScript(ctx context.Context, scriptID uuid.UUID, params UpdateRemediationScriptParams) (*RemediationScript, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if scriptID == uuid.Nil {
		return nil, errors.New("script id is required")
	}

	// When re-signing on content change we need the platform + version as they
	// exist right now; pull them eagerly so we can feed them to the signer.
	// The read also lets us short-circuit a no-op update back to the caller.
	existing, err := s.GetRemediationScriptByID(ctx, scriptID)
	if err != nil {
		return nil, fmt.Errorf("load script for update: %w", err)
	}
	if existing == nil {
		return nil, nil
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

		// Re-sign whenever content changes. We use the existing platform and
		// version because those are NOT mutable via this update path — they
		// were locked at create time.
		if params.Signer != nil {
			sig, alg, signErr := params.Signer(content, existing.Platform, existing.Version)
			if signErr != nil {
				return nil, fmt.Errorf("re-sign remediation script: %w", signErr)
			}
			if sig != "" {
				args = append(args, sig, alg)
			} else {
				args = append(args, nil, nil)
			}
			updates = append(updates, fmt.Sprintf("signature = $%d", argPos), fmt.Sprintf("signature_algorithm = $%d", argPos+1))
			argPos += 2
		} else {
			// No signer provided on a content change — explicitly wipe the old
			// signature so a stale sig never authenticates new content. The
			// engine will refuse to exec the unsigned row, which is the right
			// failure mode.
			args = append(args, nil, nil)
			updates = append(updates, fmt.Sprintf("signature = $%d", argPos), fmt.Sprintf("signature_algorithm = $%d", argPos+1))
			argPos += 2
		}
	}
	if params.RollbackContent != nil {
		content := strings.TrimSpace(*params.RollbackContent)
		if content == "" {
			// Explicit empty string clears the rollback script.
			args = append(args, nil, nil)
		} else {
			args = append(args, content, computeChecksum(content))
		}
		updates = append(updates, fmt.Sprintf("rollback_content = $%d", argPos), fmt.Sprintf("rollback_checksum = $%d", argPos+1))
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
		return existing, nil
	}

	updates = append(updates, fmt.Sprintf("updated_at = $%d", argPos))
	args = append(args, s.clock())

	query := fmt.Sprintf(`
		UPDATE remediation_scripts
		SET %s
		WHERE id = $1
		RETURNING %s
	`, strings.Join(updates, ", "), remediationScriptColumns)

	row := s.db.QueryRowContext(ctx, query, args...)
	return scanRemediationScript(row)
}

// BackfillRemediationScriptSignatures signs every row whose signature is
// currently NULL. Called at CP startup after the migrations run so any
// pre-Sprint-3 scripts get a valid CP CA signature before the engine looks at
// them. Returns how many rows were signed; a nil signer short-circuits with
// zero and no error (dev-mode servers run without a CA key).
func (s *Store) BackfillRemediationScriptSignatures(ctx context.Context, signer ScriptSignerFunc) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if signer == nil {
		return 0, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, platform, script_content, version
		FROM remediation_scripts
		WHERE signature IS NULL
	`)
	if err != nil {
		return 0, fmt.Errorf("query unsigned remediation scripts: %w", err)
	}
	type pending struct {
		id       uuid.UUID
		platform string
		content  string
		version  int
	}
	var pendingRows []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.platform, &p.content, &p.version); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan pending row: %w", err)
		}
		pendingRows = append(pendingRows, p)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close pending rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate pending rows: %w", err)
	}

	signed := 0
	now := s.clock()
	for _, p := range pendingRows {
		sig, alg, signErr := signer(p.content, p.platform, p.version)
		if signErr != nil {
			return signed, fmt.Errorf("sign script %s: %w", p.id, signErr)
		}
		if sig == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE remediation_scripts
			SET signature = $2, signature_algorithm = $3, updated_at = $4
			WHERE id = $1 AND signature IS NULL
		`, p.id, sig, alg, now); err != nil {
			return signed, fmt.Errorf("update script %s signature: %w", p.id, err)
		}
		signed++
	}
	return signed, nil
}

// scanRemediationScript scans a single-row result (sql.Row) whose columns
// match remediationScriptColumns.
func scanRemediationScript(row *sql.Row) (*RemediationScript, error) {
	return scanRemediationScriptRow(row)
}

// scanRemediationScriptRow shares scan logic between *sql.Row and *sql.Rows.
// The rowScanner interface is defined once in templates.go.
func scanRemediationScriptRow(row rowScanner) (*RemediationScript, error) {
	var script RemediationScript
	var checksum, signature, signatureAlg, rollbackContent, rollbackChecksum sql.NullString
	var metadataRaw []byte
	var createdBy sql.NullString

	if err := row.Scan(
		&script.ID,
		&script.RuleID,
		&script.Platform,
		&script.ScriptType,
		&script.ScriptContent,
		&checksum,
		&signature,
		&signatureAlg,
		&rollbackContent,
		&rollbackChecksum,
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
	if signature.Valid {
		script.Signature = signature
	}
	if signatureAlg.Valid {
		script.SignatureAlgorithm = signatureAlg
	}
	if rollbackContent.Valid {
		script.RollbackContent = rollbackContent
	}
	if rollbackChecksum.Valid {
		script.RollbackChecksum = rollbackChecksum
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
