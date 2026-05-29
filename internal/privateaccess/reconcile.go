package privateaccess

import (
	"fmt"
	"net/netip"
	"strings"
	"time"
)

const defaultMaxPeerStaleness = 15 * time.Minute

func ReconcileExposure(observations []ExposureObservation, snapshots []Snapshot, opts ReconcileOptions) []ExposureFinding {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	maxStaleness := opts.MaxPeerStaleness
	if maxStaleness <= 0 {
		maxStaleness = defaultMaxPeerStaleness
	}

	findings := make([]ExposureFinding, 0)
	for _, snapshot := range snapshots {
		findings = append(findings, reconcileProviderDrift(snapshot, now, maxStaleness)...)
	}
	for _, observation := range observations {
		coverage := findPrivateAccessCoverage(observation, snapshots)
		exposureEvidence := observationExposureEvidence(observation)
		postureEvidence := observationPostureEvidence(observation)
		switch {
		case observation.PubliclyReachable && coverage.covered:
			findings = append(findings, ExposureFinding{
				Type:      FindingPolicyDrift,
				Severity:  "high",
				Provider:  coverage.provider,
				NodeID:    observation.NodeID,
				ServiceID: observation.ServiceID,
				Detail:    fmt.Sprintf("%s is publicly reachable while also covered by private-access policy", observationLabel(observation)),
				Evidence:  appendEvidence(coverage.evidence, exposureEvidence...),
			})
		case observation.PubliclyReachable:
			findings = append(findings, ExposureFinding{
				Type:      FindingPubliclyExposed,
				Severity:  "high",
				NodeID:    observation.NodeID,
				ServiceID: observation.ServiceID,
				Detail:    fmt.Sprintf("%s is publicly reachable without private-access coverage", observationLabel(observation)),
				Evidence:  exposureEvidence,
			})
		case coverage.covered:
			findings = append(findings, ExposureFinding{
				Type:      FindingPrivateAccessOnly,
				Severity:  "info",
				Provider:  coverage.provider,
				NodeID:    observation.NodeID,
				ServiceID: observation.ServiceID,
				Detail:    fmt.Sprintf("%s is reachable through approved private-access policy", observationLabel(observation)),
				Evidence:  appendEvidence(coverage.evidence, postureEvidence...),
			})
		default:
			findings = append(findings, ExposureFinding{
				Type:      FindingUnmanaged,
				Severity:  "medium",
				NodeID:    observation.NodeID,
				ServiceID: observation.ServiceID,
				Detail:    fmt.Sprintf("%s has no matching private-access provider coverage", observationLabel(observation)),
				Evidence:  postureEvidence,
			})
		}
	}
	return findings
}

type coverageResult struct {
	covered  bool
	provider ProviderKind
	evidence []string
}

func findPrivateAccessCoverage(observation ExposureObservation, snapshots []Snapshot) coverageResult {
	for _, snapshot := range snapshots {
		if evidence := serviceCoverageEvidence(observation, snapshot); len(evidence) > 0 {
			return coverageResult{covered: true, provider: snapshot.Provider, evidence: evidence}
		}
		if evidence := routeCoverageEvidence(observation, snapshot); len(evidence) > 0 {
			return coverageResult{covered: true, provider: snapshot.Provider, evidence: evidence}
		}
	}
	return coverageResult{}
}

func serviceCoverageEvidence(observation ExposureObservation, snapshot Snapshot) []string {
	for _, service := range snapshot.Services {
		if !service.Enabled || !matchesServiceTarget(observation, service) || !policyAllowsService(snapshot, service.ID, observation) {
			continue
		}
		return []string{
			"provider:" + string(snapshot.Provider),
			"service:" + service.ID,
			"policy:service:" + service.ID,
		}
	}
	return nil
}

func routeCoverageEvidence(observation ExposureObservation, snapshot Snapshot) []string {
	address, ok := parseObservationAddress(observation.Address)
	if !ok {
		return nil
	}
	for _, route := range snapshot.Routes {
		if !route.Enabled || !cidrContains(route.CIDR, address) || !policyAllowsRoute(snapshot, route.ID, route.CIDR, observation) {
			continue
		}
		evidence := []string{
			"provider:" + string(snapshot.Provider),
			"route:" + route.ID,
			"cidr:" + route.CIDR,
		}
		if route.PeerID != "" {
			evidence = append(evidence, "peer:"+route.PeerID)
		}
		return evidence
	}
	return nil
}

func reconcileProviderDrift(snapshot Snapshot, now time.Time, maxStaleness time.Duration) []ExposureFinding {
	var findings []ExposureFinding
	for _, route := range snapshot.Routes {
		if route.Enabled && isBroadRoute(route.CIDR) {
			findings = append(findings, ExposureFinding{
				Type:     FindingPolicyDrift,
				Severity: "high",
				Provider: snapshot.Provider,
				Detail:   fmt.Sprintf("route %s advertises broad CIDR %s", firstNonEmpty(route.Name, route.ID), route.CIDR),
				Evidence: []string{"route:" + route.ID, "cidr:" + route.CIDR},
			})
		}
	}
	for _, peer := range snapshot.Peers {
		if peer.LastSeenAt.IsZero() || now.Sub(peer.LastSeenAt) <= maxStaleness || strings.EqualFold(peer.Status, "connected") {
			continue
		}
		findings = append(findings, ExposureFinding{
			Type:     FindingPolicyDrift,
			Severity: "medium",
			Provider: snapshot.Provider,
			NodeID:   peer.NodeID,
			Detail:   fmt.Sprintf("peer %s has stale provider status %q", firstNonEmpty(peer.Name, peer.ID), peer.Status),
			Evidence: []string{"peer:" + peer.ID, "last_seen:" + peer.LastSeenAt.UTC().Format(time.RFC3339)},
		})
	}
	return findings
}

func matchesServiceTarget(observation ExposureObservation, service Service) bool {
	if service.NodeID != "" && observation.NodeID != "" && service.NodeID != observation.NodeID {
		return false
	}
	if service.Host != "" && observation.Address != "" && !strings.EqualFold(service.Host, observation.Address) {
		return false
	}
	if service.Protocol != "" && observation.Protocol != "" && !strings.EqualFold(service.Protocol, observation.Protocol) {
		return false
	}
	if len(service.Ports) == 0 || observation.Port == 0 {
		return true
	}
	for _, port := range service.Ports {
		if port == observation.Port {
			return true
		}
	}
	return false
}

func policyAllowsService(snapshot Snapshot, serviceID string, observation ExposureObservation) bool {
	for _, policy := range snapshot.Policies {
		if !policy.Enabled || strings.EqualFold(policy.Action, "deny") {
			continue
		}
		for _, resource := range policy.Resources {
			if !resourceMatchesObservation(resource, observation) {
				continue
			}
			for _, candidate := range resource.ServiceIDs {
				if candidate == serviceID {
					return true
				}
			}
		}
	}
	return false
}

func policyAllowsRoute(snapshot Snapshot, routeID, cidr string, observation ExposureObservation) bool {
	for _, policy := range snapshot.Policies {
		if !policy.Enabled || strings.EqualFold(policy.Action, "deny") {
			continue
		}
		for _, resource := range policy.Resources {
			if !resourceMatchesObservation(resource, observation) {
				continue
			}
			for _, candidate := range resource.RouteIDs {
				if candidate == routeID {
					return true
				}
			}
			for _, candidate := range resource.CIDRs {
				if strings.TrimSpace(candidate) == strings.TrimSpace(cidr) {
					return true
				}
			}
		}
	}
	return false
}

func resourceMatchesObservation(resource PolicyResource, observation ExposureObservation) bool {
	if resource.Protocol != "" && observation.Protocol != "" && !strings.EqualFold(resource.Protocol, observation.Protocol) {
		return false
	}
	if len(resource.Ports) == 0 || observation.Port == 0 {
		return true
	}
	for _, port := range resource.Ports {
		if port == observation.Port {
			return true
		}
	}
	return false
}

func parseObservationAddress(value string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	return addr, err == nil
}

func cidrContains(cidr string, addr netip.Addr) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	return err == nil && prefix.Contains(addr)
}

func isBroadRoute(cidr string) bool {
	switch strings.TrimSpace(cidr) {
	case "0.0.0.0/0", "::/0":
		return true
	default:
		return false
	}
}

func observationLabel(observation ExposureObservation) string {
	name := firstNonEmpty(observation.Name, observation.ServiceID, observation.NodeID, observation.Address)
	endpoint := observation.Protocol
	if observation.Port > 0 {
		endpoint = fmt.Sprintf("%s/%d", firstNonEmpty(endpoint, "tcp"), observation.Port)
	}
	if endpoint == "" {
		return name
	}
	return name + " (" + endpoint + ")"
}

func observationExposureEvidence(observation ExposureObservation) []string {
	if observation.Metadata == nil {
		return nil
	}
	var evidence []string
	appendKeyedEvidence := func(prefix, key string) {
		if value := strings.TrimSpace(observation.Metadata[key]); value != "" {
			evidence = append(evidence, prefix+":"+value)
		}
	}
	appendKeyedEvidence("public_path", "public_path")
	appendKeyedEvidence("listen_addr", "listen_addr")
	appendKeyedEvidence("node_public_ip", "node_public_ip")
	appendKeyedEvidence("firewall", "firewall_posture")
	appendKeyedEvidence("firewall_rule", "firewall_evidence")
	return dedupeEvidence(evidence)
}

func observationPostureEvidence(observation ExposureObservation) []string {
	if observation.Metadata == nil {
		return nil
	}
	var evidence []string
	if value := strings.TrimSpace(observation.Metadata["node_public_ip"]); value != "" {
		evidence = append(evidence, "node_public_ip:"+value)
	}
	if value := strings.TrimSpace(observation.Metadata["firewall_posture"]); value != "" {
		evidence = append(evidence, "firewall:"+value)
	}
	if value := strings.TrimSpace(observation.Metadata["firewall_evidence"]); value != "" {
		evidence = append(evidence, "firewall_rule:"+value)
	}
	return dedupeEvidence(evidence)
}

func appendEvidence(base []string, extra ...string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make([]string, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return dedupeEvidence(out)
}

func dedupeEvidence(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
