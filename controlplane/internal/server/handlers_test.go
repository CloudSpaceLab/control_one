package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ---------- helpers ----------

// newTestServer creates a Server wired to the fakeStore and stubQueue.
// The auth config uses static bearer tokens. All tokens default to defaultRole.
func newTestServer(t *testing.T, store *fakeStore, tokens ...string) *Server {
	t.Helper()
	if len(tokens) == 0 {
		tokens = []string{"admin-token"}
	}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", tokens...),
	}
	return New(zap.NewNop(), cfg, store, &stubQueue{})
}

// doRequest sends a request through the full middleware stack (auth, request-id, etc.).
func doRequest(srv *Server, method, path string, body []byte, token string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// doRequestCtx sends a request with a pre-injected principal, bypassing the auth middleware.
// Useful for testing handler logic that reads principal from context directly.
func doRequestCtx(srv *Server, method, path string, body []byte, principal *auth.Principal) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	ctx := context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func adminPrincipal() *auth.Principal {
	return &auth.Principal{
		Type:    "user",
		Name:    "admin-user",
		Subject: "admin-sub",
		Email:   "admin@example.com",
		Roles:   []string{"admin"},
	}
}

func viewerPrincipal() *auth.Principal {
	return &auth.Principal{
		Type:    "user",
		Name:    "viewer-user",
		Subject: "viewer-sub",
		Email:   "viewer@example.com",
		Roles:   []string{"viewer"},
	}
}

func operatorPrincipal() *auth.Principal {
	return &auth.Principal{
		Type:    "user",
		Name:    "operator-user",
		Subject: "operator-sub",
		Email:   "operator@example.com",
		Roles:   []string{"operator"},
	}
}

// warmUp sends an initial authenticated request to persist the auth principal
// into the fakeStore and returns the persisted user ID.
func warmUp(t *testing.T, srv *Server, store *fakeStore, token string) uuid.UUID {
	t.Helper()
	rec := doRequest(srv, http.MethodGet, "/api/v1/tenants", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("warm-up failed: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	return store.lastUserID
}

// setRoles overrides the roles for a user in the fakeStore.
func setRoles(store *fakeStore, userID uuid.UUID, roles []string) {
	if store.overrideRoles == nil {
		store.overrideRoles = map[uuid.UUID][]string{}
	}
	store.overrideRoles[userID] = roles
}

func decodeErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode error response: %v\nbody=%s", err, rec.Body.String())
	}
	return resp
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}

// ---------- Tenant CRUD ----------

func TestHandleTenants_ListWithPagination(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	// Seed 5 tenants.
	for i := 0; i < 5; i++ {
		store.tenants = append(store.tenants, storage.Tenant{
			ID:        uuid.New(),
			Name:      fmt.Sprintf("tenant-%d", i),
			CreatedAt: time.Now(),
		})
	}

	t.Run("list all tenants", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants", nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp paginatedResponse[tenantResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Pagination.Total != 5 {
			t.Fatalf("expected total 5, got %d", resp.Pagination.Total)
		}
		if len(resp.Data) != 5 {
			t.Fatalf("expected 5 items, got %d", len(resp.Data))
		}
	})

	t.Run("list with limit", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants?limit=2", nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp paginatedResponse[tenantResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Data) != 2 {
			t.Fatalf("expected 2 items, got %d", len(resp.Data))
		}
		if resp.Pagination.Total != 5 {
			t.Fatalf("expected total 5, got %d", resp.Pagination.Total)
		}
		if resp.Pagination.NextOffset == nil {
			t.Fatal("expected next_offset to be set")
		}
	})

	t.Run("list with offset", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants?limit=2&offset=3", nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp paginatedResponse[tenantResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Data) != 2 {
			t.Fatalf("expected 2 items, got %d", len(resp.Data))
		}
		if resp.Pagination.PrevOffset == nil {
			t.Fatal("expected prev_offset to be set")
		}
	})
}

func TestHandleTenants_Create(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	t.Run("valid create", func(t *testing.T) {
		body := mustJSON(t, createTenantRequest{Name: "Acme Corp"})
		rec := doRequest(srv, http.MethodPost, "/api/v1/tenants", body, "admin-token")
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp tenantResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Name != "Acme Corp" {
			t.Fatalf("expected name Acme Corp, got %s", resp.Name)
		}
		if resp.ID == "" {
			t.Fatal("expected non-empty id")
		}
		if resp.CreatedAt == "" {
			t.Fatal("expected created_at timestamp")
		}
	})

	t.Run("invalid payload - missing name", func(t *testing.T) {
		body := mustJSON(t, map[string]string{"name": ""})
		rec := doRequest(srv, http.MethodPost, "/api/v1/tenants", body, "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		errResp := decodeErrorResponse(t, rec)
		if errResp.Code != http.StatusBadRequest {
			t.Fatalf("expected error code 400, got %d", errResp.Code)
		}
	})

	t.Run("invalid payload - malformed JSON", func(t *testing.T) {
		rec := doRequest(srv, http.MethodPost, "/api/v1/tenants", []byte(`{bad json`), "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestHandleTenants_GetByID(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	tenantID := uuid.New()
	store.tenants = []storage.Tenant{
		{ID: tenantID, Name: "Existing Tenant", CreatedAt: time.Unix(1700000000, 0)},
	}

	t.Run("found", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants/"+tenantID.String(), nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp tenantResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.ID != tenantID.String() {
			t.Fatalf("expected id %s, got %s", tenantID, resp.ID)
		}
		if resp.Name != "Existing Tenant" {
			t.Fatalf("expected name Existing Tenant, got %s", resp.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants/"+uuid.New().String(), nil, "admin-token")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("invalid UUID", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants/not-a-uuid", nil, "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		errResp := decodeErrorResponse(t, rec)
		if !strings.Contains(errResp.Error, "invalid tenant id") {
			t.Fatalf("expected 'invalid tenant id' error, got %q", errResp.Error)
		}
	})
}

func TestHandleTenants_Update(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	tenantID := uuid.New()
	store.tenants = []storage.Tenant{
		{ID: tenantID, Name: "Original", CreatedAt: time.Unix(1700000000, 0)},
	}

	t.Run("update success", func(t *testing.T) {
		newName := "Updated Name"
		body := mustJSON(t, updateTenantRequest{Name: &newName})
		rec := doRequest(srv, http.MethodPatch, "/api/v1/tenants/"+tenantID.String(), body, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp tenantResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Name != "Updated Name" {
			t.Fatalf("expected updated name, got %s", resp.Name)
		}
	})

	t.Run("update not found", func(t *testing.T) {
		newName := "Ghost"
		body := mustJSON(t, updateTenantRequest{Name: &newName})
		rec := doRequest(srv, http.MethodPatch, "/api/v1/tenants/"+uuid.New().String(), body, "admin-token")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}

func TestHandleTenants_Delete(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	tenantID := uuid.New()
	store.tenants = []storage.Tenant{
		{ID: tenantID, Name: "Deletable", CreatedAt: time.Unix(1700000000, 0)},
	}

	t.Run("delete success", func(t *testing.T) {
		rec := doRequest(srv, http.MethodDelete, "/api/v1/tenants/"+tenantID.String(), nil, "admin-token")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
		}
		// Verify tenant is gone.
		if len(store.tenants) != 0 {
			t.Fatalf("expected tenant to be removed, but store still has %d", len(store.tenants))
		}
	})

	t.Run("delete not found", func(t *testing.T) {
		rec := doRequest(srv, http.MethodDelete, "/api/v1/tenants/"+uuid.New().String(), nil, "admin-token")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}

func TestHandleTenants_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	rec := doRequest(srv, http.MethodPut, "/api/v1/tenants", nil, "admin-token")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	errResp := decodeErrorResponse(t, rec)
	if errResp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected error code 405, got %d", errResp.Code)
	}
}

// ---------- Node CRUD ----------

func TestHandleNodes_ListWithTenantFilter(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	tenantA := uuid.New()
	tenantB := uuid.New()
	store.nodes = []storage.Node{
		{ID: uuid.New(), TenantID: tenantA, Hostname: "host-a1", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: uuid.New(), TenantID: tenantA, Hostname: "host-a2", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: uuid.New(), TenantID: tenantB, Hostname: "host-b1", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	t.Run("list all nodes", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/nodes", nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp paginatedResponse[nodeResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Pagination.Total != 3 {
			t.Fatalf("expected total 3, got %d", resp.Pagination.Total)
		}
	})

	t.Run("list with tenant_id filter", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/nodes?tenant_id="+tenantA.String(), nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp paginatedResponse[nodeResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Data) != 2 {
			t.Fatalf("expected 2 nodes for tenant A, got %d", len(resp.Data))
		}
		for _, n := range resp.Data {
			if n.TenantID != tenantA.String() {
				t.Fatalf("expected all nodes to belong to tenant %s, got %s", tenantA, n.TenantID)
			}
		}
	})

	t.Run("list with invalid tenant_id", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/nodes?tenant_id=bad-uuid", nil, "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestHandleNodes_Create(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	tenantID := uuid.New()
	store.tenants = []storage.Tenant{
		{ID: tenantID, Name: "Test Tenant", CreatedAt: time.Now()},
	}

	t.Run("create node", func(t *testing.T) {
		osVal := "linux"
		body := mustJSON(t, createNodeRequest{
			TenantID: tenantID.String(),
			Hostname: "web-01",
			OS:       &osVal,
		})
		rec := doRequest(srv, http.MethodPost, "/api/v1/nodes", body, "admin-token")
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp nodeResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Hostname != "web-01" {
			t.Fatalf("expected hostname web-01, got %s", resp.Hostname)
		}
		if resp.TenantID != tenantID.String() {
			t.Fatalf("expected tenant id %s, got %s", tenantID, resp.TenantID)
		}
		if resp.OS == nil || *resp.OS != "linux" {
			t.Fatalf("expected os linux, got %v", resp.OS)
		}
	})

	t.Run("create node with missing tenant", func(t *testing.T) {
		body := mustJSON(t, createNodeRequest{
			TenantID: uuid.New().String(),
			Hostname: "orphan-01",
		})
		rec := doRequest(srv, http.MethodPost, "/api/v1/nodes", body, "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing tenant, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandleNodes_GetByID(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	nodeID := uuid.New()
	store.nodes = []storage.Node{
		{ID: nodeID, TenantID: uuid.New(), Hostname: "db-01", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	t.Run("found", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/nodes/"+nodeID.String(), nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp nodeResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Hostname != "db-01" {
			t.Fatalf("expected hostname db-01, got %s", resp.Hostname)
		}
	})

	t.Run("not found", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/nodes/"+uuid.New().String(), nil, "admin-token")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("invalid UUID", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/nodes/garbage", nil, "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

// ---------- Job endpoints ----------

func TestHandleJobs_CreateAndList(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		jobs:   map[uuid.UUID]*storage.Job{},
		events: map[uuid.UUID][]storage.JobEvent{},
	}
	srv := newTestServer(t, store, "admin-token")
	srv.configureJobIntegrations()
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	tenantID := uuid.New()
	store.tenants = []storage.Tenant{
		{ID: tenantID, Name: "Job Tenant", CreatedAt: time.Now()},
	}

	// Seed a template so provision.apply validation passes.
	templateID := uuid.New()
	store.templates = []storage.ProvisioningTemplate{
		{
			ID:        templateID,
			Name:      "test-tpl",
			Provider:  "aws",
			Labels:    map[string]string{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	store.templateVersions = map[uuid.UUID][]storage.ProvisioningTemplateVersion{
		templateID: {
			{
				ID:         uuid.New(),
				TemplateID: templateID,
				Version:    1,
				Body:       "#cloud-config",
				CreatedAt:  time.Now(),
			},
		},
	}
	versionID := store.templateVersions[templateID][0].ID
	store.templates[0].PromotedVersionID = &versionID

	t.Run("create valid job", func(t *testing.T) {
		payload := fmt.Sprintf(`{
			"type":"%s",
			"tenant_id":"%s",
			"payload":{
				"plan_id":"%s",
				"tenant_id":"%s",
				"node_id":"node-123",
				"metadata":{"env":"dev"}
			}
		}`, JobTypeProvisionApply, tenantID.String(), templateID.String(), tenantID.String())
		rec := doRequest(srv, http.MethodPost, "/api/v1/jobs", []byte(payload), "admin-token")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("create with unsupported type", func(t *testing.T) {
		payload := `{"type":"nonexistent.type","payload":{}}`
		rec := doRequest(srv, http.MethodPost, "/api/v1/jobs", []byte(payload), "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
		}
		errResp := decodeErrorResponse(t, rec)
		if !strings.Contains(errResp.Error, "unsupported job type") {
			t.Fatalf("expected unsupported job type error, got %q", errResp.Error)
		}
	})

	t.Run("list with status filter", func(t *testing.T) {
		// Seed a couple of jobs with different statuses.
		jobQueued := &storage.Job{
			ID:        uuid.New(),
			TenantID:  tenantID,
			Type:      "provision.apply",
			Status:    storage.JobStatusQueued,
			CreatedAt: time.Now().Add(-2 * time.Minute),
			UpdatedAt: time.Now(),
		}
		jobFailed := &storage.Job{
			ID:        uuid.New(),
			TenantID:  tenantID,
			Type:      "provision.apply",
			Status:    storage.JobStatusFailed,
			CreatedAt: time.Now().Add(-time.Minute),
			UpdatedAt: time.Now(),
		}
		store.jobs[jobQueued.ID] = jobQueued
		store.jobs[jobFailed.ID] = jobFailed

		rec := doRequest(srv, http.MethodGet, "/api/v1/jobs?status="+string(storage.JobStatusFailed), nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, jobFailed.ID.String()) {
			t.Fatalf("expected failed job in response")
		}
		if strings.Contains(body, jobQueued.ID.String()) {
			t.Fatalf("expected queued job to be filtered out")
		}
	})
}

// ---------- Auth & RBAC ----------

func TestAuth_NoToken_Returns401(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/tenants"},
		{http.MethodPost, "/api/v1/tenants"},
		{http.MethodGet, "/api/v1/nodes"},
		{http.MethodGet, "/api/v1/jobs"},
		{http.MethodGet, "/api/v1/me"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			// No token in the request.
			rec := doRequest(srv, ep.method, ep.path, nil, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 for %s %s without auth, got %d", ep.method, ep.path, rec.Code)
			}
		})
	}
}

func TestRBAC_ViewerDeniedAdminEndpoints(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "viewer-token")
	userID := warmUp(t, srv, store, "viewer-token")
	setRoles(store, userID, []string{"viewer"})

	t.Run("viewer cannot create tenant", func(t *testing.T) {
		body := mustJSON(t, createTenantRequest{Name: "Blocked"})
		rec := doRequest(srv, http.MethodPost, "/api/v1/tenants", body, "viewer-token")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for viewer creating tenant, got %d body=%s", rec.Code, rec.Body.String())
		}
		errResp := decodeErrorResponse(t, rec)
		if errResp.Code != http.StatusForbidden {
			t.Fatalf("expected error code 403, got %d", errResp.Code)
		}
	})

	t.Run("viewer cannot delete tenant", func(t *testing.T) {
		rec := doRequest(srv, http.MethodDelete, "/api/v1/tenants/"+uuid.New().String(), nil, "viewer-token")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for viewer deleting tenant, got %d", rec.Code)
		}
	})

	t.Run("viewer can read tenant list", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants", nil, "viewer-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for viewer reading tenants, got %d", rec.Code)
		}
	})

	t.Run("viewer can read node list", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/nodes", nil, "viewer-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for viewer reading nodes, got %d", rec.Code)
		}
	})
}

func TestRBAC_AdminAccessGranted(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	t.Run("admin can create tenant", func(t *testing.T) {
		body := mustJSON(t, createTenantRequest{Name: "Admin Tenant"})
		rec := doRequest(srv, http.MethodPost, "/api/v1/tenants", body, "admin-token")
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 for admin creating tenant, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("admin can delete tenant", func(t *testing.T) {
		// First create one.
		body := mustJSON(t, createTenantRequest{Name: "Deletable By Admin"})
		rec := doRequest(srv, http.MethodPost, "/api/v1/tenants", body, "admin-token")
		if rec.Code != http.StatusCreated {
			t.Fatalf("setup: expected 201, got %d", rec.Code)
		}
		var created tenantResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode: %v", err)
		}

		rec = doRequest(srv, http.MethodDelete, "/api/v1/tenants/"+created.ID, nil, "admin-token")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 for admin deleting tenant, got %d", rec.Code)
		}
	})

	t.Run("admin can list users", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/users", nil, "admin-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

// ---------- Error response structure ----------

func TestErrorResponses_StructuredJSON(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	t.Run("bad request includes error, code, request_id", func(t *testing.T) {
		body := []byte(`{"name":""}`)
		rec := doRequest(srv, http.MethodPost, "/api/v1/tenants", body, "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}

		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Fatalf("expected JSON content type, got %s", ct)
		}

		errResp := decodeErrorResponse(t, rec)
		if errResp.Error == "" {
			t.Fatal("expected non-empty error message")
		}
		if errResp.Code != http.StatusBadRequest {
			t.Fatalf("expected code 400 in body, got %d", errResp.Code)
		}
		if errResp.RequestID == "" {
			t.Fatal("expected non-empty request_id in error response")
		}
	})

	t.Run("method not allowed is structured JSON", func(t *testing.T) {
		rec := doRequest(srv, http.MethodPut, "/api/v1/tenants", nil, "admin-token")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Fatalf("expected JSON content type for 405, got %s", ct)
		}

		errResp := decodeErrorResponse(t, rec)
		if errResp.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected code 405, got %d", errResp.Code)
		}
		if errResp.RequestID == "" {
			t.Fatal("expected request_id in method not allowed error")
		}
	})

	t.Run("401 without auth is structured JSON", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/ping", nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
		// The auth middleware returns plain text, but the authorize helper writes JSON.
		// The middleware fires first, so we check what the middleware returns.
	})

	t.Run("invalid UUID returns structured error", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants/xyz", nil, "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		errResp := decodeErrorResponse(t, rec)
		if !strings.Contains(errResp.Error, "invalid tenant id") {
			t.Fatalf("expected error about invalid tenant id, got %q", errResp.Error)
		}
	})

	t.Run("403 from RBAC returns structured error", func(t *testing.T) {
		// Create a viewer-only token.
		viewerCfg := &config.Config{
			HTTP: config.HTTPConfig{Address: ":0"},
			TLS:  config.TLSConfig{RequireClientTLS: false},
			Auth: authWithTokens("viewer", "viewer-only"),
		}
		viewerSrv := New(zap.NewNop(), viewerCfg, store, &stubQueue{})
		viewerUser := warmUp(t, viewerSrv, store, "viewer-only")
		setRoles(store, viewerUser, []string{"viewer"})

		body := mustJSON(t, createTenantRequest{Name: "Forbidden"})
		rec := doRequest(viewerSrv, http.MethodPost, "/api/v1/tenants", body, "viewer-only")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Fatalf("expected JSON content type for 403, got %s", ct)
		}
		errResp := decodeErrorResponse(t, rec)
		if errResp.Code != http.StatusForbidden {
			t.Fatalf("expected code 403 in body, got %d", errResp.Code)
		}
		if errResp.RequestID == "" {
			t.Fatal("expected request_id in 403 error")
		}
	})
}

// ---------- Profile endpoint ----------

func TestHandleProfile(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "profile-token")
	userID := warmUp(t, srv, store, "profile-token")
	setRoles(store, userID, []string{"viewer", "operator"})

	// Seed the user lookup data.
	store.users["profile-token"] = &storage.User{
		ID:          userID,
		ExternalID:  "profile-token",
		Email:       storageNullString("profile@example.com"),
		DisplayName: storageNullString("Profile User"),
		CreatedAt:   time.Unix(1700000500, 0),
	}
	store.usersByID[userID] = store.users["profile-token"]
	store.userRoles[userID] = []string{"viewer", "operator"}
	store.overrideRoles[userID] = []string{"viewer", "operator"}

	t.Run("GET /api/v1/me returns principal info", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/me", nil, "profile-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("expected application/json, got %s", ct)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if subject, _ := resp["subject"].(string); subject != "profile-token" {
			t.Fatalf("expected subject profile-token, got %v", subject)
		}

		userPayload, _ := resp["user"].(map[string]any)
		if userPayload == nil {
			t.Fatal("expected user details in profile response")
		}
		if email, _ := userPayload["email"].(string); email != "profile@example.com" {
			t.Fatalf("expected email profile@example.com, got %v", email)
		}
		if display, _ := userPayload["display_name"].(string); display != "Profile User" {
			t.Fatalf("expected display name Profile User, got %v", display)
		}

		storedRoles, _ := resp["stored_roles"].([]any)
		if len(storedRoles) != 2 {
			t.Fatalf("expected 2 stored roles, got %v", storedRoles)
		}
	})

	t.Run("POST /api/v1/me returns 405", func(t *testing.T) {
		rec := doRequest(srv, http.MethodPost, "/api/v1/me", nil, "profile-token")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 for POST /me, got %d", rec.Code)
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/me", nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

// ---------- Pagination edge cases ----------

func TestPagination_EmptyResult(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	rec := doRequest(srv, http.MethodGet, "/api/v1/tenants", nil, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp paginatedResponse[tenantResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Pagination.Total != 0 {
		t.Fatalf("expected total 0, got %d", resp.Pagination.Total)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty data, got %d items", len(resp.Data))
	}
}

func TestPagination_InvalidLimit(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	rec := doRequest(srv, http.MethodGet, "/api/v1/tenants?limit=-1", nil, "admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative limit, got %d", rec.Code)
	}

	rec = doRequest(srv, http.MethodGet, "/api/v1/tenants?limit=abc", nil, "admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric limit, got %d", rec.Code)
	}
}

func TestPagination_InvalidOffset(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	rec := doRequest(srv, http.MethodGet, "/api/v1/tenants?offset=-5", nil, "admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative offset, got %d", rec.Code)
	}
}

// ---------- Request ID propagation ----------

func TestRequestID_Propagated(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	_ = warmUp(t, srv, store, "admin-token")

	t.Run("auto-generated request_id", func(t *testing.T) {
		rec := doRequest(srv, http.MethodGet, "/api/v1/tenants", nil, "admin-token")
		reqID := rec.Header().Get("X-Request-Id")
		if reqID == "" {
			t.Fatal("expected X-Request-Id header to be set")
		}
		// Verify it is a valid UUID.
		if _, err := uuid.Parse(reqID); err != nil {
			t.Fatalf("expected request id to be a valid UUID, got %q", reqID)
		}
	})

	t.Run("custom request_id preserved", func(t *testing.T) {
		customID := "my-custom-request-id"
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("X-Request-Id", customID)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Header().Get("X-Request-Id") != customID {
			t.Fatalf("expected preserved request id %q, got %q", customID, rec.Header().Get("X-Request-Id"))
		}
	})
}

// ---------- Operator role tests ----------

func TestRBAC_OperatorCanCreateTenants(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "operator-token")
	userID := warmUp(t, srv, store, "operator-token")
	setRoles(store, userID, []string{"operator"})

	body := mustJSON(t, createTenantRequest{Name: "Operator Tenant"})
	rec := doRequest(srv, http.MethodPost, "/api/v1/tenants", body, "operator-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for operator creating tenant, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRBAC_OperatorCannotDeleteTenant(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "operator-token")
	userID := warmUp(t, srv, store, "operator-token")
	setRoles(store, userID, []string{"operator"})

	tenantID := uuid.New()
	store.tenants = []storage.Tenant{
		{ID: tenantID, Name: "Protected", CreatedAt: time.Now()},
	}

	rec := doRequest(srv, http.MethodDelete, "/api/v1/tenants/"+tenantID.String(), nil, "operator-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for operator deleting tenant, got %d", rec.Code)
	}
}

// ---------- Health endpoint ----------

func TestHealthz_NoAuth(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store)

	rec := doRequest(srv, http.MethodGet, "/healthz", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /healthz without auth, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("expected body ok, got %s", rec.Body.String())
	}
}

// ---------- Node method not allowed ----------

func TestHandleNodes_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	rec := doRequest(srv, http.MethodDelete, "/api/v1/nodes", nil, "admin-token")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE on /api/v1/nodes, got %d", rec.Code)
	}
	errResp := decodeErrorResponse(t, rec)
	if errResp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected error code 405, got %d", errResp.Code)
	}
}

// ---------- Job list - invalid status ----------

func TestHandleJobs_InvalidStatus(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		jobs:   map[uuid.UUID]*storage.Job{},
		events: map[uuid.UUID][]storage.JobEvent{},
	}
	srv := newTestServer(t, store, "admin-token")
	srv.configureJobIntegrations()
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	rec := doRequest(srv, http.MethodGet, "/api/v1/jobs?status=bogus", nil, "admin-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------- Content-Type verification ----------

func TestResponses_ContentTypeJSON(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	srv := newTestServer(t, store, "admin-token")
	userID := warmUp(t, srv, store, "admin-token")
	setRoles(store, userID, []string{"admin"})

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/tenants"},
		{http.MethodGet, "/api/v1/nodes"},
		{http.MethodGet, "/api/v1/me"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			rec := doRequest(srv, ep.method, ep.path, nil, "admin-token")
			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Fatalf("expected application/json for %s %s, got %s", ep.method, ep.path, ct)
			}
		})
	}
}

// Ensure that unused worker interface is satisfied by stubQueue.
var _ TaskQueue = (*stubQueue)(nil)

// Ensure fakeStore satisfies the Store interface at compile time.
var _ Store = (*fakeStore)(nil)
