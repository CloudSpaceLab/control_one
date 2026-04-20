package server

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// This file is the heartbeat compatibility shim for rollout health gates.
// It lives in the main (non-test) build because
// cluster_rollouts_jobs.go::nodeLastSeenAt calls testLookupNodeLastSeen at
// runtime. Worktree A is adding `storage.Node.LastSeenAt` in migration 0028;
// when that lands on `seigha`, nodeLastSeenAt will read the real column and
// this file can be deleted.
//
// relies on nodes.last_seen_at from migration 0028 (Worktree A)
//
// In production the map stays nil, so testLookupNodeLastSeen always returns
// nil and the heartbeat gate interprets that as "never heartbeated" — the
// wave stays pending until gate.timeout and is marked unhealthy.
var (
	testNodeLastSeenMu sync.RWMutex
	testNodeLastSeen   map[uuid.UUID]time.Time
)

// testLookupNodeLastSeen returns the heartbeat time recorded for the given
// node id, or nil if none was registered. The test-only setter is
// setTestNodeLastSeen below.
func testLookupNodeLastSeen(id uuid.UUID) *time.Time {
	testNodeLastSeenMu.RLock()
	defer testNodeLastSeenMu.RUnlock()
	if testNodeLastSeen == nil {
		return nil
	}
	if t, ok := testNodeLastSeen[id]; ok {
		copied := t
		return &copied
	}
	return nil
}

// setTestNodeLastSeen records a heartbeat timestamp for a node id. Tests only.
func setTestNodeLastSeen(id uuid.UUID, t time.Time) {
	testNodeLastSeenMu.Lock()
	defer testNodeLastSeenMu.Unlock()
	if testNodeLastSeen == nil {
		testNodeLastSeen = map[uuid.UUID]time.Time{}
	}
	testNodeLastSeen[id] = t
}

// clearTestNodeLastSeen wipes the in-memory heartbeat map. Tests only.
func clearTestNodeLastSeen() {
	testNodeLastSeenMu.Lock()
	defer testNodeLastSeenMu.Unlock()
	testNodeLastSeen = nil
}
