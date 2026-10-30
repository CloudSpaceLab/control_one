package offlineapt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfflineSnapshotExactScopeAndTampering(t *testing.T) {
	for _, scenario := range []string{"unchanged", "source changed", "content changed", "content removed", "extra content", "manifest changed", "key changed", "digest mismatch", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			spec := fixture(t)
			s, err := Stage(spec, []Selection{{Name: "liblzma5", Version: "5.6.1+really5.4.5-1ubuntu0.4"}})
			require.NoError(t, err)
			defer s.Close()
			require.Len(t, s.Digest(), 64)
			require.NoError(t, s.Verify(s.Digest()))
			require.Equal(t, "liblzma5", s.manifest.Requested[0].Name)
			approval := s.ApprovalContent()
			approval.Requested[0].Version = "999"
			require.NotEqual(t, "999", s.manifest.Requested[0].Version, "exported identity must not alias private manifest")
			_, err = s.CheckApprovedArchives(context.Background(), approval)
			require.ErrorContains(t, err, "differs from staged snapshot")
			path := filepath.Join(s.spec.Repository, "dists", spec.Suite, "InRelease")
			switch scenario {
			case "unchanged":
				return
			case "source changed":
				require.NoError(t, os.WriteFile(filepath.Join(spec.Repository, "dists", spec.Suite, "InRelease"), []byte("source replaced"), 0644))
				require.NoError(t, s.Verify(s.Digest()), "staged bytes independent of source")
				return
			case "content changed":
				require.NoError(t, os.Chmod(path, 0644))
				require.NoError(t, os.WriteFile(path, []byte("tampered"), 0644))
			case "content removed":
				require.NoError(t, os.Chmod(filepath.Dir(path), 0755))
				require.NoError(t, os.Remove(path))
			case "extra content":
				require.NoError(t, os.Chmod(s.spec.Repository, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(s.spec.Repository, "unexpected"), nil, 0444))
			case "manifest changed":
				path = filepath.Join(s.root, "manifest.json")
				require.NoError(t, os.Chmod(path, 0644))
				require.NoError(t, os.WriteFile(path, []byte("{}"), 0644))
			case "key changed":
				require.NoError(t, os.Chmod(s.spec.Keyring, 0644))
				require.NoError(t, os.WriteFile(s.spec.Keyring, []byte("other key"), 0644))
			case "digest mismatch":
				require.Error(t, s.Verify(strings.Repeat("0", 64)))
				return
			case "symlink":
				require.NoError(t, os.Chmod(filepath.Dir(path), 0755))
				require.NoError(t, os.Remove(path))
				require.NoError(t, os.Symlink(spec.Keyring, path))
			}
			require.Error(t, s.Verify(s.Digest()))
			_, err = s.Assess(context.Background(), s.Digest())
			require.Error(t, err)
		})
	}
}

func TestOfflineSnapshotRejectsScopeAndRecursiveWork(t *testing.T) {
	spec := fixture(t)
	for _, scope := range [][]Selection{nil, {{Name: "--bad", Version: "1"}}, {{Name: "liblzma5", Version: "1;touch"}}, {{Name: "liblzma5", Version: "--1"}}, {{Name: "liblzma5", Version: "1"}, {Name: "liblzma5", Version: "2"}}} {
		_, err := Stage(spec, scope)
		require.Error(t, err)
	}
	spec.WorkRoot = spec.Repository
	_, err := Stage(spec, []Selection{{Name: "liblzma5", Version: "1"}})
	require.ErrorContains(t, err, "outside repository")
}
