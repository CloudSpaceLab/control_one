package storage

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func phase2SnapshotTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("PATCH_SECURITY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("dedicated disposable database required")
	}
	s, err := New(zap.NewNop(), config.DatabaseConfig{URL: url, MaxOpenConns: 8}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	var database string
	require.NoError(t, s.db.QueryRow(`SELECT current_database()`).Scan(&database))
	require.True(t, strings.HasPrefix(database, "controlone_patch_security_test_"))
	require.NoError(t, migrate.Apply(context.Background(), s.db))
	return s
}

func TestAuthoritativeRemovalDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	for _, scenario := range []string{"complete_empty", "complete_other_package", "legacy_unknown", "partial_rejected", "count_rejected", "narrower_feed", "application_outside_scope", "rollback", "reappears", "out_of_order"} {
		t.Run(scenario, func(t *testing.T) {
			node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString()})
			require.NoError(t, err)
			now := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
			old := VulnerabilityFinding{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, PackageName: "removed-demo", InstalledVersion: "1", PackageSource: "dpkg", Arch: "amd64", CVEID: "CVE-2026-REMOVAL-FIXTURE", EvidenceSource: "offline_vulnerability_feed", FirstSeenAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour)}
			if scenario == "application_outside_scope" {
				old.PackageSource = "npm"
			}
			require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{old}))
			var items []packageinventory.Item
			if scenario == "complete_other_package" {
				items = []packageinventory.Item{{Name: "other", Version: "2", Source: "dpkg", Arch: "amd64"}}
			}
			meta := packageinventory.Snapshot{Schema: 1, Scope: "dpkg", Complete: true, ObservedAt: now, SHA256: packageinventory.Digest(items), Count: len(items)}
			if scenario == "legacy_unknown" {
				require.NoError(t, s.ReplaceNodePackages(ctx, node.ID, nil))
				require.NoError(t, s.UpsertNodeInventorySync(ctx, NodeInventorySync{NodeID: node.ID, LastFullSync: now}))
			} else {
				if scenario == "partial_rejected" {
					meta.Complete = false
				}
				if scenario == "count_rejected" {
					meta.Count++
				}
				err = s.ReplaceNativePackageSnapshot(ctx, node.ID, items, meta, meta.SHA256)
				if scenario == "partial_rejected" || scenario == "count_rejected" {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
			}
			if scenario == "out_of_order" {
				older := meta
				older.ObservedAt = now.Add(-time.Minute)
				require.ErrorIs(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, items, older, older.SHA256), ErrAssessmentSuperseded)
			}
			var inventory []offlinebundle.PackageInventory
			for _, p := range items {
				inventory = append(inventory, offlinebundle.PackageInventory{Name: p.Name, Version: p.Version, Source: p.Source, Arch: p.Arch})
			}
			evidence := map[string]any{"native_snapshot": meta, "findings_updated": true, "assessed_advisory_ids": []string{old.CVEID}}
			if scenario == "legacy_unknown" {
				delete(evidence, "native_snapshot")
			}
			if scenario == "narrower_feed" {
				evidence["assessed_advisory_ids"] = []string{"CVE-2026-OTHER"}
			}
			raw, err := json.Marshal(evidence)
			require.NoError(t, err)
			a := NodeAssessment{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, StartedAt: now, CompletedAt: time.Now().UTC(), Status: "partial", InventoryGeneration: offlinebundle.VulnerabilityInventoryGeneration(inventory), Evidence: raw}
			if scenario == "rollback" {
				require.NoError(t, execAssessmentFault(ctx, s, "RAISE EXCEPTION 'removal rollback fixture'"))
			}
			err = s.ApplyNodeAssessment(ctx, a, nil, nil)
			if scenario == "rollback" {
				require.Error(t, err)
				_, err = s.db.ExecContext(ctx, `DROP TRIGGER assessment_fault ON node_assessments;DROP FUNCTION assessment_fault()`)
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
			}
			var resolved bool
			var basis string
			require.NoError(t, s.db.QueryRowContext(ctx, `SELECT resolved_at IS NOT NULL,COALESCE(evidence->>'resolution_basis','') FROM node_vulnerability_findings WHERE id=$1`, old.ID).Scan(&resolved, &basis))
			want := scenario != "legacy_unknown" && scenario != "narrower_feed" && scenario != "application_outside_scope" && scenario != "rollback"
			require.Equal(t, want, resolved)
			if want {
				require.Equal(t, "complete_native_package_removal", basis)
			}
			if scenario == "reappears" {
				items = []packageinventory.Item{{Name: old.PackageName, Version: old.InstalledVersion, Source: old.PackageSource, Arch: old.Arch}}
				meta.ObservedAt = now.Add(time.Millisecond)
				meta.Count = 1
				meta.SHA256 = packageinventory.Digest(items)
				require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, items, meta, meta.SHA256))
				old.LastSeenAt = meta.ObservedAt
				a.ID = uuid.New()
				a.StartedAt = meta.ObservedAt
				a.CompletedAt = time.Now().UTC()
				a.InventoryGeneration = offlinebundle.VulnerabilityInventoryGeneration([]offlinebundle.PackageInventory{{Name: old.PackageName, Version: old.InstalledVersion, Source: old.PackageSource, Arch: old.Arch}})
				a.Evidence = json.RawMessage(`{"findings_updated":true}`)
				require.NoError(t, s.ApplyNodeAssessment(ctx, a, []VulnerabilityFinding{old}, nil))
				require.NoError(t, s.db.QueryRowContext(ctx, `SELECT resolved_at IS NOT NULL FROM node_vulnerability_findings WHERE id=$1`, old.ID).Scan(&resolved))
				require.False(t, resolved, "reappearing vulnerable package must reopen its finding")
			}
		})
	}
}

func TestPublicationLeaseAndDivergentReplicaDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	reader, err := s.AcquireVulnerabilityPublication(ctx, false)
	require.NoError(t, err)
	deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err = s.AcquireVulnerabilityPublication(deadline, true)
	require.Error(t, err, "publisher cannot race an assessment")
	reader()
	reader()
	writer, err := s.AcquireVulnerabilityPublication(ctx, true)
	require.NoError(t, err)
	require.NoError(t, s.SetVulnerabilityPublication(ctx, "generation-a"))
	writer()
	reader, err = s.AcquireVulnerabilityPublication(ctx, false)
	require.NoError(t, err)
	require.NoError(t, s.CheckVulnerabilityPublication(ctx, "generation-a"))
	require.Error(t, s.CheckVulnerabilityPublication(ctx, "different-replica"))
	reader()
	leaseContext, terminate := context.WithCancel(ctx)
	release, err := s.AcquireVulnerabilityPublication(leaseContext, true)
	require.NoError(t, err)
	terminate()
	release()
	recoveryContext, recoverCancel := context.WithTimeout(ctx, time.Second)
	defer recoverCancel()
	recovered, err := s.AcquireVulnerabilityPublication(recoveryContext, true)
	require.NoError(t, err)
	recovered()
}
