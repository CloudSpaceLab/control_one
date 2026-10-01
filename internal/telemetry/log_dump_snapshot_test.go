package telemetry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSnapshotLogSpoolIsNonDestructive(t *testing.T) {
	t.Parallel()
	svc := New(nil, zap.NewNop(), nil)
	svc.WithDurability(DurabilityOptions{
		LogSpoolDir:      t.TempDir(),
		LogSpoolMaxBytes: 1 << 20,
	})
	_, err := svc.logSpool.AppendBytes([]byte("{\"entries\":[{\"message\":\"one\"}]}\n"))
	require.NoError(t, err)

	before, err := svc.logSpool.Records()
	require.NoError(t, err)
	require.Len(t, before, 1)

	now := time.Now().UTC()
	snapshot, err := svc.SnapshotLogSpool(now.Add(-time.Minute), now.Add(time.Minute), 1<<20)
	require.NoError(t, err)
	require.True(t, snapshot.Available)
	require.False(t, snapshot.Truncated)
	require.Equal(t, 1, snapshot.Records)
	require.Contains(t, string(snapshot.Data), "\"message\":\"one\"")

	after, err := svc.logSpool.Records()
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestSnapshotLogSpoolAvailabilityAndBounds(t *testing.T) {
	t.Parallel()
	disabled := New(nil, zap.NewNop(), nil)
	now := time.Now().UTC()
	snapshot, err := disabled.SnapshotLogSpool(now.Add(-time.Minute), now.Add(time.Minute), 1024)
	require.NoError(t, err)
	require.False(t, snapshot.Available)
	require.NotEmpty(t, snapshot.Reason)

	empty := New(nil, zap.NewNop(), nil)
	empty.WithDurability(DurabilityOptions{LogSpoolDir: t.TempDir(), LogSpoolMaxBytes: 1 << 20})
	snapshot, err = empty.SnapshotLogSpool(now.Add(-time.Minute), now.Add(time.Minute), 1024)
	require.NoError(t, err)
	require.True(t, snapshot.Available)
	require.Empty(t, snapshot.Data)
	require.Zero(t, snapshot.Records)

	_, err = empty.logSpool.AppendBytes([]byte("0123456789"))
	require.NoError(t, err)
	snapshot, err = empty.SnapshotLogSpool(now.Add(-time.Minute), now.Add(time.Minute), 4)
	require.NoError(t, err)
	require.True(t, snapshot.Available)
	require.True(t, snapshot.Truncated)
	require.Empty(t, snapshot.Data)
	require.Zero(t, snapshot.Records)
}
