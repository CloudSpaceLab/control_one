package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// setupEnrollmentServer wires a Server with a fakeStore seeded with a tenant
// and a valid enrollment token. It returns the server, the raw token string,
// and the tenant id so callers can construct enroll requests.
func setupEnrollmentServer(t *testing.T) (*Server, string, uuid.UUID) {
	t.Helper()

	tenantID := uuid.New()
	rawToken := "cot_test_enroll_token_value"
	h := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(h[:])

	store := &fakeStore{
		tenants: []storage.Tenant{{
			ID:        tenantID,
			Name:      "enroll-test",
			CreatedAt: time.Now(),
		}},
		enrollmentTokens: map[string]storage.EnrollmentToken{
			tokenHash: {
				ID:        uuid.New(),
				TenantID:  tenantID,
				Name:      "test-token",
				TokenHash: tokenHash,
				MaxNodes:  10,
				ExpiresAt: time.Now().Add(1 * time.Hour),
				CreatedAt: time.Now(),
			},
		},
	}

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	return srv, rawToken, tenantID
}

func enroll(t *testing.T, srv *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleEnroll(rec, req)
	return rec
}

func TestEnrollCreatesNodeOnFirstCall(t *testing.T) {
	t.Parallel()

	srv, rawToken, tenantID := setupEnrollmentServer(t)

	rec := enroll(t, srv, map[string]any{
		"token":      rawToken,
		"hostname":   "host-1",
		"machine_id": "machine-uuid-1",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}

	var resp enrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.NodeID == "" {
		t.Fatal("expected node_id in response")
	}
	if resp.TenantID != tenantID.String() {
		t.Fatalf("tenant_id = %s, want %s", resp.TenantID, tenantID.String())
	}

	store := srv.store.(*fakeStore)
	if len(store.nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(store.nodes))
	}
	got := store.nodes[0]
	if !got.MachineID.Valid || got.MachineID.String != "machine-uuid-1" {
		t.Fatalf("machine_id not persisted: %+v", got.MachineID)
	}
	if got.Hostname != "host-1" {
		t.Fatalf("hostname = %s, want host-1", got.Hostname)
	}
}

func TestEnrollIsIdempotentByMachineID(t *testing.T) {
	t.Parallel()

	srv, rawToken, _ := setupEnrollmentServer(t)

	first := enroll(t, srv, map[string]any{
		"token":      rawToken,
		"hostname":   "host-original",
		"machine_id": "stable-machine-id",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first enroll status = %d (%s)", first.Code, first.Body.String())
	}
	var firstResp enrollResponse
	_ = json.Unmarshal(first.Body.Bytes(), &firstResp)

	// Re-enroll with a DIFFERENT hostname but the SAME machine_id.
	second := enroll(t, srv, map[string]any{
		"token":      rawToken,
		"hostname":   "host-reimaged",
		"machine_id": "stable-machine-id",
	})
	if second.Code != http.StatusOK {
		t.Fatalf("re-enroll status = %d (%s), want 200", second.Code, second.Body.String())
	}
	var secondResp enrollResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second: %v", err)
	}

	if firstResp.NodeID != secondResp.NodeID {
		t.Fatalf("node_id changed between enrollments: %s vs %s", firstResp.NodeID, secondResp.NodeID)
	}

	store := srv.store.(*fakeStore)
	if len(store.nodes) != 1 {
		t.Fatalf("expected 1 node after re-enroll, got %d", len(store.nodes))
	}
	// Hostname should be updated to the new value.
	if store.nodes[0].Hostname != "host-reimaged" {
		t.Fatalf("hostname not updated on re-enroll: got %q want host-reimaged", store.nodes[0].Hostname)
	}
}

func TestEnrollLegacyFallsBackToHostname(t *testing.T) {
	t.Parallel()

	srv, rawToken, _ := setupEnrollmentServer(t)

	// First enrollment from a "legacy" agent that sends no machine_id.
	first := enroll(t, srv, map[string]any{
		"token":    rawToken,
		"hostname": "legacy-host",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first enroll status = %d (%s)", first.Code, first.Body.String())
	}
	var firstResp enrollResponse
	_ = json.Unmarshal(first.Body.Bytes(), &firstResp)

	// Second enrollment from the same legacy agent (same hostname, still no machine_id).
	second := enroll(t, srv, map[string]any{
		"token":    rawToken,
		"hostname": "legacy-host",
	})
	if second.Code != http.StatusOK {
		t.Fatalf("re-enroll status = %d (%s), want 200 via hostname fallback", second.Code, second.Body.String())
	}

	var secondResp enrollResponse
	_ = json.Unmarshal(second.Body.Bytes(), &secondResp)
	if firstResp.NodeID != secondResp.NodeID {
		t.Fatalf("node id changed for legacy re-enroll: %s vs %s", firstResp.NodeID, secondResp.NodeID)
	}

	store := srv.store.(*fakeStore)
	if len(store.nodes) != 1 {
		t.Fatalf("expected 1 node for legacy re-enroll, got %d", len(store.nodes))
	}
	if store.nodes[0].MachineID.Valid {
		t.Fatalf("legacy node should have no machine_id, got %q", store.nodes[0].MachineID.String)
	}
}

func TestEnrollRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	srv, _, _ := setupEnrollmentServer(t)

	rec := enroll(t, srv, map[string]any{
		"token":    "cot_this_token_does_not_exist",
		"hostname": "some-host",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestRetireNodeMarksStateRetired exercises the new POST /api/v1/nodes/:id/retire
// endpoint through the full request router to confirm the route parsing change
// in handleNodeResource still routes regular CRUD correctly.
func TestRetireNodeMarksStateRetired(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()

	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "tn", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "host",
			State:     storage.NodeStateActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("admin", "admin-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/retire", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}

	node, err := srv.store.GetNode(context.Background(), nodeID)
	if err != nil || node == nil {
		t.Fatalf("get node: %v", err)
	}
	if node.State != storage.NodeStateRetired {
		t.Fatalf("state = %q, want retired", node.State)
	}
}

// TestRetireNodeNotFound verifies 404 is returned for unknown node ids.
func TestRetireNodeNotFound(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("admin", "admin-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+uuid.New().String()+"/retire", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Ensure sql.ErrNoRows is the zero-value surface for the retire path.
var _ = sql.ErrNoRows

// TestEnrollLandsNewNodesInEnrollmentPending locks in the Sprint 2 change:
// brand-new nodes start in enrollment_pending instead of active. The
// heartbeat + first-scan gate is what moves them to active.
func TestEnrollLandsNewNodesInEnrollmentPending(t *testing.T) {
	t.Parallel()

	srv, rawToken, _ := setupEnrollmentServer(t)
	t.Cleanup(func() { srv.stopEnrollmentReaper() })

	rec := enroll(t, srv, map[string]any{
		"token":      rawToken,
		"hostname":   "pending-host",
		"machine_id": "pending-machine",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}

	store := srv.store.(*fakeStore)
	if len(store.nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(store.nodes))
	}
	if store.nodes[0].State != storage.NodeStateEnrollmentPending {
		t.Fatalf("state = %q, want enrollment_pending", store.nodes[0].State)
	}
}

// TestReenrollmentResetsFailedState verifies that re-enrolling a node that
// previously timed out (enrollment_failed) resets it to enrollment_pending so
// the heartbeat + first-scan gate can run again from scratch.
func TestReenrollmentResetsFailedState(t *testing.T) {
	t.Parallel()

	srv, rawToken, _ := setupEnrollmentServer(t)
	t.Cleanup(func() { srv.stopEnrollmentReaper() })

	// 1. First enrollment — node lands in enrollment_pending.
	rec := enroll(t, srv, map[string]any{
		"token":      rawToken,
		"hostname":   "re-enroll-host",
		"machine_id": "re-enroll-machine",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("first enroll: status = %d (%s), want 201", rec.Code, rec.Body.String())
	}

	// 2. Simulate reaper timing out the node.
	store := srv.store.(*fakeStore)
	store.mu.Lock()
	store.nodes[0].State = storage.NodeStateEnrollmentFailed
	store.nodes[0].LastSeenAt = nil
	store.nodes[0].FirstScanAt = nil
	store.mu.Unlock()

	// 3. Re-enroll with same machine_id — must return 200 (existing node path).
	rec2 := enroll(t, srv, map[string]any{
		"token":      rawToken,
		"hostname":   "re-enroll-host",
		"machine_id": "re-enroll-machine",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("re-enroll: status = %d (%s), want 200", rec2.Code, rec2.Body.String())
	}

	// 4. State must be reset to enrollment_pending so the state machine can fire.
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.nodes[0].State != storage.NodeStateEnrollmentPending {
		t.Fatalf("after re-enroll: state = %q, want enrollment_pending", store.nodes[0].State)
	}
}

// TestEnrollReaperFlipsStalePending exercises the full loop at the server
// level: enrollment creates a pending row, the reaper sees it's stale (we
// backdate its created_at), and flips it to enrollment_failed.
func TestEnrollReaperFlipsStalePending(t *testing.T) {
	t.Parallel()

	srv, rawToken, _ := setupEnrollmentServer(t)
	t.Cleanup(func() { srv.stopEnrollmentReaper() })

	rec := enroll(t, srv, map[string]any{
		"token":    rawToken,
		"hostname": "about-to-expire",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", rec.Code, rec.Body.String())
	}

	store := srv.store.(*fakeStore)
	store.mu.Lock()
	// Backdate created_at so the row is older than the 10m timeout.
	store.nodes[0].CreatedAt = time.Now().Add(-20 * time.Minute)
	store.mu.Unlock()

	srv.reapPendingEnrollments()

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.nodes[0].State != storage.NodeStateEnrollmentFailed {
		t.Fatalf("state = %q, want enrollment_failed", store.nodes[0].State)
	}
}
