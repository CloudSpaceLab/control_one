package agentruntime

import (
	"testing"
	"time"
)

func TestResolveAutoProfilesByOS(t *testing.T) {
	tests := []struct {
		goos    string
		profile string
		netflow bool
		procmon bool
	}{
		{goos: "linux", profile: ProfileBalanced, netflow: true, procmon: true},
		{goos: "windows", profile: ProfileBalanced, netflow: true, procmon: false},
		{goos: "darwin", profile: ProfileLightweight, netflow: false, procmon: false},
		{goos: "aix", profile: ProfileLightweight, netflow: false, procmon: false},
	}

	for _, tt := range tests {
		got := ResolveForOS(ProfileAuto, tt.goos)
		if got.ActiveProfile != tt.profile {
			t.Fatalf("%s active profile = %q, want %q", tt.goos, got.ActiveProfile, tt.profile)
		}
		if got.NetflowEnabled != tt.netflow {
			t.Fatalf("%s netflow = %v, want %v", tt.goos, got.NetflowEnabled, tt.netflow)
		}
		if got.ProcmonEnabled != tt.procmon {
			t.Fatalf("%s procmon = %v, want %v", tt.goos, got.ProcmonEnabled, tt.procmon)
		}
	}
}

func TestResolveBalancedIntervals(t *testing.T) {
	linux := ResolveForOS(ProfileBalanced, "linux")
	if linux.ProcmonInterval != time.Minute {
		t.Fatalf("linux procmon interval = %s", linux.ProcmonInterval)
	}
	if linux.NetflowPollInterval != 15*time.Second {
		t.Fatalf("linux netflow interval = %s", linux.NetflowPollInterval)
	}
	if linux.InventoryRefresh != 6*time.Hour {
		t.Fatalf("linux inventory refresh = %s", linux.InventoryRefresh)
	}

	windows := ResolveForOS(ProfileBalanced, "windows")
	if windows.NetflowPollInterval != 30*time.Second {
		t.Fatalf("windows netflow interval = %s", windows.NetflowPollInterval)
	}
	if windows.InventoryRefresh != 12*time.Hour {
		t.Fatalf("windows inventory refresh = %s", windows.InventoryRefresh)
	}
	if windows.ServicesInterval != 0 {
		t.Fatalf("windows balanced services should be disabled until native reuse lands; got %s", windows.ServicesInterval)
	}
}

func TestForensicEnablesExpensiveCollectorsExceptAIX(t *testing.T) {
	darwin := ResolveForOS(ProfileForensic, "darwin")
	if !darwin.NetflowEnabled || !darwin.FileAccessDefaultEnabled {
		t.Fatalf("darwin forensic should enable expensive collectors: %#v", darwin)
	}

	aix := ResolveForOS(ProfileForensic, "aix")
	if aix.NetflowEnabled || aix.FileAccessDefaultEnabled || aix.ProcmonEnabled {
		t.Fatalf("aix must remain lightweight even when forensic requested: %#v", aix)
	}
}
