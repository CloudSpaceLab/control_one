package offlinebundle

import (
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/stretchr/testify/require"
)

func TestExplainUpdateCandidateRequiresCurrentBoundEvidence(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(15 * time.Minute)
	base := UpdateCandidateView{Name: "demo", Arch: "amd64", InstalledVersion: "1.0", InventoryGeneration: "snapshot", RepositoryVerified: true, SecurityFixApplicable: true, VersionRelation: "newer", Reason: "repository_candidate_meets_selected_fixed_boundaries", AdvisoryFeeds: []AssessmentFeedEvidence{{Name: "trusted", ExpiresAt: &expiry}}, FixBoundaries: []CandidateFixBoundary{{CVEID: "CVE-DEMO", FixedVersion: "2.0", CurrentlyAffected: true}, {CVEID: "CVE-OLD", FixedVersion: "0.9", CurrentlyAffected: false}}, RepositoryObservation: &packageinventory.CandidateObservation{Name: "demo", Arch: "amd64", InstalledVersion: "1.0", CandidateVersion: "2.0", ObservedAt: now.Add(-time.Minute), State: "verified_repository_candidate", Repository: "https://archive.ubuntu.com/ubuntu", Vendor: "Ubuntu", ReleaseDate: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), ReleaseSHA256: strings.Repeat("a", 64), IndexSHA256: strings.Repeat("b", 64), ArtifactSHA256: strings.Repeat("c", 64)}}
	review := ExplainUpdateCandidate(base, now)
	require.Equal(t, "security_fix_review", review.State)
	require.Equal(t, []string{"CVE-DEMO"}, review.FindingsAddressed)
	require.Equal(t, expiry, *review.EvidenceValidUntil)
	require.Contains(t, review.Dependencies, "Not resolved")
	require.Contains(t, review.Reboot, "unknown")
	require.Contains(t, review.Urgency, "Not assigned")
	require.Len(t, review.RequiredSteps, 5)
	for _, scenario := range []string{"unverified", "stale", "wrong installed", "wrong arch", "missing inventory", "superseded feed", "expired feed", "unknown version"} {
		t.Run(scenario, func(t *testing.T) {
			v := base
			o := *base.RepositoryObservation
			v.RepositoryObservation = &o
			switch scenario {
			case "unverified":
				v.RepositoryVerified = false
			case "stale":
				o.ObservedAt = now.Add(-2 * time.Hour)
			case "wrong installed":
				o.InstalledVersion = "0.9"
			case "wrong arch":
				o.Arch = "arm64"
			case "missing inventory":
				v.InventoryGeneration = ""
			case "superseded feed":
				v.Reason = "published_advisory_generation_mismatch"
			case "expired feed":
				expired := now.Add(-time.Second)
				v.AdvisoryFeeds = []AssessmentFeedEvidence{{ExpiresAt: &expired}}
			case "unknown version":
				v.VersionRelation = "unknown"
			}
			r := ExplainUpdateCandidate(v, now)
			require.Equal(t, "evidence_required", r.State)
			require.Empty(t, r.FindingsAddressed)
		})
	}
	v := base
	v.SecurityFixApplicable = false
	require.Equal(t, "update_review", ExplainUpdateCandidate(v, now).State)
	v = base
	v.AdvisoryFeeds = nil
	require.Equal(t, "update_review", ExplainUpdateCandidate(v, now).State)
	for _, relation := range []string{"same", "older"} {
		v := base
		v.VersionRelation = relation
		require.Equal(t, "no_newer_candidate", ExplainUpdateCandidate(v, now).State)
		require.Empty(t, ExplainUpdateCandidate(v, now).FindingsAddressed)
	}
}
