package offlinebundle

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/stretchr/testify/require"
)

func TestUpdateCandidateRequiresRepositoryAndAdvisoryEvidence(t *testing.T) {
	for _, scenario := range []string{"applicable", "newer_but_below_fix", "unknown_version", "unsigned_repository", "stale_observation", "expired_repository", "missing_inventory", "different_installed", "already_current", "already_fixed", "no_advisory", "tampered_advisory"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now().UTC()
			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			feed := VulnerabilityFeed{SchemaVersion: 1, Advisories: []VulnerabilityAdvisory{{CVEID: "CVE-2026-CANDIDATE-FIXTURE", Severity: "high", AdvisoryURL: "https://vendor.example/advisory", AffectedPackages: []VulnerabilityAffectedPkg{{Name: "demo", Source: "dpkg", VersionScheme: "debian", VersionRange: "<2.0", FixedVersion: "2.0"}}}}}
			if scenario == "no_advisory" {
				feed.Advisories = nil
			}
			root := t.TempDir()
			receipt, err := Import(context.Background(), bytes.NewReader(makeVulnerabilityFeedBundle(t, priv, now, feed)), ImportOptions{RootDir: root, PublicKey: pub, Now: now})
			require.NoError(t, err)
			items := []packageinventory.Item{{Name: "demo", Version: "1.0", Source: "dpkg", Arch: "amd64"}}
			meta := &packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now.Add(-time.Minute), SHA256: packageinventory.Digest(items), Count: 1}
			inventory := []PackageInventory{{Name: "demo", Version: "1.0", Source: "dpkg", Arch: "amd64"}}
			observation := packageinventory.CandidateObservation{Name: "demo", Arch: "amd64", InstalledVersion: "1.0", CandidateVersion: "2.0", ObservedAt: now.Add(-time.Second), State: "verified_repository_candidate", Repository: "https://archive.ubuntu.com/ubuntu", Vendor: "Ubuntu", ReleaseDate: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), ReleaseSHA256: strings.Repeat("a", 64), IndexSHA256: strings.Repeat("b", 64), ArtifactSHA256: strings.Repeat("c", 64)}
			switch scenario {
			case "newer_but_below_fix":
				observation.CandidateVersion = "1.5"
			case "unknown_version":
				observation.CandidateVersion = "vendor-unknown"
			case "unsigned_repository":
				observation.State = "unknown"
			case "stale_observation":
				observation.ObservedAt = now.Add(-2 * time.Hour)
			case "expired_repository":
				observation.ExpiresAt = now.Add(-time.Second)
			case "missing_inventory":
				meta = nil
			case "different_installed":
				observation.InstalledVersion = "0.9"
			case "already_current":
				observation.CandidateVersion = "1.0"
			case "already_fixed":
				items[0].Version = "2.0"
				inventory[0].Version = "2.0"
				meta.SHA256 = packageinventory.Digest(items)
				observation.InstalledVersion = "2.0"
			case "tampered_advisory":
				require.NoError(t, os.WriteFile(receipt.Contents[0].Active, []byte(`{"schema_version":1,"advisories":[]}`), 0600))
			}
			views, _, err := AssessUpdateCandidates(root, inventory, meta, []packageinventory.CandidateObservation{observation}, now)
			if scenario == "tampered_advisory" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, views, 1)
			require.Equal(t, scenario == "applicable", views[0].SecurityFixApplicable)
			if scenario == "already_fixed" {
				require.True(t, views[0].RepositoryVerified)
				require.Equal(t, "same", views[0].VersionRelation)
				require.Len(t, views[0].FixBoundaries, 1)
				require.False(t, views[0].FixBoundaries[0].CurrentlyAffected)
				require.Equal(t, "2.0", views[0].FixBoundaries[0].FixedVersion)
				require.Equal(t, "installed_version_already_meets_selected_fixed_boundaries", views[0].Reason)
			}
			if scenario == "newer_but_below_fix" {
				require.Equal(t, "newer", views[0].VersionRelation)
				require.False(t, views[0].SecurityFixApplicable)
			}
		})
	}
}
