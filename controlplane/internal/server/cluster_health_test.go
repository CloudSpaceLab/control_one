package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// Unit tests for the pure aggregation helpers — no server plumbing needed.
func TestQuorumFor(t *testing.T) {
	cases := []struct {
		total, want int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 2},
		{4, 3},
		{5, 3},
		{6, 4},
		{7, 4},
	}
	for _, tc := range cases {
		if got := quorumFor(tc.total); got != tc.want {
			t.Fatalf("quorumFor(%d) = %d, want %d", tc.total, got, tc.want)
		}
	}
}

func TestDeriveClusterState(t *testing.T) {
	cases := []struct {
		name                     string
		healthy, total, quorum   int
		want                     string
	}{
		{"empty", 0, 0, 0, ClusterHealthEmpty},
		{"all healthy 5/5", 5, 5, 3, ClusterHealthHealthy},
		{"degraded above quorum", 4, 5, 3, ClusterHealthDegraded},
		{"degraded at quorum", 3, 5, 3, ClusterHealthDegraded},
		{"unhealthy below quorum", 2, 5, 3, ClusterHealthUnhealthy},
		{"unhealthy zero healthy", 0, 5, 3, ClusterHealthUnhealthy},
		{"single node healthy", 1, 1, 1, ClusterHealthHealthy},
		{"single node unhealthy", 0, 1, 1, ClusterHealthUnhealthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveClusterState(tc.healthy, tc.total, tc.quorum); got != tc.want {
				t.Fatalf("deriveClusterState(%d,%d,%d)=%s, want %s", tc.healthy, tc.total, tc.quorum, got, tc.want)
			}
		})
	}
}

func TestMemberHealthyMatrix(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-30 * time.Second)
	stale := now.Add(-10 * time.Minute)
	compOK := true
	compBad := false

	active := &storage.Node{State: storage.NodeStateActive}
	pending := &storage.Node{State: storage.NodeStateEnrollmentPending}
	retired := &storage.Node{State: storage.NodeStateRetired}

	cases := []struct {
		name       string
		node       *storage.Node
		lastSeen   *time.Time
		compliance *bool
		want       bool
	}{
		{"active + fresh heartbeat", active, &fresh, nil, true},
		{"active + fresh + compliance ok", active, &fresh, &compOK, true},
		{"active + fresh but failing compliance", active, &fresh, &compBad, false},
		{"active + stale heartbeat", active, &stale, nil, false},
		{"active + never heartbeated", active, nil, nil, false},
		{"pending state", pending, &fresh, nil, false},
		{"retired state", retired, &fresh, nil, false},
		{"nil node", nil, &fresh, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			healthy, _ := memberHealthy(tc.node, tc.lastSeen, now, tc.compliance)
			if healthy != tc.want {
				t.Fatalf("memberHealthy want %v got %v", tc.want, healthy)
			}
		})
	}
}

// seedHealthCluster creates a cluster and N member nodes in the fake
// store. All nodes are created with State=active. Tests can then flip
// individual state or inject heartbeat timestamps via setTestNodeLastSeen.
func seedHealthCluster(t *testing.T, env *clustersTestEnv, name string, size int) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	clusterID := uuid.New()
	if env.store.clusters == nil {
		env.store.clusters = map[uuid.UUID]*storage.Cluster{}
	}
	env.store.clusters[clusterID] = &storage.Cluster{
		ID: clusterID, TenantID: env.tenantID, Name: name, Provider: "mock",
		DesiredSize: size, RolePlan: map[string]any{}, Labels: map[string]any{},
		FailureDomainStrategy: "spread", State: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	nodeIDs := make([]uuid.UUID, 0, size)
	if env.store.clusterMembers == nil {
		env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{}
	}
	for i := 0; i < size; i++ {
		nodeID := uuid.New()
		env.store.nodes = append(env.store.nodes, storage.Node{
			ID: nodeID, TenantID: env.tenantID, Hostname: name + "-" + string(rune('a'+i)),
			State: storage.NodeStateActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
		role := "worker"
		if i < 3 && size >= 3 {
			role = "control-plane"
		}
		env.store.clusterMembers[clusterID] = append(env.store.clusterMembers[clusterID], storage.ClusterMember{
			ClusterID: clusterID, NodeID: nodeID, Role: role, Position: i, JoinedAt: time.Now(),
		})
		nodeIDs = append(nodeIDs, nodeID)
	}
	return clusterID, nodeIDs
}

func TestClusterHealthAllHealthy(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID, nodeIDs := seedHealthCluster(t, env, "prod", 5)

	defer clearTestNodeLastSeen()
	now := time.Now().UTC()
	for _, id := range nodeIDs {
		setTestNodeLastSeen(id, now.Add(-30*time.Second))
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String()+"/health", "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != ClusterHealthHealthy {
		t.Fatalf("expected healthy got %q: %+v", resp.State, resp)
	}
	if resp.TotalCount != 5 || resp.HealthyCount != 5 {
		t.Fatalf("expected 5/5 counts got %d/%d", resp.HealthyCount, resp.TotalCount)
	}
	if resp.Quorum != 3 || !resp.QuorumMet {
		t.Fatalf("expected quorum=3 met=true got %d met=%v", resp.Quorum, resp.QuorumMet)
	}
	if len(resp.Members) != 5 {
		t.Fatalf("expected 5 members got %d", len(resp.Members))
	}
	for _, m := range resp.Members {
		if !m.Healthy {
			t.Fatalf("expected member %s healthy got reason=%q", m.NodeID, m.Reason)
		}
		if m.HeartbeatAgeSecs == nil {
			t.Fatalf("expected heartbeat_age_seconds populated for member %s", m.NodeID)
		}
	}
}

func TestClusterHealthDegradedOneDown(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID, nodeIDs := seedHealthCluster(t, env, "prod", 5)

	defer clearTestNodeLastSeen()
	now := time.Now().UTC()
	for i, id := range nodeIDs {
		if i == 0 {
			// Leave this node with no heartbeat → unhealthy.
			continue
		}
		setTestNodeLastSeen(id, now.Add(-30*time.Second))
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String()+"/health", "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var resp clusterHealthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.State != ClusterHealthDegraded {
		t.Fatalf("expected degraded got %q (healthy=%d/%d quorum=%d met=%v)",
			resp.State, resp.HealthyCount, resp.TotalCount, resp.Quorum, resp.QuorumMet)
	}
	if resp.HealthyCount != 4 {
		t.Fatalf("expected 4 healthy got %d", resp.HealthyCount)
	}
	if !resp.QuorumMet {
		t.Fatalf("expected quorum still met (4 >= 3)")
	}
}

func TestClusterHealthUnhealthyBelowQuorum(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID, nodeIDs := seedHealthCluster(t, env, "prod", 5)

	defer clearTestNodeLastSeen()
	now := time.Now().UTC()
	// Only 2 nodes heartbeat → healthy=2, quorum=3 → unhealthy.
	setTestNodeLastSeen(nodeIDs[0], now.Add(-30*time.Second))
	setTestNodeLastSeen(nodeIDs[1], now.Add(-30*time.Second))

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String()+"/health", "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var resp clusterHealthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.State != ClusterHealthUnhealthy {
		t.Fatalf("expected unhealthy got %q (healthy=%d quorum=%d met=%v)",
			resp.State, resp.HealthyCount, resp.Quorum, resp.QuorumMet)
	}
	if resp.QuorumMet {
		t.Fatalf("expected quorum NOT met (2 < 3)")
	}
}

func TestClusterHealthEmptyCluster(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "empty", Provider: "mock",
			DesiredSize: 3, RolePlan: map[string]any{}, Labels: map[string]any{},
			FailureDomainStrategy: "spread", State: "pending",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String()+"/health", "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var resp clusterHealthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.State != ClusterHealthEmpty {
		t.Fatalf("expected empty state got %q", resp.State)
	}
	if resp.TotalCount != 0 || resp.HealthyCount != 0 {
		t.Fatalf("expected zero counts got %d/%d", resp.HealthyCount, resp.TotalCount)
	}
}

func TestClusterHealthNotFound(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	rec := env.call(http.MethodGet, "/api/v1/clusters/"+uuid.New().String()+"/health", "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d", rec.Code)
	}
}

func TestClusterHealthComplianceFailingMarksMemberUnhealthy(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID, nodeIDs := seedHealthCluster(t, env, "prod", 3)

	defer clearTestNodeLastSeen()
	now := time.Now().UTC()
	for _, id := range nodeIDs {
		setTestNodeLastSeen(id, now.Add(-30*time.Second))
	}

	// Node 0 has a latest-failing compliance result. Everything else unscanned.
	checkedAt := now.Add(-1 * time.Minute)
	failing := false
	if env.store.complianceResults == nil {
		env.store.complianceResults = map[uuid.UUID][]storage.ComplianceResult{}
	}
	jobID := uuid.New()
	env.store.complianceResults[jobID] = []storage.ComplianceResult{
		{
			ID: uuid.New(), JobID: jobID, TenantID: env.tenantID, NodeID: nodeIDs[0],
			RuleID: "CIS-1", Passed: failing, CheckedAt: &checkedAt, CreatedAt: now,
		},
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String()+"/health", "cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var resp clusterHealthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	// 2 healthy, 1 unhealthy (compliance failing), quorum=2 → degraded.
	if resp.State != ClusterHealthDegraded {
		t.Fatalf("expected degraded got %q (healthy=%d)", resp.State, resp.HealthyCount)
	}
	if resp.HealthyCount != 2 {
		t.Fatalf("expected 2 healthy got %d", resp.HealthyCount)
	}
}

// TestClusterListAttachesHealthSummary confirms gap 3.8: list view carries
// aggregate health so the UI can badge each row without N+1 calls.
func TestClusterListAttachesHealthSummary(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID, nodeIDs := seedHealthCluster(t, env, "prod", 3)

	defer clearTestNodeLastSeen()
	now := time.Now().UTC()
	for _, id := range nodeIDs {
		setTestNodeLastSeen(id, now.Add(-30*time.Second))
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters?tenant_id="+env.tenantID.String(),
		"cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp paginatedResponse[clusterResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Fatalf("expected at least 1 cluster row")
	}
	found := false
	for _, item := range resp.Data {
		if item.ID == clusterID.String() {
			found = true
			if item.Health == nil {
				t.Fatalf("expected health summary attached to cluster row")
			}
			if item.Health.State != ClusterHealthHealthy {
				t.Fatalf("expected healthy summary got %q", item.Health.State)
			}
			if item.Health.TotalCount != 3 || item.Health.HealthyCount != 3 {
				t.Fatalf("expected 3/3 got %d/%d", item.Health.HealthyCount, item.Health.TotalCount)
			}
		}
	}
	if !found {
		t.Fatalf("seeded cluster not found in list response")
	}
}

// TestClusterGetIncludesHealthSummary confirms the detail payload also carries
// a top-level health summary alongside members + latest_rollout.
func TestClusterGetIncludesHealthSummary(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID, nodeIDs := seedHealthCluster(t, env, "prod", 3)

	defer clearTestNodeLastSeen()
	now := time.Now().UTC()
	for _, id := range nodeIDs {
		setTestNodeLastSeen(id, now.Add(-30*time.Second))
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Health == nil {
		t.Fatalf("expected health summary on detail response")
	}
	if resp.Health.State != ClusterHealthHealthy {
		t.Fatalf("expected healthy got %q", resp.Health.State)
	}
}

// TestClusterHealthTransitionsHealthyDegradedUnhealthy walks the same 5-node
// cluster through the full state matrix by dropping heartbeats one at a time.
func TestClusterHealthTransitionsHealthyDegradedUnhealthy(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID, nodeIDs := seedHealthCluster(t, env, "fleet", 5)

	defer clearTestNodeLastSeen()
	now := time.Now().UTC()
	for _, id := range nodeIDs {
		setTestNodeLastSeen(id, now.Add(-30*time.Second))
	}

	fetchState := func() string {
		t.Helper()
		rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String()+"/health",
			"cluster-admin", nil, "viewer")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		var resp clusterHealthResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.State
	}

	// All 5 heartbeating → healthy.
	if got := fetchState(); got != ClusterHealthHealthy {
		t.Fatalf("[5/5] expected healthy got %q", got)
	}

	// Drop one heartbeat → 4/5, above quorum (3) → degraded.
	env.store.nodes[0].State = storage.NodeStateRetired
	// Force the fake store to reflect the change: nodes slice already holds
	// the row by value; we need to replace it.
	env.store.mu.Lock()
	env.store.nodes[0].State = storage.NodeStateRetired
	env.store.mu.Unlock()
	if got := fetchState(); got != ClusterHealthDegraded {
		t.Fatalf("[4/5] expected degraded got %q", got)
	}

	// Drop two more so we're below quorum → 2/5 → unhealthy.
	env.store.mu.Lock()
	env.store.nodes[1].State = storage.NodeStateRetired
	env.store.nodes[2].State = storage.NodeStateRetired
	env.store.mu.Unlock()
	if got := fetchState(); got != ClusterHealthUnhealthy {
		t.Fatalf("[2/5] expected unhealthy got %q", got)
	}
}

// Smoke test exercising the pure aggregate path directly without the HTTP
// handler — useful when debugging derivation rules.
func TestComputeClusterHealthDirect(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")
	clusterID, nodeIDs := seedHealthCluster(t, env, "direct", 3)

	defer clearTestNodeLastSeen()
	now := time.Now().UTC()
	for _, id := range nodeIDs {
		setTestNodeLastSeen(id, now.Add(-30*time.Second))
	}

	cluster, err := env.store.GetClusterByID(context.Background(), clusterID)
	if err != nil || cluster == nil {
		t.Fatalf("load cluster: %v cluster=%v", err, cluster)
	}
	resp := env.srv.computeClusterHealth(context.Background(), cluster)
	if resp.State != ClusterHealthHealthy {
		t.Fatalf("expected healthy got %s", resp.State)
	}
	if resp.Quorum != 2 {
		t.Fatalf("expected quorum 2 for size 3, got %d", resp.Quorum)
	}
}
