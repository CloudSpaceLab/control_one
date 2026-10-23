package storage

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
)

// Use an explicitly disposable database, never the development or production DB.
func TestVulnerabilityReconciliationWithDedicatedPostgres(t *testing.T) {
	url := os.Getenv("PATCH_SECURITY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PATCH_SECURITY_TEST_DATABASE_URL to a dedicated disposable database")
	}
	ctx := context.Background()
	s, err := New(zap.NewNop(), config.DatabaseConfig{URL: url}, Options{})
	require.NoError(t, err)
	defer s.Close()
	var database string
	require.NoError(t, s.db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database))
	require.True(t, strings.HasPrefix(database, "controlone_patch_security_test_"), "refusing a non-test database")
	require.NoError(t, migrate.Apply(ctx, s.db))
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "assessment-scope-" + uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: "assessment-lab", State: NodeStateActive})
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{
		testVulnerabilityFinding(tenant.ID, node.ID, "openssl", "CVE-TEST-COVERED", "offline_vulnerability_feed", now),
		testVulnerabilityFinding(tenant.ID, node.ID, "openssl", "CVE-TEST-OMITTED", "offline_vulnerability_feed", now),
	}))
	params := ReconcileVulnerabilityFindingsParams{TenantID: tenant.ID, NodeID: node.ID, EvidenceSource: "offline_vulnerability_feed", PackageScopes: []VulnerabilityFindingPackageScope{{PackageName: "openssl", InstalledVersion: "3.0.2-0ubuntu1.14", PackageSource: "apt", Arch: "amd64"}}, CVEIDs: []string{}}
	resolved, err := s.ReconcileVulnerabilityFindings(ctx, params)
	require.NoError(t, err)
	require.Zero(t, resolved)
	params.CVEIDs = []string{"CVE-TEST-COVERED"}
	resolved, err = s.ReconcileVulnerabilityFindings(ctx, params)
	require.NoError(t, err)
	require.EqualValues(t, 1, resolved)
	findings, _, err := s.ListNodeVulnerabilityFindings(ctx, VulnerabilityFindingFilter{TenantID: tenant.ID, NodeID: node.ID, IncludeResolved: true}, 100, 0)
	require.NoError(t, err)
	byCVE := vulnerabilityFindingsByCVE(findings)
	require.NotNil(t, byCVE["CVE-TEST-COVERED"].ResolvedAt)
	require.Nil(t, byCVE["CVE-TEST-OMITTED"].ResolvedAt)
	require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{
		testVulnerabilityFinding(tenant.ID, node.ID, "openssl", "CVE-TEST-HISTORICAL", "offline_vulnerability_feed", now),
		testVulnerabilityFinding(tenant.ID, node.ID, "openssl", "CVE-TEST-NEWER", "offline_vulnerability_feed", now.Add(time.Hour)),
	}))
	params.ResolveHistoricalVersions = true
	params.CVEIDs = []string{"CVE-TEST-HISTORICAL", "CVE-TEST-NEWER"}
	params.PackageScopes[0].InstalledVersion = "3.0.2-0ubuntu1.15"
	params.ObservedAt = now.Add(time.Minute)
	resolved, err = s.ReconcileVulnerabilityFindings(ctx, params)
	require.NoError(t, err)
	require.EqualValues(t, 1, resolved)
	findings, _, err = s.ListNodeVulnerabilityFindings(ctx, VulnerabilityFindingFilter{TenantID: tenant.ID, NodeID: node.ID, IncludeResolved: true}, 100, 0)
	require.NoError(t, err)
	byCVE = vulnerabilityFindingsByCVE(findings)
	require.NotNil(t, byCVE["CVE-TEST-HISTORICAL"].ResolvedAt)
	require.Equal(t, "3.0.2-0ubuntu1.15", byCVE["CVE-TEST-HISTORICAL"].Evidence["resolution_installed_version"])
	require.Nil(t, byCVE["CVE-TEST-NEWER"].ResolvedAt, "stale assessment cannot resolve newer evidence")
	require.Nil(t, byCVE["CVE-TEST-OMITTED"].ResolvedAt)
}

func TestPatchSecurityWithDedicatedPostgres(t *testing.T) {
	url := os.Getenv("PATCH_SECURITY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set PATCH_SECURITY_TEST_DATABASE_URL to a dedicated disposable database")
	}
	ctx := context.Background()
	s, err := New(zap.NewNop(), config.DatabaseConfig{URL: url, MaxOpenConns: 8}, Options{})
	require.NoError(t, err)
	defer s.Close()
	var database string
	require.NoError(t, s.db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database))
	require.True(t, strings.HasPrefix(database, "controlone_patch_security_test_"), "refusing to mutate a non-test database")
	require.NoError(t, migrate.Apply(ctx, s.db))
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "patch-security-regression-" + uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: "patch-lab", State: NodeStateActive})
	require.NoError(t, err)
	deployment, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: tenant.ID, Mode: "direct", TargetNodeCount: 1})
	require.NoError(t, err)
	state, err := s.CreateNodePatchState(ctx, NodePatchState{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: deployment.ID})
	require.NoError(t, err)
	payload, _ := json.Marshal(map[string]string{"node_id": node.ID.String(), "deployment_id": deployment.ID.String(), "node_patch_state_id": state.ID.String()})
	job, err := s.CreateJob(ctx, &Job{TenantID: tenant.ID, Type: "patch.deploy_direct", Status: JobStatusRunning, Payload: payload}, nil)
	require.NoError(t, err)
	require.NoError(t, s.SetNodePatchStateJobID(ctx, state.ID, job.ID))
	params := CompletePatchJobParams{NodeID: node.ID, JobID: job.ID, Action: job.Type, Succeeded: true, PackagesUpgraded: 1}
	wrong := params
	wrong.NodeID = uuid.New()
	_, err = s.CompletePatchJob(ctx, wrong)
	require.Error(t, err)
	wrong = params
	wrong.Action = "patch.deploy_proxy"
	_, err = s.CompletePatchJob(ctx, wrong)
	require.Error(t, err)
	unchanged, err := s.GetNodePatchStateByJobID(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", unchanged.Status)
	require.NoError(t, s.UpdateJobStatus(ctx, job.ID, JobStatusCancelled, "test cancellation", nil))
	_, err = s.CompletePatchJob(ctx, params)
	require.Error(t, err, "cancelled jobs cannot be completed")
	// Restore only this test fixture to exercise the transaction failure path.
	require.NoError(t, s.UpdateJobStatus(ctx, job.ID, JobStatusRunning, "test fixture reset", nil))
	_, err = s.db.ExecContext(ctx, `CREATE FUNCTION patch_security_fail_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='succeeded' THEN RAISE EXCEPTION 'injected terminal event failure'; END IF; RETURN NEW; END $$;
	CREATE TRIGGER patch_security_fail_event BEFORE INSERT ON job_events FOR EACH ROW EXECUTE FUNCTION patch_security_fail_event();`)
	require.NoError(t, err)
	_, err = s.CompletePatchJob(ctx, params)
	require.Error(t, err, "event failure must roll back completion")
	unchanged, err = s.GetNodePatchStateByJobID(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", unchanged.Status)
	rolledBack, err := s.GetJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, JobStatusRunning, rolledBack.Status)
	_, err = s.db.ExecContext(ctx, `DROP TRIGGER patch_security_fail_event ON job_events; DROP FUNCTION patch_security_fail_event();`)
	require.NoError(t, err)
	var wg sync.WaitGroup
	var mu sync.Mutex
	committed := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed, err := s.CompletePatchJob(ctx, params)
			if err != nil {
				t.Error(err)
				return
			}
			if changed {
				mu.Lock()
				committed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, committed, "concurrent completion must commit once")
	got, err := s.GetNodePatchStateByJobID(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", got.Status)
	finished, err := s.GetJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, JobStatusSucceeded, finished.Status)
	require.NotNil(t, finished.FinishedAt)
	var terminalEvents int
	require.NoError(t, s.db.QueryRowContext(ctx, "SELECT count(*) FROM job_events WHERE job_id=$1 AND status='succeeded'", job.ID).Scan(&terminalEvents))
	require.Equal(t, 1, terminalEvents)
	approval, err := s.CreatePatchApproval(ctx, CreatePatchApprovalParams{TenantID: tenant.ID, NodeID: node.ID, DeploymentID: deployment.ID, Mode: "direct", ExpiresAt: time.Now().Add(time.Hour), PolicyHash: "immutable-test-policy"})
	require.NoError(t, err)
	loaded, err := s.GetPatchApproval(ctx, approval.ID)
	require.NoError(t, err)
	require.Equal(t, "immutable-test-policy", loaded.PolicyHash)
	rows, _, err := s.ListPatchApprovals(ctx, ListPatchApprovalsFilter{TenantID: tenant.ID}, 20, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, loaded.PolicyHash, rows[0].PolicyHash)
	oldClock := s.clock
	s.clock = func() time.Time { return time.Now().Add(2 * time.Hour) }
	_, err = s.ResolvePatchApproval(ctx, approval.ID, ApprovalStatusApproved, uuid.Nil)
	require.Error(t, err, "expiry must be checked by the atomic SQL transition")
	s.clock = oldClock
	resolved, err := s.ResolvePatchApproval(ctx, approval.ID, ApprovalStatusApproved, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, loaded.PolicyHash, resolved.PolicyHash)
}
