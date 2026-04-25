package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type CorrelationRule struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Name          string
	Description   sql.NullString
	EventTypes    []string
	WindowSeconds int
	Threshold     int
	Dimension     string
	Severity      string
	Enabled       bool
	YAMLSpec      sql.NullString
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateCorrelationRuleParams struct {
	TenantID      uuid.UUID
	Name          string
	Description   string
	EventTypes    []string
	WindowSeconds int
	Threshold     int
	Dimension     string
	Severity      string
	Enabled       bool
	YAMLSpec      string
}

func (s *Store) CreateCorrelationRule(ctx context.Context, p CreateCorrelationRuleParams) (*CorrelationRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.Name) == "" {
		return nil, errors.New("tenant_id and name required")
	}
	if p.WindowSeconds <= 0 {
		p.WindowSeconds = 300
	}
	if p.Threshold <= 0 {
		p.Threshold = 1
	}
	if p.Dimension == "" {
		p.Dimension = "node_id"
	}
	if p.Severity == "" {
		p.Severity = "high"
	}
	var desc, spec any
	if p.Description != "" {
		desc = p.Description
	}
	if p.YAMLSpec != "" {
		spec = p.YAMLSpec
	}
	id := uuid.New()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO correlation_rules (id, tenant_id, name, description, event_types, window_seconds, threshold, dimension, severity, enabled, yaml_spec, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
	`, id, p.TenantID, p.Name, desc, pq.Array(p.EventTypes), p.WindowSeconds, p.Threshold, p.Dimension, p.Severity, p.Enabled, spec, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert correlation rule: %w", err)
	}
	return s.GetCorrelationRule(ctx, id)
}

func (s *Store) GetCorrelationRule(ctx context.Context, id uuid.UUID) (*CorrelationRule, error) {
	row := s.db.QueryRowContext(ctx, correlationRuleSelectSQL+` WHERE id = $1`, id)
	return scanCorrelationRule(row)
}

func (s *Store) ListCorrelationRules(ctx context.Context, tenantID uuid.UUID) ([]CorrelationRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, correlationRuleSelectSQL+` WHERE tenant_id = $1 AND enabled = true ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CorrelationRule
	for rows.Next() {
		r, err := scanCorrelationRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteCorrelationRule(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM correlation_rules WHERE id = $1`, id)
	return err
}

const correlationRuleSelectSQL = `
	SELECT id, tenant_id, name, description, event_types, window_seconds, threshold, dimension, severity, enabled, yaml_spec, created_at, updated_at
	FROM correlation_rules
`

func scanCorrelationRule(sc scanner) (*CorrelationRule, error) {
	var r CorrelationRule
	if err := sc.Scan(&r.ID, &r.TenantID, &r.Name, &r.Description, pq.Array(&r.EventTypes), &r.WindowSeconds, &r.Threshold, &r.Dimension, &r.Severity, &r.Enabled, &r.YAMLSpec, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ---------- behavioral_baselines ----------

type BehavioralBaseline struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	NodeID      uuid.NullUUID
	SignalType  string
	Dimension   string
	Baseline    map[string]any
	WindowDays  int
	ComputedAt  time.Time
}

// UpsertBehavioralBaseline inserts or updates a baseline row for
// (tenant, node, signal_type, dimension).
func (s *Store) UpsertBehavioralBaseline(ctx context.Context, tenantID uuid.UUID, nodeID *uuid.UUID, signalType, dimension string, baseline map[string]any, windowDays int) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil || signalType == "" || dimension == "" {
		return errors.New("tenant_id, signal_type, dimension required")
	}
	blob, err := marshalJSONBMap(baseline)
	if err != nil {
		return err
	}
	var nodeArg any
	if nodeID != nil && *nodeID != uuid.Nil {
		nodeArg = *nodeID
	}
	if windowDays <= 0 {
		windowDays = 30
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO behavioral_baselines (id, tenant_id, node_id, signal_type, dimension, baseline, window_days, computed_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (tenant_id, node_id, signal_type, dimension) DO UPDATE
		  SET baseline = EXCLUDED.baseline, window_days = EXCLUDED.window_days, computed_at = EXCLUDED.computed_at
	`, tenantID, nodeArg, signalType, dimension, blob, windowDays)
	return err
}

func (s *Store) ListBehavioralBaselines(ctx context.Context, tenantID uuid.UUID, nodeID uuid.UUID) ([]BehavioralBaseline, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if nodeID != uuid.Nil {
		where = append(where, "node_id = $2")
		args = append(args, nodeID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, node_id, signal_type, dimension, baseline, window_days, computed_at
		FROM behavioral_baselines WHERE `+strings.Join(where, " AND ")+` ORDER BY computed_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []BehavioralBaseline
	for rows.Next() {
		var b BehavioralBaseline
		var raw []byte
		if err := rows.Scan(&b.ID, &b.TenantID, &b.NodeID, &b.SignalType, &b.Dimension, &raw, &b.WindowDays, &b.ComputedAt); err != nil {
			return nil, err
		}
		m, err := decodeJSONBMap(raw)
		if err != nil {
			return nil, err
		}
		b.Baseline = m
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---------- port_observations ----------

type CreatePortObservationParams struct {
	TenantID uuid.UUID
	NodeID   *uuid.UUID
	Port     int
	Protocol string
	State    string
}

func (s *Store) CreatePortObservation(ctx context.Context, p CreatePortObservationParams) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	var nodeArg any
	if p.NodeID != nil && *p.NodeID != uuid.Nil {
		nodeArg = *p.NodeID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO port_observations (id, tenant_id, node_id, port, protocol, state, observed_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, NOW())
	`, p.TenantID, nodeArg, p.Port, p.Protocol, p.State)
	return err
}

// PortObservationStats aggregates counts per (port, state) for recommendations.
type PortObservationStats struct {
	Port     int
	Protocol string
	State    string
	Count    int
}

func (s *Store) AggregatePortObservations(ctx context.Context, tenantID uuid.UUID, since time.Time) ([]PortObservationStats, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT port, protocol, state, COUNT(*)
		FROM port_observations
		WHERE tenant_id = $1 AND observed_at >= $2
		GROUP BY port, protocol, state
		ORDER BY COUNT(*) DESC
	`, tenantID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PortObservationStats
	for rows.Next() {
		var s PortObservationStats
		if err := rows.Scan(&s.Port, &s.Protocol, &s.State, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
