package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOrganizationExecutionRequiresIndependentCISOAndFreshBinding(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	f := makeAutomationFixture(t, s)
	reviewer, e := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", DisplayName: "Isolated security reviewer", Password: uuid.NewString(), Provider: AuthProviderLocal, Roles: []string{"ciso"}})
	require.NoError(t, e)
	urgency := patchgovernance.Policy{Name: "Isolated urgency", Owner: "Service owner", Mode: "recommend_only", RetentionDays: 30, Rules: []patchgovernance.Rule{{Name: "High", Severity: "high", Exposure: "any", Importance: "any", Priority: "high", DueHours: 24}}}
	_, e = s.SavePatchGovernancePolicy(ctx, f.tenant, 0, urgency, f.actor.String())
	require.NoError(t, e)
	now := time.Now().UTC()
	p := patchgovernance.OrganizationPolicy{Name: "Independent CISO workflow", OwnerID: f.actor, ApproverID: reviewer.ID, Responsibilities: "Admin prepares; CISO reviews", Scope: "One isolated canary", CustomerAuthorization: "Isolated test only", Exclusions: "All production systems", Reason: "Exercise actual authorization gates", EffectiveAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	_, e = s.SavePatchOrganizationPolicy(ctx, f.tenant, f.actor, 0, "draft", p)
	require.NoError(t, e)
	_, e = s.DispatchPatchJob(ctx, f.params)
	require.ErrorContains(t, e, "independent approval")
	_, e = s.QueuePatchAutomationRun(ctx, f.tenant, f.approval, f.actor, f.policy.Revision, time.Time{}, "individual_approval")
	require.ErrorContains(t, e, "independent approval")
	_, e = s.SavePatchOrganizationPolicy(ctx, f.tenant, f.actor, 1, "submit", p)
	require.NoError(t, e)
	_, e = s.SavePatchOrganizationPolicy(ctx, f.tenant, f.actor, 2, "approve", p)
	require.Error(t, e)
	approved, e := s.SavePatchOrganizationPolicy(ctx, f.tenant, reviewer.ID, 2, "approve", p)
	require.NoError(t, e)
	require.NotEmpty(t, approved.BoundControls)
	for _, scopeCase := range []string{"wrong window", "unapproved package", "wrong node", "offline path", "excluded dependency"} {
		t.Run(scopeCase, func(t *testing.T) {
			tx, err := s.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			var scoped map[string]any
			require.NoError(t, json.Unmarshal(f.params.Payload, &scoped))
			node, mode := f.node, "direct"
			switch scopeCase {
			case "wrong window":
				scoped["patch_window"].(map[string]any)["id"] = uuid.NewString()
			case "unapproved package":
				scoped["package_allowlist"] = []string{"not-approved"}
			case "wrong node":
				node = uuid.New()
			case "offline path":
				mode = "airgapped"
			case "excluded dependency":
				_, err = tx.ExecContext(ctx, `UPDATE patch_review_operations SET document=jsonb_set(document,'{automation,exclusions}','["lib-blocked"]') WHERE tenant_id=$1`, f.tenant)
				require.NoError(t, err)
				plan := scoped["online_plan"].(map[string]any)
				plan["resolved"] = append(plan["resolved"].([]any), map[string]any{"name": "lib-blocked", "version": "2"})
			}
			raw, err := json.Marshal(scoped)
			require.NoError(t, err)
			require.Error(t, checkPatchOrganizationScopeTx(ctx, tx, f.tenant, node, mode, raw, 3))
		})
	}
	for _, scenario := range []struct {
		name   string
		mutate func(*sql.Tx) error
	}{
		{"expired", func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE patch_organization_policy SET document=jsonb_set(document,'{expires_at}',to_jsonb($2::text)) WHERE tenant_id=$1`, f.tenant, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano))
			return err
		}},
		{"future", func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE patch_organization_policy SET document=jsonb_set(document,'{effective_at}',to_jsonb($2::text)) WHERE tenant_id=$1`, f.tenant, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
			return err
		}},
		{"authentication owner requires execution qualification", func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE users SET external_id=$2 WHERE id=$1`, f.actor, "auth:v1:"+uuid.NewString())
			return err
		}},
		{"disabled owner", func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE users SET disabled_at=NOW() WHERE id=$1`, f.actor)
			return err
		}},
		{"directory owner requires execution qualification", func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE users SET auth_provider='ldap' WHERE id=$1`, f.actor)
			return err
		}},
		{"directory approver requires execution qualification", func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE users SET auth_provider='ldap' WHERE id=$1`, reviewer.ID)
			return err
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tx, err := s.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			require.NoError(t, scenario.mutate(tx))
			_, err = checkPatchOrganizationTx(ctx, tx, f.tenant, nil)
			require.Error(t, err)
			if scenario.name == "directory owner requires execution qualification" || scenario.name == "directory approver requires execution qualification" {
				require.ErrorContains(t, err, "directory-backed organizational owner or approver")
			}
		})
	}
	_, e = s.SavePatchOrganizationPolicy(ctx, f.tenant, reviewer.ID, 3, "draft", p)
	require.Error(t, e, "CISO cannot edit policy")
	_, e = s.DispatchPatchJob(ctx, f.params)
	require.ErrorContains(t, e, "not bound")
	dep, e := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: f.tenant, Mode: "direct", TargetNodeCount: 1, Summary: map[string]any{"online_plan": f.plan}})
	require.NoError(t, e)
	approval, e := s.CreatePatchApproval(ctx, CreatePatchApprovalParams{TenantID: f.tenant, NodeID: f.node, DeploymentID: dep.ID, Mode: "direct", WindowID: &f.window, ExpiresAt: now.Add(time.Hour), PolicyHash: f.params.PolicyHash})
	require.NoError(t, e)
	params := f.params
	params.DeploymentID = dep.ID
	params.ApprovalID = approval.ID
	state, e := s.DispatchPatchJob(ctx, params)
	require.NoError(t, e)
	require.NotNil(t, state.JobID)
	require.NoError(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID))
	// Exercise the actual dispatch and start boundaries, not only the helper.
	// The database is a disposable fixture and the provider is restored below.
	_, e = s.db.ExecContext(ctx, `UPDATE users SET auth_provider='ldap' WHERE id=$1`, reviewer.ID)
	require.NoError(t, e)
	t.Cleanup(func() {
		_, _ = s.db.ExecContext(context.Background(), `UPDATE users SET auth_provider='local' WHERE id=$1`, reviewer.ID)
	})
	_, e = s.DispatchPatchJob(ctx, params)
	require.ErrorContains(t, e, "directory-backed organizational owner or approver")
	require.ErrorContains(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID), "directory-backed organizational owner or approver")
	_, e = s.db.ExecContext(ctx, `UPDATE users SET auth_provider='local' WHERE id=$1`, reviewer.ID)
	require.NoError(t, e)
	job, e := s.GetJob(ctx, *state.JobID)
	require.NoError(t, e)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(job.Payload, &payload))
	require.EqualValues(t, 3, payload["organization_policy_revision"])
	// Changing saved controls immediately invalidates the old authorization.
	_, e = s.SavePatchReviewOperations(ctx, f.tenant, f.policy.Revision, f.policy.Policy, f.actor.String())
	require.NoError(t, e)
	require.ErrorContains(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID), "no longer matches")
	_, e = s.SavePatchOrganizationPolicy(ctx, f.tenant, f.actor, 3, "draft", p)
	require.NoError(t, e)
	_, e = s.SavePatchOrganizationPolicy(ctx, f.tenant, f.actor, 4, "submit", p)
	require.NoError(t, e)
	_, e = s.SavePatchOrganizationPolicy(ctx, f.tenant, reviewer.ID, 5, "approve", p)
	require.NoError(t, e)
	require.ErrorContains(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID), "changed")
	_, e = s.SavePatchOrganizationPolicy(ctx, f.tenant, f.actor, 6, "withdraw", p)
	require.NoError(t, e)
	require.Error(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID))
	var jobs int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, f.tenant).Scan(&jobs))
	require.Equal(t, 1, jobs, "withdrawal must not create retries")
}

func TestOrganizationScopedDelegationStopsDispatchAndStart(t *testing.T) {
	for _, outcome := range []string{"revoked", "expired"} {
		t.Run(outcome, func(t *testing.T) {
			s := phase2SnapshotTestStore(t)
			ctx := context.Background()
			f := makeAutomationFixture(t, s)
			reviewer, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", DisplayName: "Scoped reviewer", Password: uuid.NewString(), Provider: AuthProviderLocal, Roles: []string{"viewer"}})
			require.NoError(t, err)
			urgency := patchgovernance.Policy{Name: "Scoped delegation test", Owner: "Service owner", Mode: "recommend_only", RetentionDays: 30, Rules: []patchgovernance.Rule{{Name: "High", Severity: "high", Exposure: "any", Importance: "any", Priority: "high", DueHours: 24}}}
			_, err = s.SavePatchGovernancePolicy(ctx, f.tenant, 0, urgency, f.actor.String())
			require.NoError(t, err)
			expires := time.Now().UTC().Add(time.Hour)
			if outcome == "expired" {
				expires = time.Now().UTC().Add(10 * time.Second)
			}
			grant := TenantRoleControlChange{UserID: reviewer.ID, Role: "ciso", Action: "grant", ExpiresAt: expires, Reason: "Isolated scoped reviewer execution-boundary test"}
			require.NoError(t, s.SaveTenantRoleControl(ctx, f.tenant, f.actor, grant))
			session, err := s.IssueSession(ctx, reviewer.ID, time.Hour, "delegation-test", "127.0.0.1")
			require.NoError(t, err)
			now := time.Now().UTC()
			p := patchgovernance.OrganizationPolicy{Name: "Scoped reviewer approval", OwnerID: f.actor, ApproverID: reviewer.ID, Responsibilities: "Admin prepares; scoped reviewer approves", Scope: "Isolated fixture only", CustomerAuthorization: "Disposable database test", Exclusions: "Production excluded", Reason: "Verify current delegated authority at every gate", EffectiveAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
			for i, step := range []string{"draft", "submit", "approve"} {
				actor := f.actor
				if step == "approve" {
					actor = reviewer.ID
				}
				_, err = s.SavePatchOrganizationPolicy(ctx, f.tenant, actor, int64(i), step, p)
				require.NoError(t, err)
			}
			dep, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: f.tenant, Mode: "direct", TargetNodeCount: 1, Summary: map[string]any{"online_plan": f.plan}})
			require.NoError(t, err)
			ap := CreatePatchApprovalParams{TenantID: f.tenant, NodeID: f.node, DeploymentID: dep.ID, Mode: "direct", WindowID: &f.window, ExpiresAt: now.Add(time.Hour), PolicyHash: f.params.PolicyHash}
			approval, err := s.CreatePatchApproval(ctx, ap)
			require.NoError(t, err)
			params := f.params
			params.DeploymentID, params.ApprovalID = dep.ID, approval.ID
			state, err := s.DispatchPatchJob(ctx, params)
			require.NoError(t, err)
			require.NotNil(t, state.JobID)
			require.NoError(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID))
			if outcome == "revoked" {
				grant.Action, grant.ExpectedRevision = "revoke", 1
				require.NoError(t, s.SaveTenantRoleControl(ctx, f.tenant, f.actor, grant))
			} else {
				// Let the actual stored deadline pass; do not edit the role or clock.
				if remaining := time.Until(expires.Add(100 * time.Millisecond)); remaining > 0 {
					time.Sleep(remaining)
				}
			}
			_, account, err := s.ValidateSessionToken(ctx, session.Token)
			require.NoError(t, err, "the existing session may remain valid without the delegated privilege")
			roles, err := s.ListUserRoles(ctx, account.ID)
			require.NoError(t, err)
			require.Contains(t, roles, "viewer")
			require.NotContains(t, roles, "ciso")
			_, err = s.CreatePatchApproval(ctx, ap)
			require.ErrorContains(t, err, "current tenant role")
			_, err = s.DispatchPatchJob(ctx, params)
			require.ErrorContains(t, err, "current tenant role")
			require.ErrorContains(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID), "current tenant role")
			var jobs int
			require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, f.tenant).Scan(&jobs))
			require.Equal(t, 1, jobs, "lost delegation cannot create a retry or additional job")
		})
	}
}
