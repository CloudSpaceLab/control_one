package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func offlineIdentityFixture() *patchsupport.OfflineContent {
	return &patchsupport.OfflineContent{Format: 2, ManifestSHA256: strings.Repeat("a", 64), InstalledStatusSHA256: strings.Repeat("b", 64), KeyringSHA256: strings.Repeat("c", 64), Suite: "noble-updates", Component: "main", Architecture: "amd64", Requested: []patchsupport.ExactPackage{{Name: "liblzma5", Version: "1.2.3"}}, Resolved: []patchsupport.ExactPackage{{Name: "liblzma5", Version: "1.2.3"}}}
}

func TestOfflineAlwaysRequiresApproval(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	store := newPatchTestStore(tenant, node)
	srv := newPatchTestServer(store)
	ctx := context.Background()
	store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", OfflineAPTVersion: 1}, ObservedAt: time.Now().UTC()}
	store.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{tenant: {TenantID: tenant, PatchRequiresApproval: false}}
	d, err := store.CreatePatchDeployment(ctx, storage.PatchDeployment{TenantID: tenant, Mode: "airgapped", Summary: map[string]any{"offline_content": offlineIdentityFixture(), "package_allowlist": []string{"liblzma5"}, "post_patch_rescan": true}})
	require.NoError(t, err)
	gate := srv.runPatchSafetyGates(ctx, tenant, node, d.ID, "airgapped", nil, nil)
	require.False(t, gate.Allowed)
	require.True(t, gate.AwaitingApproval)
	require.NotEqual(t, uuid.Nil, gate.ApprovalID)
	require.Empty(t, store.jobs)
	missing, err := store.CreatePatchDeployment(ctx, storage.PatchDeployment{TenantID: tenant, Mode: "airgapped", Summary: map[string]any{"package_allowlist": []string{"liblzma5"}}})
	require.NoError(t, err)
	blocked := srv.runPatchSafetyGates(ctx, tenant, node, missing.ID, "airgapped", nil, nil)
	require.False(t, blocked.Allowed)
	require.False(t, blocked.AwaitingApproval)
	require.Contains(t, blocked.Reason, "format-2")
}

func TestOfflineContentPolicyRoundTripAndHashBinding(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	store := newPatchTestStore(tenant, node)
	srv := newPatchTestServer(store)
	ctx := context.Background()
	policy, err := patchPackagePolicyFromRequest(patchDeployRequest{Mode: "airgapped", NodeIDs: []string{node.String()}, PackageAllowlist: []string{"liblzma5"}, OfflineContent: offlineIdentityFixture()})
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{"offline_content": policy.OfflineContent, "package_allowlist": policy.Allowlist, "package_denylist": policy.Denylist, "post_patch_rescan": policy.PostPatchRescan})
	require.NoError(t, err)
	var summary map[string]any
	require.NoError(t, json.Unmarshal(body, &summary))
	restored := patchPackagePolicyFromSummary(summary)
	require.Equal(t, policy, restored)
	first, _, err := srv.patchApprovalPolicyHash(ctx, tenant, node, "airgapped", nil, nil, policy)
	require.NoError(t, err)
	for _, field := range []string{"manifest", "installed", "key", "version", "suite", "component", "architecture"} {
		changed := patchPackagePolicy{Allowlist: []string{"liblzma5"}, OfflineContent: offlineIdentityFixture(), PostPatchRescan: policy.PostPatchRescan}
		switch field {
		case "manifest":
			changed.OfflineContent.ManifestSHA256 = strings.Repeat("d", 64)
		case "installed":
			changed.OfflineContent.InstalledStatusSHA256 = strings.Repeat("d", 64)
		case "key":
			changed.OfflineContent.KeyringSHA256 = strings.Repeat("d", 64)
		case "version":
			changed.OfflineContent.Requested[0].Version = "1.2.4"
			changed.OfflineContent.Resolved[0].Version = "1.2.4"
		case "suite":
			changed.OfflineContent.Suite = "noble-security"
		case "component":
			changed.OfflineContent.Component = "universe"
		case "architecture":
			changed.OfflineContent.Architecture = "arm64"
		}
		next, _, err := srv.patchApprovalPolicyHash(ctx, tenant, node, "airgapped", nil, nil, changed)
		require.NoError(t, err)
		require.NotEqual(t, first, next, field)
	}
	params := patchNodeActionPlanParams(tenant, uuid.New(), uuid.New(), node, "airgapped", policy)
	require.Equal(t, policy.OfflineContent, params.Diff["offline_content"])
	_, err = srv.dispatchPatchModeToNode(ctx, tenant, uuid.New(), node, "airgapped", nil, nil, policy)
	require.ErrorContains(t, err, "requires explicit approval")
	require.Empty(t, store.jobs)
	require.Empty(t, store.states)
}

func TestOfflineContentRejectsMalformedOrMixedPolicy(t *testing.T) {
	var absent *patchsupport.OfflineContent
	require.Nil(t, patchPackagePolicyFromSummary(map[string]any{"offline_content": absent}).OfflineContent)
	for _, raw := range []any{"bad", map[string]any{"format": "wrong"}, map[string]any{"format": 1}} {
		policy := patchPackagePolicyFromSummary(map[string]any{"offline_content": raw, "package_allowlist": []string{"liblzma5"}})
		require.NotNil(t, policy.OfflineContent)
		require.Error(t, policy.OfflineContent.Validate("airgapped", policy.Allowlist, nil))
	}
	for _, mode := range []string{"direct", "proxy", "auto", "airgapped"} {
		_, err := patchPackagePolicyFromRequest(patchDeployRequest{Mode: mode, PackageAllowlist: []string{"liblzma5"}, OfflineContent: offlineIdentityFixture()})
		require.Error(t, err, "explicit single target required")
	}
}
