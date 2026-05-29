package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
)

// heartbeatPayload is the body we POST to /api/v1/nodes/:id/heartbeat. Only
// agent_version is required by the control plane; the inventory + firewall
// fields are optional and gated by delta logic to avoid resending unchanged
// data on every interval.
type heartbeatPayload struct {
	AgentVersion    string   `json:"agent_version"`
	Capabilities    []string `json:"capabilities,omitempty"`
	AgentReleaseSeq int      `json:"agent_release_seq,omitempty"`
	RuntimeProfile  string   `json:"agent_runtime_profile,omitempty"`

	CollectorState   []collectorStateReport  `json:"collector_state,omitempty"`
	CollectorBudget  []collectorBudgetReport `json:"collector_budget,omitempty"`
	AgentSelfMetrics *agentSelfMetrics       `json:"agent_self_metrics,omitempty"`

	// Inventory fields. PackageHash is always sent when collection succeeds;
	// OSPackages is the full list and is only included when the hash changed,
	// 24h have elapsed since the last full sync, or the server explicitly
	// requested a full inventory on the previous response.
	OSPackages     []PackageInfo   `json:"os_packages,omitempty"`
	PackageHash    string          `json:"package_hash,omitempty"`
	PackageCount   int             `json:"package_count,omitempty"`
	KernelVersion  string          `json:"kernel_version,omitempty"`
	OSVersion      string          `json:"os_version,omitempty"`
	ServerPurposes []ServerPurpose `json:"server_purposes,omitempty"`

	// Firewall snapshot — small, sent in full each heartbeat.
	FirewallState *FirewallState `json:"firewall_state,omitempty"`

	// NetworkPolicyReceipts reports applied/planned/drift status for desired
	// network policy states received from the control plane.
	NetworkPolicyReceipts []networkPolicyReceipt `json:"network_policy_receipts,omitempty"`

	// CompletedActions reports the outcome of pending_actions the agent
	// executed since the previous heartbeat. Drained from completedActionQueue
	// in enrichHeartbeatPayload. Empty in steady-state.
	CompletedActions []completedAction `json:"completed_actions,omitempty"`
}

// completedAction matches the server-side heartbeatCompletedAction shape.
// Status is "succeeded" | "failed".
type completedAction struct {
	Action   string         `json:"action"`
	JobID    string         `json:"job_id"`
	Status   string         `json:"status"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// heartbeatAckResponse mirrors the server's heartbeatResponse. The agent
// doesn't strictly need to interpret the body, but logging the state/
// activated flags is useful for operator debugging during enrollment.
type heartbeatAckResponse struct {
	NodeID                 string                                `json:"node_id"`
	State                  string                                `json:"state"`
	LastSeenAt             string                                `json:"last_seen_at"`
	Activated              bool                                  `json:"activated"`
	Reason                 *string                               `json:"reason,omitempty"`
	EventFilters           *json.RawMessage                      `json:"event_filters,omitempty"`
	ConnectorPolicy        *connectordiscovery.AutoConnectPolicy `json:"connector_policy,omitempty"`
	NetworkPolicy          *json.RawMessage                      `json:"network_policy,omitempty"`
	ApprovedLogSources     []approvedConnectorLogSourceDTO       `json:"approved_log_sources,omitempty"`
	PendingActions         []string                              `json:"pending_actions,omitempty"`
	FullInventoryRequested bool                                  `json:"full_inventory_requested,omitempty"`
}

// FilterApplier is invoked once per heartbeat with the controlplane-issued
// tenant policy. Wired in main.go to dispatch SetFilter / UpdateFilter /
// SetCaptureQueryText against the live netflow / fileaccess / dbquery
// managers. Optional: when nil the policy is silently ignored (useful for
// lightweight test agents that don't run collectors).
type FilterApplier func(raw json.RawMessage)

// NetworkPolicyApplier handles the controlplane-issued enforcement desired
// state and returns a receipt to include on the next heartbeat.
type NetworkPolicyApplier func(ctx context.Context, raw json.RawMessage) *networkPolicyReceipt

// ApprovedLogSourcesApplier hot-adds control-plane-approved local log sources
// after a heartbeat response. It is nil when log collection is disabled.
type ApprovedLogSourcesApplier func(ctx context.Context, sources []approvedConnectorLogSourceDTO)

// agentVersion is defined at build time via -ldflags. The zero value ("dev")
// is overwritten in release builds. Exported so the install flow can stamp it
// without dragging in an additional package.
var agentVersion = "dev"

// fullInventoryInterval is how often the agent forces a full os_packages
// resend even when the local hash is unchanged. Caps the worst-case staleness
// of the server's view to one day.
const fullInventoryInterval = 24 * time.Hour

// inventoryCache holds the agent's view of what the server already knows so
// successive heartbeats can short-circuit the full os_packages resend. The
// cache is in-memory only — agent restart triggers a fresh full sync, which
// is intentional (server reconciles via hash comparison).
type inventoryCache struct {
	mu           sync.Mutex
	hash         string
	count        int
	kernel       string
	osVersion    string
	lastFullSync time.Time
	forceNext    bool // server set full_inventory_requested on the previous ack
}

// agentInventoryCache is the singleton — heartbeat.go is the only consumer.
var agentInventoryCache = &inventoryCache{}

// startControlPlaneHeartbeat launches a goroutine that periodically POSTs a
// heartbeat to the control plane. The loop exits when ctx is cancelled.
//
// Parameters are deliberately simple: the nodeagent main() owns the *api.Client
// (already wired with the mTLS cert), the node id, and the heartbeat interval
// from the enrollment response.
func startControlPlaneHeartbeat(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, interval time.Duration, applyFilters FilterApplier, applyNetworkPolicy NetworkPolicyApplier, applyApprovedLogSources ApprovedLogSourcesApplier, selfUpdater SelfUpdater) {
	if client == nil || nodeID == "" {
		log.Warn("heartbeat loop not started: missing client or node id")
		return
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}

	go runControlPlaneHeartbeat(ctx, client, log, nodeID, interval, applyFilters, applyNetworkPolicy, applyApprovedLogSources, selfUpdater)
}

func runControlPlaneHeartbeat(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, interval time.Duration, applyFilters FilterApplier, applyNetworkPolicy NetworkPolicyApplier, applyApprovedLogSources ApprovedLogSourcesApplier, selfUpdater SelfUpdater) {
	logger := log.Named("cp-heartbeat")
	logger.Info("starting control-plane heartbeat",
		zap.String("node_id", nodeID),
		zap.Duration("interval", interval),
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Fire one immediately so the UI sees the node moving quickly — operators
	// watching the enrollment table don't want to wait a full interval for
	// the first update.
	if err := sendHeartbeat(ctx, client, logger, nodeID, applyFilters, applyNetworkPolicy, applyApprovedLogSources, selfUpdater); err != nil {
		logger.Debug("initial heartbeat failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("heartbeat loop stopped")
			return
		case <-ticker.C:
			if err := sendHeartbeat(ctx, client, logger, nodeID, applyFilters, applyNetworkPolicy, applyApprovedLogSources, selfUpdater); err != nil {
				logger.Debug("heartbeat attempt failed", zap.Error(err))
			}
		}
	}
}

// sendHeartbeat performs one POST. Errors are logged at debug — a transient
// network blip should not clutter the operator console. The response body is
// parsed only for informational logging.
func sendHeartbeat(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, applyFilters FilterApplier, applyNetworkPolicy NetworkPolicyApplier, applyApprovedLogSources ApprovedLogSourcesApplier, selfUpdater SelfUpdater) error {
	payload := heartbeatPayload{
		AgentVersion:     heartbeatAgentVersion(),
		Capabilities:     heartbeatAgentCapabilities(),
		RuntimeProfile:   heartbeatRuntimeProfile(),
		CollectorState:   heartbeatCollectorState(),
		CollectorBudget:  heartbeatCollectorBudgets(),
		AgentSelfMetrics: collectAgentSelfMetrics(),
	}
	if selfUpdater != nil {
		payload.AgentReleaseSeq = selfUpdater.CurrentReleaseSeq()
	}
	enrichHeartbeatPayload(&payload, log)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	// Bound each call so the loop never gets stuck on an unresponsive CP.
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := client.Do(callCtx, http.MethodPost, "/api/v1/nodes/"+nodeID+"/heartbeat", body)
	if err != nil {
		return fmt.Errorf("post heartbeat: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		// Drain body briefly to surface the server message in logs.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("heartbeat status %d: %s", resp.StatusCode, string(snippet))
	}

	var ack heartbeatAckResponse
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		// Non-fatal — the heartbeat still landed; just means the server
		// decided not to send a JSON body. Log and move on.
		log.Debug("decode heartbeat ack", zap.Error(err))
		return nil
	}

	if ack.Activated {
		log.Info("node activated by control plane", zap.String("state", ack.State))
	} else {
		log.Debug("heartbeat acknowledged",
			zap.String("state", ack.State),
			zap.Any("reason", ack.Reason),
		)
	}

	// Hot-reload tenant capture-filter policy.
	if applyFilters != nil && ack.EventFilters != nil {
		applyFilters(*ack.EventFilters)
	}
	if ack.ConnectorPolicy != nil {
		setConnectorAutoConnectPolicy(*ack.ConnectorPolicy)
	}
	if applyApprovedLogSources != nil && len(ack.ApprovedLogSources) > 0 {
		applyApprovedLogSources(ctx, ack.ApprovedLogSources)
	}
	if ack.NetworkPolicy != nil {
		if applyNetworkPolicy != nil {
			if receipt := applyNetworkPolicy(ctx, *ack.NetworkPolicy); receipt != nil {
				enqueueNetworkPolicyReceipt(*receipt)
			}
		} else {
			log.Info("network isolation policy received", zap.ByteString("policy", *ack.NetworkPolicy))
		}
	}

	// Dispatch pending actions instructed by the control plane.
	for _, action := range ack.PendingActions {
		switch {
		case (action == agentUpdateJob || strings.HasPrefix(action, agentUpdateJob+":")) && selfUpdater != nil:
			jobID := ""
			if _, rawID, ok := strings.Cut(action, ":"); ok {
				jobID = rawID
			}
			log.Info("control plane requested agent self-update")
			go selfUpdater.TriggerUpdate(ctx, client, log, jobID)
		case strings.HasPrefix(action, "firewall.rule_add:"),
			strings.HasPrefix(action, "firewall.rule_delete:"):
			// Run synchronously — completion gets reported on the *next*
			// heartbeat, so we want the result available before this call
			// returns. The actual exec lives in firewall_exec.go.
			executeFirewallAction(ctx, client, log, action)
		case strings.HasPrefix(action, "patch.deploy_direct:"),
			strings.HasPrefix(action, "patch.deploy_proxy:"),
			strings.HasPrefix(action, "patch.deploy_airgapped:"),
			strings.HasPrefix(action, "patch.inventory_scan:"):
			// Patch runs are slow (apt-get upgrade can take minutes), so
			// we dispatch async and let the next heartbeat drain results.
			go executePatchAction(ctx, client, log, action)
		case strings.HasPrefix(action, "squid.install:"),
			strings.HasPrefix(action, "squid.reconfigure:"),
			strings.HasPrefix(action, "squid.configure_client:"):
			// Squid actions are short-lived but still go async to avoid
			// blocking the heartbeat path on slow apt-get installs.
			go executeSquidAction(ctx, client, log, action)
		case strings.HasPrefix(action, "webserver.inventory_scan:"),
			strings.HasPrefix(action, "webserver.config_plan:"),
			strings.HasPrefix(action, "webserver.config_apply:"),
			strings.HasPrefix(action, "webserver.blocklist_update:"),
			strings.HasPrefix(action, "webserver.config_rollback:"):
			// Webserver control uses managed snippets plus validation/reload
			// receipts; run it off the heartbeat path and report completion
			// in the next completed_actions drain.
			go executeWebserverAction(ctx, client, log, action)
		case strings.HasPrefix(action, "ailogfixer.plan:"),
			strings.HasPrefix(action, "ailogfixer.apply:"),
			strings.HasPrefix(action, "ailogfixer.rollback:"):
			// AI LogFixer actions run node-local and return structured dry-run
			// or mutation receipts through the normal heartbeat completion queue.
			go executeAILogFixerAction(ctx, client, log, action)
		}
	}
	if ack.FullInventoryRequested {
		// Server's stored hash diverged from ours (or it has no record).
		// Force a full resend on the next heartbeat regardless of cache state.
		agentInventoryCache.mu.Lock()
		agentInventoryCache.forceNext = true
		agentInventoryCache.mu.Unlock()
		log.Debug("server requested full inventory on next heartbeat")
	}
	return nil
}

func heartbeatAgentCapabilities() []string {
	base := []string{
		"firewall_control.v1",
		"patch_management.v1",
		"event_filters.v1",
		"network_policy_desired_state.v1",
		"network_policy_receipts.v1",
		"webserver_control.v1",
		"ailogfixer_remediation.v1",
		"server_purpose_inventory.v1",
		"connector_discovery.v1",
		"connector_auto_connect_policy.v1",
		"connector_approved_sources.v1",
		"connection_lifecycle_headers.v1",
		"app_dependency_inventory.v1",
		"agent_update_job_status.v1",
	}
	return append(base, heartbeatRuntimeCapabilities()...)
}

// enrichHeartbeatPayload adds os_packages / firewall_state / kernel + os
// version to a heartbeat payload, applying delta logic so the os_packages
// array is only sent when needed. Collection failures degrade silently —
// missing fields are fine, blocked heartbeats are not.
func enrichHeartbeatPayload(payload *heartbeatPayload, log *zap.Logger) {
	// Firewall is always sent in full — it's small and changes meaningfully.
	st := cachedFirewallSnapshot()
	payload.FirewallState = &st

	// Drain any completed firewall actions accumulated since the last
	// heartbeat. The server uses these to flip node_firewall_rules.status.
	if drained := drainCompletedActions(); len(drained) > 0 {
		payload.CompletedActions = drained
	}
	if receipts := drainNetworkPolicyReceipts(); len(receipts) > 0 {
		payload.NetworkPolicyReceipts = receipts
	}

	pkgs, hash, kernel, osVer, purposes, err := cachedInventorySnapshot()
	if err != nil {
		log.Debug("collect inventory failed; omitting", zap.Error(err))
		return
	}
	if hash == "" {
		// Platform doesn't support inventory or returned no packages — leave
		// the inventory fields off the payload entirely.
		return
	}
	payload.PackageHash = hash
	payload.PackageCount = len(pkgs)
	payload.KernelVersion = kernel
	payload.OSVersion = osVer
	payload.ServerPurposes = purposes

	agentInventoryCache.mu.Lock()
	defer agentInventoryCache.mu.Unlock()

	now := time.Now().UTC()
	needsFull := agentInventoryCache.forceNext ||
		agentInventoryCache.hash == "" ||
		agentInventoryCache.hash != hash ||
		now.Sub(agentInventoryCache.lastFullSync) >= fullInventoryInterval

	if !needsFull {
		// Hash matches the last full sync and 24h haven't elapsed — omit the
		// array. Server keeps its existing inventory.
		return
	}
	payload.OSPackages = pkgs
	agentInventoryCache.hash = hash
	agentInventoryCache.count = len(pkgs)
	agentInventoryCache.kernel = payload.KernelVersion
	agentInventoryCache.osVersion = payload.OSVersion
	agentInventoryCache.lastFullSync = now
	agentInventoryCache.forceNext = false
}

// kernelVersion returns the running kernel version. On Linux, `uname -r`.
// On Windows, the build number from `cmd /c ver`. Empty on errors — the
// server tolerates absence.
func kernelVersion() string {
	switch runtime.GOOS {
	case "linux", "darwin":
		out, err := exec.Command("uname", "-r").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	case "aix":
		out, err := exec.Command("uname", "-v").Output()
		if err != nil {
			return ""
		}
		ver := strings.TrimSpace(string(out))
		relOut, _ := exec.Command("uname", "-r").Output()
		rel := strings.TrimSpace(string(relOut))
		if rel != "" {
			return ver + "." + rel
		}
		return ver
	case "windows":
		out, err := exec.Command("cmd", "/c", "ver").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	default:
		return ""
	}
}

// osVersion returns a human-readable OS version string. Linux: PRETTY_NAME
// from /etc/os-release. macOS: `sw_vers -productVersion`. Windows: same as
// kernelVersion (cmd /c ver). Empty on errors.
func osVersion() string {
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/etc/os-release")
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
			}
		}
		return ""
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err != nil {
			return ""
		}
		return "macOS " + strings.TrimSpace(string(out))
	case "windows":
		return kernelVersion()
	case "aix":
		out, err := exec.Command("oslevel", "-s").Output()
		if err != nil {
			return kernelVersion()
		}
		return "AIX " + strings.TrimSpace(string(out))
	default:
		return ""
	}
}

// heartbeatAgentVersion exposes the linker-injected version string plus
// runtime context. Separated so tests can swap it.
func heartbeatAgentVersion() string {
	return fmt.Sprintf("%s (%s/%s)", agentVersion, runtime.GOOS, runtime.GOARCH)
}
