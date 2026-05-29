package contentpacks

import (
	"strings"
	"testing"
	"time"
)

func TestRegistryInstallsAndEnablesCompatiblePack(t *testing.T) {
	now := time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC)
	manifest := *mustManifest(t, validPackYAML)
	manifest.MinControlOneVersion = "1.2.0"
	registry := NewRegistry("1.3.0")

	record, err := registry.Install(manifest, now)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if record.Status != PackStatus(PackStatusInstalled) || !record.Compatible {
		t.Fatalf("installed record = %#v", record)
	}
	enabled, err := registry.Enable(manifest.PackID, manifest.PackVersion, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if enabled.Status != PackStatus(PackStatusEnabled) || enabled.EnabledAt == nil {
		t.Fatalf("enabled record = %#v", enabled)
	}

	resolved, ok := registry.ResolveSource("nginx.access")
	if !ok {
		t.Fatal("ResolveSource() ok = false")
	}
	if resolved.PackVersion != "1.0.0" || resolved.Source.SourceID != "nginx.access" {
		t.Fatalf("resolved = %#v", resolved)
	}
	if len(resolved.Parsers) != 1 || resolved.Parsers[0].ParserID != "nginx.access.combined" {
		t.Fatalf("resolved parsers = %#v", resolved.Parsers)
	}
	if len(resolved.Detections) != 1 || len(resolved.Samples) != 1 {
		t.Fatalf("resolved detections/samples = %#v %#v", resolved.Detections, resolved.Samples)
	}
}

func TestRegistryRejectsDuplicatePackVersion(t *testing.T) {
	manifest := *mustManifest(t, validPackYAML)
	registry := NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, time.Now()); err != nil {
		t.Fatalf("first Install() error = %v", err)
	}
	if _, err := registry.Install(manifest, time.Now()); err == nil {
		t.Fatal("second Install() error = nil, want duplicate error")
	}
}

func TestRegistryRejectsIncompatiblePack(t *testing.T) {
	manifest := *mustManifest(t, validPackYAML)
	manifest.MinControlOneVersion = "9.0.0"
	registry := NewRegistry("1.0.0")
	_, err := registry.Install(manifest, time.Now())
	if err == nil {
		t.Fatal("Install() error = nil, want compatibility error")
	}
	if !strings.Contains(err.Error(), "requires Control One") {
		t.Fatalf("error = %v, want compatibility detail", err)
	}
}

func TestRegistryDefensivelyCopiesInstalledManifest(t *testing.T) {
	manifest := *mustManifest(t, validPackYAML)
	registry := NewRegistry("1.0.0")
	record, err := registry.Install(manifest, time.Now())
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	manifest.Sources[0].SourceID = "tampered.original"
	record.Manifest.Sources[0].SourceID = "tampered.returned"

	if _, err := registry.Enable("controlone.nginx", "1.0.0", time.Now()); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	resolved, ok := registry.ResolveSource("nginx.access")
	if !ok {
		t.Fatal("ResolveSource(nginx.access) ok = false after external mutation")
	}
	resolved.Source.SourceID = "tampered.resolved"
	again, ok := registry.ResolveSource("nginx.access")
	if !ok {
		t.Fatal("second ResolveSource(nginx.access) ok = false")
	}
	if again.Source.SourceID != "nginx.access" {
		t.Fatalf("registry source mutated through resolved copy: %q", again.Source.SourceID)
	}
}

func TestRegistryEnablingNewPackVersionCreatesRollbackCandidate(t *testing.T) {
	now := time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC)
	v1 := *mustManifest(t, validPackYAML)
	v2 := *mustManifest(t, validPackYAML)
	v2.PackVersion = "1.1.0"
	registry := NewRegistry("1.0.0")

	if _, err := registry.Install(v1, now); err != nil {
		t.Fatalf("install v1: %v", err)
	}
	if _, err := registry.Enable(v1.PackID, v1.PackVersion, now); err != nil {
		t.Fatalf("enable v1: %v", err)
	}
	if _, err := registry.Install(v2, now.Add(time.Minute)); err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if _, err := registry.Enable(v2.PackID, v2.PackVersion, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("enable v2: %v", err)
	}

	oldRecord, ok := registry.Get(v1.PackID, v1.PackVersion)
	if !ok {
		t.Fatal("v1 record missing")
	}
	if oldRecord.Status != PackStatus(PackStatusRollbackAvailable) {
		t.Fatalf("v1 status = %q, want rollback_available", oldRecord.Status)
	}
	resolved, ok := registry.ResolveSource("nginx.access")
	if !ok {
		t.Fatal("ResolveSource() ok = false")
	}
	if resolved.PackVersion != "1.1.0" {
		t.Fatalf("resolved version = %q, want 1.1.0", resolved.PackVersion)
	}
}

func TestRegistryRejectsEnabledSourceConflictAcrossPacks(t *testing.T) {
	now := time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC)
	primary := *mustManifest(t, validPackYAML)
	other := *mustManifest(t, validPackYAML)
	other.PackID = "controlone.altnginx"
	registry := NewRegistry("1.0.0")

	if _, err := registry.Install(primary, now); err != nil {
		t.Fatalf("install primary: %v", err)
	}
	if _, err := registry.Enable(primary.PackID, primary.PackVersion, now); err != nil {
		t.Fatalf("enable primary: %v", err)
	}
	if _, err := registry.Install(other, now); err != nil {
		t.Fatalf("install other: %v", err)
	}
	if _, err := registry.Enable(other.PackID, other.PackVersion, now); err == nil {
		t.Fatal("Enable(conflicting pack) error = nil, want conflict")
	}
}

func TestRegistryQuarantinePreventsResolutionAndEnable(t *testing.T) {
	now := time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC)
	manifest := *mustManifest(t, validPackYAML)
	registry := NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := registry.Quarantine(manifest.PackID, manifest.PackVersion, "parser regression", now); err != nil {
		t.Fatalf("quarantine: %v", err)
	}
	if _, ok := registry.ResolveSource("nginx.access"); ok {
		t.Fatal("ResolveSource() ok = true for quarantined pack")
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err == nil {
		t.Fatal("Enable(quarantined) error = nil, want quarantine error")
	}
}

func TestRegistrySnapshotRestorePreservesLifecycleState(t *testing.T) {
	now := time.Date(2026, 5, 27, 18, 0, 0, 0, time.UTC)
	enabledManifest := *mustManifest(t, validPackYAML)
	quarantinedManifest := *mustManifest(t, validPackYAML)
	quarantinedManifest.PackID = "controlone.auditd"
	quarantinedManifest.Sources[0].SourceID = "linux.auditd"
	quarantinedManifest.Sources[0].DisplayName = "Linux auditd"
	quarantinedManifest.Sources[0].Product = "auditd"
	quarantinedManifest.Sources[0].Parsers = []string{"linux.auditd.raw"}
	quarantinedManifest.Sources[0].Samples = []string{"linux.auditd.raw.good"}
	quarantinedManifest.Sources = []SourceProfile{quarantinedManifest.Sources[0]}
	quarantinedManifest.Parsers = []ParserProfile{{
		ParserID:    "linux.auditd.raw",
		DisplayName: "Linux auditd raw",
		Version:     "1.0.0",
		Stages:      []ParserStage{{Type: StageJSON}},
	}}
	quarantinedManifest.Samples = []SampleCase{{
		CaseID:     "linux.auditd.raw.good",
		SourceID:   "linux.auditd",
		ParserID:   "linux.auditd.raw",
		InputPath:  "samples/auditd.jsonl",
		GoldenPath: "samples/auditd.golden.jsonl",
	}}

	registry := NewRegistry("1.0.0")
	if _, err := registry.Install(enabledManifest, now); err != nil {
		t.Fatalf("install enabled manifest: %v", err)
	}
	if _, err := registry.Enable(enabledManifest.PackID, enabledManifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := registry.Install(quarantinedManifest, now); err != nil {
		t.Fatalf("install quarantined manifest: %v", err)
	}
	if _, err := registry.Quarantine(quarantinedManifest.PackID, quarantinedManifest.PackVersion, "bad golden", now); err != nil {
		t.Fatalf("quarantine manifest: %v", err)
	}

	restored, err := NewRegistryFromSnapshot(registry.Snapshot(now), "")
	if err != nil {
		t.Fatalf("NewRegistryFromSnapshot() error = %v", err)
	}
	if _, ok := restored.ResolveSource("nginx.access"); !ok {
		t.Fatal("ResolveSource(nginx.access) ok = false after restore")
	}
	if _, ok := restored.ResolveSource("linux.auditd"); ok {
		t.Fatal("ResolveSource(linux.auditd) ok = true for restored quarantined pack")
	}
	record, ok := restored.Get(quarantinedManifest.PackID, quarantinedManifest.PackVersion)
	if !ok || record.Status != PackStatusQuarantined || record.QuarantineReason != "bad golden" {
		t.Fatalf("restored quarantine record = %#v ok=%v", record, ok)
	}
}

func TestRegistrySnapshotRestoreRejectsEnabledSourceConflict(t *testing.T) {
	now := time.Date(2026, 5, 27, 18, 30, 0, 0, time.UTC)
	first := *mustManifest(t, validPackYAML)
	second := *mustManifest(t, validPackYAML)
	second.PackID = "controlone.othernginx"
	registry := NewRegistry("1.0.0")
	firstRecord, err := registry.Install(first, now)
	if err != nil {
		t.Fatalf("install first: %v", err)
	}
	secondRecord, err := registry.Install(second, now)
	if err != nil {
		t.Fatalf("install second: %v", err)
	}
	firstRecord.Status = PackStatusEnabled
	secondRecord.Status = PackStatusEnabled
	snapshot := RegistrySnapshot{
		SchemaVersion:     SchemaVersion,
		ControlOneVersion: "1.0.0",
		ExportedAt:        now,
		Packs:             []PackRecord{*firstRecord, *secondRecord},
	}
	_, err = NewRegistryFromSnapshot(snapshot, "")
	if err == nil || !strings.Contains(err.Error(), "enabled source conflict") {
		t.Fatalf("restore err = %v, want enabled source conflict", err)
	}
}

func TestCompareSemverHandlesReleaseAfterPrerelease(t *testing.T) {
	if CompareSemver("1.0.0", "1.0.0-beta.1") <= 0 {
		t.Fatal("release should compare greater than prerelease")
	}
	if CompareSemver("1.10.0", "1.2.0") <= 0 {
		t.Fatal("numeric minor comparison should not sort lexically")
	}
}
