package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOnlineDispatchDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	for _, scenario := range []string{"direct", "proxy", "missing approval", "stripped", "changed payload", "changed action plan", "withdrawn", "changed proxy", "changed calendar", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			nodeID := uuid.New()
			now := time.Now().UTC()
			mode := "direct"
			if scenario == "proxy" || scenario == "changed proxy" {
				mode = "proxy"
			}
			transport := patchsupport.OnlineTransport{NodeID: nodeID.String(), TenantID: tenant.ID.String(), Mode: mode}
			var proxy *SquidProxy
			if mode == "proxy" {
				proxyHost := "proxy-" + uuid.NewString() + ".local"
				proxy, err = s.CreateSquidProxy(ctx, SquidProxy{TenantID: tenant.ID, Host: proxyHost, Port: 3128, Status: "registered", Whitelist: []string{"archive.ubuntu.com"}})
				require.NoError(t, err)
				transport.ProxyID = proxy.ID.String()
				transport.ProxyURL = "http://" + proxyHost + ":3128"
				transport.RepositoryHosts = proxy.Whitelist
			}
			plan := patchsupport.OnlinePlan{Version: 1, NodeID: nodeID.String(), Mode: mode, ProxyURL: transport.ProxyURL, RepositoryHosts: transport.RepositoryHosts, TransportContext: transport.Digest(), InstalledStateSHA256: strings.Repeat("b", 64), InputsSHA256: strings.Repeat("c", 64), ManifestSHA256: strings.Repeat("d", 64), Requested: []string{"liba"}, Resolved: []patchsupport.ExactPackage{{Name: "liba", Version: "2"}}, Review: patchsupport.OfflineReview{PreparedAt: now, ExpiresAt: now.Add(time.Hour), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []patchsupport.PlanChange{{Name: "liba", InstalledVersion: "1", ProposedVersion: "2", Role: "requested"}}}}
			plan.SHA256 = plan.Digest()
			runtime := patchsupport.Runtime{OS: "linux", Manager: "apt", OnlinePlanVersion: 1}
			node, err := s.CreateNode(ctx, &Node{ID: nodeID, TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: map[string]any{"patch.runtime": patchsupport.Evidence{Runtime: runtime, ObservedAt: now}, "patch.online_plan": map[string]any{"status": "ready", "plan": plan, "job_id": uuid.NewString(), "observed_at": now}}})
			require.NoError(t, err)
			if proxy != nil {
				_, err = s.UpsertNodePatchConfig(ctx, NodePatchConfig{NodeID: node.ID, Mode: "proxy", ProxyID: &proxy.ID})
				require.NoError(t, err)
			}
			d, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: tenant.ID, Mode: mode, TargetNodeCount: 1, Summary: map[string]any{"online_plan": plan}})
			require.NoError(t, err)
			a, err := s.CreatePatchApproval(ctx, CreatePatchApprovalParams{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: d.ID, Mode: mode, ExpiresAt: plan.Review.ExpiresAt, PolicyHash: "reviewed-exact-online-policy"})
			require.NoError(t, err)
			payload := map[string]any{"online_plan": plan, "package_allowlist": plan.Requested, "post_patch_rescan": true, "patch_runtime": runtime, "proxy_url": plan.ProxyURL}
			p := DispatchPatchJobParams{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: d.ID, Mode: mode, ApprovalID: a.ID, PolicyHash: a.PolicyHash, Plan: CreateActionPlanParams{State: ActionPlanStateQueued, Diff: map[string]any{"online_plan": plan}}}
			switch scenario {
			case "missing approval":
				p.ApprovalID = uuid.Nil
			case "stripped":
				delete(payload, "online_plan")
			case "changed payload":
				copy := plan
				copy.ManifestSHA256 = strings.Repeat("e", 64)
				copy.SHA256 = copy.Digest()
				payload["online_plan"] = copy
			case "changed action plan":
				delete(p.Plan.Diff, "online_plan")
			case "withdrawn":
				require.NoError(t, s.RefreshOnlinePlanReport(ctx, tenant.ID, node.ID, plan.SHA256, "unavailable"))
			case "changed proxy":
				require.NoError(t, s.UpdateSquidProxyWhitelist(ctx, proxy.ID, []string{"different.example"}))
			case "changed calendar":
				payload["patch_window"] = &patchsupport.Window{ID: uuid.NewString(), OpensAt: now.Add(-time.Minute), ClosesAt: now.Add(time.Hour)}
			case "expired":
				copy := plan
				copy.Review.ExpiresAt = now.Add(-time.Second)
				copy.SHA256 = copy.Digest()
				payload["online_plan"] = copy
			}
			p.Payload, err = json.Marshal(payload)
			require.NoError(t, err)
			state, err := s.DispatchPatchJob(ctx, p)
			if scenario == "direct" || scenario == "proxy" {
				require.NoError(t, err)
				require.NotNil(t, state.JobID)
				again, e := s.DispatchPatchJob(ctx, p)
				require.NoError(t, e)
				require.Equal(t, state.JobID, again.JobID)
				resolved, e := s.GetPatchApproval(ctx, a.ID)
				require.NoError(t, e)
				require.Equal(t, ApprovalStatusApproved, resolved.Status)
			} else {
				require.Error(t, err)
				var jobs int
				require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE payload->>'deployment_id'=$1`, d.ID.String()).Scan(&jobs))
				require.Zero(t, jobs)
				resolved, e := s.GetPatchApproval(ctx, a.ID)
				require.NoError(t, e)
				require.Equal(t, ApprovalStatusPending, resolved.Status)
			}
		})
	}
}
