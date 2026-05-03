package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// enrollmentPendingTimeout controls how long a node may sit in
// enrollment_pending before the reaper flips it to enrollment_failed.
// Exported (lowercase but package-visible) so tests can override it to keep
// runtimes short — the production default is 10 minutes.
const enrollmentPendingTimeout = 10 * time.Minute

// reaperScanInterval is how often the pending-enrollment reaper wakes up to
// look for stale rows. 1m keeps the failure latency reasonable while leaving
// ample headroom above the 10m timeout.
const reaperScanInterval = time.Minute

// webhook event types emitted by the heartbeat + reaper.
const (
	EventEnrollmentCompleted = "enrollment.completed"
	EventEnrollmentTimedOut  = "enrollment.timed_out"
)

// heartbeatRequest is the body of POST /api/v1/nodes/:id/heartbeat.
// Everything is optional — the essential signal is that the agent called us,
// using a client cert whose CN matches the node id.
type heartbeatRequest struct {
	AgentVersion string `json:"agent_version"`

	// Inventory + firewall snapshot fields. Optional. Old agents send none of
	// these; new agents send firewall every heartbeat and os_packages only
	// when the hash changed, 24h elapsed, or the server requested a full
	// inventory on the previous response.
	OSPackages    []heartbeatPackage      `json:"os_packages,omitempty"`
	PackageHash   string                  `json:"package_hash,omitempty"`
	PackageCount  int                     `json:"package_count,omitempty"`
	KernelVersion string                  `json:"kernel_version,omitempty"`
	OSVersion     string                  `json:"os_version,omitempty"`
	FirewallState *heartbeatFirewallState `json:"firewall_state,omitempty"`

	// CompletedActions reports the outcome of pending_actions the agent
	// processed since the last heartbeat. Only firewall.* actions use it
	// today; agent_update has its own retirement path.
	CompletedActions []heartbeatCompletedAction `json:"completed_actions,omitempty"`
}

// heartbeatCompletedAction is one row in completed_actions[]. Status is
// one of "succeeded" | "failed". For failed, Error is the agent-side
// reason; surfaces back to the operator UI.
type heartbeatCompletedAction struct {
	Action string `json:"action"`
	JobID  string `json:"job_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// Action-specific metadata. Populated by the agent for actions where
	// summary data matters (e.g. patch.deploy_direct: packages_upgraded,
	// log_tail). Empty for actions that need no payload back.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// heartbeatPackage is the per-package payload entry the agent sends.
type heartbeatPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	Arch        string `json:"arch,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
}

// heartbeatFirewallState mirrors the agent-side payload. Translated into
// storage.NodeFirewallState before persisting.
type heartbeatFirewallState struct {
	Type    string                 `json:"type"`
	Enabled bool                   `json:"enabled"`
	Rules   []storage.FirewallRule `json:"rules,omitempty"`
	Zones   []storage.FirewallZone `json:"zones,omitempty"`
}

type heartbeatResponse struct {
	NodeID                 string                      `json:"node_id"`
	State                  string                      `json:"state"`
	LastSeenAt             string                      `json:"last_seen_at"`
	Activated              bool                        `json:"activated"`
	Reason                 *string                     `json:"reason,omitempty"`
	EventFilters           *storage.TenantEventFilters `json:"event_filters,omitempty"`
	PendingActions         []string                    `json:"pending_actions,omitempty"`
	FullInventoryRequested bool                        `json:"full_inventory_requested,omitempty"`
}

// handleNodeHeartbeat is the mTLS endpoint the agent hits every heartbeat
// interval (default 60s). CN-vs-URL-id validation is enforced so a compromised
// agent cert can only poke its own node. A successful call:
//  1. bumps nodes.last_seen_at
//  2. if state=enrollment_pending AND first_scan_at is set, flips -> active
//     and emits enrollment.completed.
//  3. otherwise leaves the state alone.
func (s *Server) handleNodeHeartbeat(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if principal.Type != "agent" {
		// Heartbeat MUST come from a mTLS-authenticated agent, never a
		// bearer-token operator — even an admin can't forge heartbeats.
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	// The agent middleware stores the cert CN in principal.Name. Enforce
	// that it matches the URL-scoped node id — otherwise node-A's cert
	// can't be used to touch node-B.
	cn := strings.TrimSpace(principal.Name)
	if cn == "" || !strings.EqualFold(cn, nodeID.String()) {
		s.logger.Warn("heartbeat CN mismatch",
			zap.String("cn", cn),
			zap.String("node_id", nodeID.String()),
		)
		http.Error(w, "client cert CN does not match node id", http.StatusForbidden)
		return
	}

	// Body is optional but if provided must decode. We deliberately do NOT
	// use DisallowUnknownFields here — newer agents may send fields older
	// servers don't know about, and rolling deployments need that to be
	// tolerated rather than rejected with 400.
	var body heartbeatRequest
	if r.ContentLength > 0 {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}

	node, err := s.store.TouchNodeHeartbeat(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("touch heartbeat", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Persist agent version so operators can see it in the nodes table.
	if body.AgentVersion != "" {
		if verr := s.store.UpdateNodeAgentVersion(r.Context(), nodeID, body.AgentVersion); verr != nil {
			s.logger.Warn("update agent version", zap.Error(verr))
		}
	}

	fullInventoryRequested := s.processHeartbeatInventory(r.Context(), nodeID, body)
	s.processHeartbeatFirewall(r.Context(), nodeID, body)
	s.processHeartbeatCompletedActions(r.Context(), nodeID, body.CompletedActions)

	activated, resultState := s.maybeActivatePendingNode(r.Context(), node)

	lastSeen := time.Now().UTC().Format(time.RFC3339)
	if node.LastSeenAt != nil {
		lastSeen = node.LastSeenAt.UTC().Format(time.RFC3339)
	}

	resp := heartbeatResponse{
		NodeID:                 node.ID.String(),
		State:                  resultState,
		LastSeenAt:             lastSeen,
		Activated:              activated,
		FullInventoryRequested: fullInventoryRequested,
	}
	// Deliver tenant capture-filter policy back to the agent so collectors
	// hot-reload without a restart. Storage layer returns defaults when no
	// row exists (Phase 5 contract). Errors are logged + ignored — heartbeat
	// is too important to fail on a policy lookup blip.
	if filters, ferr := s.store.GetTenantEventFilters(r.Context(), node.TenantID); ferr == nil && filters != nil {
		resp.EventFilters = filters
	} else if ferr != nil {
		s.logger.Warn("get tenant event filters during heartbeat",
			zap.String("tenant_id", node.TenantID.String()),
			zap.Error(ferr))
	}
	if resultState == storage.NodeStateEnrollmentPending {
		// Surface which gate is still pending so the agent can log usefully.
		var reason string
		if node.FirstScanAt == nil {
			reason = "awaiting first compliance scan"
		} else {
			reason = "enrollment gate still pending"
		}
		resp.Reason = &reason
	}
	// Check for queued self-update and signal the agent.
	if pendingJob, jerr := s.store.GetPendingAgentUpdateJob(r.Context(), nodeID); jerr == nil && pendingJob != nil {
		resp.PendingActions = append(resp.PendingActions, "agent_update")
		// Transition job to running so it won't appear again on the next heartbeat
		// until the agent confirms completion (or the job times out).
		_ = s.store.UpdateJobStatus(r.Context(), pendingJob.ID, storage.JobStatusRunning, "agent notified via heartbeat", nil)
	}
	// Append pending firewall actions (PR 3). Each is encoded as
	// "<job_type>:<job_id>" so the agent can dispatch in one switch and so
	// completed_actions[] echo back the same job_id.
	if pending, ferr := s.store.ListPendingNodeFirewallRules(r.Context(), nodeID); ferr == nil {
		for _, rule := range pending {
			if rule.JobID == nil {
				continue
			}
			actionType := JobTypeFirewallRuleAdd
			if rule.Action == "allow" {
				actionType = JobTypeFirewallRuleDelete
			}
			resp.PendingActions = append(resp.PendingActions, actionType+":"+rule.JobID.String())
			// Mark the job Running so the worker view reflects in-flight state.
			_ = s.store.UpdateJobStatus(r.Context(), *rule.JobID, storage.JobStatusRunning, "agent notified via heartbeat", nil)
		}
	} else if !errors.Is(ferr, sql.ErrNoRows) {
		s.logger.Warn("list pending firewall rules", zap.Error(ferr))
	}
	// Append pending patch actions (PR 4). Same encoding as firewall.* —
	// "patch.deploy_direct:<job_id>".
	if pending, perr := s.store.ListPendingNodePatchStates(r.Context(), nodeID); perr == nil {
		for _, ps := range pending {
			if ps.JobID == nil {
				continue
			}
			resp.PendingActions = append(resp.PendingActions, JobTypePatchDeployDirect+":"+ps.JobID.String())
			_ = s.store.UpdateJobStatus(r.Context(), *ps.JobID, storage.JobStatusRunning, "agent notified via heartbeat", nil)
		}
	} else if !errors.Is(perr, sql.ErrNoRows) {
		s.logger.Warn("list pending patch states", zap.Error(perr))
	}
	writeJSON(w, http.StatusOK, resp)
}

// processHeartbeatCompletedActions reads agent-reported outcomes for actions
// dispatched on previous heartbeats. Currently only firewall.rule_add /
// firewall.rule_delete use this — agent_update has its own retirement path.
func (s *Server) processHeartbeatCompletedActions(ctx context.Context, _ uuid.UUID, completed []heartbeatCompletedAction) {
	if len(completed) == 0 {
		return
	}
	for _, c := range completed {
		jobID, err := uuid.Parse(strings.TrimSpace(c.JobID))
		if err != nil {
			s.logger.Warn("completed_action with invalid job_id", zap.String("job_id", c.JobID))
			continue
		}
		switch c.Action {
		case JobTypeFirewallRuleAdd, JobTypeFirewallRuleDelete:
			rule, rerr := s.store.GetNodeFirewallRuleByJobID(ctx, jobID)
			if rerr != nil {
				s.logger.Warn("get firewall rule by job id", zap.Error(rerr))
				continue
			}
			if rule == nil {
				continue
			}
			if c.Status == "succeeded" {
				// Apply or remove — record in the same row.
				if c.Action == JobTypeFirewallRuleDelete {
					_ = s.store.MarkNodeFirewallRuleRemoved(ctx, rule.ID)
				} else {
					_ = s.store.MarkNodeFirewallRuleApplied(ctx, rule.ID)
				}
				_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "agent reported success", nil)
			} else {
				errMsg := strings.TrimSpace(c.Error)
				if errMsg == "" {
					errMsg = "agent reported failure"
				}
				_ = s.store.MarkNodeFirewallRuleFailed(ctx, rule.ID, errMsg)
				_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, errMsg, map[string]any{"error": errMsg})
			}
		case JobTypePatchDeployDirect:
			ps, perr := s.store.GetNodePatchStateByJobID(ctx, jobID)
			if perr != nil {
				s.logger.Warn("get patch state by job id", zap.Error(perr))
				continue
			}
			if ps == nil {
				continue
			}
			logTail, _ := c.Metadata["log_tail"].(string)
			pkgsUpgraded := metadataInt(c.Metadata, "packages_upgraded")
			if c.Status == "succeeded" {
				_ = s.store.MarkNodePatchApplied(ctx, ps.ID, pkgsUpgraded, logTail)
				_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "agent reported success", map[string]any{
					"packages_upgraded": pkgsUpgraded,
				})
			} else {
				errMsg := strings.TrimSpace(c.Error)
				if errMsg == "" {
					errMsg = "agent reported failure"
				}
				_ = s.store.MarkNodePatchFailed(ctx, ps.ID, errMsg, logTail)
				_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, errMsg, map[string]any{"error": errMsg})
			}
			// Roll up the parent deployment if every node has finished.
			s.maybeRollupPatchDeployment(ctx, ps.DeploymentID)
		default:
			// Ignore unknown actions (forward-compat with future action types).
			continue
		}
	}
}

// metadataInt is a tiny helper for plucking JSON-decoded numbers out of a
// map[string]any. agent-reported metadata is encoded by encoding/json so
// numeric fields land as float64.
func metadataInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

// maybeRollupPatchDeployment flips a deployment to completed/partial/failed
// once every node has reported. Safe to call on every node completion —
// it's a no-op while pendings remain.
func (s *Server) maybeRollupPatchDeployment(ctx context.Context, deploymentID uuid.UUID) {
	rows, err := s.store.ListNodePatchStatesForDeployment(ctx, deploymentID)
	if err != nil {
		s.logger.Warn("rollup patch deployment", zap.Error(err))
		return
	}
	pending, applied, failed := 0, 0, 0
	for _, r := range rows {
		switch r.Status {
		case "pending":
			pending++
		case "applied":
			applied++
		case "failed":
			failed++
		}
	}
	if pending > 0 {
		return
	}
	status := "completed"
	switch {
	case applied == 0 && failed > 0:
		status = "failed"
	case failed > 0:
		status = "partial"
	}
	_ = s.store.UpdatePatchDeploymentStatus(ctx, deploymentID, status, true)
}

// fullInventoryRefreshInterval bounds how stale the server's package
// inventory may be before we ask the agent for a fresh full sync. Mirrors
// the agent-side cap so neither side drifts independently.
const fullInventoryRefreshInterval = 24 * time.Hour

// processHeartbeatInventory persists package inventory updates and returns
// true when the response should signal full_inventory_requested. The agent
// sends:
//   - body.OSPackages populated (full sync) — server replaces the table and
//     records the new hash
//   - body.PackageHash only (delta) — server compares to stored hash; if it
//     matches, just touches last_seen_at; if it doesn't match (or no record),
//     returns true so the agent resends a full list next heartbeat.
//
// If the agent didn't send any inventory fields (old agent or unsupported
// platform), this is a no-op.
func (s *Server) processHeartbeatInventory(ctx context.Context, nodeID uuid.UUID, body heartbeatRequest) bool {
	if body.PackageHash == "" {
		return false
	}

	// Full sync path.
	if len(body.OSPackages) > 0 {
		pkgs := make([]storage.NodePackage, 0, len(body.OSPackages))
		for _, p := range body.OSPackages {
			np := storage.NodePackage{
				NodeID:  nodeID,
				Name:    p.Name,
				Version: p.Version,
				Source:  p.Source,
			}
			if p.Arch != "" {
				arch := p.Arch
				np.Arch = &arch
			}
			if p.InstalledAt != "" {
				if t, err := time.Parse(time.RFC3339, p.InstalledAt); err == nil {
					np.InstalledAt = &t
				}
			}
			pkgs = append(pkgs, np)
		}
		if err := s.store.ReplaceNodePackages(ctx, nodeID, pkgs); err != nil {
			s.logger.Warn("replace node packages", zap.Error(err), zap.String("node_id", nodeID.String()))
			return true // ask for a fresh resend on the next heartbeat
		}
		sync := storage.NodeInventorySync{
			NodeID:       nodeID,
			PackageHash:  body.PackageHash,
			PackageCount: body.PackageCount,
			LastFullSync: time.Now().UTC(),
			LastSeenAt:   time.Now().UTC(),
		}
		if body.KernelVersion != "" {
			k := body.KernelVersion
			sync.KernelVersion = &k
		}
		if body.OSVersion != "" {
			o := body.OSVersion
			sync.OSVersion = &o
		}
		if err := s.store.UpsertNodeInventorySync(ctx, sync); err != nil {
			s.logger.Warn("upsert node inventory sync", zap.Error(err))
		}
		return false
	}

	// Hash-only delta path.
	rows, err := s.store.TouchNodeInventorySync(ctx, nodeID, body.PackageHash)
	if err != nil {
		s.logger.Warn("touch node inventory sync", zap.Error(err))
		return true
	}
	if rows == 0 {
		// Either no record exists or the hash diverged. Either way, ask for
		// a full resend on the next heartbeat.
		return true
	}

	// Hash matched. Cap server-side staleness at fullInventoryRefreshInterval.
	if existing, gerr := s.store.GetNodeInventorySync(ctx, nodeID); gerr == nil && existing != nil {
		if time.Since(existing.LastFullSync) >= fullInventoryRefreshInterval {
			return true
		}
	}
	return false
}

// processHeartbeatFirewall persists the firewall snapshot in full each time.
// No delta logic — payload is small and changes are operationally significant.
func (s *Server) processHeartbeatFirewall(ctx context.Context, nodeID uuid.UUID, body heartbeatRequest) {
	if body.FirewallState == nil {
		return
	}
	st := storage.NodeFirewallState{
		NodeID:       nodeID,
		FirewallType: body.FirewallState.Type,
		Enabled:      body.FirewallState.Enabled,
		Rules:        body.FirewallState.Rules,
		Zones:        body.FirewallState.Zones,
		ObservedAt:   time.Now().UTC(),
	}
	if err := s.store.UpsertNodeFirewallState(ctx, st); err != nil {
		s.logger.Warn("upsert node firewall state", zap.Error(err), zap.String("node_id", nodeID.String()))
	}
}

// maybeActivatePendingNode encapsulates the "is the enrollment gate closed?"
// logic shared by the heartbeat handler and the compliance first-scan hook.
// If `node.State == enrollment_pending` and both last_seen_at + first_scan_at
// are non-nil, it transitions the node to active and emits the
// enrollment.completed webhook. Returns (activated, effectiveState). The
// effective state is what the caller should report back.
func (s *Server) maybeActivatePendingNode(ctx context.Context, node *storage.Node) (bool, string) {
	if node == nil {
		return false, ""
	}
	if node.State != storage.NodeStateEnrollmentPending {
		return false, node.State
	}
	if node.LastSeenAt == nil || node.FirstScanAt == nil {
		return false, node.State
	}

	if err := s.store.SetNodeState(ctx, node.ID, storage.NodeStateActive); err != nil {
		s.logger.Error("activate pending node", zap.Error(err), zap.String("node_id", node.ID.String()))
		return false, node.State
	}

	payload := map[string]any{
		"node_id":       node.ID.String(),
		"tenant_id":     node.TenantID.String(),
		"hostname":      node.Hostname,
		"first_scan_at": node.FirstScanAt.UTC().Format(time.RFC3339),
		"last_seen_at":  node.LastSeenAt.UTC().Format(time.RFC3339),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}
	go s.emitEnrollmentWebhook(context.Background(), node.TenantID, EventEnrollmentCompleted, payload)

	return true, storage.NodeStateActive
}

// emitEnrollmentWebhook fans an enrollment event out to every webhook
// subscribed to the given event type. Deliveries are best-effort — we log
// failures but never surface them back to the caller, consistent with the
// compliance event emitter pattern.
func (s *Server) emitEnrollmentWebhook(ctx context.Context, tenantID uuid.UUID, eventType string, payload map[string]any) {
	if s.store == nil {
		return
	}
	webhooks, err := s.store.ListWebhooksByEvent(ctx, tenantID, eventType)
	if err != nil {
		s.logger.Warn("list webhooks for enrollment event", zap.String("event_type", eventType), zap.Error(err))
		return
	}
	for i := range webhooks {
		wh := webhooks[i]
		go s.deliverAndRecordCompliance(ctx, &wh, eventType, payload)
	}
}

// --- reaper -----------------------------------------------------------------

// enrollmentReaperState owns the channel used to stop the reaper goroutine.
// The reaper is started by Server.Start() and torn down by Server.Stop(), so
// request-path code never spawns background work — that used to race with
// test mutations of the in-memory fake store.
type enrollmentReaperState struct {
	stopCh chan struct{}
}

// pendingReaper is initialised on first use. Tests that need deterministic
// control can replace s.reaperNow.
func (s *Server) reaperNow() time.Time {
	if s.clockOverride != nil {
		return s.clockOverride()
	}
	return time.Now().UTC()
}

// startEnrollmentReaper spins up the background loop that flips stale
// enrollment_pending rows to enrollment_failed. Called once from Server.Start.
// Safe to call again after stopEnrollmentReaper — a fresh stopCh is allocated.
func (s *Server) startEnrollmentReaper() {
	if s.enrollmentReaper.stopCh != nil {
		select {
		case <-s.enrollmentReaper.stopCh:
			// previous stopCh was closed; fall through to reallocate
		default:
			return // already running
		}
	}
	s.enrollmentReaper.stopCh = make(chan struct{})
	go s.runEnrollmentPendingReaper(s.enrollmentReaper.stopCh)
}

// stopEnrollmentReaper halts the reaper goroutine. Safe to call when the
// reaper was never started (no-op in that case).
func (s *Server) stopEnrollmentReaper() {
	if s.enrollmentReaper.stopCh == nil {
		return
	}
	select {
	case <-s.enrollmentReaper.stopCh:
		// already stopped
	default:
		close(s.enrollmentReaper.stopCh)
	}
}

// runEnrollmentPendingReaper is the background loop. It wakes on
// reaperScanInterval, asks the store for any pending rows older than the
// timeout, and flips each one to enrollment_failed + emits a webhook.
func (s *Server) runEnrollmentPendingReaper(stop <-chan struct{}) {
	ticker := time.NewTicker(reaperScanInterval)
	defer ticker.Stop()

	// Drain immediately on start so nodes created with backdated timestamps
	// don't need to wait a full interval to time out.
	s.reapPendingEnrollments()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.reapPendingEnrollments()
		}
	}
}

// reapPendingEnrollments does a single reaper pass. Isolated from the loop so
// tests can drive it deterministically.
func (s *Server) reapPendingEnrollments() {
	if s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cutoff := s.reaperNow().Add(-enrollmentPendingTimeout)
	stale, err := s.store.ListEnrollmentPendingNodesOlderThan(ctx, cutoff)
	if err != nil {
		s.logger.Warn("list stale pending nodes", zap.Error(err))
		return
	}
	for i := range stale {
		node := stale[i]
		if err := s.store.SetNodeState(ctx, node.ID, storage.NodeStateEnrollmentFailed); err != nil {
			s.logger.Warn("flip node to enrollment_failed",
				zap.String("node_id", node.ID.String()),
				zap.Error(err),
			)
			continue
		}
		s.logger.Info("enrollment timed out",
			zap.String("node_id", node.ID.String()),
			zap.String("tenant_id", node.TenantID.String()),
			zap.String("hostname", node.Hostname),
		)
		payload := map[string]any{
			"node_id":    node.ID.String(),
			"tenant_id":  node.TenantID.String(),
			"hostname":   node.Hostname,
			"created_at": node.CreatedAt.UTC().Format(time.RFC3339),
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
			"reason":     "heartbeat + first scan not received within timeout",
		}
		s.emitEnrollmentWebhook(ctx, node.TenantID, EventEnrollmentTimedOut, payload)
	}
}
