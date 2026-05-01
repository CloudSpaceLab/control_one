package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NodeFirewallRule represents one operator-driven block/allow rule pinned to a
// specific node. One entity_action (the operator intent) fans out to N rows,
// one per affected node. Status moves: pending → applied | failed | removed.
type NodeFirewallRule struct {
	ID             uuid.UUID
	EntityActionID uuid.UUID
	NodeID         uuid.UUID
	TenantID       uuid.UUID
	Action         string  // block | allow
	Direction      string  // in | out
	Protocol       *string // tcp | udp | icmp | nil=any
	Port           *int    // 0/nil = any
	Source         *string // IP/CIDR being blocked
	Dest           *string
	Tag            string // c1-{entity_action_id}
	Status         string // pending | applied | failed | removed
	Error          *string
	JobID          *uuid.UUID
	RequestedAt    time.Time
	AppliedAt      *time.Time
	RemovedAt      *time.Time
}

// ActiveBlock is the rolled-up view of one entity_action across all the nodes
// it touched: how many succeeded, how many failed, how many are still pending.
type ActiveBlock struct {
	EntityActionID uuid.UUID
	TenantID       uuid.UUID
	EntityType     string
	EntityID       string
	Action         string
	Reason         *string
	ExpiresAt      *time.Time
	CreatedAt      time.Time
	TotalNodes     int
	NodesApplied   int
	NodesFailed    int
	NodesPending   int
	NodesRemoved   int
}

// NodeFirewallRuleInsert is the payload for CreateNodeFirewallRule.
type NodeFirewallRuleInsert struct {
	EntityActionID uuid.UUID
	NodeID         uuid.UUID
	TenantID       uuid.UUID
	Action         string
	Direction      string
	Protocol       *string
	Port           *int
	Source         *string
	Dest           *string
	Tag            string
}

// CreateNodeFirewallRule inserts a row in pending state. Used when the
// operator dispatches a block — one INSERT per affected node. Idempotent on
// (entity_action_id, node_id) — if the row already exists we leave it alone
// (caller should check existence first if they need an update path).
func (s *Store) CreateNodeFirewallRule(ctx context.Context, in NodeFirewallRuleInsert) (*NodeFirewallRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := &NodeFirewallRule{}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO node_firewall_rules
			(entity_action_id, node_id, tenant_id, action, direction, protocol, port, source, dest, tag, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending')
		ON CONFLICT (entity_action_id, node_id) DO NOTHING
		RETURNING id, entity_action_id, node_id, tenant_id, action, direction, protocol, port, source, dest, tag, status, requested_at
	`,
		in.EntityActionID, in.NodeID, in.TenantID, in.Action, in.Direction,
		in.Protocol, in.Port, in.Source, in.Dest, in.Tag,
	).Scan(
		&row.ID, &row.EntityActionID, &row.NodeID, &row.TenantID,
		&row.Action, &row.Direction, &row.Protocol, &row.Port,
		&row.Source, &row.Dest, &row.Tag, &row.Status, &row.RequestedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// Row already existed for this (entity_action_id, node_id); fetch it.
		return s.getNodeFirewallRuleByEntityAndNode(ctx, in.EntityActionID, in.NodeID)
	}
	if err != nil {
		return nil, fmt.Errorf("insert node firewall rule: %w", err)
	}
	return row, nil
}

// SetNodeFirewallRuleJobID updates the job_id pointer on an existing rule
// row. Called once the job is created so heartbeat dispatch can cross-reference.
func (s *Store) SetNodeFirewallRuleJobID(ctx context.Context, ruleID, jobID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_firewall_rules SET job_id = $2 WHERE id = $1
	`, ruleID, jobID)
	if err != nil {
		return fmt.Errorf("set node firewall rule job id: %w", err)
	}
	return nil
}

// MarkNodeFirewallRuleApplied records a successful agent-side apply.
func (s *Store) MarkNodeFirewallRuleApplied(ctx context.Context, ruleID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_firewall_rules
		SET status = 'applied', applied_at = NOW(), error = NULL
		WHERE id = $1 AND status IN ('pending','failed')
	`, ruleID)
	if err != nil {
		return fmt.Errorf("mark applied: %w", err)
	}
	return nil
}

// MarkNodeFirewallRuleFailed records an agent-side error.
func (s *Store) MarkNodeFirewallRuleFailed(ctx context.Context, ruleID uuid.UUID, errMsg string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_firewall_rules
		SET status = 'failed', error = $2
		WHERE id = $1 AND status = 'pending'
	`, ruleID, errMsg)
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

// MarkNodeFirewallRuleRemoved is set when the operator allows/expires a block.
func (s *Store) MarkNodeFirewallRuleRemoved(ctx context.Context, ruleID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_firewall_rules
		SET status = 'removed', removed_at = NOW()
		WHERE id = $1
	`, ruleID)
	if err != nil {
		return fmt.Errorf("mark removed: %w", err)
	}
	return nil
}

// ListPendingNodeFirewallRules returns the rules a given node is responsible
// for executing (status=pending, node matches). Drives heartbeat dispatch.
func (s *Store) ListPendingNodeFirewallRules(ctx context.Context, nodeID uuid.UUID) ([]NodeFirewallRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, entity_action_id, node_id, tenant_id, action, direction,
		       protocol, port, source, dest, tag, status, error, job_id,
		       requested_at, applied_at, removed_at
		FROM node_firewall_rules
		WHERE node_id = $1 AND status = 'pending'
		ORDER BY requested_at ASC
		LIMIT 50
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list pending firewall rules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanNodeFirewallRuleRows(rows)
}

// ListNodeFirewallRulesForEntityAction returns all rule rows for one
// operator action — used to roll up "applied to N/M nodes" status.
func (s *Store) ListNodeFirewallRulesForEntityAction(ctx context.Context, entityActionID uuid.UUID) ([]NodeFirewallRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, entity_action_id, node_id, tenant_id, action, direction,
		       protocol, port, source, dest, tag, status, error, job_id,
		       requested_at, applied_at, removed_at
		FROM node_firewall_rules
		WHERE entity_action_id = $1
		ORDER BY requested_at ASC
	`, entityActionID)
	if err != nil {
		return nil, fmt.Errorf("list firewall rules for entity action: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanNodeFirewallRuleRows(rows)
}

// ListActiveBlocks returns the rolled-up active-blocks view for a tenant.
// Joins entity_actions ⟕ node_firewall_rules; groups counts by status.
// Includes blocks that are wholly removed only when keepRemoved is true.
func (s *Store) ListActiveBlocks(ctx context.Context, tenantID uuid.UUID, limit, offset int, keepRemoved bool) ([]ActiveBlock, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	havingClause := "HAVING COUNT(r.id) FILTER (WHERE r.status IN ('pending','applied','failed')) > 0"
	if keepRemoved {
		havingClause = ""
	}
	q := `
		SELECT
			ea.id, ea.tenant_id, ea.entity_type, ea.entity_id,
			ea.action, ea.reason, ea.expires_at, ea.created_at,
			COUNT(r.id) AS total_nodes,
			COUNT(r.id) FILTER (WHERE r.status = 'applied') AS nodes_applied,
			COUNT(r.id) FILTER (WHERE r.status = 'failed')  AS nodes_failed,
			COUNT(r.id) FILTER (WHERE r.status = 'pending') AS nodes_pending,
			COUNT(r.id) FILTER (WHERE r.status = 'removed') AS nodes_removed
		FROM entity_actions ea
		JOIN node_firewall_rules r ON r.entity_action_id = ea.id
		WHERE ea.tenant_id = $1
		GROUP BY ea.id
		` + havingClause + `
		ORDER BY ea.created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := s.db.QueryContext(ctx, q, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list active blocks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ActiveBlock
	for rows.Next() {
		var b ActiveBlock
		if err := rows.Scan(
			&b.EntityActionID, &b.TenantID, &b.EntityType, &b.EntityID,
			&b.Action, &b.Reason, &b.ExpiresAt, &b.CreatedAt,
			&b.TotalNodes, &b.NodesApplied, &b.NodesFailed, &b.NodesPending, &b.NodesRemoved,
		); err != nil {
			return nil, fmt.Errorf("scan active block: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetNodeFirewallRuleByJobID is used by the heartbeat completion path: agent
// reports {job_id, status, error} and we need to find the rule row that owns
// that job to update its status.
func (s *Store) GetNodeFirewallRuleByJobID(ctx context.Context, jobID uuid.UUID) (*NodeFirewallRule, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, entity_action_id, node_id, tenant_id, action, direction,
		       protocol, port, source, dest, tag, status, error, job_id,
		       requested_at, applied_at, removed_at
		FROM node_firewall_rules
		WHERE job_id = $1
		LIMIT 1
	`, jobID)
	r, err := scanOneNodeFirewallRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (s *Store) getNodeFirewallRuleByEntityAndNode(ctx context.Context, entityActionID, nodeID uuid.UUID) (*NodeFirewallRule, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, entity_action_id, node_id, tenant_id, action, direction,
		       protocol, port, source, dest, tag, status, error, job_id,
		       requested_at, applied_at, removed_at
		FROM node_firewall_rules
		WHERE entity_action_id = $1 AND node_id = $2
	`, entityActionID, nodeID)
	r, err := scanOneNodeFirewallRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func scanOneNodeFirewallRule(row *sql.Row) (*NodeFirewallRule, error) {
	r := &NodeFirewallRule{}
	if err := row.Scan(
		&r.ID, &r.EntityActionID, &r.NodeID, &r.TenantID,
		&r.Action, &r.Direction, &r.Protocol, &r.Port,
		&r.Source, &r.Dest, &r.Tag, &r.Status, &r.Error, &r.JobID,
		&r.RequestedAt, &r.AppliedAt, &r.RemovedAt,
	); err != nil {
		return nil, err
	}
	return r, nil
}

func scanNodeFirewallRuleRows(rows *sql.Rows) ([]NodeFirewallRule, error) {
	var out []NodeFirewallRule
	for rows.Next() {
		var r NodeFirewallRule
		if err := rows.Scan(
			&r.ID, &r.EntityActionID, &r.NodeID, &r.TenantID,
			&r.Action, &r.Direction, &r.Protocol, &r.Port,
			&r.Source, &r.Dest, &r.Tag, &r.Status, &r.Error, &r.JobID,
			&r.RequestedAt, &r.AppliedAt, &r.RemovedAt,
		); err != nil {
			return nil, fmt.Errorf("scan firewall rule: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
