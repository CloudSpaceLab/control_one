package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

// Cluster job types for the async control-plane queue.
const (
	JobTypeClusterProvision = "cluster.provision"
	JobTypeClusterScale     = "cluster.scale"
	JobTypeClusterTeardown  = "cluster.teardown"
)

// Sprint 1 left shrink + teardown as 501 stubs. Sprint 2 Worktree E unblocks
// both: handlers now return 202 + job_id and the worker drains
// (DeregisterLB → Destroy → RemoveClusterMember → DeleteCluster).

// ─── Request / Response types ───────────────────────────────────────

type clusterRolePlanRole struct {
	Name              string `json:"name"`
	Count             int    `json:"count"`
	TemplateVersionID string `json:"template_version_id,omitempty"`
}

type clusterRolePlan struct {
	Roles []clusterRolePlanRole `json:"roles"`
}

type createClusterRequest struct {
	TenantID              string          `json:"tenant_id"`
	Name                  string          `json:"name"`
	Provider              string          `json:"provider"`
	TemplateID            *string         `json:"template_id,omitempty"`
	DesiredSize           *int            `json:"desired_size,omitempty"`
	RolePlan              clusterRolePlan `json:"role_plan"`
	Labels                map[string]any  `json:"labels,omitempty"`
	FailureDomainStrategy string          `json:"failure_domain_strategy,omitempty"`
}

type updateClusterRequest struct {
	DesiredSize           *int             `json:"desired_size,omitempty"`
	RolePlan              *clusterRolePlan `json:"role_plan,omitempty"`
	Labels                *map[string]any  `json:"labels,omitempty"`
	FailureDomainStrategy *string          `json:"failure_domain_strategy,omitempty"`
}

type clusterMemberResponse struct {
	NodeID   string `json:"node_id"`
	Role     string `json:"role"`
	Position int    `json:"position"`
	JoinedAt string `json:"joined_at"`
}

type clusterRolloutResponse struct {
	ID                string         `json:"id"`
	TemplateVersionID string         `json:"template_version_id"`
	WaveSize          int            `json:"wave_size"`
	WaveStrategy      string         `json:"wave_strategy"`
	HealthGate        map[string]any `json:"health_gate"`
	State             string         `json:"state"`
	CurrentWave       int            `json:"current_wave"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type clusterResponse struct {
	ID                    string                  `json:"id"`
	TenantID              string                  `json:"tenant_id"`
	Name                  string                  `json:"name"`
	Provider              string                  `json:"provider"`
	DesiredSize           int                     `json:"desired_size"`
	RolePlan              map[string]any          `json:"role_plan"`
	Labels                map[string]any          `json:"labels"`
	FailureDomainStrategy string                  `json:"failure_domain_strategy"`
	State                 string                  `json:"state"`
	TemplateID            *string                 `json:"template_id,omitempty"`
	CreatedAt             string                  `json:"created_at"`
	UpdatedAt             string                  `json:"updated_at"`
	Members               []clusterMemberResponse `json:"members,omitempty"`
	LatestRollout         *clusterRolloutResponse `json:"latest_rollout,omitempty"`
	Health                *clusterHealthSummary   `json:"health,omitempty"`
}

type clusterAcceptedResponse struct {
	ClusterID string `json:"cluster_id"`
	JobID     string `json:"job_id"`
	State     string `json:"state"`
}

// ─── Payload types for async jobs ───────────────────────────────────

type clusterProvisionPayload struct {
	ClusterID string `json:"cluster_id"`
	TenantID  string `json:"tenant_id"`
}

type clusterScalePayload struct {
	ClusterID   string `json:"cluster_id"`
	TenantID    string `json:"tenant_id"`
	DesiredSize int    `json:"desired_size"`
	Delta       int    `json:"delta"`
	Direction   string `json:"direction"`
}

// ─── Handlers ───────────────────────────────────────────────────────

func (s *Server) handleClusters(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListClusters(w, r)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleCreateCluster(w, r, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleClusterSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(trimmed, "/")
	clusterID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid cluster id", http.StatusBadRequest)
		return
	}

	// /rollouts subtree delegates to the cluster-rollouts API handler.
	if len(segments) >= 2 && segments[1] == "rollouts" {
		s.handleClusterRolloutsRoute(w, r, clusterID, segments[2:])
		return
	}

	// /health subtree — read-only aggregate view (gap 3.8).
	if len(segments) == 2 && segments[1] == "health" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleClusterHealth(w, r, clusterID)
		return
	}

	// /secrets subtree delegates to the cluster-secrets API handler.
	if len(segments) >= 2 && segments[1] == "secrets" {
		s.handleClusterSecretsRoute(w, r, clusterID, segments[2:])
		return
	}

	if len(segments) != 1 {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetCluster(w, r, clusterID)
	case http.MethodPatch:
		patchPrincipal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handlePatchCluster(w, r, clusterID, patchPrincipal)
	case http.MethodDelete:
		deletePrincipal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.handleDeleteCluster(w, r, clusterID, deletePrincipal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch, http.MethodDelete}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListClusters(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	clusters, total, err := s.store.ListClusters(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Error("list clusters", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]clusterResponse, 0, len(clusters))
	for i := range clusters {
		item := newClusterResponse(clusters[i], nil, nil)
		// Attach an aggregate health summary so the list view can render a
		// badge per row without N+1 calls. The full per-member breakdown is
		// only loaded on the detail / /health endpoints.
		summary := newClusterHealthSummary(s.computeClusterHealth(r.Context(), &clusters[i]))
		item.Health = &summary
		respItems = append(respItems, item)
	}

	resp := paginatedResponse[clusterResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetCluster(w http.ResponseWriter, r *http.Request, clusterID uuid.UUID) {
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}

	members, err := s.store.ListClusterMembers(r.Context(), clusterID)
	if err != nil {
		s.logger.Warn("list cluster members", zap.Error(err))
	}

	var latest *storage.ClusterRollout
	rollouts, _, err := s.store.ListClusterRollouts(r.Context(), clusterID, 1, 0)
	if err != nil {
		s.logger.Warn("list cluster rollouts", zap.Error(err))
	} else if len(rollouts) > 0 {
		rollout := rollouts[0]
		latest = &rollout
	}

	resp := newClusterResponse(*cluster, members, latest)
	// Attach an aggregate health summary so the detail panel can render a badge
	// without a second API round-trip. Callers who need per-member heartbeat age
	// hit /api/v1/clusters/:id/health separately.
	summary := newClusterHealthSummary(s.computeClusterHealth(r.Context(), cluster))
	resp.Health = &summary
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateCluster(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	var req createClusterRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := validateCreateClusterRequest(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	// If a tenant exists in storage, validate it. Skip if store doesn't expose GetTenant.
	if tenant, tErr := s.store.GetTenant(r.Context(), tenantID); tErr != nil {
		s.logger.Error("lookup tenant for cluster", zap.Error(tErr))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	} else if tenant == nil {
		http.Error(w, "tenant not found", http.StatusBadRequest)
		return
	}

	// Compute desired size from role plan when omitted.
	rolePlanSum := 0
	for _, role := range req.RolePlan.Roles {
		rolePlanSum += role.Count
	}
	desiredSize := rolePlanSum
	if req.DesiredSize != nil {
		desiredSize = *req.DesiredSize
	}
	if desiredSize < rolePlanSum {
		http.Error(w, "desired_size must be >= sum(role_plan.roles.count)", http.StatusBadRequest)
		return
	}

	rolePlanMap := rolePlanToMap(req.RolePlan)
	labels := req.Labels
	if labels == nil {
		labels = map[string]any{}
	}

	strategy := strings.TrimSpace(req.FailureDomainStrategy)
	if strategy == "" {
		strategy = "spread"
	}

	params := storage.CreateClusterParams{
		TenantID:              tenantID,
		Name:                  strings.TrimSpace(req.Name),
		Provider:              strings.TrimSpace(req.Provider),
		DesiredSize:           desiredSize,
		RolePlan:              rolePlanMap,
		Labels:                labels,
		FailureDomainStrategy: strategy,
		State:                 "pending",
	}
	if req.TemplateID != nil {
		if tid, tErr := uuid.Parse(strings.TrimSpace(*req.TemplateID)); tErr == nil && tid != uuid.Nil {
			params.TemplateID = &tid
		} else if tErr != nil {
			http.Error(w, "invalid template_id", http.StatusBadRequest)
			return
		}
	}

	cluster, err := s.store.CreateCluster(r.Context(), params)
	if err != nil {
		http.Error(w, fmt.Sprintf("create cluster failed: %v", err), http.StatusBadRequest)
		return
	}

	jobID, enqueueErr := s.enqueueClusterProvision(r, cluster)
	if enqueueErr != nil {
		s.logger.Error("enqueue cluster provision", zap.Error(enqueueErr))
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	s.recordAudit(r.Context(), principal, cluster.TenantID, "cluster.created", "cluster", cluster.ID.String(), map[string]any{
		"provider":     cluster.Provider,
		"desired_size": cluster.DesiredSize,
		"job_id":       jobID.String(),
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(clusterAcceptedResponse{
		ClusterID: cluster.ID.String(),
		JobID:     jobID.String(),
		State:     cluster.State,
	})
}

func (s *Server) handlePatchCluster(w http.ResponseWriter, r *http.Request, clusterID uuid.UUID, principal *auth.Principal) {
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster for patch", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}

	var req updateClusterRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	// Scale path: desired_size was supplied.
	if req.DesiredSize != nil {
		newSize := *req.DesiredSize
		if newSize < 0 {
			http.Error(w, "desired_size must be non-negative", http.StatusBadRequest)
			return
		}
		delta := newSize - cluster.DesiredSize

		// Apply role_plan + labels + strategy updates first (if included) so
		// the scale job works against the updated shape. Shrink + expand both
		// go through the same update block — only the direction differs.
		updateParams := storage.UpdateClusterParams{DesiredSize: &newSize}
		if req.RolePlan != nil {
			rp := rolePlanToMap(*req.RolePlan)
			updateParams.RolePlan = &rp
		}
		labelsChanged := false
		if req.Labels != nil {
			labels := *req.Labels
			if labels == nil {
				labels = map[string]any{}
			}
			updateParams.Labels = &labels
			labelsChanged = true
		}
		if req.FailureDomainStrategy != nil {
			strategy := strings.TrimSpace(*req.FailureDomainStrategy)
			if strategy == "" {
				http.Error(w, "failure_domain_strategy cannot be empty", http.StatusBadRequest)
				return
			}
			updateParams.FailureDomainStrategy = &strategy
		}

		updated, updErr := s.store.UpdateCluster(r.Context(), clusterID, updateParams)
		if updErr != nil {
			http.Error(w, fmt.Sprintf("update cluster: %v", updErr), http.StatusBadRequest)
			return
		}
		if updated == nil {
			http.NotFound(w, r)
			return
		}

		if labelsChanged {
			s.propagateClusterLabelsToMembers(r.Context(), updated.ID)
		}

		if delta == 0 {
			// Nothing to scale. Return the updated cluster with no job.
			writeJSON(w, http.StatusOK, newClusterResponse(*updated, nil, nil))
			return
		}

		direction := "expand"
		if delta < 0 {
			direction = "shrink"
		}

		jobID, enqueueErr := s.enqueueClusterScale(r, updated, delta)
		if enqueueErr != nil {
			s.logger.Error("enqueue cluster scale", zap.Error(enqueueErr))
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}

		s.recordAudit(r.Context(), principal, updated.TenantID, "cluster.scale", "cluster", updated.ID.String(), map[string]any{
			"desired_size": updated.DesiredSize,
			"delta":        delta,
			"direction":    direction,
			"job_id":       jobID.String(),
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(clusterAcceptedResponse{
			ClusterID: updated.ID.String(),
			JobID:     jobID.String(),
			State:     updated.State,
		})
		return
	}

	// Labels / role_plan / strategy-only update (no scale).
	updateParams := storage.UpdateClusterParams{}
	hasUpdate := false
	if req.RolePlan != nil {
		rp := rolePlanToMap(*req.RolePlan)
		updateParams.RolePlan = &rp
		hasUpdate = true
	}
	if req.Labels != nil {
		labels := *req.Labels
		if labels == nil {
			labels = map[string]any{}
		}
		updateParams.Labels = &labels
		hasUpdate = true
	}
	if req.FailureDomainStrategy != nil {
		strategy := strings.TrimSpace(*req.FailureDomainStrategy)
		if strategy == "" {
			http.Error(w, "failure_domain_strategy cannot be empty", http.StatusBadRequest)
			return
		}
		updateParams.FailureDomainStrategy = &strategy
		hasUpdate = true
	}
	if !hasUpdate {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	updated, err := s.store.UpdateCluster(r.Context(), clusterID, updateParams)
	if err != nil {
		http.Error(w, fmt.Sprintf("update cluster: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	if req.Labels != nil {
		s.propagateClusterLabelsToMembers(r.Context(), updated.ID)
	}

	s.recordAudit(r.Context(), principal, updated.TenantID, "cluster.updated", "cluster", updated.ID.String(), map[string]any{
		"labels_updated":    req.Labels != nil,
		"role_plan_updated": req.RolePlan != nil,
	})

	writeJSON(w, http.StatusOK, newClusterResponse(*updated, nil, nil))
}

// propagateClusterLabelsToMembers loops each current member of the cluster and
// refreshes the `cluster.`-prefixed projection on the node. Invoked from
// handleUpdateCluster whenever labels changed. Failures are logged but do not
// fail the PATCH — the cluster update itself is already persisted.
func (s *Server) propagateClusterLabelsToMembers(ctx context.Context, clusterID uuid.UUID) {
	members, err := s.store.ListClusterMembers(ctx, clusterID)
	if err != nil {
		s.logger.Warn("list cluster members for label propagation", zap.Error(err))
		return
	}
	for _, m := range members {
		if pErr := s.store.PropagateClusterLabelsToNode(ctx, clusterID, m.NodeID); pErr != nil {
			s.logger.Warn("propagate cluster labels to node failed",
				zap.String("cluster_id", clusterID.String()),
				zap.String("node_id", m.NodeID.String()),
				zap.Error(pErr),
			)
		}
	}
}

func (s *Server) handleDeleteCluster(w http.ResponseWriter, r *http.Request, clusterID uuid.UUID, principal *auth.Principal) {
	cluster, err := s.store.GetClusterByID(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("get cluster for delete", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.NotFound(w, r)
		return
	}

	// Sprint 2 Worktree E unblocks teardown: enqueue cluster.teardown which
	// drains members (DeregisterLB → Destroy → RemoveClusterMember) in
	// reverse-position order and finally deletes the cluster row.
	jobID, enqueueErr := s.enqueueClusterTeardown(r, cluster)
	if enqueueErr != nil {
		s.logger.Error("enqueue cluster teardown", zap.Error(enqueueErr))
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	s.recordAudit(r.Context(), principal, cluster.TenantID, "cluster.teardown", "cluster", cluster.ID.String(), map[string]any{
		"job_id": jobID.String(),
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(clusterAcceptedResponse{
		ClusterID: cluster.ID.String(),
		JobID:     jobID.String(),
		State:     "terminating",
	})
}

// ─── Helpers ────────────────────────────────────────────────────────

func validateCreateClusterRequest(req *createClusterRequest) error {
	if req == nil {
		return errors.New("payload is required")
	}
	if strings.TrimSpace(req.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(req.Provider) == "" {
		return errors.New("provider is required")
	}
	if len(req.RolePlan.Roles) == 0 {
		return errors.New("role_plan.roles must contain at least one role")
	}
	seen := map[string]struct{}{}
	for i, role := range req.RolePlan.Roles {
		name := strings.TrimSpace(role.Name)
		if name == "" {
			return fmt.Errorf("role_plan.roles[%d].name is required", i)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("role_plan.roles[%d].name %q is duplicated", i, name)
		}
		seen[name] = struct{}{}
		if role.Count < 1 {
			return fmt.Errorf("role_plan.roles[%d].count must be >= 1", i)
		}
	}
	if req.DesiredSize != nil && *req.DesiredSize < 0 {
		return errors.New("desired_size must be non-negative")
	}
	return nil
}

func rolePlanToMap(plan clusterRolePlan) map[string]any {
	roles := make([]any, 0, len(plan.Roles))
	for _, r := range plan.Roles {
		entry := map[string]any{
			"name":  strings.TrimSpace(r.Name),
			"count": r.Count,
		}
		if strings.TrimSpace(r.TemplateVersionID) != "" {
			entry["template_version_id"] = strings.TrimSpace(r.TemplateVersionID)
		}
		roles = append(roles, entry)
	}
	return map[string]any{"roles": roles}
}

func rolePlanFromMap(raw map[string]any) clusterRolePlan {
	plan := clusterRolePlan{}
	if raw == nil {
		return plan
	}
	rolesRaw, ok := raw["roles"].([]any)
	if !ok {
		return plan
	}
	for _, item := range rolesRaw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := clusterRolePlanRole{}
		if name, ok := entry["name"].(string); ok {
			role.Name = name
		}
		switch v := entry["count"].(type) {
		case float64:
			role.Count = int(v)
		case int:
			role.Count = v
		case int64:
			role.Count = int(v)
		}
		if tvid, ok := entry["template_version_id"].(string); ok {
			role.TemplateVersionID = tvid
		}
		plan.Roles = append(plan.Roles, role)
	}
	return plan
}

func newClusterResponse(cluster storage.Cluster, members []storage.ClusterMember, rollout *storage.ClusterRollout) clusterResponse {
	resp := clusterResponse{
		ID:                    cluster.ID.String(),
		TenantID:              cluster.TenantID.String(),
		Name:                  cluster.Name,
		Provider:              cluster.Provider,
		DesiredSize:           cluster.DesiredSize,
		RolePlan:              cluster.RolePlan,
		Labels:                cluster.Labels,
		FailureDomainStrategy: cluster.FailureDomainStrategy,
		State:                 cluster.State,
		CreatedAt:             formatTime(cluster.CreatedAt),
		UpdatedAt:             formatTime(cluster.UpdatedAt),
	}
	if resp.RolePlan == nil {
		resp.RolePlan = map[string]any{}
	}
	if resp.Labels == nil {
		resp.Labels = map[string]any{}
	}
	if cluster.TemplateID.Valid {
		id := cluster.TemplateID.UUID.String()
		resp.TemplateID = &id
	}
	if len(members) > 0 {
		resp.Members = make([]clusterMemberResponse, 0, len(members))
		for _, m := range members {
			resp.Members = append(resp.Members, clusterMemberResponse{
				NodeID:   m.NodeID.String(),
				Role:     m.Role,
				Position: m.Position,
				JoinedAt: formatTime(m.JoinedAt),
			})
		}
	}
	if rollout != nil {
		resp.LatestRollout = &clusterRolloutResponse{
			ID:                rollout.ID.String(),
			TemplateVersionID: rollout.TemplateVersionID.String(),
			WaveSize:          rollout.WaveSize,
			WaveStrategy:      rollout.WaveStrategy,
			HealthGate:        rollout.HealthGate,
			State:             rollout.State,
			CurrentWave:       rollout.CurrentWave,
			CreatedAt:         formatTime(rollout.CreatedAt),
			UpdatedAt:         formatTime(rollout.UpdatedAt),
		}
		if resp.LatestRollout.HealthGate == nil {
			resp.LatestRollout.HealthGate = map[string]any{}
		}
	}
	return resp
}

// enqueueClusterProvision creates a cluster.provision job + task pair.
func (s *Server) enqueueClusterProvision(r *http.Request, cluster *storage.Cluster) (uuid.UUID, error) {
	payload := clusterProvisionPayload{
		ClusterID: cluster.ID.String(),
		TenantID:  cluster.TenantID.String(),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal cluster.provision payload: %w", err)
	}

	job := &storage.Job{
		TenantID: cluster.TenantID,
		Type:     JobTypeClusterProvision,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	event := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: "cluster.provision queued",
	}

	ctx := r.Context()
	created, err := s.store.CreateJob(ctx, job, event)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create cluster.provision job: %w", err)
	}

	if s.worker == nil {
		// No worker — caller wasn't configured with an async backend. Treat as infra error.
		return uuid.Nil, errors.New("worker unavailable")
	}
	task := worker.Task{
		Name:         fmt.Sprintf("cluster-provision-%s", created.ID),
		Job:          s.buildClusterProvisionJob(created.ID, cluster.ID, cluster.TenantID),
		MaxAttempts:  1,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, created.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), nil)
		return uuid.Nil, fmt.Errorf("enqueue cluster.provision task: %w", err)
	}
	return created.ID, nil
}

// enqueueClusterScale creates a cluster.scale job + task pair (both expand and
// shrink share this path — the shrink direction is unblocked in Sprint 2 E).
func (s *Server) enqueueClusterScale(r *http.Request, cluster *storage.Cluster, delta int) (uuid.UUID, error) {
	direction := "expand"
	if delta < 0 {
		direction = "shrink"
	}
	payload := clusterScalePayload{
		ClusterID:   cluster.ID.String(),
		TenantID:    cluster.TenantID.String(),
		DesiredSize: cluster.DesiredSize,
		Delta:       delta,
		Direction:   direction,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal cluster.scale payload: %w", err)
	}

	job := &storage.Job{
		TenantID: cluster.TenantID,
		Type:     JobTypeClusterScale,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	event := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: fmt.Sprintf("cluster.scale queued (delta=%d, direction=%s)", delta, direction),
	}

	ctx := r.Context()
	created, err := s.store.CreateJob(ctx, job, event)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create cluster.scale job: %w", err)
	}

	if s.worker == nil {
		return uuid.Nil, errors.New("worker unavailable")
	}
	task := worker.Task{
		Name:         fmt.Sprintf("cluster-scale-%s", created.ID),
		Job:          s.buildClusterScaleJob(created.ID, cluster.ID, cluster.TenantID, delta),
		MaxAttempts:  1,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, created.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), nil)
		return uuid.Nil, fmt.Errorf("enqueue cluster.scale task: %w", err)
	}
	return created.ID, nil
}

// enqueueClusterTeardown creates a cluster.teardown job + task pair. Invoked
// from handleDeleteCluster — the worker drains every member and finally
// deletes the cluster row.
func (s *Server) enqueueClusterTeardown(r *http.Request, cluster *storage.Cluster) (uuid.UUID, error) {
	payload := clusterProvisionPayload{
		ClusterID: cluster.ID.String(),
		TenantID:  cluster.TenantID.String(),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal cluster.teardown payload: %w", err)
	}

	job := &storage.Job{
		TenantID: cluster.TenantID,
		Type:     JobTypeClusterTeardown,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	event := &storage.JobEvent{
		Status:  storage.JobStatusQueued,
		Message: "cluster.teardown queued",
	}

	ctx := r.Context()
	created, err := s.store.CreateJob(ctx, job, event)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create cluster.teardown job: %w", err)
	}

	if s.worker == nil {
		return uuid.Nil, errors.New("worker unavailable")
	}
	task := worker.Task{
		Name:         fmt.Sprintf("cluster-teardown-%s", created.ID),
		Job:          s.buildClusterTeardownJob(created.ID, cluster.ID, cluster.TenantID),
		MaxAttempts:  1,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, created.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), nil)
		return uuid.Nil, fmt.Errorf("enqueue cluster.teardown task: %w", err)
	}
	return created.ID, nil
}
