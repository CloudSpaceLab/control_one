package storage

import (
	"context"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPatchDispatchWithDedicatedPostgres(t *testing.T) {
	url := os.Getenv("PATCH_SECURITY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("dedicated disposable database required")
	}
	ctx := context.Background()
	s, err := New(zap.NewNop(), config.DatabaseConfig{URL: url, MaxOpenConns: 8}, Options{})
	require.NoError(t, err)
	defer s.Close()
	var database string
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database))
	require.True(t, strings.HasPrefix(database, "controlone_patch_security_test_"), "refusing non-test database")
	require.NoError(t, migrate.Apply(ctx, s.db))
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "dispatch-" + uuid.NewString()})
	require.NoError(t, err)
	fixture := func(approval bool) DispatchPatchJobParams {
		n, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: "dispatch-" + uuid.NewString(), State: NodeStateActive})
		require.NoError(t, err)
		d, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: tenant.ID, Mode: "direct", TargetNodeCount: 1})
		require.NoError(t, err)
		p := DispatchPatchJobParams{TenantID: tenant.ID, NodeID: n.ID, DeploymentID: d.ID, Mode: "direct", Payload: json.RawMessage(`{"package_allowlist":["fixture-package"]}`), Plan: CreateActionPlanParams{State: ActionPlanStateQueued, Diff: map[string]any{"package_allowlist": []string{"fixture-package"}}}}
		if approval {
			a, err := s.CreatePatchApproval(ctx, CreatePatchApprovalParams{TenantID: tenant.ID, NodeID: n.ID, DeploymentID: d.ID, Mode: "direct", ExpiresAt: time.Now().Add(time.Hour), PolicyHash: "reviewed-policy"})
			require.NoError(t, err)
			p.ApprovalID, p.PolicyHash = a.ID, a.PolicyHash
		}
		return p
	}
	count := func(query string, id uuid.UUID) int {
		var n int
		require.NoError(t, s.db.QueryRowContext(ctx, query, id).Scan(&n))
		return n
	}
	for _, fault := range []struct{ name, table, operation, condition, body string }{
		{"state-insert", "node_patch_state", "INSERT", "true", "RAISE EXCEPTION 'dispatch fault'"},
		{"plan-insert", "action_plans", "INSERT", "true", "RAISE EXCEPTION 'dispatch fault'"},
		{"job-insert", "jobs", "INSERT", "true", "RAISE EXCEPTION 'dispatch fault'"},
		{"event-insert", "job_events", "INSERT", "true", "RAISE EXCEPTION 'dispatch fault'"},
		{"job-link", "node_patch_state", "UPDATE", "NEW.job_id IS NOT NULL", "RAISE EXCEPTION 'dispatch fault'"},
		{"silent-job-link", "node_patch_state", "UPDATE", "NEW.job_id IS NOT NULL", "RETURN NULL"},
		{"connection-loss-at-job-link", "node_patch_state", "UPDATE", "NEW.job_id IS NOT NULL", "PERFORM pg_terminate_backend(pg_backend_pid())"},
		{"approval-resolution", "patch_approvals", "UPDATE", "NEW.status='approved'", "RAISE EXCEPTION 'dispatch fault'"},
	} {
		p := fixture(true)
		t.Run(fault.name, func(t *testing.T) {
			_, err := s.db.ExecContext(ctx, `CREATE FUNCTION patch_dispatch_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF `+fault.condition+` THEN `+fault.body+`; END IF; RETURN NEW; END $$; CREATE TRIGGER patch_dispatch_fault BEFORE `+fault.operation+` ON `+fault.table+` FOR EACH ROW EXECUTE FUNCTION patch_dispatch_fault();`)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, err := s.db.ExecContext(ctx, `DROP TRIGGER patch_dispatch_fault ON `+fault.table+`; DROP FUNCTION patch_dispatch_fault();`)
				require.NoError(t, err)
			})
			_, err = s.DispatchPatchJob(ctx, p)
			require.Error(t, err)
			require.Zero(t, count(`SELECT count(*) FROM node_patch_state WHERE deployment_id=$1`, p.DeploymentID))
			require.Zero(t, count(`SELECT count(*) FROM action_plans WHERE node_id=$1`, p.NodeID))
			require.Zero(t, count(`SELECT count(*) FROM jobs WHERE payload->>'node_id'=$1::text`, p.NodeID))
			a, err := s.GetPatchApproval(ctx, p.ApprovalID)
			require.NoError(t, err)
			require.Equal(t, ApprovalStatusPending, a.Status)
		})
		// Retry the same approved intent after the injected fault is removed.
		ps, err := s.DispatchPatchJob(ctx, p)
		require.NoError(t, err)
		require.NotNil(t, ps.JobID)
		a, err := s.GetPatchApproval(ctx, p.ApprovalID)
		require.NoError(t, err)
		require.Equal(t, ApprovalStatusApproved, a.Status)
	}
	t.Run("concurrent-duplicate-dispatch", func(t *testing.T) {
		for _, requiresApproval := range []bool{false, true} {
			p := fixture(requiresApproval)
			ids := make(chan uuid.UUID, 16)
			errs := make(chan error, 16)
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ps, err := s.DispatchPatchJob(ctx, p)
					errs <- err
					if err == nil {
						ids <- *ps.JobID
					}
				}()
			}
			wg.Wait()
			close(ids)
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			unique := map[uuid.UUID]bool{}
			for id := range ids {
				unique[id] = true
			}
			require.Len(t, unique, 1)
			require.Equal(t, 1, count(`SELECT count(*) FROM action_plans WHERE node_id=$1`, p.NodeID))
			require.Equal(t, 1, count(`SELECT count(*) FROM jobs WHERE payload->>'node_id'=$1::text`, p.NodeID))
			p.Payload = json.RawMessage(`{"package_allowlist":["changed-package"]}`)
			_, err := s.DispatchPatchJob(ctx, p)
			require.Error(t, err, "changed policy cannot overwrite a linked dispatch")
			for id := range unique {
				linked, err := s.GetNodePatchStateByJobID(ctx, id)
				require.NoError(t, err)
				require.NoError(t, s.MarkUnlinkedPatchDispatchFailed(ctx, linked.ID, "rejected duplicate", ""))
				unchanged, err := s.GetNodePatchStateByJobID(ctx, id)
				require.NoError(t, err)
				require.Equal(t, "pending", unchanged.Status, "a rejected duplicate must not fail an active linked job")
			}
			if requiresApproval {
				a, err := s.GetPatchApproval(ctx, p.ApprovalID)
				require.NoError(t, err)
				require.Equal(t, ApprovalStatusApproved, a.Status)
			}
		}
	})
	t.Run("stale-approval", func(t *testing.T) {
		p := fixture(true)
		oldClock := s.clock
		s.clock = func() time.Time { return time.Now().Add(2 * time.Hour) }
		_, err := s.DispatchPatchJob(ctx, p)
		s.clock = oldClock
		require.Error(t, err)
		require.Zero(t, count(`SELECT count(*) FROM node_patch_state WHERE deployment_id=$1`, p.DeploymentID))
	})
	t.Run("terminal-plan-with-missing-link-is-not-reexecuted", func(t *testing.T) {
		p := fixture(false)
		state, err := s.CreateNodePatchState(ctx, NodePatchState{TenantID: p.TenantID, NodeID: p.NodeID, DeploymentID: p.DeploymentID})
		require.NoError(t, err)
		planParams := p.Plan
		planParams.TenantID = p.TenantID
		planParams.NodeID = &p.NodeID
		planParams.Domain = "patch"
		planParams.ActionKind = "patch.deploy"
		planParams.State = ActionPlanStateSucceeded
		planParams.IdempotencyKey = "patch_node_state:" + state.ID.String()
		_, err = s.CreateActionPlan(ctx, planParams)
		require.NoError(t, err)
		_, err = s.DispatchPatchJob(ctx, p)
		require.Error(t, err)
		require.Zero(t, count(`SELECT count(*) FROM jobs WHERE payload->>'node_id'=$1::text`, p.NodeID))
	})
	t.Run("wave-lock-independent-sessions-and-disconnect", func(t *testing.T) {
		p := fixture(false)
		other, err := New(zap.NewNop(), config.DatabaseConfig{URL: url, MaxOpenConns: 4}, Options{})
		require.NoError(t, err)
		defer other.Close()
		release, held, err := s.AcquirePatchDeploymentDispatch(ctx, p.DeploymentID)
		require.NoError(t, err)
		require.True(t, held)
		_, held, err = other.AcquirePatchDeploymentDispatch(ctx, p.DeploymentID)
		require.NoError(t, err)
		require.False(t, held)
		// Kill only the task's lock-holding backend in this guarded disposable DB.
		_, err = other.db.ExecContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND query LIKE 'SELECT pg_try_advisory_lock%'`)
		require.NoError(t, err)
		release()
		releaseAgain, held, err := other.AcquirePatchDeploymentDispatch(ctx, p.DeploymentID)
		require.NoError(t, err)
		require.True(t, held)
		releaseAgain()
	})
	t.Run("legacy-incomplete-intents", func(t *testing.T) {
		for _, kind := range []string{"missing-job", "orphan-job", "expired-approval", "pending-approval", "fresh-intent"} {
			t.Run(kind, func(t *testing.T) {
				p := fixture(kind == "expired-approval" || kind == "pending-approval")
				ps, err := s.CreateNodePatchState(ctx, NodePatchState{TenantID: p.TenantID, DeploymentID: p.DeploymentID, NodeID: p.NodeID})
				require.NoError(t, err)
				if kind != "fresh-intent" {
					_, err = s.db.ExecContext(ctx, `UPDATE node_patch_state SET requested_at=NOW()-INTERVAL '10 minutes' WHERE id=$1`, ps.ID)
					require.NoError(t, err)
				}
				if kind == "expired-approval" {
					_, err = s.db.ExecContext(ctx, `UPDATE patch_approvals SET expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, p.ApprovalID)
					require.NoError(t, err)
				}
				var orphan *Job
				if kind == "orphan-job" {
					payload, _ := json.Marshal(map[string]string{"node_id": p.NodeID.String(), "node_patch_state_id": ps.ID.String()})
					orphan, err = s.CreateJob(ctx, &Job{TenantID: p.TenantID, Type: "patch.deploy_direct", Status: JobStatusQueued, Payload: payload}, nil)
					require.NoError(t, err)
				}
				repaired, err := s.ReconcileIncompletePatchDispatches(ctx, p.NodeID)
				require.NoError(t, err)
				safePending := kind == "pending-approval" || kind == "fresh-intent"
				if safePending {
					require.Zero(t, repaired)
				} else {
					require.Equal(t, 1, repaired)
					d, err := s.GetPatchDeployment(ctx, p.DeploymentID)
					require.NoError(t, err)
					require.Equal(t, "failed", d.Status)
				}
				if orphan != nil {
					j, err := s.GetJob(ctx, orphan.ID)
					require.NoError(t, err)
					require.Equal(t, JobStatusCancelled, j.Status)
				}
				again, err := s.ReconcileIncompletePatchDispatches(ctx, p.NodeID)
				require.NoError(t, err)
				require.Zero(t, again)
			})
		}
	})
}
