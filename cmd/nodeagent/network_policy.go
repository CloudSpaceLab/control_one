package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/firewall"
)

const networkPolicyReceiptQueueCap = 128
const networkPolicyDriftReceiptInterval = 10 * time.Minute
const networkPolicyStateKey = "network_policy_last_enforced"

type networkPolicyApplierOptions struct {
	ReceiptInterval time.Duration
	Now             func() time.Time
	PublicKeyPath   string
	Enforce         bool
	Firewall        func() (networkPolicyFirewall, error)
	StateDir        string
}

type networkPolicyFirewall interface {
	Backend() firewall.Backend
	Apply(context.Context, firewall.Rule) error
	Remove(context.Context, firewall.Rule) error
	List(context.Context, string) ([]firewall.Rule, error)
}

type networkPolicyDesiredState struct {
	SchemaVersion       string                         `json:"schema_version"`
	PolicyClass         string                         `json:"policy_class"`
	DesiredStateID      string                         `json:"desired_state_id"`
	CompiledAt          string                         `json:"compiled_at"`
	Mode                string                         `json:"mode"`
	Active              bool                           `json:"active"`
	LocalOnly           bool                           `json:"local_only"`
	ExpiresAt           *string                        `json:"expires_at,omitempty"`
	Reason              string                         `json:"reason,omitempty"`
	AllowedApplications []string                       `json:"allowed_applications,omitempty"`
	AllowlistCIDRs      []string                       `json:"allowlist_cidrs,omitempty"`
	SourceRefs          []string                       `json:"source_refs,omitempty"`
	TemplateErrors      []string                       `json:"template_errors,omitempty"`
	Enforcement         networkPolicyEnforcementIntent `json:"enforcement"`
	Signature           string                         `json:"signature,omitempty"`
	SignatureAlgorithm  string                         `json:"signature_algorithm,omitempty"`
	SignatureKeyID      string                         `json:"signature_key_id,omitempty"`
}

type networkPolicyEnforcementIntent struct {
	Engine                string   `json:"engine"`
	DefaultInboundAction  string   `json:"default_inbound_action"`
	DefaultOutboundAction string   `json:"default_outbound_action"`
	AllowlistCIDRs        []string `json:"allowlist_cidrs,omitempty"`
	ApplicationScope      string   `json:"application_scope"`
	UnsupportedControls   []string `json:"unsupported_controls,omitempty"`
}

type networkPolicyReceipt struct {
	DesiredStateID    string   `json:"desired_state_id"`
	SchemaVersion     string   `json:"schema_version"`
	Mode              string   `json:"mode"`
	Status            string   `json:"status"`
	Backend           string   `json:"backend,omitempty"`
	DryRun            bool     `json:"dry_run"`
	PlannedRules      int      `json:"planned_rules"`
	AppliedRules      int      `json:"applied_rules"`
	RemovedRules      int      `json:"removed_rules,omitempty"`
	MissingControls   []string `json:"missing_controls,omitempty"`
	Drift             []string `json:"drift,omitempty"`
	Error             string   `json:"error,omitempty"`
	SignaturePresent  bool     `json:"signature_present"`
	SignatureValid    bool     `json:"signature_valid,omitempty"`
	SignatureKeyID    string   `json:"signature_key_id,omitempty"`
	ObservedAt        string   `json:"observed_at"`
	RollbackAvailable bool     `json:"rollback_available"`
}

type networkPolicyVerifier struct {
	publicKey ed25519.PublicKey
	keyID     string
	loadErr   error
}

type networkPolicyPersistedState struct {
	DesiredStateID string                    `json:"desired_state_id"`
	DesiredState   networkPolicyDesiredState `json:"desired_state"`
	PersistedAt    string                    `json:"persisted_at"`
}

var (
	networkPolicyReceiptMu    sync.Mutex
	networkPolicyReceiptQueue []networkPolicyReceipt
)

func enqueueNetworkPolicyReceipt(r networkPolicyReceipt) {
	networkPolicyReceiptMu.Lock()
	defer networkPolicyReceiptMu.Unlock()
	if len(networkPolicyReceiptQueue) >= networkPolicyReceiptQueueCap {
		networkPolicyReceiptQueue = networkPolicyReceiptQueue[1:]
	}
	networkPolicyReceiptQueue = append(networkPolicyReceiptQueue, r)
}

func drainNetworkPolicyReceipts() []networkPolicyReceipt {
	networkPolicyReceiptMu.Lock()
	defer networkPolicyReceiptMu.Unlock()
	if len(networkPolicyReceiptQueue) == 0 {
		return nil
	}
	out := make([]networkPolicyReceipt, len(networkPolicyReceiptQueue))
	copy(out, networkPolicyReceiptQueue)
	networkPolicyReceiptQueue = networkPolicyReceiptQueue[:0]
	return out
}

func makeNetworkPolicyApplier(log *zap.Logger) NetworkPolicyApplier {
	return makeNetworkPolicyApplierWithOptions(log, networkPolicyApplierOptions{
		ReceiptInterval: networkPolicyDriftReceiptInterval,
	})
}

func makeNetworkPolicyApplierWithOptions(log *zap.Logger, opts networkPolicyApplierOptions) NetworkPolicyApplier {
	var mu sync.Mutex
	lastDesiredStateID := ""
	lastReceiptAt := time.Time{}
	lastReceiptStatus := ""
	var lastEnforced *networkPolicyDesiredState
	if opts.ReceiptInterval <= 0 {
		opts.ReceiptInterval = networkPolicyDriftReceiptInterval
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	verifier := loadNetworkPolicyVerifier(opts.PublicKeyPath)
	if verifier.loadErr != nil && log != nil {
		log.Warn("network policy signature verification unavailable",
			zap.String("public_key_path", opts.PublicKeyPath),
			zap.Error(verifier.loadErr),
		)
	}
	if opts.Enforce {
		persisted, err := loadPersistedNetworkPolicyState(opts.StateDir, verifier)
		if err != nil && log != nil {
			log.Warn("network policy persisted rollback state ignored",
				zap.String("state_dir", opts.StateDir),
				zap.Error(err),
			)
		} else if persisted != nil {
			lastEnforced = persisted
			if log != nil {
				log.Info("network policy rollback state restored",
					zap.String("state_dir", opts.StateDir),
					zap.String("desired_state_id", persisted.DesiredStateID),
				)
			}
		}
	}
	firewallProvider := opts.Firewall
	if firewallProvider == nil {
		firewallProvider = func() (networkPolicyFirewall, error) {
			return ensureFirewallManager()
		}
	}
	return func(ctx context.Context, raw json.RawMessage) *networkPolicyReceipt {
		now := nowFn().UTC()
		var desired networkPolicyDesiredState
		if err := json.Unmarshal(raw, &desired); err != nil {
			return &networkPolicyReceipt{
				Status:     "failed",
				Error:      "decode network policy: " + err.Error(),
				ObservedAt: now.Format(time.RFC3339),
			}
		}
		desired.DesiredStateID = strings.TrimSpace(desired.DesiredStateID)
		if desired.DesiredStateID == "" {
			desired.DesiredStateID = "unknown"
		}

		mu.Lock()
		var previous *networkPolicyDesiredState
		if lastEnforced != nil {
			copyPrevious := *lastEnforced
			previous = &copyPrevious
		}
		if desired.DesiredStateID == lastDesiredStateID && lastReceiptStatus != "failed" && now.Sub(lastReceiptAt) < opts.ReceiptInterval {
			mu.Unlock()
			return nil
		}
		lastDesiredStateID = desired.DesiredStateID
		lastReceiptAt = now
		mu.Unlock()

		receipt := buildNetworkPolicyReceipt(ctx, desired, now, verifier, opts.Enforce, firewallProvider, previous)
		mu.Lock()
		switch {
		case receipt.Status == "applied" || shouldPersistNetworkPolicyRollbackState(receipt, desired, opts.Enforce):
			if err := savePersistedNetworkPolicyState(opts.StateDir, &desired, now); err != nil {
				if receipt.Error != "" {
					receipt.Error += "; "
				}
				receipt.Status = "failed"
				receipt.Error += "persist network policy rollback state: " + err.Error()
				receipt.MissingControls = appendUniqueString(receipt.MissingControls, "network_policy_state_persistence")
			}
			copyDesired := desired
			lastEnforced = &copyDesired
		case receipt.Status == "rollback_applied":
			if err := savePersistedNetworkPolicyState(opts.StateDir, nil, now); err != nil {
				receipt.Status = "failed"
				receipt.Error = "clear network policy rollback state: " + err.Error()
				receipt.MissingControls = appendUniqueString(receipt.MissingControls, "network_policy_state_persistence")
			}
			lastEnforced = nil
		}
		lastReceiptStatus = receipt.Status
		mu.Unlock()
		if log != nil {
			log.Info("network policy desired state evaluated",
				zap.String("desired_state_id", receipt.DesiredStateID),
				zap.String("mode", receipt.Mode),
				zap.String("status", receipt.Status),
				zap.String("backend", receipt.Backend),
				zap.Int("planned_rules", receipt.PlannedRules),
				zap.Strings("missing_controls", receipt.MissingControls),
			)
		}
		return &receipt
	}
}

func shouldPersistNetworkPolicyRollbackState(receipt networkPolicyReceipt, desired networkPolicyDesiredState, enforce bool) bool {
	return enforce && desired.Active && receipt.SignatureValid && receipt.AppliedRules > 0
}

func loadPersistedNetworkPolicyState(stateDir string, verifier networkPolicyVerifier) (*networkPolicyDesiredState, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil, nil
	}
	raw, ok := loadAgentState(stateDir)[networkPolicyStateKey]
	if !ok || raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal persisted network policy state: %w", err)
	}
	var persisted networkPolicyPersistedState
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, fmt.Errorf("decode persisted network policy state: %w", err)
	}
	if strings.TrimSpace(persisted.DesiredState.DesiredStateID) == "" {
		return nil, errors.New("persisted network policy state missing desired_state_id")
	}
	if persisted.DesiredStateID != "" && !strings.EqualFold(persisted.DesiredStateID, persisted.DesiredState.DesiredStateID) {
		return nil, errors.New("persisted network policy state id mismatch")
	}
	if !persisted.DesiredState.Active {
		return nil, errors.New("persisted network policy state is inactive")
	}
	if err := validateNetworkPolicyTrust(persisted.DesiredState, verifier); err != nil {
		return nil, fmt.Errorf("persisted network policy state failed trust validation: %w", err)
	}
	desired := persisted.DesiredState
	return &desired, nil
}

func savePersistedNetworkPolicyState(stateDir string, desired *networkPolicyDesiredState, now time.Time) error {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil
	}
	state := loadAgentState(stateDir)
	if desired == nil {
		delete(state, networkPolicyStateKey)
		return saveAgentState(stateDir, state)
	}
	state[networkPolicyStateKey] = networkPolicyPersistedState{
		DesiredStateID: desired.DesiredStateID,
		DesiredState:   *desired,
		PersistedAt:    now.UTC().Format(time.RFC3339),
	}
	return saveAgentState(stateDir, state)
}

func buildNetworkPolicyReceipt(ctx context.Context, desired networkPolicyDesiredState, now time.Time, verifier networkPolicyVerifier, enforce bool, firewallProvider func() (networkPolicyFirewall, error), previous *networkPolicyDesiredState) networkPolicyReceipt {
	receipt := networkPolicyReceipt{
		DesiredStateID:    desired.DesiredStateID,
		SchemaVersion:     desired.SchemaVersion,
		Mode:              desired.Mode,
		Status:            "planned_dry_run",
		DryRun:            true,
		SignaturePresent:  strings.TrimSpace(desired.Signature) != "",
		SignatureKeyID:    desired.SignatureKeyID,
		ObservedAt:        now.UTC().Format(time.RFC3339),
		RollbackAvailable: false,
	}
	if !desired.Active {
		if enforce && previous != nil {
			removed, err := rollbackNetworkPolicyRules(ctx, firewallProvider, *previous)
			receipt.DryRun = false
			receipt.RemovedRules = removed
			receipt.RollbackAvailable = false
			if err != nil {
				receipt.Status = "failed"
				receipt.Error = err.Error()
			} else {
				receipt.Status = "rollback_applied"
			}
			return receipt
		}
		receipt.Status = "no_op"
		return receipt
	}
	if err := validateNetworkPolicyTrust(desired, verifier); err != nil {
		receipt.Status = "failed"
		receipt.Error = err.Error()
		receipt.MissingControls = appendUniqueString(receipt.MissingControls, "policy_signature")
		return receipt
	}
	receipt.SignatureValid = true

	mgr, err := firewallProvider()
	if err != nil || mgr == nil || mgr.Backend() == nil {
		receipt.Status = "unsupported"
		receipt.Error = "no firewall backend available"
		if err != nil {
			receipt.Error = err.Error()
		}
		receipt.MissingControls = append(receipt.MissingControls, "host_firewall_backend")
		return receipt
	}
	receipt.Backend = mgr.Backend().Name()

	planned := plannedNetworkPolicyRules(desired)
	receipt.PlannedRules = len(planned)
	receipt.MissingControls = append(receipt.MissingControls, desired.Enforcement.UnsupportedControls...)
	if desired.Enforcement.ApplicationScope == "unsupported" && len(desired.AllowedApplications) > 0 {
		receipt.MissingControls = appendUniqueString(receipt.MissingControls, "application_allowlist")
	}
	if len(planned) == 0 {
		receipt.Status = "unsupported"
		receipt.MissingControls = appendUniqueString(receipt.MissingControls, "firewall_rule_shape")
		return receipt
	}

	existing, listErr := mgr.List(ctx, networkPolicyRuleTag(desired))
	if listErr == nil && len(existing) < len(planned) {
		receipt.Drift = append(receipt.Drift, "planned_rules_not_present")
		receipt.Status = "drift_detected"
	}
	if !enforce {
		return receipt
	}
	receipt.DryRun = false
	receipt.RollbackAvailable = true
	if previous != nil && !strings.EqualFold(previous.DesiredStateID, desired.DesiredStateID) {
		removed, err := rollbackNetworkPolicyRules(ctx, firewallProvider, *previous)
		receipt.RemovedRules = removed
		if err != nil {
			receipt.Status = "failed"
			receipt.Error = err.Error()
			return receipt
		}
	}
	if listErr == nil && len(existing) >= len(planned) {
		receipt.Status = "applied"
		receipt.AppliedRules = len(planned)
		receipt.Drift = nil
		return receipt
	}
	applied, err := applyNetworkPolicyRules(ctx, mgr, planned)
	receipt.AppliedRules = applied
	if err != nil {
		receipt.Status = "failed"
		receipt.Error = err.Error()
		return receipt
	}
	receipt.Status = "applied"
	receipt.Drift = nil
	return receipt
}

func applyNetworkPolicyRules(ctx context.Context, mgr networkPolicyFirewall, rules []firewall.Rule) (int, error) {
	if mgr == nil {
		return 0, errors.New("no firewall backend available")
	}
	applied := 0
	for _, rule := range rules {
		if err := mgr.Apply(ctx, rule); err != nil {
			return applied, fmt.Errorf("apply network policy rule %d: %w", applied+1, err)
		}
		applied++
	}
	return applied, nil
}

func rollbackNetworkPolicyRules(ctx context.Context, firewallProvider func() (networkPolicyFirewall, error), desired networkPolicyDesiredState) (int, error) {
	if firewallProvider == nil {
		return 0, errors.New("no firewall backend available")
	}
	mgr, err := firewallProvider()
	if err != nil || mgr == nil || mgr.Backend() == nil {
		if err != nil {
			return 0, err
		}
		return 0, errors.New("no firewall backend available")
	}
	removed := 0
	for _, rule := range plannedNetworkPolicyRules(desired) {
		if err := mgr.Remove(ctx, rule); err != nil {
			return removed, fmt.Errorf("rollback network policy rule %d: %w", removed+1, err)
		}
		removed++
	}
	return removed, nil
}

func validateNetworkPolicyTrust(desired networkPolicyDesiredState, verifier networkPolicyVerifier) error {
	if expectedID := computeNetworkPolicyDesiredStateID(desired); expectedID == "" || !strings.EqualFold(expectedID, desired.DesiredStateID) {
		return fmt.Errorf("network policy desired_state_id mismatch")
	}
	if strings.TrimSpace(desired.Signature) == "" {
		return errors.New("network policy signature missing")
	}
	if !strings.EqualFold(strings.TrimSpace(desired.SignatureAlgorithm), "ed25519") {
		return errors.New("network policy signature algorithm unsupported")
	}
	if verifier.loadErr != nil {
		return fmt.Errorf("network policy public key unavailable: %w", verifier.loadErr)
	}
	if len(verifier.publicKey) != ed25519.PublicKeySize {
		return errors.New("network policy public key unavailable")
	}
	if desired.SignatureKeyID != "" && verifier.keyID != "" && !strings.EqualFold(desired.SignatureKeyID, verifier.keyID) {
		return errors.New("network policy signature key mismatch")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(desired.Signature))
	if err != nil {
		return fmt.Errorf("network policy signature decode: %w", err)
	}
	if !ed25519.Verify(verifier.publicKey, []byte(desired.DesiredStateID), sig) {
		return errors.New("network policy signature verification failed")
	}
	return nil
}

func computeNetworkPolicyDesiredStateID(policy networkPolicyDesiredState) string {
	expiresAt := ""
	if policy.ExpiresAt != nil {
		expiresAt = *policy.ExpiresAt
	}
	payload := struct {
		SchemaVersion       string                         `json:"schema_version"`
		PolicyClass         string                         `json:"policy_class"`
		Mode                string                         `json:"mode"`
		Active              bool                           `json:"active"`
		LocalOnly           bool                           `json:"local_only"`
		ExpiresAt           string                         `json:"expires_at,omitempty"`
		Reason              string                         `json:"reason,omitempty"`
		AllowedApplications []string                       `json:"allowed_applications,omitempty"`
		AllowlistCIDRs      []string                       `json:"allowlist_cidrs,omitempty"`
		SourceRefs          []string                       `json:"source_refs,omitempty"`
		TemplateErrors      []string                       `json:"template_errors,omitempty"`
		Enforcement         networkPolicyEnforcementIntent `json:"enforcement"`
	}{
		SchemaVersion:       policy.SchemaVersion,
		PolicyClass:         policy.PolicyClass,
		Mode:                policy.Mode,
		Active:              policy.Active,
		LocalOnly:           policy.LocalOnly,
		ExpiresAt:           expiresAt,
		Reason:              policy.Reason,
		AllowedApplications: sortedStringCopy(policy.AllowedApplications),
		AllowlistCIDRs:      sortedStringCopy(policy.AllowlistCIDRs),
		SourceRefs:          sortedStringCopy(policy.SourceRefs),
		TemplateErrors:      sortedStringCopy(policy.TemplateErrors),
		Enforcement:         policy.Enforcement,
	}
	payload.Enforcement.AllowlistCIDRs = sortedStringCopy(payload.Enforcement.AllowlistCIDRs)
	payload.Enforcement.UnsupportedControls = sortedStringCopy(payload.Enforcement.UnsupportedControls)
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func loadNetworkPolicyVerifier(publicKeyPath string) networkPolicyVerifier {
	publicKeyPath = strings.TrimSpace(publicKeyPath)
	if publicKeyPath == "" {
		return networkPolicyVerifier{loadErr: errors.New("policy public key path not configured")}
	}
	data, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return networkPolicyVerifier{loadErr: fmt.Errorf("read policy public key: %w", err)}
	}
	pub, err := parseNetworkPolicyPublicKey(data)
	if err != nil {
		return networkPolicyVerifier{loadErr: err}
	}
	sum := sha256.Sum256(pub)
	return networkPolicyVerifier{
		publicKey: pub,
		keyID:     "sha256:" + hex.EncodeToString(sum[:]),
	}
}

func parseNetworkPolicyPublicKey(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block != nil {
		return parseNetworkPolicyPublicKeyDER(block.Bytes)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(data)))
	if err != nil {
		return nil, fmt.Errorf("unsupported policy public key encoding: %w", err)
	}
	if len(decoded) == ed25519.PublicKeySize {
		out := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(out, decoded)
		return out, nil
	}
	return parseNetworkPolicyPublicKeyDER(decoded)
}

func parseNetworkPolicyPublicKeyDER(der []byte) (ed25519.PublicKey, error) {
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse policy public key: %w", err)
	}
	key, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("policy public key is not ed25519")
	}
	return key, nil
}

func plannedNetworkPolicyRules(desired networkPolicyDesiredState) []firewall.Rule {
	tag := networkPolicyRuleTag(desired)
	reason := "control one network policy " + desired.DesiredStateID
	cidrs := desired.Enforcement.AllowlistCIDRs
	if len(cidrs) == 0 {
		cidrs = desired.AllowlistCIDRs
	}
	var rules []firewall.Rule
	if strings.EqualFold(desired.Enforcement.DefaultInboundAction, "block") {
		for _, cidr := range cidrs {
			rules = append(rules, firewall.Rule{Source: cidr, Direction: firewall.DirectionIn, Action: firewall.ActionAllow, Tag: tag, Comment: reason})
		}
		rules = append(rules,
			firewall.Rule{Source: "0.0.0.0/0", Direction: firewall.DirectionIn, Action: firewall.ActionBlock, Tag: tag, Comment: reason},
			firewall.Rule{Source: "::/0", Direction: firewall.DirectionIn, Action: firewall.ActionBlock, Tag: tag, Comment: reason},
		)
	}
	if strings.EqualFold(desired.Enforcement.DefaultOutboundAction, "block") {
		for _, cidr := range cidrs {
			rules = append(rules, firewall.Rule{Dest: cidr, Direction: firewall.DirectionOut, Action: firewall.ActionAllow, Tag: tag, Comment: reason})
		}
		rules = append(rules,
			firewall.Rule{Dest: "0.0.0.0/0", Direction: firewall.DirectionOut, Action: firewall.ActionBlock, Tag: tag, Comment: reason},
			firewall.Rule{Dest: "::/0", Direction: firewall.DirectionOut, Action: firewall.ActionBlock, Tag: tag, Comment: reason},
		)
	}
	return rules
}

func networkPolicyRuleTag(desired networkPolicyDesiredState) string {
	id := strings.TrimPrefix(strings.TrimSpace(desired.DesiredStateID), "sha256:")
	if len(id) > 16 {
		id = id[:16]
	}
	if id == "" {
		id = "unknown"
	}
	return "controlone:network_policy:" + id
}

func sortedStringCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}
