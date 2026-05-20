package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestPostureDriftExplainReturnsDesiredStateAndReceipts(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	auditID := uuid.New()
	nodeIDText := nodeID.String()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	store := &fakeStore{
		nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
			Hostname: "prod-edge-1",
			Labels: map[string]any{
				isolationModeLabel:       isolationModeAirgapped,
				isolationReasonLabel:     "emergency lockdown",
				isolationAllowCIDRsLabel: []any{"10.0.0.0/8"},
				isolationAllowAppsLabel:  []any{"patch-agent"},
			},
		}},
		auditLogs: []storage.AuditLog{{
			ID:           auditID,
			TenantID:     tenantID,
			ActorType:    "agent",
			Action:       "network_policy.receipt",
			ResourceType: "node",
			ResourceID:   &nodeIDText,
			CreatedAt:    now,
			Metadata: map[string]any{
				"desired_state_id":    "sha256:abc",
				"schema_version":      networkPolicySchemaVersion,
				"mode":                isolationModeAirgapped,
				"status":              "planned_dry_run",
				"backend":             "dry-run",
				"dry_run":             true,
				"planned_rules":       2,
				"applied_rules":       0,
				"missing_controls":    []any{"application_allowlist"},
				"drift":               []any{"planned_rules_not_present"},
				"signature_present":   true,
				"signature_valid":     true,
				"signature_key_id":    "test-key",
				"observed_at":         now.Format(time.RFC3339),
				"rollback_available":  false,
				"receipt_contract":    "network_policy.receipt.v1",
				"agent_policy_status": "accepted",
			},
		}},
	}
	srv := &Server{store: store}

	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "posture_drift_explain", Input: map[string]any{
			"node_id": nodeID.String(),
			"since":   now.Add(-time.Hour).Format(time.RFC3339),
			"until":   now.Add(time.Hour).Format(time.RFC3339),
		}},
	)
	if err != nil {
		t.Fatalf("execute posture drift tool: %v", err)
	}
	resp, ok := exec.Payload.(postureDriftExplainResponse)
	if !ok {
		t.Fatalf("unexpected payload type %T", exec.Payload)
	}
	if resp.TenantID != tenantID.String() || resp.NodeID != nodeID.String() {
		t.Fatalf("unexpected scope: %#v", resp)
	}
	if resp.Desired == nil || resp.Desired.Mode != isolationModeAirgapped || !resp.Desired.LocalOnly {
		t.Fatalf("expected airgapped desired state, got %#v", resp.Desired)
	}
	if len(resp.Desired.UnsupportedControls) != 1 || resp.Desired.UnsupportedControls[0] != "application_allowlist" {
		t.Fatalf("expected unsupported application control surfaced, got %#v", resp.Desired.UnsupportedControls)
	}
	if resp.Summary.Total != 1 || resp.Summary.WithDrift != 1 || resp.Summary.MissingControlReceipts != 1 || resp.Summary.RollbackAvailable != 0 {
		t.Fatalf("unexpected summary: %#v", resp.Summary)
	}
	if got := resp.Summary.ByDrift["planned_rules_not_present"]; got != 1 {
		t.Fatalf("expected drift summary count, got %#v", resp.Summary.ByDrift)
	}
	if len(resp.Receipts) != 1 || len(resp.Citations) != 1 {
		t.Fatalf("expected one receipt and citation, got receipts=%#v citations=%#v", resp.Receipts, resp.Citations)
	}
	receipt := resp.Receipts[0]
	if receipt.AuditID != auditID.String() || receipt.Status != "planned_dry_run" || !receipt.DryRun || !receipt.SignaturePresent || !receipt.SignatureValid || receipt.RollbackAvailable {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	if len(receipt.CitationIDs) != 1 || resp.Citations[0].SourceRecordID != "audit_logs:"+auditID.String() {
		t.Fatalf("expected audit log citation, got receipt=%#v citations=%#v", receipt, resp.Citations)
	}
	if exec.Citation.Tool != "posture_drift_explain" || !strings.Contains(exec.Citation.Detail, "1 receipts") {
		t.Fatalf("unexpected execution citation: %#v", exec.Citation)
	}
	if got := fmt.Sprint(resp); strings.Contains(got, "private_key") {
		t.Fatalf("posture drift response leaked unexpected secret material: %s", got)
	}
}

func TestPostureDriftExplainRejectsCrossTenantNode(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}}}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "posture_drift_explain", Input: map[string]any{"node_id": nodeID.String()}},
	)
	if err == nil || !strings.Contains(err.Error(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, got %v", err)
	}
}
