package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type ProxyManagement struct {
	ProxyID     uuid.UUID  `json:"proxy_id"`
	NodeID      uuid.UUID  `json:"node_id"`
	Revision    int64      `json:"revision"`
	ClientCIDRs []string   `json:"client_cidrs"`
	ProbeURLs   []string   `json:"probe_urls"`
	LastJobID   *uuid.UUID `json:"last_job_id,omitempty"`
}

func (s *Store) GetProxyManagement(ctx context.Context, tenant, proxy uuid.UUID) (*ProxyManagement, error) {
	p := &ProxyManagement{}
	var clients, probes []byte
	err := s.db.QueryRowContext(ctx, `SELECT proxy_id,node_id,revision,client_cidrs,probe_urls,last_job_id FROM squid_proxy_management WHERE proxy_id=$1 AND tenant_id=$2`, proxy, tenant).Scan(&p.ProxyID, &p.NodeID, &p.Revision, &clients, &probes, &p.LastJobID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(clients, &p.ClientCIDRs); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(probes, &p.ProbeURLs); err != nil {
		return nil, err
	}
	return p, nil
}

// Timeout records uncertainty but deliberately leaves the missing-receipt
// conflict in place. An offline target is not proved stopped by its deadline.
func (s *Store) ExpireManagedProxy(ctx context.Context, tenant, proxy, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var updated uuid.UUID
	err = tx.QueryRowContext(ctx, `UPDATE jobs SET status='failed',finished_at=NOW(),updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND type IN ('squid.install','squid.reconfigure','squid.remove') AND status='running' AND (payload->>'expires_at')::timestamptz<NOW() AND EXISTS(SELECT 1 FROM squid_proxy_management WHERE proxy_id=$3 AND tenant_id=$2 AND last_job_id=$1) RETURNING id`, id, tenant, proxy).Scan(&updated)
	if err == sql.ErrNoRows {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE squid_proxies SET status='degraded',last_validated_at=NULL,last_error='Proxy job timed out; target outcome unknown; reconcile before a fresh change',updated_at=NOW() WHERE id=$1 AND tenant_id=$2`, proxy, tenant)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at)VALUES($1,$2,'failed','Proxy action expired; target outcome unknown; no automatic retry',NOW())`, uuid.New(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Binding is immutable. Reusing an address does not authorize managing a
// different machine. Policy edits are performed only by a reviewed new job.
func (s *Store) BindProxyManagement(ctx context.Context, tenant, proxy, node uuid.UUID, clients, probes []string) (*ProxyManagement, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var hostname string
	if err = tx.QueryRowContext(ctx, `SELECT hostname FROM nodes WHERE id=$1 AND tenant_id=$2 AND state='active' FOR UPDATE`, node, tenant).Scan(&hostname); err != nil {
		return nil, err
	}
	var host, status string
	var port int
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT host,port,status,whitelist FROM squid_proxies WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, proxy, tenant).Scan(&host, &port, &status, &raw); err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(hostname, ".")) || port != 3128 || status == "removed" {
		return nil, fmt.Errorf("management requires the registered host to match the enrolled hostname and port 3128")
	}
	var hosts []string
	if err = json.Unmarshal(raw, &hosts); err != nil {
		return nil, err
	}
	if _, err = patchsupport.ManagedProxyConfig(hosts, clients); err != nil {
		return nil, err
	}
	if err = patchsupport.ValidateProxyProbes(hosts, probes); err != nil {
		return nil, err
	}
	if err = rejectUnreconciledProxyAction(ctx, tx, tenant, node); err != nil {
		return nil, err
	}
	c, _ := json.Marshal(clients)
	p, _ := json.Marshal(probes)
	_, err = tx.ExecContext(ctx, `INSERT INTO squid_proxy_management(proxy_id,tenant_id,node_id,revision,client_cidrs,probe_urls) VALUES($1,$2,$3,1,$4,$5)`, proxy, tenant, node, c, p)
	if err != nil {
		return nil, fmt.Errorf("proxy or host is already bound: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetProxyManagement(ctx, tenant, proxy)
}
