package util

import (
	"runtime"
	"testing"
)

// TestReadMachineIDShape verifies ReadMachineID is callable and returns either
// an empty string (unsupported/unavailable) or a non-empty identifier without
// panicking. We don't assert a specific value because the result is OS- and
// host-specific; CI runners may have different machine-ids.
func TestReadMachineIDShape(t *testing.T) {
	id, err := ReadMachineID()
	if err != nil {
		t.Fatalf("ReadMachineID returned error: %v", err)
	}

	// The ID is either empty (unsupported OS or missing source file) or
	// a reasonably-sized identifier. Empty is valid — callers fall back to
	// hostname-based dedup in that case.
	if id != "" && len(id) < 8 {
		t.Fatalf("suspiciously short machine id %q on %s", id, runtime.GOOS)
	}
}

// TestReadMachineIDStable verifies multiple consecutive calls return the same
// identifier. The function is expected to be pure with respect to machine
// state across the test run.
func TestReadMachineIDStable(t *testing.T) {
	first, err := ReadMachineID()
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	second, err := ReadMachineID()
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if first != second {
		t.Fatalf("machine id not stable: %q vs %q", first, second)
	}
}
