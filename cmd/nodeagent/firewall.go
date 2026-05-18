package main

import (
	"os/exec"
	"runtime"
	"strings"
)

// FirewallState is the snapshot the agent reports each heartbeat. Always sent
// in full (the payload is small) so the server can flip rules in real time.
type FirewallState struct {
	Type    string         `json:"type"` // ufw | firewalld | iptables | nftables | windows_defender_firewall | none
	Enabled bool           `json:"enabled"`
	Rules   []FirewallRule `json:"rules,omitempty"`
	Zones   []FirewallZone `json:"zones,omitempty"`
}

// FirewallRule mirrors the storage shape — kept in this package so the agent
// has no Go-side dependency on the controlplane storage package.
type FirewallRule struct {
	Action    string `json:"action,omitempty"`
	Direction string `json:"direction,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Port      string `json:"port,omitempty"`
	Source    string `json:"source,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Raw       string `json:"raw,omitempty"`
}

// FirewallZone mirrors the firewalld zone snapshot.
type FirewallZone struct {
	Name       string   `json:"name"`
	Default    bool     `json:"default,omitempty"`
	Interfaces []string `json:"interfaces,omitempty"`
	Sources    []string `json:"sources,omitempty"`
	Services   []string `json:"services,omitempty"`
}

// collectFirewall returns the most-specific firewall snapshot we can
// produce. Order of detection on Linux: ufw → firewalld → nftables → iptables.
// On Windows we shell out to netsh. Errors degrade to type="none".
func collectFirewall() FirewallState {
	switch runtime.GOOS {
	case "linux":
		return collectLinuxFirewall()
	case "windows":
		return collectWindowsFirewall()
	default:
		return FirewallState{Type: "none"}
	}
}

func collectLinuxFirewall() FirewallState {
	if _, err := exec.LookPath("ufw"); err == nil {
		if st, ok := tryUFW(); ok {
			return st
		}
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		if st, ok := tryFirewalld(); ok {
			return st
		}
	}
	if _, err := exec.LookPath("nft"); err == nil {
		if st, ok := tryNftables(); ok {
			return st
		}
	}
	if _, err := exec.LookPath("iptables"); err == nil {
		if st, ok := tryIptables(); ok {
			return st
		}
	}
	return FirewallState{Type: "none"}
}

func tryUFW() (FirewallState, bool) {
	out, err := exec.Command("ufw", "status", "verbose").Output()
	if err != nil {
		return FirewallState{}, false
	}
	st := FirewallState{Type: "ufw"}
	text := string(out)
	if strings.Contains(text, "Status: active") {
		st.Enabled = true
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Status:") || strings.HasPrefix(line, "Logging:") {
			continue
		}
		if strings.HasPrefix(line, "New profiles:") {
			continue
		}
		if strings.HasPrefix(line, "To") || strings.HasPrefix(line, "--") {
			continue
		}
		st.Rules = append(st.Rules, FirewallRule{Raw: line})
	}
	return st, true
}

func tryFirewalld() (FirewallState, bool) {
	stateOut, err := exec.Command("firewall-cmd", "--state").Output()
	if err != nil {
		return FirewallState{Type: "firewalld"}, true
	}
	st := FirewallState{Type: "firewalld", Enabled: strings.TrimSpace(string(stateOut)) == "running"}

	if zonesOut, err := exec.Command("firewall-cmd", "--get-active-zones").Output(); err == nil {
		st.Zones = parseFirewalldZones(string(zonesOut))
		// Mark default zone if present.
		if defOut, err := exec.Command("firewall-cmd", "--get-default-zone").Output(); err == nil {
			def := strings.TrimSpace(string(defOut))
			for i := range st.Zones {
				if st.Zones[i].Name == def {
					st.Zones[i].Default = true
				}
			}
		}
	}
	return st, true
}

// parseFirewalldZones reads `firewall-cmd --get-active-zones` output, which
// alternates zone names with their interface/source tags on the next lines.
func parseFirewalldZones(out string) []FirewallZone {
	lines := strings.Split(out, "\n")
	var zones []FirewallZone
	var cur *FirewallZone
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r ")
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			if cur != nil {
				zones = append(zones, *cur)
			}
			z := FirewallZone{Name: strings.TrimSpace(line)}
			cur = &z
			continue
		}
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "interfaces:"):
			cur.Interfaces = strings.Fields(strings.TrimPrefix(trim, "interfaces:"))
		case strings.HasPrefix(trim, "sources:"):
			cur.Sources = strings.Fields(strings.TrimPrefix(trim, "sources:"))
		}
	}
	if cur != nil {
		zones = append(zones, *cur)
	}
	return zones
}

func tryNftables() (FirewallState, bool) {
	out, err := exec.Command("nft", "list", "ruleset").Output()
	if err != nil {
		return FirewallState{}, false
	}
	text := strings.TrimSpace(string(out))
	st := FirewallState{Type: "nftables", Enabled: text != ""}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "table ") || strings.HasPrefix(line, "chain ") || line == "}" || line == "{" {
			continue
		}
		st.Rules = append(st.Rules, FirewallRule{Raw: line})
	}
	return st, true
}

func tryIptables() (FirewallState, bool) {
	out, err := exec.Command("iptables", "-S").Output()
	if err != nil {
		return FirewallState{}, false
	}
	st := FirewallState{Type: "iptables", Enabled: true}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		st.Rules = append(st.Rules, FirewallRule{Raw: line})
	}
	return st, true
}

func collectWindowsFirewall() FirewallState {
	out, err := exec.Command("netsh", "advfirewall", "show", "currentprofile").Output()
	if err != nil {
		return FirewallState{Type: "none"}
	}
	st := FirewallState{Type: "windows_defender_firewall"}
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if strings.HasPrefix(line, "State") {
			st.Enabled = strings.Contains(strings.ToUpper(line), "ON")
		}
	}
	return st
}
