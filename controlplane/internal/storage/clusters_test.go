package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/stretchr/testify/assert"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
)

// setupClusterStore spins up a postgres container and applies the FULL migration
// pipeline (not just the hand-picked subset that `setupPostgresStore` uses).
// Cluster tests need tables introduced by 0025/0026/0027 which aren't reachable
// via the init-scripts path.
func setupClusterStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Mirror the existing storage tests' guard: skip if no docker. The
	// DockerImageAuth call doubles as both a liveness probe and a credentials
	// probe; since public images (postgres:16-alpine) are pullable without
	// credentials, we treat "credentials not found" as a skip signal too.
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pg.Terminate(ctx))
	})

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	logger := zap.NewNop()
	store, err := New(logger, config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	applyCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	require.NoError(t, migrate.Apply(applyCtx, store.DB()))

	return store
}

func clustersTestTenant(t *testing.T, ctx context.Context, store *Store, name string) *Tenant {
	t.Helper()
	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: name})
	require.NoError(t, err)
	return tenant
}

func clustersTestNode(t *testing.T, ctx context.Context, store *Store, tenantID uuid.UUID, hostname string) *Node {
	t.Helper()
	node, err := store.CreateNode(ctx, &Node{
		ID:       uuid.New(),
		TenantID: tenantID,
		Hostname: hostname,
	})
	require.NoError(t, err)
	return node
}

func TestCreateCluster(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-create")

	rolePlan := map[string]any{
		"roles": []any{
			map[string]any{"name": "control-plane", "count": float64(3)},
			map[string]any{"name": "worker", "count": float64(5)},
		},
	}
	labels := map[string]any{"env": "prod", "region": "eu-west-1"}

	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:              tenant.ID,
		Name:                  "prod-k8s",
		Provider:              "aws",
		DesiredSize:           8,
		RolePlan:              rolePlan,
		Labels:                labels,
		FailureDomainStrategy: "spread",
		State:                 "pending",
	})
	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.NotEqual(t, uuid.Nil, cluster.ID)
	assert.Equal(t, tenant.ID, cluster.TenantID)
	assert.Equal(t, "prod-k8s", cluster.Name)
	assert.Equal(t, "aws", cluster.Provider)
	assert.Equal(t, 8, cluster.DesiredSize)
	assert.Equal(t, "spread", cluster.FailureDomainStrategy)
	assert.Equal(t, "pending", cluster.State)
	assert.False(t, cluster.TemplateID.Valid)
	assert.Equal(t, labels, cluster.Labels)
	assert.Equal(t, rolePlan, cluster.RolePlan)
	assert.False(t, cluster.CreatedAt.IsZero())
	assert.False(t, cluster.UpdatedAt.IsZero())
}

func TestCreateClusterDefaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-defaults")

	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "defaults-cluster",
		Provider:    "libvirt",
		DesiredSize: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, "spread", cluster.FailureDomainStrategy)
	assert.Equal(t, "pending", cluster.State)
	assert.Equal(t, map[string]any{}, cluster.RolePlan)
	assert.Equal(t, map[string]any{}, cluster.Labels)
}

func TestCreateClusterValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-validation")

	cases := []struct {
		name   string
		params CreateClusterParams
	}{
		{"missing tenant", CreateClusterParams{Name: "n", Provider: "p", DesiredSize: 1}},
		{"empty name", CreateClusterParams{TenantID: tenant.ID, Name: "  ", Provider: "p", DesiredSize: 1}},
		{"empty provider", CreateClusterParams{TenantID: tenant.ID, Name: "c", Provider: "", DesiredSize: 1}},
		{"negative size", CreateClusterParams{TenantID: tenant.ID, Name: "c", Provider: "p", DesiredSize: -1}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.CreateCluster(ctx, tc.params)
			assert.Error(t, err)
		})
	}
}

func TestGetClusterByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-get-id")

	created, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "get-by-id",
		Provider:    "aws",
		DesiredSize: 3,
		Labels:      map[string]any{"env": "dev"},
	})
	require.NoError(t, err)

	got, err := store.GetClusterByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "get-by-id", got.Name)
	assert.Equal(t, map[string]any{"env": "dev"}, got.Labels)

	missing, err := store.GetClusterByID(ctx, uuid.New())
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestGetClusterByName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-get-name")

	created, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "named-cluster",
		Provider:    "azure",
		DesiredSize: 2,
	})
	require.NoError(t, err)

	got, err := store.GetClusterByName(ctx, tenant.ID, "named-cluster")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, created.ID, got.ID)

	missing, err := store.GetClusterByName(ctx, tenant.ID, "no-such-cluster")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestClusterUniqueTenantName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenantA := clustersTestTenant(t, ctx, store, "cluster-tenant-unique-a")
	tenantB := clustersTestTenant(t, ctx, store, "cluster-tenant-unique-b")

	_, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenantA.ID,
		Name:        "shared-name",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.NoError(t, err)

	// Same tenant, same name: must fail.
	_, err = store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenantA.ID,
		Name:        "shared-name",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.Error(t, err)

	// Different tenant, same name: must succeed.
	_, err = store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenantB.ID,
		Name:        "shared-name",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.NoError(t, err)
}

func TestListClusters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenantA := clustersTestTenant(t, ctx, store, "cluster-tenant-list-a")
	tenantB := clustersTestTenant(t, ctx, store, "cluster-tenant-list-b")

	for i := 0; i < 3; i++ {
		_, err := store.CreateCluster(ctx, CreateClusterParams{
			TenantID:    tenantA.ID,
			Name:        fmt.Sprintf("list-a-%d", i),
			Provider:    "aws",
			DesiredSize: 1,
		})
		require.NoError(t, err)
	}
	_, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenantB.ID,
		Name:        "list-b-0",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.NoError(t, err)

	clustersA, total, err := store.ListClusters(ctx, tenantA.ID, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, clustersA, 3)
	for _, cluster := range clustersA {
		assert.Equal(t, tenantA.ID, cluster.TenantID)
	}

	clustersB, totalB, err := store.ListClusters(ctx, tenantB.ID, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, totalB)
	assert.Len(t, clustersB, 1)
}

func TestUpdateCluster(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-update")

	created, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "update-me",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.NoError(t, err)

	newSize := 7
	newState := "running"
	newLabels := map[string]any{"updated": true}
	newRolePlan := map[string]any{"roles": []any{map[string]any{"name": "worker", "count": float64(7)}}}

	updated, err := store.UpdateCluster(ctx, created.ID, UpdateClusterParams{
		DesiredSize: &newSize,
		State:       &newState,
		Labels:      &newLabels,
		RolePlan:    &newRolePlan,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, 7, updated.DesiredSize)
	assert.Equal(t, "running", updated.State)
	assert.Equal(t, newLabels, updated.Labels)
	assert.Equal(t, newRolePlan, updated.RolePlan)
	assert.True(t, updated.UpdatedAt.After(created.UpdatedAt) || updated.UpdatedAt.Equal(created.UpdatedAt))
}

func TestUpdateClusterNoFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-update-noop")
	created, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "noop-update",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.NoError(t, err)

	got, err := store.UpdateCluster(ctx, created.ID, UpdateClusterParams{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, created.ID, got.ID)
}

func TestDeleteCluster(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-delete")
	created, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "delete-me",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteCluster(ctx, created.ID))

	got, err := store.GetClusterByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, got)

	err = store.DeleteCluster(ctx, created.ID)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestCountClustersByTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-count")

	count, err := store.CountClustersByTenant(ctx, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	for i := 0; i < 4; i++ {
		_, err := store.CreateCluster(ctx, CreateClusterParams{
			TenantID:    tenant.ID,
			Name:        fmt.Sprintf("count-%d", i),
			Provider:    "aws",
			DesiredSize: 1,
		})
		require.NoError(t, err)
	}

	count, err = store.CountClustersByTenant(ctx, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, count)
}

func TestClusterMembersCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-members")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "members-cluster",
		Provider:    "aws",
		DesiredSize: 3,
	})
	require.NoError(t, err)

	node1 := clustersTestNode(t, ctx, store, tenant.ID, "host-1")
	node2 := clustersTestNode(t, ctx, store, tenant.ID, "host-2")
	node3 := clustersTestNode(t, ctx, store, tenant.ID, "host-3")

	m1, err := store.AddClusterMember(ctx, cluster.ID, node1.ID, "control-plane", 0)
	require.NoError(t, err)
	assert.Equal(t, cluster.ID, m1.ClusterID)
	assert.Equal(t, node1.ID, m1.NodeID)
	assert.Equal(t, "control-plane", m1.Role)
	assert.Equal(t, 0, m1.Position)

	_, err = store.AddClusterMember(ctx, cluster.ID, node2.ID, "control-plane", 1)
	require.NoError(t, err)
	_, err = store.AddClusterMember(ctx, cluster.ID, node3.ID, "worker", 0)
	require.NoError(t, err)

	members, err := store.ListClusterMembers(ctx, cluster.ID)
	require.NoError(t, err)
	assert.Len(t, members, 3)

	// Unique (cluster_id, role, position) must be enforced.
	_, err = store.AddClusterMember(ctx, cluster.ID, node3.ID, "control-plane", 0)
	require.Error(t, err)

	// Primary key prevents same (cluster_id, node_id) twice.
	_, err = store.AddClusterMember(ctx, cluster.ID, node1.ID, "worker", 99)
	require.Error(t, err)

	require.NoError(t, store.RemoveClusterMember(ctx, cluster.ID, node1.ID))
	members, err = store.ListClusterMembers(ctx, cluster.ID)
	require.NoError(t, err)
	assert.Len(t, members, 2)

	err = store.RemoveClusterMember(ctx, cluster.ID, node1.ID)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestClusterCascadeDeleteRemovesMembersAndRollouts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-cascade")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "cascade-cluster",
		Provider:    "aws",
		DesiredSize: 2,
	})
	require.NoError(t, err)

	// Seed a template + version so rollout FK is satisfied.
	tpl, err := store.CreateProvisioningTemplate(ctx, &ProvisioningTemplate{
		Name:     "cascade-template",
		Provider: "aws",
	})
	require.NoError(t, err)
	version, err := store.CreateProvisioningTemplateVersion(ctx, CreateTemplateVersionParams{
		TemplateID: tpl.ID,
		Body:       "cascade body",
	})
	require.NoError(t, err)

	node := clustersTestNode(t, ctx, store, tenant.ID, "cascade-host")
	_, err = store.AddClusterMember(ctx, cluster.ID, node.ID, "worker", 0)
	require.NoError(t, err)

	rollout, err := store.CreateClusterRollout(ctx, CreateClusterRolloutParams{
		ClusterID:         cluster.ID,
		TemplateVersionID: version.ID,
		WaveSize:          2,
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteCluster(ctx, cluster.ID))

	members, err := store.ListClusterMembers(ctx, cluster.ID)
	require.NoError(t, err)
	assert.Empty(t, members)

	got, err := store.GetClusterRolloutByID(ctx, rollout.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "rollout should cascade-delete with cluster")
}

func TestTenantCascadeDeletesClusters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-cascade-tenant")
	created, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "tenant-cascade",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteTenant(ctx, tenant.ID))

	got, err := store.GetClusterByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "cluster should cascade-delete with tenant")
}

func TestClusterRolePlanJSONBRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-roleplan")

	rolePlan := map[string]any{
		"roles": []any{
			map[string]any{
				"name":                "control-plane",
				"count":               float64(3),
				"template_version_id": "11111111-2222-3333-4444-555555555555",
			},
			map[string]any{
				"name":                "worker",
				"count":               float64(5),
				"template_version_id": "99999999-8888-7777-6666-555555555555",
			},
		},
		"metadata": map[string]any{
			"owner": "platform-team",
			"tier":  float64(1),
		},
	}

	created, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "roleplan-cluster",
		Provider:    "aws",
		DesiredSize: 8,
		RolePlan:    rolePlan,
	})
	require.NoError(t, err)

	got, err := store.GetClusterByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, rolePlan, got.RolePlan)
}

func TestCreateClusterRollout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-rollout")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "rollout-cluster",
		Provider:    "aws",
		DesiredSize: 3,
	})
	require.NoError(t, err)

	tpl, err := store.CreateProvisioningTemplate(ctx, &ProvisioningTemplate{
		Name:     "rollout-template",
		Provider: "aws",
	})
	require.NoError(t, err)
	version, err := store.CreateProvisioningTemplateVersion(ctx, CreateTemplateVersionParams{
		TemplateID: tpl.ID,
		Body:       "rollout body",
	})
	require.NoError(t, err)

	healthGate := map[string]any{
		"type":    "heartbeat",
		"timeout": "5m",
	}

	rollout, err := store.CreateClusterRollout(ctx, CreateClusterRolloutParams{
		ClusterID:         cluster.ID,
		TemplateVersionID: version.ID,
		WaveSize:          2,
		WaveStrategy:      "canary",
		HealthGate:        healthGate,
		State:             "pending",
		CurrentWave:       0,
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, rollout.ID)
	assert.Equal(t, cluster.ID, rollout.ClusterID)
	assert.Equal(t, version.ID, rollout.TemplateVersionID)
	assert.Equal(t, 2, rollout.WaveSize)
	assert.Equal(t, "canary", rollout.WaveStrategy)
	assert.Equal(t, healthGate, rollout.HealthGate)
	assert.Equal(t, "pending", rollout.State)
	assert.Equal(t, 0, rollout.CurrentWave)
}

func TestClusterRolloutCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-rollout-crud")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "rollout-crud",
		Provider:    "aws",
		DesiredSize: 2,
	})
	require.NoError(t, err)

	tpl, err := store.CreateProvisioningTemplate(ctx, &ProvisioningTemplate{
		Name:     "rollout-crud-template",
		Provider: "aws",
	})
	require.NoError(t, err)
	version, err := store.CreateProvisioningTemplateVersion(ctx, CreateTemplateVersionParams{
		TemplateID: tpl.ID,
		Body:       "body",
	})
	require.NoError(t, err)

	rollout, err := store.CreateClusterRollout(ctx, CreateClusterRolloutParams{
		ClusterID:         cluster.ID,
		TemplateVersionID: version.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, rollout.WaveSize)
	assert.Equal(t, "rolling", rollout.WaveStrategy)
	assert.Equal(t, "pending", rollout.State)

	fetched, err := store.GetClusterRolloutByID(ctx, rollout.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, rollout.ID, fetched.ID)

	listed, total, err := store.ListClusterRollouts(ctx, cluster.ID, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, listed, 1)

	newState := "running"
	newWave := 2
	newStrategy := "canary"
	newGate := map[string]any{"type": "http", "path": "/healthz"}
	updated, err := store.UpdateClusterRollout(ctx, rollout.ID, UpdateClusterRolloutParams{
		State:        &newState,
		CurrentWave:  &newWave,
		WaveStrategy: &newStrategy,
		HealthGate:   &newGate,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "running", updated.State)
	assert.Equal(t, 2, updated.CurrentWave)
	assert.Equal(t, "canary", updated.WaveStrategy)
	assert.Equal(t, newGate, updated.HealthGate)

	require.NoError(t, store.DeleteClusterRollout(ctx, rollout.ID))

	missing, err := store.GetClusterRolloutByID(ctx, rollout.ID)
	require.NoError(t, err)
	assert.Nil(t, missing)

	err = store.DeleteClusterRollout(ctx, rollout.ID)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestClusterWithTemplateID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-template")
	tpl, err := store.CreateProvisioningTemplate(ctx, &ProvisioningTemplate{
		Name:     "cluster-template",
		Provider: "aws",
	})
	require.NoError(t, err)

	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "templated-cluster",
		Provider:    "aws",
		DesiredSize: 2,
		TemplateID:  &tpl.ID,
	})
	require.NoError(t, err)
	require.True(t, cluster.TemplateID.Valid)
	assert.Equal(t, tpl.ID, cluster.TemplateID.UUID)

	// Clear template_id via ClearTemplateID.
	updated, err := store.UpdateCluster(ctx, cluster.ID, UpdateClusterParams{
		ClearTemplateID: true,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.TemplateID.Valid)
}

func TestClusterRolloutValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	_, err := store.CreateClusterRollout(ctx, CreateClusterRolloutParams{})
	assert.Error(t, err, "missing cluster id must error")

	_, err = store.CreateClusterRollout(ctx, CreateClusterRolloutParams{
		ClusterID: uuid.New(),
	})
	assert.Error(t, err, "missing template version id must error")

	// Negative wave size.
	_, err = store.CreateClusterRollout(ctx, CreateClusterRolloutParams{
		ClusterID:         uuid.New(),
		TemplateVersionID: uuid.New(),
		WaveSize:          -1,
	})
	assert.Error(t, err)
}

func TestUpdateClusterValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-update-validation")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        "uv-cluster",
		Provider:    "aws",
		DesiredSize: 1,
	})
	require.NoError(t, err)

	emptyName := "   "
	_, err = store.UpdateCluster(ctx, cluster.ID, UpdateClusterParams{Name: &emptyName})
	require.Error(t, err)

	negSize := -5
	_, err = store.UpdateCluster(ctx, cluster.ID, UpdateClusterParams{DesiredSize: &negSize})
	require.Error(t, err)
}

// sanity: ensure that the rowScanner-based scan path propagates ErrNoRows correctly
// through Get* accessors (exercised by nil returns in earlier tests).
var _ = errors.Is
