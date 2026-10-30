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

func TestOfflineDispatchDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	for _, scenario := range []string{"approved", "missing approval", "missing runtime", "old agent", "legacy manifest", "content changed", "plan changed", "no rescan", "published approved", "published expired", "published changed", "published withdrawn", "published stripped source", "published action source"} {
		t.Run(scenario, func(t *testing.T) {
			content := &patchsupport.OfflineContent{Format: 2, ManifestSHA256: strings.Repeat("a", 64), InstalledStatusSHA256: strings.Repeat("b", 64), KeyringSHA256: strings.Repeat("c", 64), Suite: "noble-updates", Component: "main", Architecture: "amd64", Requested: []patchsupport.ExactPackage{{Name: "liblzma5", Version: "1.2.3"}}, Resolved: []patchsupport.ExactPackage{{Name: "liblzma5", Version: "1.2.3"}}}
			runtime := patchsupport.Runtime{OS: "linux", Manager: "apt", OfflineAPTVersion: 1}
			if scenario == "old agent" {
				runtime.OfflineAPTVersion = 0
			}
			node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: map[string]any{"patch.runtime": patchsupport.Evidence{Runtime: runtime, ObservedAt: time.Now().UTC()}}})
			require.NoError(t, err)
			summary := map[string]any{"offline_content": content, "package_allowlist": []string{"liblzma5"}, "post_patch_rescan": true}
			if strings.HasPrefix(scenario, "published ") {
				summary["published_offline_plan_sha256"] = content.ManifestSHA256
				report := patchsupport.PublishedPlanReport{Version: 1, Status: "ready", Plans: []patchsupport.OfflineContent{*content}}
				observed := time.Now().UTC()
				switch scenario {
				case "published expired":
					observed = observed.Add(-6 * time.Minute)
				case "published changed":
					report.Plans[0].ManifestSHA256 = strings.Repeat("d", 64)
				case "published withdrawn":
					report.Status = "unavailable"
					report.Plans = nil
				}
				node.Labels["patch.offline_plans"] = patchsupport.PublishedPlanEvidence{Report: report, ObservedAt: observed}
				require.NoError(t, s.UpdateNodeLabels(ctx, node.ID, node.Labels))
			}
			deployment, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: tenant.ID, Mode: "airgapped", TargetNodeCount: 1, Summary: summary})
			require.NoError(t, err)
			approval, err := s.CreatePatchApproval(ctx, CreatePatchApprovalParams{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: deployment.ID, Mode: "airgapped", PolicyHash: "isolated-offline-fixture-policy", ExpiresAt: time.Now().Add(time.Hour)})
			require.NoError(t, err)
			payload := map[string]any{"offline_content": content, "patch_runtime": runtime, "post_patch_rescan": true, "package_allowlist": []string{"liblzma5"}}
			p := DispatchPatchJobParams{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: deployment.ID, Mode: "airgapped", ApprovalID: approval.ID, PolicyHash: approval.PolicyHash, Plan: CreateActionPlanParams{State: ActionPlanStateQueued, Diff: map[string]any{"offline_content": content}}}
			if strings.HasPrefix(scenario, "published ") {
				payload["published_offline_plan_sha256"] = content.ManifestSHA256
				p.Plan.Diff["published_offline_plan_sha256"] = content.ManifestSHA256
			}
			switch scenario {
			case "published stripped source":
				delete(payload, "published_offline_plan_sha256")
			case "published action source":
				delete(p.Plan.Diff, "published_offline_plan_sha256")
			case "missing approval":
				p.ApprovalID = uuid.Nil
			case "missing runtime":
				delete(payload, "patch_runtime")
			case "legacy manifest":
				content.Format = 1
			case "content changed":
				content.ManifestSHA256 = strings.Repeat("d", 64)
			case "plan changed":
				p.Plan.Diff["offline_content"] = nil
			case "no rescan":
				payload["post_patch_rescan"] = false
			}
			p.Payload, err = json.Marshal(payload)
			require.NoError(t, err)
			state, err := s.DispatchPatchJob(ctx, p)
			if scenario == "approved" || scenario == "published approved" {
				require.NoError(t, err)
				require.NotNil(t, state.JobID)
				after, err := s.GetPatchApproval(ctx, approval.ID)
				require.NoError(t, err)
				require.Equal(t, ApprovalStatusApproved, after.Status)
				retried, err := s.DispatchPatchJob(ctx, p)
				require.NoError(t, err)
				require.Equal(t, state.JobID, retried.JobID)
			} else {
				require.Error(t, err)
				var count int
				require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM node_patch_state WHERE deployment_id=$1`, deployment.ID).Scan(&count))
				require.Zero(t, count)
				after, e := s.GetPatchApproval(ctx, approval.ID)
				require.NoError(t, e)
				require.Equal(t, ApprovalStatusPending, after.Status)
			}
		})
	}
}
