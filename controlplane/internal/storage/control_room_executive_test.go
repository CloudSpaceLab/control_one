package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetRuleViolationSummaryUsesExactTenantScopedTotals(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "executive-rule-summary-" + uuid.NewString()[:6]})
	require.NoError(t, err)
	otherTenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "executive-rule-summary-other-" + uuid.NewString()[:6]})
	require.NoError(t, err)

	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	prevSince := since.Add(-24 * time.Hour)
	prevUntil := since
	ruleCritical := uuid.New()
	ruleHigh := uuid.New()
	ruleMedium := uuid.New()

	insert := func(tenantID, ruleID uuid.UUID, ruleType, severity string, count int, triggeredAt time.Time) {
		t.Helper()
		for i := 0; i < count; i++ {
			_, err := store.db.ExecContext(ctx, `
				INSERT INTO rule_trigger_log (
					id, tenant_id, rule_id, rule_type, severity, details, triggered_at
				) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, $6)
			`, uuid.New(), tenantID, ruleID, ruleType, severity, triggeredAt.Add(time.Duration(i)*time.Second))
			require.NoError(t, err)
		}
	}

	insert(tenant.ID, ruleCritical, "log", "critical", 14, since.Add(time.Hour))
	insert(tenant.ID, ruleHigh, "port", "high", 10, since.Add(2*time.Hour))
	insert(tenant.ID, ruleMedium, "compliance", "medium", 6, since.Add(3*time.Hour))
	insert(tenant.ID, ruleCritical, "log", "low", 7, prevSince.Add(time.Hour))

	// A noisy second tenant must never inflate the executive total.
	insert(otherTenant.ID, uuid.New(), "log", "critical", 40, since.Add(time.Hour))
	// A future-skewed row for the same tenant must not leak into the selected period.
	insert(tenant.ID, uuid.New(), "log", "critical", 1, until.Add(time.Minute))

	summary, err := store.GetRuleViolationSummary(ctx, tenant.ID, since, until, prevSince, prevUntil, 2)
	require.NoError(t, err)
	require.Equal(t, 30, summary.Total)
	require.Equal(t, 14, summary.Critical)
	require.Equal(t, 10, summary.High)
	require.Equal(t, 6, summary.Medium)
	require.Equal(t, 0, summary.Low)
	require.Equal(t, 7, summary.PreviousTotal)

	// The top-rule sample is bounded independently from the exact total.
	require.Len(t, summary.TopRules, 2)
	require.Equal(t, ruleCritical, summary.TopRules[0].RuleID)
	require.Equal(t, 14, summary.TopRules[0].Count)
	require.Equal(t, "critical", summary.TopRules[0].Severity)
	require.Equal(t, ruleHigh, summary.TopRules[1].RuleID)
	require.Equal(t, 10, summary.TopRules[1].Count)
	require.Equal(t, "high", summary.TopRules[1].Severity)
	require.Greater(t, summary.Total, summary.TopRules[0].Count+summary.TopRules[1].Count)
}


func TestGetAutomaticResponseSummaryCountsVerifiedAutomaticWork(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "executive-auto-summary-" + uuid.NewString()[:6]})
	require.NoError(t, err)

	since := time.Now().UTC().Add(-time.Hour)
	until := time.Now().UTC().Add(time.Hour)

	createPlan := func(domain, action string, automatic bool, state ActionPlanState) *ActionPlan {
		t.Helper()
		plan, err := store.CreateActionPlan(ctx, CreateActionPlanParams{
			TenantID:   tenant.ID,
			Domain:     domain,
			ActionKind: action,
			State:      state,
			Risk:       "high",
			Diff:       map[string]any{"auto_triggered": automatic},
			SourceRef:  map[string]any{},
		})
		require.NoError(t, err)
		require.NotNil(t, plan)
		return plan
	}
	succeed := func(plan *ActionPlan) {
		t.Helper()
		_, err := store.CreateActionReceipt(ctx, CreateActionReceiptParams{
			ActionPlanID: plan.ID,
			TenantID:     tenant.ID,
			State:        ActionPlanStateSucceeded,
			Receipt:      map[string]any{"success": true},
			Verification: map[string]any{"verified": true},
		})
		require.NoError(t, err)
	}

	blocked := createPlan("firewall", "block", true, ActionPlanStateQueued)
	succeed(blocked)
	remediated := createPlan("remediation", "remediation.execute", true, ActionPlanStateQueued)
	succeed(remediated)
	contained := createPlan("network", "contain", true, ActionPlanStateQueued)
	succeed(contained)

	manual := createPlan("remediation", "remediation.execute", false, ActionPlanStateQueued)
	succeed(manual)

	// A succeeded plan without a receipt is not verified and must not count.
	_ = createPlan("remediation", "remediation.execute", true, ActionPlanStateSucceeded)

	failed := createPlan("firewall", "block", true, ActionPlanStateFailed)

	summary, err := store.GetAutomaticResponseSummary(ctx, tenant.ID, since, until, 1)
	require.NoError(t, err)
	require.Equal(t, 3, summary.HandledAutomatically)
	require.Equal(t, 1, summary.Blocked)
	require.Equal(t, 1, summary.Contained)
	require.Equal(t, 1, summary.Remediated)
	require.Equal(t, 1, summary.Failed)
	require.Len(t, summary.FailedPlans, 1)
	require.Equal(t, failed.ID, summary.FailedPlans[0].ID)
}
