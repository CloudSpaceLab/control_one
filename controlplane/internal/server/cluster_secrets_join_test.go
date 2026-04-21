package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// TestClusterSecretIntegrationThreeNodeScale replays the task exit criterion:
//  1. Provision a 3-node cluster
//  2. Upsert a cluster-scoped secret — every node should receive it
//  3. Scale to 4 nodes — the new node should also receive the secret
//  4. Delete the secret — every node should get a tombstone
func TestClusterSecretIntegrationThreeNodeScale(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")
	ctx := context.Background()

	// ── 1. Provision the initial 3-node cluster via the API ────────────
	createBody := map[string]any{
		"tenant_id": env.tenantID.String(),
		"name":      "integration-3n",
		"provider":  "mock",
		"role_plan": map[string]any{
			"roles": []map[string]any{
				{"name": "worker", "count": 3},
			},
		},
		"labels": map[string]any{},
	}
	rec := env.call(http.MethodPost, "/api/v1/clusters", "cs-admin", createBody, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cluster create: expected 202 got %d body=%s", rec.Code, rec.Body.String())
	}
	// Run the provision job so the 3 members exist.
	env.runLastTask()

	clusters, _, err := env.store.ListClusters(ctx, env.tenantID, 0, 0)
	if err != nil {
		t.Fatalf("list clusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	cluster := &clusters[0]

	members, err := env.store.ListClusterMembers(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members after provision, got %d", len(members))
	}

	// ── 2. Upsert a cluster secret — verify fan-out to all 3 ───────────
	putRec := env.call(http.MethodPut,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/DB_PASSWORD",
		"cs-admin",
		map[string]any{"value": "p@ssw0rd"},
		"admin",
	)
	if putRec.Code != http.StatusOK {
		t.Fatalf("secret upsert: %d body=%s", putRec.Code, putRec.Body.String())
	}
	env.runLastTask() // run the fan-out job

	state, err := env.store.ListClusterSecretNodeState(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("list state: %v", err)
	}
	if len(state) != 3 {
		t.Fatalf("expected 3 push rows (one per node), got %d", len(state))
	}
	for _, s := range state {
		if s.Action != ClusterSecretFanOutActionUpsert {
			t.Fatalf("expected upsert action, got %s", s.Action)
		}
		if s.Key != "DB_PASSWORD" {
			t.Fatalf("expected DB_PASSWORD, got %s", s.Key)
		}
	}

	// ── 3. Scale to 4 nodes — new node should auto-receive the secret ──
	scaleBody := map[string]any{
		"desired_size": 4,
		"role_plan": map[string]any{
			"roles": []map[string]any{
				{"name": "worker", "count": 4},
			},
		},
	}
	scaleRec := env.call(http.MethodPatch,
		"/api/v1/clusters/"+cluster.ID.String(),
		"cs-admin",
		scaleBody,
		"admin",
	)
	if scaleRec.Code != http.StatusAccepted {
		t.Fatalf("scale: expected 202 got %d body=%s", scaleRec.Code, scaleRec.Body.String())
	}
	env.runLastTask() // provision the 4th member — the join hook runs here

	members, err = env.store.ListClusterMembers(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("list members after scale: %v", err)
	}
	if len(members) != 4 {
		t.Fatalf("expected 4 members after scale, got %d", len(members))
	}
	// Find the new node id — it's the one not in the original 3.
	originalIDs := map[uuid.UUID]bool{}
	// We don't have the original member list persisted here, so just
	// assert every member has a push row for the secret.
	state, err = env.store.ListClusterSecretNodeState(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("list state after scale: %v", err)
	}
	if len(state) != 4 {
		t.Fatalf("expected 4 push rows after scale-up, got %d", len(state))
	}
	coveredNodes := map[uuid.UUID]bool{}
	for _, s := range state {
		coveredNodes[s.NodeID] = true
	}
	for _, m := range members {
		if !coveredNodes[m.NodeID] {
			t.Fatalf("member %s missing secret push row after join", m.NodeID)
		}
	}
	_ = originalIDs // silence unused

	// ── 4. Delete the secret — tombstones on every node ────────────────
	delRec := env.call(http.MethodDelete,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/DB_PASSWORD",
		"cs-admin",
		nil,
		"admin",
	)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204 got %d body=%s", delRec.Code, delRec.Body.String())
	}
	env.runLastTask()

	state, err = env.store.ListClusterSecretNodeState(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("list state after delete: %v", err)
	}
	if len(state) != 4 {
		t.Fatalf("expected 4 tombstone rows, got %d", len(state))
	}
	for _, s := range state {
		if s.Action != ClusterSecretFanOutActionDelete {
			t.Fatalf("expected delete action, got %s", s.Action)
		}
	}

	// ── Sanity: the cluster_secrets row itself is gone ─────────────────
	sec, err := env.store.GetClusterSecret(ctx, cluster.ID, "DB_PASSWORD")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if sec != nil {
		t.Fatalf("expected secret row to be gone, got %+v", sec)
	}
}

// TestClusterSecretJoinHookIdempotent exercises the join path when the
// secret set is empty — should be a no-op rather than a hard failure.
func TestClusterSecretJoinHookEmptySet(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")
	ctx := context.Background()

	cluster, err := env.store.CreateCluster(ctx, storage.CreateClusterParams{
		TenantID:    env.tenantID,
		Name:        "empty-secrets",
		Provider:    "mock",
		DesiredSize: 1,
	})
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}
	nodeID := uuid.New()
	pushed, failed := env.srv.PushClusterSecretsToNewMember(ctx, cluster.ID, nodeID)
	if pushed != 0 || failed != 0 {
		t.Fatalf("expected zero pushes on empty secret set, got pushed=%d failed=%d", pushed, failed)
	}
}
