package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCandidateObservationPersistenceDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString()})
	require.NoError(t, err)
	items := []packageinventory.Item{{Name: "demo", Version: "1", Source: "dpkg", Arch: "amd64"}}
	now := time.Now().UTC().Add(-time.Second)
	meta := packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now, SHA256: packageinventory.Digest(items), Count: 1}
	require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, items, meta, meta.SHA256))
	base := packageinventory.CandidateObservation{Name: "demo", Arch: "amd64", InstalledVersion: "1", CandidateVersion: "2", ObservedAt: now, State: "verified_repository_candidate", Repository: "https://archive.ubuntu.com/ubuntu", Vendor: "Ubuntu", ReleaseDate: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), ReleaseSHA256: strings.Repeat("a", 64), IndexSHA256: strings.Repeat("b", 64), ArtifactSHA256: strings.Repeat("c", 64)}
	for _, scenario := range []string{"snapshot_mismatch", "installed_mismatch", "future", "expired", "unsafe_uri", "missing_provenance", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			o := base
			sha := meta.SHA256
			rows := []packageinventory.CandidateObservation{o}
			switch scenario {
			case "snapshot_mismatch":
				sha = "other-generation"
			case "installed_mismatch":
				o.InstalledVersion = "0"
			case "future":
				o.ObservedAt = time.Now().Add(time.Hour)
			case "expired":
				o.ExpiresAt = now.Add(-time.Second)
			case "unsafe_uri":
				o.Repository = "https://private:secret@example.test/ubuntu"
			case "missing_provenance":
				o.IndexSHA256 = ""
			case "duplicate":
				rows = append(rows, o)
			}
			rows[0] = o
			require.Error(t, s.RecordNodeUpdateCandidates(ctx, node.ID, sha, rows))
		})
	}
	require.NoError(t, s.RecordNodeUpdateCandidates(ctx, node.ID, meta.SHA256, []packageinventory.CandidateObservation{base}))
	changed := base
	changed.CandidateVersion = "999"
	require.NoError(t, s.RecordNodeUpdateCandidates(ctx, node.ID, meta.SHA256, []packageinventory.CandidateObservation{changed}))
	saved, err := s.GetNodeUpdateCandidates(ctx, node.ID)
	require.NoError(t, err)
	require.Len(t, saved, 1)
	require.Equal(t, "2", saved[0].CandidateVersion, "duplicate observation identity cannot overwrite its provenance")
	_, err = s.db.ExecContext(ctx, `UPDATE node_update_candidate_observations SET observed_at=NOW()-INTERVAL '2 hours' WHERE node_id=$1`, node.ID)
	require.NoError(t, err)
	saved, err = s.GetNodeUpdateCandidates(ctx, node.ID)
	require.NoError(t, err)
	require.Empty(t, saved, "expired observations are not current availability")
	base.ObservedAt = time.Now().UTC()
	require.NoError(t, s.RecordNodeUpdateCandidates(ctx, node.ID, meta.SHA256, []packageinventory.CandidateObservation{base}))
	items[0].Version = "3"
	meta.ObservedAt = time.Now().UTC()
	meta.SHA256 = packageinventory.Digest(items)
	require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, items, meta, meta.SHA256))
	saved, err = s.GetNodeUpdateCandidates(ctx, node.ID)
	require.NoError(t, err)
	require.Empty(t, saved, "another inventory generation cannot inherit old candidate evidence")
}
