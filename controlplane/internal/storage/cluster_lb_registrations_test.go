package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterLBRegistrationLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-lb-lifecycle")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID: tenant.ID, Name: "lb-lifecycle", Provider: "aws", DesiredSize: 1,
	})
	require.NoError(t, err)
	node := clustersTestNode(t, ctx, store, tenant.ID, "lb-host")

	reg, err := store.CreateClusterLBRegistration(ctx, CreateClusterLBRegistrationParams{
		ClusterID:    cluster.ID,
		NodeID:       node.ID,
		Provider:     "aws",
		LBIdentifier: "arn:aws:elbv2:tg/foo",
	})
	require.NoError(t, err)
	assert.Equal(t, cluster.ID, reg.ClusterID)
	assert.Equal(t, node.ID, reg.NodeID)
	assert.Equal(t, "aws", reg.Provider)
	assert.Equal(t, "arn:aws:elbv2:tg/foo", reg.LBIdentifier)
	assert.False(t, reg.RegisteredAt.IsZero())
	assert.Nil(t, reg.DeregisteredAt)

	// Re-inserting the same (cluster, node, lb) row must upsert, not error.
	reg2, err := store.CreateClusterLBRegistration(ctx, CreateClusterLBRegistrationParams{
		ClusterID:    cluster.ID,
		NodeID:       node.ID,
		Provider:     "aws",
		LBIdentifier: "arn:aws:elbv2:tg/foo",
	})
	require.NoError(t, err)
	assert.Nil(t, reg2.DeregisteredAt, "upsert should clear deregistered_at")

	// Mark deregistered.
	require.NoError(t, store.MarkClusterLBRegistrationDeregistered(ctx, cluster.ID, node.ID, "arn:aws:elbv2:tg/foo"))

	rows, err := store.ListClusterLBRegistrationsForNode(ctx, node.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].DeregisteredAt)

	// Mark-deregister for a row that doesn't exist should return ErrNoRows.
	err = store.MarkClusterLBRegistrationDeregistered(ctx, cluster.ID, uuid.New(), "missing")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestClusterLBRegistrationListByCluster(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-lb-list")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID: tenant.ID, Name: "lb-list", Provider: "aws", DesiredSize: 3,
	})
	require.NoError(t, err)

	nodes := []*Node{}
	for i := 0; i < 3; i++ {
		nodes = append(nodes, clustersTestNode(t, ctx, store, tenant.ID, "lb-list-host-"+string(rune('a'+i))))
	}
	for _, n := range nodes {
		_, err := store.CreateClusterLBRegistration(ctx, CreateClusterLBRegistrationParams{
			ClusterID:    cluster.ID,
			NodeID:       n.ID,
			Provider:     "aws",
			LBIdentifier: "arn:aws:elbv2:tg/list",
		})
		require.NoError(t, err)
	}

	rows, err := store.ListClusterLBRegistrationsForCluster(ctx, cluster.ID)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}

func TestClusterLBRegistrationCascadeOnClusterDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-lb-cascade")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID: tenant.ID, Name: "lb-cascade", Provider: "aws", DesiredSize: 1,
	})
	require.NoError(t, err)
	node := clustersTestNode(t, ctx, store, tenant.ID, "lb-cascade-host")
	_, err = store.CreateClusterLBRegistration(ctx, CreateClusterLBRegistrationParams{
		ClusterID:    cluster.ID,
		NodeID:       node.ID,
		Provider:     "aws",
		LBIdentifier: "arn:aws:elbv2:tg/cascade",
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteCluster(ctx, cluster.ID))

	rows, err := store.ListClusterLBRegistrationsForNode(ctx, node.ID)
	require.NoError(t, err)
	assert.Empty(t, rows, "lb registrations should cascade-delete with cluster")
}

func TestClusterLBRegistrationValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	cases := []struct {
		name   string
		params CreateClusterLBRegistrationParams
	}{
		{"missing cluster", CreateClusterLBRegistrationParams{NodeID: uuid.New(), Provider: "aws", LBIdentifier: "a"}},
		{"missing node", CreateClusterLBRegistrationParams{ClusterID: uuid.New(), Provider: "aws", LBIdentifier: "a"}},
		{"missing provider", CreateClusterLBRegistrationParams{ClusterID: uuid.New(), NodeID: uuid.New(), LBIdentifier: "a"}},
		{"missing identifier", CreateClusterLBRegistrationParams{ClusterID: uuid.New(), NodeID: uuid.New(), Provider: "aws"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.CreateClusterLBRegistration(ctx, tc.params)
			assert.Error(t, err)
		})
	}
}
