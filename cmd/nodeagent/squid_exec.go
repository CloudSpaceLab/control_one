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
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// Squid job type strings — agent-side mirrors of the controlplane constants
// in patch.go. Kept duplicated so the dispatcher in heartbeat.go can branch
// without importing controlplane internals.
const (
	squidJobInstall         = "squid.install"
	squidJobReconfigure     = "squid.reconfigure"
	squidJobConfigureClient = "squid.configure_client"
)

// squidExecTimeout caps a single squid action.
const squidExecTimeout = 10 * time.Minute

// squidActionDetail mirrors the server-side squidJobPayload.
type squidActionDetail struct {
	ProxyID   string   `json:"proxy_id"`
	NodeID    string   `json:"node_id"`
	Whitelist []string `json:"whitelist,omitempty"`
	ProxyURL  string   `json:"proxy_url,omitempty"`
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

	detail, err := fetchSquidJobDetail(ctx, client, jobID)
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

	switch jobType {
	case squidJobInstall:
		runSquidInstall(execCtx, log, jobType, jobID, detail)
	case squidJobReconfigure:
		runSquidReconfigure(execCtx, log, jobType, jobID, detail)
	case squidJobConfigureClient:
		runSquidConfigureClient(execCtx, log, jobType, jobID, detail)
	default:
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("unknown squid action %q", jobType),
		})
	}
}

// runSquidInstall installs squid via apt or yum. macOS / Windows are
// unsupported — operators provision squid on a dedicated bastion, so the
// agent code path only needs to cover Linux.
func runSquidInstall(ctx context.Context, log *zap.Logger, jobType, jobID string, _ squidActionDetail) {
	if runtime.GOOS != "linux" {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  "squid install only supported on Linux",
		})
		return
	}
	var (
		cmdName string
		args    []string
		label   string
	)
	switch {
	case lookHas("apt-get"):
		cmdName = "/bin/sh"
		args = []string{"-c", "apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get -y -qq install squid"}
		label = "apt-get install squid"
	case lookHas("dnf"):
		cmdName = "dnf"
		args = []string{"-y", "--quiet", "install", "squid"}
		label = "dnf install squid"
	case lookHas("yum"):
		cmdName = "yum"
		args = []string{"-y", "--quiet", "install", "squid"}
		label = "yum install squid"
	default:
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  "no supported package manager for squid install",
		})
		return
	}
	log.Info("installing squid", zap.String("command", label))
	cmd := exec.CommandContext(ctx, cmdName, args...) // #nosec G204 — static per package manager.
	output, err := cmd.CombinedOutput()
	if err != nil {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  err.Error(),
			Metadata: map[string]any{
				"log_tail": tailString(string(output), 4096),
			},
		})
		return
	}
	enqueueCompletedAction(completedAction{
		Action: jobType,
		JobID:  jobID,
		Status: "succeeded",
		Metadata: map[string]any{
			"log_tail": tailString(string(output), 4096),
		},
	})
}

// runSquidReconfigure writes the new whitelist to /etc/squid/squid.conf and
// triggers `squid -k reconfigure`. The controlplane already validated the
// config via `squid -k parse` before dispatching, so we trust the payload.
func runSquidReconfigure(ctx context.Context, log *zap.Logger, jobType, jobID string, detail squidActionDetail) {
	if runtime.GOOS != "linux" {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  "squid reconfigure only supported on Linux",
		})
		return
	}
	body := "http_port 3128\n"
	for _, h := range detail.Whitelist {
		// Agent-side defence-in-depth: only emit safe-looking hostnames into
		// the squid config. Server validated already, but redundant checks
		// here ensure a corrupted job payload can't inject directives.
		if !isSafeSquidHostAgent(h) {
			enqueueCompletedAction(completedAction{
				Action: jobType,
				JobID:  jobID,
				Status: "failed",
				Error:  fmt.Sprintf("invalid host %q in whitelist", h),
			})
			return
		}
		body += fmt.Sprintf("acl allowed_hosts dstdomain %s\n", h)
	}
	body += "http_access allow allowed_hosts\n"
	body += "http_access deny all\n"
	if err := os.WriteFile("/etc/squid/squid.conf", []byte(body), 0o644); err != nil { // #nosec G306 — squid expects 644.
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("write config: %v", err),
		})
		return
	}
	log.Info("squid -k reconfigure")
	cmd := exec.CommandContext(ctx, "squid", "-k", "reconfigure")
	output, err := cmd.CombinedOutput()
	if err != nil {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  err.Error(),
			Metadata: map[string]any{
				"log_tail": tailString(string(output), 4096),
			},
		})
		return
	}
	enqueueCompletedAction(completedAction{
		Action: jobType,
		JobID:  jobID,
		Status: "succeeded",
		Metadata: map[string]any{
			"log_tail": tailString(string(output), 4096),
		},
	})
}

// runSquidConfigureClient drops apt.conf.d/95proxy on Linux or runs
// `netsh winhttp set proxy` on Windows so the node's package manager picks
// up the proxy URL.
func runSquidConfigureClient(ctx context.Context, log *zap.Logger, jobType, jobID string, detail squidActionDetail) {
	if detail.ProxyURL == "" {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  "proxy_url required",
		})
		return
	}
	switch runtime.GOOS {
	case "linux":
		body := fmt.Sprintf("Acquire::http::Proxy \"%s\";\nAcquire::https::Proxy \"%s\";\n",
			detail.ProxyURL, detail.ProxyURL,
		)
		if err := os.WriteFile("/etc/apt/apt.conf.d/95proxy", []byte(body), 0o644); err != nil { // #nosec G306
			enqueueCompletedAction(completedAction{
				Action: jobType,
				JobID:  jobID,
				Status: "failed",
				Error:  fmt.Sprintf("write apt proxy: %v", err),
			})
			return
		}
		log.Info("apt proxy configured", zap.String("proxy_url", detail.ProxyURL))
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "succeeded",
		})
	case "windows":
		// netsh winhttp set proxy <proxy>:port — strip http:// scheme.
		proxy := strings.TrimPrefix(strings.TrimPrefix(detail.ProxyURL, "http://"), "https://")
		cmd := exec.CommandContext(ctx, "netsh", "winhttp", "set", "proxy", proxy)
		output, err := cmd.CombinedOutput()
		if err != nil {
			enqueueCompletedAction(completedAction{
				Action: jobType,
				JobID:  jobID,
				Status: "failed",
				Error:  err.Error(),
				Metadata: map[string]any{
					"log_tail": tailString(string(output), 2048),
				},
			})
			return
		}
		log.Info("netsh winhttp proxy configured", zap.String("proxy", proxy))
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "succeeded",
			Metadata: map[string]any{
				"log_tail": tailString(string(output), 2048),
			},
		})
	default:
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("client configure not supported on %s", runtime.GOOS),
		})
	}
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
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return detail, fmt.Errorf("decode job: %w", err)
	}
	if len(envelope.Payload) == 0 {
		return detail, fmt.Errorf("job has empty payload")
	}
	if err := json.Unmarshal(envelope.Payload, &detail); err != nil {
		return detail, fmt.Errorf("decode squid payload: %w", err)
	}
	return detail, nil
}

func lookHas(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func isSafeSquidHostAgent(h string) bool {
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
