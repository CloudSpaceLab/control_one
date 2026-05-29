package contentpacks

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestReplayManifestSamplesPassesGoldenJSONL(t *testing.T) {
	manifest := replayTestManifest()
	root := fstest.MapFS{
		"samples/test.jsonl": {
			Data: []byte(`{"raw":"{\"status\":200,\"user\":\"alice\"}","labels":{"node_id":"node-1"}}` + "\n"),
		},
		"samples/test.golden.jsonl": {
			Data: []byte(`{"parser_id":"controlone.test.json","status":"parsed","fields":{"event":{"kind":"event"},"status":200,"user":"alice"},"labels":{"node_id":"node-1"}}` + "\n"),
		},
	}
	report, err := ReplayManifestSamples(context.Background(), manifest, root, SampleReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if !report.Passed() {
		t.Fatalf("report = %#v, want pass", report)
	}
	if report.TotalCases != 1 || report.PassedCases != 1 || report.TotalEvents != 1 {
		t.Fatalf("report counts = %#v", report)
	}
}

func TestReplayManifestSamplesReportsGoldenMismatch(t *testing.T) {
	manifest := replayTestManifest()
	root := fstest.MapFS{
		"samples/test.jsonl": {
			Data: []byte(`{"raw":"{\"status\":200,\"user\":\"alice\"}"}` + "\n"),
		},
		"samples/test.golden.jsonl": {
			Data: []byte(`{"status":"parsed","fields":{"event":{"kind":"event"},"status":403,"user":"alice"}}` + "\n"),
		},
	}
	report, err := ReplayManifestSamples(context.Background(), manifest, root, SampleReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if report.Passed() {
		t.Fatalf("report passed, want mismatch: %#v", report)
	}
	if len(report.Failures) != 1 || report.Failures[0].Field != "fields" {
		t.Fatalf("failures = %#v, want field mismatch", report.Failures)
	}
}

func TestReplayManifestSamplesCanAllowExtraFields(t *testing.T) {
	manifest := replayTestManifest()
	root := fstest.MapFS{
		"samples/test.jsonl": {
			Data: []byte(`{"raw":"{\"status\":200,\"user\":\"alice\",\"method\":\"GET\"}"}` + "\n"),
		},
		"samples/test.golden.jsonl": {
			Data: []byte(`{"status":"parsed","fields":{"status":200}}` + "\n"),
		},
	}
	report, err := ReplayManifestSamples(context.Background(), manifest, root, SampleReplayOptions{AllowExtraFields: true})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if !report.Passed() {
		t.Fatalf("report = %#v, want pass with subset golden", report)
	}
}

func TestReplayManifestSamplesValidatesNormalizedSchema(t *testing.T) {
	manifest := replayTestManifest()
	root := fstest.MapFS{
		"samples/test.jsonl": {
			Data: []byte(`{"raw":"{\"event\":{\"kind\":\"event\"},\"source\":{\"ip\":\"10.0.0.5\",\"port\":\"443\"}}"}` + "\n"),
		},
		"samples/test.golden.jsonl": {
			Data: []byte(`{"status":"parsed","fields":{"event":{"kind":"event"},"source":{"ip":"10.0.0.5","port":"443"}}}` + "\n"),
		},
	}
	report, err := ReplayManifestSamples(context.Background(), manifest, root, SampleReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if report.Passed() {
		t.Fatalf("report passed, want schema validation failure")
	}
	if len(report.Failures) != 1 || report.Failures[0].Field != "schema.source.port" {
		t.Fatalf("failures = %#v, want source.port schema failure", report.Failures)
	}

	legacyReport, err := ReplayManifestSamples(context.Background(), manifest, root, SampleReplayOptions{DisableSchemaValidation: true})
	if err != nil {
		t.Fatalf("ReplayManifestSamples(disable schema) error = %v", err)
	}
	if !legacyReport.Passed() {
		t.Fatalf("legacyReport = %#v, want pass with schema validation disabled", legacyReport)
	}
}

func TestReplayManifestSamplesAcceptsJSONDocumentEnvelope(t *testing.T) {
	manifest := replayTestManifest()
	manifest.Samples[0].InputPath = "samples/test-input.json"
	manifest.Samples[0].GoldenPath = "samples/test-golden.json"
	root := fstest.MapFS{
		"samples/test-input.json": {
			Data: []byte(`[{"raw":"{\"status\":201,\"user\":\"bob\"}"}]`),
		},
		"samples/test-golden.json": {
			Data: []byte(`{"records":[{"status":"parsed","event":{"fields":{"event":{"kind":"event"},"status":201,"user":"bob"}}}]}`),
		},
	}
	report, err := ReplayManifestSamples(context.Background(), manifest, root, SampleReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if !report.Passed() {
		t.Fatalf("report = %#v, want pass", report)
	}
}

func TestReplayManifestSamplesRejectsTraversalPath(t *testing.T) {
	manifest := replayTestManifest()
	manifest.Samples[0].InputPath = "samples/../secret.jsonl"
	root := fstest.MapFS{
		"secret.jsonl": {
			Data: []byte(`{"raw":"{}"}` + "\n"),
		},
		"samples/test.golden.jsonl": {
			Data: []byte(`{"status":"parsed","fields":{}}` + "\n"),
		},
	}
	report, err := ReplayManifestSamples(context.Background(), manifest, root, SampleReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if report.Passed() {
		t.Fatalf("report passed, want traversal failure")
	}
	if len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "escapes content pack root") {
		t.Fatalf("failures = %#v, want traversal error", report.Failures)
	}
}

func replayTestManifest() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		PackID:        "controlone.replay_test",
		PackVersion:   "1.0.0",
		DisplayName:   "Replay Test Pack",
		License: LicenseMetadata{
			SPDX: "Apache-2.0",
		},
		Provenance: Provenance{
			Author: "Control One",
		},
		Sources: []SourceProfile{{
			SourceID:        "controlone.test",
			DisplayName:     "Control One Test",
			Product:         "control_one",
			SourceClass:     "test",
			RiskClass:       RiskLow,
			DataSensitivity: SensitivityLow,
			CollectorModes:  []string{CollectorNodeFileLog},
			Schemas: SchemaBinding{
				Primary: SchemaOCSF,
				OCSF: OCSFBinding{
					Category: "application_activity",
					Class:    "application_event",
				},
			},
			Parsers: []string{"controlone.test.json"},
			Samples: []string{"controlone.test.good"},
		}},
		Parsers: []ParserProfile{{
			ParserID:    "controlone.test.json",
			DisplayName: "Control One Test JSON",
			Version:     "1.0.0",
			Stages: []ParserStage{
				{Type: StageJSON},
				{Type: StageFieldMap, Config: map[string]any{
					"set": map[string]any{"event.kind": "event"},
				}},
			},
		}},
		Samples: []SampleCase{{
			CaseID:     "controlone.test.good",
			SourceID:   "controlone.test",
			ParserID:   "controlone.test.json",
			InputPath:  "samples/test.jsonl",
			GoldenPath: "samples/test.golden.jsonl",
		}},
	}
}
