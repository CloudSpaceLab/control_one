package contentpacks

import (
	"context"
	"testing"
)

func TestAWSCloudAuditPackValidatesAndReplays(t *testing.T) {
	pack, err := AWSCloudAuditPack()
	if err != nil {
		t.Fatalf("AWSCloudAuditPack() error = %v", err)
	}
	if pack.Manifest.PackID != "controlone.aws_cloud_audit" {
		t.Fatalf("pack id = %q", pack.Manifest.PackID)
	}
	if len(pack.Manifest.Sources) != 4 {
		t.Fatalf("sources = %d, want 4", len(pack.Manifest.Sources))
	}
	if len(pack.Manifest.Parsers) != 4 {
		t.Fatalf("parsers = %d, want 4", len(pack.Manifest.Parsers))
	}
	if len(pack.Manifest.Detections) < 5 {
		t.Fatalf("detections = %d, want at least 5", len(pack.Manifest.Detections))
	}
	if err := Validate(pack.Manifest); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	report, err := ReplayManifestSamples(context.Background(), pack.Manifest, pack.Root, SampleReplayOptions{})
	if err != nil {
		t.Fatalf("ReplayManifestSamples() error = %v", err)
	}
	if !report.Passed() || report.TotalCases != 4 || report.TotalEvents != 4 {
		t.Fatalf("sample report = %#v", report)
	}
	detectionReport, err := ReplayManifestDetections(context.Background(), pack.Manifest, pack.Root, DetectionReplayOptions{
		DetectionLoadOptions: DetectionLoadOptions{SigmaFieldMap: DefaultSigmaFieldMap()},
	})
	if err != nil {
		t.Fatalf("ReplayManifestDetections() error = %v", err)
	}
	if !detectionReport.Passed() {
		t.Fatalf("detection report = %#v", detectionReport)
	}
	if detectionReport.TotalRules != len(pack.Manifest.Detections) {
		t.Fatalf("rules = %d, want %d", detectionReport.TotalRules, len(pack.Manifest.Detections))
	}
}

func TestAWSCloudAuditParsersNormalizeExpectedFields(t *testing.T) {
	samples := awsCloudSamples()
	want := map[string]map[string]any{
		"aws.cloudtrail.root.sample": {
			"event.action":     "ConsoleLogin",
			"event.provider":   "signin.amazonaws.com",
			"user.name":        "arn:aws:iam::123456789012:root",
			"source.ip":        "203.0.113.10",
			"event.dataset":    "aws.cloudtrail",
			"cloud.provider":   "aws",
			"aws.mfa_used":     "No",
		},
		"aws.cloudtrail.insight.sample": {
			"event.action":      "RunInstances",
			"event.provider":    "ec2.amazonaws.com",
			"event.dataset":     "aws.cloudtrail.insights",
			"aws.insight.type":  "ApiCallRateInsight",
			"aws.insight.state": "Start",
		},
		"aws.vpc_flow.reject.sample": {
			"source.ip":      "198.51.100.5",
			"destination.ip": "10.0.1.10",
			"event.action":   "REJECT",
			"event.dataset":  "aws.vpc_flow",
		},
		"aws.guardduty.finding.sample": {
			"event.action":     "UnauthorizedAccess:EC2/SSHBruteForce",
			"event.dataset":    "aws.guardduty",
			"source.ip":        "198.51.100.5",
			"destination.port": "22",
		},
	}
	for _, sample := range samples {
		var profile ParserProfile
		for _, candidate := range awsCloudParserProfiles() {
			if candidate.ParserID == sample.ParserID {
				profile = candidate
				break
			}
		}
		compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
		if err != nil {
			t.Fatalf("compile %s: %v", sample.ParserID, err)
		}
		out, err := compiled.Parse(ParserInput{Raw: sample.Raw})
		if err != nil {
			t.Fatalf("parse %s: %v", sample.CaseID, err)
		}
		for field, expected := range want[sample.CaseID] {
			got, ok := getField(out.Event.Fields, field)
			if !ok {
				t.Fatalf("%s missing field %s", sample.CaseID, field)
			}
			if field == "destination.port" {
				if got != expected && got != float64(22) {
					t.Fatalf("%s %s = %#v, want %#v", sample.CaseID, field, got, expected)
				}
				continue
			}
			if got != expected {
				t.Fatalf("%s %s = %#v, want %#v", sample.CaseID, field, got, expected)
			}
		}
	}
}

func TestAWSCloudAuditPackIncludesRequiredSourceCoverage(t *testing.T) {
	pack, err := AWSCloudAuditPack()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]SourceProfile{}
	for _, source := range pack.Manifest.Sources {
		seen[source.SourceID] = source
	}
	for _, sourceID := range []string{"aws.cloudtrail", "aws.cloudtrail_insights", "aws.vpc_flow", "aws.guardduty"} {
		source, ok := seen[sourceID]
		if !ok {
			t.Fatalf("missing source %s", sourceID)
		}
		if len(source.Parsers) != 1 || len(source.Samples) == 0 || len(source.Detections) == 0 {
			t.Fatalf("source %s incomplete: %#v", sourceID, source)
		}
		if !source.ApprovalRequired {
			t.Fatalf("source %s must require approval", sourceID)
		}
	}
}
