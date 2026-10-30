package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

func (s *Store) SetOfflineRepository(ctx context.Context, tenant, node uuid.UUID, repo patchsupport.OfflineRepository) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	body, _ := json.Marshal(repo)
	res, err := s.db.ExecContext(ctx, `UPDATE nodes SET labels=COALESCE(labels,'{}'::jsonb)||jsonb_build_object('patch.offline_repository',$3::jsonb),updated_at=NOW() WHERE id=$1 AND tenant_id=$2`, node, tenant, body)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) QueueOfflinePreparation(ctx context.Context, tenant, node uuid.UUID, p patchsupport.OfflinePreparation) (*Job, error) {
	if p.NodeID != node.String() {
		return nil, fmt.Errorf("preparation target mismatch")
	}
	if err := p.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT labels FROM nodes WHERE id=$1 AND tenant_id=$2 AND state='active' FOR UPDATE`, node, tenant).Scan(&raw); err != nil {
		return nil, err
	}
	if err = rejectUnreconciledProxyAction(ctx, tx, tenant, node); err != nil {
		return nil, err
	}
	var labels map[string]json.RawMessage
	_ = json.Unmarshal(raw, &labels)
	var repo patchsupport.OfflineRepository
	var runtime patchsupport.Evidence
	if json.Unmarshal(labels["patch.offline_repository"], &repo) != nil || !reflect.DeepEqual(repo, p.Repository) || !repo.Enabled {
		return nil, fmt.Errorf("approved repository changed or disabled")
	}
	if json.Unmarshal(labels["patch.runtime"], &runtime) != nil || !runtime.Current(time.Now().UTC()) || runtime.OfflinePrepareVersion != 1 || runtime.Runtime.Validate("airgapped") != nil {
		return nil, fmt.Errorf("current compatible preparation runtime required")
	}
	var existingID uuid.UUID
	var existingRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT id,payload FROM jobs WHERE tenant_id=$1 AND type=$2 AND payload->>'node_id'=$3 AND status IN ('queued','running') ORDER BY created_at LIMIT 1 FOR UPDATE`, tenant, patchsupport.PrepareOfflineJob, node.String()).Scan(&existingID, &existingRaw)
	if err == nil {
		var old patchsupport.OfflinePreparation
		if json.Unmarshal(existingRaw, &old) != nil || !reflect.DeepEqual(old.Repository, p.Repository) || !reflect.DeepEqual(old.Packages, p.Packages) {
			return nil, fmt.Errorf("another offline preparation is already in progress")
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetJob(ctx, existingID)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	body, _ := json.Marshal(p)
	id := uuid.New()
	now := s.clock()
	_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,tenant_id,type,status,payload,retries,max_retries,started_at,created_at,updated_at) VALUES($1,$2,$3,'running',$4,0,0,$5,$5,$5)`, id, tenant, patchsupport.PrepareOfflineJob, body, now)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET labels=COALESCE(labels,'{}'::jsonb)||jsonb_build_object('patch.offline_preparation',$2::text) WHERE id=$1`, node, id.String()); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at) VALUES($1,$2,'running',$3,$4)`, uuid.New(), id, "Offline plan preparation requested by "+p.RequestedBy+"; no package installation authorized", now)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetJob(ctx, id)
}

func (s *Store) PendingOfflinePreparations(ctx context.Context, tenant, node uuid.UUID) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,payload FROM jobs WHERE tenant_id=$1 AND type=$2 AND payload->>'node_id'=$3 AND status='running' ORDER BY created_at LIMIT 2`, tenant, patchsupport.PrepareOfflineJob, node.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j := Job{TenantID: tenant, Type: patchsupport.PrepareOfflineJob, Status: JobStatusRunning}
		if err = rows.Scan(&j.ID, &j.Payload); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) CompleteOfflinePreparation(ctx context.Context, tenant, node, id uuid.UUID, succeeded bool, message string) error {
	return s.finishOfflinePreparation(ctx, tenant, node, id, succeeded, message, false, "")
}

// RecordOfflinePreparationReceipt retains the agent result even when the server
// already timed the request out. It never rewrites an existing terminal result.
func (s *Store) RecordOfflinePreparationReceipt(ctx context.Context, tenant, node, id uuid.UUID, succeeded bool, message string, manifest ...string) error {
	digest := ""
	if len(manifest) == 1 {
		digest = manifest[0]
	}
	return s.finishOfflinePreparation(ctx, tenant, node, id, succeeded, message, true, digest)
}

func (s *Store) finishOfflinePreparation(ctx context.Context, tenant, node, id uuid.UUID, succeeded bool, message string, receipt bool, manifest string) error {
	status := JobStatusFailed
	if succeeded {
		status = JobStatusSucceeded
	}
	if len(message) > 4096 {
		message = message[:4096]
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if receipt {
		var payload []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload FROM jobs WHERE id=$1 AND tenant_id=$2 AND type=$3 AND payload->>'node_id'=$4 FOR UPDATE`, id, tenant, patchsupport.PrepareOfflineJob, node.String()).Scan(&payload); err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(payload, &fields); err != nil {
			return err
		}
		var previous string
		_ = json.Unmarshal(fields["_offline_preparation_receipt"], &previous)
		if previous != "" && previous != string(status) {
			return fmt.Errorf("conflicting preparation receipt")
		}
		if previous == "" {
			if _, err = tx.ExecContext(ctx, `UPDATE jobs SET payload=payload||jsonb_build_object('_offline_preparation_receipt',$2::text,'_offline_preparation_receipt_message',$3::text,'_offline_preparation_receipt_at',NOW()),updated_at=NOW() WHERE id=$1`, id, string(status), message); err != nil {
				return err
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET status=$4,finished_at=NOW(),updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND type=$5 AND payload->>'node_id'=$3 AND status IN ('queued','running')`, id, tenant, node.String(), status, patchsupport.PrepareOfflineJob)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 && receipt && succeeded {
		decoded, decodeErr := hex.DecodeString(manifest)
		if decodeErr == nil && len(decoded) == 32 {
			_, err = tx.ExecContext(ctx, `UPDATE nodes SET labels=COALESCE(labels,'{}'::jsonb)||jsonb_build_object('patch.offline_prepared_plan',jsonb_build_object('job_id',$3::text,'manifest_sha256',$4::text,'completed_at',NOW())) WHERE id=$1 AND tenant_id=$2`, node, tenant, id.String(), manifest)
			if err != nil {
				return err
			}
		}
	}
	if n > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at) VALUES($1,$2,$3,$4,NOW())`, uuid.New(), id, status, message)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
