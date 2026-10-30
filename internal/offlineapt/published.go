package offlineapt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

const publishedPlanFile = "published-plan.json"

func requirePublishedSelection(base, digest string, approved patchsupport.OfflineContent) error {
	path := filepath.Join(base, publishedPlanFile)
	if err := protectedPath(path); err != nil {
		return fmt.Errorf("published offline selection unavailable: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 131073))
	if err != nil || len(body) > 131072 {
		return fmt.Errorf("invalid published offline selection")
	}
	var selected patchsupport.OfflineContent
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&selected) != nil || dec.Decode(new(any)) != io.EOF || selected.ManifestSHA256 != digest || !reflect.DeepEqual(selected, approved) {
		return fmt.Errorf("published offline selection withdrawn or changed before execution")
	}
	return nil
}

// PublishSelection records only the last explicitly staged plan. It never scans
// arbitrary directories or exposes repository paths, keys or agent credentials.
func PublishSelection(base string, plan patchsupport.OfflineContent) error {
	return PublishSelectionContext(context.Background(), base, plan)
}

func PublishSelectionContext(ctx context.Context, base string, plan patchsupport.OfflineContent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := lockStaging(base)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err = OpenApproved(base, plan); err != nil {
		return err
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(base, ".published-plan-")
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
	if err = os.Rename(f.Name(), filepath.Join(base, publishedPlanFile)); err != nil {
		return err
	}
	dir, err := os.Open(base)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// PublishedSelection revalidates the selected snapshot and signed local archive
// preflight. Discovery cannot authorize execution; Install repeats all checks.
func PublishedSelection(ctx context.Context, base string) patchsupport.PublishedPlanReport {
	report := patchsupport.PublishedPlanReport{Version: 1, Status: "unavailable", Plans: []patchsupport.OfflineContent{}}
	if err := CheckExecutionIsolation(); err != nil {
		return report
	}
	if _, err := os.Lstat(base); os.IsNotExist(err) {
		report.Status = "empty"
		return report
	}
	unlock, err := lockStaging(base)
	if err != nil {
		return report
	}
	defer unlock()
	path := filepath.Join(base, publishedPlanFile)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		report.Status = "empty"
		return report
	}
	if protectedPath(path) != nil {
		return report
	}
	f, err := os.Open(path)
	if err != nil {
		return report
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 131073))
	if err != nil || len(body) > 131072 {
		return report
	}
	var plan patchsupport.OfflineContent
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&plan) != nil || dec.Decode(new(any)) != io.EOF {
		return report
	}
	snapshot, err := OpenApproved(base, plan)
	if err != nil {
		return report
	}
	defer snapshot.Close()
	if _, err = snapshot.CheckApprovedArchives(ctx, plan); err != nil {
		return report
	}
	report.Status, report.Plans = "ready", []patchsupport.OfflineContent{plan}
	return report
}
