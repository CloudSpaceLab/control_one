package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// patchActionDetail mirrors the server-side patchJobPayload. We only need
// the ids — the actual upgrade command is OS-determined here.
type patchActionDetail struct {
	NodePatchStateID string `json:"node_patch_state_id"`
	NodeID           string `json:"node_id"`
	DeploymentID     string `json:"deployment_id"`
	Mode             string `json:"mode"`
}

// patchTimeout caps a single agent-side upgrade run. Bigger fleets see
// occasional 5-minute apt-get upgrades; 20m gives headroom while still
// stopping a wedged dpkg from blocking heartbeats forever.
const patchTimeout = 20 * time.Minute

// executePatchAction parses a patch.deploy_direct pending action, fetches
// the job payload from the control plane, runs the OS-appropriate package
// manager, and enqueues a completion record for the next heartbeat to
// drain. Errors are reported as status=failed; the operator UI surfaces
// them via the per-deployment per-node detail panel.
func executePatchAction(ctx context.Context, client *api.Client, log *zap.Logger, pendingAction string) {
	parts := strings.SplitN(pendingAction, ":", 2)
	if len(parts) != 2 {
		log.Warn("patch pending action malformed", zap.String("raw", pendingAction))
		return
	}
	jobType, jobID := parts[0], parts[1]

	detail, err := fetchPatchJobDetail(ctx, client, jobID)
	if err != nil {
		log.Warn("fetch patch job detail", zap.Error(err), zap.String("job_id", jobID))
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("fetch job detail: %v", err),
		})
		return
	}

	cmdName, args, label, ok := patchCommandForOS()
	if !ok {
		log.Warn("no patch command for platform", zap.String("os", runtime.GOOS))
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("no patch command available for %s", runtime.GOOS),
		})
		return
	}

	execCtx, cancel := context.WithTimeout(ctx, patchTimeout)
	defer cancel()

	log.Info("running patch deployment",
		zap.String("job_id", jobID),
		zap.String("deployment_id", detail.DeploymentID),
		zap.String("command", label),
	)
	cmd := exec.CommandContext(execCtx, cmdName, args...) // #nosec G204 — args are static per-OS.
	output, runErr := cmd.CombinedOutput()
	logTail := tailString(string(output), 4096)

	if runErr != nil {
		errMsg := runErr.Error()
		log.Warn("patch deployment failed",
			zap.String("job_id", jobID),
			zap.String("err", errMsg),
		)
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  errMsg,
			Metadata: map[string]any{
				"log_tail": logTail,
			},
		})
		return
	}

	pkgs := countUpgradedPackages(string(output))
	log.Info("patch deployment succeeded",
		zap.String("job_id", jobID),
		zap.Int("packages_upgraded", pkgs),
	)
	enqueueCompletedAction(completedAction{
		Action: jobType,
		JobID:  jobID,
		Status: "succeeded",
		Metadata: map[string]any{
			"packages_upgraded": pkgs,
			"log_tail":          logTail,
		},
	})
}

// patchCommandForOS picks the right package manager invocation per OS.
// Returns (binary, args, human-readable-label, ok). ok=false when the
// platform isn't supported (the caller reports failure rather than
// silently no-op'ing — operators want to see "macOS not supported" in the
// UI rather than a phantom success).
//
// On Linux we prefer apt-get when available, fall back to dnf/yum. We do
// NOT chain the two; if /usr/bin/apt-get exists, that's the package
// manager — same node won't have both as primary. A fancier detector
// (read /etc/os-release) is overkill for the MVP.
func patchCommandForOS() (cmd string, args []string, label string, ok bool) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("apt-get"); err == nil {
			return "/bin/sh", []string{"-c",
				"apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get -y -qq -o Dpkg::Options::=--force-confold upgrade",
			}, "apt-get update + upgrade", true
		}
		if _, err := exec.LookPath("dnf"); err == nil {
			return "dnf", []string{"-y", "--quiet", "upgrade"}, "dnf -y upgrade", true
		}
		if _, err := exec.LookPath("yum"); err == nil {
			return "yum", []string{"-y", "--quiet", "update"}, "yum -y update", true
		}
		return "", nil, "", false
	case "windows":
		if _, err := exec.LookPath("winget"); err == nil {
			return "winget", []string{
				"upgrade", "--all",
				"--silent",
				"--accept-source-agreements",
				"--accept-package-agreements",
				"--disable-interactivity",
			}, "winget upgrade --all", true
		}
		return "", nil, "", false
	default:
		return "", nil, "", false
	}
}

// countUpgradedPackages parses package-manager output to extract a rough
// upgraded-count. Best-effort — when the parse fails (unknown format,
// malformed output) we return 0 and rely on the log tail.
//
// Patterns recognised:
//   - apt: "X upgraded, Y newly installed"
//   - dnf/yum: "Upgraded:" stanza followed by N package lines (we count
//     the lines that look like name-version.arch).
//   - winget: "Successfully installed" lines (one per package).
func countUpgradedPackages(out string) int {
	if n := parseAptUpgradedCount(out); n > 0 {
		return n
	}
	if n := parseDnfUpgradedCount(out); n > 0 {
		return n
	}
	if n := parseWingetUpgradedCount(out); n > 0 {
		return n
	}
	return 0
}

func parseAptUpgradedCount(out string) int {
	for _, line := range strings.Split(out, "\n") {
		// Format: "5 upgraded, 0 newly installed, 0 to remove and 1 not upgraded."
		if idx := strings.Index(line, " upgraded,"); idx > 0 {
			prefix := strings.TrimSpace(line[:idx])
			var n int
			if _, err := fmt.Sscanf(prefix, "%d", &n); err == nil {
				return n
			}
		}
	}
	return 0
}

func parseDnfUpgradedCount(out string) int {
	// dnf prints "Upgrade  X Packages" near the end of the transaction
	// summary. We grep for that pattern.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Upgrade") && strings.HasSuffix(line, "Packages") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				var n int
				if _, err := fmt.Sscanf(fields[1], "%d", &n); err == nil {
					return n
				}
			}
		}
	}
	return 0
}

func parseWingetUpgradedCount(out string) int {
	// winget prints one "Successfully installed" per upgraded package.
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Successfully installed") {
			count++
		}
	}
	return count
}

// tailString returns the last n bytes of s, prefixed with "…\n" if it
// truncated. Keeps log_tail bounded so heartbeat payloads don't bloat.
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…\n" + s[len(s)-n:]
}

// fetchPatchJobDetail fetches /api/v1/jobs/<id> and decodes the payload.
// Mirrors fetchFirewallJobDetail in firewall_exec.go — same job envelope
// shape; only the inner payload differs.
func fetchPatchJobDetail(ctx context.Context, client *api.Client, jobID string) (patchActionDetail, error) {
	var detail patchActionDetail
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
		return detail, fmt.Errorf("decode patch payload: %w", err)
	}
	return detail, nil
}
