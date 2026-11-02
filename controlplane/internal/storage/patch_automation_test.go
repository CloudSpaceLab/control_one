package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
	"time"
)

type automationFixture struct {
	tenant, node, actor, approval, window uuid.UUID
	policy                                PatchReviewOperations
	plan                                  patchsupport.OnlinePlan
	params                                DispatchPatchJobParams
}

func makeAutomationFixture(t *testing.T, s *Store) automationFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	tenant, e := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, e)
	user, e := s.EnsureUser(ctx, uuid.NewString(), uuid.NewString()+"@example.invalid", "Automation fixture")
	require.NoError(t, e)
	require.NoError(t, s.SetUserRoles(ctx, user.ID, []string{"admin"}))
	nodeID := uuid.New()
	transport := patchsupport.OnlineTransport{NodeID: nodeID.String(), TenantID: tenant.ID.String(), Mode: "direct"}
	plan := patchsupport.OnlinePlan{Version: 1, NodeID: nodeID.String(), Mode: "direct", TransportContext: transport.Digest(), InstalledStateSHA256: strings.Repeat("b", 64), InputsSHA256: strings.Repeat("c", 64), ManifestSHA256: strings.Repeat("d", 64), Requested: []string{"liba"}, Resolved: []patchsupport.ExactPackage{{Name: "liba", Version: "2"}}, Review: patchsupport.OfflineReview{PreparedAt: now, ExpiresAt: now.Add(45 * time.Minute), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []patchsupport.PlanChange{{Name: "liba", InstalledVersion: "1", ProposedVersion: "2", Role: "requested"}}}}
	plan.SHA256 = plan.Digest()
	plan.ArtifactsSHA256 = strings.Repeat("e", 64)
	plan.SHA256 = plan.Digest()
	runtime := patchsupport.Runtime{OS: "linux", Manager: "apt", OnlinePlanVersion: 1, CalendarWindowStartCheck: true}
	node, e := s.CreateNode(ctx, &Node{ID: nodeID, TenantID: tenant.ID, Hostname: uuid.NewString(), OS: sql.NullString{String: "ubuntu 24.04", Valid: true}, Arch: sql.NullString{String: "amd64", Valid: true}, State: NodeStateActive, Labels: map[string]any{"patch.runtime": patchsupport.Evidence{Runtime: runtime, ObservedAt: now}, "patch.online_plan": map[string]any{"status": "ready", "plan": plan, "job_id": uuid.NewString(), "observed_at": now}}})
	require.NoError(t, e)
	window, e := s.CreateMaintenanceWindow(ctx, MaintenanceWindow{TenantID: tenant.ID, Name: "Isolated scheduler fixture", NodeIDs: []uuid.UUID{node.ID}, OpensAt: now.Add(-time.Minute), ClosesAt: now.Add(time.Hour), AllowRepos: []string{}})
	require.NoError(t, e)
	_, e = s.db.ExecContext(ctx, `UPDATE maintenance_windows SET status='open' WHERE id=$1`, window.ID)
	require.NoError(t, e)
	_, e = s.UpsertNodePatchConfig(ctx, NodePatchConfig{NodeID: node.ID, Mode: "direct", WindowID: &window.ID})
	require.NoError(t, e)
	items := []packageinventory.Item{{Name: "liba", Version: "1", Source: "dpkg", Arch: "amd64"}}
	meta := packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now, SHA256: packageinventory.Digest(items), Count: 1}
	require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, items, meta, meta.SHA256))
	generation := "automation-" + uuid.NewString()
	require.NoError(t, s.SetVulnerabilityPublication(ctx, generation))
	var inventoryRevision int64
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT inventory_revision FROM nodes WHERE id=$1`, node.ID).Scan(&inventoryRevision))
	raw, _ := json.Marshal(map[string]any{"inventory_revision": inventoryRevision, "native_snapshot": meta, "findings_updated": true, "feed_generation": generation, "feeds": []map[string]any{{"expires_at": now.Add(time.Hour)}}})
	require.NoError(t, s.RecordNodeAssessment(ctx, NodeAssessment{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, StartedAt: now, CompletedAt: time.Now(), Status: "partial", InventoryGeneration: "fixture", Evidence: raw}))
	no := false
	require.NoError(t, s.RecordPatchHealth(ctx, node.ID, packageinventory.Health{ObservedAt: now, BootID: uuid.NewString(), RebootRequired: &no, PackageAudit: "healthy", ServiceHealth: "healthy", ServicesComplete: true, ConsumersComplete: true}))
	o := patchgovernance.DefaultOperations()
	o.Owner = "Isolated test owner"
	o.Reason = "Scheduler transaction fixture"
	end := now.Add(time.Hour)
	o.Automation = patchgovernance.AutomationPolicy{Enabled: true, Mode: "approved_scheduled", NodeIDs: []string{node.ID.String()}, CanaryNodeIDs: []string{node.ID.String()}, Packages: []string{"liba"}, Exclusions: []string{}, WindowID: window.ID.String(), Transport: "direct", MaxWaveSize: 1, ObservationMinutes: 1, HealthProfile: patchgovernance.AutomationHealthProfile, ValidUntil: &end, RequireApproval: true, RequireExactPlan: true, RequireFreshAssessment: true, RequireHealth: true, RequireRebootVerification: true, StopOnFailure: true}
	ops, e := s.SavePatchReviewOperations(ctx, tenant.ID, 0, o, user.ID.String())
	require.NoError(t, e)
	dep, e := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: tenant.ID, Mode: "direct", TargetNodeCount: 1, Summary: map[string]any{"online_plan": plan}})
	require.NoError(t, e)
	approval, e := s.CreatePatchApproval(ctx, CreatePatchApprovalParams{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: dep.ID, Mode: "direct", WindowID: &window.ID, ExpiresAt: now.Add(time.Hour), PolicyHash: "isolated-scheduled-plan"})
	require.NoError(t, e)
	payload, _ := json.Marshal(map[string]any{"online_plan": plan, "package_allowlist": plan.Requested, "post_patch_rescan": true, "patch_runtime": runtime, "patch_window": patchsupport.Window{ID: window.ID.String(), OpensAt: window.OpensAt, ClosesAt: window.ClosesAt}})
	return automationFixture{tenant: tenant.ID, node: node.ID, actor: user.ID, approval: approval.ID, window: window.ID, policy: ops, plan: plan, params: DispatchPatchJobParams{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: dep.ID, Mode: "direct", ApprovalID: approval.ID, ApproverID: user.ID, PolicyHash: approval.PolicyHash, Payload: payload, Plan: CreateActionPlanParams{State: ActionPlanStateQueued, Diff: map[string]any{"online_plan": plan}}}}
}
func bindAutomationFixture(t *testing.T, s *Store, f automationFixture) (PatchAutomationRun, DispatchPatchJobParams) {
	t.Helper()
	r, e := s.QueuePatchAutomationRun(context.Background(), f.tenant, f.approval, f.actor, f.policy.Revision, time.Time{}, "individual_approval")
	require.NoError(t, e)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(f.params.Payload, &payload))
	payload["automation_run_id"] = r.ID.String()
	p := f.params
	p.Payload, e = json.Marshal(payload)
	require.NoError(t, e)
	p.Plan.Diff = map[string]any{"online_plan": f.plan, "automation_run_id": r.ID.String()}
	return r, p
}

func addAutomationFollower(t *testing.T, s *Store, f automationFixture) automationFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	out := f
	out.node = uuid.New()
	out.plan = f.plan
	out.plan.NodeID = out.node.String()
	out.plan.TransportContext = (patchsupport.OnlineTransport{TenantID: f.tenant.String(), NodeID: out.node.String(), Mode: "direct"}).Digest()
	out.plan.SHA256 = out.plan.Digest()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(f.params.Payload, &payload))
	runtime := payload["patch_runtime"]
	_, e := s.CreateNode(ctx, &Node{ID: out.node, TenantID: f.tenant, Hostname: uuid.NewString(), OS: sql.NullString{String: "ubuntu 24.04", Valid: true}, Arch: sql.NullString{String: "amd64", Valid: true}, State: NodeStateActive, Labels: map[string]any{"patch.runtime": map[string]any{"os": "linux", "manager": "apt", "online_plan_version": 1, "calendar_window_start_check": true, "observed_at": now}, "patch.online_plan": map[string]any{"status": "ready", "plan": out.plan, "job_id": uuid.NewString(), "observed_at": now}}})
	require.NoError(t, e)
	_, e = s.db.ExecContext(ctx, `UPDATE maintenance_windows SET node_ids=array_append(node_ids,$2) WHERE id=$1`, f.window, out.node)
	require.NoError(t, e)
	_, e = s.UpsertNodePatchConfig(ctx, NodePatchConfig{NodeID: out.node, Mode: "direct", WindowID: &f.window})
	require.NoError(t, e)
	items := []packageinventory.Item{{Name: "liba", Version: "1", Source: "dpkg", Arch: "amd64"}}
	meta := packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now, SHA256: packageinventory.Digest(items), Count: 1}
	require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, out.node, items, meta, meta.SHA256))
	var gen string
	var rev int64
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT generation FROM vulnerability_publication WHERE id=1`).Scan(&gen))
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT inventory_revision FROM nodes WHERE id=$1`, out.node).Scan(&rev))
	raw, _ := json.Marshal(map[string]any{"inventory_revision": rev, "native_snapshot": meta, "findings_updated": true, "feed_generation": gen, "feeds": []map[string]any{{"expires_at": now.Add(time.Hour)}}})
	require.NoError(t, s.RecordNodeAssessment(ctx, NodeAssessment{ID: uuid.New(), TenantID: f.tenant, NodeID: out.node, StartedAt: now, CompletedAt: time.Now(), Status: "partial", InventoryGeneration: "follower", Evidence: raw}))
	health, e := s.GetNodePatchHealth(ctx, f.node)
	require.NoError(t, e)
	require.NoError(t, s.RecordPatchHealth(ctx, out.node, *health))
	policy := f.policy.Policy
	policy.Automation.Mode = "automatic"
	policy.Automation.RequireApproval = false
	policy.Automation.NodeIDs = append(append([]string{}, policy.Automation.NodeIDs...), out.node.String())
	saved, e := s.SavePatchReviewOperations(ctx, f.tenant, f.policy.Revision, policy, f.actor.String())
	require.NoError(t, e)
	out.policy = saved
	d, e := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: f.tenant, Mode: "direct", TargetNodeCount: 1, Summary: map[string]any{"online_plan": out.plan}})
	require.NoError(t, e)
	a, e := s.CreatePatchApproval(ctx, CreatePatchApprovalParams{TenantID: f.tenant, NodeID: out.node, DeploymentID: d.ID, Mode: "direct", WindowID: &f.window, ExpiresAt: out.plan.Review.ExpiresAt, PolicyHash: "isolated-follower-exact"})
	require.NoError(t, e)
	out.approval = a.ID
	payload["online_plan"] = out.plan
	payload["patch_runtime"] = runtime
	out.params = f.params
	out.params.NodeID = out.node
	out.params.DeploymentID = d.ID
	out.params.ApprovalID = a.ID
	out.params.PolicyHash = a.PolicyHash
	out.params.Payload, e = json.Marshal(payload)
	require.NoError(t, e)
	out.params.Plan = CreateActionPlanParams{State: ActionPlanStateQueued, Diff: map[string]any{"online_plan": out.plan}}
	return out
}

func verifyAutomationCanary(t *testing.T, s *Store, f automationFixture, job uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	health, e := s.GetNodePatchHealth(ctx, f.node)
	require.NoError(t, e)
	rawHealth, _ := json.Marshal(health)
	_, e = s.CompletePatchJob(ctx, CompletePatchJobParams{NodeID: f.node, JobID: job, Action: "patch.deploy_direct", Succeeded: true, Health: rawHealth})
	require.NoError(t, e)
	items := []packageinventory.Item{{Name: "liba", Version: "2", Source: "dpkg", Arch: "amd64"}}
	now := time.Now().UTC()
	meta := packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now, SHA256: packageinventory.Digest(items), Count: 1}
	require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, f.node, items, meta, meta.SHA256))
	health.ObservedAt = time.Now().UTC()
	require.NoError(t, s.RecordPatchHealth(ctx, f.node, *health))
	var gen string
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT generation FROM vulnerability_publication WHERE id=1`).Scan(&gen))
	end := now.Add(time.Hour)
	raw, _ := json.Marshal(map[string]any{"native_snapshot": meta, "assessed_advisory_ids": []string{"CVE-AUTOMATION-FIXTURE"}, "verified_fixed_scopes": []offlinebundle.VerificationScope{{Name: "liba", Source: "dpkg", Arch: "amd64", CVEID: "CVE-AUTOMATION-FIXTURE"}}, "findings_updated": true, "feed_generation": gen, "feeds": []offlinebundle.AssessmentFeedEvidence{{Name: "isolated-signed-feed", ExpiresAt: &end}}})
	a := NodeAssessment{ID: uuid.New(), TenantID: f.tenant, NodeID: f.node, StartedAt: now, CompletedAt: time.Now(), Status: "partial", InventoryGeneration: offlinebundle.VulnerabilityInventoryGeneration([]offlinebundle.PackageInventory{{Name: "liba", Version: "2", Source: "dpkg", Arch: "amd64", Platform: "ubuntu 24.04"}}), Evidence: raw}
	require.NoError(t, s.ApplyNodeAssessment(ctx, a, nil, []ReconcileVulnerabilityFindingsParams{{TenantID: f.tenant, NodeID: f.node, EvidenceSource: "offline_vulnerability_feed", CVEIDs: []string{"CVE-AUTOMATION-FIXTURE"}, PackageScopes: []VulnerabilityFindingPackageScope{{PackageName: "liba", InstalledVersion: "2", PackageSource: "dpkg", Arch: "amd64"}}, ResolveHistoricalVersions: true, ObservedAt: now}}))
	ok, e := s.PatchVerificationAllowsPromotion(ctx, job)
	require.NoError(t, e)
	require.True(t, ok)
}

func TestAutomationCanaryAndAutomaticFollowerDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	canary := makeAutomationFixture(t, s)
	follower := addAutomationFollower(t, s, canary)
	canary.policy = follower.policy
	now := time.Now().UTC()
	require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{{ID: uuid.New(), TenantID: canary.tenant, NodeID: canary.node, PackageName: "liba", InstalledVersion: "1", PackageSource: "dpkg", Arch: "amd64", CVEID: "CVE-AUTOMATION-FIXTURE", FixedVersion: "2", EvidenceSource: "offline_vulnerability_feed", FirstSeenAt: now.Add(-time.Hour), LastSeenAt: now}}))
	require.NoError(t, s.DiscoverPatchAutomationRuns(ctx))
	runs, e := s.ListPatchAutomationRuns(ctx, canary.tenant)
	require.NoError(t, e)
	require.Len(t, runs, 1)
	auto := runs[0]
	require.Equal(t, "tenant_policy", auto.Authority)
	require.Equal(t, follower.approval, auto.ApprovalID)
	_, e = s.CheckPatchAutomationRun(ctx, canary.tenant, auto.ID)
	require.ErrorContains(t, e, "canary")
	_, params := bindAutomationFixture(t, s, canary)
	state, e := s.DispatchPatchJob(ctx, params)
	require.NoError(t, e)
	verifyAutomationCanary(t, s, canary, *state.JobID)
	_, e = s.CheckPatchAutomationRun(ctx, canary.tenant, auto.ID)
	require.ErrorContains(t, e, "observation")
	// Time-only fixture adjustment avoids a blocking wall-clock wait. The real
	// verification was computed above by authoritative inventory/assessment.
	_, e = s.db.ExecContext(ctx, `UPDATE patch_verifications SET execution_completed_at=NOW()-INTERVAL '2 minutes' WHERE job_id=$1`, *state.JobID)
	require.NoError(t, e)
	_, e = s.CheckPatchAutomationRun(ctx, canary.tenant, auto.ID)
	require.NoError(t, e)
	// Version strings alone are insufficient. A changed acquired archive set
	// must invalidate both a future dispatch and an agent start authorization.
	original := follower.plan
	follower.plan.ArtifactsSHA256 = strings.Repeat("f", 64)
	follower.plan.SHA256 = follower.plan.Digest()
	changed, _ := json.Marshal(follower.plan)
	_, e = s.db.ExecContext(ctx, `UPDATE patch_deployments SET summary=jsonb_set(summary,'{online_plan}',$2::jsonb) WHERE id=$1`, follower.params.DeploymentID, changed)
	require.NoError(t, e)
	_, e = s.CheckPatchAutomationRun(ctx, canary.tenant, auto.ID)
	require.ErrorContains(t, e, "dependency closure")
	follower.plan = original
	restored, _ := json.Marshal(original)
	_, e = s.db.ExecContext(ctx, `UPDATE patch_deployments SET summary=jsonb_set(summary,'{online_plan}',$2::jsonb) WHERE id=$1`, follower.params.DeploymentID, restored)
	require.NoError(t, e)
	require.NoError(t, s.DiscoverPatchAutomationRuns(ctx))
	again, e := s.ListPatchAutomationRuns(ctx, canary.tenant)
	require.NoError(t, e)
	require.Len(t, again, 2, "discovery must not duplicate canary or follower authorization")
	var payload map[string]any
	require.NoError(t, json.Unmarshal(follower.params.Payload, &payload))
	payload["automation_run_id"] = auto.ID.String()
	follower.params.Payload, e = json.Marshal(payload)
	require.NoError(t, e)
	follower.params.Plan.Diff["automation_run_id"] = auto.ID.String()
	next, e := s.DispatchPatchJob(ctx, follower.params)
	require.NoError(t, e)
	require.NotNil(t, next.JobID)
	require.NoError(t, s.CheckPatchAutomationExecution(ctx, follower.tenant, auto.ID, *next.JobID))
	_, e = s.db.ExecContext(ctx, `UPDATE node_patch_health SET evidence=jsonb_set(evidence,'{service_health}','"unknown"') WHERE node_id=$1`, canary.node)
	require.NoError(t, e)
	require.Error(t, s.CheckPatchAutomationExecution(ctx, follower.tenant, auto.ID, *next.JobID))
}

func TestAutomationScheduledAtomicDispatchDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	f := makeAutomationFixture(t, s)
	r, p := bindAutomationFixture(t, s, f)
	require.True(t, r.ExpiresAt.Before(f.policy.Policy.Automation.ValidUntil.Add(time.Second)))
	_, e := s.CheckPatchAutomationRun(ctx, f.tenant, r.ID)
	require.NoError(t, e)
	_, e = s.QueuePatchAutomationRun(ctx, f.tenant, f.approval, f.actor, f.policy.Revision, time.Time{}, "individual_approval")
	require.Error(t, e)
	_, e = s.db.ExecContext(ctx, `CREATE FUNCTION automation_fixture_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='automation_dispatched' THEN RAISE EXCEPTION 'isolated automation event fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER automation_fixture_fault BEFORE INSERT ON patch_governance_events FOR EACH ROW EXECUTE FUNCTION automation_fixture_fault()`)
	require.NoError(t, e)
	t.Cleanup(func() {
		_, _ = s.db.ExecContext(ctx, `DROP TRIGGER IF EXISTS automation_fixture_fault ON patch_governance_events; DROP FUNCTION IF EXISTS automation_fixture_fault()`)
	})
	_, e = s.DispatchPatchJob(ctx, p)
	require.Error(t, e)
	a, e := s.GetPatchApproval(ctx, f.approval)
	require.NoError(t, e)
	require.Equal(t, ApprovalStatusPending, a.Status)
	var jobs int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, f.tenant).Scan(&jobs))
	require.Zero(t, jobs)
	_, e = s.db.ExecContext(ctx, `DROP TRIGGER automation_fixture_fault ON patch_governance_events;DROP FUNCTION automation_fixture_fault()`)
	require.NoError(t, e)
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := s.DispatchPatchJob(ctx, p)
			errs <- err
			if err == nil && state.JobID != nil {
				ids <- *state.JobID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var job uuid.UUID
	for id := range ids {
		if job != uuid.Nil {
			require.Equal(t, job, id)
		}
		job = id
	}
	require.NotEqual(t, uuid.Nil, job)
	require.NoError(t, s.CheckPatchAutomationExecution(ctx, f.tenant, r.ID, job))
	f.policy.Policy.Automation.Enabled = false
	_, e = s.SavePatchReviewOperations(ctx, f.tenant, f.policy.Revision, f.policy.Policy, f.actor.String())
	require.NoError(t, e)
	require.Error(t, s.CheckPatchAutomationExecution(ctx, f.tenant, r.ID, job))
}
func TestAutomationScheduledRejectionsDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	for _, scenario := range []string{"not_due", "cancelled", "expired", "policy_changed", "unknown_health", "stale_health", "changed_feed", "closed_window", "revoked_role", "stripped_run", "automatic_canary"} {
		t.Run(scenario, func(t *testing.T) {
			f := makeAutomationFixture(t, s)
			if scenario == "automatic_canary" {
				f.policy.Policy.Automation.Mode = "automatic"
				f.policy.Policy.Automation.RequireApproval = false
				saved, e := s.SavePatchReviewOperations(ctx, f.tenant, f.policy.Revision, f.policy.Policy, f.actor.String())
				require.NoError(t, e)
				_, e = s.QueuePatchAutomationRun(ctx, f.tenant, f.approval, f.actor, saved.Revision, time.Time{}, "tenant_policy")
				require.ErrorContains(t, e, "individual")
				return
			}
			r, p := bindAutomationFixture(t, s, f)
			switch scenario {
			case "not_due":
				_, e := s.db.ExecContext(ctx, `UPDATE patch_automation_runs SET not_before=NOW()+INTERVAL '1 minute' WHERE id=$1`, r.ID)
				require.NoError(t, e)
			case "cancelled":
				require.NoError(t, s.CancelPatchAutomationRun(ctx, f.tenant, r.ID, f.actor.String()))
			case "expired":
				_, e := s.db.ExecContext(ctx, `UPDATE patch_automation_runs SET not_before=NOW()-INTERVAL '2 hours',expires_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, r.ID)
				require.NoError(t, e)
			case "policy_changed":
				f.policy.Policy.Automation.Enabled = false
				_, e := s.SavePatchReviewOperations(ctx, f.tenant, f.policy.Revision, f.policy.Policy, f.actor.String())
				require.NoError(t, e)
			case "unknown_health":
				_, e := s.db.ExecContext(ctx, `UPDATE node_patch_health SET evidence=jsonb_set(evidence,'{service_health}','"unknown"') WHERE node_id=$1`, f.node)
				require.NoError(t, e)
			case "stale_health":
				_, e := s.db.ExecContext(ctx, `UPDATE node_patch_health SET observed_at=NOW()-INTERVAL '10 minutes' WHERE node_id=$1`, f.node)
				require.NoError(t, e)
			case "changed_feed":
				require.NoError(t, s.SetVulnerabilityPublication(ctx, "changed-"+uuid.NewString()))
			case "closed_window":
				_, e := s.db.ExecContext(ctx, `UPDATE maintenance_windows SET status='closed' WHERE id=$1`, f.window)
				require.NoError(t, e)
			case "revoked_role":
				require.NoError(t, s.SetUserRoles(ctx, f.actor, []string{}))
			case "stripped_run":
				p.Payload = f.params.Payload
			}
			_, e := s.DispatchPatchJob(ctx, p)
			require.Error(t, e)
			var jobs int
			require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, f.tenant).Scan(&jobs))
			require.Zero(t, jobs)
			a, e := s.GetPatchApproval(ctx, f.approval)
			require.NoError(t, e)
			require.Equal(t, ApprovalStatusPending, a.Status)
		})
	}
}

func TestAutomationScheduleVersusImmediateApprovalDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	for attempt := 0; attempt < 4; attempt++ {
		f := makeAutomationFixture(t, s)
		start := make(chan struct{})
		queued := make(chan error, 1)
		dispatched := make(chan error, 1)
		go func() {
			<-start
			_, e := s.QueuePatchAutomationRun(ctx, f.tenant, f.approval, f.actor, f.policy.Revision, time.Time{}, "individual_approval")
			queued <- e
		}()
		go func() { <-start; _, e := s.DispatchPatchJob(ctx, f.params); dispatched <- e }()
		close(start)
		queueErr, dispatchErr := <-queued, <-dispatched
		require.True(t, queueErr != nil || dispatchErr != nil, "schedule and immediate approval must never both commit")
		var jobs, runs int
		require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, f.tenant).Scan(&jobs))
		require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM patch_automation_runs WHERE tenant_id=$1`, f.tenant).Scan(&runs))
		if queueErr == nil {
			require.Error(t, dispatchErr)
			require.Equal(t, 1, runs)
			require.Zero(t, jobs)
		} else {
			require.NoError(t, dispatchErr)
			require.Equal(t, 1, jobs)
			require.Zero(t, runs)
		}
	}
}
