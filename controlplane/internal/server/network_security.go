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

// Job type constants for the firewall enforcement pipeline. The agent is
// driven by these via heartbeat PendingActions; the control plane records
// outcomes via heartbeat completed_actions.
const (
	// JobTypeFirewallRuleAdd dispatches an Apply on the agent's firewall.Manager.
	JobTypeFirewallRuleAdd = "firewall.rule_add"
	// JobTypeFirewallRuleDelete dispatches a Remove on the agent's firewall.Manager.
	JobTypeFirewallRuleDelete = "firewall.rule_delete"
)

// firewallJobPayload is the payload shape for both add + delete jobs. The
// agent receives this via the heartbeat dispatch (encoded into PendingActions
// as `firewall.rule_add:<job_id>`) and reads the live row from the
// node_firewall_rules table on the back-channel — but we also embed the
// effective rule fields in the job payload for log/audit purposes and so
// the executor can run from the job alone if needed.
type firewallJobPayload struct {
	NodeFirewallRuleID string `json:"node_firewall_rule_id"`
	NodeID             string `json:"node_id"`
	EntityActionID     string `json:"entity_action_id"`
	ActionPlanID       string `json:"action_plan_id,omitempty"`
	Action             string `json:"action"`
	Direction          string `json:"direction"`
	Source             string `json:"source,omitempty"`
	Dest               string `json:"dest,omitempty"`
	Port               int    `json:"port,omitempty"`
	Protocol           string `json:"protocol,omitempty"`
	Tag                string `json:"tag"`
	TTLSeconds         *int   `json:"ttl_seconds,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

func decodeFirewallPayload(raw json.RawMessage) (any, error) {
	var p firewallJobPayload
	if len(raw) == 0 {
		return nil, errors.New("firewall payload required")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid firewall payload: %w", err)
	}
	if strings.TrimSpace(p.NodeFirewallRuleID) == "" {
		return nil, errors.New("node_firewall_rule_id required")
	}
	if _, err := uuid.Parse(p.NodeFirewallRuleID); err != nil {
		return nil, fmt.Errorf("node_firewall_rule_id must be UUID: %w", err)
	}
	if _, err := uuid.Parse(p.NodeID); err != nil {
		return nil, fmt.Errorf("node_id must be UUID: %w", err)
	}
	if p.Action != "block" && p.Action != "allow" {
		return nil, errors.New(`action must be "block" or "allow"`)
	}
	return p, nil
}

// handleFirewallRuleJob is the control-plane-side handler for both rule_add
// and rule_delete. The agent does the actual enforcement; this handler just
// observes the job. Marking as Running happens when heartbeat dispatches the
// PendingAction; success/failure transitions happen when the agent reports
// back via completed_actions. So this handler's role is mostly: ensure the
// job exists with a valid payload and stays in queued state until the agent
// picks it up.
func (s *Server) handleFirewallRuleJob(_ context.Context, job *storage.Job) error {
	if job == nil {
		return errors.New("nil job")
	}
	// The dispatch + completion lifecycle is heartbeat-driven, so the worker
	// loop has nothing to *do* synchronously. Returning nil parks the job in
	// its current state; the worker manager will not overwrite Succeeded /
	// Failed if the agent has already reported back.
	//
	// Note: if the agent never picks up the action (offline node), the job
	// will sit in Queued / Running until manually cancelled. A future PR can
	// add a TTL sweep — for now operators see "pending: N nodes" in the UI.
	return nil
}

// dispatchFirewallRule creates the per-node rule row, the corresponding job,
// and links them together. Used by handleEntityActions when an operator
// blocks an IP and we've identified the affected nodes.
//
// Returns the (rule, job) pair so callers can build their response, or an
// error if either insert fails. The pair is created in two writes (rule
// first, then job, then SetNodeFirewallRuleJobID); a partial failure leaves
// an orphan rule in pending state, which is benign — heartbeat dispatch
// silently skips rules without a job_id.
func (s *Server) dispatchFirewallRule(
	ctx context.Context,
	tenantID uuid.UUID,
	entityActionID uuid.UUID,
	nodeID uuid.UUID,
	action string, // "block" or "allow"
	source string, // IP or CIDR being blocked
	reason string,
	ttlSeconds *int,
) (*storage.NodeFirewallRule, *storage.Job, error) {
	tag := "c1-" + entityActionID.String()
	srcCopy := source
	in := storage.NodeFirewallRuleInsert{
		EntityActionID: entityActionID,
		NodeID:         nodeID,
		TenantID:       tenantID,
		Action:         action,
		Direction:      "in",
		Source:         &srcCopy,
		Tag:            tag,
	}
	rule, err := s.store.CreateNodeFirewallRule(ctx, in)
	if err != nil {
		return nil, nil, fmt.Errorf("create node firewall rule: %w", err)
	}
	if rule == nil {
		return nil, nil, errors.New("create node firewall rule returned nil")
	}

	// Build job payload.
	jobType := JobTypeFirewallRuleAdd
	payloadAction := action
	if action == "allow" {
		// "allow" in this context means "remove the previous block rule".
		jobType = JobTypeFirewallRuleDelete
		payloadAction = "block"
	}
	jobID := uuid.New()
	payload := firewallJobPayload{
		NodeFirewallRuleID: rule.ID.String(),
		NodeID:             nodeID.String(),
		EntityActionID:     entityActionID.String(),
		Action:             payloadAction,
		Direction:          "in",
		Source:             source,
		Tag:                tag,
		TTLSeconds:         ttlSeconds,
		Reason:             reason,
	}
	if actionPlanID := s.createFirewallActionPlan(ctx, tenantID, nodeID, entityActionID, rule.ID, jobID, payload); actionPlanID != uuid.Nil {
		payload.ActionPlanID = actionPlanID.String()
	}
	payloadBytes, _ := json.Marshal(payload)

	job := &storage.Job{
		ID:       jobID,
		TenantID: tenantID,
		Type:     jobType,
		Status:   storage.JobStatusQueued,
		Payload:  payloadBytes,
	}
	created, err := s.store.CreateJob(ctx, job, nil)
	if err != nil {
		s.markActionPlanFailed(ctx, payload.ActionPlanID)
		return rule, nil, fmt.Errorf("create firewall job: %w", err)
	}
	if err := s.store.SetNodeFirewallRuleJobID(ctx, rule.ID, created.ID); err != nil {
		s.logger.Warn("set firewall rule job id", zap.Error(err))
	}
	rule.JobID = &created.ID
	return rule, created, nil
}

// resolveAffectedNodesForIP finds the nodes that have actually seen traffic
// to/from the given IP in the last 7 days through the selected analytics
// connection backend.
// Used when scope != "fleet". Returns deduped list of node UUIDs.
func (s *Server) resolveAffectedNodesForIP(ctx context.Context, tenantID, ip string) ([]uuid.UUID, error) {
	until := time.Now().UTC()
	since := until.AddDate(0, 0, -7)
	conns, _, err := s.listAnalyticsConnectionsForIP(ctx, tenantID, ip, since, until, 1000)
	if err != nil {
		return nil, fmt.Errorf("list connections for ip: %w", err)
	}
	seen := make(map[uuid.UUID]struct{}, len(conns))
	out := make([]uuid.UUID, 0, len(conns))
	for _, c := range conns {
		if c.NodeID == "" {
			continue
		}
		nid, err := uuid.Parse(c.NodeID)
		if err != nil {
			continue
		}
		if _, ok := seen[nid]; ok {
			continue
		}
		seen[nid] = struct{}{}
		out = append(out, nid)
	}
	return out, nil
}

// handleListActiveBlocks serves GET /api/v1/network/active-blocks?tenant_id=&limit=&offset=&include_removed=
func (s *Server) handleListActiveBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 50)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	keepRemoved := strings.EqualFold(r.URL.Query().Get("include_removed"), "true")

	blocks, err := s.store.ListActiveBlocks(r.Context(), tenantID, limit, offset, keepRemoved)
	if err != nil {
		s.logger.Warn("list active blocks", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := struct {
		Blocks      []storage.ActiveBlock `json:"blocks"`
		GeneratedAt time.Time             `json:"generated_at"`
	}{
		Blocks:      blocks,
		GeneratedAt: time.Now().UTC(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleListBlockNodes serves GET /api/v1/network/blocks/:id/nodes — per-node
// rule rows for one entity_action so operators can see which nodes succeeded
// vs failed.
func (s *Server) handleListBlockNodes(w http.ResponseWriter, r *http.Request, entityActionID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	rules, err := s.store.ListNodeFirewallRulesForEntityAction(r.Context(), entityActionID)
	if err != nil {
		s.logger.Warn("list firewall rules for entity action", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Rules []storage.NodeFirewallRule `json:"rules"`
	}{Rules: rules})
}

// handleNetworkBlocksSubroute dispatches /api/v1/network/blocks/{id}/...
// Currently only the .../nodes path exists; future per-block actions (e.g.
// retry-failed) can plug in here without registering more top-level routes.
func (s *Server) handleNetworkBlocksSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/network/blocks/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "block id must be a UUID", http.StatusBadRequest)
		return
	}
	if len(parts) >= 2 && parts[1] == "nodes" {
		s.handleListBlockNodes(w, r, id)
		return
	}
	http.NotFound(w, r)
}

// parseIntDefault parses a string as int, falling back to def on any error.
func parseIntDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return def
	}
	return n
}
