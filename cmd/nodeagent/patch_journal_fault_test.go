package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatchJournalPersistenceFaultMatrix(t *testing.T) {
	for _, operation := range []string{"reserve", "complete", "acknowledge"} {
		for _, stage := range []string{"mkdir", "create", "chmod", "write", "sync", "close", "rename", "dirsync"} {
			for _, fault := range []syscall.Errno{syscall.ENOSPC, syscall.EIO, syscall.EROFS} {
				t.Run(fmt.Sprintf("%s/%s/%d", operation, stage, fault), func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "journal.json")
					journal, err := openPatchJournal(path)
					require.NoError(t, err)
					reserved, err := journal.reserve("existing", patchJobDirect)
					require.NoError(t, err)
					require.True(t, reserved)
					if operation != "complete" {
						require.NoError(t, journal.complete(completedAction{Action: patchJobDirect, JobID: "existing", Status: "succeeded"}))
					}
					before, err := os.ReadFile(path)
					require.NoError(t, err)
					journal.persistFault = func(s string) error {
						if s == stage {
							return &os.PathError{Op: s, Path: path, Err: fault}
						}
						return nil
					}
					switch operation {
					case "reserve":
						ok, e := journal.reserve("new", patchJobDirect)
						require.Error(t, e)
						require.False(t, ok)
					case "complete":
						require.Error(t, journal.complete(completedAction{Action: patchJobDirect, JobID: "existing", Status: "succeeded"}))
					case "acknowledge":
						require.Error(t, journal.acknowledge([]string{"existing"}))
					}
					require.True(t, journal.dirty)
					require.Empty(t, journal.pending(), "never report an unpersisted snapshot")
					require.Error(t, journal.acknowledge([]string{"existing"}), "cannot acknowledge while the snapshot cannot persist")
					require.False(t, journal.entries["existing"].Acknowledged)
					after, err := os.ReadFile(path)
					require.NoError(t, err)
					if stage != "dirsync" {
						require.True(t, bytes.Equal(before, after), "pre-replacement failure changed the old journal")
					}
					temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".patch-journal-*"))
					require.NoError(t, err)
					require.Empty(t, temporary)
					journal.persistFault = nil
					require.Len(t, journal.pending(), 1, "retry must persist and recover the retained result")
					reopened, err := openPatchJournal(path)
					require.NoError(t, err)
					require.Len(t, reopened.pending(), 1)
					require.Equal(t, "succeeded", reopened.pending()[0].Status)
					ok, err := reopened.reserve("existing", patchJobDirect)
					require.NoError(t, err)
					require.False(t, ok)
					require.NoError(t, reopened.acknowledge([]string{"existing"}))
					reopened, err = openPatchJournal(path)
					require.NoError(t, err)
					require.Empty(t, reopened.pending())
				})
			}
		}
	}
}

func TestPatchJournalCompletionWriteFailureMustPersistBeforeReport(t *testing.T) {
	journal, err := openPatchJournal(filepath.Join(t.TempDir(), "journal.json"))
	require.NoError(t, err)
	_, err = journal.reserve("job", patchJobDirect)
	require.NoError(t, err)
	journal.persistFault = func(string) error { return syscall.ENOSPC }
	require.Error(t, journal.complete(completedAction{JobID: "job", Action: patchJobDirect, Status: "succeeded"}))
	require.Empty(t, journal.pending())
	journal.persistFault = nil
	require.Len(t, journal.pending(), 1)
	reopened, err := openPatchJournal(journal.path)
	require.NoError(t, err)
	require.Equal(t, "succeeded", reopened.pending()[0].Status)
}
