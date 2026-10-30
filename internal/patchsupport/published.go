package patchsupport

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// PublishedPlanReport is a replaceable discovery snapshot, never approval or
// authority to install. Currently one active plan per enrolled node is exposed.
type PublishedPlanReport struct {
	Version int              `json:"version"`
	Status  string           `json:"status"`
	Plans   []OfflineContent `json:"plans"`
}

type PublishedPlanEvidence struct {
	Report     PublishedPlanReport `json:"report"`
	ObservedAt time.Time           `json:"observed_at"`
}

func (r PublishedPlanReport) Validate() error {
	if body, err := json.Marshal(r); err != nil || len(body) > 131072 {
		return fmt.Errorf("published offline plan report too large")
	}
	if r.Version != 1 || len(r.Plans) > 1 {
		return fmt.Errorf("invalid published offline plan report")
	}
	if r.Status != "ready" && r.Status != "empty" && r.Status != "unavailable" {
		return fmt.Errorf("invalid published offline plan status")
	}
	if (r.Status == "ready") != (len(r.Plans) == 1) {
		return fmt.Errorf("inconsistent published offline plans")
	}
	for _, p := range r.Plans {
		allow := []string{}
		for _, pkg := range p.Requested {
			allow = append(allow, pkg.Name)
		}
		if err := p.ValidateInstallation("airgapped", allow, nil); err != nil {
			return err
		}
	}
	return nil
}

func (e PublishedPlanEvidence) Require(digest string, content *OfflineContent, now time.Time) error {
	if e.Report.Validate() != nil || e.Report.Status != "ready" || e.ObservedAt.IsZero() || e.ObservedAt.After(now) || now.Sub(e.ObservedAt) > 5*time.Minute {
		return fmt.Errorf("fresh agent-published offline plan required; restage and refresh plans")
	}
	if content == nil || digest != content.ManifestSHA256 || !reflect.DeepEqual(e.Report.Plans[0], *content) {
		return fmt.Errorf("agent-published offline plan changed or belongs to another target")
	}
	return nil
}
