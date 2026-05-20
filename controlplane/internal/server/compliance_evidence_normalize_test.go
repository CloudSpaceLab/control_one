package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

func TestComplianceResultMetadataRedactsCredentialEvidence(t *testing.T) {
	t.Parallel()

	metadata := complianceResultMetadata(map[string]any{
		"rule_id": "credential-shadow",
		"failed_condition": map[string]any{
			"field":    "facts.shadow.root.hash",
			"op":       "not_exists",
			"expected": nil,
			"actual":   "$6$rounds=5000$raw-shadow-hash",
			"found":    true,
		},
		"conditions": []any{
			map[string]any{
				"field":  "facts.ssh.password_auth",
				"actual": false,
				"passed": true,
			},
		},
	})

	if metadata["evidence_contract"] != complianceEvidenceContractVersion {
		t.Fatalf("unexpected evidence contract: %#v", metadata)
	}
	if fmt.Sprint(metadata) == "" || contains(fmt.Sprint(metadata), "raw-shadow-hash") {
		t.Fatalf("raw credential evidence leaked into metadata: %#v", metadata)
	}
	if metadata["evidence_redacted"] != true {
		t.Fatalf("expected redaction marker, got %#v", metadata)
	}
	evidence := complianceEvidenceFromMetadata(metadata)
	failed := metadataMap(evidence["failed_condition"])
	actual := metadataMap(failed["actual"])
	if actual["redacted"] != true || actual["present"] != true {
		t.Fatalf("expected redacted actual value, got %#v", failed["actual"])
	}
	if actual["credential_algorithm"] != "sha512_crypt" || actual["credential_state"] != "hashed" {
		t.Fatalf("expected privacy-preserving credential descriptor, got %#v", actual)
	}
	if actual["value_type"] != "string" || actual["length_bucket"] != "17-64" {
		t.Fatalf("expected type and length bucket without raw value, got %#v", actual)
	}
}

func TestPersistComplianceResultsIncludesSanitizedEvidence(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	store := &fakeStore{complianceResults: map[uuid.UUID][]storage.ComplianceResult{}}
	srv := &Server{store: store}
	checkedAt := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	err := srv.persistComplianceResults(context.Background(), &storage.Job{
		ID:       jobID,
		TenantID: tenantID,
	}, &compliancePayload{
		NodeID: nodeID.String(),
		ScanID: "scan-1",
	}, []compliance.Result{{
		RuleID:    "credential-shadow",
		Passed:    false,
		Severity:  "high",
		Details:   "shadow hash must not be uploaded raw",
		CheckedAt: checkedAt,
		Evidence: map[string]any{
			"failed_condition": map[string]any{
				"field":  "facts.shadow.root.hash",
				"actual": "$6$raw-shadow-hash",
			},
		},
	}})
	if err != nil {
		t.Fatalf("persistComplianceResults: %v", err)
	}
	stored := store.complianceResults[jobID]
	if len(stored) != 1 {
		t.Fatalf("stored results = %d, want 1", len(stored))
	}
	if stored[0].Metadata["evidence_contract"] != complianceEvidenceContractVersion {
		t.Fatalf("metadata missing evidence contract: %#v", stored[0].Metadata)
	}
	if contains(fmt.Sprint(stored[0].Metadata), "raw-shadow-hash") {
		t.Fatalf("raw credential evidence leaked into stored metadata: %#v", stored[0].Metadata)
	}
}

func TestComplianceCredentialDescriptorsAvoidRawMaterial(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		value     string
		algorithm string
		state     string
	}{
		{name: "ntlm", value: "8846f7eaee8fb117ad06bdd830b7586c", algorithm: "ntlm", state: "hashed"},
		{name: "locked", value: "!$6$rounds=5000$locked", state: "locked"},
		{name: "plaintext", value: "Password123!", state: "plaintext_like"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactedEvidenceValue(tc.value)
			if got["redacted"] != true || got["present"] != true {
				t.Fatalf("expected redacted present marker, got %#v", got)
			}
			if got["credential_state"] != tc.state {
				t.Fatalf("credential_state = %#v, want %s: %#v", got["credential_state"], tc.state, got)
			}
			if tc.algorithm != "" && got["credential_algorithm"] != tc.algorithm {
				t.Fatalf("credential_algorithm = %#v, want %s: %#v", got["credential_algorithm"], tc.algorithm, got)
			}
			if contains(fmt.Sprint(got), tc.value) {
				t.Fatalf("raw credential material leaked into descriptor: %#v", got)
			}
		})
	}
}

func TestComplianceEvidenceNormalizationRedactsSecretShapedValues(t *testing.T) {
	t.Parallel()

	metadata := complianceResultMetadata(map[string]any{
		"summary": "collector returned account status",
		"sample":  "root:$6$rounds=5000$raw-shadow-hash",
		"token":   "Bearer abcdefghijklmnopqrstuvwxyz0123456789",
	})
	if contains(fmt.Sprint(metadata), "raw-shadow-hash") || contains(fmt.Sprint(metadata), "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("secret-shaped evidence leaked into metadata: %#v", metadata)
	}
	if metadata["evidence_redacted"] != true {
		t.Fatalf("expected value-shape redaction marker, got %#v", metadata)
	}
	evidence := complianceEvidenceFromMetadata(metadata)
	if metadataMap(evidence["sample"])["redacted"] != true {
		t.Fatalf("expected shadow-shaped sample to be redacted, got %#v", evidence["sample"])
	}
	if metadataMap(evidence["token"])["redacted"] != true {
		t.Fatalf("expected token-shaped value to be redacted, got %#v", evidence["token"])
	}
}
