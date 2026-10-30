package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSquidJournalDuplicateDeliveryAndRestart(t *testing.T) {
	oldJournal, oldNode := agentPatchJournal, patchAgentNodeID
	t.Cleanup(func() { agentPatchJournal, patchAgentNodeID = oldJournal, oldNode })
	path := filepath.Join(t.TempDir(), "journal.json")
	var err error
	agentPatchJournal, err = openPatchJournal(path)
	require.NoError(t, err)
	patchAgentNodeID = uuid.NewString()
	id := uuid.NewString()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// Wrong node must fail before any executable can be called.
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "type": squidJobReconfigure, "status": "running", "payload": squidActionDetail{ProxyID: uuid.NewString(), NodeID: uuid.NewString(), ExpiresAt: time.Now().Add(time.Minute), RequestedBy: "admin", Whitelist: []string{"archive.ubuntu.com"}}})
	}))
	defer srv.Close()
	client, err := api.NewClient(srv.URL, "", "", "", "")
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			executeSquidAction(context.Background(), client, zap.NewNop(), squidJobReconfigure+":"+id)
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, requests.Load())
	for i := 0; i < 2; i++ {
		pending := agentPatchJournal.pending()
		require.Len(t, pending, 1, "lost heartbeat must not drain a proxy result")
		require.Equal(t, "failed", pending[0].Status)
	}
	agentPatchJournal, err = openPatchJournal(path)
	require.NoError(t, err)
	executeSquidAction(context.Background(), client, zap.NewNop(), squidJobReconfigure+":"+id)
	require.EqualValues(t, 1, requests.Load(), "restart must not repeat a reserved action")
	require.NoError(t, agentPatchJournal.acknowledge([]string{id}))
	agentPatchJournal, err = openPatchJournal(path)
	require.NoError(t, err)
	require.Empty(t, agentPatchJournal.pending())
}

func TestSquidJournalRefusesMissingOrUnwritableJournal(t *testing.T) {
	old := agentPatchJournal
	t.Cleanup(func() { agentPatchJournal = old })
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer srv.Close()
	client, err := api.NewClient(srv.URL, "", "", "", "")
	require.NoError(t, err)
	agentPatchJournal = nil
	executeSquidAction(context.Background(), client, zap.NewNop(), squidJobInstall+":"+uuid.NewString())
	agentPatchJournal, err = openPatchJournal(filepath.Join(t.TempDir(), "journal.json"))
	require.NoError(t, err)
	agentPatchJournal.persistFault = func(stage string) error { return fmt.Errorf("disk unavailable") }
	executeSquidAction(context.Background(), client, zap.NewNop(), squidJobInstall+":"+uuid.NewString())
	require.Zero(t, requests.Load(), "no job fetch or command before durable reservation")
}

func TestSquidJournalInterruptedActionIsNeverReplayed(t *testing.T) {
	for _, action := range []string{squidJobInstall, squidJobReconfigure, "squid.remove"} {
		t.Run(action, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal.json")
			j, err := openPatchJournal(path)
			require.NoError(t, err)
			id := uuid.NewString()
			ok, err := j.reserve(id, action)
			require.NoError(t, err)
			require.True(t, ok)
			j, err = openPatchJournal(path)
			require.NoError(t, err)
			pending := j.pending()
			require.Len(t, pending, 1)
			require.True(t, strings.Contains(pending[0].Error, "outcome unknown"))
			ok, err = j.reserve(id, action)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestSquidExecutionWaitsForPackageMutationLock(t *testing.T) {
	old := agentPatchJournal
	t.Cleanup(func() { agentPatchJournal = old })
	var err error
	agentPatchJournal, err = openPatchJournal(filepath.Join(t.TempDir(), "journal.json"))
	require.NoError(t, err)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	defer srv.Close()
	client, err := api.NewClient(srv.URL, "", "", "", "")
	require.NoError(t, err)
	id := uuid.NewString()
	patchExecutionMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		executeSquidAction(context.Background(), client, zap.NewNop(), squidJobReconfigure+":"+id)
	}()
	reserved := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		agentPatchJournal.mu.Lock()
		_, reserved = agentPatchJournal.entries[id]
		agentPatchJournal.mu.Unlock()
		if reserved {
			break
		}
		time.Sleep(time.Millisecond)
	}
	before := requests.Load()
	patchExecutionMu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy execution did not leave lock")
	}
	require.True(t, reserved)
	require.Zero(t, before, "job must be revalidated after competing package mutation completes")
	require.EqualValues(t, 1, requests.Load())
}
