package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

func (s *Store) QueueOnlinePreparation(ctx context.Context, tenant, node uuid.UUID, p patchsupport.OnlinePreparation) (*Job, error) {
	if p.NodeID != node.String() {
		return nil, fmt.Errorf("online preparation target mismatch")
	}
	if err := p.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var labelsRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT labels FROM nodes WHERE id=$1 AND tenant_id=$2 AND state='active' FOR UPDATE`, node, tenant).Scan(&labelsRaw); err != nil {
		return nil, err
	}
	if err = rejectUnreconciledProxyAction(ctx, tx, tenant, node); err != nil {
		return nil, err
	}
	var labels map[string]json.RawMessage
	_ = json.Unmarshal(labelsRaw, &labels)
	var runtime patchsupport.Evidence
	if json.Unmarshal(labels["patch.runtime"], &runtime) != nil || !runtime.Current(time.Now().UTC()) || runtime.OnlinePlanVersion != 1 || runtime.OS != "linux" || runtime.Manager != "apt" {
		return nil, fmt.Errorf("current exact-plan APT runtime required")
	}
	var existingID uuid.UUID
	var existing []byte
	err = tx.QueryRowContext(ctx, `SELECT id,payload FROM jobs WHERE tenant_id=$1 AND type=$2 AND payload->>'node_id'=$3 AND status IN ('queued','running') ORDER BY created_at LIMIT 1 FOR UPDATE`, tenant, patchsupport.PrepareOnlineJob, node.String()).Scan(&existingID, &existing)
	if err == nil {
		var old patchsupport.OnlinePreparation
		if json.Unmarshal(existing, &old) != nil || old.Mode != p.Mode || old.ProxyURL != p.ProxyURL || old.TransportContext != p.TransportContext || !reflect.DeepEqual(old.Packages, p.Packages) {
			return nil, fmt.Errorf("another online preparation is in progress")
		}
		if !old.ExpiresAt.After(time.Now().UTC()) {
			return nil, fmt.Errorf("previous online preparation expired; refresh its status before preparing again")
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,tenant_id,type,status,payload,retries,max_retries,started_at,created_at,updated_at) VALUES($1,$2,$3,'running',$4,0,0,$5,$5,$5)`, id, tenant, patchsupport.PrepareOnlineJob, body, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET labels=COALESCE(labels,'{}'::jsonb)||jsonb_build_object('patch.online_preparation',$2::text) WHERE id=$1`, node, id.String()); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at) VALUES($1,$2,'running',$3,$4)`, uuid.New(), id, "Exact online preparation requested; no package installation authorized", now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetJob(ctx, id)
}

func (s *Store) PendingOnlinePreparations(ctx context.Context, tenant, node uuid.UUID) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,payload FROM jobs WHERE tenant_id=$1 AND type=$2 AND payload->>'node_id'=$3 AND status='running' ORDER BY created_at LIMIT 2`, tenant, patchsupport.PrepareOnlineJob, node.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j := Job{TenantID: tenant, Type: patchsupport.PrepareOnlineJob, Status: JobStatusRunning}
		if err = rows.Scan(&j.ID, &j.Payload); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Agent receipt status is retained independently of the server's terminal job
// state. A late or superseded success is acknowledged but cannot publish a plan
// or revive an expired request. Publication and terminal result commit together.
func (s *Store) FinishOnlinePreparation(ctx context.Context, tenant, node, id uuid.UUID, receipt bool, result, message string, plan *patchsupport.OnlinePlan) error {
	if result != "succeeded" && result != "failed" {
		return fmt.Errorf("terminal online preparation result required")
	}
	if len(message) > 4096 {
		message = message[:4096]
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT payload,status FROM jobs WHERE id=$1 AND tenant_id=$2 AND type=$3 AND payload->>'node_id'=$4 FOR UPDATE`, id, tenant, patchsupport.PrepareOnlineJob, node.String()).Scan(&raw, &status); err != nil {
		return err
	}
	var p patchsupport.OnlinePreparation
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if receipt {
		var previous struct {
			Status string `json:"_online_preparation_receipt"`
		}
		_ = json.Unmarshal(raw, &previous)
		if previous.Status != "" && previous.Status != result {
			return fmt.Errorf("conflicting online preparation receipt")
		}
		if previous.Status == "" {
			if _, err = tx.ExecContext(ctx, `UPDATE jobs SET payload=payload||jsonb_build_object('_online_preparation_receipt',$2::text,'_online_preparation_receipt_message',$3::text,'_online_preparation_receipt_at',NOW()) WHERE id=$1`, id, result, message); err != nil {
				return err
			}
		}
	}
	if status != "queued" && status != "running" {
		return tx.Commit()
	}
	succeeded := receipt && result == "succeeded" && plan != nil && plan.Validate(time.Now().UTC()) == nil && plan.Matches(p) && p.ExpiresAt.After(time.Now().UTC())
	terminal := "failed"
	if succeeded {
		terminal = "succeeded"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status=$2,finished_at=NOW(),updated_at=NOW() WHERE id=$1`, id, terminal); err != nil {
		return err
	}
	if succeeded {
		body, _ := json.Marshal(plan)
		if _, err = tx.ExecContext(ctx, `UPDATE nodes SET labels=COALESCE(labels,'{}'::jsonb)||jsonb_build_object('patch.online_plan',jsonb_build_object('status','ready','plan',$3::jsonb,'job_id',$4::text,'observed_at',NOW())) WHERE id=$1 AND tenant_id=$2`, node, tenant, body, id.String()); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at) VALUES($1,$2,$3,$4,NOW())`, uuid.New(), id, terminal, message); err != nil {
		return err
	}
	return tx.Commit()
}

// A stale heartbeat cannot overwrite a newly published plan. Refresh only the
// status/observation of the matching immutable identity, preserving provenance.
func (s *Store) RefreshOnlinePlanReport(ctx context.Context, tenant, node uuid.UUID, expected, status string) error {
	if status != "ready" && status != "unavailable" {
		return fmt.Errorf("invalid online plan report status")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET labels=jsonb_set(jsonb_set(labels,'{patch.online_plan,status}',to_jsonb($4::text)),'{patch.online_plan,observed_at}',to_jsonb(NOW())),updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND labels->'patch.online_plan'->'plan'->>'sha256'=$3`, node, tenant, expected, status)
	return err
}
