package storage

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProxyRegistrationDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	proxy, err := s.CreateSquidProxy(ctx, SquidProxy{TenantID: tenant.ID, Host: "proxy.example.invalid", Port: 3128})
	require.NoError(t, err)
	require.Equal(t, "registered", proxy.Status)
	require.Nil(t, proxy.LastValidatedAt)
	saved, err := s.GetSquidProxy(ctx, proxy.ID)
	require.NoError(t, err)
	require.Equal(t, "registered", saved.Status)
	require.Nil(t, saved.LastValidatedAt)
	require.Empty(t, saved.Whitelist)
}
