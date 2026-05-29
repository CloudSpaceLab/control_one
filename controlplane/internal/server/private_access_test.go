package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

func TestPrivateAccessObservationsUseNodePublicIPAndDefaultDeny(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	serviceID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	observations := privateAccessObservationsFromNodeServicesWithContext([]storage.NodeService{{
		ID:         serviceID,
		NodeID:     nodeID,
		TenantID:   tenantID,
		Process:    "nginx",
		ListenAddr: "0.0.0.0",
		Port:       443,
	}}, privateAccessExposureContext{
		Nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
			PublicIP: sql.NullString{String: "203.0.113.10", Valid: true},
		}},
		Firewalls: map[uuid.UUID]*storage.NodeFirewallState{
			nodeID: {
				NodeID:       nodeID,
				FirewallType: "ufw",
				Enabled:      true,
				Rules:        []storage.FirewallRule{{Raw: "Default: deny (incoming), allow (outgoing), deny (routed)"}},
				ObservedAt:   now,
			},
		},
		NodeContextKnown: true,
		Now:              now,
	})

	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	if observations[0].PubliclyReachable {
		t.Fatalf("expected default-deny firewall to block public reachability, got %#v", observations[0])
	}
	if observations[0].Metadata["public_path"] != "node_public_ip_wildcard_listener" {
		t.Fatalf("expected public IP listener evidence, got %#v", observations[0].Metadata)
	}
	if observations[0].Metadata["firewall_posture"] != "default_deny" {
		t.Fatalf("expected default-deny firewall posture, got %#v", observations[0].Metadata)
	}
}

func TestPrivateAccessObservationsTreatDefaultDenyPublicAllowAsReachable(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	observations := privateAccessObservationsFromNodeServicesWithContext([]storage.NodeService{{
		ID:         uuid.New(),
		NodeID:     nodeID,
		TenantID:   tenantID,
		Process:    "sshd",
		ListenAddr: "0.0.0.0",
		Port:       22,
	}}, privateAccessExposureContext{
		Nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
			PublicIP: sql.NullString{String: "198.51.100.8", Valid: true},
		}},
		Firewalls: map[uuid.UUID]*storage.NodeFirewallState{
			nodeID: {
				NodeID:       nodeID,
				FirewallType: "ufw",
				Enabled:      true,
				Rules: []storage.FirewallRule{
					{Raw: "Default: deny (incoming), allow (outgoing), deny (routed)"},
					{Action: "allow", Direction: "in", Protocol: "tcp", Port: "22", Source: "0.0.0.0/0", Comment: "ssh break-glass"},
				},
				ObservedAt: now,
			},
		},
		NodeContextKnown: true,
		Now:              now,
	})

	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	if !observations[0].PubliclyReachable {
		t.Fatalf("expected public allow rule to keep service reachable, got %#v", observations[0])
	}
	if observations[0].Metadata["firewall_posture"] != "default_deny_public_allow" {
		t.Fatalf("expected public allow firewall posture, got %#v", observations[0].Metadata)
	}
	if observations[0].Metadata["firewall_evidence"] != "ssh break-glass" {
		t.Fatalf("expected compact firewall rule evidence, got %#v", observations[0].Metadata)
	}
}

func TestPrivateAccessObservationsDoNotInferPublicWithoutNodePublicIP(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()

	observations := privateAccessObservationsFromNodeServicesWithContext([]storage.NodeService{{
		ID:         uuid.New(),
		NodeID:     nodeID,
		TenantID:   tenantID,
		Process:    "postgres",
		ListenAddr: "0.0.0.0",
		Port:       5432,
	}}, privateAccessExposureContext{
		Nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
		}},
		NodeContextKnown: true,
		Now:              time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
	})

	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	if observations[0].PubliclyReachable {
		t.Fatalf("expected no public reachability without node public IP, got %#v", observations[0])
	}
	if _, ok := observations[0].Metadata["public_path"]; ok {
		t.Fatalf("did not expect public path evidence, got %#v", observations[0].Metadata)
	}
}

func TestPrivateAccessObservationsUseLoadBalancerExposureLabels(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()

	observations := privateAccessObservationsFromNodeServicesWithContext([]storage.NodeService{{
		ID:         uuid.New(),
		NodeID:     nodeID,
		TenantID:   tenantID,
		Process:    "admin-api",
		ListenAddr: "10.10.2.15",
		Port:       8443,
	}}, privateAccessExposureContext{
		Nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
			Labels: map[string]any{
				"load_balancer.public":       true,
				"load_balancer.public_ports": []any{8443},
				"load_balancer.public_dns":   "admin-api.example.bank",
			},
		}},
		LoadBalancers: map[uuid.UUID][]storage.ClusterLBRegistration{
			nodeID: {{
				NodeID:       nodeID,
				Provider:     "aws",
				LBIdentifier: "arn:aws:elasticloadbalancing:af-south-1:123456789012:loadbalancer/app/admin-api/abc",
				RegisteredAt: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
			}},
		},
		NodeContextKnown: true,
		Now:              time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
	})

	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	if !observations[0].PubliclyReachable {
		t.Fatalf("expected public load balancer label to mark service reachable, got %#v", observations[0])
	}
	if observations[0].Metadata["public_path"] != "public_load_balancer" {
		t.Fatalf("public_path = %q metadata=%#v", observations[0].Metadata["public_path"], observations[0].Metadata)
	}
	if observations[0].Metadata["load_balancer_public_dns"] != "admin-api.example.bank" || observations[0].Metadata["load_balancer_evidence"] == "" {
		t.Fatalf("expected load balancer evidence, got %#v", observations[0].Metadata)
	}
}

func TestPrivateAccessExposureFindingCreatesSOCCase(t *testing.T) {
	srv, store := dashboardAdminHarness(t, "operator", "operator-token")
	tenantID := store.tenants[0].ID
	findingID := uuid.New()
	nodeID := uuid.New()
	serviceID := uuid.New()
	store.privateAccessFindings = []storage.PrivateAccessExposureFindingRecord{{
		ID:         findingID,
		TenantID:   tenantID,
		Provider:   privateaccess.ProviderNetBird,
		Type:       privateaccess.FindingPubliclyExposed,
		Severity:   "high",
		NodeID:     &nodeID,
		ServiceID:  &serviceID,
		Detail:     "admin-api is publicly reachable without approved private-access policy",
		Evidence:   []string{"public_path=public_load_balancer", "load_balancer_evidence=aws:arn"},
		ObservedAt: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
	}}

	rec := dashboardCall(t, srv, "operator-token", http.MethodPost, "/api/v1/private-access/exposure/findings/"+findingID.String()+"/soc-case?tenant_id="+tenantID.String())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201", rec.Code, rec.Body.String())
	}
	var resp struct {
		Case socCaseResponse `json:"case"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Case.TriggerType != "private_access_exposure" || resp.Case.TriggerEventType != "private_access_exposure."+privateaccess.FindingPubliclyExposed {
		t.Fatalf("unexpected case trigger: %+v", resp.Case)
	}
	hasExposureEvidence := false
	for _, ref := range resp.Case.EvidenceRefs {
		if ref.Kind == "private_access_exposure_finding" && strings.Contains(ref.ID, findingID.String()) {
			hasExposureEvidence = true
			break
		}
	}
	if !hasExposureEvidence {
		t.Fatalf("expected private-access evidence ref, got %+v", resp.Case.EvidenceRefs)
	}
	if len(store.aiInvestigations) != 1 || store.aiInvestigations[0].NodeID != nodeID {
		t.Fatalf("case not persisted against node: %+v", store.aiInvestigations)
	}
}
