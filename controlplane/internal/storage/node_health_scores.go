package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NodeHealthScore is the latest predictive health snapshot for a single
// node. The score is recomputed hourly by JobTypeHealthPredict; the
// table only ever holds one row per node, upserted in place.
//
// Components carries the per-signal penalty breakdown (e.g.
// {"smart_reallocated": -35, "primary_component": "smart_reallocated"})
// plus calibration metadata when RiskLevel == "calibrating".
type NodeHealthScore struct {
	NodeID     uuid.UUID
	Score      int
	RiskLevel  string
	Components map[string]any
	ComputedAt time.Time
}

// UpsertNodeHealthScoreParams is the operator-set portion. Score must
// be 0..100; RiskLevel is one of low|medium|high|critical|calibrating.
type UpsertNodeHealthScoreParams struct {
	NodeID     uuid.UUID
	Score      int
	RiskLevel  string
	Components map[string]any
}

// GetNodeHealthScore returns the latest score for the node, or nil + nil
// when no row exists (node has never been scored).
func (s *Store) GetNodeHealthScore(ctx context.Context, nodeID uuid.UUID) (*NodeHealthScore, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT node_id, score, risk_level, components, computed_at
		FROM node_health_scores
		WHERE node_id = $1
	`, nodeID)
	var out NodeHealthScore
	var raw []byte
	if err := row.Scan(&out.NodeID, &out.Score, &out.RiskLevel, &raw, &out.ComputedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get node health score: %w", err)
	}
	m, err := decodeJSONBMap(raw)
	if err != nil {
		return nil, err
	}
	out.Components = m
	return &out, nil
}

// UpsertNodeHealthScore writes the latest score, replacing any existing
// row for the node. computed_at is bumped to NOW() each call.
func (s *Store) UpsertNodeHealthScore(ctx context.Context, p UpsertNodeHealthScoreParams) (*NodeHealthScore, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.NodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	if p.Score < 0 || p.Score > 100 {
		return nil, errors.New("score must be 0..100")
	}
	if p.RiskLevel == "" {
		p.RiskLevel = "calibrating"
	}
	componentsJSON, err := marshalJSONBMap(p.Components)
	if err != nil {
		return nil, fmt.Errorf("marshal components: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO node_health_scores (node_id, score, risk_level, components, computed_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (node_id) DO UPDATE SET
			score       = EXCLUDED.score,
			risk_level  = EXCLUDED.risk_level,
			components  = EXCLUDED.components,
			computed_at = NOW()
		RETURNING node_id, score, risk_level, components, computed_at
	`, p.NodeID, p.Score, p.RiskLevel, componentsJSON)
	var out NodeHealthScore
	var raw []byte
	if err := row.Scan(&out.NodeID, &out.Score, &out.RiskLevel, &raw, &out.ComputedAt); err != nil {
		return nil, fmt.Errorf("upsert node health score: %w", err)
	}
	m, err := decodeJSONBMap(raw)
	if err != nil {
		return nil, err
	}
	out.Components = m
	return &out, nil
}

// AtRiskNodeRow joins node_health_scores with nodes for the fleet
// roll-up endpoint. Tenant filter is applied via the nodes table so we
// can scope per tenant without storing tenant_id redundantly on the
// score table.
type AtRiskNodeRow struct {
	NodeID     uuid.UUID
	TenantID   uuid.UUID
	Hostname   string
	Score      int
	RiskLevel  string
	Components map[string]any
	ComputedAt time.Time
}

// ListAtRiskNodes returns nodes whose score is at or below the threshold
// (i.e. higher risk), ordered by score ASC (worst first). When threshold
// is 0 or negative, the default of 49 is used (HIGH + CRIT bands).
// Calibrating rows are excluded — they're not at-risk, just unknown.
func (s *Store) ListAtRiskNodes(ctx context.Context, tenantID uuid.UUID, threshold int) ([]AtRiskNodeRow, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if threshold <= 0 {
		threshold = 49
	}
	clauses := []string{"hs.risk_level <> 'calibrating'", "hs.score <= $1"}
	args := []any{threshold}
	if tenantID != uuid.Nil {
		args = append(args, tenantID)
		clauses = append(clauses, fmt.Sprintf("n.tenant_id = $%d", len(args)))
	}
	q := `
		SELECT hs.node_id, n.tenant_id, n.hostname, hs.score, hs.risk_level, hs.components, hs.computed_at
		FROM node_health_scores hs
		JOIN nodes n ON n.id = hs.node_id
		WHERE ` + clauses[0]
	for i := 1; i < len(clauses); i++ {
		q += " AND " + clauses[i]
	}
	q += `
		ORDER BY hs.score ASC, hs.computed_at DESC
	`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list at-risk nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AtRiskNodeRow
	for rows.Next() {
		var r AtRiskNodeRow
		var raw []byte
		if err := rows.Scan(&r.NodeID, &r.TenantID, &r.Hostname, &r.Score, &r.RiskLevel, &raw, &r.ComputedAt); err != nil {
			return nil, fmt.Errorf("scan at-risk node: %w", err)
		}
		m, err := decodeJSONBMap(raw)
		if err != nil {
			return nil, err
		}
		r.Components = m
		out = append(out, r)
	}
	return out, rows.Err()
}
