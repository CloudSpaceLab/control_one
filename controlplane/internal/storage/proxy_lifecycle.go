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

// Called while holding the node lock shared by dispatch/preparation creators.
func rejectUnreconciledProxyAction(ctx context.Context, tx *sql.Tx, tenant, node uuid.UUID) error {
	var busy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE tenant_id=$1 AND payload->>'node_id'=$2 AND type IN ('squid.install','squid.reconfigure','squid.remove') AND (status IN ('queued','running') OR NOT(payload ? '_proxy_receipt')))`, tenant, node.String()).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("unreconciled proxy action blocks package changes")
	}
	return nil
}

// QueueProxyLifecycle is a durable storage primitive, not a public dispatch
// endpoint. HTTP lifecycle actions remain gated until target qualification.
// All creators take the node lock before the proxy lock, in that order.
func (s *Store) QueueProxyLifecycle(ctx context.Context, tenant, node uuid.UUID, action string, p patchsupport.ProxyLifecycle) (*Job, error) {
	if p.NodeID != node.String() {
		return nil, fmt.Errorf("proxy target mismatch")
	}
	if err := p.Validate(action, time.Now().UTC()); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var labels []byte
	if err = tx.QueryRowContext(ctx, `SELECT labels FROM nodes WHERE id=$1 AND tenant_id=$2 AND state='active' FOR UPDATE`, node, tenant).Scan(&labels); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	var runtime patchsupport.Evidence
	if json.Unmarshal(labels, &fields) != nil || json.Unmarshal(fields["patch.runtime"], &runtime) != nil || !runtime.Current(time.Now().UTC()) || runtime.OS != "linux" || runtime.ProxyLifecycleVersion < 1 || (p.Revision > 0 && runtime.ProxyLifecycleVersion != 2) {
		return nil, fmt.Errorf("current durable proxy runtime required")
	}
	var proxy uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM squid_proxies WHERE id=$1 AND tenant_id=$2 AND status<>'removed' FOR UPDATE`, p.ProxyID, tenant).Scan(&proxy); err != nil {
		return nil, err
	}
	var id uuid.UUID
	var oldAction string
	var raw []byte
	// Expiry is not proof that an offline target stopped. An unresolved job
	// continues to block conflicting changes until its receipt is reconciled.
	err = tx.QueryRowContext(ctx, `SELECT id,type,payload FROM jobs WHERE tenant_id=$1 AND type IN ('squid.install','squid.reconfigure','squid.remove') AND (payload->>'node_id'=$2 OR payload->>'proxy_id'=$3) AND (status IN ('queued','running') OR NOT(payload ? '_proxy_receipt')) ORDER BY created_at LIMIT 1`, tenant, node.String(), p.ProxyID).Scan(&id, &oldAction, &raw)
	if err == nil {
		var old patchsupport.ProxyLifecycle
		if json.Unmarshal(raw, &old) != nil || oldAction != action || !reflect.DeepEqual(old, p) {
			return nil, fmt.Errorf("unreconciled proxy action already exists for this target or proxy")
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetJob(ctx, id)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if p.Revision > 0 {
		var boundNode uuid.UUID
		var revision int64
		if err = tx.QueryRowContext(ctx, `SELECT node_id,revision FROM squid_proxy_management WHERE proxy_id=$1 AND tenant_id=$2 FOR UPDATE`, p.ProxyID, tenant).Scan(&boundNode, &revision); err != nil {
			return nil, err
		}
		if boundNode != node || p.Revision != revision+1 {
			return nil, fmt.Errorf("proxy target binding or policy revision changed; reload before submitting")
		}
	}
	var busy bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE tenant_id=$1 AND payload->>'node_id'=$2 AND status IN ('queued','running') AND type IN ('patch.deploy_direct','patch.deploy_proxy','patch.deploy_airgapped','patch.prepare_offline'))`, tenant, node.String()).Scan(&busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, fmt.Errorf("patch or preparation already in progress")
	}
	if action == "squid.remove" {
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_patch_config WHERE proxy_id=$1 AND mode='proxy')`, p.ProxyID).Scan(&busy); err != nil {
			return nil, err
		}
		if busy {
			return nil, fmt.Errorf("reassign proxy-mode clients before removing this proxy")
		}
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}

	id = uuid.New()
	_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,tenant_id,type,status,payload,retries,max_retries,started_at,created_at,updated_at) VALUES($1,$2,$3,'running',$4,0,0,NOW(),NOW(),NOW())`, id, tenant, action, body)
	if err != nil {
		return nil, err
	}
	if p.Revision > 0 {
		clients, _ := json.Marshal(p.ClientCIDRs)
		probes, _ := json.Marshal(p.ProbeURLs)
		_, err = tx.ExecContext(ctx, `UPDATE squid_proxy_management SET revision=$2,client_cidrs=$3,probe_urls=$4,last_job_id=$5,updated_at=NOW() WHERE proxy_id=$1`, p.ProxyID, p.Revision, clients, probes, id)
		if err != nil {
			return nil, err
		}
		status := "installing"
		if action == "squid.remove" {
			status = "removing"
		}
		_, err = tx.ExecContext(ctx, `UPDATE squid_proxies SET status=$2,last_validated_at=NULL,last_error='Requested change awaiting target verification',updated_at=NOW() WHERE id=$1`, p.ProxyID, status)
		if err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at) VALUES($1,$2,'running','Durable proxy request reserved; target execution not yet verified',NOW())`, uuid.New(), id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetJob(ctx, id)
}

func (s *Store) PendingProxyLifecycle(ctx context.Context, tenant, node uuid.UUID) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,type,payload FROM jobs WHERE tenant_id=$1 AND payload->>'node_id'=$2 AND type IN ('squid.install','squid.reconfigure','squid.remove') AND status='running' ORDER BY created_at LIMIT 2`, tenant, node.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j := Job{TenantID: tenant, Status: JobStatusRunning}
		if err = rows.Scan(&j.ID, &j.Type, &j.Payload); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// RecordProxyLifecycleReceipt commits the first authenticated target result
// and its event together. It never infers upstream readiness or proxy health.
func (s *Store) RecordProxyLifecycleReceipt(ctx context.Context, tenant, node, id uuid.UUID, action, status, message string) error {
	return s.recordProxyReceipt(ctx, tenant, node, id, action, status, message, nil)
}

func (s *Store) RecordManagedProxyReceipt(ctx context.Context, tenant, node, id uuid.UUID, action, status, message string, proof patchsupport.ProxyLifecycleProof) error {
	return s.recordProxyReceipt(ctx, tenant, node, id, action, status, message, &proof)
}

func (s *Store) recordProxyReceipt(ctx context.Context, tenant, node, id uuid.UUID, action, status, message string, proof *patchsupport.ProxyLifecycleProof) error {
	if !patchsupport.IsProxyLifecycle(action) || (status != "succeeded" && status != "failed") {
		return fmt.Errorf("terminal proxy result required")
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
	var oldStatus string
	if err = tx.QueryRowContext(ctx, `SELECT j.payload,j.status FROM jobs j JOIN nodes n ON n.id=$3 AND n.tenant_id=j.tenant_id WHERE j.id=$1 AND j.tenant_id=$2 AND j.type=$4 AND j.payload->>'node_id'=$5 FOR UPDATE OF j`, id, tenant, node, action, node.String()).Scan(&raw, &oldStatus); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	var request patchsupport.ProxyLifecycle
	if err = json.Unmarshal(raw, &request); err != nil {
		return err
	}
	var previous string
	_ = json.Unmarshal(fields["_proxy_receipt"], &previous)
	if previous != "" {
		if previous != status {
			return fmt.Errorf("conflicting proxy receipt")
		}
		return tx.Commit()
	}
	if request.Revision > 0 {
		if proof == nil {
			return fmt.Errorf("managed proxy completion proof required")
		}
		var latest uuid.UUID
		var bound uuid.UUID
		var revision int64
		if err = tx.QueryRowContext(ctx, `SELECT last_job_id,node_id,revision FROM squid_proxy_management WHERE proxy_id=$1 AND tenant_id=$2 FOR UPDATE`, request.ProxyID, tenant).Scan(&latest, &bound, &revision); err != nil {
			return err
		}
		if latest != id || bound != node || revision != request.Revision {
			return fmt.Errorf("proxy receipt no longer matches the bound change")
		}
		if status == "succeeded" {
			if proof.ObservedAt.IsZero() || proof.ObservedAt.After(time.Now().UTC().Add(30*time.Second)) || proof.ObservedAt.After(request.ExpiresAt) {
				return fmt.Errorf("invalid proxy verification time")
			}
			if action == "squid.remove" {
				if !proof.Removed || proof.ServiceActive {
					return fmt.Errorf("proxy removal not verified")
				}
			} else {
				digest, e := patchsupport.ProxyConfigSHA(request.Whitelist, request.ClientCIDRs)
				if e != nil || proof.ConfigSHA256 != digest || !proof.ServiceActive || !proof.UpstreamVerified {
					return fmt.Errorf("proxy configuration/service/upstream proof incomplete")
				}
			}
		}
		proxyStatus := "degraded"
		if status == "succeeded" && (oldStatus == "running" || oldStatus == "queued") && time.Since(proof.ObservedAt) <= 5*time.Minute {
			proxyStatus = "healthy"
			if action == "squid.remove" {
				proxyStatus = "removed"
			}
		}
		var hosts []byte
		if action != "squid.remove" {
			hosts, _ = json.Marshal(request.Whitelist)
		}
		_, err = tx.ExecContext(ctx, `UPDATE squid_proxies SET status=$2,last_error=CASE WHEN $2 IN ('healthy','removed') THEN NULL ELSE $3 END,last_validated_at=CASE WHEN $2='healthy' THEN $6::timestamptz ELSE NULL END,whitelist=CASE WHEN $2='healthy' THEN $4::jsonb ELSE whitelist END,updated_at=NOW() WHERE id=$1 AND tenant_id=$5`, request.ProxyID, proxyStatus, message, hosts, tenant, proof.ObservedAt)
		if err != nil {
			return err
		}
		if proxyStatus == "removed" {
			if _, err = tx.ExecContext(ctx, `UPDATE squid_proxy_management SET retired_at=NOW() WHERE proxy_id=$1 AND last_job_id=$2`, request.ProxyID, id); err != nil {
				return err
			}
		}
		proofRaw, _ := json.Marshal(proof)
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET payload=payload||jsonb_build_object('_proxy_proof',$2::jsonb) WHERE id=$1`, id, proofRaw); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET payload=payload||jsonb_build_object('_proxy_receipt',$2::text,'_proxy_receipt_message',$3::text,'_proxy_receipt_at',NOW()),status=CASE WHEN status IN ('queued','running') THEN $2::job_status ELSE status END,finished_at=COALESCE(finished_at,NOW()),updated_at=NOW() WHERE id=$1`, id, status, message)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at) VALUES($1,$2,$3,$4,NOW())`, uuid.New(), id, status, "Proxy target receipt: "+message)
	if err != nil {
		return err
	}
	return tx.Commit()
}
