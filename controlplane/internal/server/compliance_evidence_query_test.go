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

func TestComplianceEvidenceQueryReturnsCitedSanitizedEvidence(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	resultID := uuid.New()
	evidenceID := uuid.New()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	framework := "SOC2"
	control := "CC6.1"
	evidenceType := "config_snapshot"
	checksum := "sha256:abc123"
	size := int64(512)

	store := &fakeStore{
		nodes: []storage.Node{{ID: nodeID, TenantID: tenantID}},
		complianceResults: map[uuid.UUID][]storage.ComplianceResult{
			jobID: {{
				ID:        resultID,
				JobID:     jobID,
				TenantID:  tenantID,
				NodeID:    nodeID,
				RuleID:    "ssh-password-auth",
				Passed:    false,
				Severity:  testStringPtr("high"),
				Details:   testStringPtr("SSH password auth is enabled"),
				CheckedAt: &now,
				Metadata: complianceResultMetadata(map[string]any{
					"rule_id":   "ssh-password-auth",
					"framework": framework,
					"control":   control,
					"failed_condition": map[string]any{
						"field":  "facts.shadow.root.hash",
						"actual": "$6$raw-shadow-hash",
					},
				}),
			}},
		},
		complianceEvidence: []storage.ComplianceEvidence{
			{
				ID:           uuid.New(),
				TenantID:     tenantID,
				EvidenceType: evidenceType,
				Framework:    &framework,
				ControlRef:   testStringPtr("CC9.9"),
				Title:        "Distractor evidence outside requested control",
				UploadedAt:   now.Add(-30 * time.Minute),
			},
			{
				ID:           uuid.New(),
				TenantID:     tenantID,
				EvidenceType: evidenceType,
				Framework:    &framework,
				ControlRef:   &control,
				Title:        "Expired before report window end",
				UploadedAt:   now.Add(-20 * time.Minute),
				ExpiresAt:    testTimePtr(now.Add(30 * time.Minute)),
			},
			{
				ID:            evidenceID,
				TenantID:      tenantID,
				EvidenceType:  evidenceType,
				Framework:     &framework,
				ControlRef:    &control,
				Title:         "SSH daemon config",
				Description:   testStringPtr("Collected from hardened baseline job"),
				FilePath:      testStringPtr("C:/tmp/secret/path/sshd_config"),
				FileSizeBytes: &size,
				MimeType:      testStringPtr("text/plain"),
				Checksum:      &checksum,
				UploadedAt:    now.Add(-time.Hour),
			},
		},
	}
	srv := &Server{store: store}

	exec, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "compliance_evidence_query", Input: map[string]any{
			"node_id":     nodeID.String(),
			"framework":   framework,
			"control_ref": control,
			"since":       now.Add(-24 * time.Hour).Format(time.RFC3339),
			"until":       now.Add(time.Hour).Format(time.RFC3339),
			"limit":       1,
		}},
	)
	if err != nil {
		t.Fatalf("execute tool: %v", err)
	}
	resp, ok := exec.Payload.(complianceEvidenceQueryResponse)
	if !ok {
		t.Fatalf("unexpected payload type %T", exec.Payload)
	}
	if resp.Summary.Total != 2 || resp.Summary.EvaluatorEvidence != 1 || resp.Summary.UploadedEvidence != 1 {
		t.Fatalf("unexpected summary: %#v", resp.Summary)
	}
	if resp.Summary.Fresh != 2 || resp.Summary.Stale != 0 || resp.Summary.Expired != 0 {
		t.Fatalf("unexpected freshness summary: %#v", resp.Summary)
	}
	if len(resp.Citations) != 2 {
		t.Fatalf("expected two citations, got %#v", resp.Citations)
	}
	if got := fmt.Sprint(resp); strings.Contains(got, "raw-shadow-hash") || strings.Contains(got, "C:/tmp/secret/path") {
		t.Fatalf("sensitive evidence leaked: %s", got)
	}
	evaluator := resp.EvaluatorEvidence[0]
	if !evaluator.Redacted || evaluator.Framework != framework || evaluator.ControlRef != control || len(evaluator.CitationIDs) != 1 {
		t.Fatalf("unexpected evaluator evidence: %#v", evaluator)
	}
	failed := metadataMap(evaluator.Evidence["failed_condition"])
	if actual := metadataMap(failed["actual"]); actual["redacted"] != true {
		t.Fatalf("expected redacted failed actual value, got %#v", failed)
	}
	uploaded := resp.UploadedEvidence[0]
	if uploaded.Checksum != checksum || uploaded.FileSizeBytes == nil || *uploaded.FileSizeBytes != size || len(uploaded.CitationIDs) != 1 {
		t.Fatalf("unexpected uploaded evidence: %#v", uploaded)
	}
	if uploaded.Freshness != "fresh" || uploaded.AgeSeconds != int64((2*time.Hour).Seconds()) {
		t.Fatalf("unexpected uploaded evidence freshness: %#v", uploaded)
	}
	if exec.Citation.Tool != "compliance_evidence_query" || !strings.Contains(exec.Citation.Detail, "2 evidence") {
		t.Fatalf("unexpected citation: %#v", exec.Citation)
	}
}

func TestComplianceEvidenceQueryRejectsCrossTenantNode(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	otherTenantID := uuid.New()
	nodeID := uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: otherTenantID}}}}

	_, err := srv.executeAITool(
		context.Background(),
		&auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}},
		tenantID,
		llm.ToolCall{Name: "compliance_evidence_query", Input: map[string]any{"node_id": nodeID.String()}},
	)
	if err == nil || !strings.Contains(err.Error(), "outside requested tenant") {
		t.Fatalf("expected cross-tenant node rejection, got %v", err)
	}
}

func testStringPtr(value string) *string {
	return &value
}

func testTimePtr(value time.Time) *time.Time {
	return &value
}
