package patchgovernance

import (
	"fmt"
	"slices"
	"time"
)

const AutomationHealthProfile = "ubuntu-apt-service-reboot-v1"

// Installation policy activation is separate from saving a review draft. This
// profile requires current native package, complete service and reboot evidence;
// callers must check those observations again under the dispatch transaction.
func (a AutomationPolicy) ValidateActivation() error {
	if !a.Enabled {
		return nil
	}
	if a.Mode == "recommend_only" {
		return fmt.Errorf("recommendation-only cannot enable installation")
	}
	if a.HealthProfile != AutomationHealthProfile || (a.Transport != "direct" && a.Transport != "proxy") {
		return fmt.Errorf("installation requires the qualified Ubuntu exact-APT service/reboot profile and direct or proxy transport")
	}
	if a.ObservationMinutes < 1 || a.ObservationMinutes > 1440 {
		return fmt.Errorf("canary observation must be 1–1440 minutes")
	}
	return nil
}

// MatchAutomationApproval never widens a reviewed package selection. An
// exclusion applies to dependencies as well as explicitly requested packages.
func MatchAutomationApproval(a AutomationPolicy, node, mode, window string, requested, changed []string, now time.Time) error {
	if !a.Enabled || a.Mode == "recommend_only" || a.ValidUntil == nil || !a.ValidUntil.After(now) {
		return fmt.Errorf("installation policy is disabled or expired")
	}
	if err := a.ValidateActivation(); err != nil {
		return err
	}
	if !slices.Contains(a.NodeIDs, node) || mode != a.Transport || window != a.WindowID || len(requested) == 0 {
		return fmt.Errorf("approval is outside the explicit node, transport or calendar scope")
	}
	for _, p := range requested {
		if !slices.Contains(a.Packages, p) || slices.Contains(a.Exclusions, p) {
			return fmt.Errorf("requested package is outside the policy scope")
		}
	}
	for _, p := range changed {
		if slices.Contains(a.Exclusions, p) {
			return fmt.Errorf("resolved dependency is excluded by policy")
		}
	}
	return nil
}
