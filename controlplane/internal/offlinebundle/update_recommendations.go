package offlinebundle

import (
	"sort"
	"time"
)

// UpdateReview is a read-only explanation, never an installation plan or an
// urgency policy. Unknown dependencies/reboot impact remain explicit blockers
// to claiming that a candidate is safe to install or promote automatically.
type UpdateReview struct {
	State              string     `json:"state"`
	Summary            string     `json:"summary"`
	EvaluatedAt        time.Time  `json:"evaluated_at"`
	EvidenceValidUntil *time.Time `json:"evidence_valid_until,omitempty"`
	FindingsAddressed  []string   `json:"findings_addressed"`
	Dependencies       string     `json:"dependencies"`
	Reboot             string     `json:"reboot"`
	Health             string     `json:"health"`
	Urgency            string     `json:"urgency"`
	RequiredSteps      []string   `json:"required_steps"`
}

func ExplainUpdateCandidate(v UpdateCandidateView, now time.Time) UpdateReview {
	review := UpdateReview{State: "evidence_required", Summary: "Refresh authoritative inventory and verified repository evidence before reviewing this candidate.", EvaluatedAt: now, FindingsAddressed: []string{}, Dependencies: "Not resolved for this candidate; review the exact installation plan and required dependencies before approval.", Reboot: "Candidate reboot impact is unknown; current machine reboot state does not predict this update's impact.", Health: "Define service probes and capture fresh package, service and reboot evidence after installation.", Urgency: "Not assigned: tenant policy, exploitation, exposure, business importance and compatibility are not established by version comparison.", RequiredSteps: []string{"Refresh inventory and trusted candidate evidence", "Resolve the exact package and dependency scope", "Review compatibility, service probes and reboot requirements", "Obtain the required approval and maintenance window", "Install on an authorized canary, then reassess and verify health before promotion"}}
	o := v.RepositoryObservation
	if o == nil || !v.RepositoryVerified || !o.VerifiedAt(now) || o.Name != v.Name || o.Arch != v.Arch || o.InstalledVersion != v.InstalledVersion || v.InventoryGeneration == "" {
		return review
	}
	until := o.ObservedAt.Add(time.Hour)
	if o.ExpiresAt.Before(until) {
		until = o.ExpiresAt
	}
	for _, feed := range v.AdvisoryFeeds {
		if feed.ExpiresAt != nil && feed.ExpiresAt.Before(until) {
			until = *feed.ExpiresAt
		}
	}
	review.EvidenceValidUntil = &until
	if !until.After(now) {
		return review
	}
	switch v.VersionRelation {
	case "same":
		review.State = "no_newer_candidate"
		review.Summary = "The observed repository candidate equals the installed version. This does not establish complete security coverage."
	case "older":
		review.State = "no_newer_candidate"
		review.Summary = "The observed repository candidate is older than the installed version. Do not infer that a downgrade is recommended."
	case "newer":
		review.State = "update_review"
		review.Summary = "A newer verified repository version is available for review. A security fix and installation safety are not established by being newer."
		if v.SecurityFixApplicable && len(v.AdvisoryFeeds) > 0 && v.Reason == "repository_candidate_meets_selected_fixed_boundaries" {
			seen := map[string]bool{}
			for _, f := range v.FixBoundaries {
				if f.CurrentlyAffected && f.CVEID != "" && f.FixedVersion != "" {
					seen[f.CVEID] = true
				}
			}
			for id := range seen {
				review.FindingsAddressed = append(review.FindingsAddressed, id)
			}
			sort.Strings(review.FindingsAddressed)
			if len(review.FindingsAddressed) > 0 {
				review.State = "security_fix_review"
				review.Summary = "The verified candidate meets the selected advisory fix boundaries for the listed findings. Review dependencies and compatibility; installation and remediation are not yet verified."
			}
		}
	default:
		review.State = "evidence_required"
		review.Summary = "The candidate version comparison is unavailable; do not recommend installation from this record."
	}
	if v.Reason == "trusted_advisory_assessment_unavailable" || v.Reason == "published_advisory_generation_mismatch" {
		review.State = "evidence_required"
		review.FindingsAddressed = []string{}
		review.Summary = "Trusted advisory assessment is unavailable or superseded. Refresh it before making a security recommendation."
	}
	return review
}
