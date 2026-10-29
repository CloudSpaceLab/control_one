package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func (f *fakeStore) RecordNodeAssessment(_ context.Context, a storage.NodeAssessment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assessments = append(f.assessments, a)
	return nil
}

// Handler tests use this in-memory adapter; transaction/fault behavior is
// qualified separately against PostgreSQL, not inferred from this fake.
func (f *fakeStore) ApplyNodeAssessment(ctx context.Context, a storage.NodeAssessment, findings []storage.VulnerabilityFinding, resolutions []storage.ReconcileVulnerabilityFindingsParams) error {
	if err := f.UpsertVulnerabilityFindings(ctx, findings); err != nil {
		return err
	}
	for _, r := range resolutions {
		if _, err := f.ReconcileVulnerabilityFindings(ctx, r); err != nil {
			return err
		}
	}
	return f.RecordNodeAssessment(ctx, a)
}

func TestNodeAssessmentPersistsFailureAndPartialEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		offset time.Duration
		want   string
	}{
		{"partial", 0, "partial"}, {"expired", -48 * time.Hour, "expired"}, {"missing", 0, "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			node := storage.Node{ID: uuid.New(), TenantID: uuid.New()}
			store := &fakeStore{nodes: []storage.Node{node}}
			require.NoError(t, store.ReplaceNodePackages(ctx, node.ID, []storage.NodePackage{{NodeID: node.ID, Name: "demo-package", Version: "1.0.0", Source: "npm"}}))
			root := t.TempDir()
			now := time.Now().UTC().Add(test.offset)
			if test.name != "missing" {
				pub, priv, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
				feed := offlinebundle.VulnerabilityFeed{SchemaVersion: 1, Advisories: []offlinebundle.VulnerabilityAdvisory{{CVEID: "CVE-2026-TEST", AffectedPackages: []offlinebundle.VulnerabilityAffectedPkg{{Name: "demo-package", Source: "npm", VersionScheme: "semver", VersionRange: "<2.0.0", FixedVersion: "2.0.0"}}}}}
				bundle := makeServerVulnerabilityBundleWithFeed(t, priv, "assessment-fixture", "1", 1, now, feed)
				_, err = offlinebundle.Import(ctx, bytes.NewReader(bundle), offlinebundle.ImportOptions{RootDir: root, PublicKey: pub, Now: now})
				require.NoError(t, err)
			}
			srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})
			srv.offlineContentRoot = root
			_, err := srv.importOfflineVulnerabilityFeedFindingsForNode(ctx, store, root, node, time.Now().UTC())
			if test.want == "partial" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			a, err := store.GetLatestNodeAssessment(ctx, node.TenantID, node.ID)
			require.NoError(t, err)
			require.NotNil(t, a)
			require.Equal(t, test.want, a.Status)
			require.Len(t, a.InventoryGeneration, 64)
			var evidence nodeAssessmentEvidence
			require.NoError(t, json.Unmarshal(a.Evidence, &evidence))
			require.Equal(t, 1, evidence.PackageCount)
			require.Equal(t, "unknown", evidence.InventoryCompleteness)
			require.Equal(t, test.want == "partial", evidence.FindingsUpdated)
			if test.name == "expired" {
				require.Len(t, evidence.FeedInputs, 1)
				require.False(t, evidence.FeedInputs[0].Validated)
				require.Empty(t, evidence.Feeds)
			}
			view, err := srv.latestNodeAssessment(ctx, &node)
			require.NoError(t, err)
			require.Equal(t, test.want, view.CurrentStatus)
			// A later inventory cannot inherit the applicability of the saved attempt.
			require.NoError(t, store.ReplaceNodePackages(ctx, node.ID, []storage.NodePackage{{NodeID: node.ID, Name: "demo-package", Version: "2.0.0", Source: "npm"}}))
			view, err = srv.latestNodeAssessment(ctx, &node)
			require.NoError(t, err)
			require.Equal(t, "unknown", view.CurrentStatus)
			require.Contains(t, view.CurrentReasons, "inventory_generation_changed")
			persisted, _ := store.GetLatestNodeAssessment(ctx, node.TenantID, node.ID)
			require.Equal(t, test.want, persisted.Status, "read-side applicability must not rewrite immutable evidence")
		})
	}
}
func (f *fakeStore) GetLatestNodeAssessment(_ context.Context, tenantID, nodeID uuid.UUID) (*storage.NodeAssessment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *storage.NodeAssessment
	for _, a := range f.assessments {
		if a.TenantID == tenantID && a.NodeID == nodeID && (latest == nil || a.StartedAt.After(latest.StartedAt) || a.StartedAt.Equal(latest.StartedAt) && a.ID.String() > latest.ID.String()) {
			copy := a
			latest = &copy
		}
	}
	return latest, nil
}
