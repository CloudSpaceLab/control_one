package server

import (
	"context"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPublishedOfflinePlanHeartbeatAndApproval(t *testing.T) {
	tenant, nodeID := uuid.New(), uuid.New()
	store := newPatchTestStore(tenant, nodeID)
	srv := newPatchTestServer(store)
	node := &store.nodes[0]
	node.Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", OfflineAPTVersion: 1}, ObservedAt: time.Now().UTC()}
	plan := offlineIdentityFixture()
	report := &patchsupport.PublishedPlanReport{Version: 1, Status: "ready", Plans: []patchsupport.OfflineContent{*plan}}
	updated, err := srv.updatePublishedOfflinePlans(context.Background(), node, report)
	require.NoError(t, err)
	policy := patchPackagePolicy{OfflineContent: plan, PublishedOfflinePlanSHA256: plan.ManifestSHA256, Allowlist: []string{"liblzma5"}, PostPatchRescan: true}
	require.NoError(t, validatePublishedOfflinePlan(updated, policy, time.Now().UTC()))
	first, _, err := srv.patchApprovalPolicyHash(context.Background(), tenant, nodeID, "airgapped", nil, nil, policy)
	require.NoError(t, err)
	manual := policy
	manual.PublishedOfflinePlanSHA256 = ""
	second, _, err := srv.patchApprovalPolicyHash(context.Background(), tenant, nodeID, "airgapped", nil, nil, manual)
	require.NoError(t, err)
	require.NotEqual(t, first, second, "changing plan source requires new review")
	for _, invalid := range []*patchsupport.PublishedPlanReport{nil, {Version: 99, Status: "ready", Plans: report.Plans}, {Version: 1, Status: "empty"}} {
		revoked, err := srv.updatePublishedOfflinePlans(context.Background(), updated, invalid)
		require.NoError(t, err)
		require.Error(t, validatePublishedOfflinePlan(revoked, policy, time.Now().UTC()))
		require.NoError(t, validatePublishedOfflinePlan(revoked, manual, time.Now().UTC()), "manual transfer keeps independent installer verification")
	}
}

func TestOperatorCannotForgePatchDiscoveryEvidence(t *testing.T) {
	actual := map[string]any{"patch.offline_plans": "authentic", "patch.runtime": "actual", "team": "old"}
	updated := preservePatchEvidence(actual, map[string]any{"patch.offline_plans": "forged", "patch.runtime": "forged", "patch.other": "forged", "team": "new"})
	require.Equal(t, "authentic", updated["patch.offline_plans"])
	require.Equal(t, "actual", updated["patch.runtime"])
	require.NotContains(t, updated, "patch.other")
	require.Equal(t, "new", updated["team"])
}
