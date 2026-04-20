package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedRolloutForWaves creates a tenant, cluster, template version, and rollout
// so wave tests have valid FK targets.
func seedRolloutForWaves(t *testing.T, ctx context.Context, store *Store, prefix string) *ClusterRollout {
	t.Helper()

	tenant := clustersTestTenant(t, ctx, store, prefix+"-tenant")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID:    tenant.ID,
		Name:        prefix + "-cluster",
		Provider:    "aws",
		DesiredSize: 6,
	})
	require.NoError(t, err)

	tpl, err := store.CreateProvisioningTemplate(ctx, &ProvisioningTemplate{
		Name:     prefix + "-template",
		Provider: "aws",
	})
	require.NoError(t, err)
	version, err := store.CreateProvisioningTemplateVersion(ctx, CreateTemplateVersionParams{
		TemplateID: tpl.ID,
		Body:       prefix + " body",
	})
	require.NoError(t, err)

	rollout, err := store.CreateClusterRollout(ctx, CreateClusterRolloutParams{
		ClusterID:         cluster.ID,
		TemplateVersionID: version.ID,
		WaveSize:          2,
		WaveStrategy:      "rolling",
		HealthGate: map[string]any{
			"type":    "heartbeat",
			"grace":   "5m",
			"timeout": "10m",
		},
		State:       "pending",
		CurrentWave: 0,
	})
	require.NoError(t, err)
	return rollout
}

func TestCreateClusterRolloutWave(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	rollout := seedRolloutForWaves(t, ctx, store, "wave-create")

	members := []uuid.UUID{uuid.New(), uuid.New()}
	gate := map[string]any{"observed": "ok", "count": float64(2)}
	started := time.Now().UTC().Truncate(time.Millisecond)

	wave, err := store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
		RolloutID:  rollout.ID,
		WaveNumber: 0,
		MemberIDs:  members,
		State:      ClusterRolloutWaveStateRunning,
		StartedAt:  started,
		GateResult: gate,
	})
	require.NoError(t, err)
	require.NotNil(t, wave)
	assert.Equal(t, rollout.ID, wave.RolloutID)
	assert.Equal(t, 0, wave.WaveNumber)
	assert.Equal(t, ClusterRolloutWaveStateRunning, wave.State)
	assert.ElementsMatch(t, members, wave.MemberIDs)
	assert.Equal(t, gate, wave.GateResult)
	assert.False(t, wave.StartedAt.IsZero())
	assert.Nil(t, wave.CompletedAt)
}

func TestCreateClusterRolloutWaveDefaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	rollout := seedRolloutForWaves(t, ctx, store, "wave-defaults")
	wave, err := store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
		RolloutID:  rollout.ID,
		WaveNumber: 0,
		MemberIDs:  []uuid.UUID{uuid.New()},
	})
	require.NoError(t, err)
	assert.Equal(t, ClusterRolloutWaveStateRunning, wave.State)
	assert.Nil(t, wave.GateResult)
	assert.False(t, wave.StartedAt.IsZero())
}

func TestClusterRolloutWaveUniquePerRollout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	rollout := seedRolloutForWaves(t, ctx, store, "wave-unique")

	_, err := store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
		RolloutID:  rollout.ID,
		WaveNumber: 0,
		MemberIDs:  []uuid.UUID{uuid.New()},
	})
	require.NoError(t, err)

	_, err = store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
		RolloutID:  rollout.ID,
		WaveNumber: 0,
		MemberIDs:  []uuid.UUID{uuid.New()},
	})
	require.Error(t, err, "unique constraint on (rollout_id, wave_number) should fire")
}

func TestListClusterRolloutWavesOrdered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	rollout := seedRolloutForWaves(t, ctx, store, "wave-list")

	// Insert out of order to confirm the SELECT sorts ascending.
	for _, n := range []int{2, 0, 1} {
		_, err := store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
			RolloutID:  rollout.ID,
			WaveNumber: n,
			MemberIDs:  []uuid.UUID{uuid.New()},
		})
		require.NoError(t, err)
	}

	waves, err := store.ListClusterRolloutWaves(ctx, rollout.ID)
	require.NoError(t, err)
	require.Len(t, waves, 3)
	for i, wv := range waves {
		assert.Equal(t, i, wv.WaveNumber)
	}
}

func TestGetClusterRolloutWaveByNumber(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	rollout := seedRolloutForWaves(t, ctx, store, "wave-getbynum")
	members := []uuid.UUID{uuid.New(), uuid.New()}
	_, err := store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
		RolloutID:  rollout.ID,
		WaveNumber: 1,
		MemberIDs:  members,
	})
	require.NoError(t, err)

	got, err := store.GetClusterRolloutWaveByNumber(ctx, rollout.ID, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, got.WaveNumber)
	assert.ElementsMatch(t, members, got.MemberIDs)

	missing, err := store.GetClusterRolloutWaveByNumber(ctx, rollout.ID, 99)
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestUpdateClusterRolloutWaveState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	rollout := seedRolloutForWaves(t, ctx, store, "wave-update")
	wave, err := store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
		RolloutID:  rollout.ID,
		WaveNumber: 0,
		MemberIDs:  []uuid.UUID{uuid.New(), uuid.New()},
		State:      ClusterRolloutWaveStateRunning,
	})
	require.NoError(t, err)

	completed := time.Now().UTC()
	healthy := ClusterRolloutWaveStateHealthy
	gate := map[string]any{"checked": float64(2), "passed": true}
	updated, err := store.UpdateClusterRolloutWave(ctx, wave.ID, UpdateClusterRolloutWaveParams{
		State:       &healthy,
		GateResult:  &gate,
		CompletedAt: &completed,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, ClusterRolloutWaveStateHealthy, updated.State)
	assert.Equal(t, gate, updated.GateResult)
	require.NotNil(t, updated.CompletedAt)
	assert.WithinDuration(t, completed, *updated.CompletedAt, time.Second)
}

func TestClusterRolloutWaveCascadeDeleteFromRollout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	rollout := seedRolloutForWaves(t, ctx, store, "wave-cascade")
	_, err := store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
		RolloutID:  rollout.ID,
		WaveNumber: 0,
		MemberIDs:  []uuid.UUID{uuid.New()},
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteClusterRollout(ctx, rollout.ID))

	waves, err := store.ListClusterRolloutWaves(ctx, rollout.ID)
	require.NoError(t, err)
	assert.Empty(t, waves, "rollout delete should cascade to waves")
}

func TestClusterRolloutWaveCascadeDeleteFromCluster(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)

	rollout := seedRolloutForWaves(t, ctx, store, "wave-cluster-cascade")
	_, err := store.CreateClusterRolloutWave(ctx, CreateClusterRolloutWaveParams{
		RolloutID:  rollout.ID,
		WaveNumber: 0,
		MemberIDs:  []uuid.UUID{uuid.New()},
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteCluster(ctx, rollout.ClusterID))

	waves, err := store.ListClusterRolloutWaves(ctx, rollout.ID)
	require.NoError(t, err)
	assert.Empty(t, waves, "cluster delete should cascade through rollout to waves")
}
