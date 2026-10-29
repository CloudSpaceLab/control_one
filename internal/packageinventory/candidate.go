package packageinventory

import (
	"encoding/hex"
	"net/url"
	"time"
)

// CandidateObservation is repository availability evidence, separate from the
// advisory's fixed boundary and from post-install verification.
type CandidateObservation struct {
	Name             string    `json:"name"`
	Arch             string    `json:"arch"`
	RepositoryArch   string    `json:"repository_arch"`
	InstalledVersion string    `json:"installed_version"`
	CandidateVersion string    `json:"candidate_version"`
	ObservedAt       time.Time `json:"observed_at"`
	State            string    `json:"state"`
	Reason           string    `json:"reason"`
	Repository       string    `json:"repository"`
	Vendor           string    `json:"vendor"`
	Suite            string    `json:"suite"`
	Component        string    `json:"component"`
	ReleaseDate      time.Time `json:"release_date"`
	ExpiresAt        time.Time `json:"expires_at"`
	ReleaseSHA256    string    `json:"release_sha256"`
	IndexSHA256      string    `json:"index_sha256"`
	ArtifactSHA256   string    `json:"artifact_sha256"`
}

func (o CandidateObservation) VerifiedAt(now time.Time) bool {
	u, err := url.Parse(o.Repository)
	if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if o.State != "verified_repository_candidate" || o.Name == "" || o.Arch == "" || o.InstalledVersion == "" || o.CandidateVersion == "" || o.Vendor != "Ubuntu" || o.ObservedAt.IsZero() || o.ObservedAt.After(now) || now.Sub(o.ObservedAt) > time.Hour || o.ReleaseDate.IsZero() || o.ReleaseDate.After(o.ObservedAt) || !o.ExpiresAt.After(now) || o.ExpiresAt.After(o.ReleaseDate.Add(7*24*time.Hour)) {
		return false
	}
	for _, digest := range []string{o.ReleaseSHA256, o.IndexSHA256, o.ArtifactSHA256} {
		raw, err := hex.DecodeString(digest)
		if err != nil || len(raw) != 32 {
			return false
		}
	}
	return true
}
