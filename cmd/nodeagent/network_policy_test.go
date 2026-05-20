package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/firewall"
	"go.uber.org/zap"
)

func TestNetworkPolicyApplierSuppressesUnchangedDesiredState(t *testing.T) {
	t.Parallel()

	applier := makeNetworkPolicyApplier(zap.NewNop())
	raw := json.RawMessage(`{"schema_version":"network_policy.desired_state.v1","desired_state_id":"sha256:abc","mode":"online","active":false,"enforcement":{"engine":"host_firewall","default_inbound_action":"allow","default_outbound_action":"allow","application_scope":"unsupported"}}`)

	first := applier(context.Background(), raw)
	if first == nil {
		t.Fatalf("expected first receipt")
	}
	if first.Status != "no_op" || first.DesiredStateID != "sha256:abc" {
		t.Fatalf("unexpected first receipt: %#v", first)
	}
	if second := applier(context.Background(), raw); second != nil {
		t.Fatalf("unchanged policy should not emit another receipt: %#v", second)
	}
}

func TestNetworkPolicyApplierRechecksUnchangedDesiredStateOnCadence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	applier := makeNetworkPolicyApplierWithOptions(zap.NewNop(), networkPolicyApplierOptions{
		ReceiptInterval: 10 * time.Minute,
		Now: func() time.Time {
			return now
		},
	})
	raw := json.RawMessage(`{"schema_version":"network_policy.desired_state.v1","desired_state_id":"sha256:abc","mode":"online","active":false,"enforcement":{"engine":"host_firewall","default_inbound_action":"allow","default_outbound_action":"allow","application_scope":"unsupported"}}`)

	if first := applier(context.Background(), raw); first == nil || first.Status != "no_op" {
		t.Fatalf("expected first no-op receipt, got %#v", first)
	}
	now = now.Add(9 * time.Minute)
	if second := applier(context.Background(), raw); second != nil {
		t.Fatalf("unchanged policy before cadence should be suppressed: %#v", second)
	}
	now = now.Add(2 * time.Minute)
	if third := applier(context.Background(), raw); third == nil || third.Status != "no_op" {
		t.Fatalf("expected cadence receipt, got %#v", third)
	}
}

func TestPlannedNetworkPolicyRulesBuildsReversibleLockdownShape(t *testing.T) {
	t.Parallel()

	desired := networkPolicyDesiredState{
		DesiredStateID:      "sha256:1234567890abcdef9999",
		Mode:                "airgapped",
		Active:              true,
		LocalOnly:           true,
		AllowedApplications: []string{"patch"},
		Enforcement: networkPolicyEnforcementIntent{
			DefaultInboundAction:  "block",
			DefaultOutboundAction: "block",
			AllowlistCIDRs:        []string{"10.0.0.0/8"},
		},
	}
	rules := plannedNetworkPolicyRules(desired)

	if len(rules) != 6 {
		t.Fatalf("planned rule count = %d, want 6: %#v", len(rules), rules)
	}
	if rules[0].Action != firewall.ActionAllow || rules[0].Direction != firewall.DirectionIn || rules[0].Source != "10.0.0.0/8" {
		t.Fatalf("expected inbound allowlist rule first, got %#v", rules[0])
	}
	if rules[2].Action != firewall.ActionBlock || rules[2].Source != "::/0" {
		t.Fatalf("expected inbound default block, got %#v", rules[2])
	}
	if rules[3].Action != firewall.ActionAllow || rules[3].Direction != firewall.DirectionOut || rules[3].Dest != "10.0.0.0/8" {
		t.Fatalf("expected outbound allowlist rule, got %#v", rules[3])
	}
	if rules[5].Action != firewall.ActionBlock || rules[5].Dest != "::/0" {
		t.Fatalf("expected outbound default block, got %#v", rules[5])
	}
}

func TestNetworkPolicyTrustAcceptsSignedCanonicalState(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	desired := networkPolicyDesiredState{
		SchemaVersion:  "network_policy.desired_state.v1",
		PolicyClass:    "enforcement",
		Mode:           "whitelist",
		Active:         true,
		DesiredStateID: "",
		Enforcement: networkPolicyEnforcementIntent{
			Engine:               "host_firewall",
			DefaultInboundAction: "block",
			ApplicationScope:     "unsupported",
		},
		SourceRefs: []string{"node.labels"},
	}
	desired.DesiredStateID = computeNetworkPolicyDesiredStateID(desired)
	desired.SignatureAlgorithm = "ed25519"
	desired.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(desired.DesiredStateID)))

	if err := validateNetworkPolicyTrust(desired, networkPolicyVerifier{publicKey: pub}); err != nil {
		t.Fatalf("expected valid signed desired state: %v", err)
	}
	desired.Mode = "airgapped"
	if err := validateNetworkPolicyTrust(desired, networkPolicyVerifier{publicKey: pub}); err == nil {
		t.Fatalf("expected tampered desired state to fail verification")
	}
}

func TestNetworkPolicyReceiptRejectsUnsignedActivePolicy(t *testing.T) {
	t.Parallel()

	desired := networkPolicyDesiredState{
		SchemaVersion:  "network_policy.desired_state.v1",
		PolicyClass:    "enforcement",
		Mode:           "whitelist",
		Active:         true,
		DesiredStateID: "",
		Enforcement: networkPolicyEnforcementIntent{
			Engine:               "host_firewall",
			DefaultInboundAction: "block",
			ApplicationScope:     "unsupported",
		},
		SourceRefs: []string{"node.labels"},
	}
	desired.DesiredStateID = computeNetworkPolicyDesiredStateID(desired)

	receipt := buildNetworkPolicyReceipt(context.Background(), desired, time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC), networkPolicyVerifier{}, false, nilNetworkPolicyFirewallProvider, nil)
	if receipt.Status != "failed" || receipt.SignatureValid || receipt.Error == "" || !containsTestString(receipt.MissingControls, "policy_signature") {
		t.Fatalf("expected unsigned active policy failure, got %#v", receipt)
	}
}

func TestNetworkPolicyReceiptAppliesSignedPolicyWhenEnforcementEnabled(t *testing.T) {
	t.Parallel()

	desired, verifier := signedTestNetworkPolicy(t, "whitelist")
	fw := &fakeNetworkPolicyFirewall{}
	receipt := buildNetworkPolicyReceipt(
		context.Background(),
		desired,
		time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC),
		verifier,
		true,
		func() (networkPolicyFirewall, error) { return fw, nil },
		nil,
	)

	if receipt.Status != "applied" || receipt.DryRun || receipt.AppliedRules == 0 || !receipt.RollbackAvailable || !receipt.SignatureValid {
		t.Fatalf("expected applied receipt, got %#v", receipt)
	}
	if len(fw.applied) != receipt.PlannedRules {
		t.Fatalf("applied rules = %d, want %d", len(fw.applied), receipt.PlannedRules)
	}
}

func TestNetworkPolicyReceiptRollsBackPreviousPolicyOnOnlineState(t *testing.T) {
	t.Parallel()

	previous, verifier := signedTestNetworkPolicy(t, "airgapped")
	online := previous
	online.Mode = "online"
	online.Active = false
	online.LocalOnly = false
	online.Enforcement.DefaultInboundAction = "allow"
	online.Enforcement.DefaultOutboundAction = "allow"
	online.Signature = ""
	online.SignatureAlgorithm = ""
	online.SignatureKeyID = ""
	online.DesiredStateID = computeNetworkPolicyDesiredStateID(online)
	fw := &fakeNetworkPolicyFirewall{}

	receipt := buildNetworkPolicyReceipt(
		context.Background(),
		online,
		time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC),
		verifier,
		true,
		func() (networkPolicyFirewall, error) { return fw, nil },
		&previous,
	)

	if receipt.Status != "rollback_applied" || receipt.DryRun || receipt.RemovedRules == 0 {
		t.Fatalf("expected rollback receipt, got %#v", receipt)
	}
	if len(fw.removed) != receipt.RemovedRules {
		t.Fatalf("removed rules = %d, want %d", len(fw.removed), receipt.RemovedRules)
	}
}

func TestNetworkPolicyApplierPersistsAndRestoresRollbackState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	desired, verifier := signedTestNetworkPolicy(t, "airgapped")
	publicKeyPath := writeTestNetworkPolicyPublicKey(t, stateDir, verifier.publicKey)
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	fw := &fakeNetworkPolicyFirewall{}
	applier := makeNetworkPolicyApplierWithOptions(zap.NewNop(), networkPolicyApplierOptions{
		Now:           func() time.Time { return now },
		PublicKeyPath: publicKeyPath,
		Enforce:       true,
		Firewall:      func() (networkPolicyFirewall, error) { return fw, nil },
		StateDir:      stateDir,
	})

	rawDesired, err := json.Marshal(desired)
	if err != nil {
		t.Fatalf("marshal desired: %v", err)
	}
	applied := applier(context.Background(), json.RawMessage(rawDesired))
	if applied == nil || applied.Status != "applied" {
		t.Fatalf("expected applied receipt, got %#v", applied)
	}
	persisted, err := loadPersistedNetworkPolicyState(stateDir, verifier)
	if err != nil {
		t.Fatalf("load persisted rollback state: %v", err)
	}
	if persisted == nil || persisted.DesiredStateID != desired.DesiredStateID {
		t.Fatalf("persisted rollback state = %#v, want %s", persisted, desired.DesiredStateID)
	}

	online := desired
	online.Mode = "online"
	online.Active = false
	online.LocalOnly = false
	online.Enforcement.DefaultInboundAction = "allow"
	online.Enforcement.DefaultOutboundAction = "allow"
	online.Signature = ""
	online.SignatureAlgorithm = ""
	online.SignatureKeyID = ""
	online.DesiredStateID = computeNetworkPolicyDesiredStateID(online)
	rawOnline, err := json.Marshal(online)
	if err != nil {
		t.Fatalf("marshal online desired state: %v", err)
	}
	restartedFirewall := &fakeNetworkPolicyFirewall{}
	restarted := makeNetworkPolicyApplierWithOptions(zap.NewNop(), networkPolicyApplierOptions{
		Now:           func() time.Time { return now.Add(time.Minute) },
		PublicKeyPath: publicKeyPath,
		Enforce:       true,
		Firewall:      func() (networkPolicyFirewall, error) { return restartedFirewall, nil },
		StateDir:      stateDir,
	})
	rollback := restarted(context.Background(), json.RawMessage(rawOnline))
	if rollback == nil || rollback.Status != "rollback_applied" || rollback.RemovedRules == 0 {
		t.Fatalf("expected restart rollback receipt, got %#v", rollback)
	}
	if len(restartedFirewall.removed) != rollback.RemovedRules {
		t.Fatalf("removed rules = %d, want %d", len(restartedFirewall.removed), rollback.RemovedRules)
	}
	cleared := loadAgentState(stateDir)
	if _, ok := cleared[networkPolicyStateKey]; ok {
		t.Fatalf("rollback state was not cleared: %#v", cleared[networkPolicyStateKey])
	}
}

func TestNetworkPolicyApplierKeepsRollbackStateWhenRollbackFails(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	desired, verifier := signedTestNetworkPolicy(t, "airgapped")
	publicKeyPath := writeTestNetworkPolicyPublicKey(t, stateDir, verifier.publicKey)
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	applier := makeNetworkPolicyApplierWithOptions(zap.NewNop(), networkPolicyApplierOptions{
		Now:           func() time.Time { return now },
		PublicKeyPath: publicKeyPath,
		Enforce:       true,
		Firewall:      func() (networkPolicyFirewall, error) { return &fakeNetworkPolicyFirewall{}, nil },
		StateDir:      stateDir,
	})
	rawDesired, err := json.Marshal(desired)
	if err != nil {
		t.Fatalf("marshal desired: %v", err)
	}
	if receipt := applier(context.Background(), json.RawMessage(rawDesired)); receipt == nil || receipt.Status != "applied" {
		t.Fatalf("expected applied receipt, got %#v", receipt)
	}

	online := desired
	online.Mode = "online"
	online.Active = false
	online.LocalOnly = false
	online.Enforcement.DefaultInboundAction = "allow"
	online.Enforcement.DefaultOutboundAction = "allow"
	online.Signature = ""
	online.SignatureAlgorithm = ""
	online.SignatureKeyID = ""
	online.DesiredStateID = computeNetworkPolicyDesiredStateID(online)
	rawOnline, err := json.Marshal(online)
	if err != nil {
		t.Fatalf("marshal online desired state: %v", err)
	}
	rollbackFirewall := &fakeNetworkPolicyFirewall{removeErr: errors.New("remove failed")}
	restarted := makeNetworkPolicyApplierWithOptions(zap.NewNop(), networkPolicyApplierOptions{
		Now:           func() time.Time { return now.Add(time.Minute) },
		PublicKeyPath: publicKeyPath,
		Enforce:       true,
		Firewall: func() (networkPolicyFirewall, error) {
			return rollbackFirewall, nil
		},
		StateDir: stateDir,
	})
	rollback := restarted(context.Background(), json.RawMessage(rawOnline))
	if rollback == nil || rollback.Status != "failed" {
		t.Fatalf("expected failed rollback receipt, got %#v", rollback)
	}
	if len(rollbackFirewall.removed) == 0 {
		t.Fatalf("expected rollback to attempt rule removal")
	}
	if persisted, err := loadPersistedNetworkPolicyState(stateDir, verifier); err != nil || persisted == nil || persisted.DesiredStateID != desired.DesiredStateID {
		t.Fatalf("rollback state should remain retryable after failure, state=%#v err=%v", persisted, err)
	}
	rollbackFirewall.removeErr = nil
	retry := restarted(context.Background(), json.RawMessage(rawOnline))
	if retry == nil || retry.Status != "rollback_applied" {
		t.Fatalf("expected failed rollback to retry without cadence suppression, got %#v", retry)
	}
}

func TestNetworkPolicyApplierPersistsPartialApplyFailureForRollback(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	desired, verifier := signedTestNetworkPolicy(t, "airgapped")
	publicKeyPath := writeTestNetworkPolicyPublicKey(t, stateDir, verifier.publicKey)
	fw := &fakeNetworkPolicyFirewall{applyErrAfter: 1}
	applier := makeNetworkPolicyApplierWithOptions(zap.NewNop(), networkPolicyApplierOptions{
		Now:           func() time.Time { return time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC) },
		PublicKeyPath: publicKeyPath,
		Enforce:       true,
		Firewall:      func() (networkPolicyFirewall, error) { return fw, nil },
		StateDir:      stateDir,
	})
	rawDesired, err := json.Marshal(desired)
	if err != nil {
		t.Fatalf("marshal desired: %v", err)
	}

	receipt := applier(context.Background(), json.RawMessage(rawDesired))
	if receipt == nil || receipt.Status != "failed" || receipt.AppliedRules == 0 {
		t.Fatalf("expected partial apply failure receipt, got %#v", receipt)
	}
	if persisted, err := loadPersistedNetworkPolicyState(stateDir, verifier); err != nil || persisted == nil || persisted.DesiredStateID != desired.DesiredStateID {
		t.Fatalf("partial apply should persist rollback state, state=%#v err=%v", persisted, err)
	}
}

func TestParseNetworkPolicyPublicKeyAcceptsPEM(t *testing.T) {
	t.Parallel()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	parsed, err := parseNetworkPolicyPublicKey(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if string(parsed) != string(pub) {
		t.Fatalf("parsed key mismatch")
	}
}

func signedTestNetworkPolicy(t *testing.T, mode string) (networkPolicyDesiredState, networkPolicyVerifier) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	desired := networkPolicyDesiredState{
		SchemaVersion: "network_policy.desired_state.v1",
		PolicyClass:   "enforcement",
		Mode:          mode,
		Active:        mode != "online",
		LocalOnly:     mode == "airgapped",
		AllowlistCIDRs: []string{
			"10.0.0.0/8",
		},
		Enforcement: networkPolicyEnforcementIntent{
			Engine:                "host_firewall",
			DefaultInboundAction:  "block",
			DefaultOutboundAction: "allow",
			AllowlistCIDRs:        []string{"10.0.0.0/8"},
			ApplicationScope:      "unsupported",
		},
		SourceRefs: []string{"node.labels"},
	}
	if mode == "airgapped" {
		desired.Enforcement.DefaultOutboundAction = "block"
	}
	desired.DesiredStateID = computeNetworkPolicyDesiredStateID(desired)
	desired.SignatureAlgorithm = "ed25519"
	desired.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(desired.DesiredStateID)))
	return desired, networkPolicyVerifier{publicKey: pub}
}

func writeTestNetworkPolicyPublicKey(t *testing.T, dir string, pub ed25519.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	path := filepath.Join(dir, "policy-public.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	return path
}

func nilNetworkPolicyFirewallProvider() (networkPolicyFirewall, error) {
	return nil, firewall.ErrNoBackend
}

type fakeNetworkPolicyFirewall struct {
	applied       []firewall.Rule
	removed       []firewall.Rule
	existing      []firewall.Rule
	removeErr     error
	applyErrAfter int
}

func (f *fakeNetworkPolicyFirewall) Backend() firewall.Backend {
	return fakeNetworkPolicyBackend{}
}

func (f *fakeNetworkPolicyFirewall) Apply(_ context.Context, rule firewall.Rule) error {
	f.applied = append(f.applied, rule)
	if f.applyErrAfter > 0 && len(f.applied) > f.applyErrAfter {
		return errors.New("apply failed")
	}
	return nil
}

func (f *fakeNetworkPolicyFirewall) Remove(_ context.Context, rule firewall.Rule) error {
	f.removed = append(f.removed, rule)
	if f.removeErr != nil {
		return f.removeErr
	}
	return nil
}

func (f *fakeNetworkPolicyFirewall) List(_ context.Context, _ string) ([]firewall.Rule, error) {
	return append([]firewall.Rule(nil), f.existing...), nil
}

type fakeNetworkPolicyBackend struct{}

func (fakeNetworkPolicyBackend) Name() string                                { return "fake-firewall" }
func (fakeNetworkPolicyBackend) Available() bool                             { return true }
func (fakeNetworkPolicyBackend) Apply(context.Context, firewall.Rule) error  { return nil }
func (fakeNetworkPolicyBackend) Remove(context.Context, firewall.Rule) error { return nil }
func (fakeNetworkPolicyBackend) List(context.Context, string) ([]firewall.Rule, error) {
	return nil, nil
}

func TestNetworkPolicyReceiptQueueDrains(t *testing.T) {
	networkPolicyReceiptMu.Lock()
	networkPolicyReceiptQueue = nil
	networkPolicyReceiptMu.Unlock()

	enqueueNetworkPolicyReceipt(networkPolicyReceipt{DesiredStateID: "sha256:a", Status: "planned"})
	enqueueNetworkPolicyReceipt(networkPolicyReceipt{DesiredStateID: "sha256:b", Status: "no_op"})

	got := drainNetworkPolicyReceipts()
	if len(got) != 2 {
		t.Fatalf("drained %d receipts, want 2", len(got))
	}
	if again := drainNetworkPolicyReceipts(); len(again) != 0 {
		t.Fatalf("queue should be empty, got %#v", again)
	}
}

func containsTestString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
