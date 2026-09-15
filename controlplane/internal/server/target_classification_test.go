package server

import (
	"testing"
)

func TestClassifyBatteryAndDesktopOS(t *testing.T) {
	t.Parallel()

	labels := map[string]any{
		"target.battery_present": true,
	}
 targetType, confidence, evidence := ClassifyTarget(labels, "Windows 11", "amd64", "")
	if targetType != "personal_pc" {
		t.Errorf("targetType = %q, want %q", targetType, "personal_pc")
	}
	if confidence != 70 {
		t.Errorf("confidence = %d, want 70", confidence)
	}
	if len(evidence) == 0 {
		t.Error("expected non-empty evidence")
	}
}

func TestClassifyServerOS(t *testing.T) {
	t.Parallel()

	labels := map[string]any{}
	targetType, confidence, evidence := ClassifyTarget(labels, "Windows Server 2022", "amd64", "")
	if targetType != "server" {
		t.Errorf("targetType = %q, want %q", targetType, "server")
	}
	if confidence != 65 {
		t.Errorf("confidence = %d, want 65", confidence)
	}
	if len(evidence) == 0 {
		t.Error("expected non-empty evidence")
	}
	_ = evidence
}

func TestClassifyCloudMetadata(t *testing.T) {
	t.Parallel()

	labels := map[string]any{
		"target.cloud_provider": "aws",
	}
 targetType, confidence, evidence := ClassifyTarget(labels, "Ubuntu 22.04", "amd64", "")
	if targetType != "cloud_instance" {
		t.Errorf("targetType = %q, want %q", targetType, "cloud_instance")
	}
	if confidence != 60 {
		t.Errorf("confidence = %d, want 60", confidence)
	}
	if len(evidence) == 0 {
		t.Error("expected non-empty evidence")
	}
	_ = evidence
}

func TestClassifyDomainController(t *testing.T) {
	t.Parallel()

	labels := map[string]any{
		"target.domain_controller": true,
	}
 targetType, confidence, evidence := ClassifyTarget(labels, "Windows Server 2022", "amd64", "")
	if targetType != "domain_controller" {
		t.Errorf("targetType = %q, want %q", targetType, "domain_controller")
	}
	if confidence != 75 {
		t.Errorf("confidence = %d, want 75", confidence)
	}
	if len(evidence) == 0 {
		t.Error("expected non-empty evidence")
	}
	_ = evidence
}

func TestClassifyAgentHighConfidencePreserved(t *testing.T) {
	t.Parallel()

	labels := map[string]any{
		"target.type":                      "workstation",
		"target.classification_confidence": float64(85),
		"target.classification_evidence":   []any{"agent self-report"},
	}
 targetType, confidence, _ := ClassifyTarget(labels, "Windows Server 2022", "amd64", "")
	if targetType != "workstation" {
		t.Errorf("targetType = %q, want %q (agent classification should be preserved)", targetType, "workstation")
	}
	if confidence != 85 {
		t.Errorf("confidence = %d, want 85", confidence)
	}
}

func TestClassifyNoEvidenceUnknown(t *testing.T) {
	t.Parallel()

	labels := map[string]any{}
	targetType, confidence, evidence := ClassifyTarget(labels, "", "", "")
	if targetType != "unknown" {
		t.Errorf("targetType = %q, want %q", targetType, "unknown")
	}
	if confidence != 0 {
		t.Errorf("confidence = %d, want 0", confidence)
	}
	if evidence != nil {
		t.Errorf("evidence = %v, want nil", evidence)
	}
}

func TestClassifyLowAgentConfidenceOverridden(t *testing.T) {
	t.Parallel()

	labels := map[string]any{
		"target.type":                      "unknown",
		"target.classification_confidence": float64(20),
	}
 targetType, confidence, _ := ClassifyTarget(labels, "Windows Server 2022", "amd64", "")
	if targetType != "server" {
		t.Errorf("targetType = %q, want %q (low confidence agent should be overridden)", targetType, "server")
	}
	if confidence != 65 {
		t.Errorf("confidence = %d, want 65", confidence)
	}
}

func TestClassifyServiceKeywords(t *testing.T) {
	t.Parallel()

	labels := map[string]any{
		"service.role": "webserver,redis",
	}
 targetType, confidence, evidence := ClassifyTarget(labels, "Ubuntu 22.04", "amd64", "")
	if targetType != "server" {
		t.Errorf("targetType = %q, want %q", targetType, "server")
	}
	if confidence != 60 {
		t.Errorf("confidence = %d, want 60", confidence)
	}
	if len(evidence) == 0 {
		t.Error("expected non-empty evidence from service keywords")
	}
}

func TestClassifyDeviceBatteryLabel(t *testing.T) {
	t.Parallel()

	labels := map[string]any{
		"device.battery": "true",
	}
 targetType, confidence, _ := ClassifyTarget(labels, "macOS", "arm64", "")
	if targetType != "personal_pc" {
		t.Errorf("targetType = %q, want %q", targetType, "personal_pc")
	}
	if confidence != 70 {
		t.Errorf("confidence = %d, want 70", confidence)
	}
}
