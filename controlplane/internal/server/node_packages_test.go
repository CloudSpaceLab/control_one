package server

import (
	"context"
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

// TestHandleNodePackagesReturnsRows seeds the fakeStore via the same write
// path the heartbeat ingest uses (ReplaceNodePackages) and asserts the read
// endpoint returns the expected rows for an authenticated viewer.
func TestHandleNodePackagesReturnsRows(t *testing.T) {
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

	installed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	arch := "amd64"
	if err := store.ReplaceNodePackages(context.Background(), nodeID, []storage.NodePackage{
		{NodeID: nodeID, Name: "openssl", Version: "3.0.10", Source: "apt", Arch: &arch, InstalledAt: &installed},
		{NodeID: nodeID, Name: "curl", Version: "7.88.1", Source: "apt"},
	}); err != nil {
		t.Fatalf("seed packages: %v", err)
	}

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/packages", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}

	var body struct {
		Data []nodePackageResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("rows = %d, want 2 (%s)", len(body.Data), rec.Body.String())
	}
	// Spot-check the row that exercises the optional fields.
	var openssl *nodePackageResponse
	for i, row := range body.Data {
		if row.Name == "openssl" {
			openssl = &body.Data[i]
			break
		}
	}
	if openssl == nil {
		t.Fatalf("openssl row missing from response: %s", rec.Body.String())
	}
	if openssl.Source != "apt" {
		t.Fatalf("source = %q, want apt", openssl.Source)
	}
	if openssl.Arch == nil || *openssl.Arch != "amd64" {
		t.Fatalf("arch = %v, want amd64", openssl.Arch)
	}
	if openssl.InstalledAt == nil || *openssl.InstalledAt != installed.UTC().Format(time.RFC3339) {
		t.Fatalf("installed_at = %v, want %s", openssl.InstalledAt, installed.UTC().Format(time.RFC3339))
	}
	if openssl.NodeID != nodeID.String() {
		t.Fatalf("node_id = %q, want %q", openssl.NodeID, nodeID.String())
	}
}

// TestHandleNodePackagesEmptyForKnownNode verifies a node with no rows returns
// 200 with an empty data slice rather than 404 — empty inventory is a valid
// state (host enrolled, hash matches, agent hasn't done a full sync yet).
func TestHandleNodePackagesEmptyForKnownNode(t *testing.T) {
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
		Auth: authWithTokens("viewer", "viewer-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/packages", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []nodePackageResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 0 {
		t.Fatalf("rows = %d, want 0", len(body.Data))
	}
}

// TestHandleNodePackagesUnknownNode404 locks in the contract that a stale UI
// link to a deleted/unknown node id surfaces 404 rather than masquerading as
// an empty inventory.
func TestHandleNodePackagesUnknownNode404(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+uuid.New().String()+"/packages", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleNodePackagesRequiresAuth checks that an unauthenticated caller
// gets bounced before any storage access happens.
func TestHandleNodePackagesRequiresAuth(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+uuid.New().String()+"/packages", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestHandleNodePackagesRejectsNonGet locks in the Allow header contract.
func TestHandleNodePackagesRejectsNonGet(t *testing.T) {
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
		Auth: authWithTokens("viewer", "viewer-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/packages", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want %q", got, http.MethodGet)
	}
}
