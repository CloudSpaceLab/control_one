package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AgentRolloutState is one row in agent_rollout_state — per-tenant control
// over staged self-update rollout. Empty / zero state means "no rollout
// configured": rollout_pct=0, target_release_seq=0, paused=false. Agents
// reading that row will not self-update (rollout_pct=0 fails the bucket
// gate for every node).
type AgentRolloutState struct {
	TenantID         uuid.UUID
	TargetReleaseSeq int
	TargetVersion    string
	RolloutPct       int
	Paused           bool
	UpdatedBy        *uuid.UUID
	UpdatedAt        time.Time
}

// AgentRolloutUpdate is the operator-set portion of the row. tenant_id is
// path-scoped on the endpoint side, not in the body.
type AgentRolloutUpdate struct {
	TargetReleaseSeq int
	TargetVersion    string
	RolloutPct       int
	Paused           bool
	UpdatedBy        *uuid.UUID
}

// GetAgentRolloutState returns the row for the tenant, or nil when no row
// exists. nil + nil error is "no rollout configured" — a clean state.
func (s *Store) GetAgentRolloutState(ctx context.Context, tenantID uuid.UUID) (*AgentRolloutState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, target_release_seq, target_version, rollout_pct, paused, updated_by, updated_at
		FROM agent_rollout_state
		WHERE tenant_id = $1
	`, tenantID)
	var st AgentRolloutState
	if err := row.Scan(
		&st.TenantID, &st.TargetReleaseSeq, &st.TargetVersion,
		&st.RolloutPct, &st.Paused, &st.UpdatedBy, &st.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get agent rollout state: %w", err)
	}
	return &st, nil
}

// UpsertAgentRolloutState writes the operator-set portion of the row,
// creating it if absent. updated_at is bumped to NOW().
func (s *Store) UpsertAgentRolloutState(ctx context.Context, tenantID uuid.UUID, in AgentRolloutUpdate) (*AgentRolloutState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if in.RolloutPct < 0 || in.RolloutPct > 100 {
		return nil, errors.New("rollout_pct must be 0..100")
	}
	if in.TargetReleaseSeq < 0 {
		return nil, errors.New("target_release_seq must be >= 0")
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO agent_rollout_state
			(tenant_id, target_release_seq, target_version, rollout_pct, paused, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET
			target_release_seq = EXCLUDED.target_release_seq,
			target_version     = EXCLUDED.target_version,
			rollout_pct        = EXCLUDED.rollout_pct,
			paused             = EXCLUDED.paused,
			updated_by         = EXCLUDED.updated_by,
			updated_at         = NOW()
		RETURNING tenant_id, target_release_seq, target_version, rollout_pct, paused, updated_by, updated_at
	`,
		tenantID, in.TargetReleaseSeq, in.TargetVersion, in.RolloutPct, in.Paused, in.UpdatedBy,
	)
	var st AgentRolloutState
	if err := row.Scan(
		&st.TenantID, &st.TargetReleaseSeq, &st.TargetVersion,
		&st.RolloutPct, &st.Paused, &st.UpdatedBy, &st.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("upsert agent rollout state: %w", err)
	}
	return &st, nil
}
