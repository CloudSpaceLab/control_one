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
)

// JobTypePatchDeployDirect dispatches an apt/dnf/winget upgrade run on the
// agent. Lifecycle is heartbeat-driven (same channel firewall.* uses):
// dispatch via PendingActions, completion via completed_actions.
const JobTypePatchDeployDirect = "patch.deploy_direct"

// patchJobPayload is the per-node payload the agent receives. Future modes
// (proxy, airgapped) extend this with additional fields; for now it's
// effectively just the node + state ids.
type patchJobPayload struct {
	NodePatchStateID string `json:"node_patch_state_id"`
	NodeID           string `json:"node_id"`
	DeploymentID     string `json:"deployment_id"`
	Mode             string `json:"mode"`
}

func decodePatchPayload(raw json.RawMessage) (any, error) {
	var p patchJobPayload
	if len(raw) == 0 {
		return nil, errors.New("patch payload required")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid patch payload: %w", err)
	}
	if _, err := uuid.Parse(p.NodePatchStateID); err != nil {
		return nil, fmt.Errorf("node_patch_state_id must be UUID: %w", err)
	}
	if _, err := uuid.Parse(p.NodeID); err != nil {
		return nil, fmt.Errorf("node_id must be UUID: %w", err)
	}
	return p, nil
}

// handlePatchDeployJob is the worker-side handler. Lifecycle is heartbeat
// driven so this is a no-op — the worker just parks the job in its current
// state until the agent reports back via completed_actions.
func (s *Server) handlePatchDeployJob(_ context.Context, _ *storage.Job) error {
	return nil
}

// ── HTTP endpoints ────────────────────────────────────────────────────────

type patchDeployRequest struct {
	TenantID string   `json:"tenant_id"`
	NodeIDs  []string `json:"node_ids,omitempty"` // empty → every enrolled node in the tenant
	Mode     string   `json:"mode,omitempty"`     // "direct" today; reserved for future
	Reason   string   `json:"reason,omitempty"`
}

type patchDeployResponse struct {
	Deployment *storage.PatchDeployment `json:"deployment"`
	NodeCount  int                      `json:"node_count"`
}

// handlePatchDeployments routes /api/v1/patch/deployments — POST creates a
// new deployment (operator-initiated), GET lists.
func (s *Server) handlePatchDeployments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreatePatchDeployment(w, r)
	case http.MethodGet:
		s.handleListPatchDeployments(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreatePatchDeployment(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req patchDeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "direct"
	}
	if mode != "direct" {
		// Proxy + airgapped land in a follow-up. Reject other modes
		// loudly so misconfigured clients don't silently fall back.
		http.Error(w, "only mode=direct is supported in this release", http.StatusBadRequest)
		return
	}

	// Resolve target nodes. Empty list → all enrolled nodes in tenant.
	nodeIDs, err := s.resolvePatchTargets(r.Context(), tenantID, req.NodeIDs)
	if err != nil {
		s.logger.Warn("resolve patch targets", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if len(nodeIDs) == 0 {
		http.Error(w, "no target nodes resolved", http.StatusBadRequest)
		return
	}

	requestedBy := principalUserID(s, r.Context(), principal)
	var by *uuid.UUID
	if requestedBy != uuid.Nil {
		id := requestedBy
		by = &id
	}
	deployment, err := s.store.CreatePatchDeployment(r.Context(), storage.PatchDeployment{
		TenantID:        tenantID,
		Mode:            mode,
		TargetNodeCount: len(nodeIDs),
		RequestedBy:     by,
		Summary: map[string]any{
			"reason":     req.Reason,
			"node_count": len(nodeIDs),
			"requested":  time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		s.logger.Error("create patch deployment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	dispatched := 0
	for _, nid := range nodeIDs {
		if _, err := s.dispatchPatchToNode(r.Context(), tenantID, deployment.ID, nid, mode); err != nil {
			s.logger.Warn("dispatch patch to node",
				zap.Error(err),
				zap.String("node_id", nid.String()),
				zap.String("deployment_id", deployment.ID.String()),
			)
			continue
		}
		dispatched++
	}

	// Flip header to in_progress as soon as we've dispatched at least one node.
	if dispatched > 0 {
		_ = s.store.UpdatePatchDeploymentStatus(r.Context(), deployment.ID, "in_progress", false)
	} else {
		_ = s.store.UpdatePatchDeploymentStatus(r.Context(), deployment.ID, "failed", true)
	}

	s.recordAudit(r.Context(), principal, tenantID, "patch.deploy.queued", "patch_deployment", deployment.ID.String(), map[string]any{
		"mode":       mode,
		"node_count": len(nodeIDs),
		"reason":     req.Reason,
	})

	writeJSON(w, http.StatusCreated, patchDeployResponse{
		Deployment: deployment,
		NodeCount:  dispatched,
	})
}

func (s *Server) handleListPatchDeployments(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 50)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	deployments, err := s.store.ListPatchDeployments(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Warn("list patch deployments", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Deployments []storage.PatchDeployment `json:"deployments"`
		GeneratedAt time.Time                 `json:"generated_at"`
	}{
		Deployments: deployments,
		GeneratedAt: time.Now().UTC(),
	})
}

// handlePatchDeploymentSubroute serves /api/v1/patch/deployments/:id/...
// Currently only .../nodes is implemented.
func (s *Server) handlePatchDeploymentSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/patch/deployments/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "deployment id must be a UUID", http.StatusBadRequest)
		return
	}
	if len(parts) >= 2 && parts[1] == "nodes" {
		s.handleListPatchDeploymentNodes(w, r, id)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleListPatchDeploymentNodes(w http.ResponseWriter, r *http.Request, deploymentID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	rows, err := s.store.ListNodePatchStatesForDeployment(r.Context(), deploymentID)
	if err != nil {
		s.logger.Warn("list patch deployment nodes", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Rows []storage.NodePatchState `json:"rows"`
	}{Rows: rows})
}

// ── helpers ───────────────────────────────────────────────────────────────

// resolvePatchTargets validates the operator-supplied node ids against the
// tenant boundary, or — when the list is empty — pulls every enrolled node
// in the tenant. Bounded at 1000 to keep one operator click from
// accidentally fanning out to the entire fleet of a megatenant.
func (s *Server) resolvePatchTargets(ctx context.Context, tenantID uuid.UUID, raw []string) ([]uuid.UUID, error) {
	if len(raw) == 0 {
		nodes, _, err := s.store.ListNodes(ctx, tenantID, "", 1000, 0)
		if err != nil {
			return nil, err
		}
		out := make([]uuid.UUID, 0, len(nodes))
		for i := range nodes {
			out = append(out, nodes[i].ID)
		}
		return out, nil
	}
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		nid, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("invalid node_id %q: %w", s, err)
		}
		out = append(out, nid)
	}
	// Enforce tenant boundary — caller can't reach into another tenant by
	// supplying its node ids.
	for _, nid := range out {
		node, err := s.store.GetNode(ctx, nid)
		if err != nil {
			return nil, err
		}
		if node == nil || node.TenantID != tenantID {
			return nil, fmt.Errorf("node %s does not belong to tenant", nid.String())
		}
	}
	return out, nil
}

// dispatchPatchToNode creates the per-node row + the corresponding job and
// links them. Uses the same pattern as PR 3's firewall fan-out.
func (s *Server) dispatchPatchToNode(ctx context.Context, tenantID, deploymentID, nodeID uuid.UUID, mode string) (*storage.NodePatchState, error) {
	state, err := s.store.CreateNodePatchState(ctx, storage.NodePatchState{
		DeploymentID: deploymentID,
		NodeID:       nodeID,
		TenantID:     tenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("create node patch state: %w", err)
	}
	if state == nil {
		return nil, errors.New("create node patch state returned nil")
	}

	payload := patchJobPayload{
		NodePatchStateID: state.ID.String(),
		NodeID:           nodeID.String(),
		DeploymentID:     deploymentID.String(),
		Mode:             mode,
	}
	payloadBytes, _ := json.Marshal(payload)
	job := &storage.Job{
		TenantID: tenantID,
		Type:     JobTypePatchDeployDirect,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	created, err := s.store.CreateJob(ctx, job, nil)
	if err != nil {
		return state, fmt.Errorf("create patch job: %w", err)
	}
	if err := s.store.SetNodePatchStateJobID(ctx, state.ID, created.ID); err != nil {
		s.logger.Warn("set node patch state job id", zap.Error(err))
	}
	state.JobID = &created.ID
	return state, nil
}
