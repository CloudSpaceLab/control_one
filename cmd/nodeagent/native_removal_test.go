package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/stretchr/testify/require"
)

func TestNativeDPKGIsolatedRemovalOracle(t *testing.T) {
	if os.Getenv("CONTROLONE_NATIVE_REMOVAL_ORACLE") != "1" {
		t.Skip("existing disposable Ubuntu lab, isolated dpkg root only")
	}
	base := t.TempDir()
	packageRoot := filepath.Join(base, "package")
	root := filepath.Join(base, "root")
	database := filepath.Join(root, "var/lib/dpkg")
	for _, directory := range []string{filepath.Join(packageRoot, "DEBIAN"), filepath.Join(packageRoot, "usr/share/controlone-phase2-fixture"), filepath.Join(database, "updates")} {
		require.NoError(t, os.MkdirAll(directory, 0755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(database, "status"), nil, 0600))
	// No executable payload, conffile, pre/post-install script or service unit.
	require.NoError(t, os.WriteFile(filepath.Join(packageRoot, "DEBIAN/control"), []byte("Package: controlone-phase2-removal-fixture\nVersion: 1.0\nArchitecture: all\nMaintainer: Qualification <noreply@example.invalid>\nDescription: isolated non-executable package-manager qualification fixture\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(packageRoot, "usr/share/controlone-phase2-fixture/README"), []byte("Disposable isolated-root qualification data only.\n"), 0644))
	deb := filepath.Join(base, "fixture.deb")
	run := func(command string, args ...string) []byte {
		t.Helper()
		out, err := exec.Command(command, args...).CombinedOutput()
		require.NoError(t, err, string(out))
		return out
	}
	run("dpkg-deb", "--build", "--root-owner-group", packageRoot, deb)
	run("dpkg", "--root", root, "--admindir", database, "--unpack", deb)
	run("dpkg", "--root", root, "--admindir", database, "--configure", "controlone-phase2-removal-fixture")
	query := func() []PackageInfo {
		out := run("dpkg-query", "--admindir", database, "-W", "-f=${Package}\t${Version}\t${Architecture}\t${db:Status-Status}\n")
		items, err := parseCompleteNativeInventory(string(out), "dpkg")
		require.NoError(t, err)
		return items
	}
	before := query()
	require.Len(t, before, 1)
	require.Equal(t, "controlone-phase2-removal-fixture", before[0].Name)
	beforeAt := time.Now().UTC()
	run("dpkg", "--root", root, "--admindir", database, "--remove", "controlone-phase2-removal-fixture")
	after := query()
	require.Empty(t, after)
	afterAt := time.Now().UTC()
	beforeMeta := packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: beforeAt, SHA256: packageinventory.Digest(before), Count: len(before)}
	afterMeta := packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: afterAt, SHA256: packageinventory.Digest(after), Count: len(after)}
	require.NoError(t, beforeMeta.Validate(before, afterAt))
	require.NoError(t, afterMeta.Validate(after, afterAt))
	if path := os.Getenv("CONTROLONE_NATIVE_REMOVAL_EVIDENCE"); path != "" {
		raw, err := json.Marshal(map[string]any{"before": before, "before_snapshot": beforeMeta, "after": after, "after_snapshot": afterMeta})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, raw, 0600))
	}
	t.Logf("Real isolated dpkg transition: installed=%d removed=%d before=%s after=%s", len(before), len(after), beforeMeta.SHA256, afterMeta.SHA256)
}
