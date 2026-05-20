package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

// capturingQueue records every task it's handed so tests can inspect
// enqueue-count and run the job function synchronously.
type capturingQueue struct {
	mu    sync.Mutex
	tasks []worker.Task
}

func (c *capturingQueue) Enqueue(task worker.Task) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tasks = append(c.tasks, task)
	return nil
}

func (c *capturingQueue) EnqueueAt(task worker.Task, _ time.Time) error {
	return c.Enqueue(task)
}

func (c *capturingQueue) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tasks)
}

func (c *capturingQueue) lastTask() *worker.Task {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.tasks) == 0 {
		return nil
	}
	t := c.tasks[len(c.tasks)-1]
	return &t
}

// clustersTestEnv bundles the wiring needed for each cluster-handler test.
type clustersTestEnv struct {
	t        *testing.T
	srv      *Server
	store    *fakeStore
	queue    *capturingQueue
	tenantID uuid.UUID
}

func setupClustersEnv(t *testing.T, token, defaultRole string) *clustersTestEnv {
	t.Helper()
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens(defaultRole, token),
	}

	tenantID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{
			{ID: tenantID, Name: "acme", CreatedAt: time.Unix(1700000000, 0)},
		},
		userRoles: map[uuid.UUID][]string{},
	}
	queue := &capturingQueue{}
	srv := New(logger, cfg, store, queue)

	return &clustersTestEnv{
		t:        t,
		srv:      srv,
		store:    store,
		queue:    queue,
		tenantID: tenantID,
	}
}

// call makes a request and returns the recorder. If role != "", the test's
// persisted principal is promoted to that role before the call.
func (e *clustersTestEnv) call(method, path, token string, body any, promoteTo string) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}

	// Warm up once so the user gets persisted into the fake store with the
	// default role, then swap in the target role.
	if promoteTo != "" {
		warm := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
		warm.Header.Set("Authorization", "Bearer "+token)
		warmRec := httptest.NewRecorder()
		e.srv.Handler().ServeHTTP(warmRec, warm)
		if e.store.overrideRoles == nil {
			e.store.overrideRoles = map[uuid.UUID][]string{}
		}
		e.store.overrideRoles[e.store.lastUserID] = []string{promoteTo}
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rec, req)
	return rec
}

// runLastTask executes the most recently-enqueued task (used by tests that
// assert on post-provisioning state).
func (e *clustersTestEnv) runLastTask() {
	e.t.Helper()
	task := e.queue.lastTask()
	if task == nil {
		e.t.Fatalf("expected a queued task, got none")
	}
	if err := task.Job(context.Background()); err != nil {
		e.t.Fatalf("task job failed: %v", err)
	}
}

func createClusterBody(tenantID uuid.UUID, name string, extra map[string]any) map[string]any {
	body := map[string]any{
		"tenant_id": tenantID.String(),
		"name":      name,
		"provider":  "mock",
		"role_plan": map[string]any{
			"roles": []map[string]any{
				{"name": "control-plane", "count": 3},
				{"name": "worker", "count": 2},
			},
		},
		"labels": map[string]any{
			"env":                "prod",
			"availability_zones": []string{"us-east-1a", "us-east-1b", "us-east-1c"},
		},
		"failure_domain_strategy": "spread",
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func TestClustersCreateReturnsAcceptedAndEnqueuesJob(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	rec := env.call(http.MethodPost, "/api/v1/clusters", "cluster-admin",
		createClusterBody(env.tenantID, "prod-k8s-eu-west", nil), "admin")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp clusterAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode accept response: %v", err)
	}
	if resp.ClusterID == "" || resp.JobID == "" {
		t.Fatalf("expected cluster_id and job_id populated, got %+v", resp)
	}
	if resp.State != "pending" {
		t.Fatalf("expected initial state 'pending', got %q", resp.State)
	}

	if env.queue.count() != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", env.queue.count())
	}

	// Cluster should be persisted with desired_size = sum(roles) = 5.
	cluster, err := env.store.GetClusterByID(context.Background(), uuid.MustParse(resp.ClusterID))
	if err != nil || cluster == nil {
		t.Fatalf("expected cluster persisted: err=%v cluster=%v", err, cluster)
	}
	if cluster.DesiredSize != 5 {
		t.Fatalf("expected desired_size 5, got %d", cluster.DesiredSize)
	}
	if cluster.FailureDomainStrategy != "spread" {
		t.Fatalf("expected strategy 'spread', got %q", cluster.FailureDomainStrategy)
	}

	// Persisted job should carry the cluster id in its payload.
	job, err := env.store.GetJob(context.Background(), uuid.MustParse(resp.JobID))
	if err != nil || job == nil {
		t.Fatalf("expected job persisted: err=%v job=%v", err, job)
	}
	if job.Type != JobTypeClusterProvision {
		t.Fatalf("expected job type %q, got %q", JobTypeClusterProvision, job.Type)
	}
	var payload clusterProvisionPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode job payload: %v", err)
	}
	if payload.ClusterID != resp.ClusterID {
		t.Fatalf("payload cluster_id mismatch: %s vs %s", payload.ClusterID, resp.ClusterID)
	}
}

func TestClustersCreateViewerForbidden(t *testing.T) {
	env := setupClustersEnv(t, "cluster-viewer", "viewer")

	rec := env.call(http.MethodPost, "/api/v1/clusters", "cluster-viewer",
		createClusterBody(env.tenantID, "prod-k8s", nil), "viewer")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d body=%s", rec.Code, rec.Body.String())
	}
	if env.queue.count() != 0 {
		t.Fatalf("expected 0 tasks enqueued, got %d", env.queue.count())
	}
}

func TestClustersCreateOperatorForbidden(t *testing.T) {
	// Create must require admin specifically — operator should be denied.
	env := setupClustersEnv(t, "cluster-op", "viewer")

	rec := env.call(http.MethodPost, "/api/v1/clusters", "cluster-op",
		createClusterBody(env.tenantID, "prod-k8s", nil), "operator")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected operator blocked with 403, got %d", rec.Code)
	}
}

func TestClustersCreateRejectsInvalidPayload(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing tenant_id", map[string]any{"name": "c1", "provider": "mock", "role_plan": map[string]any{"roles": []map[string]any{{"name": "w", "count": 1}}}}},
		{"missing name", map[string]any{"tenant_id": env.tenantID.String(), "provider": "mock", "role_plan": map[string]any{"roles": []map[string]any{{"name": "w", "count": 1}}}}},
		{"missing provider", map[string]any{"tenant_id": env.tenantID.String(), "name": "c1", "role_plan": map[string]any{"roles": []map[string]any{{"name": "w", "count": 1}}}}},
		{"empty role_plan", map[string]any{"tenant_id": env.tenantID.String(), "name": "c1", "provider": "mock", "role_plan": map[string]any{"roles": []map[string]any{}}}},
		{"zero role count", map[string]any{"tenant_id": env.tenantID.String(), "name": "c1", "provider": "mock", "role_plan": map[string]any{"roles": []map[string]any{{"name": "w", "count": 0}}}}},
		{"duplicate role name", map[string]any{"tenant_id": env.tenantID.String(), "name": "c1", "provider": "mock", "role_plan": map[string]any{"roles": []map[string]any{{"name": "w", "count": 1}, {"name": "w", "count": 1}}}}},
		{"desired_size below role sum", map[string]any{"tenant_id": env.tenantID.String(), "name": "c1", "provider": "mock", "desired_size": 1, "role_plan": map[string]any{"roles": []map[string]any{{"name": "w", "count": 5}}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.call(http.MethodPost, "/api/v1/clusters", "cluster-admin", tc.body, "admin")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestClustersCreateUnknownTenant(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	body := createClusterBody(uuid.New(), "ghost-cluster", nil)
	rec := env.call(http.MethodPost, "/api/v1/clusters", "cluster-admin", body, "admin")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown tenant got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestClustersListIsTenantScoped(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	// Seed two tenants: ours, and a second that must be filtered out.
	otherTenant := uuid.New()
	env.store.mu.Lock()
	env.store.tenants = append(env.store.tenants, storage.Tenant{
		ID: otherTenant, Name: "other", CreatedAt: time.Now(),
	})
	env.store.mu.Unlock()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{}
	myClusterID := uuid.New()
	theirClusterID := uuid.New()
	env.store.clusters[myClusterID] = &storage.Cluster{
		ID: myClusterID, TenantID: env.tenantID, Name: "mine", Provider: "mock",
		DesiredSize: 3, RolePlan: map[string]any{}, Labels: map[string]any{},
		FailureDomainStrategy: "spread", State: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	env.store.clusters[theirClusterID] = &storage.Cluster{
		ID: theirClusterID, TenantID: otherTenant, Name: "theirs", Provider: "mock",
		DesiredSize: 1, RolePlan: map[string]any{}, Labels: map[string]any{},
		FailureDomainStrategy: "spread", State: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters?tenant_id="+env.tenantID.String(), "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp paginatedResponse[clusterResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected exactly 1 cluster for tenant, got %d", len(resp.Data))
	}
	if resp.Data[0].Name != "mine" {
		t.Fatalf("expected 'mine', got %q", resp.Data[0].Name)
	}
}

func TestClustersGetIncludesMembersAndRollout(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "live", Provider: "mock",
			DesiredSize: 2, RolePlan: map[string]any{}, Labels: map[string]any{},
			FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{
		clusterID: {
			{ClusterID: clusterID, NodeID: uuid.New(), Role: "control-plane", Position: 0, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: 0, JoinedAt: time.Now()},
		},
	}
	rolloutID := uuid.New()
	env.store.clusterRollouts = map[uuid.UUID][]storage.ClusterRollout{
		clusterID: {
			{
				ID: rolloutID, ClusterID: clusterID, TemplateVersionID: uuid.New(),
				WaveSize: 1, WaveStrategy: "rolling", HealthGate: map[string]any{},
				State: "pending", CurrentWave: 0,
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			},
		},
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String(), "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(resp.Members))
	}
	if resp.LatestRollout == nil || resp.LatestRollout.ID != rolloutID.String() {
		t.Fatalf("expected latest rollout %s, got %+v", rolloutID, resp.LatestRollout)
	}
}

func TestClustersGetNotFound(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+uuid.New().String(), "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestClustersScaleExpandEnqueuesScaleJob(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	// Seed a pre-existing cluster at desired_size=3.
	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "scale-me", Provider: "mock",
			DesiredSize: 3,
			RolePlan: map[string]any{
				"roles": []any{
					map[string]any{"name": "worker", "count": 3},
				},
			},
			Labels: map[string]any{}, FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}

	rec := env.call(http.MethodPatch, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", map[string]any{"desired_size": 6}, "admin")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.JobID == "" {
		t.Fatalf("expected job id in scale response")
	}

	if env.queue.count() != 1 {
		t.Fatalf("expected 1 scale task enqueued, got %d", env.queue.count())
	}
	job, _ := env.store.GetJob(context.Background(), uuid.MustParse(resp.JobID))
	if job == nil || job.Type != JobTypeClusterScale {
		t.Fatalf("expected cluster.scale job, got %+v", job)
	}
	var payload clusterScalePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode scale payload: %v", err)
	}
	if payload.Direction != "expand" {
		t.Fatalf("expected direction 'expand', got %q", payload.Direction)
	}
	if payload.Delta != 3 {
		t.Fatalf("expected delta 3, got %d", payload.Delta)
	}
	if payload.DesiredSize != 6 {
		t.Fatalf("expected desired_size 6, got %d", payload.DesiredSize)
	}

	// Cluster should now reflect updated desired_size.
	updated, _ := env.store.GetClusterByID(context.Background(), clusterID)
	if updated == nil || updated.DesiredSize != 6 {
		t.Fatalf("expected cluster desired_size 6, got %+v", updated)
	}
}

// TestClustersScaleShrinkEnqueuesShrinkJob verifies Sprint 2 E unblocks the
// Sprint 1 501 stub — shrink now returns 202 + job_id, direction=shrink.
func TestClustersScaleShrinkEnqueuesShrinkJob(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "shrink-me", Provider: "mock",
			DesiredSize: 5,
			RolePlan: map[string]any{
				"roles": []any{map[string]any{"name": "worker", "count": 5}},
			},
			Labels: map[string]any{}, FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	// Seed 5 existing members so shrink has something to drain.
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{clusterID: {}}
	for pos := 0; pos < 5; pos++ {
		env.store.clusterMembers[clusterID] = append(env.store.clusterMembers[clusterID], storage.ClusterMember{
			ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: pos, JoinedAt: time.Now(),
		})
	}

	rec := env.call(http.MethodPatch, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", map[string]any{"desired_size": 2}, "admin")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for shrink, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.JobID == "" {
		t.Fatalf("expected job id in shrink response")
	}
	if env.queue.count() != 1 {
		t.Fatalf("expected 1 scale task enqueued, got %d", env.queue.count())
	}
	job, _ := env.store.GetJob(context.Background(), uuid.MustParse(resp.JobID))
	if job == nil || job.Type != JobTypeClusterScale {
		t.Fatalf("expected cluster.scale job, got %+v", job)
	}
	var payload clusterScalePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode scale payload: %v", err)
	}
	if payload.Direction != "shrink" {
		t.Fatalf("expected direction 'shrink', got %q", payload.Direction)
	}
	if payload.Delta != -3 {
		t.Fatalf("expected delta -3, got %d", payload.Delta)
	}

	// Run the task synchronously and verify 3 members were drained.
	env.runLastTask()

	members, _ := env.store.ListClusterMembers(context.Background(), clusterID)
	if len(members) != 2 {
		t.Fatalf("expected 2 members after shrink, got %d", len(members))
	}
}

func TestClustersPatchLabelsOnly(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "tag-me", Provider: "mock",
			DesiredSize: 3, RolePlan: map[string]any{}, Labels: map[string]any{"env": "dev"},
			FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}

	rec := env.call(http.MethodPatch, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", map[string]any{
			"labels": map[string]any{"env": "prod", "team": "platform"},
		}, "admin")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for label update, got %d body=%s", rec.Code, rec.Body.String())
	}
	if env.queue.count() != 0 {
		t.Fatalf("labels-only patch should not enqueue, got %d", env.queue.count())
	}
	updated, _ := env.store.GetClusterByID(context.Background(), clusterID)
	if updated == nil || updated.Labels["env"] != "prod" || updated.Labels["team"] != "platform" {
		t.Fatalf("expected labels updated, got %+v", updated)
	}
}

// TestClustersDeleteEnqueuesTeardownJob verifies Sprint 2 E unblocks the
// Sprint 1 501 stub — DELETE returns 202 + job_id, cluster flips to
// terminating, then the worker drains + deletes.
func TestClustersDeleteEnqueuesTeardownJob(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "kill-me", Provider: "mock",
			DesiredSize: 3, RolePlan: map[string]any{}, Labels: map[string]any{},
			FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{clusterID: {
		{ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: 0, JoinedAt: time.Now()},
		{ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: 1, JoinedAt: time.Now()},
		{ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: 2, JoinedAt: time.Now()},
	}}

	rec := env.call(http.MethodDelete, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", nil, "admin")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for teardown, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.State != "terminating" {
		t.Fatalf("expected state 'terminating', got %q", resp.State)
	}
	if env.queue.count() != 1 {
		t.Fatalf("delete should enqueue 1 teardown task, got %d", env.queue.count())
	}
	job, _ := env.store.GetJob(context.Background(), uuid.MustParse(resp.JobID))
	if job == nil || job.Type != JobTypeClusterTeardown {
		t.Fatalf("expected cluster.teardown job, got %+v", job)
	}

	// Cluster still exists until the worker runs.
	cluster, _ := env.store.GetClusterByID(context.Background(), clusterID)
	if cluster == nil {
		t.Fatalf("cluster should still exist before worker runs")
	}

	// Run the teardown synchronously and verify cluster is gone + members drained.
	env.runLastTask()
	cluster, _ = env.store.GetClusterByID(context.Background(), clusterID)
	if cluster != nil {
		t.Fatalf("cluster should have been deleted after teardown, got %+v", cluster)
	}
	members, _ := env.store.ListClusterMembers(context.Background(), clusterID)
	if len(members) != 0 {
		t.Fatalf("expected all members drained after teardown, got %d", len(members))
	}
}

func TestClustersProvisionJobPopulatesMembersAndMarksRunning(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	// Create via API so the provision job is enqueued normally.
	body := createClusterBody(env.tenantID, "full-run", nil)
	rec := env.call(http.MethodPost, "/api/v1/clusters", "cluster-admin", body, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create cluster: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterAcceptedResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	// Execute the provision job synchronously.
	env.runLastTask()

	// Cluster should be marked running.
	cluster, _ := env.store.GetClusterByID(context.Background(), uuid.MustParse(resp.ClusterID))
	if cluster == nil || cluster.State != "running" {
		t.Fatalf("expected cluster state 'running' after provision, got %+v", cluster)
	}

	members, _ := env.store.ListClusterMembers(context.Background(), cluster.ID)
	if len(members) != 5 {
		t.Fatalf("expected 5 members (3 cp + 2 worker), got %d", len(members))
	}

	// Count per role.
	roleCounts := map[string]int{}
	for _, m := range members {
		roleCounts[m.Role]++
	}
	if roleCounts["control-plane"] != 3 {
		t.Fatalf("expected 3 control-plane members, got %d", roleCounts["control-plane"])
	}
	if roleCounts["worker"] != 2 {
		t.Fatalf("expected 2 worker members, got %d", roleCounts["worker"])
	}

	// Job should be flipped to succeeded.
	job, _ := env.store.GetJob(context.Background(), uuid.MustParse(resp.JobID))
	if job == nil || job.Status != storage.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %+v", job)
	}
}

func TestClustersScaleExpandAddsOnlyDeltaMembers(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	// Seed a cluster already at desired 2 workers, pre-provisioned.
	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "scale-run", Provider: "mock",
			DesiredSize: 2,
			RolePlan: map[string]any{
				"roles": []any{
					map[string]any{"name": "worker", "count": 2},
				},
			},
			Labels: map[string]any{}, FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{
		clusterID: {
			{ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: 0, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: 1, JoinedAt: time.Now()},
		},
	}

	// Expand to 4 workers.
	rec := env.call(http.MethodPatch, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", map[string]any{
			"desired_size": 4,
			"role_plan": map[string]any{
				"roles": []map[string]any{{"name": "worker", "count": 4}},
			},
		}, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	env.runLastTask()

	members, _ := env.store.ListClusterMembers(context.Background(), clusterID)
	if len(members) != 4 {
		t.Fatalf("expected 4 members after expand, got %d", len(members))
	}

	// Positions should be 0..3 without duplication.
	positions := map[int]bool{}
	for _, m := range members {
		if positions[m.Position] {
			t.Fatalf("duplicate position %d in scaled cluster", m.Position)
		}
		positions[m.Position] = true
	}
}

func TestClustersSubrouteInvalidID(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	rec := env.call(http.MethodGet, "/api/v1/clusters/not-a-uuid", "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad uuid, got %d", rec.Code)
	}
}

func TestClustersMethodNotAllowed(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	// PUT not supported on the collection.
	rec := env.call(http.MethodPut, "/api/v1/clusters", "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on PUT collection, got %d", rec.Code)
	}
}

type tenantGatedFakeStore struct {
	fakeStore
	allowedTenants map[uuid.UUID]bool
}

func (f *tenantGatedFakeStore) UserHasTenantRole(_ context.Context, _ uuid.UUID, tenantID uuid.UUID, _ []string) (bool, error) {
	return f.allowedTenants[tenantID], nil
}

func callTenantGatedServer(t *testing.T, srv *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestClustersListRequiresTenantAccess(t *testing.T) {
	allowedTenant := uuid.New()
	blockedTenant := uuid.New()
	mineID := uuid.New()
	theirsID := uuid.New()
	store := &tenantGatedFakeStore{
		fakeStore: fakeStore{
			tenants: []storage.Tenant{
				{ID: allowedTenant, Name: "allowed", CreatedAt: time.Now()},
				{ID: blockedTenant, Name: "blocked", CreatedAt: time.Now()},
			},
			userRoles: map[uuid.UUID][]string{},
			clusters: map[uuid.UUID]*storage.Cluster{
				mineID: {
					ID: mineID, TenantID: allowedTenant, Name: "mine", Provider: "mock",
					DesiredSize: 1, RolePlan: map[string]any{}, Labels: map[string]any{},
					FailureDomainStrategy: "spread", State: "running", CreatedAt: time.Now(), UpdatedAt: time.Now(),
				},
				theirsID: {
					ID: theirsID, TenantID: blockedTenant, Name: "theirs", Provider: "mock",
					DesiredSize: 1, RolePlan: map[string]any{}, Labels: map[string]any{},
					FailureDomainStrategy: "spread", State: "running", CreatedAt: time.Now(), UpdatedAt: time.Now(),
				},
			},
		},
		allowedTenants: map[uuid.UUID]bool{allowedTenant: true},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "cluster-viewer"),
	}, store, &stubQueue{})

	rec := callTenantGatedServer(t, srv, http.MethodGet, "/api/v1/clusters?tenant_id="+blockedTenant.String(), "cluster-viewer", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected blocked tenant list to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = callTenantGatedServer(t, srv, http.MethodGet, "/api/v1/clusters?tenant_id="+allowedTenant.String(), "cluster-viewer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected allowed tenant list to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp paginatedResponse[clusterResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != mineID.String() {
		t.Fatalf("expected only allowed tenant cluster, got %+v", resp.Data)
	}
}

func TestClustersResourceRoutesRequireResourceTenantAccess(t *testing.T) {
	allowedTenant := uuid.New()
	blockedTenant := uuid.New()
	clusterID := uuid.New()
	store := &tenantGatedFakeStore{
		fakeStore: fakeStore{
			tenants: []storage.Tenant{
				{ID: allowedTenant, Name: "allowed", CreatedAt: time.Now()},
				{ID: blockedTenant, Name: "blocked", CreatedAt: time.Now()},
			},
			userRoles: map[uuid.UUID][]string{},
			clusters: map[uuid.UUID]*storage.Cluster{
				clusterID: {
					ID: clusterID, TenantID: blockedTenant, Name: "blocked-cluster", Provider: "mock",
					DesiredSize: 1, RolePlan: map[string]any{}, Labels: map[string]any{},
					FailureDomainStrategy: "spread", State: "running", CreatedAt: time.Now(), UpdatedAt: time.Now(),
				},
			},
		},
		allowedTenants: map[uuid.UUID]bool{allowedTenant: true},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "cluster-admin"),
	}, store, &stubQueue{})

	rec := callTenantGatedServer(t, srv, http.MethodGet, "/api/v1/clusters/"+clusterID.String(), "cluster-admin", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant get to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = callTenantGatedServer(t, srv, http.MethodPost, "/api/v1/clusters", "cluster-admin",
		createClusterBody(blockedTenant, "blocked-create", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant create to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
