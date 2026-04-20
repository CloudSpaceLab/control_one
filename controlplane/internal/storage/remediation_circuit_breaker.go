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

// RemediationCircuitBreakerState captures the per (tenant, rule) circuit-breaker
// trip state. When a row exists with acked_at=NULL, the breaker is "open" and
// the dispatch path must short-circuit new remediations for that tuple.
type RemediationCircuitBreakerState struct {
	TenantID      uuid.UUID
	RuleID        string
	TrippedAt     time.Time
	TrippedReason string
	AckedAt       *time.Time
	AckedBy       *uuid.UUID
}

// RemediationFailRate is the return type of RemediationFailRate for the
// dispatch path's circuit-breaker decision.
type RemediationFailRate struct {
	Samples int
	Failed  int
	Pct     int // integer percent, rounded down.
}

// GetCircuitBreakerState returns the breaker row for (tenantID, ruleID) or nil
// if no row exists. Expired/acked breakers still return the row — the caller
// checks `AckedAt != nil` to decide.
func (s *Store) GetCircuitBreakerState(ctx context.Context, tenantID uuid.UUID, ruleID string) (*RemediationCircuitBreakerState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return nil, errors.New("rule id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, rule_id, tripped_at, tripped_reason, acked_at, acked_by
		FROM remediation_circuit_breaker_state
		WHERE tenant_id = $1 AND rule_id = $2
	`, tenantID, ruleID)

	var (
		state   RemediationCircuitBreakerState
		ackedAt sql.NullTime
		ackedBy sql.NullString
	)
	if err := row.Scan(
		&state.TenantID,
		&state.RuleID,
		&state.TrippedAt,
		&state.TrippedReason,
		&ackedAt,
		&ackedBy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get circuit breaker state: %w", err)
	}

	if ackedAt.Valid {
		t := ackedAt.Time.UTC()
		state.AckedAt = &t
	}
	if ackedBy.Valid {
		if parsed, err := uuid.Parse(ackedBy.String); err == nil {
			state.AckedBy = &parsed
		}
	}
	return &state, nil
}

// TripCircuitBreaker sets the breaker tripped (clears any prior ack so the
// breaker re-opens on a fresh trip). Idempotent if the breaker was already
// open.
func (s *Store) TripCircuitBreaker(ctx context.Context, tenantID uuid.UUID, ruleID, reason string) (*RemediationCircuitBreakerState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return nil, errors.New("rule id is required")
	}

	now := s.clock().UTC()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "automatic trip"
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO remediation_circuit_breaker_state (tenant_id, rule_id, tripped_at, tripped_reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, rule_id) DO UPDATE SET
			tripped_at     = EXCLUDED.tripped_at,
			tripped_reason = EXCLUDED.tripped_reason,
			acked_at       = NULL,
			acked_by       = NULL
		RETURNING tenant_id, rule_id, tripped_at, tripped_reason, acked_at, acked_by
	`, tenantID, ruleID, now, reason)

	var (
		state   RemediationCircuitBreakerState
		ackedAt sql.NullTime
		ackedBy sql.NullString
	)
	if err := row.Scan(
		&state.TenantID,
		&state.RuleID,
		&state.TrippedAt,
		&state.TrippedReason,
		&ackedAt,
		&ackedBy,
	); err != nil {
		return nil, fmt.Errorf("trip circuit breaker: %w", err)
	}
	if ackedAt.Valid {
		t := ackedAt.Time.UTC()
		state.AckedAt = &t
	}
	if ackedBy.Valid {
		if parsed, err := uuid.Parse(ackedBy.String); err == nil {
			state.AckedBy = &parsed
		}
	}
	return &state, nil
}

// AckCircuitBreaker marks a breaker acknowledged so the dispatch path resumes.
// Returns sql.ErrNoRows when no matching row exists.
func (s *Store) AckCircuitBreaker(ctx context.Context, tenantID uuid.UUID, ruleID string, ackerID uuid.UUID) (*RemediationCircuitBreakerState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return nil, errors.New("rule id is required")
	}

	var ackerArg any
	if ackerID != uuid.Nil {
		ackerArg = ackerID
	}

	now := s.clock().UTC()

	row := s.db.QueryRowContext(ctx, `
		UPDATE remediation_circuit_breaker_state
		SET acked_at = $3, acked_by = $4
		WHERE tenant_id = $1 AND rule_id = $2
		RETURNING tenant_id, rule_id, tripped_at, tripped_reason, acked_at, acked_by
	`, tenantID, ruleID, now, ackerArg)

	var (
		state   RemediationCircuitBreakerState
		ackedAt sql.NullTime
		ackedBy sql.NullString
	)
	if err := row.Scan(
		&state.TenantID,
		&state.RuleID,
		&state.TrippedAt,
		&state.TrippedReason,
		&ackedAt,
		&ackedBy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("ack circuit breaker: %w", err)
	}
	if ackedAt.Valid {
		t := ackedAt.Time.UTC()
		state.AckedAt = &t
	}
	if ackedBy.Valid {
		if parsed, err := uuid.Parse(ackedBy.String); err == nil {
			state.AckedBy = &parsed
		}
	}
	return &state, nil
}

// RemediationFailRate returns the recent fail rate for a tenant+rule's
// remediation.execute jobs. The calculation uses the existing jobs table and
// counts succeeded/failed finished jobs within `window` ending at now. When
// fewer than any samples are available the caller is expected to short-circuit.
func (s *Store) RemediationFailRate(ctx context.Context, tenantID uuid.UUID, ruleID string, window time.Duration) (*RemediationFailRate, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return nil, errors.New("rule id is required")
	}
	if window <= 0 {
		return nil, errors.New("window must be positive")
	}

	since := s.clock().UTC().Add(-window)

	// The rule_id is embedded in the job payload. We match it with a JSONB
	// filter — the jobs table (mig 0002) stores payload as JSONB so the
	// comparison is cheap against the existing jobs_tenant_id_idx.
	row := s.db.QueryRowContext(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status IN ('succeeded', 'failed', 'cancelled')) AS samples,
		  COUNT(*) FILTER (WHERE status = 'failed')                                AS failed
		FROM jobs
		WHERE tenant_id = $1
		  AND type = 'remediation.execute'
		  AND updated_at >= $2
		  AND payload ->> 'rule_id' = $3
	`, tenantID, since, ruleID)

	var rate RemediationFailRate
	if err := row.Scan(&rate.Samples, &rate.Failed); err != nil {
		return nil, fmt.Errorf("query remediation fail rate: %w", err)
	}
	if rate.Samples > 0 {
		rate.Pct = (rate.Failed * 100) / rate.Samples
	}
	return &rate, nil
}
