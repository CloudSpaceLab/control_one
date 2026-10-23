package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

type CompletePatchJobParams struct {
	NodeID           uuid.UUID
	JobID            uuid.UUID
	Action           string
	Succeeded        bool
	PackagesUpgraded int
	LogTail          string
	Error            string
}

// CompletePatchJob atomically validates dispatch ownership and terminalizes both
// job and patch state. Row locks serialize concurrent reports and cancellation.
// False means this exact terminal result was already committed.
func (s *Store) CompletePatchJob(ctx context.Context, p CompletePatchJobParams) (bool, error) {
	if s.db == nil || p.NodeID == uuid.Nil || p.JobID == uuid.Nil {
		return false, errors.New("patch completion identity required")
	}
	if p.Action != "patch.deploy_direct" && p.Action != "patch.deploy_proxy" && p.Action != "patch.deploy_airgapped" {
		return false, errors.New("invalid patch action")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var stateID, deploymentID, stateTenant, jobTenant uuid.UUID
	var stateStatus, jobStatus, jobType string
	var payload []byte
	err = tx.QueryRowContext(ctx, `SELECT p.id,p.deployment_id,p.tenant_id,p.status,j.tenant_id,j.status,j.type,j.payload
		FROM node_patch_state p JOIN jobs j ON j.id=p.job_id
		WHERE p.job_id=$1 AND p.node_id=$2 FOR UPDATE OF p,j`, p.JobID, p.NodeID).Scan(&stateID, &deploymentID, &stateTenant, &stateStatus, &jobTenant, &jobStatus, &jobType, &payload)
	if err != nil {
		return false, fmt.Errorf("load owned patch dispatch: %w", err)
	}
	var dispatch struct {
		NodeID       string `json:"node_id"`
		DeploymentID string `json:"deployment_id"`
		StateID      string `json:"node_patch_state_id"`
	}
	if json.Unmarshal(payload, &dispatch) != nil || stateTenant != jobTenant || jobType != p.Action || dispatch.NodeID != p.NodeID.String() || dispatch.DeploymentID != deploymentID.String() || dispatch.StateID != stateID.String() {
		return false, errors.New("patch dispatch ownership mismatch")
	}
	wantState, wantJob := "failed", JobStatusFailed
	if p.Succeeded {
		wantState, wantJob = "applied", JobStatusSucceeded
	}
	if stateStatus == wantState && jobStatus == string(wantJob) {
		return false, nil
	}
	if stateStatus != "pending" || jobStatus == string(JobStatusCancelled) || jobStatus == string(JobStatusFailed) || jobStatus == string(JobStatusSucceeded) {
		return false, errors.New("patch dispatch already terminal or cancelled")
	}
	now := s.clock().UTC()
	if p.Succeeded {
		_, err = tx.ExecContext(ctx, `UPDATE node_patch_state SET status='applied',applied_at=$2,packages_upgraded=$3,log_tail=$4,error=NULL WHERE id=$1`, stateID, now, p.PackagesUpgraded, p.LogTail)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE node_patch_state SET status='failed',applied_at=$2,log_tail=$3,error=$4 WHERE id=$1`, stateID, now, p.LogTail, p.Error)
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status=$2,updated_at=$3,finished_at=$3 WHERE id=$1`, p.JobID, wantJob, now); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at) VALUES($1,$2,$3,$4,$5)`, uuid.New(), p.JobID, wantJob, "agent patch completion committed", now); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
