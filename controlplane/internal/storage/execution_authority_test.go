package storage

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/executionauthority"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func executionAuthorityFixture(t *testing.T, s *Store) (*atomic.Bool, *atomic.Int32) {
	t.Helper()
	revoked := new(atomic.Bool)
	calls := new(atomic.Int32)
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request executionauthority.Request
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(executionauthority.Decision{Request: request, DecisionID: uuid.NewString(), Active: !revoked.Load(), Allowed: !revoked.Load(), Roles: []string{"admin"}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(8 * time.Second)})
	}))
	t.Cleanup(provider.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	credential := filepath.Join(dir, "credential")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: provider.Certificate().Raw}), 0600))
	require.NoError(t, os.WriteFile(credential, []byte(strings.Repeat("x", 32)), 0600))
	client, err := executionauthority.New(executionauthority.Config{Enabled: true, Endpoint: provider.URL, CAFile: ca, CredentialFile: credential}, "https://execution-fixture.invalid")
	require.NoError(t, err)
	s.executionAuthority = client
	return revoked, calls
}

func TestExecutionAuthorityScheduleDispatchStartRevocation(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	f := makeAutomationFixture(t, s)
	u, _, err := s.PersistAuthenticationRoles(ctx, "oidc:https://execution-fixture.invalid", uuid.NewString(), uuid.NewString()+"@example.invalid", "Authority contract fixture", []string{"admin"})
	require.NoError(t, err)
	f.actor = u.ID
	f.params.ApproverID = u.ID
	_, err = s.QueuePatchAutomationRun(ctx, f.tenant, f.approval, f.actor, f.policy.Revision, time.Time{}, "individual_approval")
	require.ErrorContains(t, err, "authority is unavailable")
	revoked, calls := executionAuthorityFixture(t, s)
	r, p := bindAutomationFixture(t, s, f)
	require.EqualValues(t, 1, calls.Load())
	_, err = s.CheckPatchAutomationRun(ctx, f.tenant, r.ID)
	require.NoError(t, err)
	revoked.Store(true)
	_, err = s.DispatchPatchJob(ctx, p)
	require.ErrorContains(t, err, "revoked")
	var count int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, f.tenant).Scan(&count))
	require.Zero(t, count)
	revoked.Store(false)
	// Fresh provider checks must preserve atomic duplicate dispatch handling.
	var group sync.WaitGroup
	states := make([]*NodePatchState, 2)
	dispatchErrors := make([]error, 2)
	for i := range states {
		group.Add(1)
		go func(i int) { defer group.Done(); states[i], dispatchErrors[i] = s.DispatchPatchJob(ctx, p) }(i)
	}
	group.Wait()
	for _, dispatchErr := range dispatchErrors {
		require.NoError(t, dispatchErr)
	}
	state := states[0]
	require.NotNil(t, state.JobID)
	require.Equal(t, *state.JobID, *states[1].JobID)
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, f.tenant).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, s.CheckPatchAutomationExecution(ctx, f.tenant, r.ID, *state.JobID))
	revoked.Store(true)
	require.ErrorContains(t, s.CheckPatchAutomationExecution(ctx, f.tenant, r.ID, *state.JobID), "revoked")
	revoked.Store(false)
	// A positive provider decision cannot manufacture a removed Control One grant.
	var subject string
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT subject FROM authentication_authority_bindings WHERE user_id=$1`, u.ID).Scan(&subject))
	_, _, err = s.PersistAuthenticationRoles(ctx, "oidc:https://execution-fixture.invalid", subject, u.Email.String, "", nil)
	require.NoError(t, err)
	require.ErrorContains(t, s.CheckPatchAutomationExecution(ctx, f.tenant, r.ID, *state.JobID), "scheduling authority unavailable")
	logs, total, err := s.ListAuditLogs(ctx, AuditLogFilter{GlobalIdentity: true, Action: "identity.execution_authority.check", ResourceID: u.ID.String()}, 100, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, total, 7)
	require.NotEmpty(t, logs)
	for _, log := range logs {
		require.NotContains(t, log.Metadata, "subject")
		require.NotContains(t, log.Metadata, "credential")
	}
}

func TestExecutionAuthorityProofScopeExpiryAndBinding(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	f := makeAutomationFixture(t, s)
	u, _, err := s.PersistAuthenticationRoles(ctx, "oidc:https://execution-fixture.invalid", uuid.NewString(), "", "", []string{"admin"})
	require.NoError(t, err)
	executionAuthorityFixture(t, s)
	proofCtx, err := s.prepareExecutionAuthority(ctx, f.tenant, "dispatch", uuid.NewString(), authorityTarget{u.ID, "individual_approval"})
	require.NoError(t, err)
	tx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, checkExecutionAuthorityTx(proofCtx, tx, f.tenant, u.ID, "individual_approval"))
	_, err = tx.ExecContext(ctx, `UPDATE users SET auth_provider='static' WHERE id=$1`, u.ID)
	require.NoError(t, err)
	require.Error(t, checkExecutionAuthorityTx(proofCtx, tx, f.tenant, u.ID, "individual_approval"))
	_, err = tx.ExecContext(ctx, `UPDATE users SET auth_provider='oidc' WHERE id=$1`, u.ID)
	require.NoError(t, err)
	require.Error(t, checkExecutionAuthorityTx(proofCtx, tx, uuid.New(), u.ID, "individual_approval"))
	require.Error(t, checkExecutionAuthorityTx(proofCtx, tx, f.tenant, u.ID, "organization_owner"))
	require.NoError(t, tx.Rollback())
	proofs := proofCtx.Value(authorityProofKey{}).(authorityProofs)
	key := u.ID.String() + ":individual_approval"
	proof := proofs.entries[key]
	proof.decision.ExpiresAt = time.Now().Add(-time.Second)
	proofs.entries[key] = proof
	require.ErrorContains(t, checkExecutionProofDeadlines(context.WithValue(ctx, authorityProofKey{}, proofs)), "expired while awaiting")
	tx, err = s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	require.ErrorContains(t, checkExecutionAuthorityTx(context.WithValue(ctx, authorityProofKey{}, proofs), tx, f.tenant, u.ID, "individual_approval"), "fresh execution-time")
	require.Error(t, bindAuthenticationAuthority(ctx, tx, u.ID, u.ExternalID, "oidc:https://wrong.invalid", "someone-else"))
}

func TestExecutionAuthorityOrganizationDispatchAndStart(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	f := makeAutomationFixture(t, s)
	owner, _, err := s.PersistAuthenticationRoles(ctx, "oidc:https://execution-fixture.invalid", uuid.NewString(), "", "SSO owner fixture", []string{"admin"})
	require.NoError(t, err)
	reviewer, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Password: uuid.NewString(), Roles: []string{"ciso"}})
	require.NoError(t, err)
	_, err = s.SavePatchGovernancePolicy(ctx, f.tenant, 0, patchgovernance.Policy{Name: "Authority fixture", Owner: "Service owner", Mode: "recommend_only", RetentionDays: 30, Rules: []patchgovernance.Rule{{Name: "High", Severity: "high", Exposure: "any", Importance: "any", Priority: "high", DueHours: 24}}}, f.actor.String())
	require.NoError(t, err)
	now := time.Now().UTC()
	policy := patchgovernance.OrganizationPolicy{Name: "SSO authority contract", OwnerID: owner.ID, ApproverID: reviewer.ID, Responsibilities: "Independent existing CISO approves", Scope: "Isolated fixture", CustomerAuthorization: "Test fixture only", Exclusions: "Production excluded", Reason: "Execution authority enforcement test", EffectiveAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	for i, step := range []string{"draft", "submit", "approve"} {
		actor := f.actor
		if step == "approve" {
			actor = reviewer.ID
		}
		_, err = s.SavePatchOrganizationPolicy(ctx, f.tenant, actor, int64(i), step, policy)
		require.NoError(t, err)
	}
	revoked, _ := executionAuthorityFixture(t, s)
	deployment, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: f.tenant, Mode: "direct", TargetNodeCount: 1, Summary: map[string]any{"online_plan": f.plan}})
	require.NoError(t, err)
	approval, err := s.CreatePatchApproval(ctx, CreatePatchApprovalParams{TenantID: f.tenant, NodeID: f.node, DeploymentID: deployment.ID, Mode: "direct", WindowID: &f.window, ExpiresAt: now.Add(time.Hour), PolicyHash: f.params.PolicyHash})
	require.NoError(t, err)
	params := f.params
	params.DeploymentID = deployment.ID
	params.ApprovalID = approval.ID
	f.params = params
	f.approval = approval.ID
	run, scheduled := bindAutomationFixture(t, s, f)
	params = scheduled
	state, err := s.DispatchPatchJob(ctx, params)
	require.NoError(t, err)
	require.NoError(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID))
	require.NoError(t, s.CheckPatchAutomationExecution(ctx, f.tenant, run.ID, *state.JobID))
	_, err = s.db.ExecContext(ctx, `UPDATE patch_organization_policy SET status='withdrawn' WHERE tenant_id=$1`, f.tenant)
	require.NoError(t, err)
	require.ErrorContains(t, s.CheckPatchAutomationExecution(ctx, f.tenant, run.ID, *state.JobID), "independent approval")
	_, err = s.db.ExecContext(ctx, `UPDATE patch_organization_policy SET status='approved' WHERE tenant_id=$1`, f.tenant)
	require.NoError(t, err)
	revoked.Store(true)
	require.ErrorContains(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID), "revoked")
	revoked.Store(false)
	_, err = s.db.ExecContext(ctx, `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, owner.ID)
	require.NoError(t, err)
	require.ErrorContains(t, s.CheckPatchOrganizationExecution(ctx, f.tenant, f.node, *state.JobID), "identity unavailable")
}
