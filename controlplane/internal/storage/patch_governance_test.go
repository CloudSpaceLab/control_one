package storage

import (
	"context"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestPatchGovernanceDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	p := patchgovernance.Policy{Name: "Isolated policy", Owner: "Test operations", Mode: "recommend_only", RetentionDays: 30, Rules: []patchgovernance.Rule{{Name: "Critical", Severity: "critical", Exposure: "any", Importance: "any", Priority: "high", DueHours: 72}}}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.SavePatchGovernancePolicy(ctx, tenant.ID, 0, p, "test-reviewer")
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else {
			require.ErrorIs(t, e, ErrGovernanceConflict)
			conflict++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
	saved, err := s.GetPatchGovernancePolicy(ctx, tenant.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Revision)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive})
	require.NoError(t, err)
	now := time.Now().UTC()
	f := testVulnerabilityFinding(tenant.ID, node.ID, "liba", "CVE-TEST-GOVERNANCE", "isolated", now)
	require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{f}))
	findings, _, err := s.ListTenantPatchReviewFindings(ctx, tenant.ID)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f = findings[0]
	until := now.Add(time.Hour)
	review := patchgovernance.Tracking{Owner: "Lab operator", EscalateTo: "Lab review queue", Exposure: "unknown", Importance: "unknown", Compatibility: "unknown", Reason: "Test bounded exception", ExceptionUntil: &until}
	row, err := s.SavePatchGovernanceTracking(ctx, tenant.ID, f.ID, 0, review, "test-admin")
	require.NoError(t, err)
	require.EqualValues(t, 1, row.Revision)
	_, err = s.SavePatchGovernanceTracking(ctx, tenant.ID, f.ID, 0, review, "stale-reviewer")
	require.ErrorIs(t, err, ErrGovernanceConflict)
	_, err = s.SavePatchGovernanceTracking(ctx, uuid.New(), f.ID, 1, review, "wrong-tenant")
	require.Error(t, err)
	events, truncated, err := s.ListPatchGovernanceEvents(ctx, tenant.ID, 30)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, events, 2)
	findings, _, err = s.ListTenantPatchReviewFindings(ctx, tenant.ID)
	require.NoError(t, err)
	require.Nil(t, findings[0].ResolvedAt, "review exceptions cannot resolve findings")
	var jobs int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, tenant.ID).Scan(&jobs))
	require.Zero(t, jobs)
}
