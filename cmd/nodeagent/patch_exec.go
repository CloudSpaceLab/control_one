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

// Patch job type strings — duplicated agent-side so the dispatcher can
// branch without importing the controlplane server package.
const (
	patchJobDirect    = "patch.deploy_direct"
	patchJobProxy     = "patch.deploy_proxy"
	patchJobAirgapped = "patch.deploy_airgapped"
	patchJobInventory = "patch.inventory_scan"
)

// patchActionDetail mirrors the server-side patchJobPayload. The id fields
// drive heartbeat correlation; ProxyURL / StagedRepoPath are mode-specific.
type patchActionDetail struct {
	NodePatchStateID string   `json:"node_patch_state_id"`
	NodeID           string   `json:"node_id"`
	DeploymentID     string   `json:"deployment_id"`
	Mode             string   `json:"mode"`
	PackageAllowlist []string `json:"package_allowlist,omitempty"`
	PackageDenylist  []string `json:"package_denylist,omitempty"`
	PostPatchRescan  bool     `json:"post_patch_rescan,omitempty"`

	// Proxy mode.
	ProxyURL string `json:"proxy_url,omitempty"`

	// Airgapped mode — pre-staged repository sources file.
	StagedRepoPath string `json:"staged_repo_path,omitempty"`
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

	// Inventory scan — read-only, separate code path.
	if jobType == patchJobInventory {
		executePatchInventory(ctx, log, jobType, jobID)
		return
	}

	// Resolve the effective job-type from detail.Mode so we route correctly
	// even when the controlplane heartbeat encoded everything as
	// patch.deploy_direct (back-compat with the PR #30 dispatch path).
	effectiveJobType := jobType
	switch detail.Mode {
	case "proxy":
		effectiveJobType = patchJobProxy
	case "airgapped":
		effectiveJobType = patchJobAirgapped
	case "direct":
		effectiveJobType = patchJobDirect
	}
	if err := validatePatchPackagePolicy(detail); err != nil {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  err.Error(),
		})
		return
	}

	cmdName, args, env, label, ok := patchCommandForJob(effectiveJobType, detail)
	if !ok {
		log.Warn("no patch command for platform / mode",
			zap.String("os", runtime.GOOS),
			zap.String("job_type", jobType),
		)
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("no patch command available for %s/%s", runtime.GOOS, jobType),
		})
		return
	}

	execCtx, cancel := context.WithTimeout(ctx, patchTimeout)
	defer cancel()

	log.Info("running patch deployment",
		zap.String("job_id", jobID),
		zap.String("deployment_id", detail.DeploymentID),
		zap.String("mode", detail.Mode),
		zap.String("command", label),
	)
	cmd := exec.CommandContext(execCtx, cmdName, args...) // #nosec G204 — args are static per-OS / mode.
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
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
			"post_patch_rescan": detail.PostPatchRescan,
		},
	})
}

// patchCommandForJob picks the package manager invocation appropriate for
// the (job type, OS) pair. Returns (binary, args, env, label, ok). Env is a
// list of "KEY=VALUE" appended to cmd.Env to inject HTTP_PROXY/HTTPS_PROXY in
// proxy mode. ok=false signals the platform isn't supported.
//
// Direct  — apt-get / dnf / yum / winget upgrade in place.
// Proxy   — same commands, but HTTP_PROXY/HTTPS_PROXY exported from
//
//	detail.ProxyURL so apt/dnf route through the managed Squid.
//
// Airgapped — apt-get only: -o Dir::Etc::SourceList=<staged> reads from a
//
//	pre-staged repo file dropped on the node by the bundle. Other
//	OSes are not supported in this mode (operators must use proxy).
func patchCommandForJob(jobType string, detail patchActionDetail) (cmd string, args []string, env []string, label string, ok bool) {
	switch jobType {
	case patchJobAirgapped:
		// Airgapped is Linux/apt only for this iteration. dnf/yum airgapped
		// flows can be added later by extending this branch.
		if runtime.GOOS != "linux" {
			return "", nil, nil, "", false
		}
		if _, err := exec.LookPath("apt-get"); err != nil {
			return "", nil, nil, "", false
		}
		staged := detail.StagedRepoPath
		if strings.TrimSpace(staged) == "" {
			return "", nil, nil, "", false
		}
		if allowlist := normalizePatchPackageList(detail.PackageAllowlist); len(allowlist) > 0 {
			return "/bin/sh", []string{"-c", fmt.Sprintf(
				"apt-get -o Dir::Etc::SourceList=%s update -qq && DEBIAN_FRONTEND=noninteractive apt-get -o Dir::Etc::SourceList=%s -y -qq -o Dpkg::Options::=--force-confold install --only-upgrade %s",
				staged, staged, shellQuoteArgs(allowlist),
			)}, nil, "apt-get airgapped allowlist (staged: " + staged + ")", true
		}
		return "/bin/sh", []string{"-c", fmt.Sprintf(
			"apt-get -o Dir::Etc::SourceList=%s update -qq && DEBIAN_FRONTEND=noninteractive apt-get -o Dir::Etc::SourceList=%s -y -qq -o Dpkg::Options::=--force-confold upgrade",
			staged, staged,
		)}, nil, "apt-get airgapped (staged: " + staged + ")", true

	case patchJobProxy:
		// Proxy mode: pass HTTP_PROXY/HTTPS_PROXY env to the package manager.
		// Falls back to direct if no proxy URL (server side should reject this).
		if detail.ProxyURL == "" {
			return "", nil, nil, "", false
		}
		envProxy := []string{
			"http_proxy=" + detail.ProxyURL,
			"https_proxy=" + detail.ProxyURL,
			"HTTP_PROXY=" + detail.ProxyURL,
			"HTTPS_PROXY=" + detail.ProxyURL,
		}
		// Reuse the direct command tables but with proxy env injected.
		base, baseArgs, _, baseLabel, baseOK := patchCommandForJob(patchJobDirect, detail)
		if !baseOK {
			return "", nil, nil, "", false
		}
		return base, baseArgs, envProxy, baseLabel + " (via proxy " + detail.ProxyURL + ")", true

	default: // patchJobDirect or anything else falls back to direct.
		allowlist := normalizePatchPackageList(detail.PackageAllowlist)
		switch runtime.GOOS {
		case "linux":
			if _, err := exec.LookPath("apt-get"); err == nil {
				if len(allowlist) > 0 {
					return "/bin/sh", []string{"-c",
						"apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get -y -qq -o Dpkg::Options::=--force-confold install --only-upgrade " + shellQuoteArgs(allowlist),
					}, nil, "apt-get update + install --only-upgrade allowlist", true
				}
				return "/bin/sh", []string{"-c",
					"apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get -y -qq -o Dpkg::Options::=--force-confold upgrade",
				}, nil, "apt-get update + upgrade", true
			}
			if _, err := exec.LookPath("dnf"); err == nil {
				if len(allowlist) > 0 {
					return "dnf", append([]string{"-y", "--quiet", "upgrade"}, allowlist...), nil, "dnf -y upgrade allowlist", true
				}
				return "dnf", []string{"-y", "--quiet", "upgrade"}, nil, "dnf -y upgrade", true
			}
			if _, err := exec.LookPath("yum"); err == nil {
				if len(allowlist) > 0 {
					return "yum", append([]string{"-y", "--quiet", "update"}, allowlist...), nil, "yum -y update allowlist", true
				}
				return "yum", []string{"-y", "--quiet", "update"}, nil, "yum -y update", true
			}
			return "", nil, nil, "", false
		case "windows":
			if _, err := exec.LookPath("winget"); err == nil {
				if len(allowlist) > 0 {
					return "", nil, nil, "", false
				}
				return "winget", []string{
					"upgrade", "--all",
					"--silent",
					"--accept-source-agreements",
					"--accept-package-agreements",
					"--disable-interactivity",
				}, nil, "winget upgrade --all", true
			}
			return "", nil, nil, "", false
		default:
			return "", nil, nil, "", false
		}
	}
}

func validatePatchPackagePolicy(detail patchActionDetail) error {
	allowlist := normalizePatchPackageList(detail.PackageAllowlist)
	denylist := normalizePatchPackageList(detail.PackageDenylist)
	for _, allow := range allowlist {
		for _, deny := range denylist {
			if strings.EqualFold(allow, deny) {
				return fmt.Errorf("patch package %q is both allowed and denied", allow)
			}
		}
	}
	if len(denylist) > 0 && len(allowlist) == 0 {
		return fmt.Errorf("package denylist %v cannot be enforced for full-system upgrade; provide a package allowlist", denylist)
	}
	if runtime.GOOS == "windows" && len(allowlist) > 0 {
		return fmt.Errorf("package allowlist is not yet supported for winget patch jobs")
	}
	return nil
}

func normalizePatchPackageList(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func shellQuoteArgs(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+strings.ReplaceAll(value, "'", "'\"'\"'")+"'")
	}
	return strings.Join(quoted, " ")
}

// executePatchInventory runs the read-only enumeration of available
// upgrades. Output is reported back via completed_actions metadata under the
// "upgradable" key; the controlplane stores the count + delta.
func executePatchInventory(ctx context.Context, log *zap.Logger, jobType, jobID string) {
	var (
		cmdName string
		args    []string
		label   string
	)
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("apt"); err == nil {
			cmdName = "apt"
			args = []string{"list", "--upgradable"}
			label = "apt list --upgradable"
		} else if _, err := exec.LookPath("dnf"); err == nil {
			cmdName = "dnf"
			args = []string{"--quiet", "check-update"}
			label = "dnf check-update"
		} else if _, err := exec.LookPath("yum"); err == nil {
			cmdName = "yum"
			args = []string{"--quiet", "check-update"}
			label = "yum check-update"
		}
	case "windows":
		if _, err := exec.LookPath("winget"); err == nil {
			cmdName = "winget"
			args = []string{"upgrade"}
			label = "winget upgrade"
		}
	}
	if cmdName == "" {
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  fmt.Sprintf("no inventory command for %s", runtime.GOOS),
		})
		return
	}
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	log.Info("running patch inventory scan", zap.String("command", label))
	cmd := exec.CommandContext(execCtx, cmdName, args...) // #nosec G204 — args static per OS.
	output, runErr := cmd.CombinedOutput()
	upgradable := countUpgradableLines(string(output))
	if runErr != nil && runErr.Error() != "exit status 100" {
		// dnf/yum exits 100 when updates are available — treat that as success.
		enqueueCompletedAction(completedAction{
			Action: jobType,
			JobID:  jobID,
			Status: "failed",
			Error:  runErr.Error(),
			Metadata: map[string]any{
				"log_tail":   tailString(string(output), 4096),
				"upgradable": upgradable,
			},
		})
		return
	}
	enqueueCompletedAction(completedAction{
		Action: jobType,
		JobID:  jobID,
		Status: "succeeded",
		Metadata: map[string]any{
			"upgradable": upgradable,
			"log_tail":   tailString(string(output), 4096),
		},
	})
}

// countUpgradableLines parses the output of an inventory scan to extract a
// rough count of upgradable packages.
func countUpgradableLines(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Listing") || strings.HasPrefix(line, "WARNING") {
			continue
		}
		if strings.Contains(line, "/") || strings.HasPrefix(line, "Last metadata") {
			continue
		}
		// apt: "package/codename version arch [upgradable from: ...]"
		if strings.Contains(line, "[upgradable") {
			count++
			continue
		}
		// dnf/yum: "name.arch version repo"
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.Contains(fields[0], ".") {
			count++
		}
	}
	return count
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
