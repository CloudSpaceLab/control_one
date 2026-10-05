package networkdevice

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSourceAndCollectorFreshnessAreIndependent(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-time.Minute)
	old := now.Add(-time.Hour)
	require.Equal(t, "ready", EffectiveSourceState("ready", &recent, 300, now))
	require.Equal(t, "stale", CollectorFreshness("healthy", &old, now))
	require.Equal(t, "stale", EffectiveSourceState("ready", &old, 300, now))
	require.Equal(t, "healthy", CollectorFreshness("healthy", &recent, now))
	for _, state := range []string{"not_configured", "auth_failed", "unreachable", "policy_blocked", "unsupported"} {
		require.Equal(t, state, EffectiveSourceState(state, nil, 300, now))
	}
	r := SourceReport{State: "ready", ObservedAt: now}
	require.Error(t, r.Validate(now))
	r.LastContactAt = &recent
	require.NoError(t, r.Validate(now))
	r.QueueDepth = -1
	require.Error(t, r.Validate(now))
}
