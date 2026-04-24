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

// buildClusterScaleJob returns a worker.Task function. Positive `delta`
// dispatches the expand path (fills under-filled role slots). Negative `delta`
// dispatches the shrink path (drains members in reverse-position order:
// DeregisterLB → Destroy → RemoveClusterMember). Worktree E unblocks the
// Sprint 1 501 stub — handlers no longer reject shrink at the HTTP layer.
func (s *Server) buildClusterScaleJob(jobID, clusterID, tenantID uuid.UUID, delta int) func(context.Context) error {
	return func(ctx context.Context) error {
		if delta == 0 {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "cluster.scale no-op (delta=0)", map[string]any{
				"finished_at": time.Now(),
			})
			return nil
		}
		if delta < 0 {
			return s.shrinkClusterMembers(ctx, jobID, clusterID, tenantID, -delta)
		}
		return s.provisionClusterMembers(ctx, jobID, clusterID, tenantID, delta)
	}
}

// buildClusterTeardownJob returns a worker.Task function that drains every
// cluster member (reverse-position) through DeregisterLB → Destroy →
// RemoveClusterMember and finally calls DeleteCluster. Any per-node failure
// is logged but does not abort the rest of the drain — a half-destroyed
// cluster is worse than a fully-destroyed one that complains about 1 stuck
// node.
func (s *Server) buildClusterTeardownJob(jobID, clusterID, tenantID uuid.UUID) func(context.Context) error {
	return func(ctx context.Context) error {
		return s.teardownCluster(ctx, jobID, clusterID, tenantID)
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

	// When a hypervisor host is attached, load it (and its credential, if any)
	// once up-front so every slot can share the same metadata prefix. Credentials
	// are decrypted in-memory and flattened into the adapter metadata map under
	// the `_cred_*` namespace so adapters can pick what they need without having
	// to know the full credential shape.
	hostMetadata := map[string]string{}
	if cluster.HypervisorHostID.Valid {
		host, hostErr := s.store.GetHypervisorHost(ctx, cluster.HypervisorHostID.UUID)
		if hostErr != nil {
			s.logger.Warn("load hypervisor host for provisioning", zap.Error(hostErr))
		} else if host != nil {
			hostMetadata["_endpoint_url"] = host.EndpointURL
			hostMetadata["_hypervisor_host_id"] = host.ID.String()
			if host.Datacenter.Valid {
				hostMetadata["_hypervisor_host_dc"] = host.Datacenter.String
				if _, exists := hostMetadata["datacenter"]; !exists {
					hostMetadata["datacenter"] = host.Datacenter.String
				}
			}
			if host.CredentialID.Valid {
				cred, credErr := s.store.GetProviderCredential(ctx, host.CredentialID.UUID)
				if credErr != nil {
					s.logger.Warn("load provider credential", zap.Error(credErr))
				} else if cred != nil {
					if rawCfg, openErr := s.openProviderCredential(cred); openErr != nil {
						s.logger.Warn("decrypt provider credential", zap.Error(openErr))
					} else {
						for k, v := range rawCfg {
							if str, ok := v.(string); ok {
								hostMetadata["_cred_"+k] = str
							}
						}
					}
				}
			}
		}
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
		for k, v := range hostMetadata {
			metadata[k] = v
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

		// Post-member-add hooks: register with LB (if one is configured on
		// the cluster), record the registration row, then propagate cluster
		// labels to the node. Failures here are logged but do NOT fail the
		// slot — a node without LB registration is still a valid member and
		// operators can reconcile later.
		clusterMeta := buildClusterMeta(cluster)
		lbIdentifier := extractLBIdentifier(clusterMeta)
		if lbIdentifier != "" {
			if lbErr := adapter.RegisterLB(ctx, nodeID.String(), clusterMeta); lbErr != nil {
				s.logger.Warn("cluster lb register failed",
					zap.String("cluster_id", cluster.ID.String()),
					zap.String("node_id", nodeID.String()),
					zap.Error(lbErr),
				)
			} else if _, regErr := s.store.CreateClusterLBRegistration(ctx, storage.CreateClusterLBRegistrationParams{
				ClusterID:    cluster.ID,
				NodeID:       nodeID,
				Provider:     cluster.Provider,
				LBIdentifier: lbIdentifier,
			}); regErr != nil {
				s.logger.Warn("persist cluster lb registration failed",
					zap.String("cluster_id", cluster.ID.String()),
					zap.String("node_id", nodeID.String()),
					zap.Error(regErr),
				)
			}
		}
		if propErr := s.store.PropagateClusterLabelsToNode(ctx, cluster.ID, nodeID); propErr != nil {
			s.logger.Warn("propagate cluster labels to node failed",
				zap.String("cluster_id", cluster.ID.String()),
				zap.String("node_id", nodeID.String()),
				zap.Error(propErr),
			)
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

// shrinkClusterMembers drains `drainCount` members from the cluster in
// reverse-position order: DeregisterLB → Destroy → RemoveClusterMember. The
// LB registration row is flipped to deregistered_at in the same pass. Per-
// member failures are logged but don't abort the drain.
func (s *Server) shrinkClusterMembers(ctx context.Context, jobID, clusterID, tenantID uuid.UUID, drainCount int) error {
	_ = tenantID // reserved for tenant-scoped audit enrichment
	_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, fmt.Sprintf("draining %d members", drainCount), map[string]any{
		"started_at": time.Now(),
	})

	cluster, err := s.store.GetClusterByID(ctx, clusterID)
	if err != nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("load cluster for shrink: %w", err))
	}
	if cluster == nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("cluster %s not found", clusterID))
	}

	members, err := s.store.ListClusterMembers(ctx, clusterID)
	if err != nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("list cluster members: %w", err))
	}

	drainOrder := drainOrderReverse(members)
	if drainCount > len(drainOrder) {
		drainCount = len(drainOrder)
	}
	drainOrder = drainOrder[:drainCount]

	adapter := provisioning.NewAdapter(cluster.Provider, s.logger.Named("cluster-shrink"), nil)
	clusterMeta := buildClusterMeta(cluster)
	lbIdentifier := extractLBIdentifier(clusterMeta)

	var drained, failures int
	for _, m := range drainOrder {
		s.drainSingleMember(ctx, adapter, cluster, m, clusterMeta, lbIdentifier, &drained, &failures)
	}

	message := fmt.Sprintf("cluster.scale shrink completed: %d drained, %d failed", drained, failures)
	status := storage.JobStatusSucceeded
	if failures > 0 && drained == 0 {
		status = storage.JobStatusFailed
	}
	_ = s.store.UpdateJobStatus(ctx, jobID, status, message, map[string]any{
		"finished_at": time.Now(),
	})
	return nil
}

// teardownCluster is the cluster.teardown handler. It walks every member in
// reverse-position order, drains them, then deletes the cluster row. Cluster
// state is flipped to "terminating" at start and "deleted" on completion.
func (s *Server) teardownCluster(ctx context.Context, jobID, clusterID, tenantID uuid.UUID) error {
	_ = tenantID // reserved for tenant-scoped audit enrichment
	_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "cluster teardown starting", map[string]any{
		"started_at": time.Now(),
	})

	cluster, err := s.store.GetClusterByID(ctx, clusterID)
	if err != nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("load cluster for teardown: %w", err))
	}
	if cluster == nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("cluster %s not found", clusterID))
	}

	terminating := "terminating"
	if _, updErr := s.store.UpdateCluster(ctx, cluster.ID, storage.UpdateClusterParams{State: &terminating}); updErr != nil {
		s.logger.Warn("flip cluster state to terminating", zap.Error(updErr))
	}

	members, err := s.store.ListClusterMembers(ctx, clusterID)
	if err != nil {
		return s.failClusterJob(ctx, jobID, fmt.Errorf("list cluster members: %w", err))
	}

	adapter := provisioning.NewAdapter(cluster.Provider, s.logger.Named("cluster-teardown"), nil)
	clusterMeta := buildClusterMeta(cluster)
	lbIdentifier := extractLBIdentifier(clusterMeta)

	drainOrder := drainOrderReverse(members)
	var drained, failures int
	for _, m := range drainOrder {
		s.drainSingleMember(ctx, adapter, cluster, m, clusterMeta, lbIdentifier, &drained, &failures)
	}

	if deleteErr := s.store.DeleteCluster(ctx, cluster.ID); deleteErr != nil {
		s.logger.Error("delete cluster after teardown", zap.Error(deleteErr), zap.String("cluster_id", cluster.ID.String()))
		_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, fmt.Sprintf("teardown drained %d/%d but cluster delete failed: %v", drained, len(drainOrder), deleteErr), map[string]any{
			"finished_at": time.Now(),
		})
		return deleteErr
	}

	status := storage.JobStatusSucceeded
	if failures > 0 && drained == 0 {
		status = storage.JobStatusFailed
	}
	_ = s.store.UpdateJobStatus(ctx, jobID, status, fmt.Sprintf("cluster teardown completed: %d drained, %d failed", drained, failures), map[string]any{
		"finished_at": time.Now(),
	})
	return nil
}

// drainSingleMember is the shared drain body for shrink + teardown. We
// deliberately keep the order DeregisterLB → Destroy → RemoveClusterMember:
//   - LB out first so no new traffic lands
//   - then the cloud resource
//   - then the membership row (which strips cluster.* labels from the node)
//
// Failures at any step are logged and counted but don't short-circuit — a
// half-drained cluster should still progress toward fully-drained.
func (s *Server) drainSingleMember(
	ctx context.Context,
	adapter provisioning.Adapter,
	cluster *storage.Cluster,
	m storage.ClusterMember,
	clusterMeta map[string]any,
	lbIdentifier string,
	drained *int,
	failures *int,
) {
	if lbIdentifier != "" {
		if derr := adapter.DeregisterLB(ctx, m.NodeID.String(), clusterMeta); derr != nil {
			s.logger.Warn("deregister lb failed during drain",
				zap.String("cluster_id", cluster.ID.String()),
				zap.String("node_id", m.NodeID.String()),
				zap.Error(derr),
			)
		}
		if mErr := s.store.MarkClusterLBRegistrationDeregistered(ctx, cluster.ID, m.NodeID, lbIdentifier); mErr != nil {
			s.logger.Debug("no lb registration row to mark",
				zap.String("cluster_id", cluster.ID.String()),
				zap.String("node_id", m.NodeID.String()),
				zap.Error(mErr),
			)
		}
	}

	if destErr := adapter.Destroy(ctx, m.NodeID.String()); destErr != nil {
		s.logger.Warn("destroy member failed during drain",
			zap.String("cluster_id", cluster.ID.String()),
			zap.String("node_id", m.NodeID.String()),
			zap.Error(destErr),
		)
		*failures++
		// still try to remove the membership row below so the cluster
		// member count stays accurate
	}

	if remErr := s.store.RemoveClusterMember(ctx, cluster.ID, m.NodeID); remErr != nil {
		s.logger.Warn("remove cluster member failed during drain",
			zap.String("cluster_id", cluster.ID.String()),
			zap.String("node_id", m.NodeID.String()),
			zap.Error(remErr),
		)
		*failures++
		return
	}

	*drained++
}

// drainOrderReverse returns members sorted so highest-position slots drain
// first. We prefer reverse-position because operators typically provision in
// position order (cp-0, cp-1, cp-2…) and expect teardown/shrink to unwind in
// the reverse order — last-in-first-out semantics.
func drainOrderReverse(members []storage.ClusterMember) []storage.ClusterMember {
	out := make([]storage.ClusterMember, len(members))
	copy(out, members)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			// Worker roles drain before control-plane roles by default — the
			// lexicographic order happens to give us worker < control-plane
			// (both 'w' > 'c' means control-plane drains first alphabetically)
			// so we explicitly pull `worker` first when both present.
			if out[i].Role == "worker" {
				return true
			}
			if out[j].Role == "worker" {
				return false
			}
			return out[i].Role > out[j].Role
		}
		return out[i].Position > out[j].Position
	})
	return out
}

// buildClusterMeta projects the cluster's labels into an untyped map suitable
// for the adapter metadata channel. Cluster-LB-specific keys (lb_target_group_arn,
// lb_backend_pool_id, lb_pool) are surfaced at the top level so adapters can
// find them with a single lookup.
func buildClusterMeta(cluster *storage.Cluster) map[string]any {
	if cluster == nil {
		return map[string]any{}
	}
	meta := make(map[string]any, len(cluster.Labels)+4)
	meta["cluster_id"] = cluster.ID.String()
	meta["tenant_id"] = cluster.TenantID.String()
	meta["provider"] = cluster.Provider
	for k, v := range cluster.Labels {
		meta[k] = v
	}
	return meta
}

// extractLBIdentifier pulls the first recognised LB identifier from cluster
// metadata. Returns "" if no LB is configured — callers short-circuit LB
// register/deregister when this is empty.
func extractLBIdentifier(meta map[string]any) string {
	for _, key := range []string{"lb_target_group_arn", "lb_backend_pool_id", "lb_pool"} {
		if raw, ok := meta[key]; ok {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
