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
}

type heartbeatResponse struct {
	NodeID         string                      `json:"node_id"`
	State          string                      `json:"state"`
	LastSeenAt     string                      `json:"last_seen_at"`
	Activated      bool                        `json:"activated"`
	Reason         *string                     `json:"reason,omitempty"`
	EventFilters   *storage.TenantEventFilters `json:"event_filters,omitempty"`
	PendingActions []string                    `json:"pending_actions,omitempty"`
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

	// Body is optional but if provided must decode cleanly.
	var body heartbeatRequest
	if r.ContentLength > 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
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

	activated, resultState := s.maybeActivatePendingNode(r.Context(), node)

	lastSeen := time.Now().UTC().Format(time.RFC3339)
	if node.LastSeenAt != nil {
		lastSeen = node.LastSeenAt.UTC().Format(time.RFC3339)
	}

	resp := heartbeatResponse{
		NodeID:     node.ID.String(),
		State:      resultState,
		LastSeenAt: lastSeen,
		Activated:  activated,
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
		resp.PendingActions = []string{"agent_update"}
		// Transition job to running so it won't appear again on the next heartbeat
		// until the agent confirms completion (or the job times out).
		_ = s.store.UpdateJobStatus(r.Context(), pendingJob.ID, storage.JobStatusRunning, "agent notified via heartbeat", nil)
	}
	writeJSON(w, http.StatusOK, resp)
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
