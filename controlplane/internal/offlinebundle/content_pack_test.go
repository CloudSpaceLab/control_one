package offlinebundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

func TestImportSignedContentPackAndReplayActiveGoldens(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 27, 16, 0, 0, 0, time.UTC)
	body := makeSIEMContentPackBundle(t, priv, now)

	root := t.TempDir()
	receipt, err := Import(context.Background(), bytes.NewReader(body), ImportOptions{
		RootDir:   root,
		PublicKey: pub,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("import content pack bundle: %v", err)
	}
	if len(receipt.Contents) != 1 || receipt.Contents[0].Type != ContentTypeSIEMContentPack {
		t.Fatalf("receipt contents = %#v", receipt.Contents)
	}

	packs, err := LoadActiveContentPacks(root)
	if err != nil {
		t.Fatalf("load active content packs: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("packs = %#v, want one", packs)
	}
	if packs[0].Manifest.PackID != "controlone.offline_replay" || packs[0].ContentReceipt.BundleID != "siem-pack" {
		t.Fatalf("missing manifest/receipt provenance: %#v", packs[0])
	}

	replays, err := ReplayActiveContentPacks(context.Background(), root, contentpacks.SampleReplayOptions{})
	if err != nil {
		t.Fatalf("replay active content packs: %v", err)
	}
	if len(replays) != 1 || !replays[0].Report.Passed() {
		t.Fatalf("replays = %#v, want passing report", replays)
	}
	if !replays[0].DetectionReport.Passed() {
		t.Fatalf("detection replay = %#v, want pass", replays[0].DetectionReport)
	}

	registry := contentpacks.NewRegistry("1.0.0")
	synced, err := SyncActiveContentPacksToRegistry(context.Background(), root, registry, contentpacks.SampleReplayOptions{}, now)
	if err != nil {
		t.Fatalf("sync active content packs: %v", err)
	}
	if len(synced) != 1 || synced[0].Action != ContentPackRegistryActionInstalledEnabled {
		t.Fatalf("synced = %#v, want installed_enabled", synced)
	}
	if synced[0].Record.Status != contentpacks.PackStatusEnabled {
		t.Fatalf("record status = %q, want enabled", synced[0].Record.Status)
	}
	if !synced[0].DetectionReport.Passed() {
		t.Fatalf("sync detection replay = %#v, want pass", synced[0].DetectionReport)
	}
	resolved, ok := registry.ResolveSource("controlone.offline")
	if !ok || resolved.PackID != "controlone.offline_replay" {
		t.Fatalf("resolved = %#v ok=%v, want enabled offline source", resolved, ok)
	}

	again, err := SyncActiveContentPacksToRegistry(context.Background(), root, registry, contentpacks.SampleReplayOptions{}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second sync active content packs: %v", err)
	}
	if len(again) != 1 || again[0].Action != ContentPackRegistryActionAlreadyEnabled {
		t.Fatalf("second sync = %#v, want already_enabled", again)
	}
}

func TestSyncActiveContentPackQuarantinesReplayFailure(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 27, 17, 0, 0, 0, time.UTC)
	body := makeSIEMContentPackBundleWithArchive(t, priv, now, makeSIEMContentPackArchive(t, true))

	root := t.TempDir()
	if _, err := Import(context.Background(), bytes.NewReader(body), ImportOptions{
		RootDir:   root,
		PublicKey: pub,
		Now:       now,
	}); err != nil {
		t.Fatalf("import content pack bundle: %v", err)
	}

	registry := contentpacks.NewRegistry("1.0.0")
	synced, err := SyncActiveContentPacksToRegistry(context.Background(), root, registry, contentpacks.SampleReplayOptions{}, now)
	if err != nil {
		t.Fatalf("sync active content packs: %v", err)
	}
	if len(synced) != 1 || synced[0].Action != ContentPackRegistryActionQuarantined {
		t.Fatalf("synced = %#v, want quarantined", synced)
	}
	if synced[0].Record.Status != contentpacks.PackStatusQuarantined || synced[0].Record.QuarantineReason == "" {
		t.Fatalf("record = %#v, want quarantined with reason", synced[0].Record)
	}
	if _, ok := registry.ResolveSource("controlone.offline"); ok {
		t.Fatal("ResolveSource() ok = true for quarantined replay-failed pack")
	}

	again, err := SyncActiveContentPacksToRegistry(context.Background(), root, registry, contentpacks.SampleReplayOptions{}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second sync active content packs: %v", err)
	}
	if len(again) != 1 || again[0].Action != ContentPackRegistryActionQuarantined {
		t.Fatalf("second sync = %#v, want still quarantined", again)
	}
}

func TestSyncActiveContentPackQuarantinesDetectionReplayFailure(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 27, 18, 0, 0, 0, time.UTC)
	body := makeSIEMContentPackBundleWithArchive(t, priv, now, makeSIEMContentPackArchiveWithBrokenDetection(t))

	root := t.TempDir()
	if _, err := Import(context.Background(), bytes.NewReader(body), ImportOptions{
		RootDir:   root,
		PublicKey: pub,
		Now:       now,
	}); err != nil {
		t.Fatalf("import content pack bundle: %v", err)
	}

	replays, err := ReplayActiveContentPacks(context.Background(), root, contentpacks.SampleReplayOptions{})
	if err != nil {
		t.Fatalf("replay active content packs: %v", err)
	}
	if len(replays) != 1 || replays[0].DetectionReport.Passed() {
		t.Fatalf("detection replays = %#v, want detection failure", replays)
	}

	registry := contentpacks.NewRegistry("1.0.0")
	synced, err := SyncActiveContentPacksToRegistry(context.Background(), root, registry, contentpacks.SampleReplayOptions{}, now)
	if err != nil {
		t.Fatalf("sync active content packs: %v", err)
	}
	if len(synced) != 1 || synced[0].Action != ContentPackRegistryActionQuarantined {
		t.Fatalf("synced = %#v, want quarantined", synced)
	}
	if !synced[0].Report.Passed() {
		t.Fatalf("parser report = %#v, want parser replay to pass", synced[0].Report)
	}
	if synced[0].DetectionReport.Passed() {
		t.Fatalf("detection report = %#v, want failure", synced[0].DetectionReport)
	}
	if !strings.Contains(synced[0].Record.QuarantineReason, "detection replay failed") ||
		!strings.Contains(synced[0].Record.QuarantineReason, "read detections/missing.yml") {
		t.Fatalf("quarantine reason = %q", synced[0].Record.QuarantineReason)
	}
	if _, ok := registry.ResolveSource("controlone.offline"); ok {
		t.Fatal("ResolveSource() ok = true for detection-failed pack")
	}
}

func makeSIEMContentPackBundle(t *testing.T, priv ed25519.PrivateKey, now time.Time) []byte {
	t.Helper()
	return makeSIEMContentPackBundleWithArchive(t, priv, now, makeSIEMContentPackArchive(t, false))
}

func makeSIEMContentPackBundleWithArchive(t *testing.T, priv ed25519.PrivateKey, now time.Time, packArchive []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(packArchive)
	contentExpires := now.Add(24 * time.Hour)
	manifest := Manifest{
		SchemaVersion: 1,
		BundleID:      "siem-pack",
		Version:       "2026.05.27",
		Sequence:      1,
		IssuedAt:      now.Add(-time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		Contents: []ContentFile{{
			Type:      ContentTypeSIEMContentPack,
			Name:      "controlone-offline-replay",
			Version:   "1.0.0",
			Path:      "content/controlone-offline-replay.c1pack",
			SHA256:    hex.EncodeToString(sum[:]),
			ExpiresAt: &contentExpires,
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal bundle manifest: %v", err)
	}
	sig := ed25519.Sign(priv, manifestBytes)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	addTarFile(t, tw, ManifestPath, manifestBytes)
	addTarFile(t, tw, SignaturePath, []byte(base64.StdEncoding.EncodeToString(sig)))
	addTarFile(t, tw, "content/controlone-offline-replay.c1pack", packArchive)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func makeSIEMContentPackArchive(t *testing.T, badGolden bool) []byte {
	t.Helper()
	manifestBytes, err := json.Marshal(offlineReplayManifest())
	if err != nil {
		t.Fatalf("marshal content pack manifest: %v", err)
	}
	golden := []byte(`{"status":"parsed","fields":{"event":{"kind":"event"},"status":200,"user":"alice"}}` + "\n")
	if badGolden {
		golden = []byte(`{"status":"parsed","fields":{"event":{"kind":"event"},"status":403,"user":"alice"}}` + "\n")
	}
	files := map[string][]byte{
		"manifest.yaml":             []byte(manifestBytes),
		"samples/test.jsonl":        []byte(`{"raw":"{\"status\":200,\"user\":\"alice\"}"}` + "\n"),
		"samples/test.golden.jsonl": golden,
	}
	return makeSIEMContentPackArchiveFromFiles(t, files)
}

func makeSIEMContentPackArchiveWithBrokenDetection(t *testing.T) []byte {
	t.Helper()
	manifest := offlineReplayManifest()
	manifest.Detections = []contentpacks.Detection{{
		DetectionID: "controlone.offline.detect",
		Title:       "Offline Missing Detection",
		Kind:        contentpacks.DetectionKindSigma,
		Path:        "detections/missing.yml",
		Severity:    "low",
	}}
	manifest.Sources[0].Detections = []string{"controlone.offline.detect"}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal content pack manifest: %v", err)
	}
	files := map[string][]byte{
		"manifest.yaml":             []byte(manifestBytes),
		"samples/test.jsonl":        []byte(`{"raw":"{\"status\":200,\"user\":\"alice\"}"}` + "\n"),
		"samples/test.golden.jsonl": []byte(`{"status":"parsed","fields":{"event":{"kind":"event"},"status":200,"user":"alice"}}` + "\n"),
	}
	return makeSIEMContentPackArchiveFromFiles(t, files)
}

func makeSIEMContentPackArchiveFromFiles(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("write pack tar header: %v", err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write pack tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close pack tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close pack gzip: %v", err)
	}
	return buf.Bytes()
}

func offlineReplayManifest() contentpacks.Manifest {
	return contentpacks.Manifest{
		SchemaVersion: contentpacks.SchemaVersion,
		PackID:        "controlone.offline_replay",
		PackVersion:   "1.0.0",
		DisplayName:   "Offline Replay Pack",
		License: contentpacks.LicenseMetadata{
			SPDX: "Apache-2.0",
		},
		Provenance: contentpacks.Provenance{
			Author: "Control One",
		},
		Sources: []contentpacks.SourceProfile{{
			SourceID:        "controlone.offline",
			DisplayName:     "Control One Offline Replay",
			Product:         "control_one",
			SourceClass:     "test",
			RiskClass:       contentpacks.RiskLow,
			DataSensitivity: contentpacks.SensitivityLow,
			CollectorModes:  []string{contentpacks.CollectorNodeFileLog},
			Schemas: contentpacks.SchemaBinding{
				Primary: contentpacks.SchemaOCSF,
				OCSF: contentpacks.OCSFBinding{
					Category: "application_activity",
					Class:    "application_event",
				},
			},
			Parsers: []string{"controlone.offline.json"},
			Samples: []string{"controlone.offline.good"},
		}},
		Parsers: []contentpacks.ParserProfile{{
			ParserID:    "controlone.offline.json",
			DisplayName: "Control One Offline JSON",
			Version:     "1.0.0",
			Stages: []contentpacks.ParserStage{
				{Type: contentpacks.StageJSON},
				{Type: contentpacks.StageFieldMap, Config: map[string]any{
					"set": map[string]any{"event.kind": "event"},
				}},
			},
		}},
		Samples: []contentpacks.SampleCase{{
			CaseID:     "controlone.offline.good",
			SourceID:   "controlone.offline",
			ParserID:   "controlone.offline.json",
			InputPath:  "samples/test.jsonl",
			GoldenPath: "samples/test.golden.jsonl",
		}},
	}
}
