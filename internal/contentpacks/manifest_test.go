package contentpacks

import (
	"strings"
	"testing"
)

func TestParseManifestAcceptsBankSafePack(t *testing.T) {
	manifest, err := ParseManifest([]byte(validPackYAML))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if manifest.PackID != "controlone.nginx" {
		t.Fatalf("pack_id = %q", manifest.PackID)
	}
	if len(manifest.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(manifest.Sources))
	}
	if !manifest.Sources[1].ApprovalRequired {
		t.Fatalf("high sensitivity source must stay approval-gated")
	}
}

func TestParseManifestRejectsUnknownField(t *testing.T) {
	_, err := ParseManifest([]byte(validPackYAML + "\nunknown_field: true\n"))
	if err == nil {
		t.Fatal("ParseManifest() error = nil, want unknown field error")
	}
	if !strings.Contains(err.Error(), "field unknown_field not found") {
		t.Fatalf("error = %v, want unknown field detail", err)
	}
}

func TestValidateRejectsHighRiskWithoutApproval(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Sources[1].ApprovalRequired = false
	err := Validate(*manifest)
	assertIssue(t, err, "sources[1].approval_required")
}

func TestValidateRejectsUnsupportedParserStage(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Parsers[0].Stages[0].Type = "lua"
	err := Validate(*manifest)
	assertIssue(t, err, "parsers[0].stages[0].type")
}

func TestValidateRequiresGoldenSampleCoverage(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Samples = manifest.Samples[:1]
	err := Validate(*manifest)
	assertIssue(t, err, "samples")
}

func TestValidateRejectsUnknownSourceSampleReference(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Sources[0].Samples = append(manifest.Sources[0].Samples, "missing.sample")
	err := Validate(*manifest)
	assertIssue(t, err, "sources.nginx.access.samples")
}

func TestValidateRejectsCrossSourceSampleReference(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Sources[0].Samples = append(manifest.Sources[0].Samples, "nginx.error.text.good")
	err := Validate(*manifest)
	assertIssue(t, err, "sources.nginx.access.samples")
}

func TestValidateRejectsSampleParserNotAdvertisedBySource(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Samples[0].ParserID = "nginx.error.text"
	err := Validate(*manifest)
	assertIssue(t, err, "samples[0].parser_id")
}

func TestValidateRejectsDuplicateIDsAndUnknownRefs(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Parsers[1].ParserID = manifest.Parsers[0].ParserID
	manifest.Sources[0].Parsers = append(manifest.Sources[0].Parsers, "missing.parser")
	err := Validate(*manifest)
	assertIssue(t, err, "parsers[1].parser_id")
	assertIssue(t, err, "sources[0].parsers[1]")
}

func TestValidateRejectsMissingOCSFBinding(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Sources[0].Schemas.OCSF.Class = ""
	err := Validate(*manifest)
	assertIssue(t, err, "sources[0].schemas.ocsf.class")
}

func TestValidateRejectsUnsupportedDetectionKind(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Detections[0].Kind = "yara"
	err := Validate(*manifest)
	assertIssue(t, err, "detections[0].kind")
}

func TestValidateRejectsInvalidDetectionRiskScore(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Detections[0].RiskScore = 101
	err := Validate(*manifest)
	assertIssue(t, err, "detections[0].risk_score")
}

func TestValidateAcceptsSequenceDetectionTemporal(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Detections[0].Temporal = &DetectionTemporal{
		Kind:          "sequence",
		WindowSeconds: 120,
		GroupBy:       []string{"source.ip"},
		Sequence: []DetectionTemporalStep{
			{Field: "event.action", Op: "equals", Values: []any{"login_success"}},
			{Field: "event.action", Op: "contains", Values: []any{"admin_grant"}},
		},
	}
	if err := Validate(*manifest); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidSequenceDetectionTemporal(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Detections[0].Temporal = &DetectionTemporal{
		Kind:          "sequence",
		WindowSeconds: 120,
		Sequence: []DetectionTemporalStep{
			{Field: "event.action", Op: "matches", Values: []any{"login_success"}},
		},
	}
	err := Validate(*manifest)
	assertIssue(t, err, "detections[0].temporal.sequence")
	assertIssue(t, err, "detections[0].temporal.sequence[0].op")
}

func TestValidateAcceptsJoinDetectionTemporal(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Detections[0].Temporal = &DetectionTemporal{
		Kind:          "join",
		WindowSeconds: 120,
		GroupBy:       []string{"source.ip"},
		Join: []DetectionTemporalStep{
			{Field: "event.action", Op: "equals", Values: []any{"mfa_reset"}},
			{Field: "event.action", Op: "contains", Values: []any{"admin_grant"}},
		},
	}
	if err := Validate(*manifest); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidJoinDetectionTemporal(t *testing.T) {
	manifest := mustManifest(t, validPackYAML)
	manifest.Detections[0].Temporal = &DetectionTemporal{
		Kind:          "join",
		WindowSeconds: 120,
		Join: []DetectionTemporalStep{
			{Field: "event.action", Op: "matches", Values: []any{"mfa_reset"}},
		},
	}
	err := Validate(*manifest)
	assertIssue(t, err, "detections[0].temporal.join")
	assertIssue(t, err, "detections[0].temporal.join[0].op")
}

func mustManifest(t *testing.T, raw string) *Manifest {
	t.Helper()
	manifest, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("ParseManifest(validPackYAML) error = %v", err)
	}
	return manifest
}

func assertIssue(t *testing.T, err error, path string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want validation issue at %s", path)
	}
	validationErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want *ValidationError: %v", err, err)
	}
	for _, issue := range validationErr.Issues {
		if issue.Path == path {
			return
		}
	}
	t.Fatalf("issues = %#v, want path %s", validationErr.Issues, path)
}

const validPackYAML = `
schema_version: 1
pack_id: controlone.nginx
pack_version: 1.0.0
display_name: Control One NGINX Pack
description: NGINX access and error log profiles for bank pilots.
license:
  spdx: Apache-2.0
provenance:
  author: Control One
sources:
  - source_id: nginx.access
    display_name: NGINX Access Log
    vendor: nginx
    product: nginx
    versions: ["1.x"]
    source_class: webserver
    risk_class: low
    data_sensitivity: moderate
    collector_modes: [node_filelog, otel_filelog, syslog]
    collector_recipes:
      - mode: otel_filelog
        receiver: filelog
        exporter: otlp
        config:
          include:
            - /var/log/nginx/access.log
    approval_required: false
    required_privileges: [read_log_file]
    expected_volume:
      events_per_second: 500
      bytes_per_second: 200000
    raw_retention_default: 7d
    schemas:
      primary: ocsf
      export_aliases: [ecs]
      ocsf:
        category: network_activity
        class: http_activity
        activity: web_request
    parsers: [nginx.access.combined]
    detections: [nginx.web_scanner_burst]
    samples: [nginx.access.combined.good]
  - source_id: nginx.error
    display_name: NGINX Error Log
    vendor: nginx
    product: nginx
    source_class: webserver
    risk_class: medium
    data_sensitivity: high
    collector_modes: [node_filelog, otel_filelog]
    approval_required: true
    schemas:
      primary: ocsf
      export_aliases: [ecs]
      ocsf:
        category: application_activity
        class: application_error
    parsers: [nginx.error.text]
    samples: [nginx.error.text.good]
parsers:
  - parser_id: nginx.access.combined
    display_name: NGINX combined access parser
    version: 1.0.0
    stages:
      - stage_id: parse_line
        type: regex
        on_error: keep_raw
        config:
          pattern: '^(?P<remote_addr>\S+)'
      - stage_id: map_ocsf
        type: ocsf_map
  - parser_id: nginx.error.text
    display_name: NGINX error parser
    version: 1.0.0
    stages:
      - stage_id: parse_error
        type: regex
        on_error: keep_raw
      - stage_id: map_ocsf
        type: ocsf_map
detections:
  - detection_id: nginx.web_scanner_burst
    title: NGINX web scanner burst
    kind: sigma
    path: detections/nginx-web-scanner-burst.yaml
    severity: medium
    tags: [attack.t1595]
samples:
  - case_id: nginx.access.combined.good
    source_id: nginx.access
    parser_id: nginx.access.combined
    input_path: samples/nginx-access-combined.log
    golden_path: samples/nginx-access-combined.ocsf.json
  - case_id: nginx.error.text.good
    source_id: nginx.error
    parser_id: nginx.error.text
    input_path: samples/nginx-error.log
    golden_path: samples/nginx-error.ocsf.json
`
