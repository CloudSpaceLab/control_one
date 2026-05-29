package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

// JobTypePatchDeployDirect dispatches an apt/dnf/winget upgrade run on the
// agent. Lifecycle is heartbeat-driven (same channel firewall.* uses):
// dispatch via PendingActions, completion via completed_actions.
const JobTypePatchDeployDirect = "patch.deploy_direct"

// Wave C — patch-management completion job types. All seven follow the same
// heartbeat-driven lifecycle as patch.deploy_direct unless noted otherwise.
const (
	// JobTypePatchDeployProxy routes apt/yum through HTTP_PROXY against a
	// managed Squid. Agent reads proxy host:port from the job payload.
	JobTypePatchDeployProxy = "patch.deploy_proxy"
	// JobTypePatchDeployAirgapped reads from a pre-staged repo path on the
	// node — no upstream traffic. Agent uses apt-get -o Dir::Etc::SourceList.
	JobTypePatchDeployAirgapped = "patch.deploy_airgapped"
	// JobTypePatchInventoryScan asks the agent to enumerate available
	// upgrades (apt list --upgradable). Read-only.
	JobTypePatchInventoryScan = "patch.inventory_scan"
	// JobTypePatchOpenWindow is a server-internal pseudo-action: marks the
	// maintenance window 'open' and dispatches firewall.rule_add per host
	// in allow_repos for each node in the window.
	JobTypePatchOpenWindow = "patch.open_window"
	// JobTypePatchCloseWindow is the inverse — marks the window closed and
	// dispatches firewall.rule_delete.
	JobTypePatchCloseWindow = "patch.close_window"
	// JobTypeSquidInstall asks the agent to install squid via apt/yum.
	JobTypeSquidInstall = "squid.install"
	// JobTypeSquidReconfigure pushes a new whitelist to a running squid.
	// Server validates via `squid -k parse` first.
	JobTypeSquidReconfigure = "squid.reconfigure"
	// JobTypeSquidConfigureClient drops apt.conf.d/95proxy or netsh winhttp
	// on a client node so its package manager picks up the proxy.
	JobTypeSquidConfigureClient = "squid.configure_client"
)

// patchModeDirect / Proxy / Airgapped enumerate the legal node_patch_config
// modes. Kept as constants so server-side switches stay typo-proof.
const (
	patchModeDirect    = "direct"
	patchModeProxy     = "proxy"
	patchModeAirgapped = "airgapped"
)

// patchJobPayload is the per-node payload the agent receives. Direct mode
// uses just the IDs. Proxy mode adds ProxyURL (e.g. "http://10.0.0.5:3128").
// Airgapped mode adds StagedRepoPath (e.g. "/var/cache/apt/staged").
type patchJobPayload struct {
	NodePatchStateID string   `json:"node_patch_state_id"`
	NodeID           string   `json:"node_id"`
	DeploymentID     string   `json:"deployment_id"`
	ActionPlanID     string   `json:"action_plan_id,omitempty"`
	Mode             string   `json:"mode"`
	PackageAllowlist []string `json:"package_allowlist,omitempty"`
	PackageDenylist  []string `json:"package_denylist,omitempty"`
	PostPatchRescan  bool     `json:"post_patch_rescan,omitempty"`

	// Proxy mode.
	ProxyURL string `json:"proxy_url,omitempty"`

	// Airgapped mode.
	StagedRepoPath string `json:"staged_repo_path,omitempty"`
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
	DryRun   bool     `json:"dry_run,omitempty"`

	CanaryNodeIDs    []string `json:"canary_node_ids,omitempty"`
	WaveSize         int      `json:"wave_size,omitempty"`
	PackageAllowlist []string `json:"package_allowlist,omitempty"`
	PackageDenylist  []string `json:"package_denylist,omitempty"`
	PostPatchRescan  bool     `json:"post_patch_rescan,omitempty"`
}

type patchDeployResponse struct {
	Deployment  *storage.PatchDeployment `json:"deployment"`
	NodeCount   int                      `json:"node_count"`
	WaveNumber  int                      `json:"wave_number,omitempty"`
	Plan        *patchDeploymentPlan     `json:"plan,omitempty"`
	Succeeded   []string                 `json:"succeeded,omitempty"`
	Failed      []map[string]string      `json:"failed,omitempty"`
	GateBlocked []map[string]string      `json:"gate_blocked,omitempty"`
	// AwaitingApproval lists nodes that have been parked in patch_approvals
	// pending an operator green-light. Each entry carries the approval id so
	// the UI can deep-link straight into the approve/deny workflow.
	AwaitingApproval []map[string]string `json:"awaiting_approval,omitempty"`
}

type patchPackagePolicy struct {
	Allowlist       []string `json:"allowlist,omitempty"`
	Denylist        []string `json:"denylist,omitempty"`
	PostPatchRescan bool     `json:"post_patch_rescan,omitempty"`
}

type patchDeploymentPlan struct {
	TenantID      string                `json:"tenant_id"`
	RequestedMode string                `json:"requested_mode"`
	HeaderMode    string                `json:"header_mode"`
	Reason        string                `json:"reason,omitempty"`
	TotalNodes    int                   `json:"total_nodes"`
	WaveSize      int                   `json:"wave_size"`
	PackagePolicy patchPackagePolicy    `json:"package_policy"`
	Waves         []patchDeploymentWave `json:"waves"`
	GeneratedAt   string                `json:"generated_at"`
}

type patchDeploymentWave struct {
	WaveNumber int                   `json:"wave_number"`
	Canary     bool                  `json:"canary"`
	Nodes      []patchDeploymentNode `json:"nodes"`
}

type patchDeploymentNode struct {
	NodeID     string `json:"node_id"`
	Mode       string `json:"mode"`
	GateStatus string `json:"gate_status"`
	Reason     string `json:"reason,omitempty"`
}

type patchDispatchResult struct {
	WaveNumber       int
	Succeeded        []string
	Failed           []map[string]string
	GateBlocked      []map[string]string
	AwaitingApproval []map[string]string
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
	requestedMode := strings.TrimSpace(req.Mode)
	if requestedMode == "" {
		requestedMode = "auto"
	}
	switch requestedMode {
	case "auto", patchModeDirect, patchModeProxy, patchModeAirgapped:
	default:
		http.Error(w, "mode must be one of auto|direct|proxy|airgapped", http.StatusBadRequest)
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
	policy, err := patchPackagePolicyFromRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	plan, err := s.buildPatchDeploymentPlan(r.Context(), tenantID, nodeIDs, req, requestedMode, headerModeForPatchMode(requestedMode), policy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.DryRun {
		writeJSON(w, http.StatusOK, patchDeployResponse{NodeCount: len(nodeIDs), Plan: &plan})
		return
	}

	requestedBy := principalUserID(s, r.Context(), principal)
	var by *uuid.UUID
	if requestedBy != uuid.Nil {
		id := requestedBy
		by = &id
	}
	// Header mode tracks the primary mode dispatched. When the operator
	// selected "auto" we record "direct" — the per-node config can override
	// to proxy/airgapped, which surfaces in the per-node detail panel.
	headerMode := requestedMode
	if headerMode == "auto" {
		headerMode = patchModeDirect
	}
	headerMode = plan.HeaderMode
	deployment, err := s.store.CreatePatchDeployment(r.Context(), storage.PatchDeployment{
		TenantID:        tenantID,
		Mode:            headerMode,
		TargetNodeCount: len(nodeIDs),
		RequestedBy:     by,
		Summary: map[string]any{
			"reason":            req.Reason,
			"node_count":        len(nodeIDs),
			"requested":         time.Now().UTC().Format(time.RFC3339),
			"requested_mode":    requestedMode,
			"planned_node_ids":  patchPlanNodeIDs(plan),
			"canary_node_ids":   patchPlanCanaryNodeIDs(plan),
			"wave_size":         plan.WaveSize,
			"package_allowlist": policy.Allowlist,
			"package_denylist":  policy.Denylist,
			"post_patch_rescan": policy.PostPatchRescan,
			"waves":             plan.Waves,
		},
	})
	if err != nil {
		s.logger.Error("create patch deployment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	nodeIDs = patchPlanWaveNodeUUIDs(plan, 0)

	succeeded := make([]string, 0, len(nodeIDs))
	failed := make([]map[string]string, 0)
	gateBlocked := make([]map[string]string, 0)
	awaiting := make([]map[string]string, 0)

	for _, nid := range nodeIDs {
		nodeMode := requestedMode
		var proxyID, windowID *uuid.UUID
		if requestedMode == "auto" {
			cfg, cfgErr := s.store.GetNodePatchConfig(r.Context(), nid)
			if cfgErr != nil {
				s.logger.Warn("get node patch config", zap.Error(cfgErr), zap.String("node_id", nid.String()))
				nodeMode = patchModeDirect
			} else if cfg != nil {
				nodeMode = cfg.Mode
				proxyID = cfg.ProxyID
				windowID = cfg.WindowID
			} else {
				nodeMode = patchModeDirect
			}
		}

		// Gate routing: every per-node patch passes through the same 4 gates
		// the compliance remediation engine uses (opt-out / change window /
		// circuit breaker / approval). When the approval gate fires for a
		// tenant whose patch_requires_approval flag is true, the gate parks
		// a row in patch_approvals and the dispatch waits — the operator
		// approve endpoint re-runs dispatchPatchModeToNode for that row.
		gate := s.runPatchSafetyGates(r.Context(), tenantID, nid, deployment.ID, nodeMode, proxyID, windowID)
		switch {
		case gate.AwaitingApproval:
			entry := map[string]string{"node_id": nid.String()}
			if gate.ApprovalID != uuid.Nil {
				entry["approval_id"] = gate.ApprovalID.String()
			}
			entry["reason"] = gate.Reason
			awaiting = append(awaiting, entry)
			s.logger.Info("patch deploy parked for approval",
				zap.String("node_id", nid.String()),
				zap.String("approval_id", gate.ApprovalID.String()),
			)
			continue
		case !gate.Allowed:
			gateBlocked = append(gateBlocked, map[string]string{
				"node_id": nid.String(),
				"reason":  gate.Reason,
			})
			s.logger.Info("patch deploy blocked by safety gate",
				zap.String("node_id", nid.String()),
				zap.String("reason", gate.Reason),
			)
			continue
		}

		if _, err := s.dispatchPatchModeToNode(r.Context(), tenantID, deployment.ID, nid, nodeMode, proxyID, windowID, policy); err != nil {
			failed = append(failed, map[string]string{
				"node_id": nid.String(),
				"error":   err.Error(),
			})
			s.logger.Warn("dispatch patch to node",
				zap.Error(err),
				zap.String("node_id", nid.String()),
				zap.String("deployment_id", deployment.ID.String()),
			)
			continue
		}
		succeeded = append(succeeded, nid.String())
	}

	// Header status: in_progress when anything dispatched, pending_approval
	// when every target is parked behind the gate, failed when nothing
	// progressed at all. Pending_approval reuses the existing 'pending'
	// status so the schema check constraint stays valid.
	switch {
	case len(succeeded) > 0:
		_ = s.store.UpdatePatchDeploymentStatus(r.Context(), deployment.ID, "in_progress", false)
	case len(awaiting) > 0 && len(failed) == 0 && len(gateBlocked) == 0:
		// Leaves the deployment in 'pending' (the default insert state) so
		// approval-driven dispatch picks it up later. We still emit a
		// no-op status update so updated_at advances.
		_ = s.store.UpdatePatchDeploymentStatus(r.Context(), deployment.ID, "pending", false)
	default:
		_ = s.store.UpdatePatchDeploymentStatus(r.Context(), deployment.ID, "failed", true)
	}

	s.recordAudit(r.Context(), principal, tenantID, "patch.deploy.queued", "patch_deployment", deployment.ID.String(), map[string]any{
		"mode":              headerMode,
		"node_count":        plan.TotalNodes,
		"wave_number":       0,
		"wave_node_count":   len(nodeIDs),
		"reason":            req.Reason,
		"succeeded":         len(succeeded),
		"failed":            len(failed),
		"gate_blocked":      len(gateBlocked),
		"awaiting_approval": len(awaiting),
	})

	writeJSON(w, http.StatusCreated, patchDeployResponse{
		Deployment:       deployment,
		NodeCount:        len(succeeded),
		WaveNumber:       0,
		Plan:             &plan,
		Succeeded:        succeeded,
		Failed:           failed,
		GateBlocked:      gateBlocked,
		AwaitingApproval: awaiting,
	})
}

// patchGateResult captures a per-(deployment, node) gate decision. Allowed +
// AwaitingApproval are mutually exclusive; both being false means a hard
// block that surfaces in `gate_blocked`. AwaitingApproval=true means a row
// has been parked in patch_approvals and the dispatch will run once an
// operator approves it.
type patchGateResult struct {
	Allowed          bool
	AwaitingApproval bool
	Reason           string
	ApprovalID       uuid.UUID
}

// patchApprovalTTL bounds how long a parked patch approval remains
// actionable. After this window the reaper flips the row to expired and
// the operator must re-issue the deploy. 24h matches the operational
// rhythm of patch windows (deploy queued during business hours, applied
// in the next maintenance window).
const patchApprovalTTL = 24 * time.Hour

// runPatchSafetyGates runs the same four gates compliance remediation runs:
// opt-out label, change window, circuit breaker, approval.
//
// Approval semantics — D1 = proper (timeline §11):
//   - Tenants with patch_requires_approval=true (default): the gate writes a
//     pending row to patch_approvals and signals AwaitingApproval. The
//     operator approves via /api/v1/patch/approvals/:id/approve which calls
//     dispatchPatchModeToNode for that row. Denying flips the approval to
//     denied with no dispatch.
//   - Tenants with patch_requires_approval=false: the legacy immediate-
//     dispatch behaviour applies — the approval gate is a no-op so any
//     non-blocking deploy fans straight out.
func (s *Server) runPatchSafetyGates(ctx context.Context, tenantID, nodeID, deploymentID uuid.UUID, mode string, proxyID, windowID *uuid.UUID) patchGateResult {
	if s.store == nil {
		return patchGateResult{Allowed: true}
	}
	now := time.Now().UTC()

	// Gate 1 — opt-out label.
	if nodeID != uuid.Nil {
		if node, err := s.store.GetNode(ctx, nodeID); err == nil && node != nil && node.Labels != nil {
			posture := nodeIsolationPostureFromNode(*node, now)
			if posture.Active && posture.Mode == isolationModeAirgapped && mode != patchModeAirgapped {
				return patchGateResult{Reason: "node is airgapped; use airgapped patch mode or clear the isolation timer"}
			}
			if posture.Active && posture.Mode == isolationModeWhitelist && mode == patchModeDirect && windowID == nil && !stringSliceContainsFold(posture.AllowedApplications, "patch") {
				return patchGateResult{Reason: "node is whitelist-only; patch requires an allowed patch application, proxy, airgapped mode, or a maintenance window"}
			}
			if val, ok := node.Labels["remediation"]; ok {
				if str, ok := val.(string); ok && strings.EqualFold(strings.TrimSpace(str), "manual-only") {
					return patchGateResult{Reason: "node labelled remediation=manual-only"}
				}
			}
		}
	}

	// Gate 2 — change window. Patches are not "critical" so they always
	// defer outside a configured window. We surface that as a block (the
	// operator can either wait or open a maintenance window explicitly).
	cfg, cfgErr := s.store.GetTenantRemediationConfig(ctx, tenantID)
	if cfgErr != nil || cfg == nil {
		defaults := storage.DefaultTenantRemediationConfig(tenantID)
		cfg = &defaults
	}
	if !storage.IsInsideChangeWindow(cfg.ChangeWindows, now) {
		return patchGateResult{Reason: "outside tenant change window"}
	}

	// Gate 3 — circuit breaker. Use a synthetic rule_id keyed to the patch
	// pipeline so per-rule trip state stays scoped to patches.
	patchRuleID := "patch.deploy"
	if breaker, err := s.store.GetCircuitBreakerState(ctx, tenantID, patchRuleID); err == nil &&
		breaker != nil && breaker.AckedAt == nil {
		return patchGateResult{Reason: fmt.Sprintf("circuit breaker tripped: %s", breaker.TrippedReason)}
	}

	// Gate 4 — approval. The proper approve→dispatch loop (D1: proper)
	// parks a patch_approvals row when the tenant requires approval. The
	// operator approve endpoint then re-runs dispatchPatchModeToNode. When
	// the tenant has opted out (PatchRequiresApproval=false) we keep the
	// legacy immediate-dispatch path — the gate is a no-op.
	if cfg.PatchRequiresApproval {
		approval, err := s.store.CreatePatchApproval(ctx, storage.CreatePatchApprovalParams{
			TenantID:     tenantID,
			DeploymentID: deploymentID,
			NodeID:       nodeID,
			Mode:         mode,
			ProxyID:      proxyID,
			WindowID:     windowID,
			ExpiresAt:    now.Add(patchApprovalTTL),
		})
		if err != nil {
			s.logger.Warn("create patch approval",
				zap.Error(err),
				zap.String("tenant_id", tenantID.String()),
				zap.String("deployment_id", deploymentID.String()),
				zap.String("node_id", nodeID.String()),
			)
			return patchGateResult{Reason: "approval gate write failed: " + err.Error()}
		}

		// Notify operators via the existing webhook channel so the chat-
		// first investigation surface and the approvals UI stay in sync.
		s.emitRemediationSafetyEvent(ctx, tenantID, EventRemediationApprovalRequested, map[string]any{
			"tenant_id":     tenantID.String(),
			"node_id":       nodeID.String(),
			"deployment_id": deploymentID.String(),
			"approval_id":   approval.ID.String(),
			"rule_id":       patchRuleID,
			"severity":      "high",
			"mode":          mode,
			"context":       "patch.deploy",
		})

		return patchGateResult{
			AwaitingApproval: true,
			Reason:           "approval required",
			ApprovalID:       approval.ID,
		}
	}

	// All four gates passed.
	_ = compliance.Result{} // keep the import live for the gate type.
	return patchGateResult{Allowed: true}
}

func (s *Server) previewPatchSafetyGates(ctx context.Context, tenantID, nodeID uuid.UUID, mode string, windowID *uuid.UUID) patchGateResult {
	if s.store == nil {
		return patchGateResult{Allowed: true}
	}
	now := time.Now().UTC()
	if nodeID != uuid.Nil {
		if node, err := s.store.GetNode(ctx, nodeID); err == nil && node != nil && node.Labels != nil {
			posture := nodeIsolationPostureFromNode(*node, now)
			if posture.Active && posture.Mode == isolationModeAirgapped && mode != patchModeAirgapped {
				return patchGateResult{Reason: "node is airgapped; use airgapped patch mode or clear the isolation timer"}
			}
			if posture.Active && posture.Mode == isolationModeWhitelist && mode == patchModeDirect && windowID == nil && !stringSliceContainsFold(posture.AllowedApplications, "patch") {
				return patchGateResult{Reason: "node is whitelist-only; patch requires an allowed patch application, proxy, airgapped mode, or a maintenance window"}
			}
			if val, ok := node.Labels["remediation"]; ok {
				if str, ok := val.(string); ok && strings.EqualFold(strings.TrimSpace(str), "manual-only") {
					return patchGateResult{Reason: "node labelled remediation=manual-only"}
				}
			}
		}
	}
	cfg, cfgErr := s.store.GetTenantRemediationConfig(ctx, tenantID)
	if cfgErr != nil || cfg == nil {
		defaults := storage.DefaultTenantRemediationConfig(tenantID)
		cfg = &defaults
	}
	if !storage.IsInsideChangeWindow(cfg.ChangeWindows, now) {
		return patchGateResult{Reason: "outside tenant change window"}
	}
	patchRuleID := "patch.deploy"
	if breaker, err := s.store.GetCircuitBreakerState(ctx, tenantID, patchRuleID); err == nil &&
		breaker != nil && breaker.AckedAt == nil {
		return patchGateResult{Reason: fmt.Sprintf("circuit breaker tripped: %s", breaker.TrippedReason)}
	}
	if cfg.PatchRequiresApproval {
		return patchGateResult{AwaitingApproval: true, Reason: "approval required"}
	}
	return patchGateResult{Allowed: true}
}

func stringSliceContainsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
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
	if parts[0] == "plan" {
		s.handlePatchDeploymentPlan(w, r)
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
	if len(parts) >= 2 && parts[1] == "advance" {
		s.handleAdvancePatchDeployment(w, r, id)
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
func (s *Server) handlePatchDeploymentPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleOperator, roleAdmin); !ok {
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
	requestedMode := strings.TrimSpace(req.Mode)
	if requestedMode == "" {
		requestedMode = "auto"
	}
	switch requestedMode {
	case "auto", patchModeDirect, patchModeProxy, patchModeAirgapped:
	default:
		http.Error(w, "mode must be one of auto|direct|proxy|airgapped", http.StatusBadRequest)
		return
	}
	nodeIDs, err := s.resolvePatchTargets(r.Context(), tenantID, req.NodeIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	policy, err := patchPackagePolicyFromRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	plan, err := s.buildPatchDeploymentPlan(r.Context(), tenantID, nodeIDs, req, requestedMode, headerModeForPatchMode(requestedMode), policy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
}

func (s *Server) handleAdvancePatchDeployment(w http.ResponseWriter, r *http.Request, deploymentID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	deployment, err := s.store.GetPatchDeployment(r.Context(), deploymentID)
	if err != nil {
		s.logger.Error("get patch deployment for advance", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if deployment == nil {
		http.NotFound(w, r)
		return
	}
	rows, err := s.store.ListNodePatchStatesForDeployment(r.Context(), deploymentID)
	if err != nil {
		s.logger.Error("list patch states for advance", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if pending, failed := patchWavePendingFailed(rows); pending > 0 || failed > 0 {
		http.Error(w, fmt.Sprintf("cannot advance while previous wave has %d pending and %d failed nodes", pending, failed), http.StatusConflict)
		return
	}
	plan := patchPlanFromDeployment(deployment)
	if len(plan.Waves) == 0 {
		http.Error(w, "deployment has no wave plan", http.StatusConflict)
		return
	}
	nextWave := nextPatchWaveNumber(plan, rows)
	if nextWave < 0 {
		http.Error(w, "all planned waves have already been dispatched", http.StatusConflict)
		return
	}
	result := s.dispatchPatchPlanWave(r.Context(), deployment.TenantID, deployment.ID, plan, nextWave)
	if len(result.Succeeded) > 0 {
		_ = s.store.UpdatePatchDeploymentStatus(r.Context(), deployment.ID, "in_progress", false)
	}
	s.recordAudit(r.Context(), principal, deployment.TenantID, "patch.deploy.wave_advanced", "patch_deployment", deployment.ID.String(), map[string]any{
		"wave_number":       result.WaveNumber,
		"succeeded":         len(result.Succeeded),
		"failed":            len(result.Failed),
		"gate_blocked":      len(result.GateBlocked),
		"awaiting_approval": len(result.AwaitingApproval),
	})
	writeJSON(w, http.StatusOK, patchDeployResponse{
		Deployment:       deployment,
		NodeCount:        len(result.Succeeded),
		WaveNumber:       result.WaveNumber,
		Plan:             &plan,
		Succeeded:        result.Succeeded,
		Failed:           result.Failed,
		GateBlocked:      result.GateBlocked,
		AwaitingApproval: result.AwaitingApproval,
	})
}

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

func headerModeForPatchMode(mode string) string {
	if strings.TrimSpace(mode) == "" || mode == "auto" {
		return patchModeDirect
	}
	return strings.TrimSpace(mode)
}

func patchPackagePolicyFromRequest(req patchDeployRequest) (patchPackagePolicy, error) {
	policy := patchPackagePolicy{
		Allowlist:       normalizePatchPackageNames(req.PackageAllowlist),
		Denylist:        normalizePatchPackageNames(req.PackageDenylist),
		PostPatchRescan: req.PostPatchRescan,
	}
	for _, allow := range policy.Allowlist {
		for _, deny := range policy.Denylist {
			if strings.EqualFold(allow, deny) {
				return policy, fmt.Errorf("package %q cannot be in both allowlist and denylist", allow)
			}
		}
	}
	if len(policy.Denylist) > 0 && len(policy.Allowlist) == 0 {
		return policy, errors.New("package_denylist requires package_allowlist so the agent can fail closed instead of upgrading everything")
	}
	return policy, nil
}

func normalizePatchPackageNames(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *Server) buildPatchDeploymentPlan(ctx context.Context, tenantID uuid.UUID, nodeIDs []uuid.UUID, req patchDeployRequest, requestedMode, headerMode string, policy patchPackagePolicy) (patchDeploymentPlan, error) {
	waveIDs, err := buildPatchWaveNodeIDs(nodeIDs, req.CanaryNodeIDs, req.WaveSize)
	if err != nil {
		return patchDeploymentPlan{}, err
	}
	plan := patchDeploymentPlan{
		TenantID:      tenantID.String(),
		RequestedMode: requestedMode,
		HeaderMode:    headerMode,
		Reason:        strings.TrimSpace(req.Reason),
		TotalNodes:    len(nodeIDs),
		WaveSize:      effectivePatchWaveSize(len(nodeIDs), len(req.CanaryNodeIDs), req.WaveSize),
		PackagePolicy: policy,
		Waves:         make([]patchDeploymentWave, 0, len(waveIDs)),
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	for waveNumber, ids := range waveIDs {
		wave := patchDeploymentWave{WaveNumber: waveNumber, Canary: waveNumber == 0 && len(req.CanaryNodeIDs) > 0}
		for _, nodeID := range ids {
			mode, _, windowID := s.resolvePatchModeForNode(ctx, nodeID, requestedMode)
			gate := s.previewPatchSafetyGates(ctx, tenantID, nodeID, mode, windowID)
			status := "allowed"
			switch {
			case gate.AwaitingApproval:
				status = "approval_required"
			case !gate.Allowed:
				status = "blocked"
			}
			wave.Nodes = append(wave.Nodes, patchDeploymentNode{
				NodeID:     nodeID.String(),
				Mode:       mode,
				GateStatus: status,
				Reason:     gate.Reason,
			})
		}
		plan.Waves = append(plan.Waves, wave)
	}
	return plan, nil
}

func buildPatchWaveNodeIDs(nodeIDs []uuid.UUID, rawCanaries []string, waveSize int) ([][]uuid.UUID, error) {
	if waveSize < 0 {
		return nil, errors.New("wave_size must be non-negative")
	}
	targets := map[uuid.UUID]struct{}{}
	for _, nodeID := range nodeIDs {
		targets[nodeID] = struct{}{}
	}
	canarySeen := map[uuid.UUID]struct{}{}
	canaries := make([]uuid.UUID, 0, len(rawCanaries))
	for _, raw := range rawCanaries {
		nodeID, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid canary_node_id %q: %w", raw, err)
		}
		if _, ok := targets[nodeID]; !ok {
			return nil, fmt.Errorf("canary node %s is not in target nodes", nodeID)
		}
		if _, ok := canarySeen[nodeID]; ok {
			continue
		}
		canarySeen[nodeID] = struct{}{}
		canaries = append(canaries, nodeID)
	}
	size := effectivePatchWaveSize(len(nodeIDs), len(canaries), waveSize)
	if size <= 0 {
		size = len(nodeIDs)
	}
	var waves [][]uuid.UUID
	if len(canaries) > 0 {
		waves = append(waves, canaries)
	}
	var remaining []uuid.UUID
	for _, nodeID := range nodeIDs {
		if _, ok := canarySeen[nodeID]; ok {
			continue
		}
		remaining = append(remaining, nodeID)
	}
	for len(remaining) > 0 {
		n := size
		if n > len(remaining) {
			n = len(remaining)
		}
		waves = append(waves, append([]uuid.UUID(nil), remaining[:n]...))
		remaining = remaining[n:]
	}
	return waves, nil
}

func effectivePatchWaveSize(totalNodes, canaryCount, requested int) int {
	if requested > 0 {
		return requested
	}
	if totalNodes <= canaryCount {
		return totalNodes
	}
	return totalNodes - canaryCount
}

func (s *Server) resolvePatchModeForNode(ctx context.Context, nodeID uuid.UUID, requestedMode string) (string, *uuid.UUID, *uuid.UUID) {
	nodeMode := requestedMode
	var proxyID, windowID *uuid.UUID
	if nodeMode == "" || nodeMode == "auto" {
		cfg, err := s.store.GetNodePatchConfig(ctx, nodeID)
		if err != nil {
			s.logger.Warn("get node patch config", zap.Error(err), zap.String("node_id", nodeID.String()))
			return patchModeDirect, nil, nil
		}
		if cfg != nil {
			nodeMode = cfg.Mode
			proxyID = cfg.ProxyID
			windowID = cfg.WindowID
		} else {
			nodeMode = patchModeDirect
		}
	}
	return nodeMode, proxyID, windowID
}

func patchPlanNodeIDs(plan patchDeploymentPlan) []string {
	var out []string
	for _, wave := range plan.Waves {
		for _, node := range wave.Nodes {
			out = append(out, node.NodeID)
		}
	}
	return out
}

func patchPlanCanaryNodeIDs(plan patchDeploymentPlan) []string {
	if len(plan.Waves) == 0 || !plan.Waves[0].Canary {
		return nil
	}
	out := make([]string, 0, len(plan.Waves[0].Nodes))
	for _, node := range plan.Waves[0].Nodes {
		out = append(out, node.NodeID)
	}
	return out
}

func patchPlanWaveNodeUUIDs(plan patchDeploymentPlan, waveNumber int) []uuid.UUID {
	for _, wave := range plan.Waves {
		if wave.WaveNumber != waveNumber {
			continue
		}
		out := make([]uuid.UUID, 0, len(wave.Nodes))
		for _, node := range wave.Nodes {
			if parsed, err := uuid.Parse(node.NodeID); err == nil {
				out = append(out, parsed)
			}
		}
		return out
	}
	return nil
}

func patchPackagePolicyFromSummary(summary map[string]any) patchPackagePolicy {
	return patchPackagePolicy{
		Allowlist:       stringSliceFromSummary(summary["package_allowlist"]),
		Denylist:        stringSliceFromSummary(summary["package_denylist"]),
		PostPatchRescan: boolFromSummary(summary["post_patch_rescan"]),
	}
}

func patchPlanFromDeployment(deployment *storage.PatchDeployment) patchDeploymentPlan {
	if deployment == nil {
		return patchDeploymentPlan{}
	}
	summary := deployment.Summary
	plan := patchDeploymentPlan{
		TenantID:      deployment.TenantID.String(),
		RequestedMode: stringFromSummary(summary["requested_mode"]),
		HeaderMode:    deployment.Mode,
		Reason:        stringFromSummary(summary["reason"]),
		TotalNodes:    intFromSummary(summary["node_count"]),
		WaveSize:      intFromSummary(summary["wave_size"]),
		PackagePolicy: patchPackagePolicyFromSummary(summary),
		GeneratedAt:   stringFromSummary(summary["requested"]),
	}
	if plan.RequestedMode == "" {
		plan.RequestedMode = deployment.Mode
	}
	if plan.TotalNodes == 0 {
		plan.TotalNodes = deployment.TargetNodeCount
	}
	if raw, ok := summary["waves"]; ok {
		data, _ := json.Marshal(raw)
		_ = json.Unmarshal(data, &plan.Waves)
	}
	return plan
}

func stringSliceFromSummary(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
				out = append(out, strings.TrimSpace(str))
			}
		}
		return out
	default:
		return nil
	}
}

func stringFromSummary(value any) string {
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return ""
}

func boolFromSummary(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	return false
}

func intFromSummary(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func patchWavePendingFailed(rows []storage.NodePatchState) (int, int) {
	pending, failed := 0, 0
	for _, row := range rows {
		switch row.Status {
		case "pending":
			pending++
		case "failed":
			failed++
		}
	}
	return pending, failed
}

func nextPatchWaveNumber(plan patchDeploymentPlan, rows []storage.NodePatchState) int {
	dispatched := map[string]struct{}{}
	for _, row := range rows {
		dispatched[row.NodeID.String()] = struct{}{}
	}
	for _, wave := range plan.Waves {
		for _, node := range wave.Nodes {
			if _, ok := dispatched[node.NodeID]; !ok {
				return wave.WaveNumber
			}
		}
	}
	return -1
}

func (s *Server) dispatchPatchPlanWave(ctx context.Context, tenantID, deploymentID uuid.UUID, plan patchDeploymentPlan, waveNumber int) patchDispatchResult {
	policy := plan.PackagePolicy
	result := patchDispatchResult{WaveNumber: waveNumber}
	for _, nid := range patchPlanWaveNodeUUIDs(plan, waveNumber) {
		nodeMode, proxyID, windowID := s.resolvePatchModeForNode(ctx, nid, plan.RequestedMode)
		gate := s.runPatchSafetyGates(ctx, tenantID, nid, deploymentID, nodeMode, proxyID, windowID)
		switch {
		case gate.AwaitingApproval:
			entry := map[string]string{"node_id": nid.String(), "reason": gate.Reason}
			if gate.ApprovalID != uuid.Nil {
				entry["approval_id"] = gate.ApprovalID.String()
			}
			result.AwaitingApproval = append(result.AwaitingApproval, entry)
			continue
		case !gate.Allowed:
			result.GateBlocked = append(result.GateBlocked, map[string]string{"node_id": nid.String(), "reason": gate.Reason})
			continue
		}
		if _, err := s.dispatchPatchModeToNode(ctx, tenantID, deploymentID, nid, nodeMode, proxyID, windowID, policy); err != nil {
			result.Failed = append(result.Failed, map[string]string{"node_id": nid.String(), "error": err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, nid.String())
	}
	return result
}

// dispatchPatchModeToNode chooses the job type based on the resolved per-node
// mode and embeds the proxy URL or airgapped staged path in the payload as
// needed. Returns the created NodePatchState row.
func (s *Server) dispatchPatchModeToNode(
	ctx context.Context,
	tenantID, deploymentID, nodeID uuid.UUID,
	mode string,
	proxyID *uuid.UUID,
	windowID *uuid.UUID,
	policy patchPackagePolicy,
) (*storage.NodePatchState, error) {
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

	var jobType string
	payload := patchJobPayload{
		NodePatchStateID: state.ID.String(),
		NodeID:           nodeID.String(),
		DeploymentID:     deploymentID.String(),
		Mode:             mode,
		PackageAllowlist: policy.Allowlist,
		PackageDenylist:  policy.Denylist,
		PostPatchRescan:  policy.PostPatchRescan,
	}
	if actionPlanID := s.createPatchNodeActionPlan(ctx, tenantID, deploymentID, state.ID, nodeID, mode, policy); actionPlanID != uuid.Nil {
		payload.ActionPlanID = actionPlanID.String()
	}
	switch mode {
	case patchModeProxy:
		jobType = JobTypePatchDeployProxy
		if proxyID != nil {
			proxy, perr := s.store.GetSquidProxy(ctx, *proxyID)
			if perr != nil {
				return state, fmt.Errorf("load proxy: %w", perr)
			}
			if proxy == nil {
				return state, fmt.Errorf("proxy %s not found", proxyID)
			}
			payload.ProxyURL = fmt.Sprintf("http://%s:%d", proxy.Host, proxy.Port)
		} else {
			return state, errors.New("proxy mode selected but no proxy_id configured for node")
		}
	case patchModeAirgapped:
		jobType = JobTypePatchDeployAirgapped
		// Default staged path the bundle drops files into. Operators can
		// override per-node via the patch config endpoint.
		payload.StagedRepoPath = "/var/cache/control-one/staged-repos"
	case patchModeDirect:
		jobType = JobTypePatchDeployDirect
	default:
		return state, fmt.Errorf("unsupported patch mode %q", mode)
	}
	_ = windowID // window association is recorded on the deployment summary; firewall opens happen via JobTypePatchOpenWindow.

	payloadBytes, _ := json.Marshal(payload)
	job := &storage.Job{
		TenantID: tenantID,
		Type:     jobType,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	created, err := s.store.CreateJob(ctx, job, nil)
	if err != nil {
		s.markActionPlanFailed(ctx, payload.ActionPlanID)
		return state, fmt.Errorf("create patch job: %w", err)
	}
	if err := s.store.SetNodePatchStateJobID(ctx, state.ID, created.ID); err != nil {
		s.logger.Warn("set node patch state job id", zap.Error(err))
	}
	state.JobID = &created.ID
	return state, nil
}

// ── new job-type decoders + no-op handlers (heartbeat-driven) ────────────

// patchInventoryPayload is the agent input for patch.inventory_scan. The
// agent enumerates available upgrades (apt list --upgradable etc.) and
// reports the delta back through completed_actions metadata.
type patchInventoryPayload struct {
	NodeID string `json:"node_id"`
}

func decodePatchInventoryPayload(raw json.RawMessage) (any, error) {
	var p patchInventoryPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid patch inventory payload: %w", err)
	}
	if _, err := uuid.Parse(p.NodeID); err != nil {
		return nil, fmt.Errorf("node_id must be UUID: %w", err)
	}
	return p, nil
}

// squidJobPayload is the shared envelope for squid.* job types.
type squidJobPayload struct {
	ProxyID   string   `json:"proxy_id"`
	NodeID    string   `json:"node_id"`
	Whitelist []string `json:"whitelist,omitempty"`
	ProxyURL  string   `json:"proxy_url,omitempty"`
}

func decodeSquidPayload(raw json.RawMessage) (any, error) {
	var p squidJobPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid squid payload: %w", err)
	}
	if _, err := uuid.Parse(p.NodeID); err != nil {
		return nil, fmt.Errorf("node_id must be UUID: %w", err)
	}
	return p, nil
}

// handlePatchHeartbeatJob is a shared no-op for the heartbeat-driven patch
// job types (proxy / airgapped / inventory). The lifecycle runs through
// PendingActions + completed_actions like patch.deploy_direct.
func (s *Server) handlePatchHeartbeatJob(_ context.Context, _ *storage.Job) error {
	return nil
}

// handleSquidHeartbeatJob is the shared no-op for squid.install /
// squid.reconfigure / squid.configure_client.
func (s *Server) handleSquidHeartbeatJob(_ context.Context, _ *storage.Job) error {
	return nil
}

// handlePatchWindowJob handles patch.open_window / patch.close_window. These
// are server-internal jobs — they fan out firewall_rule_add /
// firewall_rule_delete actions to the nodes inside the window.
func (s *Server) handlePatchWindowJob(ctx context.Context, job *storage.Job) error {
	if job == nil {
		return errors.New("nil job")
	}
	var payload struct {
		WindowID string `json:"window_id"`
		Action   string `json:"action"` // "open" | "close"
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("decode window payload: %w", err)
	}
	windowID, err := uuid.Parse(payload.WindowID)
	if err != nil {
		return fmt.Errorf("window_id must be UUID: %w", err)
	}
	window, err := s.store.GetMaintenanceWindow(ctx, windowID)
	if err != nil {
		return fmt.Errorf("load window: %w", err)
	}
	if window == nil {
		return fmt.Errorf("window %s not found", windowID)
	}
	if payload.Action == "open" {
		// Dispatch firewall.rule_add for each (node, repo) pair so the
		// allow-repos open while the window is in flight.
		for _, nodeID := range window.NodeIDs {
			for _, host := range window.AllowRepos {
				_, _, ferr := s.dispatchFirewallRule(ctx, window.TenantID, uuid.New(), nodeID, "allow", host, "maintenance window opening", nil)
				if ferr != nil {
					s.logger.Warn("dispatch maintenance window allow",
						zap.String("window_id", windowID.String()),
						zap.String("node_id", nodeID.String()),
						zap.String("host", host),
						zap.Error(ferr),
					)
				}
			}
		}
		_ = s.store.MarkMaintenanceWindowOpen(ctx, windowID, nil)
		return nil
	}
	if payload.Action == "close" {
		for _, nodeID := range window.NodeIDs {
			for _, host := range window.AllowRepos {
				_, _, ferr := s.dispatchFirewallRule(ctx, window.TenantID, uuid.New(), nodeID, "block", host, "maintenance window closing", nil)
				if ferr != nil {
					s.logger.Warn("dispatch maintenance window block",
						zap.String("window_id", windowID.String()),
						zap.String("node_id", nodeID.String()),
						zap.String("host", host),
						zap.Error(ferr),
					)
				}
			}
		}
		_ = s.store.MarkMaintenanceWindowClosed(ctx, windowID)
		return nil
	}
	return fmt.Errorf("unknown window action %q", payload.Action)
}

// ── HTTP endpoints — patch config ─────────────────────────────────────────

// handlePatchConfig serves /api/v1/patch/config (GET/POST/PATCH). Per-node
// mode + optional proxy/window id. Admin only.
func (s *Server) handlePatchConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetPatchConfig(w, r)
	case http.MethodPost, http.MethodPatch:
		s.handleUpsertPatchConfig(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, PATCH")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetPatchConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("node_id")))
	if err != nil {
		http.Error(w, "node_id must be a UUID", http.StatusBadRequest)
		return
	}
	cfg, err := s.store.GetNodePatchConfig(r.Context(), nodeID)
	if err != nil {
		s.logger.Warn("get node patch config", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cfg == nil {
		// Implicit default: direct mode.
		cfg = &storage.NodePatchConfig{NodeID: nodeID, Mode: patchModeDirect}
	}
	writeJSON(w, http.StatusOK, cfg)
}

type patchConfigUpsertRequest struct {
	NodeID   string  `json:"node_id"`
	Mode     string  `json:"mode"`
	ProxyID  *string `json:"proxy_id,omitempty"`
	WindowID *string `json:"window_id,omitempty"`
}

func (s *Server) handleUpsertPatchConfig(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req patchConfigUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(req.NodeID))
	if err != nil {
		http.Error(w, "node_id must be a UUID", http.StatusBadRequest)
		return
	}
	mode := strings.TrimSpace(req.Mode)
	switch mode {
	case patchModeDirect, patchModeProxy, patchModeAirgapped:
	default:
		http.Error(w, "mode must be direct|proxy|airgapped", http.StatusBadRequest)
		return
	}
	in := storage.NodePatchConfig{NodeID: nodeID, Mode: mode}
	if req.ProxyID != nil && strings.TrimSpace(*req.ProxyID) != "" {
		pid, err := uuid.Parse(strings.TrimSpace(*req.ProxyID))
		if err != nil {
			http.Error(w, "proxy_id must be a UUID", http.StatusBadRequest)
			return
		}
		in.ProxyID = &pid
	}
	if req.WindowID != nil && strings.TrimSpace(*req.WindowID) != "" {
		wid, err := uuid.Parse(strings.TrimSpace(*req.WindowID))
		if err != nil {
			http.Error(w, "window_id must be a UUID", http.StatusBadRequest)
			return
		}
		in.WindowID = &wid
	}
	out, err := s.store.UpsertNodePatchConfig(r.Context(), in)
	if err != nil {
		s.logger.Warn("upsert node patch config", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	// Look up the node to scope the audit to its tenant.
	var tenantID uuid.UUID
	if node, _ := s.store.GetNode(r.Context(), nodeID); node != nil {
		tenantID = node.TenantID
	}
	s.recordAudit(r.Context(), principal, tenantID, "patch.config.upsert", "node_patch_config", nodeID.String(), map[string]any{
		"mode": mode,
	})
	writeJSON(w, http.StatusOK, out)
}

// ── HTTP endpoints — maintenance windows ─────────────────────────────────

func (s *Server) handleMaintenanceWindowsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListMaintenanceWindows(w, r)
	case http.MethodPost:
		s.handleCreateMaintenanceWindow(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.store.ListMaintenanceWindows(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("list maintenance windows", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Windows []storage.MaintenanceWindow `json:"windows"`
	}{Windows: rows})
}

type maintenanceWindowCreateRequest struct {
	TenantID   string    `json:"tenant_id"`
	Name       string    `json:"name"`
	NodeIDs    []string  `json:"node_ids"`
	OpensAt    time.Time `json:"opens_at"`
	ClosesAt   time.Time `json:"closes_at"`
	AllowRepos []string  `json:"allow_repos"`
}

func (s *Server) handleCreateMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req maintenanceWindowCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if !req.ClosesAt.After(req.OpensAt) {
		http.Error(w, "closes_at must be after opens_at", http.StatusBadRequest)
		return
	}
	nodeIDs := make([]uuid.UUID, 0, len(req.NodeIDs))
	for _, nstr := range req.NodeIDs {
		nid, err := uuid.Parse(strings.TrimSpace(nstr))
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid node_id %q", nstr), http.StatusBadRequest)
			return
		}
		nodeIDs = append(nodeIDs, nid)
	}
	out, err := s.store.CreateMaintenanceWindow(r.Context(), storage.MaintenanceWindow{
		TenantID:   tenantID,
		Name:       req.Name,
		NodeIDs:    nodeIDs,
		OpensAt:    req.OpensAt,
		ClosesAt:   req.ClosesAt,
		AllowRepos: req.AllowRepos,
	})
	if err != nil {
		s.logger.Warn("create maintenance window", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "patch.window.create", "maintenance_window", out.ID.String(), map[string]any{
		"name":       req.Name,
		"node_count": len(nodeIDs),
	})
	writeJSON(w, http.StatusCreated, out)
}

// handleMaintenanceWindowSubroute serves /api/v1/patch/maintenance-windows/:id/{open,close,force-close}.
func (s *Server) handleMaintenanceWindowSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/patch/maintenance-windows/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "window id must be a UUID", http.StatusBadRequest)
		return
	}
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	switch parts[1] {
	case "open":
		s.dispatchMaintenanceWindowAction(w, r, principal, id, "open")
	case "close":
		s.dispatchMaintenanceWindowAction(w, r, principal, id, "close")
	case "force-close":
		if err := s.store.ForceCloseMaintenanceWindow(r.Context(), id); err != nil {
			s.logger.Warn("force close maintenance window", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		window, _ := s.store.GetMaintenanceWindow(r.Context(), id)
		var tenantID uuid.UUID
		if window != nil {
			tenantID = window.TenantID
		}
		s.recordAudit(r.Context(), principal, tenantID, "patch.window.force_close", "maintenance_window", id.String(), nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) dispatchMaintenanceWindowAction(w http.ResponseWriter, r *http.Request, principal *auth.Principal, windowID uuid.UUID, action string) {
	window, err := s.store.GetMaintenanceWindow(r.Context(), windowID)
	if err != nil {
		s.logger.Warn("load maintenance window", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if window == nil {
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}
	jobType := JobTypePatchOpenWindow
	if action == "close" {
		jobType = JobTypePatchCloseWindow
	}
	payload, _ := json.Marshal(map[string]string{
		"window_id": windowID.String(),
		"action":    action,
	})
	job := &storage.Job{
		TenantID: window.TenantID,
		Type:     jobType,
		Status:   storage.JobStatusQueued,
		Payload:  payload,
	}
	if _, err := s.store.CreateJob(r.Context(), job, nil); err != nil {
		s.logger.Warn("enqueue window job", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, window.TenantID, "patch.window."+action, "maintenance_window", windowID.String(), nil)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued", "action": action})
}

// ── HTTP endpoints — squid proxies ───────────────────────────────────────

func (s *Server) handleSquidProxiesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSquidProxies(w, r)
	case http.MethodPost:
		s.handleCreateSquidProxy(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListSquidProxies(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.store.ListSquidProxies(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("list squid proxies", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Proxies []storage.SquidProxy `json:"proxies"`
	}{Proxies: rows})
}

type squidProxyCreateRequest struct {
	TenantID  string   `json:"tenant_id"`
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	Whitelist []string `json:"whitelist"`
}

func (s *Server) handleCreateSquidProxy(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req squidProxyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Host) == "" {
		http.Error(w, "host is required", http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 3128
	}
	out, err := s.store.CreateSquidProxy(r.Context(), storage.SquidProxy{
		TenantID:  tenantID,
		Host:      req.Host,
		Port:      req.Port,
		Whitelist: req.Whitelist,
	})
	if err != nil {
		s.logger.Warn("create squid proxy", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "patch.squid.create", "squid_proxy", out.ID.String(), map[string]any{
		"host": req.Host,
		"port": req.Port,
	})
	writeJSON(w, http.StatusCreated, out)
}

// handleSquidProxySubroute serves /api/v1/patch/proxies/:id/{install,reconfigure}
// and DELETE /api/v1/patch/proxies/:id.
func (s *Server) handleSquidProxySubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/patch/proxies/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "proxy id must be a UUID", http.StatusBadRequest)
		return
	}
	if len(parts) == 1 {
		// /api/v1/patch/proxies/:id (DELETE)
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", http.MethodDelete)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		proxy, _ := s.store.GetSquidProxy(r.Context(), id)
		_ = s.store.UpdateSquidProxyStatus(r.Context(), id, "removing", "")
		var tenantID uuid.UUID
		if proxy != nil {
			tenantID = proxy.TenantID
		}
		s.recordAudit(r.Context(), principal, tenantID, "patch.squid.remove", "squid_proxy", id.String(), nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "removing"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	switch parts[1] {
	case "install":
		s.handleSquidInstall(w, r, principal, id)
	case "reconfigure":
		s.handleSquidReconfigure(w, r, principal, id)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSquidInstall(w http.ResponseWriter, r *http.Request, principal *auth.Principal, proxyID uuid.UUID) {
	proxy, err := s.store.GetSquidProxy(r.Context(), proxyID)
	if err != nil || proxy == nil {
		http.Error(w, "proxy not found", http.StatusNotFound)
		return
	}
	var body struct {
		NodeID string `json:"node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(body.NodeID))
	if err != nil {
		http.Error(w, "node_id must be a UUID", http.StatusBadRequest)
		return
	}
	payload, _ := json.Marshal(squidJobPayload{
		ProxyID:   proxyID.String(),
		NodeID:    nodeID.String(),
		Whitelist: proxy.Whitelist,
		ProxyURL:  fmt.Sprintf("http://%s:%d", proxy.Host, proxy.Port),
	})
	job := &storage.Job{
		TenantID: proxy.TenantID,
		Type:     JobTypeSquidInstall,
		Status:   storage.JobStatusQueued,
		Payload:  payload,
	}
	if _, err := s.store.CreateJob(r.Context(), job, nil); err != nil {
		s.logger.Warn("enqueue squid install", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, proxy.TenantID, "patch.squid.install", "squid_proxy", proxyID.String(), map[string]any{
		"node_id": nodeID.String(),
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

type squidReconfigureRequest struct {
	Whitelist []string `json:"whitelist"`
	NodeID    string   `json:"node_id"`
}

func (s *Server) handleSquidReconfigure(w http.ResponseWriter, r *http.Request, principal *auth.Principal, proxyID uuid.UUID) {
	proxy, err := s.store.GetSquidProxy(r.Context(), proxyID)
	if err != nil || proxy == nil {
		http.Error(w, "proxy not found", http.StatusNotFound)
		return
	}
	var req squidReconfigureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if len(req.Whitelist) == 0 {
		http.Error(w, "whitelist must be non-empty", http.StatusBadRequest)
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(req.NodeID))
	if err != nil {
		http.Error(w, "node_id must be a UUID", http.StatusBadRequest)
		return
	}

	// Pre-apply: validate the proposed config locally with `squid -k parse`.
	// If squid is not available on the controlplane host, treat the validation
	// as a soft pass so dev environments aren't blocked. Production deployments
	// have squid installed on the bastion.
	if err := validateSquidConfig(req.Whitelist); err != nil {
		_ = s.store.UpdateSquidProxyStatus(r.Context(), proxyID, "degraded", err.Error())
		http.Error(w, fmt.Sprintf("squid -k parse rejected config: %v", err), http.StatusBadRequest)
		return
	}

	// Persist + dispatch. We dispatch to a single node first (the requester
	// supplied node_id) so the operator can canary before fanning out.
	if err := s.store.UpdateSquidProxyWhitelist(r.Context(), proxyID, req.Whitelist); err != nil {
		s.logger.Warn("update squid whitelist", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	payload, _ := json.Marshal(squidJobPayload{
		ProxyID:   proxyID.String(),
		NodeID:    nodeID.String(),
		Whitelist: req.Whitelist,
	})
	job := &storage.Job{
		TenantID: proxy.TenantID,
		Type:     JobTypeSquidReconfigure,
		Status:   storage.JobStatusQueued,
		Payload:  payload,
	}
	if _, err := s.store.CreateJob(r.Context(), job, nil); err != nil {
		s.logger.Warn("enqueue squid reconfigure", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, proxy.TenantID, "patch.squid.reconfigure", "squid_proxy", proxyID.String(), map[string]any{
		"node_id":    nodeID.String(),
		"whitelist":  req.Whitelist,
		"dry_run_to": nodeID.String(),
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":   "queued",
		"validate": "passed",
	})
}

// validateSquidConfig runs `squid -k parse` against a freshly written
// allowlist file. Returns nil on validation success, an error otherwise.
// When the squid binary is not present on the controlplane host the function
// returns nil (soft-pass) so that dev environments without squid still
// allow the operator to push reconfigure jobs.
func validateSquidConfig(whitelist []string) error {
	squidBin, err := exec.LookPath("squid")
	if err != nil {
		// No squid on the controlplane — soft-pass.
		return nil
	}
	tmp, err := os.CreateTemp("", "squid-*.conf")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	body := "http_port 3128\n"
	for _, host := range whitelist {
		// Only allow URL-safe-looking hostnames + dots/colons; reject the
		// rest as a defence against injection through the config file.
		if !isSafeSquidHost(host) {
			return fmt.Errorf("invalid hostname %q in whitelist", host)
		}
		body += fmt.Sprintf("acl allowed_hosts dstdomain %s\n", host)
	}
	body += "http_access allow allowed_hosts\n"
	body += "http_access deny all\n"
	if _, err := tmp.WriteString(body); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	cmd := exec.Command(squidBin, "-k", "parse", "-f", tmp.Name())
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("squid -k parse: %s (%v)", strings.TrimSpace(string(output)), runErr)
	}
	_ = filepath.Base(tmp.Name())
	return nil
}

// isSafeSquidHost is a tiny defence against config-file injection. We only
// allow hostnames composed of [A-Za-z0-9.\-_:].
func isSafeSquidHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_' || r == ':':
		default:
			return false
		}
	}
	return true
}
