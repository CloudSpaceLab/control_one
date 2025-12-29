package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

func TestPingEndpointAuthentication(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Address:      ":0",
			ReadTimeout:  0,
			WriteTimeout: 0,
		},
		TLS: config.TLSConfig{
			RequireClientTLS: false,
		},
		Observability: config.ObservabilityConfig{
			EnableMetrics: true,
			MetricsPath:   "/metrics",
		},
		Worker: config.WorkerConfig{},
		Auth: config.AuthConfig{
			RBAC: config.RBACConfig{DefaultRole: "admin"},
		},
	}

	srv := New(logger, cfg, nil, nil)
	handler := srv.Handler()

	t.Run("unauthenticated requests are rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status %d got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("bearer token is accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status %d got %d", http.StatusOK, rec.Code)
		}

		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected application/json content-type got %s", ct)
		}

		body := rec.Body.String()
		if !contains(body, "test-token") {
			t.Fatalf("expected body to contain principal token, got %s", body)
		}
	})
}

func TestTemplateEndpoints(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: config.AuthConfig{
			RBAC: config.RBACConfig{DefaultRole: "viewer"},
		},
	}

	store := &fakeStore{}
	srv := New(logger, cfg, store, &stubQueue{})

	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer subject-templates")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	// Warm up to persist user and grant admin role.
	rec := call(http.MethodGet, "/api/v1/tenants", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected tenants call success, got %d", rec.Code)
	}
	if store.lastUserID == uuid.Nil {
		t.Fatalf("expected user to be persisted")
	}
	store.overrideRoles = map[uuid.UUID][]string{
		store.lastUserID: {"admin"},
	}

	createPayload := map[string]any{
		"name":        "web-template",
		"provider":    "aws",
		"description": "Sample template",
		"labels": map[string]string{
			"env": "dev",
		},
	}
	body, _ := json.Marshal(createPayload)
	rec = call(http.MethodPost, "/api/v1/templates", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create template 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var created templateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected template id")
	}

	rec = call(http.MethodGet, "/api/v1/templates", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list template 200 got %d", rec.Code)
	}
	var listResp struct {
		Data []templateResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 template, got %d", len(listResp.Data))
	}

	versionPayload := map[string]any{
		"body":     "#cloud-config",
		"checksum": "abc123",
	}
	body, _ = json.Marshal(versionPayload)
	path := fmt.Sprintf("/api/v1/templates/%s/versions", created.ID)
	rec = call(http.MethodPost, path, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create version 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var version templateVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if version.Version != 1 {
		t.Fatalf("expected version 1, got %d", version.Version)
	}

	promotePath := fmt.Sprintf("/api/v1/templates/%s/versions/1/promote", created.ID)
	rec = call(http.MethodPost, promotePath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected promote 200 got %d", rec.Code)
	}

	rec = call(http.MethodGet, "/api/v1/templates/"+created.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected get template 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var detail templateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if detail.PromotedVersionID == nil || detail.PromotedVersion == nil {
		t.Fatalf("expected promoted version metadata in detail response")
	}
}

func TestEnrichProvisioningMetadata(t *testing.T) {
	logger := zap.NewNop()
	store := &fakeStore{}
	srv := New(logger, &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, &stubQueue{})

	templateID := uuid.New()
	store.templates = []storage.ProvisioningTemplate{
		{
			ID:        templateID,
			Name:      "web",
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
				Checksum:   sql.NullString{String: "sha256:abc", Valid: true},
				Body:       "#cloud-config",
				CreatedAt:  time.Now(),
			},
			{
				ID:         uuid.New(),
				TemplateID: templateID,
				Version:    2,
				Checksum:   sql.NullString{String: "sha256:def", Valid: true},
				Body:       "#cloud-config v2",
				CreatedAt:  time.Now(),
			},
		},
	}
	versionID := store.templateVersions[templateID][1].ID
	store.templates[0].PromotedVersionID = &versionID

	t.Run("uses promoted version when template_version absent", func(t *testing.T) {
		meta := map[string]string{}
		srv.enrichProvisioningMetadata(context.Background(), templateID.String(), meta)
		if meta["template_version"] != "2" {
			t.Fatalf("expected promoted version 2, got %s", meta["template_version"])
		}
		if meta["template_checksum"] != "sha256:def" {
			t.Fatalf("expected checksum sha256:def, got %s", meta["template_checksum"])
		}
	})

	t.Run("uses explicit version when provided", func(t *testing.T) {
		meta := map[string]string{"template_version": "1"}
		srv.enrichProvisioningMetadata(context.Background(), templateID.String(), meta)
		if meta["template_version"] != "1" {
			t.Fatalf("expected version 1, got %s", meta["template_version"])
		}
		if meta["template_checksum"] != "sha256:abc" {
			t.Fatalf("expected checksum sha256:abc, got %s", meta["template_checksum"])
		}
	})
}

func TestProfileEndpoint(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: config.AuthConfig{
			RBAC: config.RBACConfig{DefaultRole: "viewer"},
		},
	}

	bearerReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		return req
	}

	t.Run("returns inline principal without backing store", func(t *testing.T) {
		srv := New(logger, cfg, nil, nil)
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, bearerReq())

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}

		var resp profileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid json: %v", err)
		}
		if resp.Subject != "test-token" {
			t.Fatalf("expected subject propagated, got %s", resp.Subject)
		}
		if resp.User != nil {
			t.Fatalf("expected user payload omitted when store unavailable, got %+v", resp.User)
		}
		if len(resp.StoredRoles) != 0 {
			t.Fatalf("expected stored roles omitted, got %v", resp.StoredRoles)
		}
	})

	t.Run("returns stored roles and persisted metadata", func(t *testing.T) {
		userID := uuid.New()
		store := &fakeStore{
			users: map[string]*storage.User{
				"test-token": {
					ID:          userID,
					ExternalID:  "test-token",
					Email:       storageNullString("stored@example.com"),
					DisplayName: storageNullString("Stored User"),
					CreatedAt:   time.Unix(1700000600, 0),
				},
			},
			userRoles: map[uuid.UUID][]string{
				userID: {"viewer", "operator"},
			},
			overrideRoles: map[uuid.UUID][]string{
				userID: {"viewer", "operator"},
			},
		}
		srv := New(logger, cfg, store, nil)
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, bearerReq())

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}

		var resp profileResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid json: %v", err)
		}
		if resp.User == nil || resp.User.Email == nil || *resp.User.Email != "stored@example.com" {
			t.Fatalf("expected stored email propagated, got %+v", resp.User)
		}
		if resp.User.DisplayName == nil || *resp.User.DisplayName != "Stored User" {
			t.Fatalf("expected stored display name propagated, got %+v", resp.User)
		}
		if len(resp.StoredRoles) != 2 {
			t.Fatalf("expected stored roles returned, got %v", resp.StoredRoles)
		}
	})

	t.Run("omits stored metadata when user not persisted", func(t *testing.T) {
		store := &fakeStore{skipUserPersistence: true}
		srv := New(logger, cfg, store, nil)
		rec := httptest.NewRecorder()

		srv.Handler().ServeHTTP(rec, bearerReq())

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid json: %v", err)
		}
		if _, ok := resp["user"]; ok {
			t.Fatalf("expected user field omitted when not stored, got %v", resp["user"])
		}
		if _, ok := resp["stored_roles"]; ok {
			t.Fatalf("expected stored roles omitted when user missing, got %v", resp["stored_roles"])
		}
	})
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestRBACAuthorization(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: config.AuthConfig{RBAC: config.RBACConfig{DefaultRole: "viewer"}},
	}

	store := &fakeStore{userRoles: map[uuid.UUID][]string{}}
	srv := New(logger, cfg, store, &stubQueue{})

	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer subject-123")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	t.Run("viewer can access tenant list", func(t *testing.T) {
		rec := call(http.MethodGet, "/api/v1/tenants", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
	})

	t.Run("viewer denied control plane operations", func(t *testing.T) {
		rec := call(http.MethodPost, "/api/v1/tenants", []byte(`{"name":"Tenant X"}`))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d", rec.Code)
		}
	})

	if store.lastUserID == uuid.Nil {
		t.Fatalf("expected user to be persisted")
	}
	store.overrideRoles = map[uuid.UUID][]string{store.lastUserID: {"admin"}}
	t.Run("admin role grants access", func(t *testing.T) {
		rec := call(http.MethodPost, "/api/v1/tenants", []byte(`{"name":"Tenant Y"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 got %d", rec.Code)
		}
	})
}

func TestNodesEndpoints(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: config.AuthConfig{
			RBAC: config.RBACConfig{DefaultRole: "admin"},
		},
	}

	store := &fakeStore{}
	srv := New(logger, cfg, store, &stubQueue{})
	srv.jobHandlers = map[string]jobHandler{
		JobTypeProvisionApply: func(ctx context.Context, job *storage.Job) error {
			return nil
		},
	}

	bearerReq := func(method, path string, body []byte) *http.Request {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		return req
	}

	tenantID := uuid.New()
	store.tenants = []storage.Tenant{{ID: tenantID, Name: "Tenant A", CreatedAt: time.Unix(1700000000, 0)}}
	t.Run("GET /api/v1/nodes returns nodes", func(t *testing.T) {
		store.nodes = []storage.Node{
			{ID: uuid.New(), TenantID: tenantID, Hostname: "node-1", CreatedAt: time.Unix(1700000000, 0), UpdatedAt: time.Unix(1700000000, 0)},
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodGet, "/api/v1/nodes", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected json response, got %s", ct)
		}
		if !contains(rec.Body.String(), "node-1") {
			t.Fatalf("expected response to contain hostname: %s", rec.Body.String())
		}
	})

	t.Run("GET /api/v1/nodes filters by tenant", func(t *testing.T) {
		otherTenant := uuid.New()
		store.nodes = []storage.Node{
			{ID: uuid.New(), TenantID: tenantID, Hostname: "primary", CreatedAt: time.Unix(1700000001, 0), UpdatedAt: time.Unix(1700000001, 0)},
			{ID: uuid.New(), TenantID: otherTenant, Hostname: "secondary", CreatedAt: time.Unix(1700000002, 0), UpdatedAt: time.Unix(1700000002, 0)},
		}
		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/nodes?tenant_id="+tenantID.String(), nil)
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		body := rec.Body.String()
		if !contains(body, "primary") || contains(body, "secondary") {
			t.Fatalf("expected filtered response, got %s", body)
		}
	})

	t.Run("GET /api/v1/nodes/:id returns node detail", func(t *testing.T) {
		targetNode := storage.Node{ID: uuid.New(), TenantID: tenantID, Hostname: "detail-node", CreatedAt: time.Unix(1700000010, 0), UpdatedAt: time.Unix(1700000010, 0)}
		store.nodes = []storage.Node{targetNode}

		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/nodes/"+targetNode.ID.String(), nil)
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		if !contains(rec.Body.String(), "detail-node") {
			t.Fatalf("expected node detail response, got %s", rec.Body.String())
		}

		t.Run("returns 404 for missing node", func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := bearerReq(http.MethodGet, "/api/v1/nodes/"+uuid.New().String(), nil)
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 got %d", rec.Code)
			}
		})
	})

	t.Run("POST /api/v1/nodes creates node", func(t *testing.T) {
		payload := map[string]any{
			"tenant_id": tenantID.String(),
			"hostname":  "node-2",
			"os":        "linux",
		}
		body, _ := json.Marshal(payload)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/nodes", body))

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 got %d", rec.Code)
		}
		if store.createdNode == nil || store.createdNode.Hostname != "node-2" {
			t.Fatalf("expected store to create node-2, got %#v", store.createdNode)
		}
	})

	t.Run("POST /api/v1/nodes validates payload", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/nodes", []byte(`{"hostname":""}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/nodes rejects unknown tenant", func(t *testing.T) {
		payload := map[string]any{
			"tenant_id": uuid.New().String(),
			"hostname":  "node-3",
		}
		body, _ := json.Marshal(payload)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/nodes", body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/tenants returns tenants", func(t *testing.T) {
		store.tenants = []storage.Tenant{
			{ID: tenantID, Name: "Tenant A", CreatedAt: time.Unix(1700000003, 0)},
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodGet, "/api/v1/tenants", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		if !contains(rec.Body.String(), "Tenant A") {
			t.Fatalf("expected tenant in response: %s", rec.Body.String())
		}
	})

	t.Run("GET /api/v1/tenants paginates results", func(t *testing.T) {
		store.tenants = []storage.Tenant{
			{ID: uuid.New(), Name: "Tenant B", CreatedAt: time.Unix(1700000004, 0)},
			{ID: uuid.New(), Name: "Tenant C", CreatedAt: time.Unix(1700000005, 0)},
			{ID: uuid.New(), Name: "Tenant D", CreatedAt: time.Unix(1700000006, 0)},
		}
		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/tenants?limit=2&offset=1", nil)
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		body := rec.Body.String()
		if contains(body, "Tenant B") || !contains(body, "Tenant C") || !contains(body, "Tenant D") {
			t.Fatalf("expected windowed tenants, got %s", body)
		}
	})

	t.Run("GET /api/v1/tenants validates limit", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/tenants?limit=abc", nil)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/tenants creates tenant", func(t *testing.T) {
		payload := []byte(`{"name":"Tenant B"}`)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/tenants", payload))

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 got %d", rec.Code)
		}
		if store.createdTenant == nil || store.createdTenant.Name != "Tenant B" {
			t.Fatalf("expected tenant creation, got %#v", store.createdTenant)
		}
	})

	t.Run("POST /api/v1/tenants validates payload", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/tenants", []byte(`{"name":""}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/jobs enqueues job", func(t *testing.T) {
		store.tenants = []storage.Tenant{
			{ID: tenantID, Name: "Tenant A", CreatedAt: time.Unix(1700000000, 0)},
		}
		body := fmt.Sprintf(`{
			"type":"%s",
			"tenant_id":"%s",
			"payload":{
				"plan_id":"plan-1",
				"tenant_id":"%s",
				"node_id":"node-123",
				"metadata":{"env":"dev"}
			}
		}`, JobTypeProvisionApply, tenantID.String(), tenantID.String())
		payload := []byte(body)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/jobs validates tenant existence", func(t *testing.T) {
		tenant := uuid.New()
		payload := []byte(fmt.Sprintf(`{"type":"provision.apply","tenant_id":"%s","payload":{"plan_id":"plan-1","tenant_id":"%s","node_id":"node-999"}}`, tenant.String(), tenant.String()))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/jobs rejects tenant mismatch", func(t *testing.T) {
		otherTenant := uuid.New()
		payload := []byte(fmt.Sprintf(`{"type":"provision.apply","tenant_id":"%s","payload":{"plan_id":"plan-1","tenant_id":"%s","node_id":"node-abc"}}`, tenantID.String(), otherTenant.String()))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/jobs/:id returns job state", func(t *testing.T) {
		job, _ := store.CreateJob(context.Background(), &storage.Job{Type: "provision"}, &storage.JobEvent{Status: storage.JobStatusQueued})
		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/jobs/"+job.ID.String(), nil)
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		if !contains(rec.Body.String(), job.ID.String()) {
			t.Fatalf("expected response to contain job id")
		}
	})

	t.Run("GET /api/v1/jobs filters by status", func(t *testing.T) {
		jobA, _ := store.CreateJob(context.Background(), &storage.Job{Type: "provision.apply", Status: storage.JobStatusQueued, CreatedAt: time.Now().Add(-2 * time.Minute)}, nil)
		jobB, _ := store.CreateJob(context.Background(), &storage.Job{Type: "provision.apply", Status: storage.JobStatusFailed, CreatedAt: time.Now().Add(-time.Minute)}, nil)

		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/jobs?status="+string(storage.JobStatusFailed), nil)
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		body := rec.Body.String()
		if !contains(body, jobB.ID.String()) {
			t.Fatalf("expected failed job in response, got %s", body)
		}
		if contains(body, jobA.ID.String()) {
			t.Fatalf("expected queued job to be filtered out, got %s", body)
		}
	})

	t.Run("GET /api/v1/me returns principal profile", func(t *testing.T) {
		userID := uuid.New()
		store.users = map[string]*storage.User{
			"test-token": {
				ID:          userID,
				ExternalID:  "test-token",
				Email:       storageNullString("stored@example.com"),
				DisplayName: storageNullString("Stored User"),
				CreatedAt:   time.Unix(1700000500, 0),
			},
		}
		store.userRoles = map[uuid.UUID][]string{
			userID: {"viewer", "operator"},
		}
		store.overrideRoles = map[uuid.UUID][]string{
			userID: {"viewer", "operator"},
		}

		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/me", nil)
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected application/json got %s", ct)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid json: %v", err)
		}
		if subject, _ := resp["subject"].(string); subject != "test-token" {
			t.Fatalf("expected subject test-token got %v", subject)
		}

		storedRoles, _ := resp["stored_roles"].([]any)
		if len(storedRoles) != 2 {
			t.Fatalf("expected stored roles to be returned, got %v", storedRoles)
		}

		userPayload, _ := resp["user"].(map[string]any)
		if userPayload == nil {
			t.Fatalf("expected user payload")
		}
		if email, _ := userPayload["email"].(string); email != "stored@example.com" {
			t.Fatalf("expected stored email propagated, got %v", email)
		}
		if display, _ := userPayload["display_name"].(string); display != "Stored User" {
			t.Fatalf("expected display name propagated, got %v", display)
		}
	})
}

type fakeStore struct {
	nodes               []storage.Node
	tenants             []storage.Tenant
	createdNode         *storage.Node
	createdTenant       *storage.Tenant
	jobs                map[uuid.UUID]*storage.Job
	events              map[uuid.UUID][]storage.JobEvent
	users               map[string]*storage.User
	userRoles           map[uuid.UUID][]string
	lastUserID          uuid.UUID
	overrideRoles       map[uuid.UUID][]string
	skipUserPersistence bool
	templates           []storage.ProvisioningTemplate
	templateVersions    map[uuid.UUID][]storage.ProvisioningTemplateVersion
}

type stubQueue struct{}

func (s *stubQueue) Enqueue(worker.Task) error {
	return nil
}

func (f *fakeStore) CreateNode(_ context.Context, node *storage.Node) (*storage.Node, error) {
	if node.ID == uuid.Nil {
		node.ID = uuid.New()
	}
	if node.CreatedAt.IsZero() {
		node.CreatedAt = time.Now()
	}
	if node.UpdatedAt.IsZero() {
		node.UpdatedAt = node.CreatedAt
	}
	f.createdNode = node
	f.nodes = append(f.nodes, *node)
	return node, nil
}

func (f *fakeStore) GetNodeByHostname(_ context.Context, tenantID uuid.UUID, hostname string) (*storage.Node, error) {
	hostname = strings.TrimSpace(hostname)
	for _, node := range f.nodes {
		if node.TenantID == tenantID && strings.EqualFold(node.Hostname, hostname) {
			copy := node
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListProvisioningTemplates(_ context.Context, filter storage.ProvisioningTemplateFilter, limit, offset int) ([]storage.ProvisioningTemplate, int, error) {
	var filtered []storage.ProvisioningTemplate
	for _, tpl := range f.templates {
		if !filter.IncludeArchived && tpl.ArchivedAt.Valid {
			continue
		}
		if filter.Provider != "" && !strings.EqualFold(filter.Provider, tpl.Provider) {
			continue
		}
		if filter.NamePrefix != "" && !strings.HasPrefix(strings.ToLower(tpl.Name), strings.ToLower(filter.NamePrefix)) {
			continue
		}
		filtered = append(filtered, tpl)
	}
	total := len(filtered)
	if offset > total {
		return []storage.ProvisioningTemplate{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) CreateProvisioningTemplate(_ context.Context, tpl *storage.ProvisioningTemplate) (*storage.ProvisioningTemplate, error) {
	if tpl.ID == uuid.Nil {
		tpl.ID = uuid.New()
	}
	if tpl.CreatedAt.IsZero() {
		tpl.CreatedAt = time.Now()
	}
	if tpl.UpdatedAt.IsZero() {
		tpl.UpdatedAt = tpl.CreatedAt
	}
	f.templates = append(f.templates, *tpl)
	return tpl, nil
}

func (f *fakeStore) GetProvisioningTemplate(_ context.Context, id uuid.UUID) (*storage.ProvisioningTemplate, error) {
	for _, tpl := range f.templates {
		if tpl.ID == id {
			copy := tpl
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) CreateProvisioningTemplateVersion(_ context.Context, params storage.CreateTemplateVersionParams) (*storage.ProvisioningTemplateVersion, error) {
	if params.TemplateID == uuid.Nil {
		return nil, errors.New("template id required")
	}
	if f.templateVersions == nil {
		f.templateVersions = make(map[uuid.UUID][]storage.ProvisioningTemplateVersion)
	}
	versionNumber := len(f.templateVersions[params.TemplateID]) + 1
	version := storage.ProvisioningTemplateVersion{
		ID:         uuid.New(),
		TemplateID: params.TemplateID,
		Version:    versionNumber,
		Body:       params.Body,
		CreatedAt:  time.Now(),
	}
	if params.Checksum != nil {
		version.Checksum = sql.NullString{String: *params.Checksum, Valid: true}
	}
	if len(params.MetadataSchema) > 0 {
		version.MetadataSchema = params.MetadataSchema
	}
	if params.RolloutNotes != nil {
		version.RolloutNotes = sql.NullString{String: *params.RolloutNotes, Valid: true}
	}
	if params.CreatedBy != nil {
		version.CreatedBy = params.CreatedBy
	}
	f.templateVersions[params.TemplateID] = append(f.templateVersions[params.TemplateID], version)
	return &version, nil
}

func (f *fakeStore) PromoteProvisioningTemplateVersion(_ context.Context, templateID uuid.UUID, versionNumber int) (*storage.ProvisioningTemplateVersion, error) {
	versions := f.templateVersions[templateID]
	if versionNumber <= 0 || versionNumber > len(versions) {
		return nil, errors.New("version not found")
	}
	version := versions[versionNumber-1]
	version.PromotedAt = sql.NullTime{Time: time.Now(), Valid: true}
	for i, tpl := range f.templates {
		if tpl.ID == templateID {
			id := version.ID
			f.templates[i].PromotedVersionID = &id
			f.templates[i].UpdatedAt = time.Now()
			break
		}
	}
	versions[versionNumber-1] = version
	f.templateVersions[templateID] = versions
	return &version, nil
}

func (f *fakeStore) GetProvisioningTemplateVersion(_ context.Context, templateID uuid.UUID, versionNumber int) (*storage.ProvisioningTemplateVersion, error) {
	versions := f.templateVersions[templateID]
	if versionNumber <= 0 || versionNumber > len(versions) {
		return nil, nil
	}
	version := versions[versionNumber-1]
	return &version, nil
}

func (f *fakeStore) GetPromotedProvisioningTemplateVersion(ctx context.Context, templateID uuid.UUID) (*storage.ProvisioningTemplateVersion, error) {
	tpl, err := f.GetProvisioningTemplate(ctx, templateID)
	if err != nil || tpl == nil || tpl.PromotedVersionID == nil {
		return nil, err
	}
	for _, version := range f.templateVersions[templateID] {
		if version.ID == *tpl.PromotedVersionID {
			copy := version
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListNodes(_ context.Context, tenantID uuid.UUID, hostnamePrefix string, limit, offset int) ([]storage.Node, int, error) {
	var filtered []storage.Node
	for _, node := range f.nodes {
		if tenantID != uuid.Nil && node.TenantID != tenantID {
			continue
		}
		if hostnamePrefix != "" && !strings.HasPrefix(strings.ToLower(node.Hostname), strings.ToLower(hostnamePrefix)) {
			continue
		}
		filtered = append(filtered, node)
	}
	total := len(filtered)
	if offset > len(filtered) {
		return []storage.Node{}, total, nil
	}
	end := len(filtered)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) GetNode(_ context.Context, id uuid.UUID) (*storage.Node, error) {
	for _, node := range f.nodes {
		if node.ID == id {
			copy := node
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) CreateTenant(_ context.Context, tenant *storage.Tenant) (*storage.Tenant, error) {
	if tenant.ID == uuid.Nil {
		tenant.ID = uuid.New()
	}
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = time.Now()
	}
	f.createdTenant = tenant
	f.tenants = append(f.tenants, *tenant)
	return tenant, nil
}

func (f *fakeStore) ListTenants(_ context.Context, prefix string, limit, offset int) ([]storage.Tenant, int, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	var filtered []storage.Tenant
	for _, tenant := range f.tenants {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(tenant.Name), prefix) {
			continue
		}
		filtered = append(filtered, tenant)
	}
	total := len(filtered)
	if offset > len(filtered) {
		return []storage.Tenant{}, total, nil
	}
	end := len(filtered)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) ListJobs(_ context.Context, tenantID uuid.UUID, jobType string, status storage.JobStatus, limit, offset int) ([]storage.Job, int, error) {
	var filtered []storage.Job
	for _, job := range f.jobs {
		if tenantID != uuid.Nil && job.TenantID != tenantID {
			continue
		}
		if strings.TrimSpace(jobType) != "" && !strings.EqualFold(job.Type, jobType) {
			continue
		}
		if status != "" && job.Status != status {
			continue
		}
		filtered = append(filtered, *job)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return strings.Compare(filtered[i].ID.String(), filtered[j].ID.String()) < 0
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	total := len(filtered)
	if offset > len(filtered) {
		return []storage.Job{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) GetTenant(_ context.Context, id uuid.UUID) (*storage.Tenant, error) {
	for _, t := range f.tenants {
		if t.ID == id {
			copy := t
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) EnsureTenant(ctx context.Context, id uuid.UUID, name string) (*storage.Tenant, error) {
	if t, _ := f.GetTenant(ctx, id); t != nil {
		return t, nil
	}
	tenant := storage.Tenant{ID: id, Name: name, CreatedAt: time.Now()}
	f.tenants = append(f.tenants, tenant)
	return &tenant, nil
}

func (f *fakeStore) EnsureUser(_ context.Context, externalID, email, displayName string) (*storage.User, error) {
	if f.users == nil {
		f.users = make(map[string]*storage.User)
	}

	if existing, ok := f.users[externalID]; ok {
		f.lastUserID = existing.ID
		return existing, nil
	}

	user := &storage.User{
		ID:          uuid.New(),
		ExternalID:  externalID,
		Email:       storageNullString(email),
		DisplayName: storageNullString(displayName),
		CreatedAt:   time.Now(),
	}
	f.lastUserID = user.ID

	if f.skipUserPersistence {
		return user, nil
	}

	f.users[externalID] = user
	return user, nil
}

func (f *fakeStore) AssignRolesToUser(_ context.Context, userID uuid.UUID, roles []string) error {
	if f.userRoles == nil {
		f.userRoles = make(map[uuid.UUID][]string)
	}
	f.userRoles[userID] = sanitizeRoles(roles)
	return nil
}

func (f *fakeStore) ListUserRoles(_ context.Context, userID uuid.UUID) ([]string, error) {
	if f.overrideRoles != nil {
		if roles, ok := f.overrideRoles[userID]; ok {
			return sanitizeRoles(roles), nil
		}
	}
	return f.userRoles[userID], nil
}

func storageNullString(val string) sql.NullString {
	val = strings.TrimSpace(val)
	if val == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: val, Valid: true}
}

func sanitizeRoles(roles []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		key := strings.ToLower(role)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, role)
	}
	return out
}

func (f *fakeStore) CreateJob(_ context.Context, job *storage.Job, event *storage.JobEvent) (*storage.Job, error) {
	if f.jobs == nil {
		f.jobs = make(map[uuid.UUID]*storage.Job)
	}
	if f.events == nil {
		f.events = make(map[uuid.UUID][]storage.JobEvent)
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = job.CreatedAt
	f.jobs[job.ID] = job
	if event != nil {
		if event.ID == uuid.Nil {
			event.ID = uuid.New()
		}
		event.JobID = job.ID
		if event.CreatedAt.IsZero() {
			event.CreatedAt = job.CreatedAt
		}
		f.events[job.ID] = append(f.events[job.ID], *event)
	}
	return job, nil
}

func (f *fakeStore) UpdateJobStatus(_ context.Context, jobID uuid.UUID, status storage.JobStatus, message string, fields map[string]any) error {
	if f.jobs == nil {
		return errors.New("job store empty")
	}
	job, ok := f.jobs[jobID]
	if !ok {
		return errors.New("job not found")
	}
	job.Status = status
	job.UpdatedAt = time.Now()
	if fields != nil {
		if started, ok := fields["started_at"].(time.Time); ok {
			job.StartedAt = &started
		}
		if finished, ok := fields["finished_at"].(time.Time); ok {
			job.FinishedAt = &finished
		}
		if retries, ok := fields["retries"].(int); ok {
			job.Retries = retries
		}
	}
	if f.events == nil {
		f.events = make(map[uuid.UUID][]storage.JobEvent)
	}
	evt := storage.JobEvent{
		ID:        uuid.New(),
		JobID:     jobID,
		Status:    status,
		Message:   message,
		CreatedAt: time.Now(),
	}
	f.events[jobID] = append(f.events[jobID], evt)
	return nil
}

func (f *fakeStore) GetJob(_ context.Context, jobID uuid.UUID) (*storage.Job, error) {
	if job, ok := f.jobs[jobID]; ok {
		return job, nil
	}
	return nil, nil
}

func (f *fakeStore) ListJobEvents(_ context.Context, jobID uuid.UUID) ([]storage.JobEvent, error) {
	return f.events[jobID], nil
}

func (f *fakeStore) GetUserByExternalID(_ context.Context, externalID string) (*storage.User, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return nil, errors.New("external id required")
	}
	if f.users == nil {
		return nil, nil
	}
	if user, ok := f.users[externalID]; ok {
		return user, nil
	}
	return nil, nil
}
