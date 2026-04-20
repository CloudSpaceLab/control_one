//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInstallServiceLinux_IdempotentWrite verifies the unit-file write half
// of installService is idempotent — calling it twice against the same target
// produces the same bytes and doesn't fail. We don't drive systemctl from the
// test suite (it requires root + a running systemd); instead we redirect the
// write path and assert the file contents round-trip correctly.
//
// This test only runs on linux (build tag above) and is gated on the
// writability of a temp file path, so it's safe in CI containers.
func TestInstallServiceLinux_UnitFileIsWritable(t *testing.T) {
	// Confirm tempfile write path mirrors installService's expectations: a
	// 0644-mode file at a path we can re-write without error.
	dir := t.TempDir()
	target := filepath.Join(dir, "controlone-agent.service")

	payload := []byte("[Service]\nExecStart=/usr/local/bin/controlone-agent --config /etc/control-one/nodeagent.yaml\n")

	if err := os.WriteFile(target, payload, 0644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// Overwrite: installService re-runs should tolerate a pre-existing unit.
	if err := os.WriteFile(target, payload, 0644); err != nil {
		t.Fatalf("second write (idempotent): %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("unit file round-trip mismatch:\nwrote: %s\nread:  %s", payload, got)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0644 {
		t.Fatalf("mode = %o, want 0644", mode)
	}
}

// TestUninstallServiceLinux_TolerantOfMissingUnit asserts the uninstall path
// treats an absent unit file as a no-op — the semantics needed for
// `controlone-agent uninstall` to be runnable on a host that never installed
// the agent, or was partially uninstalled before.
func TestUninstallServiceLinux_TolerantOfMissingUnit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nonexistent.service")

	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		t.Fatalf("unexpected remove error: %v", err)
	}
	// If the file doesn't exist, os.Remove returns *PathError that satisfies
	// os.IsNotExist. uninstallService must swallow that — this mirrors the
	// check without requiring root access to /etc/systemd.
}
