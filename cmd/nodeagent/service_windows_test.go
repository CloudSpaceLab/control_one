//go:build windows

package main

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

// TestWindowsServiceConstantsExposed confirms the symbols installService and
// uninstallService rely on for idempotent install + tolerant uninstall
// resolve on windows.amd64. The real install path calls into the SCM, which
// requires admin privileges and is not exercised in unit tests (CI runs as
// a non-admin user). These assertions instead verify:
//  1. The service name/display constants are non-empty (a blank name would
//     cause CreateService to silently misbehave).
//  2. The Windows error sentinels we branch on in installService and
//     uninstallService are reachable and distinct from nil.
//
// The integration-style end-to-end test lives outside the unit suite.
func TestWindowsServiceConstantsExposed(t *testing.T) {
	if windowsServiceName == "" {
		t.Fatal("windowsServiceName must not be empty")
	}
	if windowsServiceDisplayName == "" {
		t.Fatal("windowsServiceDisplayName must not be empty")
	}
}

// TestWindowsErrorSentinelsDistinct verifies the syscall error constants
// installService/uninstallService treat as "benign" (already running, doesn't
// exist, not active, cannot accept ctrl) are distinct values — a regression
// where two of them collapsed to the same code would make the idempotency
// branches fire incorrectly.
func TestWindowsErrorSentinelsDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ERROR_SERVICE_ALREADY_RUNNING":    windows.ERROR_SERVICE_ALREADY_RUNNING,
		"ERROR_SERVICE_DOES_NOT_EXIST":     windows.ERROR_SERVICE_DOES_NOT_EXIST,
		"ERROR_SERVICE_NOT_ACTIVE":         windows.ERROR_SERVICE_NOT_ACTIVE,
		"ERROR_SERVICE_CANNOT_ACCEPT_CTRL": windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL,
	}
	seen := map[error]string{}
	for name, errSentinel := range sentinels {
		if errSentinel == nil {
			t.Fatalf("%s sentinel is nil", name)
		}
		if dup, ok := seen[errSentinel]; ok {
			t.Fatalf("sentinels collapsed: %s == %s", name, dup)
		}
		seen[errSentinel] = name
	}

	// Quick sanity: errors.Is follows syscall.Errno equality, which is how
	// installService compares them.
	if !errors.Is(windows.ERROR_SERVICE_ALREADY_RUNNING, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		t.Fatal("errors.Is broken against syscall.Errno — idempotency branch will misfire")
	}
}
