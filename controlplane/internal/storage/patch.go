package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// PatchDeployment is one operator-initiated patch run. It fans out to N
// node_patch_state rows; status rolls up across them.
type PatchDeployment struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Mode            string // direct | proxy | airgapped
	Status          string // pending | in_progress | completed | partial | failed
	TargetNodeCount int
	RequestedBy     *uuid.UUID
	RequestedAt     time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	Summary         map[string]any

	// Roll-up counts populated by ListPatchDeployments via subquery.
	NodesPending int `json:"nodes_pending,omitempty"`
	NodesApplied int `json:"nodes_applied,omitempty"`
	NodesFailed  int `json:"nodes_failed,omitempty"`
}

// NodePatchState is the per-node row created when a deployment fans out.
// Status moves pending → applied | failed as the agent reports back.
type PatchDeploymentSummary struct {
	Total            int `json:"total"`
	Pending          int `json:"pending"`
	InProgress       int `json:"in_progress"`
	Completed        int `json:"completed"`
	Partial          int `json:"partial"`
	Failed           int `json:"failed"`
	PendingApprovals int `json:"pending_approvals"`
	ExpiredApprovals int `json:"expired_approvals"`

	ActiveNodes         int `json:"active_nodes"`
	InventoryNodes      int `json:"inventory_nodes"`
	FreshInventoryNodes int `json:"fresh_inventory_nodes"`
	DirectNodes         int `json:"direct_nodes"`
	ProxyNodes          int `json:"proxy_nodes"`
	AirgappedNodes      int `json:"airgapped_nodes"`

	KnownAffectedNodes     int `json:"known_affected_nodes"`
	KnownActiveFindings    int `json:"known_active_findings"`
	KnownCriticalFindings  int `json:"known_critical_findings"`
	KnownHighFindings      int `json:"known_high_findings"`
	KnownKEVFindings       int `json:"known_kev_findings"`
	KnownPatchableFindings int `json:"known_patchable_findings"`

	WindowsScheduled int `json:"windows_scheduled"`
	WindowsOpen      int `json:"windows_open"`
	WindowsClosing   int `json:"windows_closing"`
	ProxiesHealthy   int `json:"proxies_healthy"`
	ProxiesDegraded  int `json:"proxies_degraded"`
}

func (s *Store) GetPatchDeploymentSummary(ctx context.Context, tenantIDs []uuid.UUID) (PatchDeploymentSummary, error) {
	var out PatchDeploymentSummary
	if s.db == nil {
		return out, errors.New("store database not initialized")
	}
	values := make([]string, 0, len(tenantIDs))
	for _, tenantID := range tenantIDs {
		if tenantID != uuid.Nil {
			values = append(values, tenantID.String())
		}
	}
	if len(values) == 0 {
		return out, nil
	}

	tenantArray := pq.Array(values)
	if err := s.db.QueryRowContext(ctx, `
		WITH active_nodes AS (
			SELECT id, tenant_id
			FROM nodes
			WHERE tenant_id = ANY($1::uuid[])
			  AND state = 'active'
		),
		node_posture AS (
			SELECT
				COUNT(*)::int AS active_nodes,
				COUNT(i.node_id)::int AS inventory_nodes,
				COUNT(i.node_id) FILTER (
					WHERE i.last_seen_at >= NOW() - INTERVAL '24 hours'
				)::int AS fresh_inventory_nodes,
				COUNT(*) FILTER (WHERE COALESCE(c.mode, 'direct') = 'direct')::int AS direct_nodes,
				COUNT(*) FILTER (WHERE c.mode = 'proxy')::int AS proxy_nodes,
				COUNT(*) FILTER (WHERE c.mode = 'airgapped')::int AS airgapped_nodes
			FROM active_nodes n
			LEFT JOIN node_inventory_sync i ON i.node_id = n.id
			LEFT JOIN node_patch_config c ON c.node_id = n.id
		),
		vulnerability_posture AS (
			SELECT
				COUNT(DISTINCT f.node_id)::int AS affected_nodes,
				COUNT(*)::int AS active_findings,
				COUNT(*) FILTER (WHERE LOWER(f.severity) = 'critical')::int AS critical_findings,
				COUNT(*) FILTER (WHERE LOWER(f.severity) = 'high')::int AS high_findings,
				COUNT(*) FILTER (WHERE f.kev)::int AS kev_findings,
				COUNT(*) FILTER (
					WHERE NULLIF(BTRIM(COALESCE(f.fixed_version, '')), '') IS NOT NULL
				)::int AS patchable_findings
			FROM node_vulnerability_findings f
			JOIN active_nodes n ON n.id = f.node_id
			WHERE f.tenant_id = ANY($1::uuid[])
			  AND f.resolved_at IS NULL
		),
		deployment_posture AS (
			SELECT
				COUNT(*)::int AS total,
				COUNT(*) FILTER (WHERE status = 'pending')::int AS pending,
				COUNT(*) FILTER (WHERE status = 'in_progress')::int AS in_progress,
				COUNT(*) FILTER (WHERE status = 'completed')::int AS completed,
				COUNT(*) FILTER (WHERE status = 'partial')::int AS partial,
				COUNT(*) FILTER (WHERE status = 'failed')::int AS failed
			FROM patch_deployments
			WHERE tenant_id = ANY($1::uuid[])
		),
		approval_posture AS (
			SELECT
				COUNT(*) FILTER (
					WHERE status = 'pending' AND expires_at > NOW()
				)::int AS pending,
				COUNT(*) FILTER (
					WHERE status = 'expired'
					   OR (status = 'pending' AND expires_at <= NOW())
				)::int AS expired
			FROM patch_approvals
			WHERE tenant_id = ANY($1::uuid[])
		),
		window_posture AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'scheduled')::int AS scheduled,
				COUNT(*) FILTER (WHERE status = 'open')::int AS open,
				COUNT(*) FILTER (WHERE status = 'closing')::int AS closing
			FROM maintenance_windows
			WHERE tenant_id = ANY($1::uuid[])
		),
		proxy_posture AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'healthy')::int AS healthy,
				COUNT(*) FILTER (WHERE status = 'degraded')::int AS degraded
			FROM squid_proxies
			WHERE tenant_id = ANY($1::uuid[])
		)
		SELECT
			d.total, d.pending, d.in_progress, d.completed, d.partial, d.failed,
			a.pending, a.expired,
			n.active_nodes, n.inventory_nodes, n.fresh_inventory_nodes,
			n.direct_nodes, n.proxy_nodes, n.airgapped_nodes,
			v.affected_nodes, v.active_findings, v.critical_findings, v.high_findings,
			v.kev_findings, v.patchable_findings,
			w.scheduled, w.open, w.closing,
			p.healthy, p.degraded
		FROM deployment_posture d
		CROSS JOIN approval_posture a
		CROSS JOIN node_posture n
		CROSS JOIN vulnerability_posture v
		CROSS JOIN window_posture w
		CROSS JOIN proxy_posture p
	`, tenantArray).Scan(
		&out.Total,
		&out.Pending,
		&out.InProgress,
		&out.Completed,
		&out.Partial,
		&out.Failed,
		&out.PendingApprovals,
		&out.ExpiredApprovals,
		&out.ActiveNodes,
		&out.InventoryNodes,
		&out.FreshInventoryNodes,
		&out.DirectNodes,
		&out.ProxyNodes,
		&out.AirgappedNodes,
		&out.KnownAffectedNodes,
		&out.KnownActiveFindings,
		&out.KnownCriticalFindings,
		&out.KnownHighFindings,
		&out.KnownKEVFindings,
		&out.KnownPatchableFindings,
		&out.WindowsScheduled,
		&out.WindowsOpen,
		&out.WindowsClosing,
		&out.ProxiesHealthy,
		&out.ProxiesDegraded,
	); err != nil {
		return out, fmt.Errorf("patch summary: %w", err)
	}
	return out, nil
}

type NodePatchState struct {
	ID               uuid.UUID
	DeploymentID     uuid.UUID
	NodeID           uuid.UUID
	TenantID         uuid.UUID
	Status           string
	PackagesUpgraded *int
	LogTail          *string
	Error            *string
	JobID            *uuid.UUID
	RequestedAt      time.Time
	AppliedAt        *time.Time
}

// CreatePatchDeployment inserts the deployment header. Returns the new row
// with id + timestamps populated.
func (s *Store) CreatePatchDeployment(ctx context.Context, in PatchDeployment) (*PatchDeployment, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	mode := in.Mode
	if mode == "" {
		mode = "direct"
	}
	summary := in.Summary
	if summary == nil {
		summary = map[string]any{}
	}
	summaryBytes, err := json.Marshal(summary)
	if err != nil {
		return nil, fmt.Errorf("marshal summary: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO patch_deployments
			(tenant_id, mode, status, target_node_count, requested_by, summary)
		VALUES ($1, $2, 'pending', $3, $4, $5)
		RETURNING id, tenant_id, mode, status, target_node_count, requested_by,
		          requested_at, started_at, finished_at, summary
	`, in.TenantID, mode, in.TargetNodeCount, in.RequestedBy, summaryBytes)
	out, err := scanPatchDeployment(row)
	if err != nil {
		return nil, fmt.Errorf("create patch deployment: %w", err)
	}
	return out, nil
}

// ListPatchDeployments returns deployments for a tenant in reverse chrono
// order, with rolled-up counts joined from node_patch_state. limit/offset
// pagination is bounded; callers can keep paging until result < limit.
func (s *Store) ListPatchDeployments(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]PatchDeployment, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			d.id, d.tenant_id, d.mode, d.status, d.target_node_count, d.requested_by,
			d.requested_at, d.started_at, d.finished_at, d.summary,
			COUNT(s.id) FILTER (WHERE s.status = 'pending') AS nodes_pending,
			COUNT(s.id) FILTER (WHERE s.status = 'applied') AS nodes_applied,
			COUNT(s.id) FILTER (WHERE s.status = 'failed')  AS nodes_failed
		FROM patch_deployments d
		LEFT JOIN node_patch_state s ON s.deployment_id = d.id
		WHERE d.tenant_id = $1
		GROUP BY d.id
		ORDER BY d.requested_at DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list patch deployments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PatchDeployment
	for rows.Next() {
		d := PatchDeployment{}
		var summaryBytes []byte
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.Mode, &d.Status, &d.TargetNodeCount, &d.RequestedBy,
			&d.RequestedAt, &d.StartedAt, &d.FinishedAt, &summaryBytes,
			&d.NodesPending, &d.NodesApplied, &d.NodesFailed,
		); err != nil {
			return nil, fmt.Errorf("scan patch deployment: %w", err)
		}
		if len(summaryBytes) > 0 {
			_ = json.Unmarshal(summaryBytes, &d.Summary)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetPatchDeployment fetches one deployment by id (no rollup).
func (s *Store) GetPatchDeployment(ctx context.Context, id uuid.UUID) (*PatchDeployment, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, mode, status, target_node_count, requested_by,
		       requested_at, started_at, finished_at, summary
		FROM patch_deployments WHERE id = $1
	`, id)
	out, err := scanPatchDeployment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// UpdatePatchDeploymentStatus rolls up a deployment when nodes finish
// reporting. Caller computes the new status from the per-node counts.
func (s *Store) UpdatePatchDeploymentStatus(ctx context.Context, id uuid.UUID, status string, finished bool) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if finished {
		_, err := s.db.ExecContext(ctx, `
			UPDATE patch_deployments
			SET status = $2,
			    started_at = COALESCE(started_at, NOW()),
			    finished_at = NOW()
			WHERE id = $1
		`, id, status)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE patch_deployments
		SET status = $2,
		    started_at = COALESCE(started_at, NOW())
		WHERE id = $1
	`, id, status)
	return err
}

// CreateNodePatchState inserts one per-node row in pending state.
func (s *Store) CreateNodePatchState(ctx context.Context, in NodePatchState) (*NodePatchState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO node_patch_state
			(deployment_id, node_id, tenant_id, status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (deployment_id, node_id) DO NOTHING
		RETURNING id, deployment_id, node_id, tenant_id, status,
		          packages_upgraded, log_tail, error, job_id,
		          requested_at, applied_at
	`, in.DeploymentID, in.NodeID, in.TenantID)
	out, err := scanNodePatchState(row)
	if errors.Is(err, sql.ErrNoRows) {
		// Row already existed — read back.
		return s.getNodePatchStateByDeploymentNode(ctx, in.DeploymentID, in.NodeID)
	}
	return out, err
}

// SetNodePatchStateJobID links a row to its dispatched job. Heartbeat
// dispatch reads this to surface "pending" actions to the right node.
func (s *Store) SetNodePatchStateJobID(ctx context.Context, stateID, jobID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_patch_state SET job_id = $2 WHERE id = $1
	`, stateID, jobID)
	return err
}

// MarkNodePatchApplied is called from the heartbeat completion path.
func (s *Store) MarkNodePatchApplied(ctx context.Context, id uuid.UUID, packagesUpgraded int, logTail string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_patch_state
		SET status = 'applied',
		    applied_at = NOW(),
		    packages_upgraded = $2,
		    log_tail = $3,
		    error = NULL
		WHERE id = $1 AND status = 'pending'
	`, id, packagesUpgraded, logTail)
	return err
}

// MarkNodePatchFailed records a non-zero exit / error.
func (s *Store) MarkNodePatchFailed(ctx context.Context, id uuid.UUID, errMsg, logTail string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_patch_state
		SET status = 'failed',
		    error = $2,
		    log_tail = $3
		WHERE id = $1 AND status = 'pending'
	`, id, errMsg, logTail)
	return err
}

// ListPendingNodePatchStates returns the rows the given node still needs to
// execute. Drives heartbeat PendingActions.
func (s *Store) ListPendingNodePatchStates(ctx context.Context, nodeID uuid.UUID) ([]NodePatchState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, deployment_id, node_id, tenant_id, status,
		       packages_upgraded, log_tail, error, job_id, requested_at, applied_at
		FROM node_patch_state
		WHERE node_id = $1 AND status = 'pending' AND job_id IS NOT NULL
		ORDER BY requested_at ASC
		LIMIT 50
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list pending node patch states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanNodePatchStateRows(rows)
}

// ListNodePatchStatesForDeployment returns every per-node row for an
// operator's deployment — drives the per-deployment side panel.
func (s *Store) ListNodePatchStatesForDeployment(ctx context.Context, deploymentID uuid.UUID) ([]NodePatchState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, deployment_id, node_id, tenant_id, status,
		       packages_upgraded, log_tail, error, job_id, requested_at, applied_at
		FROM node_patch_state
		WHERE deployment_id = $1
		ORDER BY requested_at ASC
	`, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("list node patch states for deployment: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanNodePatchStateRows(rows)
}

// GetNodePatchStateByJobID lets the heartbeat completion path map an
// agent-reported job id back to the row it owns.
func (s *Store) GetNodePatchStateByJobID(ctx context.Context, jobID uuid.UUID) (*NodePatchState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, deployment_id, node_id, tenant_id, status,
		       packages_upgraded, log_tail, error, job_id, requested_at, applied_at
		FROM node_patch_state WHERE job_id = $1 LIMIT 1
	`, jobID)
	out, err := scanNodePatchState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

func (s *Store) getNodePatchStateByDeploymentNode(ctx context.Context, deploymentID, nodeID uuid.UUID) (*NodePatchState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, deployment_id, node_id, tenant_id, status,
		       packages_upgraded, log_tail, error, job_id, requested_at, applied_at
		FROM node_patch_state WHERE deployment_id = $1 AND node_id = $2
	`, deploymentID, nodeID)
	out, err := scanNodePatchState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// ── scanners ──────────────────────────────────────────────────────────────

func scanPatchDeployment(row *sql.Row) (*PatchDeployment, error) {
	d := &PatchDeployment{}
	var summaryBytes []byte
	if err := row.Scan(
		&d.ID, &d.TenantID, &d.Mode, &d.Status, &d.TargetNodeCount, &d.RequestedBy,
		&d.RequestedAt, &d.StartedAt, &d.FinishedAt, &summaryBytes,
	); err != nil {
		return nil, err
	}
	if len(summaryBytes) > 0 {
		_ = json.Unmarshal(summaryBytes, &d.Summary)
	}
	return d, nil
}

func scanNodePatchState(row *sql.Row) (*NodePatchState, error) {
	r := &NodePatchState{}
	if err := row.Scan(
		&r.ID, &r.DeploymentID, &r.NodeID, &r.TenantID, &r.Status,
		&r.PackagesUpgraded, &r.LogTail, &r.Error, &r.JobID,
		&r.RequestedAt, &r.AppliedAt,
	); err != nil {
		return nil, err
	}
	return r, nil
}

func scanNodePatchStateRows(rows *sql.Rows) ([]NodePatchState, error) {
	var out []NodePatchState
	for rows.Next() {
		var r NodePatchState
		if err := rows.Scan(
			&r.ID, &r.DeploymentID, &r.NodeID, &r.TenantID, &r.Status,
			&r.PackagesUpgraded, &r.LogTail, &r.Error, &r.JobID,
			&r.RequestedAt, &r.AppliedAt,
		); err != nil {
			return nil, fmt.Errorf("scan node patch state: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
