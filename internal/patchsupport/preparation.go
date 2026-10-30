package patchsupport

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

const PrepareOfflineJob = "patch.prepare_offline"

// An administrator registers one approved source per node. The agent derives
// paths beneath a fixed root; operator requests never supply paths or commands.
type OfflineRepository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	KeyringSHA256 string `json:"keyring_sha256"`
	Suite         string `json:"suite"`
	Component     string `json:"component"`
	Enabled       bool   `json:"enabled"`
}

func (r OfflineRepository) Validate() error {
	id, err := uuid.Parse(r.ID)
	if err != nil || id == uuid.Nil || r.ID != id.String() || len(strings.TrimSpace(r.Name)) == 0 || len(r.Name) > 80 || strings.ContainsAny(r.Name, "\r\n\x00") || !offlineDigest.MatchString(r.KeyringSHA256) {
		return fmt.Errorf("invalid approved offline repository")
	}
	if !offlineSelector.MatchString(r.Suite) || len(r.Suite) > 80 || !offlineSelector.MatchString(r.Component) || len(r.Component) > 80 {
		return fmt.Errorf("invalid repository suite or component")
	}
	return nil
}

func (r OfflineRepository) RepositoryPath() string {
	return path.Join("/var/cache/control-one/offline-sources", r.ID, "repository")
}

type OfflinePreparation struct {
	NodeID      string            `json:"node_id"`
	Repository  OfflineRepository `json:"repository"`
	Packages    []ExactPackage    `json:"packages"`
	ExpiresAt   time.Time         `json:"expires_at"`
	RequestedBy string            `json:"requested_by"`
}

func (p OfflinePreparation) Validate(now time.Time) error {
	id, err := uuid.Parse(p.NodeID)
	if err != nil || id == uuid.Nil || p.NodeID != id.String() || p.Repository.Validate() != nil || !p.Repository.Enabled {
		return fmt.Errorf("active target and approved repository required")
	}
	if p.ExpiresAt.IsZero() || !p.ExpiresAt.After(now) || p.ExpiresAt.After(now.Add(11*time.Minute)) {
		return fmt.Errorf("offline preparation request expired or invalid")
	}
	if len(p.Packages) == 0 || len(p.Packages) > 100 {
		return fmt.Errorf("select 1–100 exact package versions")
	}
	for i, pkg := range p.Packages {
		if !offlinePackage.MatchString(pkg.Name) || len(pkg.Name) > 256 || !offlineVersion.MatchString(pkg.Version) || len(pkg.Version) > 256 || (i > 0 && p.Packages[i-1].Name >= pkg.Name) {
			return fmt.Errorf("unique sorted exact package versions required")
		}
	}
	return nil
}
