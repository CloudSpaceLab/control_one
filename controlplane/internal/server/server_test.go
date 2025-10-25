package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
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
		"provision": func(ctx context.Context, job *storage.Job) error {
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
		payload := []byte(`{"type":"provision","payload":{}}`)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/jobs validates tenant existence", func(t *testing.T) {
		tenant := uuid.New()
		payload := []byte(fmt.Sprintf(`{"type":"provision.apply","tenant_id":"%s","payload":{"plan_id":"plan-1","tenant_id":"%s"}}`, tenant.String(), tenant.String()))
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
}

type fakeStore struct {
	nodes         []storage.Node
	tenants       []storage.Tenant
	createdNode   *storage.Node
	createdTenant *storage.Tenant
	jobs          map[uuid.UUID]*storage.Job
	events        map[uuid.UUID][]storage.JobEvent
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

func (f *fakeStore) ListNodes(context.Context) ([]storage.Node, error) {
	return f.nodes, nil
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

func (f *fakeStore) ListTenants(context.Context) ([]storage.Tenant, error) {
	return f.tenants, nil
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
