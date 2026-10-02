package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// PatchPosture is an exact tenant-scoped snapshot assembled from authoritative
// patch, inventory and vulnerability tables. Finding counts are explicitly
// "known" because they only represent unresolved findings already ingested.
type PatchPosture struct {
	ActiveNodes            int `json:"active_nodes"`
	InventoryNodes         int `json:"inventory_nodes"`
	KnownAffectedNodes     int `json:"known_affected_nodes"`
	KnownActiveFindings    int `json:"known_active_findings"`
	KnownCriticalFindings  int `json:"known_critical_findings"`
	KnownHighFindings      int `json:"known_high_findings"`
	KnownKEVFindings       int `json:"known_kev_findings"`
	KnownPatchableFindings int `json:"known_patchable_findings"`

	DeploymentsTotal      int `json:"deployments_total"`
	DeploymentsPending    int `json:"deployments_pending"`
	DeploymentsInProgress int `json:"deployments_in_progress"`
	DeploymentsCompleted  int `json:"deployments_completed"`
	DeploymentsPartial    int `json:"deployments_partial"`
	DeploymentsFailed     int `json:"deployments_failed"`

	PendingApprovals int `json:"pending_approvals"`
	ExpiredApprovals int `json:"expired_approvals"`

	WindowsScheduled int `json:"windows_scheduled"`
	WindowsOpen      int `json:"windows_open"`
	WindowsClosing   int `json:"windows_closing"`

	ProxiesHealthy  int `json:"proxies_healthy"`
	ProxiesDegraded int `json:"proxies_degraded"`

	DirectNodes    int `json:"direct_nodes"`
	ProxyNodes     int `json:"proxy_nodes"`
	AirgappedNodes int `json:"airgapped_nodes"`
}

func (s *Store) GetPatchPosture(ctx context.Context, tenantID uuid.UUID) (PatchPosture, error) {
	var out PatchPosture
	if s.db == nil {
		return out, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return out, errors.New("tenant_id is required")
	}

	err := s.db.QueryRowContext(ctx, `
		WITH active_nodes AS (
			SELECT id
			FROM nodes
			WHERE tenant_id = $1 AND state = 'active'
		),
		node_posture AS (
			SELECT
				COUNT(*) AS active_nodes,
				COUNT(i.node_id) AS inventory_nodes,
				COUNT(*) FILTER (WHERE COALESCE(c.mode, 'direct') = 'direct') AS direct_nodes,
				COUNT(*) FILTER (WHERE c.mode = 'proxy') AS proxy_nodes,
				COUNT(*) FILTER (WHERE c.mode = 'airgapped') AS airgapped_nodes
			FROM active_nodes n
			LEFT JOIN node_inventory_sync i ON i.node_id = n.id
			LEFT JOIN node_patch_config c ON c.node_id = n.id
		),
		vulnerability_posture AS (
			SELECT
				COUNT(DISTINCT f.node_id) AS affected_nodes,
				COUNT(*) AS active_findings,
				COUNT(*) FILTER (WHERE LOWER(f.severity) = 'critical') AS critical_findings,
				COUNT(*) FILTER (WHERE LOWER(f.severity) = 'high') AS high_findings,
				COUNT(*) FILTER (WHERE f.kev) AS kev_findings,
				COUNT(*) FILTER (
					WHERE NULLIF(BTRIM(COALESCE(f.fixed_version, '')), '') IS NOT NULL
				) AS patchable_findings
			FROM node_vulnerability_findings f
			JOIN active_nodes n ON n.id = f.node_id
			WHERE f.tenant_id = $1 AND f.resolved_at IS NULL
		),
		deployment_posture AS (
			SELECT
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE status = 'pending') AS pending,
				COUNT(*) FILTER (WHERE status = 'in_progress') AS in_progress,
				COUNT(*) FILTER (WHERE status = 'completed') AS completed,
				COUNT(*) FILTER (WHERE status = 'partial') AS partial,
				COUNT(*) FILTER (WHERE status = 'failed') AS failed
			FROM patch_deployments
			WHERE tenant_id = $1
		),
		approval_posture AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'pending' AND expires_at > NOW()) AS pending,
				COUNT(*) FILTER (WHERE status = 'pending' AND expires_at <= NOW()) AS expired
			FROM patch_approvals
			WHERE tenant_id = $1
		),
		window_posture AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'scheduled') AS scheduled,
				COUNT(*) FILTER (WHERE status = 'open') AS open,
				COUNT(*) FILTER (WHERE status = 'closing') AS closing
			FROM maintenance_windows
			WHERE tenant_id = $1
		),
		proxy_posture AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'healthy') AS healthy,
				COUNT(*) FILTER (WHERE status = 'degraded') AS degraded
			FROM squid_proxies
			WHERE tenant_id = $1
		)
		SELECT
			n.active_nodes, n.inventory_nodes,
			v.affected_nodes, v.active_findings, v.critical_findings, v.high_findings,
			v.kev_findings, v.patchable_findings,
			d.total, d.pending, d.in_progress, d.completed, d.partial, d.failed,
			a.pending, a.expired,
			w.scheduled, w.open, w.closing,
			p.healthy, p.degraded,
			n.direct_nodes, n.proxy_nodes, n.airgapped_nodes
		FROM node_posture n
		CROSS JOIN vulnerability_posture v
		CROSS JOIN deployment_posture d
		CROSS JOIN approval_posture a
		CROSS JOIN window_posture w
		CROSS JOIN proxy_posture p
	`, tenantID).Scan(
		&out.ActiveNodes, &out.InventoryNodes,
		&out.KnownAffectedNodes, &out.KnownActiveFindings, &out.KnownCriticalFindings, &out.KnownHighFindings,
		&out.KnownKEVFindings, &out.KnownPatchableFindings,
		&out.DeploymentsTotal, &out.DeploymentsPending, &out.DeploymentsInProgress, &out.DeploymentsCompleted,
		&out.DeploymentsPartial, &out.DeploymentsFailed,
		&out.PendingApprovals, &out.ExpiredApprovals,
		&out.WindowsScheduled, &out.WindowsOpen, &out.WindowsClosing,
		&out.ProxiesHealthy, &out.ProxiesDegraded,
		&out.DirectNodes, &out.ProxyNodes, &out.AirgappedNodes,
	)
	if err != nil {
		return PatchPosture{}, fmt.Errorf("get patch posture: %w", err)
	}
	return out, nil
}
