package offlinebundle

import (
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/stretchr/testify/require"
)

func TestPatchVerificationLifecycleGates(t *testing.T) {
	for _, scenario := range []struct{ name, want string }{
		{"fixed", "verified_fixed"}, {"removed", "verified_removed"}, {"no_snapshot", "verification_pending"}, {"inventory_before_completion", "verification_pending"}, {"invalid_inventory", "verification_unknown"}, {"feed_untrusted", "verification_unknown"}, {"narrower_feed", "verification_unknown"}, {"no_baseline", "verification_unknown"}, {"still_affected", "verification_failed"}, {"no_fixed_boundary", "verification_unknown"}, {"health_missing", "verification_pending"}, {"health_old", "verification_pending"}, {"service_unknown", "verification_unknown"}, {"service_failed", "verification_failed"}, {"audit_failed", "verification_failed"}, {"reboot_unknown", "verification_unknown"}, {"reboot_required", "verification_pending"}, {"boot_unchanged", "verification_pending"}, {"boot_changed", "verified_fixed"}, {"deadline", "verification_unknown"}, {"superseded", "verification_unknown"}, {"application_removal_unknown", "verification_unknown"}, {"service_coverage_unknown", "verification_unknown"}, {"consumer_coverage_unknown", "verification_unknown"}, {"required_service_stopped", "verification_failed"}, {"future_completion", "verification_unknown"}, {"kernel_runtime_unknown", "verification_unknown"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			now := time.Now().UTC()
			no, yes := false, true
			items := []packageinventory.Item{{Name: "demo", Version: "2", Source: "dpkg", Arch: "amd64"}}
			meta := &packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now.Add(-10 * time.Second), SHA256: packageinventory.Digest(items), Count: 1}
			health := &packageinventory.Health{ObservedAt: now.Add(-5 * time.Second), BootID: "boot-a", RebootRequired: &no, PackageAudit: "healthy", ServiceHealth: "healthy", ServicesComplete: true, ConsumersComplete: true}
			execution := *health
			execution.ObservedAt = now.Add(-time.Minute)
			base := VerificationScope{Name: "demo", Source: "dpkg", Arch: "amd64", CVEID: "CVE-2026-FIXTURE"}
			in := VerificationInput{CompletedAt: now.Add(-time.Minute), Now: now, Snapshot: meta, Health: health, ExecutionHealth: &execution, Baseline: []VerificationScope{base}, Inventory: items, Fixed: []VerificationScope{base}, Covered: []string{base.CVEID}, FeedTrusted: true}
			switch scenario.name {
			case "service_coverage_unknown":
				health.ServicesComplete = false
			case "consumer_coverage_unknown":
				health.ConsumersComplete = false
			case "required_service_stopped":
				execution.ActiveServices = []string{"example.service"}
				health.ServiceChecks = map[string]string{"example.service": "inactive"}
			case "future_completion":
				in.CompletedAt = now.Add(time.Hour)
			case "kernel_runtime_unknown":
				base.Name = "linux-image-generic"
				in.Baseline = []VerificationScope{base}
				in.Inventory[0].Name = base.Name
				meta.SHA256 = packageinventory.Digest(in.Inventory)
				in.Fixed = []VerificationScope{base}
			case "removed":
				in.Inventory = nil
				meta.Count = 0
				meta.SHA256 = packageinventory.Digest(nil)
			case "no_snapshot":
				in.Snapshot = nil
			case "inventory_before_completion":
				meta.ObservedAt = in.CompletedAt
			case "invalid_inventory":
				meta.SHA256 = "tampered"
			case "feed_untrusted":
				in.FeedTrusted = false
			case "narrower_feed":
				in.Covered = nil
			case "no_baseline":
				in.Baseline = nil
			case "still_affected":
				in.Active = []VerificationScope{base}
			case "no_fixed_boundary":
				in.Fixed = nil
			case "health_missing":
				in.Health = nil
			case "health_old":
				health.ObservedAt = now.Add(-10 * time.Minute)
			case "service_unknown":
				health.ServiceHealth = "unknown"
			case "service_failed":
				health.ServiceHealth = "unhealthy"
			case "audit_failed":
				health.PackageAudit = "unhealthy"
			case "reboot_unknown":
				health.RebootRequired = nil
			case "reboot_required":
				health.RebootRequired = &yes
			case "boot_unchanged":
				execution.RebootRequired = &yes
			case "boot_changed":
				execution.RebootRequired = &yes
				health.BootID = "boot-b"
			case "deadline":
				in.CompletedAt = now.Add(-25 * time.Hour)
			case "superseded":
				in.Superseded = true
			case "application_removal_unknown":
				in.Inventory = nil
				meta.Count = 0
				meta.SHA256 = packageinventory.Digest(nil)
				base.Source = "npm"
				in.Baseline = []VerificationScope{base}
			}
			state, reason := EvaluatePatchVerification(in)
			require.Equal(t, scenario.want, state)
			require.NotEmpty(t, reason)
		})
	}
}
