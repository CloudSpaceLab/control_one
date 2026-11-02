package onlineapt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

func Select(ctx context.Context, spec Spec, p patchsupport.OnlinePlan) error {
	spec = spec.defaults()
	unlock, err := lock(spec.Root)
	if err != nil {
		return err
	}
	defer unlock()
	if err = Verify(spec, p); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	body, _ := json.Marshal(struct {
		SHA256 string `json:"sha256"`
	}{p.SHA256})
	f, err := os.CreateTemp(spec.Root, ".selected-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(spec.Root, "selected-plan.json")); err != nil {
		return err
	}
	return syncDir(spec.Root)
}

// Discovery rechecks baseline and source/trust inputs, not every archive on
// every heartbeat. Installation repeats full acquired-content verification.
func Discovery(spec Spec) patchsupport.OnlinePlanReport {
	spec = spec.defaults()
	result := patchsupport.OnlinePlanReport{Status: "unavailable"}
	var selected struct {
		SHA256 string `json:"sha256"`
	}
	if readJSON(filepath.Join(spec.Root, "selected-plan.json"), &selected) != nil {
		return result
	}
	p, err := Load(spec, selected.SHA256)
	if err != nil {
		return result
	}
	var m manifest
	if readJSON(filepath.Join(spec.Root, p.SHA256, "manifest.json"), &m) != nil || hashJSON(m) != p.ManifestSHA256 {
		return result
	}
	if state, e := hashFile("/var/lib/dpkg/status"); e != nil || state != p.InstalledStateSHA256 {
		return result
	}
	if inputs, e := inputIdentity(m.SourcesRoot); e != nil || inputs != p.InputsSHA256 {
		return result
	}
	result.Status, result.Plan = "ready", &p
	return result
}

func requireSelected(spec Spec, id string) error {
	var selected struct {
		SHA256 string `json:"sha256"`
	}
	if err := readJSON(filepath.Join(spec.defaults().Root, "selected-plan.json"), &selected); err != nil {
		return err
	}
	if selected.SHA256 != id {
		return fmt.Errorf("online plan was replaced or withdrawn; prepare and approve again")
	}
	return nil
}
