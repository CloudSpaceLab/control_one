package agentruntime

import (
	"runtime"
	"strings"
	"time"
)

const (
	ProfileAuto        = "auto"
	ProfileLightweight = "lightweight"
	ProfileBalanced    = "balanced"
	ProfileForensic    = "forensic"
)

// Settings captures the runtime posture the node agent should use on this OS.
// It is intentionally conservative: expensive forensic collectors are opt-in.
type Settings struct {
	RequestedProfile string
	ActiveProfile    string
	GOOS             string

	ProcmonEnabled  bool
	ProcmonInterval time.Duration
	ProcmonTopK     int

	NetflowEnabled      bool
	NetflowPollInterval time.Duration

	FileAccessDefaultEnabled bool
	DBQueryDefaultEnabled    bool
	LogCollectionDefault     bool
	LegacyTelemetryHeartbeat bool

	ServicesInterval  time.Duration
	InventoryRefresh  time.Duration
	FirewallRefresh   time.Duration
	NetflowSummaryAge time.Duration
	NetflowDrainEvery time.Duration
	NetflowMaxBuckets int

	Capabilities []string
}

// Resolve returns OS-aware defaults for a requested runtime profile.
func Resolve(requested string) Settings {
	return ResolveForOS(requested, runtime.GOOS)
}

// ResolveForOS is split out for tests and cross-platform planning checks.
func ResolveForOS(requested, goos string) Settings {
	req := normalizeProfile(requested)
	active := req
	if active == ProfileAuto {
		switch goos {
		case "linux", "windows":
			active = ProfileBalanced
		case "darwin", "aix":
			active = ProfileLightweight
		default:
			active = ProfileLightweight
		}
	}

	s := baseSettings(req, active, goos)
	switch active {
	case ProfileForensic:
		applyForensic(&s)
	case ProfileBalanced:
		applyBalanced(&s)
	default:
		applyLightweight(&s)
	}
	applyOSOverrides(&s)
	s.Capabilities = capabilitiesFor(s)
	return s
}

func normalizeProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case ProfileLightweight:
		return ProfileLightweight
	case ProfileBalanced:
		return ProfileBalanced
	case ProfileForensic:
		return ProfileForensic
	default:
		return ProfileAuto
	}
}

func baseSettings(requested, active, goos string) Settings {
	return Settings{
		RequestedProfile: requested,
		ActiveProfile:    active,
		GOOS:             goos,

		ProcmonTopK:      10,
		ServicesInterval: 30 * time.Minute,
		InventoryRefresh: 12 * time.Hour,
		FirewallRefresh:  10 * time.Minute,

		NetflowSummaryAge: 90 * time.Second,
		NetflowDrainEvery: 30 * time.Second,
		NetflowMaxBuckets: 4096,
	}
}

func applyLightweight(s *Settings) {
	s.ProcmonEnabled = false
	s.NetflowEnabled = false
	s.FileAccessDefaultEnabled = false
	s.DBQueryDefaultEnabled = false
	s.LogCollectionDefault = false
	s.LegacyTelemetryHeartbeat = false
	s.ProcmonInterval = 2 * time.Minute
	s.NetflowPollInterval = time.Minute
}

func applyBalanced(s *Settings) {
	s.ProcmonEnabled = true
	s.NetflowEnabled = true
	s.FileAccessDefaultEnabled = false
	s.DBQueryDefaultEnabled = false
	s.LogCollectionDefault = false
	s.LegacyTelemetryHeartbeat = false
	s.ProcmonInterval = time.Minute
	s.NetflowPollInterval = 15 * time.Second
	s.ServicesInterval = 15 * time.Minute
	s.InventoryRefresh = 6 * time.Hour
	s.FirewallRefresh = 5 * time.Minute
}

func applyForensic(s *Settings) {
	s.ProcmonEnabled = true
	s.NetflowEnabled = true
	s.FileAccessDefaultEnabled = true
	s.DBQueryDefaultEnabled = true
	s.LogCollectionDefault = false
	s.LegacyTelemetryHeartbeat = false
	s.ProcmonInterval = 30 * time.Second
	s.ProcmonTopK = 20
	s.NetflowPollInterval = 5 * time.Second
	s.ServicesInterval = 5 * time.Minute
	s.InventoryRefresh = time.Hour
	s.FirewallRefresh = time.Minute
	s.NetflowSummaryAge = time.Minute
}

func applyOSOverrides(s *Settings) {
	switch s.GOOS {
	case "linux":
		// Linux has cheap native procfs signals and remains the richest target.
	case "windows":
		// Windows process snapshots are relatively expensive. Balanced mode leans
		// on native netflow and host metrics; procmon is forensic-only.
		if s.ActiveProfile == ProfileBalanced {
			s.ProcmonEnabled = false
			s.NetflowPollInterval = 30 * time.Second
			s.InventoryRefresh = 12 * time.Hour
			s.FirewallRefresh = 10 * time.Minute
			s.ServicesInterval = 0
		}
	case "darwin":
		// lsof/network lifecycle collection is forensic-only on macOS.
		if s.ActiveProfile != ProfileForensic {
			s.ProcmonEnabled = false
			s.NetflowEnabled = false
			s.ServicesInterval = 30 * time.Minute
			s.InventoryRefresh = 12 * time.Hour
			s.FirewallRefresh = 0
		}
	case "aix":
		// AIX support is intentionally lightweight: perfstat/host metrics and
		// low-frequency inventory, not high-fidelity Linux-style forensics.
		s.ProcmonEnabled = false
		s.NetflowEnabled = false
		s.FileAccessDefaultEnabled = false
		s.DBQueryDefaultEnabled = false
		s.ServicesInterval = 30 * time.Minute
		s.InventoryRefresh = 12 * time.Hour
		s.FirewallRefresh = 0
	default:
		if s.ActiveProfile != ProfileForensic {
			s.ProcmonEnabled = false
			s.NetflowEnabled = false
		}
	}
}

func capabilitiesFor(s Settings) []string {
	caps := []string{
		"agent_runtime_profiles.v1",
		"collector_state.v1",
		"agent_self_metrics.v1",
		"heartbeat_inventory_cache.v1",
	}
	switch s.GOOS {
	case "linux":
		caps = append(caps, "linux_procfs_delta.v1")
	case "windows":
		caps = append(caps, "windows_native_netflow.v1", "windows_registry_inventory.v1")
	case "darwin":
		caps = append(caps, "macos_lightweight_default.v1")
	case "aix":
		caps = append(caps, "aix_lightweight_perfstat.v1")
	}
	if s.NetflowEnabled {
		caps = append(caps, "netflow_lifecycle.v1")
	}
	if s.FileAccessDefaultEnabled {
		caps = append(caps, "file_access_forensic.v1")
	}
	return caps
}
