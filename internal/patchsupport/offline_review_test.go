package patchsupport

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOfflineReviewValidationAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	fixture := func() OfflineContent {
		return OfflineContent{Format: 3, ManifestSHA256: strings.Repeat("a", 64), InstalledStatusSHA256: strings.Repeat("b", 64), KeyringSHA256: strings.Repeat("c", 64), Suite: "noble-updates", Component: "main", Architecture: "amd64", Requested: []ExactPackage{{Name: "liba", Version: "2"}}, Resolved: []ExactPackage{{Name: "liba", Version: "2"}, {Name: "libb", Version: "3"}}, Review: &OfflineReview{PreparedAt: now.Add(-time.Minute), ExpiresAt: now.Add(59 * time.Minute), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []PlanChange{{Name: "liba", InstalledVersion: "1", ProposedVersion: "2", Role: "requested"}, {Name: "libb", InstalledVersion: "", ProposedVersion: "3", Role: "dependency"}}}}
	}
	for _, scenario := range []string{"valid", "expiry", "future", "lifetime", "missing", "scope", "proposed", "before", "reboot", "service", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			c := fixture()
			switch scenario {
			case "expiry":
				c.Review.ExpiresAt = now
			case "future":
				c.Review.PreparedAt = now.Add(time.Minute)
			case "lifetime":
				c.Review.ExpiresAt = now.Add(time.Hour)
			case "missing":
				c.Review = nil
			case "scope":
				c.Review.Changes[1].Role = "requested"
			case "proposed":
				c.Review.Changes[1].ProposedVersion = "4"
			case "before":
				c.Review.Changes[1].InstalledVersion = "bad;command"
			case "reboot":
				c.Review.Reboot = "not_required"
			case "service":
				c.Review.ServiceImpact = "safe"
			case "legacy":
				c.Format = 2
			}
			err := c.ValidateInstallation("airgapped", []string{"liba"}, nil)
			if scenario == "valid" {
				require.NoError(t, err)
				require.Error(t, c.RequireCurrent(c.Review.ExpiresAt))
			} else {
				require.Error(t, err)
			}
		})
	}
}
