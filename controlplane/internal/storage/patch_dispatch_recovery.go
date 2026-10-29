package storage

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sort"
	"time"
)

// ReconcileIncompletePatchDispatches fails old, unlinked intents explicitly. A
// live pending approval is actionable and is never reclaimed. Nothing is rerun.
func (s *Store) ReconcileIncompletePatchDispatches(ctx context.Context, nodeID uuid.UUID) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	now := s.clock().UTC()
	cutoff := now.Add(-5 * time.Minute)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.deployment_id,p.tenant_id FROM node_patch_state p
 WHERE p.node_id=$1 AND p.status='pending' AND p.job_id IS NULL AND p.requested_at<$2
 AND NOT EXISTS(SELECT 1 FROM patch_approvals a WHERE a.deployment_id=p.deployment_id AND a.node_id=p.node_id AND a.tenant_id=p.tenant_id AND a.status='pending' AND a.expires_at>$3)
 ORDER BY p.id FOR UPDATE OF p`, nodeID, cutoff, now)
	if err != nil {
		return 0, err
	}
	type intent struct{ id, deployment, tenant uuid.UUID }
	var intents []intent
	for rows.Next() {
		var p intent
		if err = rows.Scan(&p.id, &p.deployment, &p.tenant); err != nil {
			_ = rows.Close()
			return 0, err
		}
		intents = append(intents, p)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	const reason = "patch dispatch incomplete before job linkage; no automatic execution; reconcile and create a new deployment"
	deployments := map[uuid.UUID]bool{}
	for _, p := range intents {
		if _, err = tx.ExecContext(ctx, `UPDATE node_patch_state SET status='failed',applied_at=$2,error=$3 WHERE id=$1`, p.id, now, reason); err != nil {
			return 0, err
		}
		// Cancel only legacy orphan jobs which have no state link anywhere.
		if _, err = tx.ExecContext(ctx, `WITH cancelled AS (
   UPDATE jobs j SET status='cancelled',updated_at=$4,finished_at=$4 WHERE j.tenant_id=$2
   AND j.type IN ('patch.deploy_direct','patch.deploy_proxy','patch.deploy_airgapped') AND j.status IN ('queued','running')
   AND j.payload->>'node_patch_state_id'=$1 AND j.payload->>'node_id'=$3
   AND NOT EXISTS(SELECT 1 FROM node_patch_state linked WHERE linked.job_id=j.id) RETURNING j.id)
   INSERT INTO job_events(id,job_id,status,message,created_at) SELECT gen_random_uuid(),id,'cancelled',$5,$4 FROM cancelled`, p.id.String(), p.tenant, nodeID.String(), now, reason); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE action_plans SET state='failed',updated_at=$3 WHERE tenant_id=$1 AND node_id=$2 AND domain='patch' AND source_ref->>'node_patch_state_id'=$4 AND state IN ('queued','running','proposed')`, p.tenant, nodeID, now, p.id.String()); err != nil {
			return 0, err
		}
		deployments[p.deployment] = true
	}
	ids := make([]uuid.UUID, 0, len(deployments))
	for id := range deployments {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for _, id := range ids {
		var target int
		if err = tx.QueryRowContext(ctx, `SELECT target_node_count FROM patch_deployments WHERE id=$1 FOR UPDATE`, id).Scan(&target); err != nil {
			return 0, err
		}
		if err = rollupPatchDeploymentTx(ctx, tx, id, target); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(intents), nil
}
