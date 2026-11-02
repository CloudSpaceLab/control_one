package main

import (
	"net"
	"os/exec"
	"runtime"
	"sort"

	"github.com/CloudSpaceLab/control_one/internal/offlineapt"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

func detectedPatchRuntime() patchsupport.Runtime {
	r := patchsupport.Runtime{OS: runtime.GOOS, CalendarWindowStartCheck: true}
	if offlineAPTPlatform() {
		r.OnlinePlanVersion = 1
	}
	if managedProxyAvailable() {
		r.ProxyLifecycleVersion = 2
	}
	if patchOfflineRoot != "" && offlineAPTPlatform() && offlineapt.CheckExecutionIsolation() == nil {
		r.OfflineAPTVersion = 1
		r.OfflinePrepareVersion = 1
		r.OfflinePlanFormat = 3
	}
	if runtime.GOOS == "linux" {
		for _, entry := range []struct{ binary, manager string }{{"apt-get", "apt"}, {"dnf", "dnf"}, {"yum", "yum"}} {
			if _, err := exec.LookPath(entry.binary); err == nil {
				r.Manager = entry.manager
				break
			}
		}
	} else if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("winget"); err == nil {
			r.Manager = "winget"
		}
	}
	return r
}

type agentNetworkObservation struct {
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Source     string `json:"source"`
	Confidence int    `json:"confidence"`
}

func heartbeatInterfaceObservations() []agentNetworkObservation {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var addresses []net.IP
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		rows, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, row := range rows {
			ip, _, err := net.ParseCIDR(row.String())
			if err == nil {
				addresses = append(addresses, ip)
			}
		}
	}
	return interfaceObservations(addresses)
}

func interfaceObservations(addresses []net.IP) []agentNetworkObservation {
	seen := map[string]bool{}
	var out []agentNetworkObservation
	for _, ip := range addresses {
		if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		value := ip.String()
		if seen[value] {
			continue
		}
		seen[value] = true
		kind := "public_ip"
		if ip.IsPrivate() {
			kind = "private_ip"
		}
		out = append(out, agentNetworkObservation{Kind: kind, Value: value, Source: "agent_interface_snapshot", Confidence: 80})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}
