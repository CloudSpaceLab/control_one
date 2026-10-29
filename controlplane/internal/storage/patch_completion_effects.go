package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// PatchCompletionEffectsCommitted is consulted after ownership validation, before
// acknowledging an agent's result. Legacy completions must be reconciled first.
func (s *Store) PatchCompletionEffectsCommitted(ctx context.Context, jobID uuid.UUID) (bool, error) {
	if s.db == nil {
		return false, errors.New("store database not initialized")
	}
	var ready bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM patch_completion_effects WHERE job_id=$1)`, jobID).Scan(&ready)
	return ready, err
}

// completePatchEffects runs inside the completion transaction with the job and
// node-state locks held. Parent locking serializes concurrent node rollups;
// the aggregate must be read in a separate statement after acquiring that lock.
func (s *Store) completePatchEffects(ctx context.Context, tx *sql.Tx, p CompletePatchJobParams, stateID, deploymentID, tenantID uuid.UUID, rawPlanID string, rescan bool) error {
	var target int
	if err := tx.QueryRowContext(ctx, `SELECT target_node_count FROM patch_deployments WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, deploymentID, tenantID).Scan(&target); err != nil {
		return fmt.Errorf("lock patch deployment: %w", err)
	}
	if err := completePatchVerificationTx(ctx, tx, p, deploymentID, tenantID); err != nil {
		return err
	}
	if rawPlanID != "" {
		planID, err := uuid.Parse(rawPlanID)
		if err != nil || planID == uuid.Nil {
			return errors.New("invalid patch action plan identity")
		}
		var owned bool
		if err := tx.QueryRowContext(ctx, `SELECT true FROM action_plans WHERE id=$1 AND tenant_id=$2 AND node_id=$3 AND domain='patch' AND action_kind='patch.deploy' FOR UPDATE`, planID, tenantID, p.NodeID).Scan(&owned); err != nil {
			return fmt.Errorf("lock owned patch action plan: %w", err)
		}
		status := "failed"
		if p.Succeeded {
			status = "succeeded"
		}
		receipt := map[string]any{"job_id": p.JobID.String(), "node_patch_state_id": stateID.String(), "deployment_id": deploymentID.String(), "status": status}
		if p.Succeeded {
			receipt["packages_upgraded"] = p.PackagesUpgraded
		}
		if p.LogTail != "" {
			receipt["log_tail"] = p.LogTail
		}
		rawReceipt, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		verification, err := json.Marshal(map[string]any{"node_patch_state": status, "job_status": status, "post_patch_inventory": p.Succeeded && rescan,"security_verification":"verification_pending","promotion_eligible":false})
		if err != nil {
			return err
		}
		// The job lock serializes retries; retain any append-only legacy receipt.
		if _, err := tx.ExecContext(ctx, `INSERT INTO action_receipts(action_plan_id,tenant_id,node_id,job_id,state,receipt,verification,error)
			SELECT $1,$2,$3,$4,$5,$6,$7,$8 WHERE NOT EXISTS
			(SELECT 1 FROM action_receipts WHERE action_plan_id=$1 AND tenant_id=$2 AND node_id=$3 AND job_id=$4 AND state=$5)`, planID, tenantID, p.NodeID, p.JobID, status, rawReceipt, verification, p.Error); err != nil {
			return fmt.Errorf("save patch receipt: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE action_plans SET state=$2,updated_at=NOW() WHERE id=$1`, planID, status); err != nil {
			return err
		}
	}
	if err := rollupPatchDeploymentTx(ctx, tx, deploymentID, target); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO patch_completion_effects(job_id) VALUES($1) ON CONFLICT(job_id) DO NOTHING`, p.JobID)
	return err
}

// RollupPatchDeployment shares the completion transaction's parent lock with
// approval outcomes, so an older application-side snapshot cannot overwrite it.
func (s *Store) RollupPatchDeployment(ctx context.Context, deploymentID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var target int
	if err = tx.QueryRowContext(ctx, `SELECT target_node_count FROM patch_deployments WHERE id=$1 FOR UPDATE`, deploymentID).Scan(&target); err != nil {
		return err
	}
	if err = rollupPatchDeploymentTx(ctx, tx, deploymentID, target); err != nil {
		return err
	}
	return tx.Commit()
}

func rollupPatchDeploymentTx(ctx context.Context, tx *sql.Tx, deploymentID uuid.UUID, target int) error {
	var total, terminal, applied, failed int
	if err := tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE status IN ('applied','failed')),count(*) FILTER(WHERE status='applied'),count(*) FILTER(WHERE status='failed') FROM node_patch_state WHERE deployment_id=$1`, deploymentID).Scan(&total, &terminal, &applied, &failed); err != nil {
		return err
	}
	status := "in_progress"
	finished := total >= target && total > 0 && terminal == total
	if finished {
		status = "completed"
		if failed > 0 {
			status = "partial"
		}
		if applied == 0 {
			status = "failed"
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE patch_deployments SET status=$2,started_at=COALESCE(started_at,NOW()),finished_at=CASE WHEN $3 THEN COALESCE(finished_at,NOW()) ELSE NULL END WHERE id=$1`, deploymentID, status, finished); err != nil {
		return fmt.Errorf("roll up patch deployment: %w", err)
	}
	return nil
}
