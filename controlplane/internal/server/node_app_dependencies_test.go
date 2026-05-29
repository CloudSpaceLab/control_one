package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestHandleNodeAppDependenciesIngestAndList(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "tn", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "app-01",
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

	body := nodeAppDependenciesRequest{Dependencies: []nodeAppDependencyItem{{
		AppRoot:        "/srv/core-api",
		Ecosystem:      "npm",
		Name:           "express",
		Version:        "4.18.2",
		PackageManager: "npm",
		ManifestPath:   "/srv/core-api/package-lock.json",
		Scope:          "runtime",
		PURL:           "pkg:npm/express@4.18.2",
		Metadata:       map[string]any{"service": "core-api"},
	}}}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/%s/app-dependencies", nodeID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, agentPrincipal(nodeID))
	rec := httptest.NewRecorder()
	srv.handleNodeAppDependencies(rec, req, nodeID)
	if rec.Code != http.StatusOK {
		t.Fatalf("ingest status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/nodes/%s/app-dependencies", nodeID), nil)
	listReq.Header.Set("Authorization", "Bearer viewer-token")
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s, want 200", listRec.Code, listRec.Body.String())
	}
	var out struct {
		Data []nodeAppDependencyResponse `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(out.Data) != 1 {
		t.Fatalf("dependencies = %#v, want one", out.Data)
	}
	dep := out.Data[0]
	if dep.Ecosystem != "npm" || dep.Name != "express" || dep.Version != "4.18.2" || dep.PURL != "pkg:npm/express@4.18.2" {
		t.Fatalf("dependency response lost app dependency evidence: %#v", dep)
	}
}

func TestHandleNodeAppDependenciesRejectsMismatchedAgent(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	store := &fakeStore{}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/%s/app-dependencies", nodeID), bytes.NewReader([]byte(`{"dependencies":[]}`)))
	req = withPrincipal(req, agentPrincipal(uuid.New()))
	rec := httptest.NewRecorder()
	srv.handleNodeAppDependencies(rec, req, nodeID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestHandleNodeAppDependenciesTriggersVulnerabilityRescan(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "tn", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "app-01",
			State:     storage.NodeStateActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}
	root := t.TempDir()
	activeDir := filepath.Join(root, "active", offlinebundle.ContentTypeVulnerabilityFeed)
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		t.Fatalf("mkdir active feed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "app-feed.json"), []byte(`{
		"schema_version":1,
		"source":"unit-test",
		"advisories":[{
			"cve_id":"CVE-2026-5000",
			"severity":"high",
			"affected_packages":[{
				"name":"pkg:npm/express@4.18.2",
				"source":"purl",
				"version_range":"< 4.18.3",
				"fixed_version":"4.18.3"
			}]
		}]
	}`), 0o644); err != nil {
		t.Fatalf("write feed: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})
	srv.offlineContentRoot = root

	body := nodeAppDependenciesRequest{Dependencies: []nodeAppDependencyItem{{
		AppRoot:   "/srv/core-api",
		Ecosystem: "npm",
		Name:      "express",
		Version:   "4.18.2",
		PURL:      "pkg:npm/express@4.18.2",
	}}}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/%s/app-dependencies", nodeID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, agentPrincipal(nodeID))
	rec := httptest.NewRecorder()
	srv.handleNodeAppDependencies(rec, req, nodeID)
	if rec.Code != http.StatusOK {
		t.Fatalf("ingest status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.vulnerabilityFindings) != 1 {
		t.Fatalf("vulnerability findings = %#v", store.vulnerabilityFindings)
	}
	finding := store.vulnerabilityFindings[0]
	if finding.CVEID != "CVE-2026-5000" || finding.PackageSource != "npm" || finding.FixedVersion != "4.18.3" {
		t.Fatalf("finding = %#v", finding)
	}
}

func TestHandleNodeAppDependenciesUnknownNode404(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+uuid.New().String()+"/app-dependencies", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestFakeStoreReplaceNodeAppDependenciesClearsRows(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	nodeID := uuid.New()
	tenantID := uuid.New()
	if err := store.ReplaceNodeAppDependencies(context.Background(), nodeID, tenantID, []storage.NodeAppDependency{{
		Ecosystem: "npm", Name: "express", Version: "4.18.2",
	}}); err != nil {
		t.Fatalf("replace deps: %v", err)
	}
	if err := store.ReplaceNodeAppDependencies(context.Background(), nodeID, tenantID, nil); err != nil {
		t.Fatalf("clear deps: %v", err)
	}
	deps, err := store.ListNodeAppDependencies(context.Background(), nodeID)
	if err != nil {
		t.Fatalf("list deps: %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("deps = %#v, want empty after clear", deps)
	}
}
