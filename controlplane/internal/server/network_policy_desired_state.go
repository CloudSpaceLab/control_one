package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const (
	networkPolicySchemaVersion = "network_policy.desired_state.v1"
	networkPolicyCapability    = "network_policy_desired_state.v1"
	networkPolicyClass         = "enforcement"
	networkPolicySourceLabels  = "node.labels"
)

type nodeNetworkPolicyTemplate struct {
	Mode                string   `json:"mode"`
	LocalOnly           *bool    `json:"local_only,omitempty"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	DurationSeconds     int64    `json:"duration_seconds,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	AllowedApplications []string `json:"allowed_applications,omitempty"`
	AllowlistCIDRs      []string `json:"allowlist_cidrs,omitempty"`
}

func (s *Server) compileNodeNetworkPolicy(ctx context.Context, node storage.Node, now time.Time) *nodeNetworkPolicy {
	posture := nodeIsolationPostureFromNode(node, now)
	sourceRefs := []string{networkPolicySourceLabels}
	var templateErrors []string

	if s != nil && s.store != nil {
		policies, err := s.store.GetEffectivePolicies(ctx, node.TenantID, node.ID)
		if err != nil {
			templateErrors = append(templateErrors, "effective_policy_lookup_failed")
		} else if len(policies) > 0 {
			var refs []string
			posture, refs, templateErrors = applyNetworkPolicyTemplates(posture, policies, now, templateErrors)
			sourceRefs = append(sourceRefs, refs...)
		}
	}

	policy := newNodeNetworkPolicy(posture, now, sourceRefs, templateErrors)
	s.signNodeNetworkPolicy(policy)
	return policy
}

func applyNetworkPolicyTemplates(posture nodeIsolationPosture, policies []storage.PolicyWithVersion, now time.Time, templateErrors []string) (nodeIsolationPosture, []string, []string) {
	refs := make([]string, 0, len(policies))
	for _, p := range policies {
		if !isNetworkPolicyTemplate(p) {
			continue
		}
		tpl, err := parseNetworkPolicyTemplate(p.RuleDefinition)
		if err != nil {
			templateErrors = append(templateErrors, fmt.Sprintf("%s:%s", p.ID.String(), err.Error()))
			continue
		}
		next, err := tpl.toPosture(now)
		if err != nil {
			templateErrors = append(templateErrors, fmt.Sprintf("%s:%s", p.ID.String(), err.Error()))
			continue
		}
		if isolationModeRankValue(next.Mode) >= isolationModeRankValue(posture.Mode) {
			posture.Mode = next.Mode
			posture.Active = next.Active
			posture.LocalOnly = next.LocalOnly
			posture.ExpiresAt = next.ExpiresAt
			posture.Expired = next.Expired
			if next.Reason != "" {
				posture.Reason = next.Reason
			}
		}
		posture.AllowedApplications = mergeStrings(posture.AllowedApplications, next.AllowedApplications, 50)
		posture.AllowlistCIDRs = mergeStrings(posture.AllowlistCIDRs, next.AllowlistCIDRs, 100)
		refs = append(refs, "policy:"+p.ID.String()+":v"+fmt.Sprint(p.Version))
	}
	return posture, refs, templateErrors
}

func isNetworkPolicyTemplate(p storage.PolicyWithVersion) bool {
	ruleType := strings.ToLower(strings.TrimSpace(p.RuleType))
	if ruleType == "network_policy" || ruleType == "posture_template" {
		return true
	}
	for k, v := range p.Labels {
		if strings.EqualFold(strings.TrimSpace(k), "control_one.policy.class") && strings.EqualFold(strings.TrimSpace(v), "network_policy") {
			return true
		}
	}
	return false
}

func parseNetworkPolicyTemplate(raw string) (nodeNetworkPolicyTemplate, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nodeNetworkPolicyTemplate{}, fmt.Errorf("invalid_json")
	}
	if key := firstCapturePolicyKey(envelope); key != "" {
		return nodeNetworkPolicyTemplate{}, fmt.Errorf("capture_filter_key_%s_not_allowed", key)
	}
	payload := []byte(raw)
	if nested, ok := envelope["network_policy"]; ok {
		payload = nested
		var nestedEnvelope map[string]json.RawMessage
		if err := json.Unmarshal(nested, &nestedEnvelope); err == nil {
			if key := firstCapturePolicyKey(nestedEnvelope); key != "" {
				return nodeNetworkPolicyTemplate{}, fmt.Errorf("capture_filter_key_%s_not_allowed", key)
			}
		}
	}
	var tpl nodeNetworkPolicyTemplate
	if err := json.Unmarshal(payload, &tpl); err != nil {
		return nodeNetworkPolicyTemplate{}, fmt.Errorf("invalid_network_policy")
	}
	return tpl, nil
}

func firstCapturePolicyKey(m map[string]json.RawMessage) string {
	captureKeys := map[string]struct{}{
		"capture_external":          {},
		"capture_internal_summary":  {},
		"capture_listening_changes": {},
		"capture_files":             {},
		"capture_db_queries":        {},
		"threat_match_full":         {},
		"db_query_text_capture":     {},
		"forensic_mode":             {},
		"file_paths_watch":          {},
		"denylist_cidrs":            {},
		"trusted_proxy_cidrs":       {},
	}
	for k := range m {
		if _, ok := captureKeys[strings.ToLower(strings.TrimSpace(k))]; ok {
			return strings.ToLower(strings.TrimSpace(k))
		}
	}
	return ""
}

func (tpl nodeNetworkPolicyTemplate) toPosture(now time.Time) (nodeIsolationPosture, error) {
	mode := normalizeIsolationMode(tpl.Mode)
	if mode == "" {
		return nodeIsolationPosture{}, fmt.Errorf("invalid_mode")
	}
	var expiresAt *time.Time
	if strings.TrimSpace(tpl.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(tpl.ExpiresAt))
		if err != nil {
			return nodeIsolationPosture{}, fmt.Errorf("invalid_expires_at")
		}
		parsed = parsed.UTC()
		expiresAt = &parsed
	}
	if tpl.DurationSeconds > 0 {
		parsed := now.Add(time.Duration(tpl.DurationSeconds) * time.Second).UTC()
		expiresAt = &parsed
	}
	expired := false
	active := mode != isolationModeOnline
	if expiresAt != nil && !expiresAt.After(now) {
		mode = isolationModeOnline
		active = false
		expired = true
	}
	localOnly := mode == isolationModeAirgapped
	if tpl.LocalOnly != nil {
		localOnly = *tpl.LocalOnly
	}
	return nodeIsolationPosture{
		Mode:                mode,
		Active:              active,
		Expired:             expired,
		LocalOnly:           localOnly,
		ExpiresAt:           expiresAt,
		Reason:              strings.TrimSpace(tpl.Reason),
		AllowedApplications: sanitizeStringSlice(tpl.AllowedApplications, 50),
		AllowlistCIDRs:      sanitizeCIDRSlice(tpl.AllowlistCIDRs, 100),
	}, nil
}

func newNodeNetworkPolicy(posture nodeIsolationPosture, now time.Time, sourceRefs, templateErrors []string) *nodeNetworkPolicy {
	mode := posture.Mode
	if mode == "" {
		mode = isolationModeOnline
	}
	out := &nodeNetworkPolicy{
		SchemaVersion:       networkPolicySchemaVersion,
		PolicyClass:         networkPolicyClass,
		CompiledAt:          now.UTC().Format(time.RFC3339),
		Mode:                mode,
		Active:              posture.Active,
		LocalOnly:           posture.LocalOnly,
		Reason:              posture.Reason,
		AllowedApplications: sanitizeStringSlice(posture.AllowedApplications, 50),
		AllowlistCIDRs:      sanitizeStringSlice(posture.AllowlistCIDRs, 100),
		SourceRefs:          sortedStrings(sourceRefs),
		TemplateErrors:      sortedStrings(templateErrors),
		Enforcement: nodeNetworkPolicyEnforcement{
			Engine:                "host_firewall",
			DefaultInboundAction:  "allow",
			DefaultOutboundAction: "allow",
			AllowlistCIDRs:        sanitizeStringSlice(posture.AllowlistCIDRs, 100),
			ApplicationScope:      "unsupported",
		},
	}
	if posture.Active {
		out.Enforcement.DefaultInboundAction = "block"
	}
	if posture.LocalOnly || posture.Mode == isolationModeAirgapped {
		out.Enforcement.DefaultOutboundAction = "block"
	}
	if len(out.AllowedApplications) > 0 {
		out.Enforcement.UnsupportedControls = append(out.Enforcement.UnsupportedControls, "application_allowlist")
	}
	if posture.ExpiresAt != nil {
		formatted := posture.ExpiresAt.UTC().Format(time.RFC3339)
		out.ExpiresAt = &formatted
	}
	out.DesiredStateID = computeNodeNetworkPolicyID(out)
	return out
}

func computeNodeNetworkPolicyID(policy *nodeNetworkPolicy) string {
	expiresAt := ""
	if policy.ExpiresAt != nil {
		expiresAt = *policy.ExpiresAt
	}
	payload := struct {
		SchemaVersion       string                       `json:"schema_version"`
		PolicyClass         string                       `json:"policy_class"`
		Mode                string                       `json:"mode"`
		Active              bool                         `json:"active"`
		LocalOnly           bool                         `json:"local_only"`
		ExpiresAt           string                       `json:"expires_at,omitempty"`
		Reason              string                       `json:"reason,omitempty"`
		AllowedApplications []string                     `json:"allowed_applications,omitempty"`
		AllowlistCIDRs      []string                     `json:"allowlist_cidrs,omitempty"`
		SourceRefs          []string                     `json:"source_refs,omitempty"`
		TemplateErrors      []string                     `json:"template_errors,omitempty"`
		Enforcement         nodeNetworkPolicyEnforcement `json:"enforcement"`
	}{
		SchemaVersion:       policy.SchemaVersion,
		PolicyClass:         policy.PolicyClass,
		Mode:                policy.Mode,
		Active:              policy.Active,
		LocalOnly:           policy.LocalOnly,
		ExpiresAt:           expiresAt,
		Reason:              policy.Reason,
		AllowedApplications: sortedStrings(policy.AllowedApplications),
		AllowlistCIDRs:      sortedStrings(policy.AllowlistCIDRs),
		SourceRefs:          sortedStrings(policy.SourceRefs),
		TemplateErrors:      sortedStrings(policy.TemplateErrors),
		Enforcement:         policy.Enforcement,
	}
	payload.Enforcement.AllowlistCIDRs = sortedStrings(payload.Enforcement.AllowlistCIDRs)
	payload.Enforcement.UnsupportedControls = sortedStrings(payload.Enforcement.UnsupportedControls)
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Server) signNodeNetworkPolicy(policy *nodeNetworkPolicy) {
	if s == nil || policy == nil || s.cfg == nil {
		return
	}
	mat := s.policySigningMaterial()
	if mat == nil || !mat.haveSigningKey || len(mat.privateKey) != ed25519.PrivateKeySize {
		return
	}
	sig := ed25519.Sign(mat.privateKey, []byte(policy.DesiredStateID))
	policy.Signature = base64.StdEncoding.EncodeToString(sig)
	policy.SignatureAlgorithm = "ed25519"
	policy.SignatureKeyID = mat.fingerprint
}

func (s *Server) policySigningMaterial() *agentSigningMaterial {
	if s == nil {
		return nil
	}
	s.policySigningOnce.Do(func() {
		s.policySigning = s.resolvePolicySigningMaterial()
	})
	return s.policySigning
}

func (s *Server) resolvePolicySigningMaterial() *agentSigningMaterial {
	mat := &agentSigningMaterial{}
	if s == nil || s.cfg == nil {
		return mat
	}
	if keyPath := strings.TrimSpace(s.cfg.Policy.SigningKeyPath); keyPath != "" {
		priv, err := loadEd25519PrivateKey(keyPath)
		if err != nil {
			s.logger.Warn("policy signing key unavailable; desired state will be unsigned",
				zap.String("path", keyPath), zap.Error(err))
		} else {
			mat.privateKey = priv
			mat.haveSigningKey = true
		}
	}
	if pubPath := strings.TrimSpace(s.cfg.Policy.PublicKeyFile); pubPath != "" {
		pemBytes, err := os.ReadFile(pubPath)
		if err != nil {
			s.logger.Warn("policy public key unavailable", zap.String("path", pubPath), zap.Error(err))
		} else if pub, fp, err := parseEd25519PublicKeyPEM(pemBytes); err != nil {
			s.logger.Warn("policy public key parse failed", zap.String("path", pubPath), zap.Error(err))
		} else {
			mat.publicKey = pub
			mat.publicKeyPEM = pemBytes
			mat.fingerprint = fp
			mat.havePublicKey = true
		}
	} else if mat.haveSigningKey {
		if pub, ok := mat.privateKey.Public().(ed25519.PublicKey); ok {
			mat.publicKey = pub
			mat.fingerprint = ed25519Fingerprint(pub)
			mat.havePublicKey = true
			if pemBytes, err := encodeEd25519PublicKeyPEM(pub); err == nil {
				mat.publicKeyPEM = pemBytes
			}
		}
	}
	if mat.haveSigningKey && mat.havePublicKey {
		if derived, ok := mat.privateKey.Public().(ed25519.PublicKey); ok && !bytes.Equal(derived, mat.publicKey) {
			s.logger.Warn("policy signing key does not match configured policy public key; desired state will be unsigned")
			mat.privateKey = nil
			mat.haveSigningKey = false
		}
	}
	return mat
}

func sanitizeCIDRSlice(in []string, limit int) []string {
	candidates := sanitizeStringSlice(in, limit)
	out := make([]string, 0, len(candidates))
	for _, s := range candidates {
		if _, _, err := net.ParseCIDR(s); err == nil {
			out = append(out, s)
		}
	}
	return out
}

func mergeStrings(a, b []string, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(a, b...) {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func sortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func isolationModeRankValue(mode string) int {
	switch normalizeIsolationMode(mode) {
	case isolationModeAirgapped:
		return 2
	case isolationModeWhitelist:
		return 1
	default:
		return 0
	}
}
