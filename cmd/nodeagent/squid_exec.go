package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

// Squid job type strings — agent-side mirrors of the controlplane constants
// in patch.go. Kept duplicated so the dispatcher in heartbeat.go can branch
// without importing controlplane internals.
const (
	squidJobInstall         = "squid.install"
	squidJobReconfigure     = "squid.reconfigure"
	squidJobConfigureClient = "squid.configure_client"
	squidJobRemove          = "squid.remove"
)

// squidExecTimeout caps a single squid action.
const squidExecTimeout = 10 * time.Minute

// squidActionDetail mirrors the server-side squidJobPayload.
type squidActionDetail struct {
	Revision     int64     `json:"revision"`
	ClientCIDRs  []string  `json:"client_cidrs"`
	ProbeURLs    []string  `json:"probe_urls"`
	SquidVersion string    `json:"squid_version"`
	ExpiresAt    time.Time `json:"expires_at"`
	RequestedBy  string    `json:"requested_by"`
	JobType      string    `json:"-"`
	ProxyID      string    `json:"proxy_id"`
	NodeID       string    `json:"node_id"`
	Whitelist    []string  `json:"whitelist,omitempty"`
	ProxyURL     string    `json:"proxy_url,omitempty"`
}

// executeSquidAction is the entrypoint for squid.* pending actions
// (install / reconfigure / configure_client). The pendingAction string is
// "<job_type>:<job_id>" — same encoding firewall.* and patch.* use.
func executeSquidAction(ctx context.Context, client *api.Client, log *zap.Logger, pendingAction string) {
	parts := strings.SplitN(pendingAction, ":", 2)
	if len(parts) != 2 {
		log.Warn("squid pending action malformed", zap.String("raw", pendingAction))
		return
	}
	jobType, jobID := parts[0], parts[1]
	if id, err := uuid.Parse(jobID); err != nil || id == uuid.Nil {
		log.Warn("proxy action refused: valid job UUID required")
		return
	}
	// Legacy configure_client is deliberately excluded: persistent global client
	// changes are not part of the qualified deployment-scoped proxy contract.
	if !patchsupport.IsProxyLifecycle(jobType) || agentPatchJournal == nil {
		log.Warn("proxy action refused: unsupported action or durable journal unavailable")
		return
	}
	reserved, reserveErr := agentPatchJournal.reserve(jobID, jobType)
	if reserveErr != nil || !reserved {
		if reserveErr != nil {
			log.Error("proxy reservation failed", zap.Error(reserveErr))
		}
		return
	}
	// Share the package/configuration mutation lock with patch installs and
	// preparation. Fetch and revalidate only after acquiring it.
	patchExecutionMu.Lock()
	defer patchExecutionMu.Unlock()

	detail, err := fetchSquidJobDetail(ctx, client, jobID)
	if err == nil {
		err = validateSquidDispatchIdentity(jobType, detail, patchAgentNodeID)
	}
	if err != nil {
		log.Warn("fetch squid job detail", zap.Error(err), zap.String("job_id", jobID))
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("fetch job detail: %v", err),
		})
		return
	}

	execCtx, cancel := context.WithTimeout(ctx, squidExecTimeout)
	defer cancel()
	execCtx, deadlineCancel := context.WithDeadline(execCtx, detail.ExpiresAt)
	defer deadlineCancel()
	result := completedAction{Action: jobType, JobID: jobID, Status: "failed"}
	if detail.Revision <= 0 || !managedProxyAvailable() {
		result.Error = "managed Ubuntu/systemd proxy runtime and reviewed ownership revision required"
		enqueueCompletedAction(result)
		return
	}
	p := detail.managedRequest()
	e := managedProxyExecutor{ConfigPath: "/etc/squid/squid.conf", MarkerPath: filepath.Join(filepath.Dir(agentPatchJournal.path), "managed-proxy.json"), Run: managedProxyRun, Ready: proxyUpstreamReady, Stopped: proxyListenerStopped, Version: func(ctx context.Context) (string, error) {
		out, err := exec.CommandContext(ctx, "dpkg-query", "--show", "--showformat=${Version}", "squid").Output()
		return strings.TrimSpace(string(out)), err
	}}
	proof, executionErr := e.Execute(execCtx, jobType, p)
	result.Metadata = map[string]any{"proxy_proof": proof, "readiness_scope": "recorded local service and approved HTTPS repository probes; not a continuous health guarantee"}
	if executionErr != nil {
		result.Error = tailString(executionErr.Error(), 4096)
	} else {
		result.Status = "succeeded"
	}
	enqueueCompletedAction(result)
	return

}

func (d squidActionDetail) managedRequest() patchsupport.ProxyLifecycle {
	return patchsupport.ProxyLifecycle{ProxyID: d.ProxyID, NodeID: d.NodeID, Whitelist: d.Whitelist, ExpiresAt: d.ExpiresAt, RequestedBy: d.RequestedBy, Revision: d.Revision, ClientCIDRs: d.ClientCIDRs, ProbeURLs: d.ProbeURLs, SquidVersion: d.SquidVersion}
}

func validateSquidDispatchIdentity(action string, detail squidActionDetail, nodeID string) error {
	if nodeID == "" || detail.NodeID != nodeID || detail.JobType != action {
		return fmt.Errorf("squid dispatch does not match this node, action, or proxy identity")
	}
	return detail.managedRequest().Validate(action, time.Now().UTC())
}

// fetchSquidJobDetail retrieves the squid payload from the controlplane.
func fetchSquidJobDetail(ctx context.Context, client *api.Client, jobID string) (squidActionDetail, error) {
	var detail squidActionDetail
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
	var envelope struct {
		ID      string          `json:"id"`
		Type    string          `json:"type"`
		Status  string          `json:"status"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return detail, fmt.Errorf("decode job: %w", err)
	}
	if envelope.ID != jobID || envelope.Status != "running" {
		return detail, fmt.Errorf("squid job identity or execution status invalid")
	}
	if len(envelope.Payload) == 0 {
		return detail, fmt.Errorf("job has empty payload")
	}
	if err := json.Unmarshal(envelope.Payload, &detail); err != nil {
		return detail, fmt.Errorf("decode squid payload: %w", err)
	}
	detail.JobType = envelope.Type
	return detail, nil
}

func lookHas(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}
