package patchsupport

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ManagedProxyConfig is deliberately explicit about both sides of access:
// approved client source networks AND approved repository destinations.
func ManagedProxyConfig(hosts, clients []string) ([]byte, error) {
	if len(clients) == 0 || len(clients) > 64 {
		return nil, fmt.Errorf("specify 1 to 64 approved client CIDRs")
	}
	for _, host := range hosts {
		if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.ContainsAny(host, ":_") {
			return nil, fmt.Errorf("managed repository hosts must be exact DNS names")
		}
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return nil, fmt.Errorf("invalid repository hostname label")
			}
		}
	}
	body, err := SquidConfig(hosts)
	if err != nil {
		return nil, err
	}
	var sources []string
	seen := map[string]bool{}
	for _, raw := range clients {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || prefix != prefix.Masked() || prefix.Bits() == 0 || prefix.Addr().IsUnspecified() || prefix.Addr().IsMulticast() || prefix.Addr().IsLinkLocalUnicast() || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("approved clients must be canonical non-default unicast CIDRs")
		}
		if (prefix.Addr().Is4() && prefix.Bits() < 8) || (!prefix.Addr().Is4() && prefix.Bits() < 32) {
			return nil, fmt.Errorf("client CIDRs must be narrower than IPv4 /8 or IPv6 /32")
		}
		if seen[raw] {
			return nil, fmt.Errorf("duplicate approved client CIDR")
		}
		seen[raw] = true
		sources = append(sources, prefix.String())
	}
	// Leave the deny rules first and final deny-all last. Localhost is needed
	// for target-side readiness probes; it does not authorize remote clients.
	return []byte(strings.Replace(string(body), "acl local_clients src 127.0.0.0/8 ::1", "acl local_clients src 127.0.0.1/32 ::1/128 "+strings.Join(sources, " "), 1)), nil
}

func ProxyConfigSHA(hosts, clients []string) (string, error) {
	body, err := ManagedProxyConfig(hosts, clients)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(body)), nil
}

func ValidateProxyProbes(hosts, probes []string) error {
	if len(probes) == 0 || len(probes) > 16 {
		return fmt.Errorf("specify 1 to 16 HTTPS repository probe URLs")
	}
	allowed := map[string]bool{}
	for _, host := range hosts {
		allowed[strings.ToLower(host)] = true
	}
	for _, raw := range probes {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") || !allowed[strings.ToLower(u.Hostname())] || strings.ContainsAny(raw, "\r\n\t ") {
			return fmt.Errorf("probes require HTTPS on an explicitly approved repository host, without credentials or queries")
		}
	}
	return nil
}

type ProxyLifecycleProof struct {
	ObservedAt       time.Time `json:"observed_at"`
	ConfigSHA256     string    `json:"config_sha256"`
	ServiceActive    bool      `json:"service_active"`
	UpstreamVerified bool      `json:"upstream_verified"`
	Removed          bool      `json:"removed"`
}
