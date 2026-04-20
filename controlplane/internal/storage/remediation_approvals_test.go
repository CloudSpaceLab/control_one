package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// seedApprovalPrereqs inserts tenant, node, and a remediation script so the
// foreign keys on remediation_approvals resolve.
func seedApprovalPrereqs(t *testing.T, ctx context.Context, store *Store) (tenantID, nodeID, scriptID uuid.UUID) {
	t.Helper()
	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "approvals-" + uuid.NewString()[:6]})
	require.NoError(t, err)
	node, err := store.CreateNode(ctx, &Node{
		ID:       uuid.New(),
		TenantID: tenant.ID,
		Hostname: "app-node-" + uuid.NewString()[:6],
	})
	require.NoError(t, err)
	script, err := store.CreateRemediationScript(ctx, CreateRemediationScriptParams{
		RuleID:        "rule-approval-" + uuid.NewString()[:4],
		Platform:      "all",
		ScriptType:    "bash",
		ScriptContent: "echo fix",
	})
	require.NoError(t, err)
	return tenant.ID, node.ID, script.ID
}

func TestRemediationApprovals_CreateApproveDenyExpire(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	t.Run("create then approve", func(t *testing.T) {
		tenantID, nodeID, scriptID := seedApprovalPrereqs(t, ctx, store)

		a, err := store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			RuleID:      "rule-approve",
			ScriptID:    scriptID,
			Severity:    "high",
			TaskPayload: []byte(`{"rule_id":"rule-approve"}`),
			ExpiresAt:   time.Now().Add(time.Hour),
		})
		require.NoError(t, err)
		require.NotNil(t, a)
		require.Equal(t, ApprovalStatusPending, a.Status)

		approverID := uuid.New()
		// seed the approver user so the FK holds
		_, err = store.db.ExecContext(ctx, `INSERT INTO users (id, external_id) VALUES ($1, $2)`, approverID, "approver-"+approverID.String())
		require.NoError(t, err)

		updated, err := store.ResolveRemediationApproval(ctx, a.ID, ApprovalStatusApproved, approverID)
		require.NoError(t, err)
		require.NotNil(t, updated)
		require.Equal(t, ApprovalStatusApproved, updated.Status)
		require.NotNil(t, updated.ApprovedAt)
		require.NotNil(t, updated.ApprovedBy)
		require.Equal(t, approverID, *updated.ApprovedBy)

		// Second resolve is a no-op / ErrNoRows.
		_, err = store.ResolveRemediationApproval(ctx, a.ID, ApprovalStatusDenied, approverID)
		require.Error(t, err)
		require.True(t, errors.Is(err, sql.ErrNoRows))
	})

	t.Run("deny transitions correctly", func(t *testing.T) {
		tenantID, nodeID, scriptID := seedApprovalPrereqs(t, ctx, store)
		a, err := store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			RuleID:      "rule-deny",
			ScriptID:    scriptID,
			Severity:    "high",
			TaskPayload: []byte(`{}`),
			ExpiresAt:   time.Now().Add(time.Hour),
		})
		require.NoError(t, err)

		updated, err := store.ResolveRemediationApproval(ctx, a.ID, ApprovalStatusDenied, uuid.Nil)
		require.NoError(t, err)
		require.Equal(t, ApprovalStatusDenied, updated.Status)
	})

	t.Run("list filters by status and tenant", func(t *testing.T) {
		tenantA, nodeA, scriptA := seedApprovalPrereqs(t, ctx, store)
		tenantB, nodeB, scriptB := seedApprovalPrereqs(t, ctx, store)

		for _, rule := range []string{"a1", "a2"} {
			_, err := store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{
				TenantID:    tenantA,
				NodeID:      nodeA,
				RuleID:      rule,
				ScriptID:    scriptA,
				Severity:    "high",
				TaskPayload: []byte(`{}`),
				ExpiresAt:   time.Now().Add(time.Hour),
			})
			require.NoError(t, err)
		}
		_, err := store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{
			TenantID:    tenantB,
			NodeID:      nodeB,
			RuleID:      "b1",
			ScriptID:    scriptB,
			Severity:    "high",
			TaskPayload: []byte(`{}`),
			ExpiresAt:   time.Now().Add(time.Hour),
		})
		require.NoError(t, err)

		rows, total, err := store.ListRemediationApprovals(ctx, ListRemediationApprovalsFilter{
			TenantID: tenantA,
			Status:   ApprovalStatusPending,
		}, 0, 0)
		require.NoError(t, err)
		require.Equal(t, 2, total, "tenantA should have 2 pending approvals")
		require.Len(t, rows, 2)
	})

	t.Run("expire flips only pending past-expires rows", func(t *testing.T) {
		tenantID, nodeID, scriptID := seedApprovalPrereqs(t, ctx, store)

		expired, err := store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			RuleID:      "rule-expired",
			ScriptID:    scriptID,
			Severity:    "high",
			TaskPayload: []byte(`{}`),
			ExpiresAt:   time.Now().Add(-time.Minute),
		})
		require.NoError(t, err)

		fresh, err := store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			RuleID:      "rule-fresh",
			ScriptID:    scriptID,
			Severity:    "high",
			TaskPayload: []byte(`{}`),
			ExpiresAt:   time.Now().Add(time.Hour),
		})
		require.NoError(t, err)

		n, err := store.ExpireRemediationApprovals(ctx, time.Now())
		require.NoError(t, err)
		require.GreaterOrEqual(t, n, 1, "at least the seeded expired row should flip")

		gotExpired, err := store.GetRemediationApproval(ctx, expired.ID)
		require.NoError(t, err)
		require.Equal(t, ApprovalStatusExpired, gotExpired.Status)

		gotFresh, err := store.GetRemediationApproval(ctx, fresh.ID)
		require.NoError(t, err)
		require.Equal(t, ApprovalStatusPending, gotFresh.Status)
	})

	t.Run("validation", func(t *testing.T) {
		_, err := store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{})
		require.Error(t, err)

		_, err = store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{
			TenantID: uuid.New(),
			NodeID:   uuid.New(),
			RuleID:   "x",
			ScriptID: uuid.New(),
			Severity: "high",
			// missing payload and expires
		})
		require.Error(t, err)
	})
}
