package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// mtlsRequest builds a request where the auth middleware has already promoted
// the caller to an agent Principal with the given CN. This mirrors what the
// real middleware does off r.TLS.PeerCertificates[0]; using the context
// directly lets us exercise the handler without spinning up a TLS stack.
func mtlsRequest(method, path, cn string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	principal := &auth.Principal{
		Type:    "agent",
		Name:    cn,
		Subject: "CN=" + cn,
		Roles:   []string{"agent"},
	}
	return req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal))
}

func buildHeartbeatServer(t *testing.T, store *fakeStore) *Server {
	t.Helper()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	t.Cleanup(func() { srv.stopEnrollmentReaper() })
	return srv
}

// TestHeartbeatBumpsLastSeen covers the happy path: a pending node without a
// first_scan gets a last_seen_at bump but stays pending.
func TestHeartbeatBumpsLastSeen(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "pending-host",
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Activated {
		t.Fatalf("node should not activate without first_scan; got activated=true")
	}
	if resp.State != storage.NodeStateEnrollmentPending {
		t.Fatalf("state = %s, want enrollment_pending", resp.State)
	}
	if store.nodes[0].LastSeenAt == nil {
		t.Fatalf("last_seen_at was not stamped")
	}
}

// TestHeartbeatActivatesAfterFirstScan drives the state machine: a pending
// node with both a heartbeat AND a prior first_scan flips to active.
func TestHeartbeatActivatesAfterFirstScan(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	firstScan := time.Now().Add(-time.Minute).UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:          nodeID,
			TenantID:    tenantID,
			Hostname:    "gated-host",
			State:       storage.NodeStateEnrollmentPending,
			FirstScanAt: &firstScan,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			Labels:      map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Activated {
		t.Fatalf("expected node to activate on heartbeat+first_scan combo")
	}
	if resp.State != storage.NodeStateActive {
		t.Fatalf("state = %s, want active", resp.State)
	}
	if store.nodes[0].State != storage.NodeStateActive {
		t.Fatalf("node state not persisted as active: %s", store.nodes[0].State)
	}
}

// TestHeartbeatRejectsMismatchedCN is the core mTLS authz check: agent cert
// CN must equal the URL-scoped node id.
func TestHeartbeatRejectsMismatchedCN(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			Hostname:  "victim",
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	imposter := uuid.New().String()
	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", imposter)
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if store.nodes[0].LastSeenAt != nil {
		t.Fatalf("last_seen_at should not be stamped on rejected heartbeat")
	}
}

// TestHeartbeatRejectsNonAgentPrincipal ensures bearer-token operators cannot
// forge heartbeats on behalf of a node.
func TestHeartbeatRejectsNonAgentPrincipal(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			Hostname:  "host",
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nil)
	principal := &auth.Principal{
		Type:    "user",
		Name:    "admin",
		Subject: "user-admin",
		Roles:   []string{"admin"},
	}
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal))
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// TestHeartbeatReturnsNotFoundForUnknownNode checks the ErrNoRows branch. The
// agent middleware can't prevent this because an agent cert survives the
// node row being deleted.
func TestHeartbeatReturnsNotFoundForUnknownNode(t *testing.T) {
	t.Parallel()

	unknown := uuid.New()
	store := &fakeStore{}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+unknown.String()+"/heartbeat", unknown.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestFirstScanHookActivatesWhenHeartbeatAlreadyLanded covers the second
// ordering: heartbeat first, then first compliance scan. The scan hook should
// activate the node.
func TestFirstScanHookActivatesWhenHeartbeatAlreadyLanded(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	lastSeen := time.Now().Add(-30 * time.Second).UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:         nodeID,
			TenantID:   tenantID,
			Hostname:   "flipped-by-scan",
			State:      storage.NodeStateEnrollmentPending,
			LastSeenAt: &lastSeen,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Labels:     map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	// Invoke the hook directly — this is what compliance.go calls after
	// persisting scan results.
	srv.handleFirstScanHook(context.Background(), nodeID)

	if store.nodes[0].State != storage.NodeStateActive {
		t.Fatalf("scan hook failed to activate node: state=%s", store.nodes[0].State)
	}
	if store.nodes[0].FirstScanAt == nil {
		t.Fatalf("scan hook failed to stamp first_scan_at")
	}
}

// TestEnrollmentReaperFlipsPendingToFailed drives the reaper directly to
// avoid relying on wall-clock sleeps. A pending node older than the timeout
// should become enrollment_failed; a fresher pending node should not.
func TestEnrollmentReaperFlipsPendingToFailed(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	oldID := uuid.New()
	freshID := uuid.New()
	activeID := uuid.New()
	past := time.Now().Add(-20 * time.Minute)
	store := &fakeStore{
		nodes: []storage.Node{
			{
				ID:        oldID,
				TenantID:  tenantID,
				Hostname:  "stale",
				State:     storage.NodeStateEnrollmentPending,
				CreatedAt: past,
				UpdatedAt: past,
				Labels:    map[string]any{},
			},
			{
				ID:        freshID,
				TenantID:  tenantID,
				Hostname:  "fresh",
				State:     storage.NodeStateEnrollmentPending,
				CreatedAt: time.Now().Add(-time.Minute),
				UpdatedAt: time.Now().Add(-time.Minute),
				Labels:    map[string]any{},
			},
			{
				ID:        activeID,
				TenantID:  tenantID,
				Hostname:  "already-active",
				State:     storage.NodeStateActive,
				CreatedAt: past,
				UpdatedAt: past,
				Labels:    map[string]any{},
			},
		},
	}
	srv := buildHeartbeatServer(t, store)
	srv.clockOverride = func() time.Time { return time.Now() }

	srv.reapPendingEnrollments()

	for _, n := range store.nodes {
		switch n.ID {
		case oldID:
			if n.State != storage.NodeStateEnrollmentFailed {
				t.Fatalf("old pending node: state=%s, want enrollment_failed", n.State)
			}
		case freshID:
			if n.State != storage.NodeStateEnrollmentPending {
				t.Fatalf("fresh pending node was flipped prematurely: state=%s", n.State)
			}
		case activeID:
			if n.State != storage.NodeStateActive {
				t.Fatalf("active node was touched by reaper: state=%s", n.State)
			}
		}
	}
}

// TestHeartbeatRejectsWrongMethod sanity-checks the allow list.
func TestHeartbeatRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// Guard against unused imports when tests are trimmed.
var _ = sql.ErrNoRows
