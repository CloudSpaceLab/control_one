package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

// NetworkOnboardingPolicy stores the administrator-managed destination allowlist.
type NetworkOnboardingPolicy struct {
	AllowedCIDRs []string
	UpdatedAt    time.Time
}

func (s *Store) GetNetworkOnboardingPolicy(ctx context.Context) (*NetworkOnboardingPolicy, error) {
	var policy NetworkOnboardingPolicy
	err := s.db.QueryRowContext(ctx, `
		SELECT allowed_cidrs, updated_at
		FROM network_onboarding_settings
		WHERE setting_key = 'allowed_cidrs'`).Scan(pq.Array(&policy.AllowedCIDRs), &policy.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if policy.AllowedCIDRs == nil {
		policy.AllowedCIDRs = []string{}
	}
	return &policy, nil
}

func (s *Store) UpsertNetworkOnboardingPolicy(ctx context.Context, cidrs []string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO network_onboarding_settings(setting_key, allowed_cidrs)
		VALUES ('allowed_cidrs', $1)
		ON CONFLICT (setting_key) DO UPDATE
		SET allowed_cidrs = EXCLUDED.allowed_cidrs, updated_at = NOW()`, pq.Array(cidrs))
	return err
}
