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
)

// heartbeatPayload is the body we POST to /api/v1/nodes/:id/heartbeat. Only
// agent_version is required by the control plane; the inventory + firewall
// fields are optional and gated by delta logic to avoid resending unchanged
// data on every interval.
type heartbeatPayload struct {
	AgentVersion string `json:"agent_version"`

	// Inventory fields. PackageHash is always sent when collection succeeds;
	// OSPackages is the full list and is only included when the hash changed,
	// 24h have elapsed since the last full sync, or the server explicitly
	// requested a full inventory on the previous response.
	OSPackages    []PackageInfo `json:"os_packages,omitempty"`
	PackageHash   string        `json:"package_hash,omitempty"`
	PackageCount  int           `json:"package_count,omitempty"`
	KernelVersion string        `json:"kernel_version,omitempty"`
	OSVersion     string        `json:"os_version,omitempty"`

	// Firewall snapshot — small, sent in full each heartbeat.
	FirewallState *FirewallState `json:"firewall_state,omitempty"`
}

// heartbeatAckResponse mirrors the server's heartbeatResponse. The agent
// doesn't strictly need to interpret the body, but logging the state/
// activated flags is useful for operator debugging during enrollment.
type heartbeatAckResponse struct {
	NodeID                 string           `json:"node_id"`
	State                  string           `json:"state"`
	LastSeenAt             string           `json:"last_seen_at"`
	Activated              bool             `json:"activated"`
	Reason                 *string          `json:"reason,omitempty"`
	EventFilters           *json.RawMessage `json:"event_filters,omitempty"`
	PendingActions         []string         `json:"pending_actions,omitempty"`
	FullInventoryRequested bool             `json:"full_inventory_requested,omitempty"`
}

// FilterApplier is invoked once per heartbeat with the controlplane-issued
// tenant policy. Wired in main.go to dispatch SetFilter / UpdateFilter /
// SetCaptureQueryText against the live netflow / fileaccess / dbquery
// managers. Optional: when nil the policy is silently ignored (useful for
// lightweight test agents that don't run collectors).
type FilterApplier func(raw json.RawMessage)

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
	mu             sync.Mutex
	hash           string
	count          int
	kernel         string
	osVersion      string
	lastFullSync   time.Time
	forceNext      bool // server set full_inventory_requested on the previous ack
}

// agentInventoryCache is the singleton — heartbeat.go is the only consumer.
var agentInventoryCache = &inventoryCache{}

// startControlPlaneHeartbeat launches a goroutine that periodically POSTs a
// heartbeat to the control plane. The loop exits when ctx is cancelled.
//
// Parameters are deliberately simple: the nodeagent main() owns the *api.Client
// (already wired with the mTLS cert), the node id, and the heartbeat interval
// from the enrollment response.
func startControlPlaneHeartbeat(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, interval time.Duration, applyFilters FilterApplier, selfUpdater SelfUpdater) {
	if client == nil || nodeID == "" {
		log.Warn("heartbeat loop not started: missing client or node id")
		return
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}

	go runControlPlaneHeartbeat(ctx, client, log, nodeID, interval, applyFilters, selfUpdater)
}

func runControlPlaneHeartbeat(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, interval time.Duration, applyFilters FilterApplier, selfUpdater SelfUpdater) {
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
	if err := sendHeartbeat(ctx, client, logger, nodeID, applyFilters, selfUpdater); err != nil {
		logger.Debug("initial heartbeat failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("heartbeat loop stopped")
			return
		case <-ticker.C:
			if err := sendHeartbeat(ctx, client, logger, nodeID, applyFilters, selfUpdater); err != nil {
				logger.Debug("heartbeat attempt failed", zap.Error(err))
			}
		}
	}
}

// sendHeartbeat performs one POST. Errors are logged at debug — a transient
// network blip should not clutter the operator console. The response body is
// parsed only for informational logging.
func sendHeartbeat(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, applyFilters FilterApplier, selfUpdater SelfUpdater) error {
	payload := heartbeatPayload{AgentVersion: heartbeatAgentVersion()}
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

	// Dispatch pending actions instructed by the control plane.
	for _, action := range ack.PendingActions {
		if action == "agent_update" && selfUpdater != nil {
			log.Info("control plane requested agent self-update")
			go selfUpdater.TriggerUpdate(ctx, client, log)
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

// enrichHeartbeatPayload adds os_packages / firewall_state / kernel + os
// version to a heartbeat payload, applying delta logic so the os_packages
// array is only sent when needed. Collection failures degrade silently —
// missing fields are fine, blocked heartbeats are not.
func enrichHeartbeatPayload(payload *heartbeatPayload, log *zap.Logger) {
	// Firewall is always sent in full — it's small and changes meaningfully.
	st := collectFirewall()
	payload.FirewallState = &st

	pkgs, hash, err := collectInventory()
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
	payload.KernelVersion = kernelVersion()
	payload.OSVersion = osVersion()

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
	default:
		return ""
	}
}

// heartbeatAgentVersion exposes the linker-injected version string plus
// runtime context. Separated so tests can swap it.
func heartbeatAgentVersion() string {
	return fmt.Sprintf("%s (%s/%s)", agentVersion, runtime.GOOS, runtime.GOARCH)
}
