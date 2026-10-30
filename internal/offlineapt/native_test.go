package offlineapt

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Reuses the lab's vendor-signed Ubuntu metadata and public archive keyring.
// No signing key is generated; all APT state is separate from the host's state.
func vendorFixture(t *testing.T) Spec {
	t.Helper()
	if runtime.GOOS != "linux" || os.Getenv("CONTROLONE_OFFLINE_APT_NATIVE") != "1" {
		t.Skip("explicit existing Ubuntu lab fixture required")
	}
	root, err := os.MkdirTemp("/tmp", "controlone-offline-native-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	require.NoError(t, os.Chmod(root, 0755))
	repo := filepath.Join(root, "repository")
	work := filepath.Join(root, "work")
	index := filepath.Join(repo, "dists", "noble-updates", "main", "binary-amd64", "Packages")
	require.NoError(t, os.MkdirAll(filepath.Dir(index), 0755))
	require.NoError(t, os.Mkdir(work, 0755))
	release, err := os.ReadFile("/var/lib/apt/lists/archive.ubuntu.com_ubuntu_dists_noble-updates_InRelease")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "dists", "noble-updates", "InRelease"), release, 0644))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	packages, err := exec.CommandContext(ctx, "/usr/lib/apt/apt-helper", "cat-file", "/var/lib/apt/lists/archive.ubuntu.com_ubuntu_dists_noble-updates_main_binary-amd64_Packages.lz4").Output()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(index, packages, 0644))
	compressed, err := os.ReadFile("/tmp/controlone-offline-Packages.xz")
	require.NoError(t, err, "stage the exact vendor-signed compressed index before the offline test")
	require.NoError(t, os.WriteFile(index+".xz", compressed, 0644))
	key := "/usr/share/keyrings/ubuntu-archive-keyring.gpg"
	keyBytes, err := os.ReadFile(key)
	require.NoError(t, err)
	return Spec{Repository: repo, WorkRoot: work, Keyring: key, KeyringSHA256: fmt.Sprintf("%x", sha256.Sum256(keyBytes)), Suite: "noble-updates", Component: "main", Architecture: "amd64"}
}

func TestNativeOfflineAPTFileOnlySignedRefresh(t *testing.T) {
	spec := vendorFixture(t)
	before, err := os.ReadFile("/var/lib/dpkg/status")
	require.NoError(t, err)
	t.Cleanup(func() {
		after, err := os.ReadFile("/var/lib/dpkg/status")
		require.NoError(t, err)
		require.Equal(t, sha256.Sum256(before), sha256.Sum256(after), "native assessment must not install packages")
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	var connections atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			_ = conn.Close()
		}
	}()
	trapRoot := filepath.Join(spec.WorkRoot, "ambient")
	require.NoError(t, os.Mkdir(trapRoot, 0755))
	marker := filepath.Join(trapRoot, "unexpected-hook")
	require.NoError(t, os.WriteFile(filepath.Join(trapRoot, "sources.list"), []byte("deb http://"+listener.Addr().String()+" stable main\n"), 0644))
	ambient := filepath.Join(trapRoot, "apt.conf")
	require.NoError(t, os.WriteFile(ambient, []byte(fmt.Sprintf("Dir::Etc::sourcelist %q;\nAPT::Update::Pre-Invoke { %q; };\n", filepath.Join(trapRoot, "sources.list"), "touch "+marker)), 0644))
	t.Setenv("APT_CONFIG", ambient)
	t.Setenv("http_proxy", "http://"+listener.Addr().String())
	t.Setenv("https_proxy", "http://"+listener.Addr().String())
	s, err := Prepare(spec)
	require.NoError(t, err)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := s.Refresh(ctx)
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "file:")
	require.NotContains(t, string(out), "http://")
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err))
	require.Zero(t, connections.Load())
	out, err = s.Simulate(ctx, []string{"liblzma5"})
	require.NoError(t, err, string(out))
	t.Log("signed file-only refresh and simulation passed; ambient HTTP source/proxy/hook unused; real installed package database unchanged")
	out, err = s.Simulate(ctx, []string{"controlone-missing-offline-package-qualification"})
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "Unable to locate package")
	// A failed refresh must revoke reuse of previously validated indexes.
	require.NoError(t, os.WriteFile(filepath.Join(spec.Repository, "dists", spec.Suite, "InRelease"), []byte("invalid signature"), 0644))
	out, err = s.Refresh(ctx)
	require.Error(t, err, string(out))
	_, err = s.Simulate(ctx, []string{"liblzma5"})
	require.ErrorContains(t, err, "refresh required")
}

func TestNativeOfflineAPTRejectsRepositoryFailures(t *testing.T) {
	for _, scenario := range []string{"signature", "index tampering", "missing index", "missing repository", "expired signed release"} {
		t.Run(scenario, func(t *testing.T) {
			spec := vendorFixture(t)
			before, err := os.ReadFile("/var/lib/dpkg/status")
			require.NoError(t, err)
			t.Cleanup(func() {
				after, err := os.ReadFile("/var/lib/dpkg/status")
				require.NoError(t, err)
				require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
			})
			s, err := Prepare(spec)
			require.NoError(t, err)
			defer s.Close()
			index := filepath.Join(spec.Repository, "dists", spec.Suite, "main", "binary-amd64", "Packages")
			switch scenario {
			case "signature":
				require.NoError(t, os.WriteFile(filepath.Join(spec.Repository, "dists", spec.Suite, "InRelease"), []byte("not a signed release"), 0644))
			case "index tampering":
				require.NoError(t, os.WriteFile(index, []byte("Package: tampered\nVersion: 999\n"), 0644))
				require.NoError(t, os.WriteFile(index+".xz", []byte("tampered compressed index"), 0644))
			case "missing index":
				require.NoError(t, os.Remove(index))
				require.NoError(t, os.Remove(index+".xz"))
			case "missing repository":
				require.NoError(t, os.Rename(spec.Repository, spec.Repository+"-unavailable"))
			case "expired signed release":
				old, err := os.ReadFile("/var/lib/apt/lists/archive.ubuntu.com_ubuntu_dists_noble_InRelease")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(spec.Repository, "dists", spec.Suite, "InRelease"), old, 0644))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out, err := s.Refresh(ctx)
			require.Error(t, err, string(out))
			if scenario == "expired signed release" {
				require.Contains(t, string(out), "expired")
			}
			_, err = s.Simulate(ctx, []string{"liblzma5"})
			require.Error(t, err)
			require.False(t, strings.Contains(string(out), "https://archive.ubuntu.com"), "no upstream fallback")
		})
	}
}

func TestNativeOfflineSnapshotExactVersion(t *testing.T) {
	spec := vendorFixture(t)
	before, err := os.ReadFile("/var/lib/dpkg/status")
	require.NoError(t, err)
	t.Cleanup(func() {
		after, err := os.ReadFile("/var/lib/dpkg/status")
		require.NoError(t, err)
		require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
	})
	version, err := exec.Command("/usr/bin/dpkg-query", "-W", "-f=${Version}", "liblzma5").Output()
	require.NoError(t, err)
	s, err := Stage(spec, []Selection{{Name: "liblzma5", Version: strings.TrimSpace(string(version))}})
	require.NoError(t, err)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := s.Assess(ctx, s.Digest())
	require.NoError(t, err, string(out))
	t.Logf("staged signed metadata and exact liblzma5 version %s; trusted digest %s; simulation only", version, s.Digest())
	absent, err := Stage(spec, []Selection{{Name: "liblzma5", Version: "0~controlone-absent"}})
	require.NoError(t, err)
	defer absent.Close()
	out, err = absent.Assess(ctx, absent.Digest())
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "was not found")
	// Replacing the source repository cannot change the copied staged bytes.
	require.NoError(t, os.WriteFile(filepath.Join(spec.Repository, "dists", spec.Suite, "InRelease"), []byte("source replaced after staging"), 0644))
	out, err = s.Assess(ctx, s.Digest())
	require.NoError(t, err, string(out))
	_, err = s.Assess(ctx, strings.Repeat("0", 64))
	require.ErrorContains(t, err, "digest mismatch")
	path := filepath.Join(s.spec.Repository, "dists", spec.Suite, "InRelease")
	require.NoError(t, os.Chmod(path, 0644))
	require.NoError(t, os.WriteFile(path, []byte("staged content changed"), 0644))
	_, err = s.Assess(ctx, s.Digest())
	require.ErrorContains(t, err, "content changed")
}

func TestNativeOfflineSnapshotArchives(t *testing.T) {
	for _, scenario := range []string{"valid archive", "changed archive", "missing archive"} {
		t.Run(scenario, func(t *testing.T) {
			spec := vendorFixture(t)
			before, err := os.ReadFile("/var/lib/dpkg/status")
			require.NoError(t, err)
			t.Cleanup(func() {
				after, err := os.ReadFile("/var/lib/dpkg/status")
				require.NoError(t, err)
				require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
			})
			archive, err := os.ReadFile("/tmp/controlone-offline-liblzma5.deb")
			require.NoError(t, err)
			path := filepath.Join(spec.Repository, "pool/main/x/xz-utils/liblzma5_5.6.1+really5.4.5-1ubuntu0.4_amd64.deb")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
			if scenario == "changed archive" {
				archive[0] ^= 0xff
			}
			if scenario != "missing archive" {
				require.NoError(t, os.WriteFile(path, archive, 0644))
			}
			s, err := Stage(spec, []Selection{{Name: "liblzma5", Version: "5.6.1+really5.4.5-1ubuntu0.4"}})
			require.NoError(t, err)
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			out, err := s.CheckApprovedArchives(ctx, s.ApprovalContent())
			if scenario == "valid archive" {
				require.NoError(t, err, string(out))
				require.Contains(t, string(out), "Download complete and in download only mode")
				t.Log("real local archive acquired and authenticated against signed index; download-only, package database unchanged")
			} else {
				require.Error(t, err, string(out))
				if scenario == "changed archive" {
					require.Contains(t, string(out), "Hash Sum mismatch")
				} else {
					require.Contains(t, string(out), "File not found")
				}
			}
		})
	}
}

func TestNativeOfflineMissingDependencyArchive(t *testing.T) {
	spec := vendorFixture(t)
	before, err := os.ReadFile("/var/lib/dpkg/status")
	require.NoError(t, err)
	t.Cleanup(func() {
		after, err := os.ReadFile("/var/lib/dpkg/status")
		require.NoError(t, err)
		require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
	})
	archive, err := os.ReadFile("/tmp/controlone-offline-liblzma5.deb")
	require.NoError(t, err)
	path := filepath.Join(spec.Repository, "pool/main/x/xz-utils/liblzma5_5.6.1+really5.4.5-1ubuntu0.4_amd64.deb")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, archive, 0644))
	s, err := Prepare(spec)
	require.NoError(t, err)
	defer s.Close()
	// Isolated installed-state fixture forces libc6 to be resolved as a new
	// dependency. The production snapshot API always pins the real host status.
	status := filepath.Join(s.root, "empty-installed-status")
	require.NoError(t, os.WriteFile(status, nil, 0644))
	config, err := os.ReadFile(s.config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(s.config, []byte(strings.ReplaceAll(string(config), `"/var/lib/dpkg/status"`, fmt.Sprintf("%q", status))), 0644))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := s.Refresh(ctx)
	require.NoError(t, err, string(out))
	out, err = s.run(ctx, "apt-get", "--download-only", "--assume-yes", "--no-remove", "install", "liblzma5=5.6.1+really5.4.5-1ubuntu0.4")
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "libc6")
	require.Contains(t, string(out), "File not found")
	require.NotContains(t, string(out), "http://")
	require.NotContains(t, string(out), "https://")
	t.Log("actual APT dependency resolution rejected missing libc6 archive with isolated empty installed-state fixture; real host database unchanged")
}
