package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

// JobTypeClusterSecretFanOut is the async job that pushes a cluster-scoped
// secret (or secret removal) to every current cluster member. The job is
// idempotent: re-running it against the same (cluster, key) tuple
// re-performs the push with the latest authoritative row — older state
// on a node is simply overwritten.
const JobTypeClusterSecretFanOut = "cluster.secret.fan_out"

// ClusterSecretFanOutPayload is the encoded job body. `Action` is one of
// `upsert` or `delete`. For `upsert` the worker reads the authoritative row
// at execution time so rapid successive PUTs collapse naturally — only the
// most recent version lands on the members.
type ClusterSecretFanOutPayload struct {
	ClusterID string `json:"cluster_id"`
	TenantID  string `json:"tenant_id"`
	Key       string `json:"key"`
	Action    string `json:"action"`
}

// enqueueClusterSecretFanOut creates a cluster.secret.fan_out job+task pair.
// Called from the PUT and DELETE cluster-secret handlers. Returns the job id
// so the handler can record it in the audit log if desired.
func (s *Server) enqueueClusterSecretFanOut(r *http.Request, cluster *storage.Cluster, action, key string) (uuid.UUID, error) {
	if !isValidClusterSecretFanOutAction(action) {
		return uuid.Nil, fmt.Errorf("unsupported fan-out action %q", action)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return uuid.Nil, errors.New("key is required for fan-out job")
	}

	payload := ClusterSecretFanOutPayload{
		ClusterID: cluster.ID.String(),
		TenantID:  cluster.TenantID.String(),
		Key:       key,
		Action:    action,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal cluster.secret.fan_out payload: %w", err)
	}

	job := &storage.Job{
		TenantID: cluster.TenantID,
		Type:     JobTypeClusterSecretFanOut,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	event := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: fmt.Sprintf("cluster.secret.fan_out queued (action=%s, key=%s)", action, key),
	}

	ctx := r.Context()
	created, err := s.store.CreateJob(ctx, job, event)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create cluster.secret.fan_out job: %w", err)
	}

	if s.worker == nil {
		return uuid.Nil, errors.New("worker unavailable")
	}
	task := worker.Task{
		Name:         fmt.Sprintf("cluster-secret-fanout-%s", created.ID),
		Job:          s.buildClusterSecretFanOutJob(created.ID, cluster.ID, cluster.TenantID, key, action),
		MaxAttempts:  3,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, created.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), nil)
		return uuid.Nil, fmt.Errorf("enqueue cluster.secret.fan_out task: %w", err)
	}
	return created.ID, nil
}

// buildClusterSecretFanOutJob returns the handler that walks every cluster
// member and applies the action. The body reloads the authoritative secret
// row at execution time so it's safe to re-run after a failure.
func (s *Server) buildClusterSecretFanOutJob(jobID, clusterID, tenantID uuid.UUID, key, action string) func(context.Context) error {
	return func(ctx context.Context) error {
		_ = tenantID // reserved for future tenant-scoped audit enrichment

		_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning,
			fmt.Sprintf("fan-out started (action=%s, key=%s)", action, key),
			map[string]any{"started_at": time.Now()})

		members, err := s.store.ListClusterMembers(ctx, clusterID)
		if err != nil {
			return s.failClusterJob(ctx, jobID, fmt.Errorf("list cluster members: %w", err))
		}
		if len(members) == 0 {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "fan-out no-op (zero members)", map[string]any{
				"finished_at": time.Now(),
			})
			return nil
		}

		var value string
		switch action {
		case ClusterSecretFanOutActionUpsert:
			secret, getErr := s.store.GetClusterSecretDecrypted(ctx, clusterID, key)
			if getErr != nil {
				return s.failClusterJob(ctx, jobID, fmt.Errorf("load cluster secret for fan-out: %w", getErr))
			}
			if secret == nil {
				// The secret was deleted between enqueue and execution. Treat
				// as a no-op upsert and let the subsequent delete fan-out
				// reconcile the members.
				_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded,
					"fan-out no-op (secret not found at execution time)",
					map[string]any{"finished_at": time.Now()})
				return nil
			}
			value = secret.Value
		case ClusterSecretFanOutActionDelete:
			value = ""
		default:
			return s.failClusterJob(ctx, jobID, fmt.Errorf("unsupported action %q", action))
		}

		var pushed, failed int
		for _, m := range members {
			pushErr := s.pushClusterSecretToNode(ctx, clusterID, m.NodeID, key, value, action)
			if pushErr != nil {
				s.logger.Warn("cluster secret push failed",
					zap.String("cluster_id", clusterID.String()),
					zap.String("node_id", m.NodeID.String()),
					zap.String("key", key),
					zap.String("action", action),
					zap.Error(pushErr),
				)
				failed++
				continue
			}
			pushed++
		}

		msg := fmt.Sprintf("fan-out completed (pushed=%d, failed=%d, action=%s, key=%s)", pushed, failed, action, key)
		status := storage.JobStatusSucceeded
		if failed > 0 && pushed == 0 {
			status = storage.JobStatusFailed
		}
		_ = s.store.UpdateJobStatus(ctx, jobID, status, msg, map[string]any{
			"finished_at": time.Now(),
		})
		return nil
	}
}

// PushClusterSecretsToNewMember pushes every current cluster_secrets row to
// a single node. Invoked from the cluster-member-join hook so a freshly
// joined node receives the cluster-wide secret set without operator
// intervention. Returns the number of pushes that succeeded + failed so
// callers can log aggregate progress.
func (s *Server) PushClusterSecretsToNewMember(ctx context.Context, clusterID, nodeID uuid.UUID) (pushed, failed int) {
	if s.store == nil {
		return 0, 0
	}
	secrets, err := s.store.ListClusterSecretsDecrypted(ctx, clusterID)
	if err != nil {
		s.logger.Warn("list cluster secrets for join push",
			zap.String("cluster_id", clusterID.String()),
			zap.String("node_id", nodeID.String()),
			zap.Error(err),
		)
		return 0, 0
	}
	for _, sec := range secrets {
		if err := s.pushClusterSecretToNode(ctx, clusterID, nodeID, sec.Key, sec.Value, ClusterSecretFanOutActionUpsert); err != nil {
			s.logger.Warn("cluster secret join-push failed",
				zap.String("cluster_id", clusterID.String()),
				zap.String("node_id", nodeID.String()),
				zap.String("key", sec.Key),
				zap.Error(err),
			)
			failed++
			continue
		}
		pushed++
	}
	return pushed, failed
}

// pushClusterSecretToNode records a per-node delivery row for a single
// (cluster, node, key) tuple. We intentionally keep the push idempotent by
// upserting into cluster_secret_node_state — re-running the fan-out worker
// against the same inputs produces the same observable state.
//
// Implementation note: this function performs the bookkeeping only. The
// actual on-node write is driven by the agent's next /secrets/sync poll.
// By persisting the sync intent on the control plane side, the agent
// converges naturally on its next cycle. For delete actions we record a
// tombstone (action='delete') so the agent knows to unset the value;
// sweeping the tombstones once every node has acked is a future concern
// that lives outside this worktree.
func (s *Server) pushClusterSecretToNode(ctx context.Context, clusterID, nodeID uuid.UUID, key, value, action string) error {
	_ = value // plaintext is intentionally not stored in metadata; node pulls via /secrets/sync

	syncStatus := "pending"
	if action == ClusterSecretFanOutActionDelete {
		syncStatus = "pending_delete"
	}

	if err := s.store.RecordClusterSecretPush(ctx, storage.ClusterSecretPushRecord{
		ClusterID:  clusterID,
		NodeID:     nodeID,
		Key:        key,
		Action:     action,
		SyncStatus: syncStatus,
	}); err != nil {
		return fmt.Errorf("record cluster secret push: %w", err)
	}
	return nil
}

// Fan-out action constants (duplicated on the storage layer too so worker
// consumers don't need to import server).
const (
	ClusterSecretFanOutActionUpsert = "upsert"
	ClusterSecretFanOutActionDelete = "delete"
)

func isValidClusterSecretFanOutAction(action string) bool {
	switch action {
	case ClusterSecretFanOutActionUpsert, ClusterSecretFanOutActionDelete:
		return true
	default:
		return false
	}
}
