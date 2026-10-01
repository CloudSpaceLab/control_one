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
	_, err = store.db.ExecContext(ctx, `UPDATE action_plans SET risk = 'critical' WHERE id = $1`, failed.ID)
	require.NoError(t, err)

	summary, err := store.GetAutomaticResponseSummary(ctx, tenant.ID, since, until)
	require.NoError(t, err)
	require.Equal(t, 3, summary.HandledAutomatically)
	require.Equal(t, 1, summary.Blocked)
	require.Equal(t, 1, summary.Contained)
	require.Equal(t, 1, summary.Remediated)
	require.Equal(t, 1, summary.Failed)
	require.Equal(t, 1, summary.FailedCritical)
}

func TestGetExecutiveAttentionSummaryIsExactBoundedAndDeduplicated(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "executive-attention-" + uuid.NewString()[:6]})
	require.NoError(t, err)
	otherTenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "executive-attention-other-" + uuid.NewString()[:6]})
	require.NoError(t, err)

	now := time.Now().UTC()
	node, err := store.CreateNode(ctx, &Node{
		ID: uuid.New(), TenantID: tenant.ID, Hostname: "payments-db-01",
		Labels: map[string]any{"dashboard_group": "Payments"},
	})
	require.NoError(t, err)

	review, err := store.CreateAlert(ctx, CreateAlertParams{
		TenantID: tenant.ID, Source: "rule", Severity: "high",
		Title: "Review me", Summary: "Human review remains required",
	})
	require.NoError(t, err)

	pendingAlert, err := store.CreateAlert(ctx, CreateAlertParams{
		TenantID: tenant.ID, Source: "correlation", Severity: "critical",
		Title: "Pending block source",
	})
	require.NoError(t, err)
	proposal, err := store.CreateIPBlocklistEntry(ctx, CreateIPBlocklistEntryParams{
		TenantID: tenant.ID, IPCIDR: "203.0.113.10/32",
		Reason: "Correlation response: rule=Known bad source; alert_id=" + pendingAlert.ID.String() + "; mode=proposal",
		Score:  80,
	})
	require.NoError(t, err)
	require.Equal(t, "proposed", proposal.Status)

	_, err = store.CreateIPBlocklistEntry(ctx, CreateIPBlocklistEntryParams{
		TenantID: tenant.ID, IPCIDR: "203.0.113.11/32",
		Reason: "Analyst note: related alert_id=" + review.ID.String(),
		Score:  50,
	})
	require.NoError(t, err)

	handledAlert, err := store.CreateAlert(ctx, CreateAlertParams{
		TenantID: tenant.ID, Source: "correlation", Severity: "critical",
		Title: "Auto handled source",
	})
	require.NoError(t, err)
	handledPlan, err := store.CreateActionPlan(ctx, CreateActionPlanParams{
		TenantID: tenant.ID, Domain: "firewall", ActionKind: "block",
		State: ActionPlanStateProposed, Risk: "high",
		Diff: map[string]any{
			"auto_triggered": true,
			"reason":         "Correlation response: rule=Auto block; alert_id=" + handledAlert.ID.String() + "; mode=auto_temporary_block",
		},
	})
	require.NoError(t, err)
	_, err = store.CreateActionReceipt(ctx, CreateActionReceiptParams{
		ActionPlanID: handledPlan.ID, TenantID: tenant.ID,
		State:        ActionPlanStateSucceeded,
		Receipt:      map[string]any{"success": true},
		Verification: map[string]any{"applied": true},
	})
	require.NoError(t, err)

	deployment, err := store.CreatePatchDeployment(ctx, PatchDeployment{
		TenantID: tenant.ID, Mode: "direct", TargetNodeCount: 1,
	})
	require.NoError(t, err)
	_, err = store.CreatePatchApproval(ctx, CreatePatchApprovalParams{
		TenantID: tenant.ID, DeploymentID: deployment.ID, NodeID: node.ID,
		Mode: "direct", ExpiresAt: now.Add(time.Hour),
	})
	require.NoError(t, err)

	script, err := store.CreateRemediationScript(ctx, CreateRemediationScriptParams{
		RuleID: "cis-1.1", Platform: "all", ScriptType: "bash", ScriptContent: "echo fix",
	})
	require.NoError(t, err)
	_, err = store.CreateRemediationApproval(ctx, CreateRemediationApprovalParams{
		TenantID: tenant.ID, NodeID: node.ID, RuleID: "cis-1.1",
		ScriptID: script.ID, Severity: "critical",
		TaskPayload: []byte(`{"script":"fix"}`), ExpiresAt: now.Add(time.Hour),
	})
	require.NoError(t, err)

	_, err = store.CreateActionPlan(ctx, CreateActionPlanParams{
		TenantID: tenant.ID, Domain: "remediation", ActionKind: "remediation.execute",
		State: ActionPlanStateFailed, Risk: "high",
		Diff: map[string]any{"auto_triggered": true},
	})
	require.NoError(t, err)

	// Cross-tenant noise must not affect either counts or samples.
	_, err = store.CreateAlert(ctx, CreateAlertParams{
		TenantID: otherTenant.ID, Source: "rule", Severity: "critical", Title: "Other tenant",
	})
	require.NoError(t, err)

	summary, err := store.GetExecutiveAttentionSummary(ctx, tenant.ID, now.Add(-time.Hour), now.Add(time.Hour), 3)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Reviews)
	require.Equal(t, 4, summary.Approvals)
	require.Equal(t, 1, summary.Interventions)
	require.Equal(t, 6, summary.Total)
	require.Equal(t, 2, summary.Critical)
	require.Len(t, summary.Items, 3, "top sample must stay bounded independently of exact total")

	ids := map[uuid.UUID]bool{}
	for _, item := range summary.Items {
		ids[item.ID] = true
		require.NotEqual(t, handledAlert.ID, item.ID)
		require.NotEqual(t, pendingAlert.ID, item.ID)
	}
	require.False(t, ids[handledAlert.ID], "verified auto-handled alert remained in review work")
	require.False(t, ids[pendingAlert.ID], "alert represented by pending approval remained in review work")

	var remediation *ExecutiveAttentionItem
	for i := range summary.Items {
		if summary.Items[i].Source == "remediation" {
			remediation = &summary.Items[i]
			break
		}
	}
	require.NotNil(t, remediation)
	require.Equal(t, "critical", remediation.Severity)
	require.Equal(t, "payments-db-01", remediation.NodeHostname)

}

func TestGetPredictiveHealthAvailabilityIsTenantScopedAndFreshnessAware(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "predictive-health-" + uuid.NewString()[:6]})
	require.NoError(t, err)
	otherTenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "predictive-health-other-" + uuid.NewString()[:6]})
	require.NoError(t, err)

	makeNode := func(tenantID uuid.UUID, hostname string) *Node {
		t.Helper()
		node, err := store.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenantID, Hostname: hostname})
		require.NoError(t, err)
		return node
	}
	fresh := makeNode(tenant.ID, "fresh")
	calibrating := makeNode(tenant.ID, "calibrating")
	stale := makeNode(tenant.ID, "stale")
	other := makeNode(otherTenant.ID, "other")

	_, err = store.UpsertNodeHealthScore(ctx, UpsertNodeHealthScoreParams{
		NodeID: fresh.ID, Score: 35, RiskLevel: "high", Components: map[string]any{"cpu": 90},
	})
	require.NoError(t, err)
	_, err = store.UpsertNodeHealthScore(ctx, UpsertNodeHealthScoreParams{
		NodeID: calibrating.ID, Score: 100, RiskLevel: "calibrating", Components: map[string]any{},
	})
	require.NoError(t, err)
	_, err = store.UpsertNodeHealthScore(ctx, UpsertNodeHealthScoreParams{
		NodeID: stale.ID, Score: 20, RiskLevel: "critical", Components: map[string]any{},
	})
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `UPDATE node_health_scores SET computed_at = NOW() - INTERVAL '6 hours' WHERE node_id = $1`, stale.ID)
	require.NoError(t, err)
	_, err = store.UpsertNodeHealthScore(ctx, UpsertNodeHealthScoreParams{
		NodeID: other.ID, Score: 10, RiskLevel: "critical", Components: map[string]any{},
	})
	require.NoError(t, err)

	summary, err := store.GetPredictiveHealthAvailability(ctx, tenant.ID, time.Now().UTC().Add(-3*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 3, summary.ScoredNodes)
	require.Equal(t, 2, summary.FreshNodes)
	require.Equal(t, 1, summary.FreshActionableNodes)
	require.Equal(t, 1, summary.FreshCalibratingNodes)
	require.Equal(t, 1, summary.StaleNodes)
	require.True(t, summary.LatestComputedAt.Valid)
}
