package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestApplyNodeAssessmentDedicatedPostgres(t *testing.T) {
	url := os.Getenv("PATCH_SECURITY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("dedicated disposable database required")
	}
	ctx := context.Background()
	s, err := New(zap.NewNop(), config.DatabaseConfig{URL: url, MaxOpenConns: 8}, Options{})
	require.NoError(t, err)
	defer s.Close()
	var database string
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database))
	require.True(t, strings.HasPrefix(database, "controlone_patch_security_test_"))
	require.NoError(t, migrate.Apply(ctx, s.db))
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString()})
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	inventory := func(version string) {
		require.NoError(t, s.ReplaceNodePackages(ctx, node.ID, []NodePackage{{Name: "demo", Version: version, Source: "npm"}}))
	}
	inventory("2.0.0")
	require.NoError(t, s.UpsertNodeInventorySync(ctx, NodeInventorySync{NodeID: node.ID, PackageCount: 1, LastFullSync: now}))
	old := VulnerabilityFinding{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, PackageName: "demo", InstalledVersion: "1.0.0", PackageSource: "npm", CVEID: "CVE-2026-TEST-A", EvidenceSource: "offline_vulnerability_feed", FirstSeenAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour)}
	omitted := old
	omitted.ID = uuid.New()
	omitted.CVEID = "CVE-2026-TEST-OMITTED"
	require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{old, omitted}))
	active := old
	active.ID = uuid.New()
	active.InstalledVersion = "2.0.0"
	active.CVEID = "CVE-2026-TEST-STILL"
	active.LastSeenAt = now
	record := NodeAssessment{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, StartedAt: now, CompletedAt: now.Add(time.Millisecond), Status: "partial", InventoryGeneration: offlinebundle.VulnerabilityInventoryGeneration([]offlinebundle.PackageInventory{{Name: "demo", Version: "2.0.0", Source: "npm"}}), Evidence: json.RawMessage(`{"findings_updated":true}`)}
	resolution := ReconcileVulnerabilityFindingsParams{TenantID: tenant.ID, NodeID: node.ID, EvidenceSource: "offline_vulnerability_feed", CVEIDs: []string{old.CVEID}, PackageScopes: []VulnerabilityFindingPackageScope{{PackageName: "demo", InstalledVersion: "2.0.0", PackageSource: "npm"}}, ResolveHistoricalVersions: true, ObservedAt: now}
	count := func(query string) int {
		var n int
		require.NoError(t, s.db.QueryRowContext(ctx, query, node.ID).Scan(&n))
		return n
	}
	for _, body := range []string{"RAISE EXCEPTION 'assessment fault'", "PERFORM pg_terminate_backend(pg_backend_pid())"} {
		require.NoError(t, execAssessmentFault(ctx, s, body))
		err := s.ApplyNodeAssessment(ctx, record, []VulnerabilityFinding{active}, []ReconcileVulnerabilityFindingsParams{resolution})
		require.Error(t, err)
		_, err = s.db.ExecContext(ctx, `DROP TRIGGER assessment_fault ON node_assessments; DROP FUNCTION assessment_fault();`)
		require.NoError(t, err)
		require.Equal(t, 0, count(`SELECT count(*) FROM node_vulnerability_findings WHERE node_id=$1 AND resolved_at IS NOT NULL`))
		require.Equal(t, 2, count(`SELECT count(*) FROM node_vulnerability_findings WHERE node_id=$1`))
		require.Equal(t, 0, count(`SELECT count(*) FROM node_assessments WHERE node_id=$1`))
	}
	inventory("3.0.0")
	require.ErrorIs(t, s.ApplyNodeAssessment(ctx, record, nil, []ReconcileVulnerabilityFindingsParams{resolution}), ErrAssessmentSuperseded)
	inventory("2.0.0")
	// Concurrent duplicate reports can commit only one complete effect set.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.ApplyNodeAssessment(ctx, record, []VulnerabilityFinding{active}, []ReconcileVulnerabilityFindingsParams{resolution})
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, count(`SELECT count(*) FROM node_assessments WHERE node_id=$1`))
	require.Equal(t, 1, count(`SELECT count(*) FROM node_vulnerability_findings WHERE node_id=$1 AND resolved_at IS NOT NULL`))
	require.Equal(t, 2, count(`SELECT count(*) FROM node_vulnerability_findings WHERE node_id=$1 AND resolved_at IS NULL`), "narrower feed retains omitted advisory and still-vulnerable finding")
	delayed := record
	delayed.ID = uuid.New()
	delayed.StartedAt = now.Add(-time.Minute)
	require.ErrorIs(t, s.ApplyNodeAssessment(ctx, delayed, []VulnerabilityFinding{old}, nil), ErrAssessmentSuperseded)
	require.Equal(t, 1, count(`SELECT count(*) FROM node_vulnerability_findings WHERE node_id=$1 AND resolved_at IS NOT NULL`))
	wrong := resolution
	wrong.TenantID = uuid.New()
	next := record
	next.ID = uuid.New()
	next.StartedAt = now.Add(time.Second)
	next.CompletedAt = now.Add(2 * time.Second)
	require.Error(t, s.ApplyNodeAssessment(ctx, next, nil, []ReconcileVulnerabilityFindingsParams{wrong}))
	require.Equal(t, 1, count(`SELECT count(*) FROM node_assessments WHERE node_id=$1`))
}

func execAssessmentFault(ctx context.Context, s *Store, body string) error {
	if body == "" {
		return errors.New("fault body required")
	}
	_, err := s.db.ExecContext(ctx, `CREATE FUNCTION assessment_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN `+body+`; RETURN NEW; END $$; CREATE TRIGGER assessment_fault BEFORE INSERT ON node_assessments FOR EACH ROW EXECUTE FUNCTION assessment_fault();`)
	return err
}

func TestAssessmentResolutionCompletenessDedicatedPostgres(t *testing.T) {
	url := os.Getenv("PATCH_SECURITY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("dedicated disposable database required")
	}
	ctx := context.Background()
	s, err := New(zap.NewNop(), config.DatabaseConfig{URL: url}, Options{})
	require.NoError(t, err)
	defer s.Close()
	var database string
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database))
	require.True(t, strings.HasPrefix(database, "controlone_patch_security_test_"))
	require.NoError(t, migrate.Apply(ctx, s.db))
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	for _, scenario := range []string{"fresh", "removed", "missing_sync", "stale", "future", "count_mismatch", "wrong_arch", "active_peer"} {
		t.Run(scenario, func(t *testing.T) {
			node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString()})
			require.NoError(t, err)
			now := time.Now().UTC().Truncate(time.Microsecond)
			old := VulnerabilityFinding{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, PackageName: "demo", InstalledVersion: "1.0.0", PackageSource: "apt", Arch: "amd64", CVEID: "CVE-2026-FIXTURE", EvidenceSource: "offline_vulnerability_feed", FirstSeenAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour)}
			require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{old}))
			arch := "amd64"
			if scenario == "wrong_arch" {
				arch = "arm64"
			}
			var packages []NodePackage
			var inventory []offlinebundle.PackageInventory
			if scenario != "removed" {
				packages = []NodePackage{{Name: "demo", Version: "2.0.0", Source: "apt", Arch: &arch}}
				inventory = []offlinebundle.PackageInventory{{Name: "demo", Version: "2.0.0", Source: "apt", Arch: arch}}
			}
			require.NoError(t, s.ReplaceNodePackages(ctx, node.ID, packages))
			if scenario != "missing_sync" {
				fullSync, count := now, len(packages)
				if scenario == "stale" {
					fullSync = now.Add(-48 * time.Hour)
				}
				if scenario == "future" {
					fullSync = now.Add(time.Hour)
				}
				if scenario == "count_mismatch" {
					count++
				}
				require.NoError(t, s.UpsertNodeInventorySync(ctx, NodeInventorySync{NodeID: node.ID, PackageCount: count, LastFullSync: fullSync}))
			}
			a := NodeAssessment{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, StartedAt: now, CompletedAt: now.Add(time.Millisecond), Status: "partial", InventoryGeneration: offlinebundle.VulnerabilityInventoryGeneration(inventory), Evidence: json.RawMessage(`{"inventory_completeness":"unknown","findings_updated":true}`)}
			var resolutions []ReconcileVulnerabilityFindingsParams
			if scenario != "removed" {
				r := ReconcileVulnerabilityFindingsParams{TenantID: tenant.ID, NodeID: node.ID, EvidenceSource: old.EvidenceSource, CVEIDs: []string{old.CVEID}, PackageScopes: []VulnerabilityFindingPackageScope{{PackageName: "demo", InstalledVersion: "2.0.0", PackageSource: "apt", Arch: "amd64"}}, ResolveHistoricalVersions: true, ObservedAt: now}
				if scenario == "active_peer" {
					r.ActiveFindings = []VulnerabilityFindingIdentity{{VulnerabilityFindingPackageScope: VulnerabilityFindingPackageScope{PackageName: "demo", InstalledVersion: "1.0.0", PackageSource: "apt", Arch: "amd64"}, CVEID: old.CVEID}}
				}
				resolutions = []ReconcileVulnerabilityFindingsParams{r}
			}
			err = s.ApplyNodeAssessment(ctx, a, nil, resolutions)
			accepted := scenario == "fresh" || scenario == "removed" || scenario == "active_peer"
			if accepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrAssessmentSuperseded)
			}
			var resolved bool
			require.NoError(t, s.db.QueryRowContext(ctx, `SELECT resolved_at IS NOT NULL FROM node_vulnerability_findings WHERE id=$1`, old.ID).Scan(&resolved))
			require.Equal(t, scenario == "fresh", resolved)
			var attempts int
			require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM node_assessments WHERE node_id=$1`, node.ID).Scan(&attempts))
			if accepted {
				require.Equal(t, 1, attempts)
			} else {
				require.Zero(t, attempts, "rejected resolution must not partially commit its assessment")
			}
		})
	}
}
