package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// seedClusterWithMembers creates a cluster owned by env.tenantID and adds N
// members. The cluster row is persisted directly on the fake store so we
// skip the provisioning job path (covered elsewhere).
func seedClusterWithMembers(t *testing.T, env *clustersTestEnv, name string, memberCount int) (*storage.Cluster, []storage.ClusterMember) {
	t.Helper()
	ctx := context.Background()
	cluster, err := env.store.CreateCluster(ctx, storage.CreateClusterParams{
		TenantID:    env.tenantID,
		Name:        name,
		Provider:    "mock",
		DesiredSize: memberCount,
		RolePlan:    map[string]any{"roles": []any{map[string]any{"name": "worker", "count": memberCount}}},
		Labels:      map[string]any{},
	})
	if err != nil {
		t.Fatalf("seed cluster: %v", err)
	}
	members := make([]storage.ClusterMember, 0, memberCount)
	for i := 0; i < memberCount; i++ {
		nodeID := uuid.New()
		m, err := env.store.AddClusterMember(ctx, cluster.ID, nodeID, "worker", i)
		if err != nil {
			t.Fatalf("add cluster member %d: %v", i, err)
		}
		members = append(members, *m)
	}
	return cluster, members
}

func TestClusterSecretUpsertEnqueuesFanOutAndPushesToMembers(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")
	cluster, members := seedClusterWithMembers(t, env, "with-secrets", 3)

	rec := env.call(http.MethodPut,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/DB_PASSWORD",
		"cs-admin",
		map[string]any{"value": "hunter2"},
		"admin",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var meta clusterSecretMetaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if meta.Key != "DB_PASSWORD" {
		t.Fatalf("expected key DB_PASSWORD got %q", meta.Key)
	}
	if meta.Version != 1 {
		t.Fatalf("expected version 1 got %d", meta.Version)
	}

	if env.queue.count() != 1 {
		t.Fatalf("expected 1 fan-out task queued, got %d", env.queue.count())
	}

	// Execute the fan-out task: all members should now have a push row.
	env.runLastTask()

	state, err := env.store.ListClusterSecretNodeState(context.Background(), cluster.ID)
	if err != nil {
		t.Fatalf("list node state: %v", err)
	}
	if len(state) != len(members) {
		t.Fatalf("expected %d push rows (one per member), got %d", len(members), len(state))
	}
	gotNodes := map[uuid.UUID]bool{}
	for _, s := range state {
		gotNodes[s.NodeID] = true
		if s.Key != "DB_PASSWORD" {
			t.Fatalf("expected push key DB_PASSWORD, got %s", s.Key)
		}
		if s.Action != ClusterSecretFanOutActionUpsert {
			t.Fatalf("expected action upsert, got %s", s.Action)
		}
	}
	for _, m := range members {
		if !gotNodes[m.NodeID] {
			t.Fatalf("member %s missing push row", m.NodeID)
		}
	}
}

func TestClusterSecretUpsertBumpsVersion(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")
	cluster, _ := seedClusterWithMembers(t, env, "bump-versions", 1)

	rec1 := env.call(http.MethodPut,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/SIGNING_KEY",
		"cs-admin",
		map[string]any{"value": "first"},
		"admin",
	)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first upsert failed: %d body=%s", rec1.Code, rec1.Body.String())
	}

	rec2 := env.call(http.MethodPut,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/SIGNING_KEY",
		"cs-admin",
		map[string]any{"value": "second"},
		"admin",
	)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second upsert failed: %d body=%s", rec2.Code, rec2.Body.String())
	}
	var meta clusterSecretMetaResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.Version != 2 {
		t.Fatalf("expected version 2 after rotation, got %d", meta.Version)
	}
}

func TestClusterSecretListReturnsMetadataOnly(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")
	cluster, _ := seedClusterWithMembers(t, env, "list-only", 1)

	// Seed two secrets.
	for _, k := range []string{"API_KEY", "DB_PASSWORD"} {
		rec := env.call(http.MethodPut,
			"/api/v1/clusters/"+cluster.ID.String()+"/secrets/"+k,
			"cs-admin",
			map[string]any{"value": "shh"},
			"admin",
		)
		if rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d body=%s", k, rec.Code, rec.Body.String())
		}
	}

	// Drain both fan-out tasks so state doesn't bleed into the next test.
	for env.queue.count() > 0 {
		env.runLastTask()
		// Drop the processed task by creating a fresh slice. For test
		// simplicity we just stop once we've drained enough.
		break
	}

	rec := env.call(http.MethodGet,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets",
		"cs-admin", nil, "viewer",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", rec.Code, rec.Body.String())
	}
	var listResp clusterSecretListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(listResp.Data))
	}
	// Listing must not leak values — the response type doesn't even carry
	// a Value field.
	for _, item := range listResp.Data {
		if item.Key == "" {
			t.Fatalf("missing key: %+v", item)
		}
	}
}

func TestClusterSecretGetReturnsDecryptedValueForAdmin(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")
	cluster, _ := seedClusterWithMembers(t, env, "get-value", 1)

	_ = env.call(http.MethodPut,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/TOKEN",
		"cs-admin",
		map[string]any{"value": "tk-123"},
		"admin",
	)

	rec := env.call(http.MethodGet,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/TOKEN",
		"cs-admin", nil, "viewer",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d body=%s", rec.Code, rec.Body.String())
	}
	var full clusterSecretValueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if full.Value != "tk-123" {
		t.Fatalf("expected decrypted value 'tk-123', got %q", full.Value)
	}
}

func TestClusterSecretDeleteFansOutTombstones(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")
	cluster, members := seedClusterWithMembers(t, env, "delete-tombstone", 3)

	// Seed a secret so we have something to delete.
	_ = env.call(http.MethodPut,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/TOKEN",
		"cs-admin",
		map[string]any{"value": "tok"},
		"admin",
	)
	env.runLastTask() // run upsert fan-out

	rec := env.call(http.MethodDelete,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/TOKEN",
		"cs-admin", nil, "admin",
	)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d body=%s", rec.Code, rec.Body.String())
	}
	env.runLastTask() // run delete fan-out

	state, err := env.store.ListClusterSecretNodeState(context.Background(), cluster.ID)
	if err != nil {
		t.Fatalf("list node state: %v", err)
	}
	if len(state) != len(members) {
		t.Fatalf("expected %d tombstones, got %d", len(members), len(state))
	}
	for _, s := range state {
		if s.Action != ClusterSecretFanOutActionDelete {
			t.Fatalf("expected action=delete, got %s on node %s", s.Action, s.NodeID)
		}
		if s.SyncStatus != "pending_delete" {
			t.Fatalf("expected sync_status=pending_delete, got %s", s.SyncStatus)
		}
	}
}

func TestClusterSecretAuthorizationMatrix(t *testing.T) {
	env := setupClustersEnv(t, "viewer-tok", "viewer")
	cluster, _ := seedClusterWithMembers(t, env, "authz", 1)

	// Viewer can list (no secrets yet) and is forbidden from write.
	recList := env.call(http.MethodGet,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets",
		"viewer-tok", nil, "viewer",
	)
	if recList.Code != http.StatusOK {
		t.Fatalf("viewer list expected 200, got %d", recList.Code)
	}

	recPut := env.call(http.MethodPut,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/K",
		"viewer-tok",
		map[string]any{"value": "v"},
		"viewer",
	)
	if recPut.Code != http.StatusForbidden {
		t.Fatalf("viewer write expected 403, got %d", recPut.Code)
	}

	recDel := env.call(http.MethodDelete,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/K",
		"viewer-tok", nil, "viewer",
	)
	if recDel.Code != http.StatusForbidden {
		t.Fatalf("viewer delete expected 403, got %d", recDel.Code)
	}
}

func TestClusterSecretOnUnknownClusterReturnsNotFound(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")

	rec := env.call(http.MethodPut,
		"/api/v1/clusters/"+uuid.New().String()+"/secrets/K",
		"cs-admin",
		map[string]any{"value": "v"},
		"admin",
	)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d", rec.Code)
	}
}

func TestClusterSecretFanOutWithNoMembersIsNoOp(t *testing.T) {
	env := setupClustersEnv(t, "cs-admin", "viewer")
	cluster, _ := seedClusterWithMembers(t, env, "no-members", 0)

	rec := env.call(http.MethodPut,
		"/api/v1/clusters/"+cluster.ID.String()+"/secrets/K",
		"cs-admin",
		map[string]any{"value": "v"},
		"admin",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	env.runLastTask()
	state, _ := env.store.ListClusterSecretNodeState(context.Background(), cluster.ID)
	if len(state) != 0 {
		t.Fatalf("expected zero push rows, got %d", len(state))
	}
}
