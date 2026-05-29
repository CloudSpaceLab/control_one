package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

func TestContentPackRegistrySnapshotJSONRoundTrip(t *testing.T) {
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := testContentPackManifest()
	now := time.Date(2026, 5, 27, 19, 0, 0, 0, time.UTC)
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	snapshot := registry.Snapshot(now)

	data, err := marshalContentPackRegistrySnapshot(snapshot)
	if err != nil {
		t.Fatalf("marshalContentPackRegistrySnapshot() error = %v", err)
	}
	decoded, err := decodeContentPackRegistrySnapshot(data)
	if err != nil {
		t.Fatalf("decodeContentPackRegistrySnapshot() error = %v", err)
	}
	restored, err := contentpacks.NewRegistryFromSnapshot(decoded, "")
	if err != nil {
		t.Fatalf("NewRegistryFromSnapshot() error = %v", err)
	}
	if _, ok := restored.ResolveSource("controlone.storage_test"); !ok {
		t.Fatal("ResolveSource() ok = false after storage snapshot round trip")
	}
}

func TestContentPackRegistrySnapshotRejectsInvalidLifecycleState(t *testing.T) {
	snapshot := contentpacks.RegistrySnapshot{
		SchemaVersion:     contentpacks.SchemaVersion,
		ControlOneVersion: "1.0.0",
		ExportedAt:        time.Date(2026, 5, 27, 19, 30, 0, 0, time.UTC),
		Packs: []contentpacks.PackRecord{{
			PackID:      "controlone.storage_test",
			PackVersion: "1.0.0",
			Status:      contentpacks.PackStatus("made_up"),
			Manifest:    testContentPackManifest(),
		}},
	}
	_, err := marshalContentPackRegistrySnapshot(snapshot)
	if err == nil || !strings.Contains(err.Error(), "unsupported snapshot status") {
		t.Fatalf("marshal error = %v, want unsupported snapshot status", err)
	}
}

func TestContentPackCollectorConfigCandidatePlanRoundTrip(t *testing.T) {
	plan := testContentPackCollectorConfigPlan()
	rendered, err := contentpacks.RenderOTelCollectorConfigYAML(plan)
	if err != nil {
		t.Fatalf("render plan: %v", err)
	}
	version := contentpacks.OTelCollectorConfigVersion(rendered)
	data, err := marshalContentPackCollectorConfigPlan(plan, version, string(rendered))
	if err != nil {
		t.Fatalf("marshalContentPackCollectorConfigPlan() error = %v", err)
	}
	decoded, err := decodeContentPackCollectorConfigPlan(data, version, string(rendered))
	if err != nil {
		t.Fatalf("decodeContentPackCollectorConfigPlan() error = %v", err)
	}
	if len(decoded.Sources) != 1 || decoded.Sources[0].SourceID != "controlone.storage_test" {
		t.Fatalf("decoded sources = %#v", decoded.Sources)
	}
}

func TestContentPackCollectorConfigCandidateRejectsDigestMismatch(t *testing.T) {
	plan := testContentPackCollectorConfigPlan()
	rendered, err := contentpacks.RenderOTelCollectorConfigYAML(plan)
	if err != nil {
		t.Fatalf("render plan: %v", err)
	}
	_, err = marshalContentPackCollectorConfigPlan(plan, "sha256:not-the-right-digest", string(rendered))
	if err == nil || !strings.Contains(err.Error(), "does not match rendered yaml digest") {
		t.Fatalf("marshal error = %v, want digest mismatch", err)
	}
}

func TestContentPackEdgeCollectorDefaultsAndStatusNormalization(t *testing.T) {
	kind, err := normalizeContentPackEdgeCollectorKind("")
	if err != nil {
		t.Fatalf("normalize empty kind: %v", err)
	}
	if kind != ContentPackEdgeCollectorKindOTel {
		t.Fatalf("kind = %q, want otel", kind)
	}
	status, err := normalizeContentPackEdgeCollectorHeartbeatStatus("", "")
	if err != nil {
		t.Fatalf("normalize empty healthy status: %v", err)
	}
	if status != ContentPackEdgeCollectorStatusHealthy {
		t.Fatalf("status = %q, want healthy", status)
	}
	status, err = normalizeContentPackEdgeCollectorHeartbeatStatus("", "exporter queue full")
	if err != nil {
		t.Fatalf("normalize empty degraded status: %v", err)
	}
	if status != ContentPackEdgeCollectorStatusDegraded {
		t.Fatalf("status = %q, want degraded", status)
	}
	if _, err := normalizeContentPackEdgeCollectorHeartbeatStatus("disabled", ""); err == nil {
		t.Fatal("normalize heartbeat status disabled error = nil, want rejection")
	}
}

func TestContentPackEdgeCollectorTokenSecretAndHash(t *testing.T) {
	token, err := newContentPackEdgeCollectorTokenSecret()
	if err != nil {
		t.Fatalf("newContentPackEdgeCollectorTokenSecret() error = %v", err)
	}
	if !strings.HasPrefix(token, ContentPackEdgeCollectorTokenPrefix) {
		t.Fatalf("token = %q, want prefix %q", token, ContentPackEdgeCollectorTokenPrefix)
	}
	hash := contentPackEdgeCollectorTokenHash(token)
	if strings.Contains(hash, token) {
		t.Fatal("token hash contains raw token")
	}
	if !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("hash = %q, want sha256 prefix", hash)
	}
	if got := contentPackEdgeCollectorTokenLastFour(token); got != token[len(token)-4:] {
		t.Fatalf("last four = %q, want %q", got, token[len(token)-4:])
	}
}

func TestContentPackSourceRuntimeStateNormalizesAndRoundTripsMetrics(t *testing.T) {
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	state, err := normalizeContentPackSourceRuntimeState(contentpacks.SourceRuntimeState{
		SourceInstanceID: " edge-1/linux.auth ",
		SourceID:         " linux.auth ",
		CollectorID:      " edge-1 ",
		CoverageState:    contentpacks.CoverageState(" parser_healthy "),
		LastParsedAt:     &now,
		Metrics: contentpacks.SourceRuntimeMetrics{
			EventsReceived: 12,
			EventsParsed:   12,
		},
		Labels: map[string]string{"receiver": "filelog/controlone.linux.auth"},
	})
	if err != nil {
		t.Fatalf("normalizeContentPackSourceRuntimeState() error = %v", err)
	}
	if state.SourceInstanceID != "edge-1/linux.auth" || state.SourceID != "linux.auth" || state.CollectorID != "edge-1" {
		t.Fatalf("normalized state = %#v", state)
	}
	if state.CoverageState != contentpacks.CoverageState(contentpacks.CoverageParserHealthy) || state.LastHealthAt == nil {
		t.Fatalf("coverage/health state = %#v", state)
	}
	data, err := marshalContentPackSourceRuntimeMetrics(state.Metrics)
	if err != nil {
		t.Fatalf("marshalContentPackSourceRuntimeMetrics() error = %v", err)
	}
	decoded := decodeContentPackSourceRuntimeMetrics(data)
	if decoded.EventsReceived != 12 || decoded.EventsParsed != 12 {
		t.Fatalf("decoded metrics = %#v", decoded)
	}
	labelsData, err := marshalContentPackStringMap(state.Labels)
	if err != nil {
		t.Fatalf("marshalContentPackStringMap() error = %v", err)
	}
	labels, err := decodeContentPackStringMap(labelsData)
	if err != nil {
		t.Fatalf("decodeContentPackStringMap() error = %v", err)
	}
	if labels["receiver"] != "filelog/controlone.linux.auth" {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestContentPackSourceProposalNormalizesBankSafeStatus(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()

	record, ok, err := normalizeContentPackSourceProposal(tenantID, nodeID, connectordiscovery.Proposal{
		ID:                  "LOCAL-LOG:Temenos-T24",
		Kind:                connectordiscovery.KindLocalLog,
		Program:             "Temenos-T24",
		CollectorType:       connectordiscovery.CollectorTypeFile,
		Formatter:           "generic",
		Confidence:          110,
		Risk:                "high",
		AutoConnectEligible: true,
		Paths:               []string{"/opt/temenos/logs/app.log", "/opt/temenos/logs/app.log"},
		Evidence:            []string{"package:temenos-tafj", "package:temenos-tafj"},
		Labels:              map[string]string{" Parser_Profile ": "temenos-t24"},
	})
	if err != nil {
		t.Fatalf("normalizeContentPackSourceProposal() error = %v", err)
	}
	if !ok {
		t.Fatal("proposal unexpectedly skipped")
	}
	if record.ProposalID != "local-log:temenos-t24" || record.Program != "temenos-t24" || record.SourceID != "temenos-t24" {
		t.Fatalf("proposal identity not normalized: %#v", record)
	}
	if record.Status != ContentPackSourceProposalStatusApprovalRequired || !record.RequiresApproval || record.AutoConnectEligible {
		t.Fatalf("high-risk proposal should be approval-gated: %#v", record)
	}
	if record.Confidence != 100 || len(record.Paths) != 1 || len(record.Evidence) != 1 {
		t.Fatalf("proposal bounds/dedupe failed: %#v", record)
	}
	if record.Labels["parser_profile"] != "temenos-t24" {
		t.Fatalf("labels = %#v", record.Labels)
	}
}

func testContentPackCollectorConfigPlan() contentpacks.OTelCollectorConfigPlan {
	return contentpacks.OTelCollectorConfigPlan{
		Sources: []contentpacks.OTelCollectorSourcePlan{{
			SourceID:            "controlone.storage_test",
			PackID:              "controlone.storage_test",
			PackVersion:         "1.0.0",
			Mode:                contentpacks.CollectorOTelFileLog,
			Receiver:            "filelog",
			ReceiverIDs:         []string{"filelog/controlone.controlone.storage_test"},
			PipelineID:          "logs/controlone.controlone.storage_test",
			PipelineType:        "logs",
			ResourceProcessorID: "resource/controlone.source.controlone.storage_test",
		}},
		Config: contentpacks.OTelCollectorConfig{
			Receivers: map[string]map[string]any{
				"filelog/controlone.controlone.storage_test": {
					"include": []string{"/var/log/controlone/storage-test.log"},
				},
			},
			Processors: map[string]any{
				"batch": map[string]any{"timeout": "1s"},
			},
			Exporters: map[string]map[string]any{
				"otlp/controlone": {"endpoint": "controlone.local:4317"},
			},
			Service: contentpacks.OTelServiceConfig{
				Pipelines: map[string]contentpacks.OTelPipelineConfig{
					"logs/controlone.controlone.storage_test": {
						Receivers:  []string{"filelog/controlone.controlone.storage_test"},
						Processors: []string{"batch"},
						Exporters:  []string{"otlp/controlone"},
					},
				},
			},
		},
	}
}

func testContentPackManifest() contentpacks.Manifest {
	return contentpacks.Manifest{
		SchemaVersion: contentpacks.SchemaVersion,
		PackID:        "controlone.storage_test",
		PackVersion:   "1.0.0",
		DisplayName:   "Storage Test Pack",
		License: contentpacks.LicenseMetadata{
			SPDX: "Apache-2.0",
		},
		Provenance: contentpacks.Provenance{
			Author: "Control One",
		},
		Sources: []contentpacks.SourceProfile{{
			SourceID:        "controlone.storage_test",
			DisplayName:     "Control One Storage Test",
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
			Parsers: []string{"controlone.storage_test.json"},
			Samples: []string{"controlone.storage_test.good"},
		}},
		Parsers: []contentpacks.ParserProfile{{
			ParserID:    "controlone.storage_test.json",
			DisplayName: "Control One Storage Test JSON",
			Version:     "1.0.0",
			Stages:      []contentpacks.ParserStage{{Type: contentpacks.StageJSON}},
		}},
		Samples: []contentpacks.SampleCase{{
			CaseID:     "controlone.storage_test.good",
			SourceID:   "controlone.storage_test",
			ParserID:   "controlone.storage_test.json",
			InputPath:  "samples/storage-test.jsonl",
			GoldenPath: "samples/storage-test.golden.jsonl",
		}},
	}
}
