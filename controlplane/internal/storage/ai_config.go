package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AIConfig holds the per-tenant LLM provider settings for the Ask AI
// surface. The api_key is plaintext for v1 — a follow-up moves it into
// the secrets infra. Code that reads AIConfig must NEVER expose APIKey
// over a JSON GET response.
type AIConfig struct {
	TenantID  uuid.UUID
	Provider  string
	Model     string
	BaseURL   string
	APIKey    string
	UpdatedAt time.Time
}

// GetAIConfig returns the tenant's AI config, or nil if no row exists.
func (s *Store) GetAIConfig(ctx context.Context, tenantID uuid.UUID) (*AIConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var c AIConfig
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, provider, model, base_url, api_key, updated_at
		FROM ai_config WHERE tenant_id = $1
	`, tenantID).Scan(&c.TenantID, &c.Provider, &c.Model, &c.BaseURL, &c.APIKey, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ai config: %w", err)
	}
	return &c, nil
}

// UpsertAIConfig inserts or updates the tenant's AI config. Empty APIKey
// preserves the existing key (no-op rotation). All other fields overwrite.
func (s *Store) UpsertAIConfig(ctx context.Context, c AIConfig) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if c.TenantID == uuid.Nil {
		return errors.New("tenant id required")
	}
	if c.Provider == "" {
		c.Provider = "anthropic"
	}
	if c.Model == "" {
		c.Model = "claude-sonnet-4-6"
	}
	now := time.Now().UTC()

	if c.APIKey == "" {
		// Preserve existing key when caller didn't send a fresh one.
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO ai_config (tenant_id, provider, model, base_url, api_key, updated_at)
			VALUES ($1, $2, $3, $4, '', $5)
			ON CONFLICT (tenant_id) DO UPDATE SET
				provider   = EXCLUDED.provider,
				model      = EXCLUDED.model,
				base_url   = EXCLUDED.base_url,
				updated_at = EXCLUDED.updated_at
		`, c.TenantID, c.Provider, c.Model, c.BaseURL, now)
		if err != nil {
			return fmt.Errorf("upsert ai config (preserve key): %w", err)
		}
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ai_config (tenant_id, provider, model, base_url, api_key, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id) DO UPDATE SET
			provider   = EXCLUDED.provider,
			model      = EXCLUDED.model,
			base_url   = EXCLUDED.base_url,
			api_key    = EXCLUDED.api_key,
			updated_at = EXCLUDED.updated_at
	`, c.TenantID, c.Provider, c.Model, c.BaseURL, c.APIKey, now)
	if err != nil {
		return fmt.Errorf("upsert ai config: %w", err)
	}
	return nil
}
