package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPatchVerificationLifecycleDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	for _, scenario := range []string{"fixed", "removed", "still_vulnerable", "service_unknown", "reboot_required", "narrower_feed", "event_rollback", "event_connection_loss", "duplicate_and_delayed", "execution_failed", "feed_expiry_at_commit", "inventory_changed_after_verification", "invalid_health_after_verification"} {
		t.Run(scenario, func(t *testing.T) {
			tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
			require.NoError(t, err)
			node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive})
			require.NoError(t, err)
			now := time.Now().UTC().Add(-time.Minute)
			items := []packageinventory.Item{{Name: "fixture-package", Version: "1", Source: "dpkg", Arch: "amd64"}}
			meta := packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now, SHA256: packageinventory.Digest(items), Count: 1}
			require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, items, meta, meta.SHA256))
			finding := VulnerabilityFinding{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, PackageName: "fixture-package", InstalledVersion: "1", PackageSource: "dpkg", Arch: "amd64", CVEID: "CVE-2026-LIFECYCLE-FIXTURE", FixedVersion: "2", EvidenceSource: "offline_vulnerability_feed", FirstSeenAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Second)}
			require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{finding}))
			deployment, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: tenant.ID, Mode: "direct", TargetNodeCount: 1})
			require.NoError(t, err)
			state, err := s.DispatchPatchJob(ctx, DispatchPatchJobParams{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: deployment.ID, Mode: "direct", Payload: json.RawMessage(`{"package_allowlist":["fixture-package"],"post_patch_rescan":true}`), Plan: CreateActionPlanParams{State: ActionPlanStateQueued, Diff: map[string]any{"package_allowlist": []string{"fixture-package"}}}})
			require.NoError(t, err)
			require.NotNil(t, state.JobID)
			before, err := s.ListNodePatchVerifications(ctx, tenant.ID, node.ID)
			require.NoError(t, err)
			require.Len(t, before, 1)
			require.Contains(t, string(before[0].Baseline), finding.CVEID)
			require.False(t, before[0].PromotionEligible)
			no, yes := false, true
			executionHealth := packageinventory.Health{ObservedAt: time.Now().UTC(), BootID: uuid.NewString(), RebootRequired: &no, PackageAudit: "healthy", ServiceHealth: "healthy", ServicesComplete: true, ConsumersComplete: true}
			rawHealth, err := json.Marshal(executionHealth)
			require.NoError(t, err)
			params := CompletePatchJobParams{NodeID: node.ID, JobID: *state.JobID, Action: "patch.deploy_direct", Succeeded: scenario != "execution_failed", Health: rawHealth}
			_, err = s.CompletePatchJob(ctx, params)
			require.NoError(t, err)
			ready, err := s.PatchVerificationAllowsPromotion(ctx, *state.JobID)
			require.NoError(t, err)
			require.False(t, ready, "installation result alone must never allow promotion")
			if scenario == "duplicate_and_delayed" {
				params.Health = json.RawMessage(`{"service_health":"unhealthy"}`)
				changed, err := s.CompletePatchJob(ctx, params)
				require.NoError(t, err)
				require.False(t, changed)
			}
			meta.ObservedAt = time.Now().UTC()
			items[0].Version = "2"
			if scenario == "still_vulnerable" {
				items[0].Version = "1"
			}
			if scenario == "removed" {
				items = nil
			}
			meta.Count = len(items)
			meta.SHA256 = packageinventory.Digest(items)
			require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, items, meta, meta.SHA256))
			health := executionHealth
			health.ObservedAt = time.Now().UTC()
			if scenario == "service_unknown" {
				health.ServiceHealth = "unknown"
			}
			if scenario == "reboot_required" {
				health.RebootRequired = &yes
			}
			require.NoError(t, s.RecordPatchHealth(ctx, node.ID, health))
			generation := "lifecycle-" + uuid.NewString()
			require.NoError(t, s.SetVulnerabilityPublication(ctx, generation))
			expires := time.Now().UTC().Add(time.Hour)
			if scenario == "feed_expiry_at_commit" {
				expires = time.Now().UTC().Add(-time.Second)
			}
			covered := []string{finding.CVEID}
			if scenario == "narrower_feed" {
				covered = []string{"CVE-2026-OTHER"}
			}
			scope := offlinebundle.VerificationScope{Name: finding.PackageName, Source: finding.PackageSource, Arch: finding.Arch, CVEID: finding.CVEID}
			fixed := []offlinebundle.VerificationScope{scope}
			var matches []VulnerabilityFinding
			var resolutions []ReconcileVulnerabilityFindingsParams
			if scenario == "still_vulnerable" {
				fixed = nil
				finding.LastSeenAt = meta.ObservedAt
				matches = []VulnerabilityFinding{finding}
			} else if scenario != "removed" && scenario != "narrower_feed" {
				resolutions = []ReconcileVulnerabilityFindingsParams{{TenantID: tenant.ID, NodeID: node.ID, EvidenceSource: finding.EvidenceSource, CVEIDs: []string{finding.CVEID}, PackageScopes: []VulnerabilityFindingPackageScope{{PackageName: finding.PackageName, InstalledVersion: "2", PackageSource: finding.PackageSource, Arch: finding.Arch}}, ResolveHistoricalVersions: true, ObservedAt: meta.ObservedAt}}
			}
			var inventory []offlinebundle.PackageInventory
			for _, p := range items {
				inventory = append(inventory, offlinebundle.PackageInventory{Name: p.Name, Version: p.Version, Source: p.Source, Arch: p.Arch})
			}
			raw, err := json.Marshal(map[string]any{"native_snapshot": meta, "assessed_advisory_ids": covered, "verified_fixed_scopes": fixed, "findings_updated": true, "feed_generation": generation, "feeds": []offlinebundle.AssessmentFeedEvidence{{Name: "signed-fixture", ExpiresAt: &expires}}})
			require.NoError(t, err)
			a := NodeAssessment{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, StartedAt: meta.ObservedAt, CompletedAt: time.Now().UTC(), Status: "partial", InventoryGeneration: offlinebundle.VulnerabilityInventoryGeneration(inventory), Evidence: raw}
			if scenario == "event_rollback" || scenario == "event_connection_loss" {
				fault := "RAISE EXCEPTION 'verification event fault'"
				if scenario == "event_connection_loss" {
					fault = "PERFORM pg_terminate_backend(pg_backend_pid())"
				}
				_, err = s.db.ExecContext(ctx, `CREATE FUNCTION verification_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN `+fault+`; RETURN NEW; END $$;CREATE TRIGGER verification_fault BEFORE INSERT ON patch_verification_events FOR EACH ROW EXECUTE FUNCTION verification_fault()`)
				require.NoError(t, err)
				err = s.ApplyNodeAssessment(ctx, a, matches, resolutions)
				require.Error(t, err)
				_, err = s.db.ExecContext(ctx, `DROP TRIGGER verification_fault ON patch_verification_events;DROP FUNCTION verification_fault()`)
				require.NoError(t, err)
				var count int
				require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM node_assessments WHERE id=$1`, a.ID).Scan(&count))
				require.Zero(t, count)
			}
			err = s.ApplyNodeAssessment(ctx, a, matches, resolutions)
			if scenario == "feed_expiry_at_commit" {
				require.ErrorIs(t, err, ErrAssessmentSuperseded)
				return
			}
			require.NoError(t, err)
			saved, err := s.ListNodePatchVerifications(ctx, tenant.ID, node.ID)
			require.NoError(t, err)
			require.Len(t, saved, 1)
			want := "verified_fixed"
			switch scenario {
			case "removed":
				want = "verified_removed"
			case "still_vulnerable":
				want = "verification_failed"
			case "service_unknown", "narrower_feed":
				want = "verification_unknown"
			case "reboot_required":
				want = "verification_pending"
			case "execution_failed":
				want = "execution_failed"
			}
			require.Equal(t, want, saved[0].CurrentState)
			require.Equal(t, want == "verified_fixed" || want == "verified_removed", saved[0].PromotionEligible)
			wrongTenant, err := s.ListNodePatchVerifications(ctx, uuid.New(), node.ID)
			require.NoError(t, err)
			require.Empty(t, wrongTenant)
			if scenario == "inventory_changed_after_verification" {
				require.NoError(t, s.ReplaceNodeAppDependencies(ctx, node.ID, tenant.ID, []NodeAppDependency{{Name: "later", Version: "1", Ecosystem: "npm"}}))
				ready, err = s.PatchVerificationAllowsPromotion(ctx, *state.JobID)
				require.NoError(t, err)
				require.False(t, ready, "later inventory revisions invalidate promotion before reassessment")
			}
			if scenario == "invalid_health_after_verification" {
				health.ObservedAt = time.Now().Add(time.Hour)
				require.NoError(t, s.RecordPatchHealth(ctx, node.ID, health))
				ready, err = s.PatchVerificationAllowsPromotion(ctx, *state.JobID)
				require.NoError(t, err)
				require.False(t, ready, "invalid health must not preserve a prior green gate")
			}
			if scenario == "duplicate_and_delayed" {
				require.Error(t, s.ApplyNodeAssessment(ctx, a, matches, resolutions))
				older := a
				older.ID = uuid.New()
				older.StartedAt = a.StartedAt.Add(-time.Second)
				require.ErrorIs(t, s.ApplyNodeAssessment(ctx, older, matches, resolutions), ErrAssessmentSuperseded)
				var events int
				require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM patch_verification_events WHERE job_id=$1`, *state.JobID).Scan(&events))
				require.Equal(t, 1, events)
			}
			if saved[0].PromotionEligible {
				require.NoError(t, s.SetVulnerabilityPublication(ctx, "replacement-"+uuid.NewString()))
				ready, err = s.PatchVerificationAllowsPromotion(ctx, *state.JobID)
				require.NoError(t, err)
				require.False(t, ready, "publication replacement must invalidate promotion")
			}
		})
	}
}
