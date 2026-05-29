package privateaccess

import (
	"testing"
	"time"
)

func TestReconcileExposureClassifiesPrivateAccessOnly(t *testing.T) {
	obs := []ExposureObservation{{
		NodeID: "node-db-1", Address: "10.10.5.20", Protocol: "tcp", Port: 5432,
	}}
	snapshots := []Snapshot{{
		Provider: ProviderNetBird,
		Routes: []Route{{
			ID: "route-db", CIDR: "10.10.5.0/24", PeerID: "peer-router", Enabled: true,
		}},
		Policies: []Policy{{
			ID: "policy-db-admins", Enabled: true, Action: "allow",
			Resources: []PolicyResource{{
				RouteIDs: []string{"route-db"},
				Protocol: "tcp",
				Ports:    []int{5432},
			}},
		}},
	}}

	findings := ReconcileExposure(obs, snapshots, ReconcileOptions{Now: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want one", findings)
	}
	if findings[0].Type != FindingPrivateAccessOnly || findings[0].Provider != ProviderNetBird {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}

func TestReconcileExposureFlagsPublicBypass(t *testing.T) {
	obs := []ExposureObservation{{
		NodeID: "node-ssh-1", Address: "10.20.0.15", Protocol: "tcp", Port: 22, PubliclyReachable: true,
		Metadata: map[string]string{
			"public_path":      "node_public_ip_wildcard_listener",
			"listen_addr":      "0.0.0.0",
			"node_public_ip":   "203.0.113.10",
			"firewall_posture": "unknown",
		},
	}}
	snapshots := []Snapshot{{
		Provider: ProviderOpenZiti,
		Services: []Service{{
			ID: "ssh-admin", NodeID: "node-ssh-1", Protocol: "tcp", Ports: []int{22}, Enabled: true,
		}},
		Policies: []Policy{{
			ID: "policy-admins", Enabled: true, Action: "allow",
			Resources: []PolicyResource{{
				ServiceIDs: []string{"ssh-admin"},
				Protocol:   "tcp",
				Ports:      []int{22},
			}},
		}},
	}}

	findings := ReconcileExposure(obs, snapshots, ReconcileOptions{})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want one", findings)
	}
	if findings[0].Type != FindingPolicyDrift || findings[0].Severity != "high" {
		t.Fatalf("expected high policy drift, got %#v", findings[0])
	}
	if !hasEvidence(findings[0].Evidence, "node_public_ip:203.0.113.10") || !hasEvidence(findings[0].Evidence, "firewall:unknown") {
		t.Fatalf("expected public exposure evidence, got %#v", findings[0].Evidence)
	}
}

func TestReconcileExposureFlagsUnmanagedPrivateService(t *testing.T) {
	findings := ReconcileExposure([]ExposureObservation{{
		NodeID: "node-app-1", Address: "10.30.1.7", Protocol: "tcp", Port: 8443,
	}}, nil, ReconcileOptions{})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want one", findings)
	}
	if findings[0].Type != FindingUnmanaged || findings[0].Severity != "medium" {
		t.Fatalf("expected unmanaged private service, got %#v", findings[0])
	}
}

func TestReconcileExposureFlagsProviderPolicyDrift(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{
		Provider: ProviderHeadscale,
		Peers: []Peer{{
			ID: "peer-stale", Name: "router-a", Status: "disconnected", LastSeenAt: now.Add(-time.Hour),
		}},
		Routes: []Route{{
			ID: "route-all", Name: "all-networks", CIDR: "0.0.0.0/0", Enabled: true,
		}},
	}

	findings := ReconcileExposure(nil, []Snapshot{snapshot}, ReconcileOptions{Now: now, MaxPeerStaleness: 10 * time.Minute})
	byType := map[string]int{}
	for _, finding := range findings {
		byType[finding.Type]++
	}
	if byType[FindingPolicyDrift] != 2 {
		t.Fatalf("findings = %#v, want broad-route and stale-peer drift", findings)
	}
}

func TestReconcileExposureCarriesProtectedPostureEvidence(t *testing.T) {
	findings := ReconcileExposure([]ExposureObservation{{
		NodeID: "node-app-1", Address: "10.30.1.7", Protocol: "tcp", Port: 8443,
		Metadata: map[string]string{
			"firewall_posture": "default_deny",
			"node_public_ip":   "198.51.100.8",
		},
	}}, nil, ReconcileOptions{})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want one", findings)
	}
	if findings[0].Type != FindingUnmanaged {
		t.Fatalf("expected unmanaged service, got %#v", findings[0])
	}
	if !hasEvidence(findings[0].Evidence, "firewall:default_deny") || !hasEvidence(findings[0].Evidence, "node_public_ip:198.51.100.8") {
		t.Fatalf("expected posture evidence, got %#v", findings[0].Evidence)
	}
}

func hasEvidence(evidence []string, want string) bool {
	for _, item := range evidence {
		if item == want {
			return true
		}
	}
	return false
}
