package patchsupport

import "testing"

func TestExecutionAvailabilityMatrix(t *testing.T) {
	for _, os := range []string{"linux", "windows", "darwin", "unknown"} {
		for _, manager := range []string{"apt", "dnf", "yum", "winget", ""} {
			for _, mode := range []string{"direct", "proxy", "airgapped", "unexpected"} {
				want := (os == "linux" && (manager == "apt" || manager == "dnf" || manager == "yum") && (mode == "direct" || mode == "proxy")) || (os == "windows" && manager == "winget" && mode == "direct")
				if got := (Runtime{OS: os, Manager: manager}).Validate(mode) == nil; got != want {
					t.Errorf("%s/%s/%s: allowed=%v want=%v", os, manager, mode, got, want)
				}
			}
		}
	}
}

func TestOfflineAPTRequiresExactCapability(t *testing.T) {
	for _, r := range []Runtime{{OS: "linux", Manager: "apt", OfflineAPTVersion: 1}, {OS: "linux", Manager: "apt"}, {OS: "linux", Manager: "apt", OfflineAPTVersion: 2}, {OS: "windows", Manager: "winget", OfflineAPTVersion: 1}, {OS: "linux", Manager: "dnf", OfflineAPTVersion: 1}} {
		want := r.OS == "linux" && r.Manager == "apt" && r.OfflineAPTVersion == 1
		if (r.Validate("airgapped") == nil) != want {
			t.Fatalf("unexpected offline availability: %+v", r)
		}
	}
}
