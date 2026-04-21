package storage

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedClusterSecretsTestKey(t *testing.T, store *Store) {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	require.NoError(t, store.SetClusterSecretKey(key))
}

func TestClusterSecretUpsertInsertBumpsVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)
	seedClusterSecretsTestKey(t, store)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-secret-upsert")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID: tenant.ID, Name: "sec-upsert", Provider: "mock", DesiredSize: 1,
	})
	require.NoError(t, err)

	first, err := store.UpsertClusterSecret(ctx, UpsertClusterSecretParams{
		ClusterID: cluster.ID,
		Key:       "DB_PASSWORD",
		Value:     "super-secret-v1",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, first.Version)
	assert.Equal(t, "DB_PASSWORD", first.Key)
	assert.NotEmpty(t, first.ValueEncrypted)
	// Ciphertext must NOT contain the plaintext anywhere.
	assert.False(t, strings.Contains(string(first.ValueEncrypted), "super-secret-v1"),
		"ciphertext must not leak plaintext bytes")

	// Second write with same key must bump version to 2 and refresh the ciphertext.
	second, err := store.UpsertClusterSecret(ctx, UpsertClusterSecretParams{
		ClusterID: cluster.ID,
		Key:       "DB_PASSWORD",
		Value:     "super-secret-v2",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, second.Version)
	assert.Equal(t, first.ID, second.ID, "upsert must keep the same row id")
	assert.NotEqual(t, first.ValueEncrypted, second.ValueEncrypted, "rotated value must re-encrypt")

	// Decrypt round-trip should yield the latest plaintext.
	fetched, err := store.GetClusterSecretDecrypted(ctx, cluster.ID, "DB_PASSWORD")
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, "super-secret-v2", fetched.Value)
	assert.Equal(t, 2, fetched.Version)
}

func TestClusterSecretGetMissingReturnsNil(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)
	seedClusterSecretsTestKey(t, store)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-secret-missing")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID: tenant.ID, Name: "sec-missing", Provider: "mock", DesiredSize: 1,
	})
	require.NoError(t, err)

	got, err := store.GetClusterSecret(ctx, cluster.ID, "NONE")
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = store.GetClusterSecretDecrypted(ctx, cluster.ID, "NONE")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestClusterSecretListOrdersByKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)
	seedClusterSecretsTestKey(t, store)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-secret-list")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID: tenant.ID, Name: "sec-list", Provider: "mock", DesiredSize: 1,
	})
	require.NoError(t, err)

	seed := map[string]string{
		"BRAVO":   "two",
		"ALPHA":   "one",
		"CHARLIE": "three",
	}
	for k, v := range seed {
		_, err := store.UpsertClusterSecret(ctx, UpsertClusterSecretParams{
			ClusterID: cluster.ID, Key: k, Value: v,
		})
		require.NoError(t, err)
	}

	listed, err := store.ListClusterSecrets(ctx, cluster.ID)
	require.NoError(t, err)
	require.Len(t, listed, 3)
	assert.Equal(t, "ALPHA", listed[0].Key)
	assert.Equal(t, "BRAVO", listed[1].Key)
	assert.Equal(t, "CHARLIE", listed[2].Key)
	// List variant must NOT fill the plaintext Value field.
	for _, s := range listed {
		assert.Empty(t, s.Value, "list must leave plaintext blank until decrypted call")
	}

	decrypted, err := store.ListClusterSecretsDecrypted(ctx, cluster.ID)
	require.NoError(t, err)
	require.Len(t, decrypted, 3)
	valueByKey := map[string]string{}
	for _, s := range decrypted {
		valueByKey[s.Key] = s.Value
	}
	assert.Equal(t, seed, valueByKey)
}

func TestClusterSecretDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)
	seedClusterSecretsTestKey(t, store)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-secret-delete")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID: tenant.ID, Name: "sec-delete", Provider: "mock", DesiredSize: 1,
	})
	require.NoError(t, err)

	_, err = store.UpsertClusterSecret(ctx, UpsertClusterSecretParams{
		ClusterID: cluster.ID, Key: "TOKEN", Value: "tk",
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteClusterSecret(ctx, cluster.ID, "TOKEN"))

	got, err := store.GetClusterSecret(ctx, cluster.ID, "TOKEN")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Second delete should report no rows.
	err = store.DeleteClusterSecret(ctx, cluster.ID, "TOKEN")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestClusterSecretCascadeOnClusterDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)
	seedClusterSecretsTestKey(t, store)

	tenant := clustersTestTenant(t, ctx, store, "cluster-tenant-secret-cascade")
	cluster, err := store.CreateCluster(ctx, CreateClusterParams{
		TenantID: tenant.ID, Name: "sec-cascade", Provider: "mock", DesiredSize: 1,
	})
	require.NoError(t, err)

	_, err = store.UpsertClusterSecret(ctx, UpsertClusterSecretParams{
		ClusterID: cluster.ID, Key: "TOKEN", Value: "tk",
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteCluster(ctx, cluster.ID))

	// Cascade: row should be gone even without an explicit DeleteClusterSecret.
	// Using a raw query so we don't hit the NoRows translation in
	// GetClusterSecret's wrapper.
	var count int
	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_secrets WHERE cluster_id = $1`, cluster.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "cluster_secrets rows should cascade-delete with their cluster")
}

func TestClusterSecretValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupClusterStore(t, ctx)
	seedClusterSecretsTestKey(t, store)

	cases := []struct {
		name   string
		params UpsertClusterSecretParams
	}{
		{"missing cluster id", UpsertClusterSecretParams{Key: "K", Value: "v"}},
		{"missing key", UpsertClusterSecretParams{ClusterID: uuid.New()}},
		{"whitespace key", UpsertClusterSecretParams{ClusterID: uuid.New(), Key: "   "}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.UpsertClusterSecret(ctx, tc.params)
			assert.Error(t, err)
		})
	}
}

func TestClusterSecretKeyOverrideRoundTrip(t *testing.T) {
	t.Parallel()

	store := &Store{}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	require.NoError(t, store.SetClusterSecretKey(key))
	cipher, err := store.encryptClusterSecretValue([]byte("hello"))
	require.NoError(t, err)
	plain, err := store.decryptClusterSecretValue(cipher)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(plain))
}

func TestClusterSecretKeyOverrideRejectsShortKey(t *testing.T) {
	t.Parallel()
	store := &Store{}
	assert.Error(t, store.SetClusterSecretKey([]byte("too-short")))
}
