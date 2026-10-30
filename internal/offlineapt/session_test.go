package offlineapt

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T) Spec {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux APT assessment session")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repository")
	work := filepath.Join(root, "work")
	key := filepath.Join(root, "keyring.gpg")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "dists", "noble-updates"), 0755))
	require.NoError(t, os.Mkdir(work, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "dists", "noble-updates", "InRelease"), []byte("fixture - not a valid signature"), 0644))
	require.NoError(t, os.WriteFile(key, []byte("public-key-fixture"), 0644))
	return Spec{Repository: repo, Keyring: key, KeyringSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("public-key-fixture"))), WorkRoot: work, Suite: "noble-updates", Component: "main", Architecture: "amd64"}
}

func TestOfflineAPTPreparationIsolation(t *testing.T) {
	spec := fixture(t)
	s, err := Prepare(spec)
	require.NoError(t, err)
	defer s.Close()
	config, err := os.ReadFile(s.config)
	require.NoError(t, err)
	for _, part := range []string{fmt.Sprintf("Dir::Etc::main %q", filepath.Join(s.root, "empty.conf")), fmt.Sprintf("Dir::Etc::parts %q", filepath.Join(s.root, "empty.d")), fmt.Sprintf("Dir::Etc::sourceparts %q", filepath.Join(s.root, "empty.d")), `Acquire::AllowInsecureRepositories "false"`, `APT::Update::Error-Mode "any"`} {
		require.Contains(t, string(config), part)
	}
	for _, transport := range []string{"http", "https", "ftp", "ssh", "mirror", "mirror+http", "cdrom"} {
		_, err := os.Stat(filepath.Join(s.root, "methods", transport))
		require.True(t, os.IsNotExist(err), transport)
	}
	source, err := os.ReadFile(filepath.Join(s.root, "sources.list"))
	require.NoError(t, err)
	require.Contains(t, string(source), "file:///")
	require.Contains(t, string(source), "signed-by=")
	require.NotContains(t, string(source), "trusted=yes")
	_, err = s.Simulate(context.Background(), []string{"liblzma5"})
	require.ErrorContains(t, err, "refresh required")
	s.refreshed = true
	for _, packages := range [][]string{nil, {"--allow-unauthenticated"}, {"liblzma5;touch"}, {"pkg\nfoo"}} {
		_, err = s.Simulate(context.Background(), packages)
		require.Error(t, err)
	}
	require.NoError(t, s.Close())
	_, err = s.Refresh(context.Background())
	require.Error(t, err)
}

func TestOfflineAPTRejectsUnsafeInputs(t *testing.T) {
	for _, scenario := range []string{"key pin", "missing release", "repo symlink", "content symlink", "writable repo", "injected selector", "relative path", "path injection"} {
		t.Run(scenario, func(t *testing.T) {
			spec := fixture(t)
			switch scenario {
			case "key pin":
				spec.KeyringSHA256 = strings.Repeat("0", 64)
			case "missing release":
				require.NoError(t, os.Remove(filepath.Join(spec.Repository, "dists", spec.Suite, "InRelease")))
			case "repo symlink":
				link := spec.Repository + "-link"
				require.NoError(t, os.Symlink(spec.Repository, link))
				spec.Repository = link
			case "content symlink":
				require.NoError(t, os.Symlink(spec.Keyring, filepath.Join(spec.Repository, "escape")))
			case "writable repo":
				require.NoError(t, os.Chmod(spec.Repository, 0777))
			case "injected selector":
				spec.Suite = "noble\ntrusted=yes"
			case "relative path":
				spec.Repository = "repository"
			case "path injection":
				spec.Repository += "\nAPT::Update::Pre-Invoke"
			}
			_, err := Prepare(spec)
			require.Error(t, err)
			files, err := os.ReadDir(spec.WorkRoot)
			require.NoError(t, err)
			require.Empty(t, files)
		})
	}
}
