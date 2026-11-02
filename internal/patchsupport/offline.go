package patchsupport

import (
	"fmt"
	"regexp"
	"time"
)

type ExactPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// PlanChange is bound to the complete installed-state hash. Empty means absent.
type PlanChange struct {
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_version"`
	ProposedVersion  string `json:"proposed_version"`
	Role             string `json:"role"`
}
type OfflineReview struct {
	PreparedAt    time.Time    `json:"prepared_at"`
	ExpiresAt     time.Time    `json:"expires_at"`
	Changes       []PlanChange `json:"changes"`
	Reboot        string       `json:"reboot"`
	ServiceImpact string       `json:"service_impact"`
}

// OfflineContent is an identity to carry through approval and dispatch, not an
// execution capability or proof that the requested dependency set is complete.
type OfflineContent struct {
	Review                *OfflineReview `json:"review,omitempty"`
	Format                int            `json:"format"`
	ManifestSHA256        string         `json:"manifest_sha256"`
	InstalledStatusSHA256 string         `json:"installed_status_sha256"`
	KeyringSHA256         string         `json:"keyring_sha256"`
	Suite                 string         `json:"suite"`
	Component             string         `json:"component"`
	Architecture          string         `json:"architecture"`
	Requested             []ExactPackage `json:"requested"`
	Resolved              []ExactPackage `json:"resolved,omitempty"`
}

var offlineDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var offlineSelector = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var offlinePackage = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9_-]+)?$`)
var offlineVersion = regexp.MustCompile(`^[0-9][A-Za-z0-9.+:~\-]*$`)

func (c *OfflineContent) ValidateInstallation(mode string, allow, deny []string) error {
	if mode == "airgapped" && (c == nil || (c.Format != 2 && c.Format != 3)) {
		return fmt.Errorf("approved resolved format-2 offline content is required")
	}
	if err := c.Validate(mode, allow, deny); err != nil {
		return err
	}
	return c.RequireCurrent(time.Now().UTC())
}

// RequireCurrent is repeated at request, approval, dispatch and immediately before install.
func (c *OfflineContent) RequireCurrent(now time.Time) error {
	if c != nil && c.Format == 3 && (c.Review == nil || c.Review.PreparedAt.After(now) || !c.Review.ExpiresAt.After(now)) {
		return fmt.Errorf("offline plan expired or not yet valid; prepare and approve a fresh plan")
	}
	return nil
}

func (c *OfflineContent) Validate(mode string, allow, deny []string) error {
	if c == nil {
		return nil
	}
	if mode != "airgapped" {
		return fmt.Errorf("offline content requires explicit airgapped mode")
	}
	if (c.Format != 1 && c.Format != 2 && c.Format != 3) || !offlineDigest.MatchString(c.ManifestSHA256) || !offlineDigest.MatchString(c.InstalledStatusSHA256) || !offlineDigest.MatchString(c.KeyringSHA256) {
		return fmt.Errorf("invalid offline content identity")
	}
	for _, s := range []string{c.Suite, c.Component, c.Architecture} {
		if !offlineSelector.MatchString(s) {
			return fmt.Errorf("invalid offline content selector")
		}
	}
	if len(c.Requested) == 0 || len(c.Requested) > 100 || len(deny) > 0 || len(allow) != len(c.Requested) {
		return fmt.Errorf("offline content requires matching bounded package allowlist without exclusions")
	}
	names := map[string]bool{}
	for i, p := range c.Requested {
		if !offlinePackage.MatchString(p.Name) || !offlineVersion.MatchString(p.Version) || len(p.Version) > 256 || (i > 0 && c.Requested[i-1].Name >= p.Name) {
			return fmt.Errorf("offline content requires unique sorted exact package versions")
		}
		names[p.Name] = true
	}
	for _, name := range allow {
		if !names[name] {
			return fmt.Errorf("offline content package scope mismatch")
		}
		delete(names, name)
	}
	if len(names) != 0 {
		return fmt.Errorf("offline content package scope mismatch")
	}
	if c.Format >= 2 {
		if len(c.Resolved) == 0 || len(c.Resolved) > 100 {
			return fmt.Errorf("resolved offline dependency plan required")
		}
		versions := map[string]string{}
		for i, p := range c.Resolved {
			if !offlinePackage.MatchString(p.Name) || !offlineVersion.MatchString(p.Version) || len(p.Version) > 256 || (i > 0 && c.Resolved[i-1].Name >= p.Name) {
				return fmt.Errorf("invalid resolved offline dependency plan")
			}
			versions[p.Name] = p.Version
		}
		for _, p := range c.Requested {
			if versions[p.Name] != p.Version {
				return fmt.Errorf("requested version absent from resolved offline plan")
			}
		}
	}
	if c.Format == 3 {
		r := c.Review
		if r == nil || r.PreparedAt.IsZero() || !r.ExpiresAt.After(r.PreparedAt) || r.ExpiresAt.Sub(r.PreparedAt) > time.Hour || len(r.Changes) != len(c.Resolved) || r.Reboot != "unknown" || r.ServiceImpact != "requires_operator_review" {
			return fmt.Errorf("invalid time-bounded offline review")
		}
		requested := map[string]bool{}
		for _, p := range c.Requested {
			requested[p.Name] = true
		}
		for i, change := range r.Changes {
			p := c.Resolved[i]
			role := "dependency"
			if requested[p.Name] {
				role = "requested"
			}
			if change.Name != p.Name || change.ProposedVersion != p.Version || change.Role != role || (change.InstalledVersion != "" && (!offlineVersion.MatchString(change.InstalledVersion) || len(change.InstalledVersion) > 256)) {
				return fmt.Errorf("offline review differs from resolved plan")
			}
		}
	} else if c.Review != nil {
		return fmt.Errorf("review requires format-3 offline content")
	}

	return nil
}
