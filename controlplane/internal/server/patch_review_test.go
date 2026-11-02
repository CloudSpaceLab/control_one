package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOfflineReviewedPlanApprovalHashAndExpiry(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	store := newPatchTestStore(tenant, node)
	srv := newPatchTestServer(store)
	runtime := patchsupport.Runtime{OS: "linux", Manager: "apt", OfflineAPTVersion: 1, OfflinePlanFormat: 3}
	store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: runtime, ObservedAt: time.Now().UTC()}
	content := offlineIdentityFixture()
	content.Format = 3
	now := time.Now().UTC()
	content.Review = &patchsupport.OfflineReview{PreparedAt: now.Add(-time.Minute), ExpiresAt: now.Add(59 * time.Minute), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []patchsupport.PlanChange{{Name: "liblzma5", InstalledVersion: "1.2.2", ProposedVersion: "1.2.3", Role: "requested"}}}
	policy := patchPackagePolicy{Allowlist: []string{"liblzma5"}, OfflineContent: content, PostPatchRescan: true}
	first, _, err := srv.patchApprovalPolicyHash(context.Background(), tenant, node, "airgapped", nil, nil, policy)
	require.NoError(t, err)
	content.Review.Changes[0].InstalledVersion = "1.2.1"
	changed, _, err := srv.patchApprovalPolicyHash(context.Background(), tenant, node, "airgapped", nil, nil, policy)
	require.NoError(t, err)
	require.NotEqual(t, first, changed, "before/after review is part of approval identity")
	content.Review.ExpiresAt = content.Review.ExpiresAt.Add(-time.Minute)
	changedExpiry, _, err := srv.patchApprovalPolicyHash(context.Background(), tenant, node, "airgapped", nil, nil, policy)
	require.NoError(t, err)
	require.NotEqual(t, changed, changedExpiry, "expiry cannot change under an existing approval")
	runtime.OfflinePlanFormat = 0
	require.ErrorContains(t, validateRuntimePackageScope(runtime, policy), "update it")
	content.Review.ExpiresAt = now.Add(-time.Second)
	_, _, err = srv.patchApprovalPolicyHash(context.Background(), tenant, node, "airgapped", nil, nil, policy)
	require.ErrorContains(t, err, "expired")
	_, err = patchPackagePolicyFromRequest(patchDeployRequest{Mode: "airgapped", NodeIDs: []string{node.String()}, PackageAllowlist: policy.Allowlist, OfflineContent: content})
	require.ErrorContains(t, err, "expired")
}

func TestOfflineReviewedApprovalRejectsChangedOrExpiredPlan(t *testing.T) {
	for _, scenario := range []string{"changed baseline review", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			tenant, node := uuid.New(), uuid.New()
			store := &patchSecurityStore{patchTestStore: newPatchTestStore(tenant, node), allowed: true}
			principal := patchOperatorPrincipal()
			store.users = map[string]*storage.User{principal.Subject: {ID: uuid.MustParse(principal.Subject), ExternalID: principal.Subject}}
			store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", OfflineAPTVersion: 1, OfflinePlanFormat: 3}, ObservedAt: time.Now().UTC()}
			srv := newPatchTestServer(store)
			content := offlineIdentityFixture()
			content.Format = 3
			now := time.Now().UTC()
			content.Review = &patchsupport.OfflineReview{PreparedAt: now.Add(-time.Minute), ExpiresAt: now.Add(59 * time.Minute), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []patchsupport.PlanChange{{Name: "liblzma5", InstalledVersion: "1.2.2", ProposedVersion: "1.2.3", Role: "requested"}}}
			body, _ := json.Marshal(patchDeployRequest{TenantID: tenant.String(), NodeIDs: []string{node.String()}, Mode: "airgapped", PackageAllowlist: []string{"liblzma5"}, OfflineContent: content})
			rec := httptest.NewRecorder()
			srv.handlePatchDeployments(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments", bytes.NewReader(body)), principal))
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			require.Len(t, store.patchApprovals, 1)
			var approval storage.PatchApproval
			for _, a := range store.patchApprovals {
				approval = a
			}
			require.Equal(t, content.Review.ExpiresAt, approval.ExpiresAt, "approval must expire no later than its exact plan")
			policy := patchPackagePolicyFromSummary(store.deployments[approval.DeploymentID].Summary)
			if scenario == "expired" {
				policy.OfflineContent.Review.ExpiresAt = now.Add(-time.Second)
			} else {
				policy.OfflineContent.Review.Changes[0].InstalledVersion = "1.2.1"
			}
			store.deployments[approval.DeploymentID].Summary["offline_content"] = policy.OfflineContent
			rec = httptest.NewRecorder()
			srv.handlePatchApprovalSubroutes(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/approvals/"+approval.ID.String()+"/approve", nil), principal))
			require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			require.Empty(t, store.jobs)
			require.Equal(t, storage.ApprovalStatusPending, store.patchApprovals[approval.ID].Status)
		})
	}
}
