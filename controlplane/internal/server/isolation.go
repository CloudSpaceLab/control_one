package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const (
	isolationModeOnline    = "online"
	isolationModeWhitelist = "whitelist"
	isolationModeAirgapped = "airgapped"

	isolationModeLabel           = "control_one.isolation.mode"
	isolationExpiresAtLabel      = "control_one.isolation.expires_at"
	isolationReasonLabel         = "control_one.isolation.reason"
	isolationAllowAppsLabel      = "control_one.isolation.allowed_applications"
	isolationAllowCIDRsLabel     = "control_one.isolation.allowlist_cidrs"
	isolationLocalOnlyLabel      = "control_one.isolation.local_connectivity_only"
	isolationUpdatedAtLabel      = "control_one.isolation.updated_at"
	isolationUpdatedByLabel      = "control_one.isolation.updated_by"
	legacyConnectivityModeLabel  = "connectivity_mode"
	legacyConnectivityUntilLabel = "connectivity_mode_until"
	legacyAllowedApplications    = "allowed_applications"
	legacyFirewallAllowlistCIDRs = "allowlist_cidrs"
)

type nodeIsolationPosture struct {
	Mode                string
	Active              bool
	Expired             bool
	LocalOnly           bool
	ExpiresAt           *time.Time
	Reason              string
	AllowedApplications []string
	AllowlistCIDRs      []string
}

type nodeNetworkPolicy struct {
	Mode                string   `json:"mode"`
	Active              bool     `json:"active"`
	LocalOnly           bool     `json:"local_only"`
	ExpiresAt           *string  `json:"expires_at,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	AllowedApplications []string `json:"allowed_applications,omitempty"`
	AllowlistCIDRs      []string `json:"allowlist_cidrs,omitempty"`
}

type nodeIsolationRequest struct {
	Mode                string   `json:"mode"`
	DurationSeconds     int64    `json:"duration_seconds,omitempty"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	AllowedApplications []string `json:"allowed_applications,omitempty"`
	AllowlistCIDRs      []string `json:"allowlist_cidrs,omitempty"`
}

func (r nodeIsolationRequest) validate(now time.Time) (nodeIsolationPosture, error) {
	mode := normalizeIsolationMode(r.Mode)
	if mode == "" {
		return nodeIsolationPosture{}, fmt.Errorf("mode must be online|whitelist|airgapped")
	}
	var expiresAt *time.Time
	if strings.TrimSpace(r.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(r.ExpiresAt))
		if err != nil {
			return nodeIsolationPosture{}, fmt.Errorf("expires_at must be RFC3339")
		}
		parsed = parsed.UTC()
		expiresAt = &parsed
	}
	if r.DurationSeconds > 0 {
		parsed := now.Add(time.Duration(r.DurationSeconds) * time.Second).UTC()
		expiresAt = &parsed
	}
	if mode == isolationModeOnline && expiresAt != nil {
		return nodeIsolationPosture{}, fmt.Errorf("online mode cannot have an expiry")
	}
	if expiresAt != nil && !expiresAt.After(now) {
		return nodeIsolationPosture{}, fmt.Errorf("expiry must be in the future")
	}
	return nodeIsolationPosture{
		Mode:                mode,
		Active:              mode != isolationModeOnline,
		LocalOnly:           mode == isolationModeAirgapped,
		ExpiresAt:           expiresAt,
		Reason:              strings.TrimSpace(r.Reason),
		AllowedApplications: sanitizeStringSlice(r.AllowedApplications, 50),
		AllowlistCIDRs:      sanitizeStringSlice(r.AllowlistCIDRs, 100),
	}, nil
}

func nodeIsolationPostureFromNode(node storage.Node, now time.Time) nodeIsolationPosture {
	return nodeIsolationPostureFromLabels(node.Labels, now)
}

func nodeIsolationPostureFromLabels(labels map[string]any, now time.Time) nodeIsolationPosture {
	mode := normalizeIsolationMode(firstLabelString(labels, isolationModeLabel, legacyConnectivityModeLabel))
	if mode == "" {
		mode = isolationModeOnline
	}
	posture := nodeIsolationPosture{
		Mode:                mode,
		Active:              mode != isolationModeOnline,
		LocalOnly:           mode == isolationModeAirgapped || labelBool(labels, isolationLocalOnlyLabel),
		Reason:              firstLabelString(labels, isolationReasonLabel),
		AllowedApplications: labelStringSlice(labels, isolationAllowAppsLabel, legacyAllowedApplications),
		AllowlistCIDRs:      labelStringSlice(labels, isolationAllowCIDRsLabel, legacyFirewallAllowlistCIDRs),
	}
	if expires := firstLabelString(labels, isolationExpiresAtLabel, legacyConnectivityUntilLabel); expires != "" {
		if parsed, err := time.Parse(time.RFC3339, expires); err == nil {
			parsed = parsed.UTC()
			posture.ExpiresAt = &parsed
			if !parsed.After(now) {
				posture.Active = false
				posture.Expired = true
				posture.Mode = isolationModeOnline
				posture.LocalOnly = false
			}
		}
	}
	return posture
}

func normalizeIsolationMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none", "normal", "default", "online":
		return isolationModeOnline
	case "whitelist", "allowlist", "whitelist_only", "allowlist_only", "locked_down":
		return isolationModeWhitelist
	case "airgap", "airgapped", "offline", "isolated", "local_only":
		return isolationModeAirgapped
	default:
		return ""
	}
}

func applyNodeIsolationLabels(labels map[string]any, posture nodeIsolationPosture, actor string, now time.Time) map[string]any {
	next := map[string]any{}
	for k, v := range labels {
		next[k] = v
	}
	for _, key := range []string{
		isolationModeLabel,
		isolationExpiresAtLabel,
		isolationReasonLabel,
		isolationAllowAppsLabel,
		isolationAllowCIDRsLabel,
		isolationLocalOnlyLabel,
		isolationUpdatedAtLabel,
		isolationUpdatedByLabel,
		legacyConnectivityModeLabel,
		legacyConnectivityUntilLabel,
	} {
		delete(next, key)
	}
	if posture.Mode == isolationModeOnline {
		return next
	}
	next[isolationModeLabel] = posture.Mode
	next[isolationLocalOnlyLabel] = posture.LocalOnly
	next[isolationUpdatedAtLabel] = now.UTC().Format(time.RFC3339)
	if actor != "" {
		next[isolationUpdatedByLabel] = actor
	}
	if posture.ExpiresAt != nil {
		next[isolationExpiresAtLabel] = posture.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if posture.Reason != "" {
		next[isolationReasonLabel] = posture.Reason
	}
	if len(posture.AllowedApplications) > 0 {
		next[isolationAllowAppsLabel] = posture.AllowedApplications
	}
	if len(posture.AllowlistCIDRs) > 0 {
		next[isolationAllowCIDRsLabel] = posture.AllowlistCIDRs
	}
	return next
}

func nodeNetworkPolicyFromPosture(posture nodeIsolationPosture) *nodeNetworkPolicy {
	if posture.Mode == isolationModeOnline && !posture.Expired {
		return nil
	}
	out := &nodeNetworkPolicy{
		Mode:                posture.Mode,
		Active:              posture.Active,
		LocalOnly:           posture.LocalOnly,
		Reason:              posture.Reason,
		AllowedApplications: posture.AllowedApplications,
		AllowlistCIDRs:      posture.AllowlistCIDRs,
	}
	if posture.ExpiresAt != nil {
		formatted := posture.ExpiresAt.UTC().Format(time.RFC3339)
		out.ExpiresAt = &formatted
	}
	return out
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func firstLabelString(labels map[string]any, keys ...string) string {
	for _, key := range keys {
		if labels == nil {
			return ""
		}
		if value, ok := labels[key]; ok {
			if s := stringFromAny(value); s != "" {
				return s
			}
		}
	}
	return ""
}

func labelStringSlice(labels map[string]any, keys ...string) []string {
	for _, key := range keys {
		if labels == nil {
			return nil
		}
		if value, ok := labels[key]; ok {
			out := anyStringSlice(value)
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func anyStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return sanitizeStringSlice(v, 100)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := stringFromAny(item); s != "" {
				out = append(out, s)
			}
		}
		return sanitizeStringSlice(out, 100)
	case string:
		parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })
		return sanitizeStringSlice(parts, 100)
	default:
		return nil
	}
}

func labelBool(labels map[string]any, key string) bool {
	if labels == nil {
		return false
	}
	switch v := labels[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.EqualFold(strings.TrimSpace(v), "yes")
	default:
		return false
	}
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}
