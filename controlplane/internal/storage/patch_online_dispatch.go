package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

func validateOnlineDispatch(ctx context.Context, tx *sql.Tx, p DispatchPatchJobParams, plan *patchsupport.OnlinePlan, allow, deny []string, rescan bool, runtime *patchsupport.Runtime, window *patchsupport.Window) error {
	if plan == nil {
		if p.Mode == "direct" || p.Mode == "proxy" {
			var raw []byte
			var evidence patchsupport.Evidence
			if err := tx.QueryRowContext(ctx, `SELECT labels->'patch.runtime' FROM nodes WHERE id=$1 AND tenant_id=$2`, p.NodeID, p.TenantID).Scan(&raw); err != nil {
				return err
			}
			if json.Unmarshal(raw, &evidence) == nil && evidence.OnlinePlanVersion == 1 {
				return fmt.Errorf("exact online plan cannot be stripped from dispatch")
			}
		}
		return nil
	}
	now := time.Now().UTC()
	scope := append([]string(nil), allow...)
	sort.Strings(scope)
	if p.ApprovalID == uuid.Nil || !rescan || len(deny) > 0 || runtime == nil || runtime.OnlinePlanVersion != 1 || plan.NodeID != p.NodeID.String() || plan.Mode != p.Mode || !reflect.DeepEqual(scope, plan.Requested) || plan.Validate(now) != nil {
		return fmt.Errorf("exact online dispatch requires current matching plan, scope, runtime and approval")
	}
	var reviewedRaw, labelsRaw []byte
	if err := tx.QueryRowContext(ctx, `SELECT summary->'online_plan' FROM patch_deployments WHERE id=$1 AND tenant_id=$2 FOR SHARE`, p.DeploymentID, p.TenantID).Scan(&reviewedRaw); err != nil {
		return err
	}
	var reviewed patchsupport.OnlinePlan
	if json.Unmarshal(reviewedRaw, &reviewed) != nil || !reflect.DeepEqual(reviewed, *plan) {
		return fmt.Errorf("online payload differs from reviewed deployment")
	}
	body, err := json.Marshal(p.Plan.Diff["online_plan"])
	if err != nil {
		return err
	}
	var actionPlan patchsupport.OnlinePlan
	if json.Unmarshal(body, &actionPlan) != nil || !reflect.DeepEqual(actionPlan, *plan) {
		return fmt.Errorf("online action plan differs from reviewed deployment")
	}
	if err = tx.QueryRowContext(ctx, `SELECT labels->'patch.online_plan' FROM nodes WHERE id=$1 AND tenant_id=$2`, p.NodeID, p.TenantID).Scan(&labelsRaw); err != nil {
		return err
	}
	var evidence struct {
		Status     string                  `json:"status"`
		Plan       patchsupport.OnlinePlan `json:"plan"`
		JobID      string                  `json:"job_id"`
		ObservedAt time.Time               `json:"observed_at"`
	}
	if json.Unmarshal(labelsRaw, &evidence) != nil || evidence.Status != "ready" || evidence.JobID == "" || evidence.ObservedAt.IsZero() || evidence.ObservedAt.After(now) || now.Sub(evidence.ObservedAt) > 5*time.Minute || !reflect.DeepEqual(evidence.Plan, *plan) {
		return fmt.Errorf("online selection changed or expired at dispatch commit")
	}
	var configuredProxy, configuredWindow string
	cfgErr := tx.QueryRowContext(ctx, `SELECT COALESCE(proxy_id::text,''),COALESCE(window_id::text,'') FROM node_patch_config WHERE node_id=$1 FOR SHARE`, p.NodeID).Scan(&configuredProxy, &configuredWindow)
	if cfgErr != nil && cfgErr != sql.ErrNoRows {
		return cfgErr
	}
	expectedWindow := ""
	if window != nil {
		expectedWindow = window.ID
	}
	if configuredWindow != expectedWindow {
		return fmt.Errorf("online node calendar association changed at dispatch commit")
	}
	transport := patchsupport.OnlineTransport{NodeID: p.NodeID.String(), TenantID: p.TenantID.String(), Mode: p.Mode}
	if p.Mode == "proxy" {
		var proxyID uuid.UUID
		proxyID, err = uuid.Parse(configuredProxy)
		if err != nil {
			return fmt.Errorf("reviewed repository proxy association unavailable")
		}
		var host, status string
		var port int
		var whitelist []byte
		if err = tx.QueryRowContext(ctx, `SELECT host,port,whitelist,status FROM squid_proxies WHERE id=$1 AND tenant_id=$2 FOR SHARE`, proxyID, p.TenantID).Scan(&host, &port, &whitelist, &status); err != nil {
			return err
		}
		if status == "removed" {
			return fmt.Errorf("reviewed proxy was removed")
		}
		transport.ProxyID = proxyID.String()
		transport.ProxyURL = fmt.Sprintf("http://%s:%d", host, port)
		if err = json.Unmarshal(whitelist, &transport.RepositoryHosts); err != nil {
			return err
		}
		var hostNode uuid.UUID
		var revision int64
		var clients, probes []byte
		err = tx.QueryRowContext(ctx, `SELECT node_id,revision,client_cidrs,probe_urls FROM squid_proxy_management WHERE proxy_id=$1 AND tenant_id=$2 FOR SHARE`, proxyID, p.TenantID).Scan(&hostNode, &revision, &clients, &probes)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			transport.ProxyNodeID = hostNode.String()
			transport.Revision = revision
			if err = json.Unmarshal(clients, &transport.ClientCIDRs); err != nil {
				return err
			}
			if err = json.Unmarshal(probes, &transport.ProbeURLs); err != nil {
				return err
			}
		}
	}
	if transport.Digest() != plan.TransportContext || transport.ProxyURL != plan.ProxyURL || !reflect.DeepEqual(transport.RepositoryHosts, plan.RepositoryHosts) {
		return fmt.Errorf("online proxy/transport policy changed at dispatch commit")
	}
	return nil
}
