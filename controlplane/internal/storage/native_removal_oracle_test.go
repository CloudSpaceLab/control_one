package storage

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNativeDPKGRemovalEvidenceDedicatedPostgres(t *testing.T) {
	path := os.Getenv("CONTROLONE_NATIVE_REMOVAL_EVIDENCE")
	if path == "" {
		t.Skip("native isolated-root oracle evidence required")
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var oracle struct {
		Before, After  []packageinventory.Item
		BeforeSnapshot packageinventory.Snapshot `json:"before_snapshot"`
		AfterSnapshot  packageinventory.Snapshot `json:"after_snapshot"`
	}
	require.NoError(t, json.Unmarshal(raw, &oracle))
	require.Len(t, oracle.Before, 1)
	require.Empty(t, oracle.After)
	require.Equal(t, "controlone-phase2-removal-fixture", oracle.Before[0].Name)
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: "native-dpkg-isolated-removal-fixture-" + uuid.NewString()})
	require.NoError(t, err)
	require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, oracle.Before, oracle.BeforeSnapshot, oracle.BeforeSnapshot.SHA256))
	finding := VulnerabilityFinding{ID: uuid.New(), NodeID: node.ID, TenantID: tenant.ID, PackageName: oracle.Before[0].Name, InstalledVersion: oracle.Before[0].Version, PackageSource: "dpkg", Arch: "all", CVEID: "CVE-2026-NATIVE-REMOVAL-FIXTURE", EvidenceSource: "offline_vulnerability_feed", FirstSeenAt: oracle.BeforeSnapshot.ObservedAt, LastSeenAt: oracle.BeforeSnapshot.ObservedAt}
	require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{finding}))
	require.NoError(t, s.ReplaceNativePackageSnapshot(ctx, node.ID, oracle.After, oracle.AfterSnapshot, oracle.AfterSnapshot.SHA256))
	evidence, err := json.Marshal(map[string]any{"native_snapshot": oracle.AfterSnapshot, "findings_updated": true, "assessed_advisory_ids": []string{finding.CVEID}})
	require.NoError(t, err)
	now := time.Now().UTC()
	attempt := NodeAssessment{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, StartedAt: now, CompletedAt: now, Status: "partial", InventoryGeneration: offlinebundle.VulnerabilityInventoryGeneration(nil), Evidence: evidence}
	require.NoError(t, s.ApplyNodeAssessment(ctx, attempt, nil, nil))
	var basis string
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT evidence->>'resolution_basis' FROM node_vulnerability_findings WHERE id=$1 AND resolved_at IS NOT NULL`, finding.ID).Scan(&basis))
	require.Equal(t, "complete_native_package_removal", basis)
}
