package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestFirewallCompletionRequiresBoundReceipt(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	jobID := uuid.New()
	rule := testFirewallRule(t, tenantID, jobID)
	base := &fakeStore{jobs: map[uuid.UUID]*storage.Job{
		jobID: {ID: jobID, TenantID: tenantID, Type: JobTypeFirewallRuleAdd, Status: storage.JobStatusRunning},
	}}
	store := &firewallCompletionStore{fakeStore: base, rule: rule}
	srv := &Server{logger: zap.NewNop(), store: store}

	srv.processHeartbeatCompletedActions(context.Background(), rule.NodeID, []heartbeatCompletedAction{{
		Action: JobTypeFirewallRuleAdd,
		JobID:  jobID.String(),
		Status: "succeeded",
	}})

	if len(store.applied) != 0 {
		t.Fatalf("rule was applied without receipt: %#v", store.applied)
	}
	if len(store.failed) != 1 || store.failed[0] != rule.ID {
		t.Fatalf("expected failed rule from missing receipt, got failed=%#v", store.failed)
	}
	if got := base.jobs[jobID].Status; got != storage.JobStatusFailed {
		t.Fatalf("job status = %s, want failed", got)
	}
}

func TestFirewallCompletionAcceptsValidReceipt(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	jobID := uuid.New()
	rule := testFirewallRule(t, tenantID, jobID)
	base := &fakeStore{jobs: map[uuid.UUID]*storage.Job{
		jobID: {ID: jobID, TenantID: tenantID, Type: JobTypeFirewallRuleAdd, Status: storage.JobStatusRunning},
	}}
	store := &firewallCompletionStore{fakeStore: base, rule: rule}
	srv := &Server{logger: zap.NewNop(), store: store}

	srv.processHeartbeatCompletedActions(context.Background(), rule.NodeID, []heartbeatCompletedAction{{
		Action:   JobTypeFirewallRuleAdd,
		JobID:    jobID.String(),
		Status:   "succeeded",
		Metadata: map[string]any{"receipt": validFirewallReceipt(rule, jobID, JobTypeFirewallRuleAdd)},
	}})

	if len(store.failed) != 0 {
		t.Fatalf("unexpected failed rule: %#v", store.failed)
	}
	if len(store.applied) != 1 || store.applied[0] != rule.ID {
		t.Fatalf("expected applied rule, got %#v", store.applied)
	}
	if got := base.jobs[jobID].Status; got != storage.JobStatusSucceeded {
		t.Fatalf("job status = %s, want succeeded", got)
	}
}

func TestFirewallCompletionCreatesUnifiedActionReceipt(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	jobID := uuid.New()
	rule := testFirewallRule(t, tenantID, jobID)
	base := &fakeStore{jobs: map[uuid.UUID]*storage.Job{
		jobID: {ID: jobID, TenantID: tenantID, Type: JobTypeFirewallRuleAdd, Status: storage.JobStatusRunning},
	}}
	plan, err := base.CreateActionPlan(context.Background(), storage.CreateActionPlanParams{
		TenantID:   tenantID,
		NodeID:     &rule.NodeID,
		Domain:     "firewall",
		ActionKind: "block",
		State:      storage.ActionPlanStateQueued,
	})
	if err != nil {
		t.Fatalf("create action plan: %v", err)
	}
	payload := firewallJobPayload{
		NodeFirewallRuleID: rule.ID.String(),
		NodeID:             rule.NodeID.String(),
		EntityActionID:     rule.EntityActionID.String(),
		ActionPlanID:       plan.ID.String(),
		Action:             "block",
		Direction:          "in",
		Source:             *rule.Source,
		Tag:                rule.Tag,
	}
	raw, _ := json.Marshal(payload)
	base.jobs[jobID].Payload = raw
	store := &firewallCompletionStore{fakeStore: base, rule: rule}
	srv := &Server{logger: zap.NewNop(), store: store}

	srv.processHeartbeatCompletedActions(context.Background(), rule.NodeID, []heartbeatCompletedAction{{
		Action:   JobTypeFirewallRuleAdd,
		JobID:    jobID.String(),
		Status:   "succeeded",
		Metadata: map[string]any{"receipt": validFirewallReceipt(rule, jobID, JobTypeFirewallRuleAdd)},
	}})

	receipts := base.actionReceipts[plan.ID]
	if len(receipts) != 1 {
		t.Fatalf("expected one unified receipt, got %d", len(receipts))
	}
	if receipts[0].State != storage.ActionPlanStateSucceeded || receipts[0].JobID.UUID != jobID {
		t.Fatalf("unexpected unified receipt: %+v", receipts[0])
	}
	if got := base.actionPlans[plan.ID].State; got != storage.ActionPlanStateSucceeded {
		t.Fatalf("action plan state = %s, want succeeded", got)
	}
}

func testFirewallRule(t *testing.T, tenantID, jobID uuid.UUID) *storage.NodeFirewallRule {
	t.Helper()
	source := "203.0.113.10"
	ruleID := uuid.New()
	entityActionID := uuid.New()
	nodeID := uuid.New()
	return &storage.NodeFirewallRule{
		ID:             ruleID,
		EntityActionID: entityActionID,
		NodeID:         nodeID,
		TenantID:       tenantID,
		Action:         "block",
		Direction:      "in",
		Source:         &source,
		Tag:            "c1-" + entityActionID.String(),
		Status:         "pending",
		JobID:          &jobID,
		RequestedAt:    time.Now().UTC(),
	}
}

func validFirewallReceipt(rule *storage.NodeFirewallRule, jobID uuid.UUID, action string) map[string]any {
	shape := firewallReceiptShapeFromRule(rule, action)
	return map[string]any{
		"contract":              "firewall.receipt.v1",
		"job_id":                jobID.String(),
		"node_firewall_rule_id": rule.ID.String(),
		"entity_action_id":      rule.EntityActionID.String(),
		"action":                action,
		"status":                "succeeded",
		"backend":               "iptables",
		"rule_fingerprint":      firewallReceiptFingerprint(shape),
		"source":                shape.Source,
		"dest":                  shape.Dest,
		"port":                  shape.Port,
		"protocol":              shape.Protocol,
		"direction":             shape.Direction,
		"rule_action":           shape.RuleAction,
		"tag":                   shape.Tag,
		"observed_at":           time.Now().UTC().Format(time.RFC3339),
	}
}

type firewallCompletionStore struct {
	*fakeStore
	rule    *storage.NodeFirewallRule
	applied []uuid.UUID
	failed  []uuid.UUID
	removed []uuid.UUID
}

func (f *firewallCompletionStore) GetNodeFirewallRuleByJobID(_ context.Context, jobID uuid.UUID) (*storage.NodeFirewallRule, error) {
	if f.rule != nil && f.rule.JobID != nil && *f.rule.JobID == jobID {
		copy := *f.rule
		return &copy, nil
	}
	return nil, nil
}

func (f *firewallCompletionStore) MarkNodeFirewallRuleApplied(_ context.Context, id uuid.UUID) error {
	f.applied = append(f.applied, id)
	return nil
}

func (f *firewallCompletionStore) MarkNodeFirewallRuleFailed(_ context.Context, id uuid.UUID, _ string) error {
	f.failed = append(f.failed, id)
	return nil
}

func (f *firewallCompletionStore) MarkNodeFirewallRuleRemoved(_ context.Context, id uuid.UUID) error {
	f.removed = append(f.removed, id)
	return nil
}
