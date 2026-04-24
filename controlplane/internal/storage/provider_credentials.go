package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProviderCredential holds provider-specific authentication material for a
// tenant. The `ConfigEncrypted` column is an AES-GCM sealed JSON blob; storage
// never unseals it — that is the server layer's responsibility.
type ProviderCredential struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Provider        string
	Name            string
	ConfigEncrypted []byte
	Nonce           []byte
	CreatedAt       time.Time
	UpdatedAt       time.Time
	RotatedAt       sql.NullTime
}

// CreateProviderCredentialParams is the input for inserting a credential.
type CreateProviderCredentialParams struct {
	TenantID        uuid.UUID
	Provider        string
	Name            string
	ConfigEncrypted []byte
	Nonce           []byte
}

// UpdateProviderCredentialParams captures rotate-style updates — only the
// ciphertext + nonce rotate, identifiers are immutable.
type UpdateProviderCredentialParams struct {
	ConfigEncrypted []byte
	Nonce           []byte
}

// CreateProviderCredential inserts a new credential row.
func (s *Store) CreateProviderCredential(ctx context.Context, params CreateProviderCredentialParams) (*ProviderCredential, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant_id is required")
	}
	provider := strings.TrimSpace(params.Provider)
	if provider == "" {
		return nil, errors.New("provider is required")
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if len(params.ConfigEncrypted) == 0 {
		return nil, errors.New("config_encrypted is required")
	}

	id := uuid.New()
	now := s.clock()

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO provider_credentials (id, tenant_id, provider, name, config_encrypted, nonce, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING id, tenant_id, provider, name, config_encrypted, nonce, created_at, updated_at, rotated_at
	`, id, params.TenantID, provider, name, params.ConfigEncrypted, params.Nonce, now)

	return scanProviderCredentialRow(row)
}

// UpdateProviderCredential rotates the sealed config for an existing credential
// and marks rotated_at. Identifiers (tenant, provider, name) are not patched.
func (s *Store) UpdateProviderCredential(ctx context.Context, id uuid.UUID, params UpdateProviderCredentialParams) (*ProviderCredential, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	if len(params.ConfigEncrypted) == 0 {
		return nil, errors.New("config_encrypted is required")
	}
	now := s.clock()

	row := s.db.QueryRowContext(ctx, `
		UPDATE provider_credentials
		SET config_encrypted = $2, nonce = $3, updated_at = $4, rotated_at = $4
		WHERE id = $1
		RETURNING id, tenant_id, provider, name, config_encrypted, nonce, created_at, updated_at, rotated_at
	`, id, params.ConfigEncrypted, params.Nonce, now)

	return scanProviderCredentialRow(row)
}

// GetProviderCredential returns the credential row for the given id.
func (s *Store) GetProviderCredential(ctx context.Context, id uuid.UUID) (*ProviderCredential, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, provider, name, config_encrypted, nonce, created_at, updated_at, rotated_at
		FROM provider_credentials
		WHERE id = $1
	`, id)
	return scanProviderCredentialRow(row)
}

// ListProviderCredentials returns credentials filtered by tenant/provider.
func (s *Store) ListProviderCredentials(ctx context.Context, tenantID uuid.UUID, provider string, limit, offset int) ([]ProviderCredential, int, error) {
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
	if p := strings.TrimSpace(provider); p != "" {
		args = append(args, p)
		clauses = append(clauses, fmt.Sprintf("provider = $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM provider_credentials WHERE %s`, strings.Join(clauses, " AND "))
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count provider credentials: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, provider, name, config_encrypted, nonce, created_at, updated_at, rotated_at
		FROM provider_credentials
		WHERE %s
		ORDER BY created_at DESC
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
		return nil, 0, fmt.Errorf("query provider credentials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var creds []ProviderCredential
	for rows.Next() {
		cred, err := scanProviderCredentialRow(rows)
		if err != nil {
			return nil, 0, err
		}
		creds = append(creds, *cred)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate provider credentials: %w", err)
	}
	return creds, total, nil
}

// DeleteProviderCredential removes a credential. Hypervisor hosts that
// reference it have their credential_id set NULL via ON DELETE SET NULL.
func (s *Store) DeleteProviderCredential(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("id is required")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM provider_credentials WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete provider credential: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete provider credential rows affected: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanProviderCredentialRow(row rowScanner) (*ProviderCredential, error) {
	var cred ProviderCredential
	if err := row.Scan(
		&cred.ID,
		&cred.TenantID,
		&cred.Provider,
		&cred.Name,
		&cred.ConfigEncrypted,
		&cred.Nonce,
		&cred.CreatedAt,
		&cred.UpdatedAt,
		&cred.RotatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan provider credential: %w", err)
	}
	return &cred, nil
}
