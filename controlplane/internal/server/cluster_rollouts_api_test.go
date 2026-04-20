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

// setupRolloutAPIEnv primes a cluster with two members so rollout create
// succeeds out of the box. It returns the env plus the seeded cluster id.
func setupRolloutAPIEnv(t *testing.T) (*clustersTestEnv, uuid.UUID) {
	t.Helper()
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID:                    clusterID,
			TenantID:              env.tenantID,
			Name:                  "rollout-api",
			Provider:              "mock",
			DesiredSize:           2,
			RolePlan:              map[string]any{},
			Labels:                map[string]any{},
			FailureDomainStrategy: "spread",
			State:                 "running",
			CreatedAt:             time.Now(),
			UpdatedAt:             time.Now(),
		},
	}
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{
		clusterID: {
			{ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: 0, JoinedAt: time.Now()},
			{ClusterID: clusterID, NodeID: uuid.New(), Role: "worker", Position: 1, JoinedAt: time.Now()},
		},
	}
	return env, clusterID
}

func TestRolloutAPICreateRequiresAdmin(t *testing.T) {
	env, clusterID := setupRolloutAPIEnv(t)

	rec := env.call(http.MethodPost, "/api/v1/clusters/"+clusterID.String()+"/rollouts",
		"cluster-admin", map[string]any{
			"template_version_id": uuid.NewString(),
			"wave_size":           1,
		}, "viewer")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRolloutAPICreateRejectsInvalidPayload(t *testing.T) {
	env, clusterID := setupRolloutAPIEnv(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing template version", map[string]any{"wave_size": 2}},
		{"bad template version", map[string]any{"template_version_id": "not-a-uuid", "wave_size": 2}},
		{"zero wave size", map[string]any{"template_version_id": uuid.NewString(), "wave_size": 0}},
		{"bad gate type", map[string]any{
			"template_version_id": uuid.NewString(),
			"wave_size":           1,
			"health_gate":         map[string]any{"type": "nope"},
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := env.call(http.MethodPost, "/api/v1/clusters/"+clusterID.String()+"/rollouts",
				"cluster-admin", tc.body, "admin")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRolloutAPICreateAndGet(t *testing.T) {
	env, clusterID := setupRolloutAPIEnv(t)

	body := map[string]any{
		"template_version_id": uuid.NewString(),
		"wave_size":           1,
		"wave_strategy":       "rolling",
		"health_gate":         map[string]any{"type": "heartbeat", "grace": "1m", "timeout": "2m"},
	}
	rec := env.call(http.MethodPost, "/api/v1/clusters/"+clusterID.String()+"/rollouts",
		"cluster-admin", body, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	var accepted clusterRolloutAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accept: %v", err)
	}
	if accepted.RolloutID == "" || accepted.JobID == "" {
		t.Fatalf("expected rollout_id + job_id, got %+v", accepted)
	}
	if accepted.State != RolloutStatePending {
		t.Fatalf("expected state=pending, got %q", accepted.State)
	}

	// GET the rollout back.
	rec = env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String()+"/rollouts/"+accepted.RolloutID,
		"cluster-admin", nil, "viewer")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET, got %d body=%s", rec.Code, rec.Body.String())
	}
	var detail clusterRolloutDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.ID != accepted.RolloutID {
		t.Fatalf("rollout id mismatch: %s vs %s", detail.ID, accepted.RolloutID)
	}
	if detail.ClusterID != clusterID.String() {
		t.Fatalf("cluster id mismatch: %s vs %s", detail.ClusterID, clusterID.String())
	}
}

func TestRolloutAPIGetWrongClusterReturns404(t *testing.T) {
	env, clusterID := setupRolloutAPIEnv(t)

	// Seed a rollout belonging to a different cluster.
	otherCluster := uuid.New()
	rollout, err := env.store.CreateClusterRollout(context.Background(), storage.CreateClusterRolloutParams{
		ClusterID:         otherCluster,
		TemplateVersionID: uuid.New(),
		WaveSize:          1,
	})
	if err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	rec := env.call(http.MethodGet, "/api/v1/clusters/"+clusterID.String()+"/rollouts/"+rollout.ID.String(),
		"cluster-admin", nil, "viewer")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-cluster read, got %d", rec.Code)
	}
}

func TestRolloutAPIAbort(t *testing.T) {
	env, clusterID := setupRolloutAPIEnv(t)

	rollout, err := env.store.CreateClusterRollout(context.Background(), storage.CreateClusterRolloutParams{
		ClusterID:         clusterID,
		TemplateVersionID: uuid.New(),
		WaveSize:          1,
		State:             RolloutStateRunning,
	})
	if err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	rec := env.call(http.MethodPost, "/api/v1/clusters/"+clusterID.String()+"/rollouts/"+rollout.ID.String()+"/abort",
		"cluster-admin", nil, "admin")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Re-fetch — rollout should be aborted.
	got, _ := env.store.GetClusterRolloutByID(context.Background(), rollout.ID)
	if got == nil || got.State != RolloutStateAborted {
		t.Fatalf("expected state=aborted, got %+v", got)
	}
}

func TestRolloutAPIAbortViewerForbidden(t *testing.T) {
	env, clusterID := setupRolloutAPIEnv(t)

	rollout, err := env.store.CreateClusterRollout(context.Background(), storage.CreateClusterRolloutParams{
		ClusterID:         clusterID,
		TemplateVersionID: uuid.New(),
		WaveSize:          1,
		State:             RolloutStateRunning,
	})
	if err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	rec := env.call(http.MethodPost, "/api/v1/clusters/"+clusterID.String()+"/rollouts/"+rollout.ID.String()+"/abort",
		"cluster-admin", nil, "viewer")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRolloutAPIResumeRequiresHalted(t *testing.T) {
	env, clusterID := setupRolloutAPIEnv(t)

	rollout, err := env.store.CreateClusterRollout(context.Background(), storage.CreateClusterRolloutParams{
		ClusterID:         clusterID,
		TemplateVersionID: uuid.New(),
		WaveSize:          1,
		State:             RolloutStateRunning,
	})
	if err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	rec := env.call(http.MethodPost, "/api/v1/clusters/"+clusterID.String()+"/rollouts/"+rollout.ID.String()+"/resume",
		"cluster-admin", nil, "admin")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when resuming non-halted, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRolloutAPIResumeFromHalted(t *testing.T) {
	env, clusterID := setupRolloutAPIEnv(t)

	rollout, err := env.store.CreateClusterRollout(context.Background(), storage.CreateClusterRolloutParams{
		ClusterID:         clusterID,
		TemplateVersionID: uuid.New(),
		WaveSize:          1,
		State:             RolloutStateHalted,
		CurrentWave:       1,
	})
	if err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	rec := env.call(http.MethodPost, "/api/v1/clusters/"+clusterID.String()+"/rollouts/"+rollout.ID.String()+"/resume",
		"cluster-admin", nil, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	got, _ := env.store.GetClusterRolloutByID(context.Background(), rollout.ID)
	if got == nil || got.State != RolloutStateRunning {
		t.Fatalf("expected state=running after resume, got %+v", got)
	}
	if env.queue.count() == 0 {
		t.Fatalf("expected an advance task enqueued on resume")
	}
}
