package packageinventory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompleteSnapshotValidation(t *testing.T) {
	now := time.Now().UTC()
	items := []Item{{Name: "demo", Version: "1", Source: "dpkg", Arch: "amd64"}}
	base := Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now, SHA256: Digest(items), Count: 1}
	require.NoError(t, base.Validate(items, now))
	empty := base
	empty.Count = 0
	empty.SHA256 = Digest(nil)
	require.NoError(t, empty.Validate(nil, now), "explicit empty complete snapshot is valid")
	for _, scenario := range []string{"partial", "count", "hash", "future", "stale", "scope", "schema", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			s := base
			rows := items
			switch scenario {
			case "partial":
				s.Complete = false
			case "count":
				s.Count = 2
			case "hash":
				s.SHA256 = "wrong"
			case "future":
				s.ObservedAt = now.Add(time.Second)
			case "stale":
				s.ObservedAt = now.Add(-25 * time.Hour)
			case "scope":
				s.Scope = "rpm"
			case "schema":
				s.Schema = 2
			case "duplicate":
				rows = append(append([]Item{}, items...), items...)
				s.Count = 2
				s.SHA256 = Digest(rows)
			}
			require.Error(t, s.Validate(rows, now))
		})
	}
}
