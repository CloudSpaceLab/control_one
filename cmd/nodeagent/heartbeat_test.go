package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// TestSendHeartbeatTransmitsPayload spins up an httptest server, points an
// agent api.Client at it, and asserts that sendHeartbeat actually puts bytes
// on the wire — including the unconditional agent_version + firewall_state
// fields documented in heartbeat.go.
func TestSendHeartbeatTransmitsPayload(t *testing.T) {
	var (
		mu         sync.Mutex
		gotMethod  string
		gotPath    string
		gotCT      string
		gotBodyRaw []byte
		gotPayload heartbeatPayload
		callCount  int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBodyRaw = body
		_ = json.Unmarshal(body, &gotPayload)

		// Mirror the real server's response shape so the agent's decode path
		// also gets exercised.
		ack := heartbeatAckResponse{
			NodeID:     "11111111-1111-1111-1111-111111111111",
			State:      "active",
			LastSeenAt: "2026-05-05T00:00:00Z",
			Activated:  false,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ack)
	}))
	defer srv.Close()

	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}

	logger := zap.NewNop()
	const nodeID = "11111111-1111-1111-1111-111111111111"

	if err := sendHeartbeat(context.Background(), client, logger, nodeID, nil, nil, nil); err != nil {
		t.Fatalf("sendHeartbeat: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if callCount != 1 {
		t.Fatalf("expected 1 call to control plane, got %d", callCount)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	wantPath := "/api/v1/nodes/" + nodeID + "/heartbeat"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if len(gotBodyRaw) == 0 {
		t.Fatal("control plane received an empty body — heartbeat sent no data")
	}

	t.Logf("wire bytes (%d): %s", len(gotBodyRaw), string(gotBodyRaw))

	if gotPayload.AgentVersion == "" {
		t.Error("agent_version was not transmitted")
	}
	if !strings.Contains(gotPayload.AgentVersion, "/") {
		t.Errorf("agent_version = %q, expected GOOS/GOARCH suffix", gotPayload.AgentVersion)
	}
	if gotPayload.FirewallState == nil {
		t.Error("firewall_state was not transmitted (should be sent every heartbeat)")
	} else if gotPayload.FirewallState.Type == "" {
		t.Errorf("firewall_state.type is empty, expected at least \"none\"")
	}
}

func TestSendHeartbeatReportsReleaseSeqAndDispatchesAgentUpdateJob(t *testing.T) {
	update := &fakeSelfUpdater{seq: 7, called: make(chan string, 1)}
	var gotPayload heartbeatPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		ack := heartbeatAckResponse{
			NodeID:         "11111111-1111-1111-1111-111111111111",
			State:          "active",
			LastSeenAt:     "2026-05-05T00:00:00Z",
			PendingActions: []string{agentUpdateJob + ":job-123"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ack)
	}))
	defer srv.Close()

	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}

	if err := sendHeartbeat(context.Background(), client, zap.NewNop(), "11111111-1111-1111-1111-111111111111", nil, nil, update); err != nil {
		t.Fatalf("sendHeartbeat: %v", err)
	}
	if gotPayload.AgentReleaseSeq != 7 {
		t.Fatalf("agent_release_seq = %d, want 7", gotPayload.AgentReleaseSeq)
	}
	select {
	case got := <-update.called:
		if got != "job-123" {
			t.Fatalf("TriggerUpdate jobID = %q, want job-123", got)
		}
	case <-time.After(time.Second):
		t.Fatal("TriggerUpdate was not called")
	}
}

type fakeSelfUpdater struct {
	seq    int
	called chan string
}

func (f *fakeSelfUpdater) CurrentReleaseSeq() int { return f.seq }

func (f *fakeSelfUpdater) TriggerUpdate(_ context.Context, _ *api.Client, _ *zap.Logger, jobID string) {
	f.called <- jobID
}
