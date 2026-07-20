package server

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestTargetOverrideRequestValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       nodeTargetOverrideRequest
		wantError bool
	}{
		{"valid server", nodeTargetOverrideRequest{TargetType: "server", OperatorOverride: true}, false},
		{"valid personal_pc", nodeTargetOverrideRequest{TargetType: "personal_pc"}, false},
		{"valid unknown", nodeTargetOverrideRequest{TargetType: "unknown"}, false},
		{"empty type", nodeTargetOverrideRequest{TargetType: ""}, true},
		{"invalid type", nodeTargetOverrideRequest{TargetType: "mainframe"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.req.validate()
			if (err != nil) != tt.wantError {
				t.Errorf("validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestAllowedTargetTypesContainsExpectedValues(t *testing.T) {
	t.Parallel()

	expected := []string{"personal_pc", "workstation", "laptop", "server", "vm", "cloud_instance", "domain_controller", "kiosk", "unknown"}
	for _, typ := range expected {
		if !allowedTargetTypes[typ] {
			t.Errorf("allowedTargetTypes missing %q", typ)
		}
	}
	if len(allowedTargetTypes) != len(expected) {
		t.Errorf("allowedTargetTypes has %d entries, expected %d", len(allowedTargetTypes), len(expected))
	}
}

func TestNodeResponseIncludesTargetMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	node := storage.Node{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Hostname: "web-server-01",
		OS:       sql.NullString{String: "linux", Valid: true},
		Arch:     sql.NullString{String: "amd64", Valid: true},
		State:    storage.NodeStateActive,
		Labels: map[string]any{
			"target.type":                      "server",
			"target.type_source":               "heuristic",
			"target.classification_confidence": float64(85),
			"target.classification_evidence":   []any{"server OS edition", "webserver listener"},
			"target.reachability_mode":         "direct_public",
			"target.install_context":           "fleet_enroll",
			"target.management_mode":           "fleet_managed",
			"target.machine_id":                "vm-abc-123",
			"target.network_observations": []any{
				map[string]any{
					"kind":          "private_ip",
					"value":         "10.0.0.25",
					"source":        "agent_interface",
					"first_seen_at": now.Format(time.RFC3339),
					"last_seen_at":  now.Format(time.RFC3339),
					"confidence":    float64(80),
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp := nodeResponseFromModel(node)

	if resp.TargetType != "server" {
		t.Errorf("target_type = %q, want %q", resp.TargetType, "server")
	}
	if resp.ReachabilityMode != "direct_public" {
		t.Errorf("reachability_mode = %q, want %q", resp.ReachabilityMode, "direct_public")
	}
	if resp.ManagementMode != "fleet_managed" {
		t.Errorf("management_mode = %q, want %q", resp.ManagementMode, "fleet_managed")
	}
	if resp.InstallContext != "fleet_enroll" {
		t.Errorf("install_context = %q, want %q", resp.InstallContext, "fleet_enroll")
	}
	if resp.MachineID != "vm-abc-123" {
		t.Errorf("machine_id = %q, want %q", resp.MachineID, "vm-abc-123")
	}
	if resp.Classification == nil {
		t.Fatal("classification is nil")
	}
	if resp.Classification.Source != "heuristic" {
		t.Errorf("classification.source = %q, want %q", resp.Classification.Source, "heuristic")
	}
	if resp.Classification.Confidence != 85 {
		t.Errorf("classification.confidence = %d, want %d", resp.Classification.Confidence, 85)
	}
	if len(resp.Classification.Evidence) != 2 {
		t.Errorf("classification.evidence len = %d, want 2", len(resp.Classification.Evidence))
	}

	// NetworkObservations: one from labels + one from public_ip (node has no public_ip)
	if len(resp.NetworkObservations) != 1 {
		t.Fatalf("network_observations len = %d, want 1", len(resp.NetworkObservations))
	}
	obs := resp.NetworkObservations[0]
	if obs.Kind != "private_ip" {
		t.Errorf("observation kind = %q, want %q", obs.Kind, "private_ip")
	}
	if obs.Value != "10.0.0.25" {
		t.Errorf("observation value = %q, want %q", obs.Value, "10.0.0.25")
	}
	if obs.Source != "agent_interface" {
		t.Errorf("observation source = %q, want %q", obs.Source, "agent_interface")
	}
	if obs.Confidence != 80 {
		t.Errorf("observation confidence = %d, want %d", obs.Confidence, 80)
	}
}

func TestNodeResponseIncludesPublicIPObservation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	node := storage.Node{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Hostname: "public-host",
		PublicIP: sql.NullString{String: "203.0.113.10", Valid: true},
		State:    storage.NodeStateActive,
		Labels: map[string]any{
			"target.type":              "workstation",
			"target.reachability_mode": "direct_public",
			"target.network_observations": []any{
				map[string]any{
					"kind":       "private_ip",
					"value":      "10.0.0.5",
					"source":     "agent_interface",
					"confidence": float64(90),
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp := nodeResponseFromModel(node)

	// Should have 2 observations: one from labels + one from PublicIP
	if len(resp.NetworkObservations) != 2 {
		t.Fatalf("network_observations len = %d, want 2", len(resp.NetworkObservations))
	}
	// First is from labels
	if resp.NetworkObservations[0].Value != "10.0.0.5" {
		t.Errorf("first observation value = %q, want %q", resp.NetworkObservations[0].Value, "10.0.0.5")
	}
	// Second is from PublicIP
	second := resp.NetworkObservations[1]
	if second.Kind != "public_ip" {
		t.Errorf("second observation kind = %q, want %q", second.Kind, "public_ip")
	}
	if second.Value != "203.0.113.10" {
		t.Errorf("second observation value = %q, want %q", second.Value, "203.0.113.10")
	}
	if second.Source != "node.public_ip" {
		t.Errorf("second observation source = %q, want %q", second.Source, "node.public_ip")
	}
}

func TestNodeResponseBackwardCompatibilityNoTargetLabels(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	node := storage.Node{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Hostname: "legacy-node",
		State:    storage.NodeStateActive,
		Labels:   map[string]any{"env": "prod"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp := nodeResponseFromModel(node)

	// Defaults from TargetMetadata when labels are absent
	if resp.TargetType != "unknown" {
		t.Errorf("target_type = %q, want %q", resp.TargetType, "unknown")
	}
	if resp.ReachabilityMode != "unknown" {
		t.Errorf("reachability_mode = %q, want %q", resp.ReachabilityMode, "unknown")
	}
	if resp.ManagementMode != "agent_managed" {
		t.Errorf("management_mode = %q, want %q", resp.ManagementMode, "agent_managed")
	}
	if resp.InstallContext != "" {
		t.Errorf("install_context = %q, want empty", resp.InstallContext)
	}
	if resp.MachineID != "" {
		t.Errorf("machine_id = %q, want empty", resp.MachineID)
	}
	if resp.Classification == nil {
		t.Fatal("classification is nil")
	}
	if resp.Classification.Source != "default" {
		t.Errorf("classification.source = %q, want %q", resp.Classification.Source, "default")
	}
	if resp.Classification.Confidence != 0 {
		t.Errorf("classification.confidence = %d, want 0", resp.Classification.Confidence)
	}
	if len(resp.NetworkObservations) != 0 {
		t.Errorf("network_observations len = %d, want 0", len(resp.NetworkObservations))
	}
}

func TestNodeResponseMachineIDFromLegacyLabel(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	node := storage.Node{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Hostname: "old-node",
		State:    storage.NodeStateActive,
		Labels: map[string]any{
			"machine_id": "legacy-machine-id",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp := nodeResponseFromModel(node)

	if resp.MachineID != "legacy-machine-id" {
		t.Errorf("machine_id = %q, want %q", resp.MachineID, "legacy-machine-id")
	}
}
