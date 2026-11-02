package offlineapt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReviewResolvedPlanUsesPinnedBaseline(t *testing.T) {
	status := []byte("Package: liba\nStatus: install ok installed\nArchitecture: amd64\nVersion: 1:1.0-1\n\nPackage: liba\nStatus: install ok installed\nArchitecture: arm64\nVersion: 9\n\nPackage: libb\nStatus: deinstall ok config-files\nArchitecture: amd64\nVersion: 2\n\nPackage: data\nStatus: install ok installed\nArchitecture: all\nVersion: 3\n")
	now := time.Now().UTC()
	resolved := []Selection{{Name: "data", Version: "3"}, {Name: "liba", Version: "1:2.0-1"}, {Name: "liba:arm64", Version: "10"}, {Name: "libb", Version: "3"}}
	r, err := reviewResolvedPlan(status, []Selection{{Name: "liba", Version: "1:2.0-1"}}, resolved, "amd64", now)
	require.NoError(t, err)
	require.Equal(t, []string{"3", "1:1.0-1", "9", ""}, []string{r.Changes[0].InstalledVersion, r.Changes[1].InstalledVersion, r.Changes[2].InstalledVersion, r.Changes[3].InstalledVersion})
	require.Equal(t, "requested", r.Changes[1].Role)
	require.Equal(t, "dependency", r.Changes[0].Role)
	require.Equal(t, now.Add(time.Hour), r.ExpiresAt)
	require.Equal(t, "unknown", r.Reboot)
	_, err = reviewResolvedPlan(append(status, []byte("\nPackage: liba\nStatus: install ok installed\nArchitecture: amd64\nVersion: 5\n")...), nil, resolved, "amd64", now)
	require.ErrorContains(t, err, "ambiguous")
}

func TestExpiredSnapshotFailsBeforeFilesystemAccess(t *testing.T) {
	s := &Snapshot{}
	s.manifest.Format = 3
	require.ErrorContains(t, s.Verify(""), "expired or not yet valid")
}
