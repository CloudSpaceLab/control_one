package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/firewall"
)

// completedActionQueue accumulates outcomes between heartbeats. Heartbeats
// drain it via drainCompletedActions before serializing the next payload.
// Bounded so a runaway dispatch can't OOM the agent — overflow drops the
// oldest entries (operator can re-dispatch from the UI).
var (
	completedActionMu    sync.Mutex
	completedActionQueue []completedAction
)

const completedActionQueueCap = 256

func enqueueCompletedAction(a completedAction) {
	completedActionMu.Lock()
	defer completedActionMu.Unlock()
	if len(completedActionQueue) >= completedActionQueueCap {
		// Drop oldest.
		completedActionQueue = completedActionQueue[1:]
	}
	completedActionQueue = append(completedActionQueue, a)
}

// drainCompletedActions returns and clears the accumulated outcomes.
// Returns nil when empty so the JSON encoder can omit the field entirely.
func drainCompletedActions() []completedAction {
	completedActionMu.Lock()
	defer completedActionMu.Unlock()
	if len(completedActionQueue) == 0 {
		return nil
	}
	out := make([]completedAction, len(completedActionQueue))
	copy(out, completedActionQueue)
	completedActionQueue = completedActionQueue[:0]
	return out
}

// firewallManager is a process-wide singleton initialized lazily on first
// firewall action. Detect() walks the available backends in priority order
// (ufw → firewalld → nftables → iptables on Linux; netsh on Windows) and
// stays cached for the lifetime of the agent.
var (
	firewallManagerMu   sync.Mutex
	firewallManagerOnce sync.Once
	firewallManager     *firewall.Manager
	firewallManagerErr  error
)

func ensureFirewallManager() (*firewall.Manager, error) {
	firewallManagerOnce.Do(func() {
		firewallManagerMu.Lock()
		defer firewallManagerMu.Unlock()
		m := firewall.New()
		if err := m.Detect(); err != nil {
			firewallManagerErr = err
			return
		}
		firewallManager = m
	})
	return firewallManager, firewallManagerErr
}

// firewallActionDetail is the agent-side mirror of the server's
// firewallJobPayload. We only need the fields required to construct a
// firewall.Rule — the rest are advisory.
type firewallActionDetail struct {
	NodeFirewallRuleID string `json:"node_firewall_rule_id"`
	NodeID             string `json:"node_id"`
	EntityActionID     string `json:"entity_action_id"`
	Action             string `json:"action"`
	Direction          string `json:"direction"`
	Source             string `json:"source"`
	Dest               string `json:"dest"`
	Port               int    `json:"port"`
	Protocol           string `json:"protocol"`
	Tag                string `json:"tag"`
	Reason             string `json:"reason"`
}

// executeFirewallAction is invoked synchronously from sendHeartbeat for each
// firewall.* PendingAction. It fetches the job detail from the control plane,
// translates it into a firewall.Rule, calls Apply or Remove on the live
// backend, and enqueues a completion record for the next heartbeat to drain.
//
// Failures degrade gracefully — a failed action just gets reported back as
// status=failed; the operator UI shows "X/N failed" and can retry.
func executeFirewallAction(ctx context.Context, client *api.Client, log *zap.Logger, pendingAction string) {
	parts := strings.SplitN(pendingAction, ":", 2)
	if len(parts) != 2 {
		log.Warn("firewall pending action malformed", zap.String("raw", pendingAction))
		return
	}
	jobType, jobID := parts[0], parts[1]

	detail, err := fetchFirewallJobDetail(ctx, client, jobID)
	if err != nil {
		log.Warn("fetch firewall job detail", zap.Error(err), zap.String("job_id", jobID))
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("fetch job detail: %v", err),
		})
		return
	}

	mgr, err := ensureFirewallManager()
	if err != nil || mgr == nil {
		errMsg := "no firewall backend available"
		if err != nil {
			errMsg = err.Error()
		}
		log.Warn("firewall manager unavailable", zap.String("err", errMsg))
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  errMsg,
		})
		return
	}

	rule := firewallRuleFromAction(jobType, detail)

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if jobType == "firewall.rule_delete" {
		err = mgr.Remove(execCtx, rule)
	} else {
		err = mgr.Apply(execCtx, rule)
	}
	if err != nil {
		log.Warn("firewall action failed",
			zap.String("job_type", jobType),
			zap.String("job_id", jobID),
			zap.Error(err),
		)
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  err.Error(),
		})
		return
	}
	log.Info("firewall action succeeded",
		zap.String("job_type", jobType),
		zap.String("job_id", jobID),
		zap.String("source", detail.Source),
	)
	invalidateFirewallCache()
	enqueueCompletedAction(completedAction{
		Action: jobType,
		JobID:  jobID,
		Status: "succeeded",
	})
}

func firewallRuleFromAction(jobType string, detail firewallActionDetail) firewall.Rule {
	rule := firewall.Rule{
		Source:    detail.Source,
		Dest:      detail.Dest,
		Port:      detail.Port,
		Protocol:  detail.Protocol,
		Direction: firewall.DirectionIn,
		Action:    firewall.ActionBlock,
		Tag:       detail.Tag,
		Comment:   detail.Reason,
	}
	if strings.EqualFold(detail.Direction, "out") {
		rule.Direction = firewall.DirectionOut
	}
	// A delete job removes the previously installed rule shape. In the IP
	// block path that means removing the original block/drop rule, not trying
	// to remove an allow/accept rule that was never installed.
	if jobType != "firewall.rule_delete" && strings.EqualFold(detail.Action, "allow") {
		rule.Action = firewall.ActionAllow
	}
	return rule
}

// fetchFirewallJobDetail GETs /api/v1/jobs/:id and decodes the payload.
// The control plane stores the firewallJobPayload in the job's payload
// column; we read that to build a firewall.Rule. Auth is the agent mTLS
// already wired into client.Do.
func fetchFirewallJobDetail(ctx context.Context, client *api.Client, jobID string) (firewallActionDetail, error) {
	var detail firewallActionDetail
	if client == nil {
		return detail, fmt.Errorf("api client unavailable")
	}
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := client.Do(callCtx, http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	if err != nil {
		return detail, fmt.Errorf("get job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return detail, fmt.Errorf("get job %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// The job response wraps payload as a JSON-encoded blob inside .payload —
	// see jobs.go handleGetJob. We accept either the full job envelope or
	// just the payload itself for forward-compat.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return detail, fmt.Errorf("decode job: %w", err)
	}
	if len(envelope.Payload) == 0 {
		return detail, fmt.Errorf("job has empty payload")
	}
	if err := json.Unmarshal(envelope.Payload, &detail); err != nil {
		return detail, fmt.Errorf("decode firewall payload: %w", err)
	}
	return detail, nil
}
