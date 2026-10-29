//go:build linux

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// These tests execute the real dispatcher and shell pipeline with PATH restricted
// to a temporary fake apt-get. No host package manager can be selected.
func patchExecutionFixture(t *testing.T, blocking bool) (string, *api.Client, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
set -eu
if ! /bin/mkdir "$PATCH_TEST_DIR/active"; then echo overlap >> "$PATCH_TEST_DIR/collisions"; exit 77; fi
echo invocation >> "$PATCH_TEST_DIR/invocations"
`
	if blocking {
		script += `echo ready > "$PATCH_TEST_DIR/ready"
while :; do /bin/sleep 1; done
`
	} else {
		script += `/bin/sleep 0.1
/bin/rmdir "$PATCH_TEST_DIR/active"
`
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "apt-get"), []byte(script), 0700))
	t.Setenv("PATH", dir)
	t.Setenv("PATCH_TEST_DIR", dir)
	calls := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "type": patchJobDirect, "status": "running", "payload": patchActionDetail{NodeID: "test-node", NodePatchStateID: "state-" + id, DeploymentID: "deployment-" + id, Mode: "direct", PackageAllowlist: []string{"fixture-package"}}})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PATCH_TEST_URL", srv.URL)
	client, err := api.NewClient(srv.URL, "", "", "", "")
	require.NoError(t, err)
	return dir, client, calls
}

func TestPatchExecutionSerializesDifferentJobsAndDeduplicates(t *testing.T) {
	dir, client, calls := patchExecutionFixture(t, false)
	previousJournal, previousID := agentPatchJournal, patchAgentNodeID
	t.Cleanup(func() { agentPatchJournal, patchAgentNodeID = previousJournal, previousID })
	var err error
	agentPatchJournal, err = openPatchJournal(filepath.Join(dir, "journal.json"))
	require.NoError(t, err)
	patchAgentNodeID = "test-node"
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		for _, id := range []string{"job-a", "job-b"} {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				executePatchAction(context.Background(), client, zap.NewNop(), patchJobDirect+":"+id)
			}(id)
		}
	}
	wg.Wait()
	require.EqualValues(t, 2, calls.Load(), "each distinct job fetched once despite repeated delivery")
	_, err = os.Stat(filepath.Join(dir, "collisions"))
	require.True(t, os.IsNotExist(err), "different jobs overlapped package execution")
	invocations, err := os.ReadFile(filepath.Join(dir, "invocations"))
	require.NoError(t, err)
	require.Equal(t, 4, strings.Count(string(invocations), "invocation"), "one update and one install per distinct job")
	pending := agentPatchJournal.pending()
	require.Len(t, pending, 2)
	for _, result := range pending {
		require.Equal(t, "succeeded", result.Status, result.Error)
	}
	// Lose the acknowledgment, reopen, and deliver both jobs again.
	agentPatchJournal, err = openPatchJournal(filepath.Join(dir, "journal.json"))
	require.NoError(t, err)
	for _, id := range []string{"job-a", "job-b"} {
		executePatchAction(context.Background(), client, zap.NewNop(), patchJobDirect+":"+id)
	}
	require.EqualValues(t, 2, calls.Load())
	require.Len(t, agentPatchJournal.pending(), 2, "lost acknowledgment retains both reports")
}

func TestPatchExecutionProcessHelper(t *testing.T) {
	if os.Getenv("PATCH_PROCESS_HELPER") != "1" {
		t.Skip("subprocess helper only")
	}
	var err error
	agentPatchJournal, err = openPatchJournal(filepath.Join(os.Getenv("PATCH_TEST_DIR"), "journal.json"))
	require.NoError(t, err)
	patchAgentNodeID = "test-node"
	client, err := api.NewClient(os.Getenv("PATCH_TEST_URL"), "", "", "", "")
	require.NoError(t, err)
	executePatchAction(context.Background(), client, zap.NewNop(), patchJobDirect+":interrupted-job")
}

func TestPatchExecutionProcessDeathDoesNotRerun(t *testing.T) {
	dir, client, calls := patchExecutionFixture(t, true)
	path, err := os.Executable()
	require.NoError(t, err)
	child := exec.Command(path, "-test.run=^TestPatchExecutionProcessHelper$")
	child.Env = append(os.Environ(), "PATCH_PROCESS_HELPER=1")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, child.Start())
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			_ = child.Wait()
		}
	})
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "ready")); return err == nil }, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, syscall.Kill(-child.Process.Pid, syscall.SIGKILL))
	require.Error(t, child.Wait())
	reaped = true
	previousJournal, previousID := agentPatchJournal, patchAgentNodeID
	t.Cleanup(func() { agentPatchJournal, patchAgentNodeID = previousJournal, previousID })
	agentPatchJournal, err = openPatchJournal(filepath.Join(dir, "journal.json"))
	require.NoError(t, err)
	patchAgentNodeID = "test-node"
	pending := agentPatchJournal.pending()
	require.Len(t, pending, 1)
	require.Equal(t, "failed", pending[0].Status)
	require.Contains(t, pending[0].Error, "outcome unknown")
	executePatchAction(context.Background(), client, zap.NewNop(), patchJobDirect+":interrupted-job")
	require.EqualValues(t, 1, calls.Load(), "interrupted execution must not even refetch the job")
	invocations, err := os.ReadFile(filepath.Join(dir, "invocations"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(invocations), "invocation"))
}
