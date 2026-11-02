package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type onlinePreparationStore interface {
	QueueOnlinePreparation(context.Context, uuid.UUID, uuid.UUID, patchsupport.OnlinePreparation) (*storage.Job, error)
	PendingOnlinePreparations(context.Context, uuid.UUID, uuid.UUID) ([]storage.Job, error)
	FinishOnlinePreparation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool, string, string, *patchsupport.OnlinePlan) error
}

// This stable snapshot excludes liveness/timestamps but includes proxy identity,
// repository ACL and managed policy revision. Preparation cannot silently switch
// to another proxy or fallback to direct acquisition.
func (s *Server) onlineTransportContext(ctx context.Context, node *storage.Node, mode string) (string, string, []string, error) {
	if node == nil || node.State != storage.NodeStateActive || (mode != "direct" && mode != "proxy") {
		return "", "", nil, fmt.Errorf("explicit direct or proxy mode required")
	}
	posture := nodeIsolationPostureFromNode(*node, time.Now().UTC())
	if posture.Active && posture.Mode == isolationModeAirgapped {
		return "", "", nil, fmt.Errorf("online preparation is unavailable while target isolation is airgapped")
	}
	if posture.Active && posture.Mode == isolationModeWhitelist && mode == "direct" && !stringSliceContainsFold(posture.AllowedApplications, "patch") {
		return "", "", nil, fmt.Errorf("target whitelist does not authorize direct package acquisition")
	}
	snapshot := patchsupport.OnlineTransport{NodeID: node.ID.String(), TenantID: node.TenantID.String(), Mode: mode}
	proxyURL := ""
	var hosts []string
	if mode == "proxy" {
		cfg, err := s.store.GetNodePatchConfig(ctx, node.ID)
		if err != nil || cfg == nil || cfg.ProxyID == nil {
			return "", "", nil, fmt.Errorf("save a repository proxy for this node before preparing a proxy plan")
		}
		proxy, err := s.store.GetSquidProxy(ctx, *cfg.ProxyID)
		if err != nil || proxy == nil || proxy.TenantID != node.TenantID || proxy.Status == "removed" {
			return "", "", nil, fmt.Errorf("repository proxy unavailable for this tenant")
		}
		proxyURL = fmt.Sprintf("http://%s:%d", proxy.Host, proxy.Port)
		if err = patchsupport.ProxyURL(proxyURL); err != nil {
			return "", "", nil, err
		}
		snapshot.ProxyID = proxy.ID.String()
		snapshot.ProxyURL = proxyURL
		snapshot.RepositoryHosts = proxy.Whitelist
		hosts = append([]string(nil), proxy.Whitelist...)
		if store, ok := s.store.(interface {
			GetProxyManagement(context.Context, uuid.UUID, uuid.UUID) (*storage.ProxyManagement, error)
		}); ok {
			policy, err := store.GetProxyManagement(ctx, node.TenantID, proxy.ID)
			if err != nil {
				return "", "", nil, err
			}
			if policy != nil {
				snapshot.ProxyNodeID = policy.NodeID.String()
				snapshot.Revision = policy.Revision
				snapshot.ClientCIDRs = policy.ClientCIDRs
				snapshot.ProbeURLs = policy.ProbeURLs
			}
		}
	}
	return snapshot.Digest(), proxyURL, hosts, nil
}

func (s *Server) handleOnlinePreparation(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", 503)
		return
	}
	var req struct {
		NodeID   string   `json:"node_id"`
		Mode     string   `json:"mode"`
		Packages []string `json:"packages"`
	}
	if r.Method == http.MethodGet {
		req.NodeID = r.URL.Query().Get("node_id")
		req.Mode = r.URL.Query().Get("mode")
	} else if r.Method == http.MethodPost {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
		dec.DisallowUnknownFields()
		if dec.Decode(&req) != nil {
			http.Error(w, "invalid online preparation", 400)
			return
		}
	} else {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	id, err := uuid.Parse(req.NodeID)
	if err != nil {
		http.Error(w, "node_id must be UUID", 400)
		return
	}
	node, err := s.store.GetNode(r.Context(), id)
	if err != nil || node == nil {
		http.Error(w, "target unavailable", 404)
		return
	}
	if principal.Type == "agent" {
		if r.Method != http.MethodGet || agentPrincipalNodeID(principal) != id {
			http.Error(w, "forbidden", 403)
			return
		}
	} else {
		roles := []string{roleViewer, roleOperator, roleAdmin}
		if r.Method == http.MethodPost {
			roles = []string{roleOperator, roleAdmin}
		}
		if !s.requireTenantAccess(w, r, principal, node.TenantID, roles...) {
			return
		}
	}
	store, ok := s.store.(onlinePreparationStore)
	if !ok {
		http.Error(w, "online preparation storage unavailable", 503)
		return
	}
	if r.Method == http.MethodGet {
		if req.Mode != "" {
			digest, proxy, hosts, err := s.onlineTransportContext(r.Context(), node, req.Mode)
			if err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			if planSHA := r.URL.Query().Get("plan_sha256"); planSHA != "" {
				if err = s.validateOnlineExecutionAuthorization(r.Context(), node, req.Mode, r.URL.Query().Get("job_id"), planSHA); err != nil {
					http.Error(w, err.Error(), 409)
					return
				}
			}
			writeJSON(w, 200, map[string]any{"transport_context": digest, "proxy_url": proxy, "repository_hosts": hosts})
			return
		}
		rawID := r.URL.Query().Get("job_id")
		if rawID == "" {
			rawID, _ = node.Labels["patch.online_preparation"].(string)
		}
		if rawID == "" {
			writeJSON(w, 200, map[string]any{"status": "idle"})
			return
		}
		jid, err := uuid.Parse(rawID)
		if err != nil {
			http.Error(w, "job_id must be UUID", 400)
			return
		}
		job, err := s.store.GetJob(r.Context(), jid)
		if err != nil || job == nil || job.TenantID != node.TenantID || job.Type != patchsupport.PrepareOnlineJob || jobPayloadNodeID(job.Payload) != id {
			http.Error(w, "preparation not found", 404)
			return
		}
		var p patchsupport.OnlinePreparation
		_ = json.Unmarshal(job.Payload, &p)
		if (job.Status == storage.JobStatusRunning || job.Status == storage.JobStatusQueued) && !p.ExpiresAt.After(time.Now().UTC()) {
			if store.FinishOnlinePreparation(r.Context(), node.TenantID, id, jid, false, "failed", "Online preparation timed out; inspect before preparing a fresh plan", nil) == nil {
				job, _ = s.store.GetJob(r.Context(), jid)
			}
		}
		if job == nil {
			http.Error(w, "preparation status unavailable", 503)
			return
		}
		events, _ := s.store.ListJobEvents(r.Context(), jid)
		message := ""
		if len(events) > 0 {
			message = events[len(events)-1].Message
		}
		writeJSON(w, 200, map[string]any{"job_id": jid, "status": job.Status, "message": message})
		return
	}
	if node.State != storage.NodeStateActive {
		http.Error(w, "active target required", 409)
		return
	}
	runtime, err := nodePatchRuntime(node, time.Now().UTC())
	if err != nil || runtime.OnlinePlanVersion != 1 || runtime.OS != "linux" || runtime.Manager != "apt" {
		http.Error(w, "current compatible exact-plan APT agent required", 409)
		return
	}
	digest, proxy, hosts, err := s.onlineTransportContext(r.Context(), node, req.Mode)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	sort.Strings(req.Packages)
	p := patchsupport.OnlinePreparation{RepositoryHosts: hosts, NodeID: id.String(), Mode: req.Mode, ProxyURL: proxy, TransportContext: digest, Packages: req.Packages, ExpiresAt: time.Now().UTC().Add(10 * time.Minute), RequestedBy: principal.Subject}
	if err = p.Validate(time.Now().UTC()); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	job, err := store.QueueOnlinePreparation(r.Context(), node.TenantID, id, p)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.recordAudit(r.Context(), principal, node.TenantID, "patch.online_plan.preparation_requested", "job", job.ID.String(), map[string]any{"node_id": id, "mode": p.Mode, "packages": p.Packages, "transport_context": p.TransportContext})
	writeJSON(w, 202, map[string]any{"job_id": job.ID, "status": job.Status, "message": "Exact online preparation queued; no installation authorized"})
}

func (s *Server) onlinePreparationActions(ctx context.Context, node *storage.Node) []string {
	store, ok := s.store.(onlinePreparationStore)
	if !ok || node == nil {
		return nil
	}
	jobs, err := store.PendingOnlinePreparations(ctx, node.TenantID, node.ID)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, job := range jobs {
		var p patchsupport.OnlinePreparation
		_ = json.Unmarshal(job.Payload, &p)
		digest, proxy, hosts, e := s.onlineTransportContext(ctx, node, p.Mode)
		runtime, re := nodePatchRuntime(node, time.Now().UTC())
		if node.State != storage.NodeStateActive || jobPayloadNodeID(job.Payload) != node.ID || p.Validate(time.Now().UTC()) != nil || e != nil || re != nil || runtime.OnlinePlanVersion != 1 || digest != p.TransportContext || proxy != p.ProxyURL || !reflect.DeepEqual(hosts, p.RepositoryHosts) {
			_ = store.FinishOnlinePreparation(ctx, node.TenantID, node.ID, job.ID, false, "failed", "Online preparation expired or transport/runtime changed; no installation authorized", nil)
			continue
		}
		out = append(out, patchsupport.PrepareOnlineJob+":"+job.ID.String())
	}
	return out
}

func (s *Server) completeOnlinePreparation(ctx context.Context, nodeID uuid.UUID, c heartbeatCompletedAction) {
	if c.Status != "succeeded" && c.Status != "failed" {
		return
	}
	id, err := uuid.Parse(c.JobID)
	if err != nil {
		return
	}
	job, err := s.store.GetJob(ctx, id)
	if err != nil || job == nil || job.Type != patchsupport.PrepareOnlineJob || jobPayloadNodeID(job.Payload) != nodeID {
		return
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil || node == nil || node.TenantID != job.TenantID {
		return
	}
	store, ok := s.store.(onlinePreparationStore)
	if !ok {
		return
	}
	message := c.Error
	var plan *patchsupport.OnlinePlan
	if c.Status == "succeeded" {
		var p patchsupport.OnlinePreparation
		_ = json.Unmarshal(job.Payload, &p)
		digest, proxy, hosts, e := s.onlineTransportContext(ctx, node, p.Mode)
		body, _ := json.Marshal(c.Metadata["online_plan"])
		candidate := &patchsupport.OnlinePlan{}
		if e == nil && digest == p.TransportContext && proxy == p.ProxyURL && reflect.DeepEqual(hosts, p.RepositoryHosts) && json.Unmarshal(body, candidate) == nil && candidate.Matches(p) && candidate.Validate(time.Now().UTC()) == nil {
			plan = candidate
			message = "Exact online plan prepared and packages acquired; no packages installed"
		} else {
			message = "Prepared result is invalid or transport changed; prepare again; no installation authorized"
		}
	}
	_ = store.FinishOnlinePreparation(ctx, job.TenantID, nodeID, id, true, c.Status, message, plan)
}

type onlinePlanEvidence struct {
	Status     string                  `json:"status"`
	Plan       patchsupport.OnlinePlan `json:"plan"`
	JobID      string                  `json:"job_id"`
	ObservedAt time.Time               `json:"observed_at"`
}

func (s *Server) validateOnlinePolicy(ctx context.Context, node *storage.Node, mode string, policy patchPackagePolicy, now time.Time) error {
	runtime, err := nodePatchRuntime(node, now)
	if err != nil {
		return err
	}
	if policy.OnlinePlan == nil {
		if runtime.OnlinePlanVersion == 1 && (mode == "direct" || mode == "proxy") {
			return fmt.Errorf("prepare and select an exact online plan before requesting this APT deployment")
		}
		return nil
	}
	p := policy.OnlinePlan
	if runtime.OnlinePlanVersion != 1 || runtime.OS != "linux" || runtime.Manager != "apt" || p.NodeID != node.ID.String() || p.Mode != mode || p.Validate(now) != nil || len(policy.Denylist) > 0 || !onlineScopeMatches(p, policy.Allowlist, policy.Denylist) {
		return fmt.Errorf("online plan does not match current runtime, target, mode, expiry or package scope")
	}
	digest, proxy, hosts, err := s.onlineTransportContext(ctx, node, mode)
	if err != nil {
		return err
	}
	if p.TransportContext != digest || p.ProxyURL != proxy || !reflect.DeepEqual(hosts, p.RepositoryHosts) {
		return fmt.Errorf("online transport/proxy policy changed; prepare and approve a new plan")
	}
	var evidence onlinePlanEvidence
	body, _ := json.Marshal(node.Labels["patch.online_plan"])
	if json.Unmarshal(body, &evidence) != nil || evidence.Status != "ready" || evidence.JobID == "" || evidence.ObservedAt.IsZero() || evidence.ObservedAt.After(now) || now.Sub(evidence.ObservedAt) > 5*time.Minute || !reflect.DeepEqual(evidence.Plan, *p) {
		return fmt.Errorf("fresh agent-published exact online plan required; refresh or prepare again")
	}
	return nil
}

func onlineScopeMatches(p *patchsupport.OnlinePlan, allow, deny []string) bool {
	if p == nil || len(deny) > 0 {
		return false
	}
	copy := append([]string(nil), allow...)
	sort.Strings(copy)
	return reflect.DeepEqual(copy, p.Requested)
}

// Only refresh a plan whose successful preparation receipt is already stored.
// A heartbeat cannot inject an unrelated plan or make a withdrawn baseline current.
func (s *Server) updateOnlinePlanReport(ctx context.Context, node *storage.Node, report *patchsupport.OnlinePlanReport) (*storage.Node, error) {
	if report == nil || node == nil {
		return node, nil
	}
	var evidence onlinePlanEvidence
	body, _ := json.Marshal(node.Labels["patch.online_plan"])
	if json.Unmarshal(body, &evidence) != nil || evidence.JobID == "" {
		return node, nil
	}
	evidence.Status = "unavailable"
	evidence.ObservedAt = time.Now().UTC()
	if report.Status == "ready" && report.Plan != nil && report.Plan.Validate(time.Now().UTC()) == nil && reflect.DeepEqual(evidence.Plan, *report.Plan) {
		evidence.Status = "ready"
	}
	labels := map[string]any{}
	for k, v := range node.Labels {
		labels[k] = v
	}
	labels["patch.online_plan"] = evidence
	store, ok := s.store.(interface {
		RefreshOnlinePlanReport(context.Context, uuid.UUID, uuid.UUID, string, string) error
	})
	if !ok {
		return node, nil
	}
	if err := store.RefreshOnlinePlanReport(ctx, node.TenantID, node.ID, evidence.Plan.SHA256, evidence.Status); err != nil {
		return node, err
	}
	copy := *node
	copy.Labels = labels
	return &copy, nil
}

// Rechecked by the APT hook under the package-manager lock. A policy change
// during lock waiting cannot reuse a previously valid approval or transport.
func (s *Server) validateOnlineExecutionAuthorization(ctx context.Context, node *storage.Node, mode, jobID, planSHA string) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return fmt.Errorf("authorized execution job required")
	}
	job, err := s.store.GetJob(ctx, id)
	if err != nil || job == nil || job.TenantID != node.TenantID || job.Type != "patch.deploy_"+mode || (job.Status != storage.JobStatusRunning && job.Status != storage.JobStatusQueued) || jobPayloadNodeID(job.Payload) != node.ID {
		return fmt.Errorf("online execution job is not current for this node")
	}
	var payload patchJobPayload
	if json.Unmarshal(job.Payload, &payload) != nil || payload.Mode != mode || payload.OnlinePlan == nil || payload.OnlinePlan.SHA256 != planSHA || payload.NodeID != node.ID.String() || payload.ProxyURL != payload.OnlinePlan.ProxyURL {
		return fmt.Errorf("online execution plan/job mismatch")
	}
	if payload.AutomationRunID != "" {
		runID, e := uuid.Parse(payload.AutomationRunID)
		if e != nil {
			return fmt.Errorf("invalid automation authorization")
		}
		store, ok := s.store.(interface {
			CheckPatchAutomationExecution(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
		})
		if !ok {
			return fmt.Errorf("automation authorization unavailable")
		}
		if e = store.CheckPatchAutomationExecution(ctx, node.TenantID, runID, id); e != nil {
			return e
		}
	}
	currentRuntime, rerr := nodePatchRuntime(node, time.Now().UTC())
	if rerr != nil || payload.Runtime == nil || *payload.Runtime != currentRuntime {
		return fmt.Errorf("online job runtime changed before execution")
	}
	state, err := s.store.GetNodePatchStateByJobID(ctx, id)
	if err != nil || state == nil || state.JobID == nil || *state.JobID != id || state.NodeID != node.ID || state.TenantID != node.TenantID || state.Status != "pending" || state.ID.String() != payload.NodePatchStateID || state.DeploymentID.String() != payload.DeploymentID {
		return fmt.Errorf("online execution state link is unavailable or terminal")
	}
	deployment, err := s.store.GetPatchDeployment(ctx, state.DeploymentID)
	if err != nil || deployment == nil || deployment.TenantID != node.TenantID {
		return fmt.Errorf("reviewed deployment unavailable")
	}
	policy := patchPackagePolicyFromSummary(deployment.Summary)
	if !reflect.DeepEqual(policy.OnlinePlan, payload.OnlinePlan) || !onlineScopeMatches(payload.OnlinePlan, payload.PackageAllowlist, payload.PackageDenylist) || !payload.PostPatchRescan {
		return fmt.Errorf("online job scope differs from approved deployment")
	}
	if err = s.validateOnlinePolicy(ctx, node, mode, policy, time.Now().UTC()); err != nil {
		return err
	}
	approvals, _, err := s.store.ListPatchApprovals(ctx, storage.ListPatchApprovalsFilter{TenantID: node.TenantID, DeploymentID: state.DeploymentID, NodeID: node.ID, Status: storage.ApprovalStatusApproved}, 1, 0)
	if err != nil || len(approvals) != 1 {
		return fmt.Errorf("current operator approval unavailable")
	}
	approval := approvals[0]
	cfg, cfgErr := s.store.GetNodePatchConfig(ctx, node.ID)
	if cfgErr != nil {
		return cfgErr
	}
	var currentWindow *uuid.UUID
	if cfg != nil {
		currentWindow = cfg.WindowID
	}
	if !reflect.DeepEqual(currentWindow, approval.WindowID) {
		return fmt.Errorf("node calendar association changed; request approval again")
	}
	if approval.WindowID == nil && payload.Window != nil {
		return fmt.Errorf("online job calendar scope differs from approval")
	}
	if approval.WindowID != nil {
		record, e := s.store.GetMaintenanceWindow(ctx, *approval.WindowID)
		if e != nil || record == nil || payload.Window == nil || payload.Window.ID != record.ID.String() || !payload.Window.OpensAt.Equal(record.OpensAt) || !payload.Window.ClosesAt.Equal(record.ClosesAt) {
			return fmt.Errorf("online calendar bounds changed before execution")
		}
	}
	hash, _, err := s.patchApprovalPolicyHash(ctx, node.TenantID, node.ID, mode, approval.ProxyID, approval.WindowID, policy)
	if err != nil || approval.Status != storage.ApprovalStatusApproved || approval.Mode != mode || !approval.ExpiresAt.After(time.Now().UTC()) || hash != approval.PolicyHash {
		return fmt.Errorf("online approval changed or expired before package mutation")
	}
	gate := s.previewPatchSafetyGates(ctx, node.TenantID, node.ID, mode, approval.WindowID)
	if !gate.Allowed && !gate.AwaitingApproval {
		return fmt.Errorf("online execution safety gate changed: %s", gate.Reason)
	}
	return nil
}
