//go:build linux

package main

import (
	"context"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPatchExecutionRefusesFilesystemReservationFailure(t *testing.T) {
	dir, client, calls := patchExecutionFixture(t, false)
	previous, previousID := agentPatchJournal, patchAgentNodeID
	t.Cleanup(func() { agentPatchJournal, patchAgentNodeID = previous, previousID })
	var err error
	agentPatchJournal, err = openPatchJournal(filepath.Join(dir, "journal.json"))
	require.NoError(t, err)
	agentPatchJournal.persistFault = func(string) error { return syscall.ENOSPC }
	patchAgentNodeID = "test-node"
	executePatchAction(context.Background(), client, zap.NewNop(), patchJobDirect+":refused-job")
	require.Zero(t, calls.Load(), "failed reservation must not even fetch the executable job")
	_, err = os.Stat(filepath.Join(dir, "invocations"))
	require.True(t, os.IsNotExist(err))
}

// Run only inside a disposable network-isolated container with a small tmpfs.
// Never fill a host filesystem; the supplied root must be /fault.
func TestPatchJournalRealDiskFull(t *testing.T) {
	root := os.Getenv("PATCH_JOURNAL_FAULT_ROOT")
	if root == "" {
		t.Skip("requires disposable /fault tmpfs")
	}
	require.Equal(t, "/fault", root)
	dir, err := os.MkdirTemp(root, "journal-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "journal.json")
	journal, err := openPatchJournal(path)
	require.NoError(t, err)
	_, err = journal.reserve("real-disk-full", patchJobDirect)
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	fill := filepath.Join(dir, "fill")
	err = os.WriteFile(fill, make([]byte, 2*1024*1024), 0600)
	require.ErrorIs(t, err, syscall.ENOSPC)
	require.ErrorIs(t, journal.complete(completedAction{Action: patchJobDirect, JobID: "real-disk-full", Status: "succeeded"}), syscall.ENOSPC)
	require.Empty(t, journal.pending())
	ok, err := journal.reserve("must-not-execute", patchJobDirect)
	require.Error(t, err)
	require.False(t, ok)
	require.Error(t, journal.acknowledge([]string{"real-disk-full"}))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoError(t, os.Remove(fill))
	require.Len(t, journal.pending(), 1)
	reopened, err := openPatchJournal(path)
	require.NoError(t, err)
	require.Equal(t, "succeeded", reopened.pending()[0].Status)
	require.NoError(t, reopened.acknowledge([]string{"real-disk-full"}))
}

func TestPatchJournalRealReadOnly(t *testing.T) {
	root := os.Getenv("PATCH_JOURNAL_READONLY_ROOT")
	if root == "" {
		t.Skip("requires disposable /readonly mount")
	}
	require.Equal(t, "/readonly", root)
	original, err := os.ReadFile(filepath.Join(root, "journal.json"))
	require.NoError(t, err)
	_, err = openPatchJournal(filepath.Join(root, "journal.json"))
	require.ErrorIs(t, err, syscall.EROFS)
	after, err := os.ReadFile(filepath.Join(root, "journal.json"))
	require.NoError(t, err)
	require.Equal(t, original, after)
}
