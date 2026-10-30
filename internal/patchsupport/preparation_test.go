package patchsupport

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestOfflinePreparationContract(t *testing.T) {
	now := time.Now().UTC()
	repo := OfflineRepository{ID: uuid.NewString(), Name: "Approved Ubuntu", KeyringSHA256: strings.Repeat("a", 64), Suite: "noble-updates", Component: "main", Enabled: true}
	p := OfflinePreparation{NodeID: uuid.NewString(), Repository: repo, Packages: []ExactPackage{{Name: "libudev1", Version: "255.4-1ubuntu8.18"}}, ExpiresAt: now.Add(10 * time.Minute)}
	require.NoError(t, p.Validate(now))
	require.Equal(t, "/var/cache/control-one/offline-sources/"+repo.ID+"/repository", repo.RepositoryPath())
	for _, scenario := range []string{"traversal", "wrong key", "disabled", "expired", "future", "shell", "duplicate", "no scope"} {
		t.Run(scenario, func(t *testing.T) {
			c := p
			c.Packages = append([]ExactPackage(nil), p.Packages...)
			switch scenario {
			case "traversal":
				c.Repository.ID = "../../etc"
			case "wrong key":
				c.Repository.KeyringSHA256 = "untrusted"
			case "disabled":
				c.Repository.Enabled = false
			case "expired":
				c.ExpiresAt = now
			case "future":
				c.ExpiresAt = now.Add(time.Hour)
			case "shell":
				c.Packages[0].Version = "1;touch /tmp/x"
			case "duplicate":
				c.Packages = append(c.Packages, c.Packages[0])
			case "no scope":
				c.Packages = nil
			}
			require.Error(t, c.Validate(now))
		})
	}
}
