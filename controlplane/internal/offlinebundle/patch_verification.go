package offlinebundle

import (
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
)

type VerificationScope struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Arch   string `json:"arch"`
	CVEID  string `json:"cve_id"`
}

type VerificationInput struct {
	CompletedAt     time.Time
	Now             time.Time
	ExecutionHealth *packageinventory.Health
	Health          *packageinventory.Health
	Snapshot        *packageinventory.Snapshot
	Baseline        []VerificationScope
	Inventory       []packageinventory.Item
	Fixed           []VerificationScope
	Active          []VerificationScope
	Covered         []string
	FeedTrusted     bool
	Superseded      bool
}

func EvaluatePatchVerification(in VerificationInput) (string, string) {
	if in.Superseded {
		return "verification_unknown", "superseded_by_later_execution"
	}
	if in.CompletedAt.After(in.Now) {
		return "verification_unknown", "execution_clock_invalid"
	}
	if in.CompletedAt.IsZero() {
		return "execution_pending", "execution_not_complete"
	}
	if in.Now.Sub(in.CompletedAt) > 24*time.Hour {
		return "verification_unknown", "verification_deadline_expired"
	}
	if in.Snapshot == nil || !in.Snapshot.ObservedAt.After(in.CompletedAt) {
		return "verification_pending", "later_authoritative_inventory_required"
	}
	if in.Snapshot.Validate(in.Inventory, in.Now) != nil {
		return "verification_unknown", "inventory_invalid_or_expired"
	}
	if !in.FeedTrusted {
		return "verification_unknown", "fresh_published_advisory_evidence_required"
	}
	if len(in.Baseline) == 0 {
		return "verification_unknown", "no_security_baseline"
	}
	covered := map[string]bool{}
	for _, cve := range in.Covered {
		covered[strings.ToUpper(cve)] = true
	}
	removed := 0
	for _, base := range in.Baseline {
		if !covered[strings.ToUpper(base.CVEID)] {
			return "verification_unknown", "baseline_advisory_not_assessed"
		}
		present := false
		for _, p := range in.Inventory {
			if p.Name == base.Name && p.Source == base.Source && p.Arch == base.Arch {
				present = true
			}
		}
		if !present {
			if base.Source != in.Snapshot.Scope {
				return "verification_unknown", "removal_outside_complete_native_scope"
			}
			removed++
			continue
		}
		for _, active := range in.Active {
			if active == base {
				return "verification_failed", "affected_package_still_installed"
			}
		}
		fixed := false
		for _, scope := range in.Fixed {
			if scope == base {
				fixed = true
			}
		}
		if !fixed {
			return "verification_unknown", "explicit_fixed_boundary_not_verified"
		}
	}
	h := in.Health
	if h == nil || !h.ObservedAt.After(in.CompletedAt) || h.ObservedAt.After(in.Now) || in.Now.Sub(h.ObservedAt) > 5*time.Minute || h.BootID == "" {
		return "verification_pending", "later_current_host_health_required"
	}
	if h.PackageAudit == "unhealthy" || h.ServiceHealth == "unhealthy" {
		return "verification_failed", "host_health_failed"
	}
	if h.PackageAudit != "healthy" || h.ServiceHealth != "healthy" {
		return "verification_unknown", "package_or_service_health_unavailable"
	}
	if !h.ServicesComplete || !h.ConsumersComplete {
		return "verification_unknown", "service_or_runtime_consumer_coverage_unknown"
	}
	if h.RebootRequired == nil {
		return "verification_unknown", "reboot_requirement_unknown"
	}
	if *h.RebootRequired {
		return "verification_pending", "reboot_required"
	}
	if in.ExecutionHealth == nil || in.ExecutionHealth.RebootRequired == nil || in.ExecutionHealth.BootID == "" {
		return "verification_unknown", "execution_reboot_baseline_missing"
	}
	if !in.ExecutionHealth.ServicesComplete {
		return "verification_unknown", "pre_install_service_baseline_unavailable"
	}
	for _, unit := range in.ExecutionHealth.ActiveServices {
		if h.ServiceChecks[unit] != "active" {
			return "verification_failed", "required_service_not_active:" + unit
		}
	}
	for _, base := range in.Baseline {
		if strings.HasPrefix(base.Name, "linux-") || strings.HasPrefix(base.Name, "kernel") {
			return "verification_unknown", "kernel_runtime_mapping_not_qualified"
		}
	}
	if in.ExecutionHealth.ObservedAt.After(in.CompletedAt) || in.CompletedAt.Sub(in.ExecutionHealth.ObservedAt) > 10*time.Minute {
		return "verification_unknown", "execution_health_timestamp_invalid"
	}
	if *in.ExecutionHealth.RebootRequired && h.BootID == in.ExecutionHealth.BootID {
		return "verification_pending", "post_reboot_boot_identity_required"
	}
	if removed == len(in.Baseline) {
		return "verified_removed", "complete_native_removal_and_health_verified"
	}
	return "verified_fixed", "explicit_fixed_boundary_and_health_verified"
}
