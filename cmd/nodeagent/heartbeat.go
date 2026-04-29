package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// heartbeatPayload is the body we POST to /api/v1/nodes/:id/heartbeat. Only
// agent_version is required by the control plane today — everything else is
// reserved for future telemetry without needing a migration.
type heartbeatPayload struct {
	AgentVersion string `json:"agent_version"`
}

// heartbeatAckResponse mirrors the server's heartbeatResponse. The agent
// doesn't strictly need to interpret the body, but logging the state/
// activated flags is useful for operator debugging during enrollment.
type heartbeatAckResponse struct {
	NodeID         string           `json:"node_id"`
	State          string           `json:"state"`
	LastSeenAt     string           `json:"last_seen_at"`
	Activated      bool             `json:"activated"`
	Reason         *string          `json:"reason,omitempty"`
	EventFilters   *json.RawMessage `json:"event_filters,omitempty"`
	PendingActions []string         `json:"pending_actions,omitempty"`
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
	return nil
}

// heartbeatAgentVersion exposes the linker-injected version string plus
// runtime context. Separated so tests can swap it.
func heartbeatAgentVersion() string {
	return fmt.Sprintf("%s (%s/%s)", agentVersion, runtime.GOOS, runtime.GOARCH)
}
