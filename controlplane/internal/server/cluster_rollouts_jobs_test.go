package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// rolloutJobFixture wires a server with a cluster that has pre-provisioned
// members so rollouts can advance without hitting the full provision flow.
type rolloutJobFixture struct {
	env               *clustersTestEnv
	clusterID         uuid.UUID
	memberIDs         []uuid.UUID
	templateVersionID uuid.UUID
}

func newRolloutJobFixture(t *testing.T, totalMembers int) *rolloutJobFixture {
	t.Helper()
	env := setupClustersEnv(t, "cluster-admin", "viewer")

	clusterID := uuid.New()
	env.store.clusters = map[uuid.UUID]*storage.Cluster{
		clusterID: {
			ID:                    clusterID,
			TenantID:              env.tenantID,
			Name:                  "rollout-job",
			Provider:              "mock",
			DesiredSize:           totalMembers,
			RolePlan:              map[string]any{},
			Labels:                map[string]any{},
			FailureDomainStrategy: "spread",
			State:                 "running",
			CreatedAt:             time.Now(),
			UpdatedAt:             time.Now(),
		},
	}
	memberIDs := make([]uuid.UUID, 0, totalMembers)
	members := make([]storage.ClusterMember, 0, totalMembers)
	for i := 0; i < totalMembers; i++ {
		id := uuid.New()
		memberIDs = append(memberIDs, id)
		members = append(members, storage.ClusterMember{
			ClusterID: clusterID,
			NodeID:    id,
			Role:      "worker",
			Position:  i,
			JoinedAt:  time.Now(),
		})
		env.store.nodes = append(env.store.nodes, storage.Node{
			ID:       id,
			TenantID: env.tenantID,
			Hostname: "host-" + id.String(),
			State:    storage.NodeStateActive,
		})
	}
	env.store.clusterMembers = map[uuid.UUID][]storage.ClusterMember{clusterID: members}
	templateVersionID := seedClusterRolloutTemplateVersion(t, env, "mock")

	clearTestNodeLastSeen()
	t.Cleanup(clearTestNodeLastSeen)

	return &rolloutJobFixture{env: env, clusterID: clusterID, memberIDs: memberIDs, templateVersionID: templateVersionID}
}

// execCapturedTasks drains the queue once, executing each task synchronously.
// Returns the number of tasks executed. Tasks enqueued during execution are
// visible to the next drain.
func execCapturedTasks(t *testing.T, env *clustersTestEnv) int {
	t.Helper()
	env.queue.mu.Lock()
	pending := env.queue.tasks
	env.queue.tasks = nil
	env.queue.mu.Unlock()
	for _, task := range pending {
		if err := task.Job(context.Background()); err != nil {
			t.Fatalf("task %s failed: %v", task.Name, err)
		}
	}
	return len(pending)
}

// drainLoop drains tasks up to maxRounds times until the queue settles.
func drainLoop(t *testing.T, f *rolloutJobFixture, maxRounds int) int {
	t.Helper()
	total := 0
	for i := 0; i < maxRounds; i++ {
		ran := execCapturedTasks(t, f.env)
		total += ran
		if ran == 0 {
			return total
		}
	}
	return total
}

func startRolloutJobTest(t *testing.T, f *rolloutJobFixture, body map[string]any) clusterRolloutAcceptedResponse {
	t.Helper()
	rec := f.env.call(http.MethodPost, "/api/v1/clusters/"+f.clusterID.String()+"/rollouts",
		"cluster-admin", body, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create rollout: got %d body=%s", rec.Code, rec.Body.String())
	}
	var accepted clusterRolloutAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accept: %v", err)
	}
	return accepted
}

func TestRolloutAdvanceHeartbeatHealthyAllWaves(t *testing.T) {
	f := newRolloutJobFixture(t, 4)

	future := time.Now().Add(10 * time.Second)
	for _, id := range f.memberIDs {
		setTestNodeLastSeen(id, future)
	}

	accepted := startRolloutJobTest(t, f, map[string]any{
		"template_version_id": f.templateVersionID.String(),
		"wave_size":           2,
		"health_gate": map[string]any{
			"type":        "heartbeat",
			"grace":       "5m",
			"start_delay": 0,
			"interval":    0,
			"timeout":     "10m",
		},
	})

	ran := drainLoop(t, f, 25)
	if ran == 0 {
		t.Fatalf("no tasks ran")
	}

	rollout, _ := f.env.store.GetClusterRolloutByID(context.Background(), uuid.MustParse(accepted.RolloutID))
	if rollout == nil {
		t.Fatalf("rollout disappeared")
	}
	if rollout.State != RolloutStateCompleted {
		t.Fatalf("expected state=completed, got %q", rollout.State)
	}

	waves, _ := f.env.store.ListClusterRolloutWaves(context.Background(), rollout.ID)
	if len(waves) != 2 {
		t.Fatalf("expected 2 waves for 4 members @ wave_size=2, got %d", len(waves))
	}
	for _, w := range waves {
		if w.State != storage.ClusterRolloutWaveStateHealthy {
			t.Fatalf("wave %d state=%q, expected healthy", w.WaveNumber, w.State)
		}
	}
}

func TestRolloutAdvanceHeartbeatHaltsOnMissingHeartbeat(t *testing.T) {
	f := newRolloutJobFixture(t, 2)

	// No heartbeats => every node is "never heartbeated" => gate fails.
	// timeout=0 so time.Since(StartedAt) >= timeout fires deterministically
	// regardless of wall-clock resolution (Windows ~15.6ms would flake on 1ns).
	accepted := startRolloutJobTest(t, f, map[string]any{
		"template_version_id": f.templateVersionID.String(),
		"wave_size":           2,
		"health_gate": map[string]any{
			"type":        "heartbeat",
			"grace":       "5m",
			"start_delay": 0,
			"interval":    0,
			"timeout":     "0",
		},
	})

	drainLoop(t, f, 10)

	rollout, _ := f.env.store.GetClusterRolloutByID(context.Background(), uuid.MustParse(accepted.RolloutID))
	if rollout == nil {
		t.Fatalf("rollout disappeared")
	}
	if rollout.State != RolloutStateHalted {
		t.Fatalf("expected state=halted, got %q", rollout.State)
	}
	waves, _ := f.env.store.ListClusterRolloutWaves(context.Background(), rollout.ID)
	if len(waves) != 1 {
		t.Fatalf("expected 1 wave recorded, got %d", len(waves))
	}
	if waves[0].State != storage.ClusterRolloutWaveStateUnhealthy {
		t.Fatalf("wave state=%q, expected unhealthy", waves[0].State)
	}
}

func TestRolloutAdvanceComplianceGate(t *testing.T) {
	f := newRolloutJobFixture(t, 2)

	// Seed verified compliance results with a checked_at in the future so
	// they land AFTER the wave's StartedAt (server uses Now() at wave create).
	future := time.Now().Add(1 * time.Hour)
	jobID := uuid.New()
	f.env.store.complianceResults = map[uuid.UUID][]storage.ComplianceResult{
		jobID: {
			{ID: uuid.New(), JobID: jobID, NodeID: f.memberIDs[0], RuleID: "cis-1.1.1", Passed: true, Verified: true, CheckedAt: &future, CreatedAt: future},
			{ID: uuid.New(), JobID: jobID, NodeID: f.memberIDs[1], RuleID: "cis-1.1.1", Passed: true, Verified: true, CheckedAt: &future, CreatedAt: future},
		},
	}

	accepted := startRolloutJobTest(t, f, map[string]any{
		"template_version_id": f.templateVersionID.String(),
		"wave_size":           2,
		"health_gate": map[string]any{
			"type":        "compliance",
			"rules":       []string{"cis-1.1.1"},
			"start_delay": 0,
			"interval":    0,
			"timeout":     "10m",
		},
	})

	drainLoop(t, f, 15)

	rollout, _ := f.env.store.GetClusterRolloutByID(context.Background(), uuid.MustParse(accepted.RolloutID))
	if rollout == nil {
		t.Fatalf("rollout disappeared")
	}
	if rollout.State != RolloutStateCompleted {
		t.Fatalf("expected state=completed, got %q", rollout.State)
	}
}

func TestRolloutHTTPGatePassesWhenProbeSucceeds(t *testing.T) {
	f := newRolloutJobFixture(t, 1)

	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("healthy"))
	}))
	t.Cleanup(probe.Close)

	host, port := splitHostPort(probe.URL)
	f.env.store.nodes[0].PublicIP = sql.NullString{String: host, Valid: true}

	accepted := startRolloutJobTest(t, f, map[string]any{
		"template_version_id": f.templateVersionID.String(),
		"wave_size":           1,
		"health_gate": map[string]any{
			"type":          "http",
			"port":          port,
			"path":          "/",
			"expect":        200,
			"probe_timeout": "2s",
			"start_delay":   0,
			"interval":      0,
			"timeout":       "5s",
		},
	})

	drainLoop(t, f, 10)

	rollout, _ := f.env.store.GetClusterRolloutByID(context.Background(), uuid.MustParse(accepted.RolloutID))
	if rollout == nil {
		t.Fatalf("rollout disappeared")
	}
	if rollout.State != RolloutStateCompleted {
		t.Fatalf("expected state=completed, got %q", rollout.State)
	}
}

func TestRolloutAbortCancelsInProgressWave(t *testing.T) {
	f := newRolloutJobFixture(t, 4)

	accepted := startRolloutJobTest(t, f, map[string]any{
		"template_version_id": f.templateVersionID.String(),
		"wave_size":           2,
		"health_gate": map[string]any{
			"type":        "heartbeat",
			"grace":       "5m",
			"start_delay": 0,
			"interval":    "1h",
			"timeout":     "1h",
		},
	})

	// One round to run advance (which enqueues gate_check).
	execCapturedTasks(t, f.env)
	// One round for gate_check (which re-enqueues itself because pending).
	execCapturedTasks(t, f.env)

	rec := f.env.call(http.MethodPost,
		"/api/v1/clusters/"+f.clusterID.String()+"/rollouts/"+accepted.RolloutID+"/abort",
		"cluster-admin", nil, "admin")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("abort got %d body=%s", rec.Code, rec.Body.String())
	}

	rollout, _ := f.env.store.GetClusterRolloutByID(context.Background(), uuid.MustParse(accepted.RolloutID))
	if rollout == nil || rollout.State != RolloutStateAborted {
		t.Fatalf("expected aborted, got %+v", rollout)
	}
	wave, _ := f.env.store.GetClusterRolloutWaveByNumber(context.Background(), rollout.ID, 0)
	if wave == nil {
		t.Fatalf("expected a wave 0 recorded")
	}
	if wave.State != storage.ClusterRolloutWaveStateAborted {
		t.Fatalf("expected wave state=aborted, got %q", wave.State)
	}
}

func TestRolloutResumeReenqueuesAdvanceAtCurrentWave(t *testing.T) {
	f := newRolloutJobFixture(t, 4)

	rollout, err := f.env.store.CreateClusterRollout(context.Background(), storage.CreateClusterRolloutParams{
		ClusterID:         f.clusterID,
		TemplateVersionID: uuid.New(),
		WaveSize:          2,
		State:             RolloutStateHalted,
		CurrentWave:       1,
		HealthGate: map[string]any{
			"type":        "heartbeat",
			"grace":       "5m",
			"start_delay": 0,
			"interval":    0,
			"timeout":     "10m",
		},
	})
	if err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	future := time.Now().Add(10 * time.Second)
	for _, id := range f.memberIDs[2:4] {
		setTestNodeLastSeen(id, future)
	}

	rec := f.env.call(http.MethodPost,
		"/api/v1/clusters/"+f.clusterID.String()+"/rollouts/"+rollout.ID.String()+"/resume",
		"cluster-admin", nil, "admin")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("resume got %d body=%s", rec.Code, rec.Body.String())
	}

	got, _ := f.env.store.GetClusterRolloutByID(context.Background(), rollout.ID)
	if got == nil || got.State != RolloutStateRunning {
		t.Fatalf("expected running after resume, got %+v", got)
	}

	drainLoop(t, f, 15)

	got, _ = f.env.store.GetClusterRolloutByID(context.Background(), rollout.ID)
	if got == nil || got.State != RolloutStateCompleted {
		t.Fatalf("expected completed after resume + drain, got %+v", got)
	}
}

// splitHostPort parses a URL like "http://127.0.0.1:43521" into host and port.
// Uses manual parsing to avoid importing net/url for such a tiny need.
func splitHostPort(url string) (string, int) {
	const prefix = "http://"
	if len(url) < len(prefix) {
		return "", 0
	}
	rest := url[len(prefix):]
	host := ""
	portStart := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' {
			host = rest[:i]
			portStart = i + 1
			break
		}
	}
	if portStart < 0 {
		return rest, 0
	}
	port, _ := strconv.Atoi(rest[portStart:])
	return host, port
}
