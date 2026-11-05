package storage

import (
	"bytes"
	"context"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestPatchOrganizationIndependentApprovalAndDrift(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, e := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, e)
	author, e := s.EnsureUser(ctx, uuid.NewString(), uuid.NewString()+"@example.invalid", "Policy author")
	require.NoError(t, e)
	approver, e := s.EnsureUser(ctx, uuid.NewString(), uuid.NewString()+"@example.invalid", "Independent approver")
	require.NoError(t, e)
	for _, id := range []uuid.UUID{author.ID, approver.ID} {
		require.NoError(t, s.SetUserRoles(ctx, id, []string{"admin"}))
	}
	priority := patchgovernance.Policy{Name: "Isolated urgency", Owner: "Lab", Mode: "recommend_only", RetentionDays: 30, Rules: []patchgovernance.Rule{{Name: "High", Severity: "high", Exposure: "any", Importance: "any", Priority: "high", DueHours: 24}}}
	_, e = s.SavePatchGovernancePolicy(ctx, tenant.ID, 0, priority, author.ID.String())
	require.NoError(t, e)
	ops := patchgovernance.DefaultOperations()
	ops.Owner = "Lab"
	ops.Reason = "Isolated review"
	_, e = s.SavePatchReviewOperations(ctx, tenant.ID, 0, ops, author.ID.String())
	require.NoError(t, e)
	now := time.Now().UTC()
	p := patchgovernance.OrganizationPolicy{Name: "Isolated organizational approval", OwnerID: author.ID, ApproverID: approver.ID, Responsibilities: "Lab operations", Scope: "No production machines", CustomerAuthorization: "Isolated fixture", Exclusions: "All production changes", Reason: "Exercise lifecycle", EffectiveAt: now, ExpiresAt: now.Add(time.Hour)}
	draft, e := s.SavePatchOrganizationPolicy(ctx, tenant.ID, author.ID, 0, "draft", p)
	require.NoError(t, e)
	require.Equal(t, "draft", draft.Status)
	_, e = s.SavePatchOrganizationPolicy(ctx, tenant.ID, author.ID, 0, "draft", p)
	require.ErrorIs(t, e, ErrGovernanceConflict)
	reviewed, e := s.SavePatchOrganizationPolicy(ctx, tenant.ID, author.ID, 1, "submit", p)
	require.NoError(t, e)
	require.Equal(t, "review", reviewed.Status)
	_, e = s.SavePatchOrganizationPolicy(ctx, tenant.ID, author.ID, 2, "approve", p)
	require.Error(t, e)
	var concurrent sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		concurrent.Add(1)
		go func() {
			defer concurrent.Done()
			_, err := s.SavePatchOrganizationPolicy(ctx, tenant.ID, approver.ID, 2, "approve", p)
			results <- err
		}()
	}
	concurrent.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrGovernanceConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflicts)
	approved, e := s.GetPatchOrganizationPolicy(ctx, tenant.ID)
	require.NoError(t, e)
	require.Equal(t, "approved", approved.Status)
	require.Equal(t, approver.ID, *approved.ApprovedBy)
	_, e = s.SavePatchOrganizationPolicy(ctx, uuid.New(), approver.ID, 3, "withdraw", p)
	require.Error(t, e)
	withdrawn, e := s.SavePatchOrganizationPolicy(ctx, tenant.ID, author.ID, 3, "withdraw", p)
	require.NoError(t, e)
	require.Equal(t, "withdrawn", withdrawn.Status)
	_, e = s.SavePatchOrganizationPolicy(ctx, tenant.ID, author.ID, 4, "draft", p)
	require.NoError(t, e)
	_, e = s.SavePatchReviewOperations(ctx, tenant.ID, 1, ops, author.ID.String())
	require.NoError(t, e)
	_, e = s.SavePatchOrganizationPolicy(ctx, tenant.ID, author.ID, 5, "submit", p)
	require.Error(t, e, "changed controls require renewed draft")
	history, e := s.ListPatchOrganizationHistory(ctx, tenant.ID)
	require.NoError(t, e)
	require.Len(t, history, 5)
	var exported bytes.Buffer
	var archive PatchReviewArchiveInput
	require.NoError(t, s.WritePatchReviewExport(ctx, tenant.ID, &exported, &archive))
	require.Equal(t, 5, archive.Counts["organization_history"])
	require.Equal(t, 1, archive.Counts["organization_policy"])
	require.NoError(t, s.SetUserRoles(ctx, approver.ID, []string{"viewer"}))
	_, e = s.SavePatchOrganizationPolicy(ctx, tenant.ID, author.ID, 5, "draft", p)
	require.Error(t, e, "revoked approver cannot be selected")
	var count int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, tenant.ID).Scan(&count))
	require.Zero(t, count)
}
