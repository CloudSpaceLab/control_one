package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

const (
	PrivateAccessAccountStatusActive   = "active"
	PrivateAccessAccountStatusDisabled = "disabled"
	PrivateAccessAccountStatusError    = "error"

	PrivateAccessImportStatusQueued    = "queued"
	PrivateAccessImportStatusRunning   = "running"
	PrivateAccessImportStatusSucceeded = "succeeded"
	PrivateAccessImportStatusFailed    = "failed"

	defaultPrivateAccessImportIntervalSeconds = 3600
	minPrivateAccessImportIntervalSeconds     = 300
)

type PrivateAccessProviderAccount struct {
	ID                    uuid.UUID                  `json:"id"`
	TenantID              uuid.UUID                  `json:"tenant_id"`
	Provider              privateaccess.ProviderKind `json:"provider"`
	AccountID             string                     `json:"account_id"`
	DisplayName           string                     `json:"display_name"`
	EndpointURL           string                     `json:"endpoint_url"`
	CredentialID          uuid.NullUUID              `json:"credential_id"`
	Status                string                     `json:"status"`
	Config                map[string]any             `json:"config,omitempty"`
	ImportEnabled         bool                       `json:"import_enabled"`
	ImportIntervalSeconds int                        `json:"import_interval_seconds"`
	NextImportAt          sql.NullTime               `json:"next_import_at"`
	LastImportAt          sql.NullTime               `json:"last_import_at"`
	LastImportStatus      string                     `json:"last_import_status"`
	LastImportError       string                     `json:"last_import_error"`
	CreatedBySubject      string                     `json:"created_by_subject"`
	CreatedAt             time.Time                  `json:"created_at"`
	UpdatedAt             time.Time                  `json:"updated_at"`
}

type UpsertPrivateAccessProviderAccountParams struct {
	TenantID              uuid.UUID
	Provider              privateaccess.ProviderKind
	AccountID             string
	DisplayName           string
	EndpointURL           string
	CredentialID          *uuid.UUID
	Config                map[string]any
	ImportEnabled         bool
	ImportIntervalSeconds int
	NextImportAt          *time.Time
	CreatedBySubject      string
}

type PrivateAccessImportRun struct {
	ID                uuid.UUID                  `json:"id"`
	TenantID          uuid.UUID                  `json:"tenant_id"`
	ProviderAccountID uuid.UUID                  `json:"provider_account_id"`
	JobID             uuid.NullUUID              `json:"job_id"`
	Provider          privateaccess.ProviderKind `json:"provider"`
	AccountID         string                     `json:"account_id"`
	Status            string                     `json:"status"`
	Summary           map[string]any             `json:"summary,omitempty"`
	Error             string                     `json:"error,omitempty"`
	StartedAt         sql.NullTime               `json:"started_at"`
	FinishedAt        sql.NullTime               `json:"finished_at"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
}

type CreatePrivateAccessImportRunParams struct {
	TenantID          uuid.UUID
	ProviderAccountID uuid.UUID
	JobID             *uuid.UUID
	Provider          privateaccess.ProviderKind
	AccountID         string
	Status            string
	Summary           map[string]any
	Error             string
	StartedAt         *time.Time
	FinishedAt        *time.Time
}

type UpdatePrivateAccessImportRunParams struct {
	JobID      *uuid.UUID
	Status     string
	Summary    map[string]any
	Error      string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

func (s *Store) UpsertPrivateAccessProviderAccount(ctx context.Context, params UpsertPrivateAccessProviderAccountParams) (*PrivateAccessProviderAccount, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	normalized, err := normalizePrivateAccessProviderAccount(params)
	if err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(normalized.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal private access provider account config: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO private_access_provider_accounts (
			tenant_id, provider, account_id, display_name, endpoint_url, credential_id, status,
			config, import_enabled, import_interval_seconds, next_import_at, created_by_subject
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12)
		ON CONFLICT (tenant_id, provider, account_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			endpoint_url = EXCLUDED.endpoint_url,
			credential_id = EXCLUDED.credential_id,
			status = EXCLUDED.status,
			config = EXCLUDED.config,
			import_enabled = EXCLUDED.import_enabled,
			import_interval_seconds = EXCLUDED.import_interval_seconds,
			next_import_at = EXCLUDED.next_import_at,
			updated_at = NOW()
		RETURNING id, tenant_id, provider, account_id, display_name, endpoint_url, credential_id,
		          status, config, import_enabled, import_interval_seconds, next_import_at,
		          last_import_at, last_import_status, last_import_error, created_by_subject,
		          created_at, updated_at
	`, normalized.TenantID, normalized.Provider, normalized.AccountID, normalized.DisplayName,
		normalized.EndpointURL, nullableUUIDPtr(normalized.CredentialID), PrivateAccessAccountStatusActive,
		configJSON, normalized.ImportEnabled, normalized.ImportIntervalSeconds, nullTimePtr(normalized.NextImportAt),
		normalized.CreatedBySubject)
	return scanPrivateAccessProviderAccount(row)
}

func (s *Store) GetPrivateAccessProviderAccount(ctx context.Context, tenantID, id uuid.UUID) (*PrivateAccessProviderAccount, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || id == uuid.Nil {
		return nil, errors.New("tenant_id and id are required")
	}
	row := s.db.QueryRowContext(ctx, privateAccessProviderAccountSelectSQL()+` WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	record, err := scanPrivateAccessProviderAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) ListPrivateAccessProviderAccounts(ctx context.Context, tenantID uuid.UUID, provider, status string, limit, offset int) ([]PrivateAccessProviderAccount, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant_id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	clauses := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if provider = strings.ToLower(strings.TrimSpace(provider)); provider != "" {
		args = append(args, provider)
		clauses = append(clauses, fmt.Sprintf("provider = $%d", len(args)))
	}
	if status = strings.ToLower(strings.TrimSpace(status)); status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM private_access_provider_accounts WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count private access provider accounts: %w", err)
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.db.QueryContext(ctx, privateAccessProviderAccountSelectSQL()+`
		WHERE `+where+`
		ORDER BY updated_at DESC, provider ASC, account_id ASC
		LIMIT $`+strconvArg(len(queryArgs)-1)+` OFFSET $`+strconvArg(len(queryArgs)),
		queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list private access provider accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PrivateAccessProviderAccount
	for rows.Next() {
		record, err := scanPrivateAccessProviderAccount(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *record)
	}
	return out, total, rows.Err()
}

func (s *Store) ListDuePrivateAccessProviderAccounts(ctx context.Context, now time.Time, limit int) ([]PrivateAccessProviderAccount, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if now.IsZero() {
		now = s.clock().UTC()
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, privateAccessProviderAccountSelectSQL()+`
		WHERE import_enabled = true
		  AND status = 'active'
		  AND (next_import_at IS NULL OR next_import_at <= $1)
		ORDER BY COALESCE(next_import_at, created_at) ASC
		LIMIT $2
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list due private access provider accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PrivateAccessProviderAccount
	for rows.Next() {
		record, err := scanPrivateAccessProviderAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

func (s *Store) RecordPrivateAccessProviderImportState(ctx context.Context, tenantID, id uuid.UUID, status, message string, importedAt time.Time, nextImportAt *time.Time) (*PrivateAccessProviderAccount, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || id == uuid.Nil {
		return nil, errors.New("tenant_id and id are required")
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = PrivateAccessImportStatusQueued
	}
	if importedAt.IsZero() {
		importedAt = s.clock().UTC()
	}
	accountStatus := PrivateAccessAccountStatusActive
	if status == PrivateAccessImportStatusFailed {
		accountStatus = PrivateAccessAccountStatusError
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE private_access_provider_accounts
		SET last_import_at = $3,
		    last_import_status = $4,
		    last_import_error = $5,
		    next_import_at = $6,
		    status = $7,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, provider, account_id, display_name, endpoint_url, credential_id,
		          status, config, import_enabled, import_interval_seconds, next_import_at,
		          last_import_at, last_import_status, last_import_error, created_by_subject,
		          created_at, updated_at
	`, tenantID, id, importedAt.UTC(), status, strings.TrimSpace(message), nullTimePtr(nextImportAt), accountStatus)
	record, err := scanPrivateAccessProviderAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) CreatePrivateAccessImportRun(ctx context.Context, params CreatePrivateAccessImportRunParams) (*PrivateAccessImportRun, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	normalized, err := normalizePrivateAccessImportRun(params)
	if err != nil {
		return nil, err
	}
	summary, err := json.Marshal(normalized.Summary)
	if err != nil {
		return nil, fmt.Errorf("marshal private access import summary: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO private_access_import_runs (
			tenant_id, provider_account_id, job_id, provider, account_id, status, summary,
			error, started_at, finished_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)
		RETURNING id, tenant_id, provider_account_id, job_id, provider, account_id, status,
		          summary, error, started_at, finished_at, created_at, updated_at
	`, normalized.TenantID, normalized.ProviderAccountID, nullableUUIDPtr(normalized.JobID), normalized.Provider,
		normalized.AccountID, normalized.Status, summary, normalized.Error,
		nullTimePtr(normalized.StartedAt), nullTimePtr(normalized.FinishedAt))
	return scanPrivateAccessImportRun(row)
}

func (s *Store) UpdatePrivateAccessImportRun(ctx context.Context, id uuid.UUID, params UpdatePrivateAccessImportRunParams) (*PrivateAccessImportRun, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	sets := []string{"updated_at = NOW()"}
	args := []any{id}
	if params.JobID != nil {
		args = append(args, nullableUUID(*params.JobID))
		sets = append(sets, fmt.Sprintf("job_id = $%d", len(args)))
	}
	if strings.TrimSpace(params.Status) != "" {
		status, err := normalizePrivateAccessImportStatus(params.Status)
		if err != nil {
			return nil, err
		}
		args = append(args, status)
		sets = append(sets, fmt.Sprintf("status = $%d", len(args)))
	}
	if params.Summary != nil {
		summary, err := json.Marshal(clonePrivateAccessMap(params.Summary))
		if err != nil {
			return nil, fmt.Errorf("marshal private access import summary: %w", err)
		}
		args = append(args, summary)
		sets = append(sets, fmt.Sprintf("summary = $%d::jsonb", len(args)))
	}
	if strings.TrimSpace(params.Error) != "" || strings.TrimSpace(params.Status) == PrivateAccessImportStatusFailed {
		args = append(args, strings.TrimSpace(params.Error))
		sets = append(sets, fmt.Sprintf("error = $%d", len(args)))
	}
	if params.StartedAt != nil {
		args = append(args, params.StartedAt.UTC())
		sets = append(sets, fmt.Sprintf("started_at = $%d", len(args)))
	}
	if params.FinishedAt != nil {
		args = append(args, params.FinishedAt.UTC())
		sets = append(sets, fmt.Sprintf("finished_at = $%d", len(args)))
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE private_access_import_runs
		SET `+strings.Join(sets, ", ")+`
		WHERE id = $1
		RETURNING id, tenant_id, provider_account_id, job_id, provider, account_id, status,
		          summary, error, started_at, finished_at, created_at, updated_at
	`, args...)
	record, err := scanPrivateAccessImportRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) ListPrivateAccessImportRuns(ctx context.Context, tenantID uuid.UUID, providerAccountID uuid.UUID, limit, offset int) ([]PrivateAccessImportRun, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant_id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	clauses := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if providerAccountID != uuid.Nil {
		args = append(args, providerAccountID)
		clauses = append(clauses, fmt.Sprintf("provider_account_id = $%d", len(args)))
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM private_access_import_runs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count private access import runs: %w", err)
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, provider_account_id, job_id, provider, account_id, status,
		       summary, error, started_at, finished_at, created_at, updated_at
		FROM private_access_import_runs
		WHERE `+where+`
		ORDER BY created_at DESC
		LIMIT $`+strconvArg(len(queryArgs)-1)+` OFFSET $`+strconvArg(len(queryArgs)),
		queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list private access import runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PrivateAccessImportRun
	for rows.Next() {
		record, err := scanPrivateAccessImportRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *record)
	}
	return out, total, rows.Err()
}

func normalizePrivateAccessProviderAccount(p UpsertPrivateAccessProviderAccountParams) (UpsertPrivateAccessProviderAccountParams, error) {
	if p.TenantID == uuid.Nil {
		return p, errors.New("tenant_id is required")
	}
	p.Provider = privateaccess.ProviderKind(strings.ToLower(strings.TrimSpace(string(p.Provider))))
	if !privateaccess.ValidProvider(p.Provider) {
		return p, fmt.Errorf("unsupported private-access provider %q", p.Provider)
	}
	p.AccountID = strings.TrimSpace(p.AccountID)
	if p.AccountID == "" {
		p.AccountID = "default"
	}
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	if p.DisplayName == "" {
		p.DisplayName = string(p.Provider) + ":" + p.AccountID
	}
	p.EndpointURL = strings.TrimSpace(p.EndpointURL)
	if p.EndpointURL != "" {
		parsed, err := url.Parse(p.EndpointURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return p, fmt.Errorf("invalid endpoint_url %q", p.EndpointURL)
		}
	}
	if p.CredentialID != nil && *p.CredentialID == uuid.Nil {
		p.CredentialID = nil
	}
	config, err := sanitizePrivateAccessProviderConfig(p.Config)
	if err != nil {
		return p, err
	}
	p.Config = config
	if p.ImportIntervalSeconds <= 0 {
		p.ImportIntervalSeconds = defaultPrivateAccessImportIntervalSeconds
	}
	if p.ImportIntervalSeconds < minPrivateAccessImportIntervalSeconds {
		return p, fmt.Errorf("import_interval_seconds must be at least %d", minPrivateAccessImportIntervalSeconds)
	}
	if p.NextImportAt != nil {
		next := p.NextImportAt.UTC()
		p.NextImportAt = &next
	}
	p.CreatedBySubject = strings.TrimSpace(p.CreatedBySubject)
	return p, nil
}

func normalizePrivateAccessImportRun(p CreatePrivateAccessImportRunParams) (CreatePrivateAccessImportRunParams, error) {
	if p.TenantID == uuid.Nil || p.ProviderAccountID == uuid.Nil {
		return p, errors.New("tenant_id and provider_account_id are required")
	}
	p.Provider = privateaccess.ProviderKind(strings.ToLower(strings.TrimSpace(string(p.Provider))))
	if !privateaccess.ValidProvider(p.Provider) {
		return p, fmt.Errorf("unsupported private-access provider %q", p.Provider)
	}
	p.AccountID = strings.TrimSpace(p.AccountID)
	if p.AccountID == "" {
		p.AccountID = "default"
	}
	status, err := normalizePrivateAccessImportStatus(p.Status)
	if err != nil {
		return p, err
	}
	p.Status = status
	p.Summary = clonePrivateAccessMap(p.Summary)
	p.Error = strings.TrimSpace(p.Error)
	if p.StartedAt != nil {
		start := p.StartedAt.UTC()
		p.StartedAt = &start
	}
	if p.FinishedAt != nil {
		finish := p.FinishedAt.UTC()
		p.FinishedAt = &finish
	}
	return p, nil
}

func normalizePrivateAccessImportStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", PrivateAccessImportStatusQueued:
		return PrivateAccessImportStatusQueued, nil
	case PrivateAccessImportStatusRunning:
		return PrivateAccessImportStatusRunning, nil
	case PrivateAccessImportStatusSucceeded:
		return PrivateAccessImportStatusSucceeded, nil
	case PrivateAccessImportStatusFailed:
		return PrivateAccessImportStatusFailed, nil
	default:
		return "", fmt.Errorf("unsupported private-access import status %q", status)
	}
}

func sanitizePrivateAccessProviderConfig(in map[string]any) (map[string]any, error) {
	out := clonePrivateAccessMap(in)
	for key, value := range out {
		lower := strings.ToLower(strings.TrimSpace(key))
		if privateAccessConfigKeyLooksSecret(lower) && !strings.HasSuffix(lower, "_ref") {
			return nil, fmt.Errorf("private access provider config must use secret references, not raw %s", key)
		}
		if nested, ok := value.(map[string]any); ok {
			sanitized, err := sanitizePrivateAccessProviderConfig(nested)
			if err != nil {
				return nil, err
			}
			out[key] = sanitized
		}
	}
	return out, nil
}

func privateAccessConfigKeyLooksSecret(key string) bool {
	for _, marker := range []string{"token", "secret", "password", "private_key", "api_key", "bearer"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func scanPrivateAccessProviderAccount(row rowScanner) (*PrivateAccessProviderAccount, error) {
	var out PrivateAccessProviderAccount
	var provider string
	var rawConfig []byte
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&provider,
		&out.AccountID,
		&out.DisplayName,
		&out.EndpointURL,
		&out.CredentialID,
		&out.Status,
		&rawConfig,
		&out.ImportEnabled,
		&out.ImportIntervalSeconds,
		&out.NextImportAt,
		&out.LastImportAt,
		&out.LastImportStatus,
		&out.LastImportError,
		&out.CreatedBySubject,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan private access provider account: %w", err)
	}
	out.Provider = privateaccess.ProviderKind(provider)
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &out.Config); err != nil {
			return nil, fmt.Errorf("decode private access provider account config: %w", err)
		}
	}
	if out.Config == nil {
		out.Config = map[string]any{}
	}
	return &out, nil
}

func scanPrivateAccessImportRun(row rowScanner) (*PrivateAccessImportRun, error) {
	var out PrivateAccessImportRun
	var provider string
	var rawSummary []byte
	if err := row.Scan(
		&out.ID,
		&out.TenantID,
		&out.ProviderAccountID,
		&out.JobID,
		&provider,
		&out.AccountID,
		&out.Status,
		&rawSummary,
		&out.Error,
		&out.StartedAt,
		&out.FinishedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan private access import run: %w", err)
	}
	out.Provider = privateaccess.ProviderKind(provider)
	if len(rawSummary) > 0 {
		if err := json.Unmarshal(rawSummary, &out.Summary); err != nil {
			return nil, fmt.Errorf("decode private access import run summary: %w", err)
		}
	}
	if out.Summary == nil {
		out.Summary = map[string]any{}
	}
	return &out, nil
}

func privateAccessProviderAccountSelectSQL() string {
	return `SELECT id, tenant_id, provider, account_id, display_name, endpoint_url, credential_id,
	              status, config, import_enabled, import_interval_seconds, next_import_at,
	              last_import_at, last_import_status, last_import_error, created_by_subject,
	              created_at, updated_at
	       FROM private_access_provider_accounts`
}

func clonePrivateAccessMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = clonePrivateAccessMap(typed)
		default:
			out[key] = typed
		}
	}
	return out
}

func strconvArg(i int) string {
	return fmt.Sprintf("%d", i)
}
