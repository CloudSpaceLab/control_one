package server

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// Default grace window (in seconds) for "healthy" heartbeat. Matches the
// rollout gate default in cluster_rollouts_jobs.go (defaultGateGrace = 5m) so
// the cluster-level health view and the rollout gate behave consistently.
const clusterHeartbeatGraceSeconds = 300

// Aggregate health states a cluster can be in. Derivation rule:
//   - healthy:   every member up AND desired_size met
//   - degraded:  at least quorum members up, but not all
//   - unhealthy: below quorum
//   - empty:     zero members (e.g. cluster freshly created, not yet provisioned)
const (
	ClusterHealthHealthy   = "healthy"
	ClusterHealthDegraded  = "degraded"
	ClusterHealthUnhealthy = "unhealthy"
	ClusterHealthEmpty     = "empty"
)

// clusterMemberHealthResponse is the per-member view returned by the /health
// endpoint. It projects just what the UI needs to render the topology +
// member table: node identity, role/position in the cluster, raw node state,
// the last heartbeat age (in seconds so the UI can colour it), whether the
// member is treated as healthy at aggregation time, and whether its latest
// compliance snapshot passes.
type clusterMemberHealthResponse struct {
	NodeID            string  `json:"node_id"`
	Hostname          string  `json:"hostname"`
	Role              string  `json:"role"`
	Position          int     `json:"position"`
	State             string  `json:"state"`
	LastSeenAt        *string `json:"last_seen_at,omitempty"`
	HeartbeatAgeSecs  *int64  `json:"heartbeat_age_seconds,omitempty"`
	Healthy           bool    `json:"healthy"`
	ComplianceHealthy *bool   `json:"compliance_healthy,omitempty"`
	Reason            string  `json:"reason,omitempty"`
}

type clusterHealthResponse struct {
	ClusterID    string                        `json:"cluster_id"`
	State        string                        `json:"state"`
	HealthyCount int                           `json:"healthy_count"`
	TotalCount   int                           `json:"total_count"`
	DesiredSize  int                           `json:"desired_size"`
	Quorum       int                           `json:"quorum"`
	QuorumMet    bool                          `json:"quorum_met"`
	ComputedAt   string                        `json:"computed_at"`
	Members      []clusterMemberHealthResponse `json:"members"`
}

// handleClusterHealth serves GET /api/v1/clusters/{id}/health. Aggregates
// per-member heartbeat + compliance state and derives a cluster-level verdict.
func (s *Server) handleClusterHealth(w http.ResponseWriter, r *http.Request, clusterID uuid.UUID) {
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster for health", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}
	resp := s.computeClusterHealth(r.Context(), cluster)
	writeJSON(w, http.StatusOK, resp)
}

// computeClusterHealth is the heart of gap 3.8 — pulls members, loads each
// member's node row, scores it, and derives the cluster-level aggregate.
// Exported internally so list-clusters and the detail handler can share the
// same logic.
func (s *Server) computeClusterHealth(ctx context.Context, cluster *storage.Cluster) clusterHealthResponse {
	now := time.Now().UTC()
	resp := clusterHealthResponse{
		ClusterID:   cluster.ID.String(),
		DesiredSize: cluster.DesiredSize,
		ComputedAt:  now.Format(time.RFC3339),
		Members:     []clusterMemberHealthResponse{},
	}

	members, err := s.store.ListClusterMembers(ctx, cluster.ID)
	if err != nil {
		// Don't fail the endpoint; surface an empty degraded state. The list
		// call is best-effort — every other error path in clusters.go also
		// logs and continues.
		s.logger.Warn("list cluster members for health", zap.Error(err))
	}
	resp.TotalCount = len(members)
	resp.Quorum = quorumFor(resp.TotalCount)

	if resp.TotalCount == 0 {
		resp.State = ClusterHealthEmpty
		resp.QuorumMet = false
		return resp
	}

	for _, m := range members {
		node, nErr := s.store.GetNode(ctx, m.NodeID)
		memberResp := clusterMemberHealthResponse{
			NodeID:   m.NodeID.String(),
			Role:     m.Role,
			Position: m.Position,
		}

		if nErr != nil {
			memberResp.State = "unknown"
			memberResp.Reason = "node lookup failed"
			s.logger.Warn("cluster health: load node", zap.String("node_id", m.NodeID.String()), zap.Error(nErr))
			resp.Members = append(resp.Members, memberResp)
			continue
		}
		if node == nil {
			memberResp.State = "missing"
			memberResp.Reason = "node not found"
			resp.Members = append(resp.Members, memberResp)
			continue
		}

		memberResp.Hostname = node.Hostname
		memberResp.State = node.State
		lastSeen := nodeLastSeenAt(node)
		if lastSeen != nil && !lastSeen.IsZero() {
			formatted := lastSeen.UTC().Format(time.RFC3339)
			memberResp.LastSeenAt = &formatted
			age := int64(now.Sub(lastSeen.UTC()) / time.Second)
			if age < 0 {
				age = 0
			}
			memberResp.HeartbeatAgeSecs = &age
		}

		// Compliance signal: look up the most recent result across all rules.
		// If the newest is failing, mark compliance_healthy=false — this is a
		// hint, not the primary health determinant (heartbeat + state are).
		if complianceHealthy, ok := s.latestComplianceHealthy(ctx, cluster.TenantID, m.NodeID); ok {
			memberResp.ComplianceHealthy = &complianceHealthy
		}

		healthy, reason := memberHealthy(node, lastSeen, now, memberResp.ComplianceHealthy)
		memberResp.Healthy = healthy
		if reason != "" {
			memberResp.Reason = reason
		}
		if healthy {
			resp.HealthyCount++
		}
		resp.Members = append(resp.Members, memberResp)
	}

	resp.QuorumMet = resp.HealthyCount >= resp.Quorum
	resp.State = deriveClusterState(resp.HealthyCount, resp.TotalCount, resp.Quorum)
	return resp
}

// memberHealthy scores a single node. A member is "healthy" when:
//   - node state is active (enrollment-pending / enrollment-failed / retired
//     are all unhealthy)
//   - heartbeat is present and within the grace window
//   - latest compliance result (if known) is passing
//
// The returned reason is empty when the member is healthy; otherwise it is a
// short human-readable cause suitable for the UI tooltip.
func memberHealthy(node *storage.Node, lastSeen *time.Time, now time.Time, complianceHealthy *bool) (bool, string) {
	if node == nil {
		return false, "node missing"
	}
	if node.State != storage.NodeStateActive {
		return false, "state: " + node.State
	}
	if lastSeen == nil || lastSeen.IsZero() {
		return false, "never heartbeated"
	}
	grace := time.Duration(clusterHeartbeatGraceSeconds) * time.Second
	if now.Sub(lastSeen.UTC()) > grace {
		return false, "heartbeat stale"
	}
	if complianceHealthy != nil && !*complianceHealthy {
		return false, "compliance failing"
	}
	return true, ""
}

// deriveClusterState applies the aggregation rule defined on the entity page:
//
//	healthy   = all members healthy AND none missing vs desired_size
//	degraded  = at least quorum members healthy, but not all
//	unhealthy = below quorum
func deriveClusterState(healthy, total, quorum int) string {
	if total == 0 {
		return ClusterHealthEmpty
	}
	if healthy >= total {
		return ClusterHealthHealthy
	}
	if healthy >= quorum {
		return ClusterHealthDegraded
	}
	return ClusterHealthUnhealthy
}

// quorumFor returns the majority quorum for an N-member cluster.
//
//	N=0 → 0 (special case: no members)
//	N=1 → 1 (trivial quorum)
//	N=2 → 2 (majority requires both — no tolerance)
//	N=3 → 2
//	N=4 → 3
//	N=5 → 3
//	N=6 → 4
//	N=7 → 4
//
// Formula: ceil((N+1)/2) = floor(N/2) + 1
func quorumFor(total int) int {
	if total <= 0 {
		return 0
	}
	return int(math.Floor(float64(total)/2.0)) + 1
}

// latestComplianceHealthy returns (pass, true) when we have a recent result to
// score against, (_, false) when no compliance rows exist for the node — in
// which case the caller leaves compliance_healthy out of the response entirely
// so the UI can distinguish "failing" from "unscanned".
func (s *Server) latestComplianceHealthy(ctx context.Context, tenantID, nodeID uuid.UUID) (bool, bool) {
	results, _, err := s.store.ListComplianceResultsFiltered(ctx, storage.ComplianceResultFilter{
		TenantID: tenantID,
		NodeID:   nodeID,
	}, 1, 0)
	if err != nil {
		s.logger.Warn("cluster health: latest compliance lookup",
			zap.String("node_id", nodeID.String()),
			zap.Error(err),
		)
		return false, false
	}
	if len(results) == 0 {
		return false, false
	}
	return results[0].Passed, true
}

// clusterHealthSummary is the trimmed shape attached to list responses — the
// UI only needs the aggregate verdict + counts for the list view.
type clusterHealthSummary struct {
	State        string `json:"state"`
	HealthyCount int    `json:"healthy_count"`
	TotalCount   int    `json:"total_count"`
	Quorum       int    `json:"quorum"`
	QuorumMet    bool   `json:"quorum_met"`
}

func newClusterHealthSummary(full clusterHealthResponse) clusterHealthSummary {
	return clusterHealthSummary{
		State:        full.State,
		HealthyCount: full.HealthyCount,
		TotalCount:   full.TotalCount,
		Quorum:       full.Quorum,
		QuorumMet:    full.QuorumMet,
	}
}

