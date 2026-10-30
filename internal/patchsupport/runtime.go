// Package patchsupport defines execution availability, not production qualification.
package patchsupport

import (
	"fmt"
	"time"
)

type Runtime struct {
	ProxyLifecycleVersion    int    `json:"proxy_lifecycle_version,omitempty"`
	OfflinePrepareVersion    int    `json:"offline_prepare_version,omitempty"`
	OS                       string `json:"os"`
	Manager                  string `json:"manager"`
	CalendarWindowStartCheck bool   `json:"calendar_window_start_check,omitempty"`
	OfflineAPTVersion        int    `json:"offline_apt_version,omitempty"`
}

type Evidence struct {
	Runtime
	ObservedAt time.Time `json:"observed_at"`
}

func (e Evidence) Current(now time.Time) bool {
	return !e.ObservedAt.IsZero() && !e.ObservedAt.After(now) && now.Sub(e.ObservedAt) <= 5*time.Minute
}

func (r Runtime) Validate(mode string) error {
	if mode != "direct" && mode != "proxy" && mode != "airgapped" {
		return fmt.Errorf("unsupported patch mode %q", mode)
	}
	if mode == "airgapped" {
		if r.OS == "linux" && r.Manager == "apt" && r.OfflineAPTVersion == 1 {
			return nil
		}
		return fmt.Errorf("airgapped patch execution is unavailable until offline repository isolation is qualified")
	}
	switch r.OS {
	case "linux":
		if r.Manager == "apt" || r.Manager == "dnf" || r.Manager == "yum" {
			return nil
		}
	case "windows":
		if r.Manager == "winget" && mode == "direct" {
			return nil
		}
		if mode == "proxy" {
			return fmt.Errorf("Windows proxy patching is unavailable: winget proxy routing is not implemented")
		}
	}
	return fmt.Errorf("patch execution unavailable for runtime %q/%q", r.OS, r.Manager)
}
