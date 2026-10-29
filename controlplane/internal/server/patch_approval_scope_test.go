package server

import (
	"context"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPatchApprovalPackageScope(t *testing.T) {
	tenant, node, deploymentID := uuid.New(), uuid.New(), uuid.New()
	store := newPatchTestStore(tenant, node)
	server := &Server{store: store}
	approval := &storage.PatchApproval{TenantID: tenant, NodeID: node, DeploymentID: deploymentID, PolicyHash: "bound-policy"}
	require.False(t, server.patchApprovalResponseWithScope(context.Background(), approval, nil).PackageScopeAvailable)
	deployment := &storage.PatchDeployment{ID: deploymentID, TenantID: tenant, Summary: map[string]any{"package_allowlist": []string{"zlib1g"}, "package_denylist": []string{"other"}}}
	store.deployments[deploymentID] = deployment
	response := server.patchApprovalResponseWithScope(context.Background(), approval, nil)
	require.True(t, response.PackageScopeAvailable)
	require.Equal(t, []string{"zlib1g"}, response.PackageAllowlist)
	require.Equal(t, []string{"other"}, response.PackageDenylist)
	deployment.Summary = map[string]any{}
	response = server.patchApprovalResponseWithScope(context.Background(), approval, nil)
	require.True(t, response.PackageScopeAvailable)
	require.Empty(t, response.PackageAllowlist)
	approval.PolicyHash = ""
	require.False(t, server.patchApprovalResponseWithScope(context.Background(), approval, nil).PackageScopeAvailable)
	approval.PolicyHash = "bound-policy"
	deployment.TenantID = uuid.New()
	require.False(t, server.patchApprovalResponseWithScope(context.Background(), approval, nil).PackageScopeAvailable)
}
