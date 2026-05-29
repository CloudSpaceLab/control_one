package contentpacks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePackContentAcceptsDirectManifest(t *testing.T) {
	manifestBytes := mustJSON(t, replayTestManifest())
	pack, err := ParsePackContent(manifestBytes)
	if err != nil {
		t.Fatalf("ParsePackContent() error = %v", err)
	}
	if pack.Manifest.PackID != "controlone.replay_test" {
		t.Fatalf("pack_id = %q", pack.Manifest.PackID)
	}
	if pack.ManifestPath != "manifest.yaml" {
		t.Fatalf("manifest path = %q", pack.ManifestPath)
	}
}

func TestParsePackContentArchiveAndReplay(t *testing.T) {
	archive := makePackArchive(t, map[string][]byte{
		"manifest.yaml":             mustJSON(t, replayTestManifest()),
		"samples/test.jsonl":        []byte(`{"raw":"{\"status\":200,\"user\":\"alice\"}"}` + "\n"),
		"samples/test.golden.jsonl": []byte(`{"status":"parsed","fields":{"event":{"kind":"event"},"status":200,"user":"alice"}}` + "\n"),
	})
	pack, err := ParsePackContent(archive)
	if err != nil {
		t.Fatalf("ParsePackContent() error = %v", err)
	}
	if pack.ManifestPath != "manifest.yaml" {
		t.Fatalf("manifest path = %q", pack.ManifestPath)
	}
	report, err := ReplayManifestSamples(context.Background(), pack.Manifest, pack.Root, SampleReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if !report.Passed() {
		t.Fatalf("report = %#v, want pass", report)
	}
}

func TestReplayPackContentRejectsUnsafeArchivePath(t *testing.T) {
	archive := makePackArchive(t, map[string][]byte{
		"../manifest.yaml": mustJSON(t, replayTestManifest()),
	})
	_, err := ReplayPackContent(context.Background(), archive, SampleReplayOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsafe content pack path") {
		t.Fatalf("ReplayPackContent() error = %v, want unsafe path", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}

func makePackArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}
