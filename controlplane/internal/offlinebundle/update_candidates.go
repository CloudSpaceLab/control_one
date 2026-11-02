package offlinebundle

import (
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"time"
)

type CandidateFixBoundary struct {
	CVEID             string `json:"cve_id"`
	Severity          string `json:"severity"`
	FixedVersion      string `json:"fixed_version"`
	AdvisoryURL       string `json:"advisory_url"`
	CurrentlyAffected bool   `json:"currently_affected"`
}

type UpdateCandidateView struct {
	Review                *UpdateReview                          `json:"review,omitempty"`
	Name                  string                                 `json:"name"`
	Arch                  string                                 `json:"arch"`
	InstalledVersion      string                                 `json:"installed_version"`
	RepositoryObservation *packageinventory.CandidateObservation `json:"repository_observation,omitempty"`
	FixBoundaries         []CandidateFixBoundary                 `json:"fix_boundaries"`
	AdvisoryFeeds         []AssessmentFeedEvidence               `json:"advisory_feeds"`
	InventoryGeneration   string                                 `json:"inventory_generation"`
	Coverage              string                                 `json:"coverage"`
	SecurityFixApplicable bool                                   `json:"security_fix_applicable"`
	Reason                string                                 `json:"reason"`
	RepositoryVerified    bool                                   `json:"repository_verified"`
	VersionRelation       string                                 `json:"version_relation"`
}

// Availability, advisory boundaries and post-install verification are separate.
func AssessUpdateCandidates(root string, inventory []PackageInventory, snapshot *packageinventory.Snapshot, observations []packageinventory.CandidateObservation, now time.Time) ([]UpdateCandidateView, AssessmentEvidence, error) {
	matches, alreadyFixed, report, err := AssessActiveVulnerabilityFeedsWithEvidence(root, inventory)
	var nativeItems []packageinventory.Item
	if snapshot != nil {
		for _, p := range inventory {
			if p.Source == snapshot.Scope {
				nativeItems = append(nativeItems, packageinventory.Item{Name: p.Name, Version: p.Version, Source: p.Source, Arch: p.Arch})
			}
		}
	}
	complete := snapshot != nil && snapshot.Validate(nativeItems, now) == nil
	generation := VulnerabilityInventoryGeneration(inventory)
	views := make([]UpdateCandidateView, 0, len(inventory))
	for _, p := range inventory {
		view := UpdateCandidateView{Name: p.Name, Arch: p.Arch, InstalledVersion: p.Version, InventoryGeneration: generation, Coverage: "selected_advisories_only", AdvisoryFeeds: report.Feeds, Reason: "repository_availability_unknown", FixBoundaries: []CandidateFixBoundary{}}
		var applicable []VulnerabilityMatch
		for _, match := range matches {
			if match.Package.Name == p.Name && match.Package.Version == p.Version && match.Package.Source == p.Source && match.Package.Arch == p.Arch {
				applicable = append(applicable, match)
				view.FixBoundaries = append(view.FixBoundaries, CandidateFixBoundary{CVEID: match.Advisory.CVEID, Severity: match.Advisory.Severity, FixedVersion: match.Affected.FixedVersion, AdvisoryURL: match.Advisory.AdvisoryURL, CurrentlyAffected: true})
			}
		}
		for _, fixed := range alreadyFixed {
			if fixed.Package.Name == p.Name && fixed.Package.Version == p.Version && fixed.Package.Source == p.Source && fixed.Package.Arch == p.Arch {
				view.FixBoundaries = append(view.FixBoundaries, CandidateFixBoundary{CVEID: fixed.CVEID, Severity: fixed.Severity, FixedVersion: fixed.FixedVersion, AdvisoryURL: fixed.AdvisoryURL})
			}
		}
		for _, o := range observations {
			if p.Source == "dpkg" && o.Name == p.Name && o.Arch == p.Arch && o.InstalledVersion == p.Version {
				copy := o
				view.RepositoryObservation = &copy
				break
			}
		}
		o := view.RepositoryObservation
		view.VersionRelation = "unknown"
		if o != nil {
			if cmp, ok := compareVulnerabilityVersion(o.CandidateVersion, p.Version, "debian"); ok {
				view.VersionRelation = "same"
				if cmp > 0 {
					view.VersionRelation = "newer"
				} else if cmp < 0 {
					view.VersionRelation = "older"
				}
			}
			view.RepositoryVerified = complete && p.Source == snapshot.Scope && o.VerifiedAt(now)
		}
		if err != nil {
			view.Reason = "trusted_advisory_assessment_unavailable"
		} else if !complete || p.Source != snapshot.Scope {
			view.Reason = "complete_native_inventory_unavailable"
		} else if o != nil && o.VerifiedAt(now) {
			view.Reason = "no_matching_security_advisory_in_selected_coverage"
			if len(applicable) == 0 && len(view.FixBoundaries) > 0 {
				view.Reason = "installed_version_already_meets_selected_fixed_boundaries"
			}
			if len(applicable) > 0 {
				view.SecurityFixApplicable = true
				view.Reason = "repository_candidate_meets_selected_fixed_boundaries"
				candidate := p
				candidate.Version = o.CandidateVersion
				// Check all declarations at the candidate, including conflicting
				// intervals; a newer number alone supplies no positive evidence.
				_, fixed, _, candidateErr := AssessActiveVulnerabilityFeedsWithEvidence(root, []PackageInventory{candidate})
				for _, match := range applicable {
					verified := false
					for _, f := range fixed {
						if f.CVEID == match.Advisory.CVEID {
							verified = true
						}
					}
					if candidateErr != nil || !verified {
						view.SecurityFixApplicable = false
						view.Reason = "repository_candidate_not_proven_to_fix_selected_advisories"
					}
				}
			}
		}
		views = append(views, view)
	}
	return views, report, err
}
