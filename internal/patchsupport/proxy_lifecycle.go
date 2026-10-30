package patchsupport

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ProxyLifecycle pins one administrator request to a target and deadline.
// It is not authorization to create jobs through the generic job endpoint.
type ProxyLifecycle struct {
	Revision     int64     `json:"revision,omitempty"`
	ClientCIDRs  []string  `json:"client_cidrs,omitempty"`
	ProbeURLs    []string  `json:"probe_urls,omitempty"`
	SquidVersion string    `json:"squid_version,omitempty"`
	ProxyID      string    `json:"proxy_id"`
	NodeID       string    `json:"node_id"`
	Whitelist    []string  `json:"whitelist,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	RequestedBy  string    `json:"requested_by"`
}

func IsProxyLifecycle(action string) bool {
	return action == "squid.install" || action == "squid.reconfigure" || action == "squid.remove"
}

func (p ProxyLifecycle) Validate(action string, now time.Time) error {
	if !IsProxyLifecycle(action) {
		return fmt.Errorf("unsupported proxy lifecycle action")
	}
	for _, id := range []string{p.ProxyID, p.NodeID} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil {
			return fmt.Errorf("proxy and node UUIDs required")
		}
	}
	if p.RequestedBy == "" || len(p.RequestedBy) > 256 {
		return fmt.Errorf("proxy request authority required")
	}
	if !p.ExpiresAt.After(now) || p.ExpiresAt.After(now.Add(15*time.Minute)) {
		return fmt.Errorf("proxy request expired or deadline exceeds fifteen minutes")
	}
	if action != "squid.remove" {
		if _, err := SquidConfig(p.Whitelist); err != nil {
			return err
		}
		if p.Revision > 0 {
			if _, err := ManagedProxyConfig(p.Whitelist, p.ClientCIDRs); err != nil {
				return err
			}
			if err := ValidateProxyProbes(p.Whitelist, p.ProbeURLs); err != nil {
				return err
			}
		}
	} else if len(p.Whitelist) != 0 {
		return fmt.Errorf("removal cannot change repository authorization")
	}
	if p.Revision < 0 {
		return fmt.Errorf("invalid proxy policy revision")
	}
	if p.Revision > 0 && action == "squid.install" {
		if p.SquidVersion == "" || len(p.SquidVersion) > 128 {
			return fmt.Errorf("reviewed exact Squid package version required")
		}
		for _, r := range p.SquidVersion {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '+' || r == ':' || r == '~' || r == '-' || r == '_') {
				return fmt.Errorf("invalid Squid version")
			}
		}
		if p.SquidVersion[0] == '-' {
			return fmt.Errorf("invalid Squid version")
		}
	}
	return nil
}
