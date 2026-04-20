package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// TestClusterProvisionRegistersLBAndPropagatesLabels locks in Worktree E's
// post-member-add hook contract: for clusters with an `lb_target_group_arn`
// (or similar) label, provision should call RegisterLB + persist a
// cluster_lb_registrations row AND propagate cluster labels to each node.
func TestClusterProvisionRegistersLBAndPropagatesLabels(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	body := map[string]any{
		"tenant_id": env.tenantID.String(),
		"name":      "lb-cluster",
		"provider":  "mock",
		"role_plan": map[string]any{
			"roles": []map[string]any{{"name": "worker", "count": 2}},
		},
		"labels": map[string]any{
			"env":                 "prod",
			"lb_target_group_arn": "arn:aws:elasticloadbalancing:us-east-1:111:targetgroup/tg/abc",
		},
	}

	rec := env.call(http.MethodPost, "/api/v1/clusters", "cluster-admin", body, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create cluster: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterAcceptedResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	env.runLastTask()

	members, _ := env.store.ListClusterMembers(context.Background(), uuid.MustParse(resp.ClusterID))
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	// Every member should have an LB registration row.
	regs, _ := env.store.ListClusterLBRegistrationsForCluster(context.Background(), uuid.MustParse(resp.ClusterID))
	if len(regs) != 2 {
		t.Fatalf("expected 2 LB registration rows, got %d: %+v", len(regs), regs)
	}
	for _, reg := range regs {
		if reg.DeregisteredAt != nil {
			t.Fatalf("expected active (nil deregistered_at), got %+v", reg)
		}
		if reg.LBIdentifier != "arn:aws:elasticloadbalancing:us-east-1:111:targetgroup/tg/abc" {
			t.Fatalf("expected target group ARN recorded, got %q", reg.LBIdentifier)
		}
	}

	// Cluster labels should be projected onto each node as cluster.<key>.
	for _, m := range members {
		labels := env.store.nodeLabels[m.NodeID]
		if labels == nil {
			t.Fatalf("expected labels on node %s", m.NodeID)
		}
		if labels["cluster.env"] != "prod" {
			t.Fatalf("expected cluster.env=prod on node %s, got %+v", m.NodeID, labels)
		}
	}
}

// TestClusterShrinkDrainsInReversePosition verifies the shrink ordering
// contract: highest-position slots drain first. Also asserts LB dereg + label
// strip are both applied.
func TestClusterShrinkDrainsInReversePosition(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "drain-me", Provider: "mock",
			DesiredSize: 4,
			RolePlan: map[string]any{
				"roles": []any{map[string]any{"name": "worker", "count": 4}},
			},
			Labels: map[string]any{
				"env":     "prod",
				"lb_pool": "nsx-pool-42",
			},
			FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	// Seed 4 members with explicit positions 0..3.
	var nodeIDs []uuid.UUID
	for pos := 0; pos < 4; pos++ {
		nid := uuid.New()
		nodeIDs = append(nodeIDs, nid)
	}
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{
		clusterID: {
			{ClusterID: clusterID, NodeID: nodeIDs[0], Role: "worker", Position: 0, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: nodeIDs[1], Role: "worker", Position: 1, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: nodeIDs[2], Role: "worker", Position: 2, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: nodeIDs[3], Role: "worker", Position: 3, JoinedAt: time.Now()},
		},
	}
	// Seed LB registrations so dereg has something to touch.
	env.store.nodeLabels = map[uuid.UUID]map[string]any{}
	for _, nid := range nodeIDs {
		env.store.nodeLabels[nid] = map[string]any{
			"cluster.env":     "prod",
			"cluster.lb_pool": "nsx-pool-42",
			"owner":           "team-a", // non-cluster label must survive
		}
		_, _ = env.store.CreateClusterLBRegistration(context.Background(), storage.CreateClusterLBRegistrationParams{
			ClusterID:    clusterID,
			NodeID:       nid,
			Provider:     "mock",
			LBIdentifier: "nsx-pool-42",
		})
	}

	// Shrink 4 → 1 (delta=-3). Positions 3, 2, 1 should drain; position 0 remains.
	rec := env.call(http.MethodPatch, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", map[string]any{"desired_size": 1}, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for shrink, got %d body=%s", rec.Code, rec.Body.String())
	}

	env.runLastTask()

	remaining, _ := env.store.ListClusterMembers(context.Background(), clusterID)
	if len(remaining) != 1 {
		t.Fatalf("expected 1 member remaining, got %d", len(remaining))
	}
	if remaining[0].Position != 0 {
		t.Fatalf("expected position 0 to remain, got %d", remaining[0].Position)
	}

	// Drained nodes (positions 1..3) should have their LB registrations flipped
	// to deregistered_at != nil.
	for idx := 1; idx <= 3; idx++ {
		regs, _ := env.store.ListClusterLBRegistrationsForNode(context.Background(), nodeIDs[idx])
		if len(regs) == 0 {
			t.Fatalf("expected lb registration row persisted for drained node %s", nodeIDs[idx])
		}
		if regs[0].DeregisteredAt == nil {
			t.Fatalf("expected deregistered_at set for drained node %s, got %+v", nodeIDs[idx], regs[0])
		}
		// Cluster labels must be stripped from drained nodes; owner=team-a stays.
		labels := env.store.nodeLabels[nodeIDs[idx]]
		for k := range labels {
			if strings.HasPrefix(k, "cluster.") {
				t.Fatalf("expected cluster.* labels stripped from drained node %s, got %+v", nodeIDs[idx], labels)
			}
		}
		if labels["owner"] != "team-a" {
			t.Fatalf("expected non-cluster owner label preserved on drained node, got %+v", labels)
		}
	}

	// Survivor (position 0) must keep its cluster.* labels.
	survivorLabels := env.store.nodeLabels[nodeIDs[0]]
	if survivorLabels["cluster.env"] != "prod" {
		t.Fatalf("expected survivor to keep cluster.env=prod, got %+v", survivorLabels)
	}
}

// TestClusterTeardownReverseDrainsAndDeletes locks the full teardown contract:
// every member drains in reverse-position order and the cluster row is deleted.
func TestClusterTeardownReverseDrainsAndDeletes(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "teardown-me", Provider: "mock",
			DesiredSize: 3,
			RolePlan: map[string]any{
				"roles": []any{map[string]any{"name": "worker", "count": 3}},
			},
			Labels:                map[string]any{"lb_target_group_arn": "arn:aws:x:tg"},
			FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	nodeIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{
		clusterID: {
			{ClusterID: clusterID, NodeID: nodeIDs[0], Role: "worker", Position: 0, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: nodeIDs[1], Role: "worker", Position: 1, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: nodeIDs[2], Role: "worker", Position: 2, JoinedAt: time.Now()},
		},
	}
	for _, nid := range nodeIDs {
		_, _ = env.store.CreateClusterLBRegistration(context.Background(), storage.CreateClusterLBRegistrationParams{
			ClusterID:    clusterID,
			NodeID:       nid,
			Provider:     "mock",
			LBIdentifier: "arn:aws:x:tg",
		})
	}

	rec := env.call(http.MethodDelete, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", nil, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterAcceptedResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	env.runLastTask()

	cluster, _ := env.store.GetClusterByID(context.Background(), clusterID)
	if cluster != nil {
		t.Fatalf("expected cluster deleted, still exists: %+v", cluster)
	}
	members, _ := env.store.ListClusterMembers(context.Background(), clusterID)
	if len(members) != 0 {
		t.Fatalf("expected 0 members after teardown, got %d", len(members))
	}
	// All LB registrations should have deregistered_at set.
	for _, nid := range nodeIDs {
		regs, _ := env.store.ListClusterLBRegistrationsForNode(context.Background(), nid)
		if len(regs) == 0 {
			continue // cascade-delete in real DB; fake store keeps them
		}
		if regs[0].DeregisteredAt == nil {
			t.Fatalf("expected lb registration flipped to deregistered for node %s", nid)
		}
	}

	// Job should be flipped to succeeded.
	job, _ := env.store.GetJob(context.Background(), uuid.MustParse(resp.JobID))
	if job == nil || job.Status != storage.JobStatusSucceeded {
		t.Fatalf("expected teardown job succeeded, got %+v", job)
	}
}

// TestClusterPatchLabelsPropagatesToMembers verifies the handleUpdateCluster
// label-change path triggers PropagateClusterLabelsToNode for every member.
func TestClusterPatchLabelsPropagatesToMembers(t *testing.T) {
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID: clusterID, TenantID: env.tenantID, Name: "relabel-me", Provider: "mock",
			DesiredSize: 2,
			RolePlan: map[string]any{
				"roles": []any{map[string]any{"name": "worker", "count": 2}},
			},
			Labels: map[string]any{"env": "dev"}, FailureDomainStrategy: "spread", State: "running",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	nodeA, nodeB := uuid.New(), uuid.New()
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{
		clusterID: {
			{ClusterID: clusterID, NodeID: nodeA, Role: "worker", Position: 0, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: nodeB, Role: "worker", Position: 1, JoinedAt: time.Now()},
		},
	}

	rec := env.call(http.MethodPatch, "/api/v1/clusters/"+clusterID.String(),
		"cluster-admin", map[string]any{
			"labels": map[string]any{"env": "prod", "team": "platform"},
		}, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for label update, got %d body=%s", rec.Code, rec.Body.String())
	}

	for _, nid := range []uuid.UUID{nodeA, nodeB} {
		labels := env.store.nodeLabels[nid]
		if labels == nil {
			t.Fatalf("expected labels propagated to node %s", nid)
		}
		if labels["cluster.env"] != "prod" {
			t.Fatalf("expected cluster.env=prod on %s, got %+v", nid, labels)
		}
		if labels["cluster.team"] != "platform" {
			t.Fatalf("expected cluster.team=platform on %s, got %+v", nid, labels)
		}
	}
}
