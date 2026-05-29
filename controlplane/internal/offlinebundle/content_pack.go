package offlinebundle

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

const ContentTypeSIEMContentPack = "siem_content_pack"

type ActiveContentPack struct {
	Manifest       contentpacks.Manifest `json:"manifest"`
	ManifestPath   string                `json:"manifest_path"`
	ActivePath     string                `json:"active_path"`
	ContentReceipt ContentReceipt        `json:"content_receipt"`
	Root           fs.FS                 `json:"-"`
}

type ActiveContentPackReplay struct {
	ActiveContentPack
	Report          contentpacks.SampleReplayReport    `json:"report"`
	DetectionReport contentpacks.DetectionReplayReport `json:"detection_report"`
}

type ActiveContentPackRegistrySync struct {
	ActiveContentPack
	Report          contentpacks.SampleReplayReport    `json:"report"`
	DetectionReport contentpacks.DetectionReplayReport `json:"detection_report"`
	Record          contentpacks.PackRecord            `json:"record"`
	Action          string                             `json:"action"`
}

const (
	ContentPackRegistryActionInstalledEnabled = "installed_enabled"
	ContentPackRegistryActionAlreadyEnabled   = "already_enabled"
	ContentPackRegistryActionAlreadyInstalled = "already_installed"
	ContentPackRegistryActionQuarantined      = "quarantined"
)

func LoadActiveContentPacks(rootDir string) ([]ActiveContentPack, error) {
	activeRoot := filepath.Join(strings.TrimSpace(rootDir), "active", cleanName(ContentTypeSIEMContentPack))
	matches, err := filepath.Glob(filepath.Join(activeRoot, "*"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	out := make([]ActiveContentPack, 0, len(matches))
	for _, activePath := range matches {
		if strings.HasSuffix(activePath, ".receipt.json") {
			continue
		}
		pack, receipt, err := readActiveContentPack(activePath)
		if err != nil {
			return nil, err
		}
		out = append(out, ActiveContentPack{
			Manifest:       pack.Manifest,
			ManifestPath:   pack.ManifestPath,
			ActivePath:     activePath,
			ContentReceipt: receipt,
			Root:           pack.Root,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manifest.PackID != out[j].Manifest.PackID {
			return out[i].Manifest.PackID < out[j].Manifest.PackID
		}
		if out[i].Manifest.PackVersion != out[j].Manifest.PackVersion {
			return contentpacks.CompareSemver(out[i].Manifest.PackVersion, out[j].Manifest.PackVersion) > 0
		}
		return out[i].ActivePath < out[j].ActivePath
	})
	return out, nil
}

func SyncActiveContentPacksToRegistry(ctx context.Context, rootDir string, registry *contentpacks.Registry, opts contentpacks.SampleReplayOptions, at time.Time) ([]ActiveContentPackRegistrySync, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if registry == nil {
		return nil, fmt.Errorf("content pack registry is nil")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	activeRoot := filepath.Join(strings.TrimSpace(rootDir), "active", cleanName(ContentTypeSIEMContentPack))
	matches, err := filepath.Glob(filepath.Join(activeRoot, "*"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	out := make([]ActiveContentPackRegistrySync, 0, len(matches))
	for _, activePath := range matches {
		if strings.HasSuffix(activePath, ".receipt.json") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		pack, receipt, err := readActiveContentPack(activePath)
		if err != nil {
			return nil, err
		}
		active := ActiveContentPack{
			Manifest:       pack.Manifest,
			ManifestPath:   pack.ManifestPath,
			ActivePath:     activePath,
			ContentReceipt: receipt,
			Root:           pack.Root,
		}
		report, err := contentpacks.ReplayManifestSamples(ctx, pack.Manifest, pack.Root, opts)
		if err != nil {
			return out, fmt.Errorf("replay active content pack %s: %w", activePath, err)
		}
		detectionReport := replayManifestDetectionsForImport(ctx, pack, opts)
		record, installed, err := ensureContentPackInstalled(registry, pack.Manifest, at)
		if err != nil {
			return out, err
		}
		action := ContentPackRegistryActionAlreadyInstalled
		if !report.Passed() || !detectionReport.Passed() {
			quarantined, err := registry.Quarantine(pack.Manifest.PackID, pack.Manifest.PackVersion, contentPackQuarantineReason(report, detectionReport), at)
			if err != nil {
				return out, err
			}
			record = *quarantined
			action = ContentPackRegistryActionQuarantined
		} else if record.Status == contentpacks.PackStatus(contentpacks.PackStatusQuarantined) {
			action = ContentPackRegistryActionQuarantined
		} else if record.Status == contentpacks.PackStatus(contentpacks.PackStatusEnabled) {
			action = ContentPackRegistryActionAlreadyEnabled
		} else {
			enabled, err := registry.Enable(pack.Manifest.PackID, pack.Manifest.PackVersion, at)
			if err != nil {
				return out, err
			}
			record = *enabled
			if installed {
				action = ContentPackRegistryActionInstalledEnabled
			} else {
				action = ContentPackRegistryActionAlreadyInstalled
			}
		}
		out = append(out, ActiveContentPackRegistrySync{
			ActiveContentPack: active,
			Report:            report,
			DetectionReport:   detectionReport,
			Record:            record,
			Action:            action,
		})
	}
	return out, nil
}

func ReplayActiveContentPacks(ctx context.Context, rootDir string, opts contentpacks.SampleReplayOptions) ([]ActiveContentPackReplay, error) {
	activeRoot := filepath.Join(strings.TrimSpace(rootDir), "active", cleanName(ContentTypeSIEMContentPack))
	matches, err := filepath.Glob(filepath.Join(activeRoot, "*"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	out := make([]ActiveContentPackReplay, 0, len(matches))
	for _, activePath := range matches {
		if strings.HasSuffix(activePath, ".receipt.json") {
			continue
		}
		pack, receipt, err := readActiveContentPack(activePath)
		if err != nil {
			return nil, err
		}
		report, err := contentpacks.ReplayManifestSamples(ctx, pack.Manifest, pack.Root, opts)
		if err != nil {
			return nil, fmt.Errorf("replay active content pack %s: %w", activePath, err)
		}
		detectionReport := replayManifestDetectionsForImport(ctx, pack, opts)
		out = append(out, ActiveContentPackReplay{
			ActiveContentPack: ActiveContentPack{
				Manifest:       pack.Manifest,
				ManifestPath:   pack.ManifestPath,
				ActivePath:     activePath,
				ContentReceipt: receipt,
				Root:           pack.Root,
			},
			Report:          report,
			DetectionReport: detectionReport,
		})
	}
	return out, nil
}

func readActiveContentPack(activePath string) (*contentpacks.PackContent, ContentReceipt, error) {
	data, err := os.ReadFile(activePath)
	if err != nil {
		return nil, ContentReceipt{}, err
	}
	pack, err := contentpacks.ParsePackContent(data)
	if err != nil {
		return nil, ContentReceipt{}, fmt.Errorf("parse active content pack %s: %w", activePath, err)
	}
	return pack, readContentReceipt(activePath + ".receipt.json"), nil
}

func ensureContentPackInstalled(registry *contentpacks.Registry, manifest contentpacks.Manifest, at time.Time) (contentpacks.PackRecord, bool, error) {
	if record, ok := registry.Get(manifest.PackID, manifest.PackVersion); ok {
		return record, false, nil
	}
	record, err := registry.Install(manifest, at)
	if err != nil {
		return contentpacks.PackRecord{}, false, err
	}
	return *record, true, nil
}

func replayQuarantineReason(report contentpacks.SampleReplayReport) string {
	if report.FailedCases == 0 && len(report.Failures) == 0 {
		return "content pack replay did not pass"
	}
	if len(report.Failures) == 0 {
		return fmt.Sprintf("content pack replay failed: %d case(s) failed", report.FailedCases)
	}
	first := report.Failures[0]
	detail := strings.TrimSpace(first.Error)
	if detail == "" {
		detail = fmt.Sprintf("%s want=%s got=%s", first.Field, first.Want, first.Got)
	}
	return fmt.Sprintf("content pack replay failed: case=%s index=%d %s", first.CaseID, first.Index, detail)
}

func replayManifestDetectionsForImport(ctx context.Context, pack *contentpacks.PackContent, opts contentpacks.SampleReplayOptions) contentpacks.DetectionReplayReport {
	report, err := contentpacks.ReplayManifestDetections(ctx, pack.Manifest, pack.Root, contentpacks.DetectionReplayOptions{
		MaxSampleBytes: opts.MaxSampleBytes,
	})
	if err == nil {
		return report
	}
	return contentpacks.DetectionReplayReport{
		PackID:      strings.TrimSpace(pack.Manifest.PackID),
		PackVersion: strings.TrimSpace(pack.Manifest.PackVersion),
		TotalRules:  len(pack.Manifest.Detections),
		TotalCases:  len(pack.Manifest.Samples),
		Failures: []contentpacks.DetectionReplayFailure{{
			Index: -1,
			Error: err.Error(),
		}},
	}
}

func contentPackQuarantineReason(sampleReport contentpacks.SampleReplayReport, detectionReport contentpacks.DetectionReplayReport) string {
	if !sampleReport.Passed() {
		return replayQuarantineReason(sampleReport)
	}
	return detectionReplayQuarantineReason(detectionReport)
}

func detectionReplayQuarantineReason(report contentpacks.DetectionReplayReport) string {
	if len(report.Failures) == 0 {
		return "content pack detection replay did not pass"
	}
	first := report.Failures[0]
	detectionID := strings.TrimSpace(first.DetectionID)
	if detectionID == "" {
		detectionID = "unknown"
	}
	return fmt.Sprintf("content pack detection replay failed: case=%s index=%d detection=%s %s", first.CaseID, first.Index, detectionID, strings.TrimSpace(first.Error))
}
