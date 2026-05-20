package server

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestNetworkPolicyDesiredStateIDIsDeterministic(t *testing.T) {
	t.Parallel()

	posture := nodeIsolationPosture{
		Mode:                isolationModeWhitelist,
		Active:              true,
		Reason:              "incident containment",
		AllowedApplications: []string{"patch"},
		AllowlistCIDRs:      []string{"10.0.0.0/8"},
	}
	first := newNodeNetworkPolicy(posture, time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC), []string{"b", "a"}, nil)
	second := newNodeNetworkPolicy(posture, time.Date(2026, 5, 19, 11, 0, 0, 0, time.UTC), []string{"a", "b"}, nil)

	if first.DesiredStateID == "" || first.DesiredStateID != second.DesiredStateID {
		t.Fatalf("desired state ids should be stable, got %q and %q", first.DesiredStateID, second.DesiredStateID)
	}
	if first.SchemaVersion != networkPolicySchemaVersion || first.PolicyClass != networkPolicyClass {
		t.Fatalf("unexpected policy contract: %#v", first)
	}
	if first.Enforcement.DefaultInboundAction != "block" || first.Enforcement.DefaultOutboundAction != "allow" {
		t.Fatalf("unexpected enforcement intent: %#v", first.Enforcement)
	}
	if len(first.Enforcement.UnsupportedControls) != 1 || first.Enforcement.UnsupportedControls[0] != "application_allowlist" {
		t.Fatalf("application scope must be explicit unsupported evidence, got %#v", first.Enforcement.UnsupportedControls)
	}
}

func TestNetworkPolicyTemplateRejectsCaptureFilters(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	policies := []storage.PolicyWithVersion{{
		Policy: storage.Policy{
			ID:       uuid.New(),
			TenantID: tenantID,
			RuleType: "network_policy",
			Enabled:  true,
		},
		Version:        3,
		VersionID:      uuid.New(),
		RuleDefinition: `{"mode":"whitelist","capture_db_queries":true,"allowlist_cidrs":["10.0.0.0/8"]}`,
	}}
	posture, refs, errs := applyNetworkPolicyTemplates(nodeIsolationPosture{Mode: isolationModeOnline}, policies, time.Now().UTC(), nil)

	if posture.Mode != isolationModeOnline || len(refs) != 0 {
		t.Fatalf("capture filter template should not become enforcement posture: posture=%#v refs=%#v", posture, refs)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "capture_filter_key_capture_db_queries_not_allowed") {
		t.Fatalf("expected capture-filter separation error, got %#v", errs)
	}
}

func TestNetworkPolicyTemplateCanRaiseIsolationPosture(t *testing.T) {
	t.Parallel()

	policyID := uuid.New()
	policies := []storage.PolicyWithVersion{{
		Policy: storage.Policy{
			ID:       policyID,
			TenantID: uuid.New(),
			RuleType: "posture_template",
			Labels:   map[string]string{"control_one.policy.class": "network_policy"},
			Enabled:  true,
		},
		Version:        2,
		VersionID:      uuid.New(),
		RuleDefinition: `{"network_policy":{"mode":"airgapped","reason":"emergency lockdown","allowlist_cidrs":["10.0.0.0/8"]}}`,
	}}
	posture, refs, errs := applyNetworkPolicyTemplates(nodeIsolationPosture{Mode: isolationModeWhitelist, Active: true}, policies, time.Now().UTC(), nil)

	if len(errs) != 0 {
		t.Fatalf("unexpected template errors: %#v", errs)
	}
	if posture.Mode != isolationModeAirgapped || !posture.LocalOnly {
		t.Fatalf("expected airgapped posture, got %#v", posture)
	}
	if len(posture.AllowlistCIDRs) != 1 || posture.AllowlistCIDRs[0] != "10.0.0.0/8" {
		t.Fatalf("expected cidr merge, got %#v", posture.AllowlistCIDRs)
	}
	if len(refs) != 1 || !strings.Contains(refs[0], policyID.String()) {
		t.Fatalf("expected source refs, got %#v", refs)
	}
}

func TestNetworkPolicyDesiredStateUsesPolicySigningKey(t *testing.T) {
	t.Parallel()

	keyDir := t.TempDir()
	privPath, pubPath, pub := writeSigningKeyPair(t, keyDir)
	srv := &Server{
		logger: zap.NewNop(),
		cfg: &config.Config{Policy: config.PolicyConfig{
			SigningKeyPath: privPath,
			PublicKeyFile:  pubPath,
		}},
	}
	policy := srv.compileNodeNetworkPolicy(context.Background(), storage.Node{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Labels: map[string]any{
			isolationModeLabel: isolationModeWhitelist,
		},
	}, time.Now().UTC())

	if policy.Signature == "" || policy.SignatureAlgorithm != "ed25519" || policy.SignatureKeyID == "" {
		t.Fatalf("expected signed network policy, got %#v", policy)
	}
	sig, err := base64.StdEncoding.DecodeString(policy.Signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(pub, []byte(policy.DesiredStateID), sig) {
		t.Fatalf("network policy signature did not verify")
	}
}

func TestNetworkPolicyDesiredStateRefusesMismatchedPolicyKeys(t *testing.T) {
	t.Parallel()

	keyDir := t.TempDir()
	privPath, _, _ := writeSigningKeyPair(t, keyDir)
	_, wrongPubPath, _ := writeSigningKeyPair(t, t.TempDir())
	srv := &Server{
		logger: zap.NewNop(),
		cfg: &config.Config{Policy: config.PolicyConfig{
			SigningKeyPath: privPath,
			PublicKeyFile:  wrongPubPath,
		}},
	}
	policy := srv.compileNodeNetworkPolicy(context.Background(), storage.Node{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Labels: map[string]any{
			isolationModeLabel: isolationModeWhitelist,
		},
	}, time.Now().UTC())

	if policy.Signature != "" || policy.SignatureKeyID != "" {
		t.Fatalf("mismatched policy keys must not sign desired state, got %#v", policy)
	}
}
