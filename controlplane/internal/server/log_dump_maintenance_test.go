package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type fakeLogDumpMaintenanceStore struct {
	timedOut   []storage.LogDump
	candidates []storage.LogDump
	failed     []uuid.UUID
	expired    []uuid.UUID
	deleting   []uuid.UUID
	deleted    []uuid.UUID
	retries    []uuid.UUID
}

func (f *fakeLogDumpMaintenanceStore) ListTimedOutLogDumps(context.Context, time.Time, time.Time, int) ([]storage.LogDump, error) {
	return f.timedOut, nil
}
func (f *fakeLogDumpMaintenanceStore) FailLogDumpAndJob(_ context.Context, _, _ uuid.UUID, dumpID, _ uuid.UUID, _ string, _ *bool, _ string, _ time.Time) error {
	f.failed = append(f.failed, dumpID)
	return nil
}
func (f *fakeLogDumpMaintenanceStore) ListLogDumpCleanupCandidates(context.Context, time.Time, int) ([]storage.LogDump, error) {
	return f.candidates, nil
}
func (f *fakeLogDumpMaintenanceStore) ExpireLogDump(_ context.Context, _, _ uuid.UUID, dumpID uuid.UUID, _ time.Time) (bool, error) {
	f.expired = append(f.expired, dumpID)
	return true, nil
}
func (f *fakeLogDumpMaintenanceStore) MarkLogDumpDeleting(_ context.Context, _, _ uuid.UUID, dumpID uuid.UUID) error {
	f.deleting = append(f.deleting, dumpID)
	return nil
}
func (f *fakeLogDumpMaintenanceStore) MarkLogDumpCleanupRetry(_ context.Context, _, _ uuid.UUID, dumpID uuid.UUID, _ string, _ time.Time) error {
	f.retries = append(f.retries, dumpID)
	return nil
}
func (f *fakeLogDumpMaintenanceStore) DeleteLogDump(_ context.Context, _, _ uuid.UUID, dumpID uuid.UUID) error {
	f.deleted = append(f.deleted, dumpID)
	return nil
}

func TestRunLogDumpMaintenanceOnceTimesOutAndDeletesExpiredArtifacts(t *testing.T) {
	root := t.TempDir()
	t.Setenv(logDumpDirEnv, root)
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tenantID, nodeID, jobID := uuid.New(), uuid.New(), uuid.New()
	timedOutID, expiredID := uuid.New(), uuid.New()

	artifactDir := filepath.Join(root, tenantID.String(), nodeID.String())
	require.NoError(t, os.MkdirAll(artifactDir, 0o750))
	artifactPath := filepath.Join(artifactDir, expiredID.String()+".ndjson")
	require.NoError(t, os.WriteFile(artifactPath, []byte("{\"ok\":true}\n"), 0o640))
	chunkDir := filepath.Join(root, ".chunks", expiredID.String(), jobID.String())
	require.NoError(t, os.MkdirAll(chunkDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(chunkDir, "0.tmp"), []byte("chunk"), 0o640))

	store := &fakeLogDumpMaintenanceStore{
		timedOut: []storage.LogDump{{
			ID: timedOutID, TenantID: tenantID, NodeID: nodeID,
			JobID:  uuid.NullUUID{UUID: jobID, Valid: true},
			Status: storage.LogDumpStatusCapturing,
		}},
		candidates: []storage.LogDump{{
			ID: expiredID, TenantID: tenantID, NodeID: nodeID, ArtifactPath: artifactPath,
			Status: storage.LogDumpStatusCaptured, ExpiresAt: now.Add(-time.Minute),
		}},
	}

	runLogDumpMaintenanceOnce(context.Background(), nil, store, now)

	require.Equal(t, []uuid.UUID{timedOutID}, store.failed)
	require.Equal(t, []uuid.UUID{expiredID}, store.expired)
	require.Equal(t, []uuid.UUID{expiredID}, store.deleting)
	require.Equal(t, []uuid.UUID{expiredID}, store.deleted)
	require.Empty(t, store.retries)
	_, err := os.Stat(artifactPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(root, ".chunks", expiredID.String()))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRunLogDumpMaintenanceOnceRetriesUnsafeArtifactPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv(logDumpDirEnv, root)
	dumpID := uuid.New()
	store := &fakeLogDumpMaintenanceStore{candidates: []storage.LogDump{{
		ID: dumpID, TenantID: uuid.New(), NodeID: uuid.New(),
		ArtifactPath: filepath.Join(t.TempDir(), "outside.ndjson"),
		Status:       storage.LogDumpStatusExpired,
	}}}
	runLogDumpMaintenanceOnce(context.Background(), nil, store, time.Now().UTC())
	require.Equal(t, []uuid.UUID{dumpID}, store.deleting)
	require.Equal(t, []uuid.UUID{dumpID}, store.retries)
	require.Empty(t, store.deleted)
}
