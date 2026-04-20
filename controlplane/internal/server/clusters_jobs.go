package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
)

// errClusterShrinkDeferred surfaces the Sprint 1 contract to any caller that
// invokes the shrink code path directly (e.g., from a worker that got a stale
// payload). The HTTP handler returns 501 well before this is reached, but
// keeping the error here documents the deferral at the job layer too.
// Teardown deferral is handled exclusively at the HTTP layer since there is
// no cluster.teardown job wired up in Sprint 1.
var errClusterShrinkDeferred = errors.New(clusterShrinkDeferredMessage)

// buildClusterProvisionJob returns a worker.Task-compatible function that
// provisions every node in the cluster's role_plan and attaches them as
// cluster members. The function is idempotent: already-populated (role,
// position) slots are skipped.
func (s *Server) buildClusterProvisionJob(jobID, clusterID, tenantID uuid.UUID) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := s.provisionClusterMembers(ctx, jobID, clusterID, tenantID, 0); err != nil {
			return err
		}
		return nil
	}
}

// buildClusterScaleJob returns a worker.Task function that adds `delta` new
// nodes to the cluster, preferring the most-underfilled role. Sprint 1 only
// supports expand; delta must be > 0. Negative deltas (shrink) are rejected at
// the HTTP layer with 501.
func (s *Server) buildClusterScaleJob(jobID, clusterID, tenantID uuid.UUID, delta int) func(context.Context) error {
	return func(ctx context.Context) error {
		if delta <= 0 {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, errClusterShrinkDeferred.Error(), map[string]any{
				"finished_at": time.Now(),
			})
			return errClusterShrinkDeferred
		}
		return s.provisionClusterMembers(ctx, jobID, clusterID, tenantID, delta)
	}
}

// provisionClusterMembers iterates the cluster's role_plan, calls the
// provisioning adapter's Apply once per (role, position) slot that isn't yet
// filled, and records a cluster_members row on each success. If `scaleDelta`
// is > 0, only that many new slots are filled (expand). If `scaleDelta` is 0,
// every missing slot is filled (fresh provision).
func (s *Server) provisionClusterMembers(ctx context.Context, jobID, clusterID, tenantID uuid.UUID, scaleDelta int) error {
	_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "provisioning cluster members", map[string]any{
		"started_at": time.Now(),
	})

	cluster, err := s.store.GetClusterByID(ctx, clusterID)
	if err != nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("load cluster: %w", err))
	}
	if cluster == nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("cluster %s not found", clusterID))
	}

	existingMembers, err := s.store.ListClusterMembers(ctx, clusterID)
	if err != nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("list cluster members: %w", err))
	}
	occupied := map[string]struct{}{}
	roleFill := map[string]int{}
	for _, m := range existingMembers {
		occupied[roleKey(m.Role, m.Position)] = struct{}{}
		roleFill[m.Role]++
	}

	plan := rolePlanFromMap(cluster.RolePlan)
	if len(plan.Roles) == 0 {
		return s.failClusterJob(ctx, jobID, errors.New("cluster has empty role_plan"))
	}

	// Build the ordered list of slots to fill.
	type slot struct {
		role     string
		position int
	}
	var slots []slot
	for _, role := range plan.Roles {
		for pos := 0; pos < role.Count; pos++ {
			if _, taken := occupied[roleKey(role.Name, pos)]; taken {
				continue
			}
			slots = append(slots, slot{role: role.Name, position: pos})
		}
	}

	if scaleDelta > 0 {
		// For expand: prefer slots in roles that are currently most-underfilled.
		// We score each role by (filled / target) and sort ascending.
		sort.SliceStable(slots, func(i, j int) bool {
			return underfillScore(slots[i].role, plan, roleFill) < underfillScore(slots[j].role, plan, roleFill)
		})
		if len(slots) > scaleDelta {
			slots = slots[:scaleDelta]
		}
	}

	// Apply failure-domain spreading by interleaving across AZ/DC metadata when
	// that strategy is set. For Sprint 1 we spread by round-robinning the
	// position number against the number of available failure domains (the
	// adapter is responsible for turning the hint into an actual placement).
	useSpread := strings.EqualFold(strings.TrimSpace(cluster.FailureDomainStrategy), "spread")
	failureDomains := pickFailureDomains(cluster.Labels)

	// Detect provider metadata enrichment hints from the host running the CP.
	detected, detectedMeta := provisioning.DetectProvider()

	adapter := provisioning.NewAdapter(cluster.Provider, s.logger.Named("cluster-provision"), nil)
	opts := provisioning.Options{
		Provider: cluster.Provider,
	}
	if cluster.TemplateID.Valid {
		opts.Template = cluster.TemplateID.UUID.String()
	}

	var successes, failures int
	for _, sl := range slots {
		// Fabricate a node id for bookkeeping. Upstream adapters that actually
		// create cloud resources will replace this with the real provider id in
		// Sprint 2 when the adapter interface learns to return node metadata.
		nodeID := uuid.New()

		metadata := map[string]string{
			"cluster_id": cluster.ID.String(),
			"tenant_id":  cluster.TenantID.String(),
			"role":       sl.role,
			"position":   fmt.Sprintf("%d", sl.position),
		}
		for k, v := range clusterLabelsAsStringMap(cluster.Labels) {
			// Don't clobber cluster-core metadata.
			if _, reserved := metadata[k]; reserved {
				continue
			}
			metadata[k] = v
		}
		if detected != "" && detected != "unknown" {
			metadata["detected_provider"] = detected
			for k, v := range detectedMeta {
				if _, exists := metadata[k]; !exists {
					metadata[k] = v
				}
			}
		}
		if useSpread && len(failureDomains) > 0 {
			metadata["failure_domain"] = failureDomains[sl.position%len(failureDomains)]
		}

		if _, applyErr := adapter.Apply(ctx, nodeID.String(), opts, metadata); applyErr != nil {
			s.logger.Warn("cluster provisioning adapter apply failed",
				zap.String("cluster_id", cluster.ID.String()),
				zap.String("role", sl.role),
				zap.Int("position", sl.position),
				zap.Error(applyErr),
			)
			failures++
			continue
		}

		if _, addErr := s.store.AddClusterMember(ctx, cluster.ID, nodeID, sl.role, sl.position); addErr != nil {
			s.logger.Warn("add cluster member failed",
				zap.String("cluster_id", cluster.ID.String()),
				zap.String("role", sl.role),
				zap.Int("position", sl.position),
				zap.Error(addErr),
			)
			failures++
			continue
		}
		successes++
		roleFill[sl.role]++
	}

	finalState := "running"
	finalStatus := storage.JobStatusSucceeded
	message := fmt.Sprintf("cluster provisioning completed: %d succeeded, %d failed", successes, failures)
	if failures > 0 && successes == 0 {
		finalState = "failed"
		finalStatus = storage.JobStatusFailed
	} else if failures > 0 {
		finalState = "degraded"
	}

	if _, updErr := s.store.UpdateCluster(ctx, cluster.ID, storage.UpdateClusterParams{
		State: &finalState,
	}); updErr != nil {
		s.logger.Warn("update cluster state after provisioning", zap.Error(updErr))
	}

	_ = s.store.UpdateJobStatus(ctx, jobID, finalStatus, message, map[string]any{
		"finished_at": time.Now(),
	})
	_ = tenantID // reserved for future tenant-scoped audit enrichment.
	return nil
}

// failClusterJob flips the job to failed and returns the original error.
func (s *Server) failClusterJob(ctx context.Context, jobID uuid.UUID, err error) error {
	_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, err.Error(), map[string]any{
		"finished_at": time.Now(),
	})
	return err
}

// roleKey returns the composite key used to detect an already-filled slot.
func roleKey(role string, position int) string {
	return fmt.Sprintf("%s::%d", role, position)
}

// underfillScore returns a float that is smaller for roles that are further
// from their target size — smaller score == higher priority to fill first.
func underfillScore(roleName string, plan clusterRolePlan, fill map[string]int) float64 {
	for _, role := range plan.Roles {
		if role.Name == roleName {
			if role.Count <= 0 {
				return 1
			}
			return float64(fill[roleName]) / float64(role.Count)
		}
	}
	return 1
}

// pickFailureDomains pulls a failure-domain list out of cluster labels. We
// look at three well-known keys in order and fall back to an empty slice.
func pickFailureDomains(labels map[string]any) []string {
	if labels == nil {
		return nil
	}
	for _, key := range []string{"availability_zones", "datacenters", "failure_domains"} {
		raw, ok := labels[key]
		if !ok {
			continue
		}
		if items, ok := raw.([]any); ok {
			out := make([]string, 0, len(items))
			for _, item := range items {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			// Comma-separated fallback.
			parts := strings.Split(s, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			return out
		}
	}
	return nil
}

// clusterLabelsAsStringMap flattens the cluster labels JSONB into a string map
// for the provisioning adapter metadata channel. Non-string values are skipped
// (they'd be rejected by the downstream wire format anyway).
func clusterLabelsAsStringMap(labels map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range labels {
		switch typed := v.(type) {
		case string:
			out[k] = typed
		case fmt.Stringer:
			out[k] = typed.String()
		}
	}
	return out
}
