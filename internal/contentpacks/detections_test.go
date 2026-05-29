package contentpacks

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CloudSpaceLab/control_one/internal/detections"
)

func TestLoadManifestDetectionsLoadsSigmaRule(t *testing.T) {
	manifest := detectionLoadManifest()
	root := fstest.MapFS{
		"detections/powershell.yml": {
			Data: powershellSigmaRule(),
		},
	}

	loaded, err := LoadManifestDetections(context.Background(), manifest, root, DetectionLoadOptions{
		SigmaFieldMap: map[string]string{
			"Image":       "process.executable",
			"CommandLine": "process.command_line",
		},
	})
	if err != nil {
		t.Fatalf("LoadManifestDetections() error = %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded = %d, want 1", len(loaded))
	}
	rule := loaded[0].Rule
	if loaded[0].Path != "detections/powershell.yml" {
		t.Fatalf("path = %q", loaded[0].Path)
	}
	if rule.ID != "windows.powershell.encoded" || rule.Title != "Manifest Encoded PowerShell" || rule.Severity != "critical" {
		t.Fatalf("rule metadata = %#v", rule)
	}
	if rule.RiskScore != 91 {
		t.Fatalf("rule risk score = %d, want manifest-pinned 91", rule.RiskScore)
	}
	wantTags := []string{"attack.execution", "attack.t1059.001", "controlone.bank"}
	if strings.Join(rule.Tags, ",") != strings.Join(wantTags, ",") {
		t.Fatalf("tags = %#v, want %#v", rule.Tags, wantTags)
	}

	match := rule.Evaluate(detections.Event{Fields: map[string]any{
		"process.executable":   `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"process.command_line": "powershell.exe -NoProfile -enc SQBFAFgA",
	}})
	if !match.Matched || match.RuleID != "windows.powershell.encoded" {
		t.Fatalf("match = %#v, want manifest-pinned match", match)
	}
}

func TestReplayManifestDetectionsEvaluatesGoldenEvents(t *testing.T) {
	manifest := detectionLoadManifest()
	root := fstest.MapFS{
		"detections/powershell.yml": {
			Data: powershellSigmaRule(),
		},
		"samples/test.golden.jsonl": {
			Data: []byte(`{"status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","process.command_line":"powershell.exe -NoProfile -enc SQBFAFgA"}}` + "\n" +
				`{"status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\cmd.exe","process.command_line":"cmd.exe /c whoami"}}` + "\n"),
		},
	}

	report, err := ReplayManifestDetections(context.Background(), manifest, root, DetectionReplayOptions{
		DetectionLoadOptions: DetectionLoadOptions{
			SigmaFieldMap: map[string]string{
				"Image":       "process.executable",
				"CommandLine": "process.command_line",
			},
		},
	})
	if err != nil {
		t.Fatalf("ReplayManifestDetections() error = %v", err)
	}
	if !report.Passed() {
		t.Fatalf("report = %#v, want pass", report)
	}
	if report.TotalRules != 1 || report.TotalCases != 1 || report.TotalEvents != 2 || report.TotalEvaluations != 2 || report.TotalMatches != 1 {
		t.Fatalf("report counts = %#v", report)
	}
	if len(report.Results) != 1 || len(report.Results[0].Matches) != 1 {
		t.Fatalf("matches = %#v", report.Results)
	}
	match := report.Results[0].Matches[0]
	if match.DetectionID != "windows.powershell.encoded" || match.Index != 0 || match.Severity != "critical" {
		t.Fatalf("match = %#v, want manifest-pinned first event", match)
	}
	if match.RiskScore != 91 {
		t.Fatalf("match risk score = %d, want manifest-pinned 91", match.RiskScore)
	}
}

func TestReplayManifestDetectionsEvaluatesTemporalThreshold(t *testing.T) {
	manifest := detectionLoadManifest()
	manifest.Detections[0].Temporal = &DetectionTemporal{
		Kind:          "threshold",
		WindowSeconds: 60,
		Threshold:     2,
		GroupBy:       []string{"user.name"},
	}
	root := fstest.MapFS{
		"detections/powershell.yml": {Data: powershellSigmaRule()},
		"samples/test.golden.jsonl": {Data: []byte(`{"timestamp":"2026-05-29T12:00:00Z","parser_id":"windows.sysmon","status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","process.command_line":"powershell.exe -enc one","user.name":"alice"}}
{"timestamp":"2026-05-29T12:00:10Z","parser_id":"windows.sysmon","status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","process.command_line":"powershell.exe -enc two","user.name":"alice"}}
`)},
	}
	report, err := ReplayManifestDetections(context.Background(), manifest, root, DetectionReplayOptions{
		DetectionLoadOptions: DetectionLoadOptions{SigmaFieldMap: DefaultSigmaFieldMap()},
	})
	if err != nil {
		t.Fatalf("ReplayManifestDetections() error = %v", err)
	}
	if report.TotalEvaluations != 2 || report.TotalMatches != 1 {
		t.Fatalf("report = %#v, want one temporal match after two evaluations", report)
	}
	match := report.Results[0].Matches[0]
	if match.Index != 1 || match.DetectionID != "windows.powershell.encoded" {
		t.Fatalf("match = %#v", match)
	}
}

func TestReplayManifestDetectionsEvaluatesTemporalSequence(t *testing.T) {
	manifest := detectionLoadManifest()
	manifest.Detections[0].Temporal = &DetectionTemporal{
		Kind:          "sequence",
		WindowSeconds: 60,
		GroupBy:       []string{"user.name"},
		Sequence: []DetectionTemporalStep{
			{Field: "process.command_line", Op: "contains", Values: []any{"stage-one"}},
			{Field: "process.command_line", Op: "contains", Values: []any{"stage-two"}},
		},
	}
	root := fstest.MapFS{
		"detections/powershell.yml": {Data: powershellSigmaRule()},
		"samples/test.golden.jsonl": {Data: []byte(`{"timestamp":"2026-05-29T12:00:00Z","parser_id":"windows.sysmon","status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","process.command_line":"powershell.exe -enc stage-one","user.name":"alice"}}
{"timestamp":"2026-05-29T12:00:10Z","parser_id":"windows.sysmon","status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","process.command_line":"powershell.exe -enc stage-two","user.name":"alice"}}
`)}}
	report, err := ReplayManifestDetections(context.Background(), manifest, root, DetectionReplayOptions{
		DetectionLoadOptions: DetectionLoadOptions{SigmaFieldMap: DefaultSigmaFieldMap()},
	})
	if err != nil {
		t.Fatalf("ReplayManifestDetections() error = %v", err)
	}
	if report.TotalEvaluations != 2 || report.TotalMatches != 1 {
		t.Fatalf("report = %#v, want one sequence match after two evaluations", report)
	}
	match := report.Results[0].Matches[0]
	if match.Index != 1 || match.DetectionID != "windows.powershell.encoded" {
		t.Fatalf("match = %#v", match)
	}
}

func TestReplayManifestDetectionsEvaluatesTemporalJoin(t *testing.T) {
	manifest := detectionLoadManifest()
	manifest.Detections[0].Temporal = &DetectionTemporal{
		Kind:          "join",
		WindowSeconds: 60,
		GroupBy:       []string{"user.name"},
		Join: []DetectionTemporalStep{
			{Field: "process.command_line", Op: "contains", Values: []any{"stage-one"}},
			{Field: "process.command_line", Op: "contains", Values: []any{"stage-two"}},
		},
	}
	root := fstest.MapFS{
		"detections/powershell.yml": {Data: powershellSigmaRule()},
		"samples/test.golden.jsonl": {Data: []byte(`{"timestamp":"2026-05-29T12:00:00Z","parser_id":"windows.sysmon","status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","process.command_line":"powershell.exe -enc stage-two","user.name":"alice"}}
{"timestamp":"2026-05-29T12:00:10Z","parser_id":"windows.sysmon","status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","process.command_line":"powershell.exe -enc stage-one","user.name":"alice"}}
`)}}
	report, err := ReplayManifestDetections(context.Background(), manifest, root, DetectionReplayOptions{
		DetectionLoadOptions: DetectionLoadOptions{SigmaFieldMap: DefaultSigmaFieldMap()},
	})
	if err != nil {
		t.Fatalf("ReplayManifestDetections() error = %v", err)
	}
	if report.TotalEvaluations != 2 || report.TotalMatches != 1 {
		t.Fatalf("report = %#v, want one join match after two evaluations", report)
	}
	match := report.Results[0].Matches[0]
	if match.Index != 1 || match.DetectionID != "windows.powershell.encoded" {
		t.Fatalf("match = %#v", match)
	}
}

func TestReplayManifestDetectionsReportsGoldenReadFailure(t *testing.T) {
	manifest := detectionLoadManifest()
	root := fstest.MapFS{
		"detections/powershell.yml": {
			Data: powershellSigmaRule(),
		},
	}

	report, err := ReplayManifestDetections(context.Background(), manifest, root, DetectionReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestDetections() error = %v", err)
	}
	if report.Passed() {
		t.Fatalf("report passed, want golden read failure")
	}
	if len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "read samples/test.golden.jsonl") {
		t.Fatalf("failures = %#v, want missing golden detail", report.Failures)
	}
}

func TestLoadManifestDetectionsRejectsTraversalPath(t *testing.T) {
	manifest := detectionLoadManifest()
	manifest.Detections[0].Path = "detections/../secret.yml"
	root := fstest.MapFS{
		"secret.yml": {Data: []byte("title: secret\n")},
	}

	_, err := LoadManifestDetections(context.Background(), manifest, root, DetectionLoadOptions{})
	if err == nil {
		t.Fatal("LoadManifestDetections() error = nil, want traversal rejection")
	}
	if !strings.Contains(err.Error(), "escapes content pack root") {
		t.Fatalf("error = %v, want traversal detail", err)
	}
}

func TestLoadManifestDetectionsReportsMissingFile(t *testing.T) {
	manifest := detectionLoadManifest()
	manifest.Detections[0].Path = "detections/missing.yml"

	_, err := LoadManifestDetections(context.Background(), manifest, fstest.MapFS{}, DetectionLoadOptions{})
	if err == nil {
		t.Fatal("LoadManifestDetections() error = nil, want missing file")
	}
	if !strings.Contains(err.Error(), "read detections/missing.yml") {
		t.Fatalf("error = %v, want missing file detail", err)
	}
}

func TestLoadManifestDetectionsRejectsUnimplementedControlOneRules(t *testing.T) {
	manifest := detectionLoadManifest()
	manifest.Detections[0].Kind = DetectionKindControlOne
	manifest.Detections[0].Path = ""

	_, err := LoadManifestDetections(context.Background(), manifest, fstest.MapFS{}, DetectionLoadOptions{})
	if err == nil {
		t.Fatal("LoadManifestDetections() error = nil, want unimplemented controlone rule error")
	}
	if !strings.Contains(err.Error(), "controlone detections are not loadable yet") {
		t.Fatalf("error = %v, want unimplemented controlone detail", err)
	}
}

func detectionLoadManifest() Manifest {
	manifest := replayTestManifest()
	manifest.Detections = []Detection{{
		DetectionID: "windows.powershell.encoded",
		Title:       "Manifest Encoded PowerShell",
		Kind:        DetectionKindSigma,
		Path:        "detections/powershell.yml",
		Severity:    "critical",
		RiskScore:   91,
		Tags:        []string{"attack.t1059.001", "controlone.bank"},
	}}
	manifest.Sources[0].Detections = []string{"windows.powershell.encoded"}
	return manifest
}

func powershellSigmaRule() []byte {
	return []byte(`
title: Sigma Encoded PowerShell
tags:
  - attack.execution
  - attack.t1059.001
logsource:
  product: windows
  category: process_creation
detection:
  selection:
    Image|endswith: '\powershell.exe'
    CommandLine|contains:
      - '-enc'
      - 'downloadstring'
  condition: selection
level: medium
`)
}
