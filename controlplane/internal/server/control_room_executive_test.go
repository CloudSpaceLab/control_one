package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type executiveRuleSummaryStore struct {
	*fakeStore
	summary storage.RuleViolationSummary
	err     error
}

func (s *executiveRuleSummaryStore) GetRuleViolationSummary(
	_ context.Context,
	_ uuid.UUID,
	_ time.Time,
	_ time.Time,
	_ time.Time,
	_ int,
) (storage.RuleViolationSummary, error) {
	if s.err != nil {
		return storage.RuleViolationSummary{}, s.err
	}
	out := s.summary
	out.TopRules = append([]storage.RuleViolationTopRule(nil), s.summary.TopRules...)
	return out, nil
}

func TestControlRoomExecutiveAttentionUsesExactAlertTotals(t *testing.T) {
	srv, base := dashboardAdminHarness(t, "viewer", "viewer-token")
	store := &executiveRuleSummaryStore{fakeStore: base}
	srv.store = store

	tenantID := base.tenants[0].ID
	now := time.Now().UTC()
	base.alerts = make([]storage.Alert, 0, 150)
	for i := 0; i < 150; i++ {
		severity := "medium"
		if i < 3 {
			severity = "critical"
		}
		base.alerts = append(base.alerts, storage.Alert{
			ID:       uuid.New(),
			TenantID: tenantID,
			Source:   "rule",
			Severity: severity,
			Title:    "Executive count regression",
			State:    "open",
			OpenedAt: now.Add(-time.Duration(i) * time.Minute),
		})
	}

	rec := dashboardCall(
		t,
		srv,
		"viewer-token",
		http.MethodGet,
		"/api/v1/control-room/executive-overview?tenant_id="+tenantID.String()+"&period=7d",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var resp controlRoomExecutiveOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode executive overview: %v", err)
	}
	if resp.Attention.Reviews != 150 || resp.Attention.Total != 150 {
		t.Fatalf("attention totals=%+v, want 150 exact reviews", resp.Attention)
	}
	if resp.Attention.Critical != 3 {
		t.Fatalf("critical attention=%d, want 3", resp.Attention.Critical)
	}
	if len(resp.Attention.Items) > 8 {
		t.Fatalf("attention sample len=%d, want <=8", len(resp.Attention.Items))
	}
	if resp.Attention.Total == len(resp.Attention.Items) {
		t.Fatalf("executive total must not be derived from bounded sample: total=%d sample=%d", resp.Attention.Total, len(resp.Attention.Items))
	}
}

func TestControlRoomExecutiveRuleViolationSummaryIsIndependentOfTopRules(t *testing.T) {
	srv, base := dashboardAdminHarness(t, "viewer", "viewer-token")
	tenantID := base.tenants[0].ID
	topRuleID := uuid.New()
	store := &executiveRuleSummaryStore{
		fakeStore: base,
		summary: storage.RuleViolationSummary{
			Total:         37,
			Critical:      3,
			High:          11,
			Medium:        18,
			Low:           5,
			PreviousTotal: 45,
			TopRules: []storage.RuleViolationTopRule{
				{RuleID: topRuleID, RuleType: "log", Severity: "critical", Count: 12},
			},
		},
	}
	srv.store = store

	rec := dashboardCall(
		t,
		srv,
		"viewer-token",
		http.MethodGet,
		"/api/v1/control-room/executive-overview?tenant_id="+tenantID.String()+"&period=7d",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var resp controlRoomExecutiveOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode executive overview: %v", err)
	}
	if !resp.Availability.Violations {
		t.Fatal("rule violations should be available")
	}
	if resp.Violations.Total != 37 || resp.Violations.Critical != 3 || resp.Violations.High != 11 {
		t.Fatalf("unexpected violations: %+v", resp.Violations)
	}
	if len(resp.Violations.TopRules) != 1 || resp.Violations.TopRules[0].Count != 12 {
		t.Fatalf("unexpected top rules: %+v", resp.Violations.TopRules)
	}
	if resp.Violations.Total == resp.Violations.TopRules[0].Count {
		t.Fatalf("total must be independent of bounded top rules: %+v", resp.Violations)
	}
}

func TestControlRoomExecutiveCountsOnlyVerifiedAutomaticResponses(t *testing.T) {
	srv, base := dashboardAdminHarness(t, "viewer", "viewer-token")
	tenantID := base.tenants[0].ID
	now := time.Now().UTC()

	successID := uuid.New()
	failedID := uuid.New()
	base.actionPlans = map[uuid.UUID]storage.ActionPlan{
		successID: {
			ID:         successID,
			TenantID:   tenantID,
			Domain:     "remediation",
			ActionKind: "remediation.execute",
			State:      storage.ActionPlanStateSucceeded,
			Risk:       "medium",
			Diff:       map[string]any{"auto_triggered": true},
			SourceRef:  map[string]any{},
			CreatedAt:  now.Add(-time.Hour),
			UpdatedAt:  now.Add(-30 * time.Minute),
		},
		failedID: {
			ID:         failedID,
			TenantID:   tenantID,
			Domain:     "firewall",
			ActionKind: "block",
			State:      storage.ActionPlanStateFailed,
			Risk:       "high",
			Diff:       map[string]any{"auto_triggered": true},
			SourceRef:  map[string]any{},
			CreatedAt:  now.Add(-40 * time.Minute),
			UpdatedAt:  now.Add(-20 * time.Minute),
		},
	}
	base.actionReceipts = map[uuid.UUID][]storage.ActionReceipt{
		successID: {
			{
				ID:           uuid.New(),
				ActionPlanID: successID,
				TenantID:     tenantID,
				State:        storage.ActionPlanStateSucceeded,
				Receipt:      map[string]any{"success": true},
				Verification: map[string]any{"script_success": true},
				CreatedAt:    now.Add(-30 * time.Minute),
			},
		},
	}
	store := &executiveRuleSummaryStore{fakeStore: base}
	srv.store = store

	rec := dashboardCall(
		t,
		srv,
		"viewer-token",
		http.MethodGet,
		"/api/v1/control-room/executive-overview?tenant_id="+tenantID.String()+"&period=24h",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var resp controlRoomExecutiveOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode executive overview: %v", err)
	}
	if resp.Response.HandledAutomatically != 1 || resp.Response.Remediated != 1 {
		t.Fatalf("verified response counts=%+v, want one automatic remediation", resp.Response)
	}
	if resp.Response.Failed != 1 {
		t.Fatalf("failed automatic responses=%d, want 1", resp.Response.Failed)
	}
	if resp.Attention.Interventions != 1 || resp.Attention.Total != 1 {
		t.Fatalf("failed automation must surface as intervention: %+v", resp.Attention)
	}
	if len(resp.Attention.Items) != 1 || resp.Attention.Items[0].Kind != "intervention" {
		t.Fatalf("unexpected intervention sample: %+v", resp.Attention.Items)
	}
}

func TestControlRoomExecutiveEstateGroupsAndIntentionalIsolation(t *testing.T) {
	now := time.Now().UTC()
	tenantID := uuid.New()
	healthyID := uuid.New()
	degradedID := uuid.New()
	airgappedID := uuid.New()
	stale := now.Add(-10 * time.Minute)
	old := now.Add(-2 * time.Hour)

	nodes := []storage.Node{
		{
			ID: healthyID, TenantID: tenantID, Hostname: "payments-01", LastSeenAt: &now,
			Labels: map[string]any{"dashboard_group": "Payments"},
		},
		{
			ID: degradedID, TenantID: tenantID, Hostname: "edge-01", LastSeenAt: &stale,
			Labels: map[string]any{"server_group": "Web Edge"},
		},
		{
			ID: airgappedID, TenantID: tenantID, Hostname: "vault-01", LastSeenAt: &old,
			Labels: map[string]any{
				"dashboard_group":  "Vault",
				isolationModeLabel: isolationModeAirgapped,
			},
		},
	}

	got := buildControlRoomExecutiveEstate(nodes, now)
	if got.GroupsTotal != 3 || got.GroupsHealthy != 2 || got.GroupsDegraded != 1 {
		t.Fatalf("unexpected estate groups: %+v", got)
	}
	if got.NodesHealthy != 1 || got.NodesTotal != 3 {
		t.Fatalf("unexpected node totals: %+v", got)
	}
	var vault *controlRoomExecutiveGroup
	for i := range got.Groups {
		if got.Groups[i].Name == "Vault" {
			vault = &got.Groups[i]
			break
		}
	}
	if vault == nil || vault.State != "healthy" || vault.IntentionallyIsolated != 1 || vault.NodesOffline != 0 {
		t.Fatalf("intentional isolation should remain healthy, got %+v", vault)
	}
}

func TestControlRoomExecutiveProtectionUsesVerifiedListenerEvidence(t *testing.T) {
	srv, base := dashboardAdminHarness(t, "viewer", "viewer-token")
	tenantID := base.tenants[0].ID
	nodeID := uuid.New()
	now := time.Now().UTC()
	base.nodes = []storage.Node{{
		ID: nodeID, TenantID: tenantID, Hostname: "edge-01", LastSeenAt: &now,
		Labels: map[string]any{"dashboard_group": "Web Edge"},
	}}
	base.nodeServices = map[uuid.UUID][]storage.NodeService{
		nodeID: {{
			NodeID: nodeID, TenantID: tenantID, ListenAddr: "0.0.0.0", Port: 443,
			ServiceKind: "web", ObservedAt: now,
		}},
	}
	base.firewallStates = map[uuid.UUID]storage.NodeFirewallState{
		nodeID: {
			NodeID: nodeID, FirewallType: "ufw", Enabled: true,
			Rules:      []storage.FirewallRule{{Raw: "Default: deny (incoming), allow (outgoing)"}},
			ObservedAt: now,
		},
	}
	store := &executiveRuleSummaryStore{fakeStore: base}
	srv.store = store

	rec := dashboardCall(
		t,
		srv,
		"viewer-token",
		http.MethodGet,
		"/api/v1/control-room/executive-overview?tenant_id="+tenantID.String()+"&period=24h",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var resp controlRoomExecutiveOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode executive overview: %v", err)
	}
	if !resp.Availability.Protection {
		t.Fatal("protection coverage should be available")
	}
	if resp.Protection.Total != 1 || resp.Protection.Protected != 1 || resp.Protection.Gaps != 0 || resp.Protection.Percentage != 100 {
		t.Fatalf("unexpected protection coverage: %+v", resp.Protection)
	}
}

func TestControlRoomAutomaticFirewallReason(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
	}{
		{"Auto-block: known malicious source", true},
		{"Correlation response: rule=SSH; alert_id=x; mode=auto_temporary_block", true},
		{"Correlation response: rule=SSH; alert_id=x; mode=proposal", false},
		{"Manual analyst block", false},
	}
	for _, tc := range cases {
		if got := controlRoomAutomaticFirewallReason(tc.reason); got != tc.want {
			t.Fatalf("controlRoomAutomaticFirewallReason(%q)=%v, want %v", tc.reason, got, tc.want)
		}
	}
}
