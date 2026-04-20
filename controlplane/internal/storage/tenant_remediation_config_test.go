package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTenantRemediationConfig_DefaultFallback(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenantID := uuid.New()
	cfg, err := store.GetTenantRemediationConfig(ctx, tenantID)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "high", cfg.MinApprovalSeverity)
	require.True(t, cfg.CriticalOverride)
	require.Equal(t, 15, cfg.CircuitBreakerWindowMin)
	require.Equal(t, 30, cfg.CircuitBreakerFailPct)
	require.Equal(t, 5, cfg.CircuitBreakerMinSamples)
	require.Empty(t, cfg.ChangeWindows)
}

func TestTenantRemediationConfig_UpsertRoundTrips(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "cfg-tenant-" + uuid.NewString()[:6]})
	require.NoError(t, err)

	in := TenantRemediationConfig{
		TenantID:            tenant.ID,
		MinApprovalSeverity: "medium",
		ChangeWindows: []ChangeWindow{
			{Days: []int{1, 2, 3, 4, 5}, StartHour: 2, EndHour: 6, Timezone: "UTC", Label: "weekday maintenance"},
		},
		CriticalOverride:         false,
		CircuitBreakerWindowMin:  30,
		CircuitBreakerFailPct:    50,
		CircuitBreakerMinSamples: 10,
	}

	saved, err := store.UpsertTenantRemediationConfig(ctx, in)
	require.NoError(t, err)
	require.NotNil(t, saved)
	require.Equal(t, "medium", saved.MinApprovalSeverity)
	require.False(t, saved.CriticalOverride)
	require.Equal(t, 1, len(saved.ChangeWindows))
	require.Equal(t, 2, saved.ChangeWindows[0].StartHour)

	reloaded, err := store.GetTenantRemediationConfig(ctx, tenant.ID)
	require.NoError(t, err)
	require.Equal(t, "medium", reloaded.MinApprovalSeverity)
	require.Equal(t, 30, reloaded.CircuitBreakerWindowMin)

	// Update again — should overwrite in place.
	in2 := in
	in2.MinApprovalSeverity = "critical"
	in2.CriticalOverride = true
	saved2, err := store.UpsertTenantRemediationConfig(ctx, in2)
	require.NoError(t, err)
	require.Equal(t, "critical", saved2.MinApprovalSeverity)
	require.True(t, saved2.CriticalOverride)
}

func TestTenantRemediationConfig_InvalidSeverity(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "bad-sev-" + uuid.NewString()[:6]})
	require.NoError(t, err)

	_, err = store.UpsertTenantRemediationConfig(ctx, TenantRemediationConfig{
		TenantID:            tenant.ID,
		MinApprovalSeverity: "super-urgent",
	})
	require.Error(t, err)
}

func TestIsInsideChangeWindow_NoWindowsOpen(t *testing.T) {
	require.True(t, IsInsideChangeWindow(nil, time.Now()))
	require.True(t, IsInsideChangeWindow([]ChangeWindow{}, time.Now()))
}

func TestIsInsideChangeWindow_HourMatching(t *testing.T) {
	utc := time.UTC
	windows := []ChangeWindow{{StartHour: 2, EndHour: 6}}

	inside := time.Date(2026, 1, 1, 3, 30, 0, 0, utc)
	outside := time.Date(2026, 1, 1, 10, 0, 0, 0, utc)
	require.True(t, IsInsideChangeWindow(windows, inside))
	require.False(t, IsInsideChangeWindow(windows, outside))
}

func TestIsInsideChangeWindow_Wraparound(t *testing.T) {
	utc := time.UTC
	windows := []ChangeWindow{{StartHour: 22, EndHour: 2}} // 22:00 → 02:00 wraps midnight.

	require.True(t, IsInsideChangeWindow(windows, time.Date(2026, 1, 1, 23, 0, 0, 0, utc)))
	require.True(t, IsInsideChangeWindow(windows, time.Date(2026, 1, 1, 1, 0, 0, 0, utc)))
	require.False(t, IsInsideChangeWindow(windows, time.Date(2026, 1, 1, 15, 0, 0, 0, utc)))
}

func TestNextChangeWindowStart(t *testing.T) {
	utc := time.UTC
	windows := []ChangeWindow{{StartHour: 2, EndHour: 6}}

	now := time.Date(2026, 1, 1, 10, 0, 0, 0, utc)
	next := NextChangeWindowStart(windows, now)
	require.False(t, next.IsZero())
	require.True(t, next.After(now), "next open must be in the future")
	require.Equal(t, 2, next.Hour())
}

func TestSeverityAtLeast(t *testing.T) {
	require.True(t, SeverityAtLeast("critical", "high"))
	require.True(t, SeverityAtLeast("high", "high"))
	require.False(t, SeverityAtLeast("medium", "high"))
	require.False(t, SeverityAtLeast("", "high"))
}
