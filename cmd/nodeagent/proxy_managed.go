package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type managedProxyMarker struct {
	ProxyID      string `json:"proxy_id"`
	NodeID       string `json:"node_id"`
	ConfigSHA256 string `json:"config_sha256"`
	Original     []byte `json:"original_configuration"`
	OriginalMode uint32 `json:"original_mode"`
	Removed      bool   `json:"removed"`
	MaskOwned    bool   `json:"mask_owned"`
}
type managedProxyExecutor struct {
	ConfigPath, MarkerPath string
	Run                    func(context.Context, string, ...string) error
	Ready                  func(context.Context, []string) error
	Stopped                func(context.Context) error
	Version                func(context.Context) (string, error)
}

func managedProxyAvailable() bool {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || os.Geteuid() != 0 || agentPatchJournal == nil || !lookHas("systemctl") || !lookHas("apt-get") {
		return false
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil || !strings.Contains("\n"+string(data), "\nID=ubuntu\n") || !strings.Contains(string(data), "VERSION_ID=\"24.04\"") {
		return false
	}
	info, err := os.Stat("/run/systemd/system")
	return err == nil && info.IsDir()
}
func managedProxyRun(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, tailString(string(out), 2048))
	}
	return nil
}
func proxyUpstreamReady(ctx context.Context, probes []string) error {
	if err := managedProxyRun(ctx, "systemctl", "is-active", "--quiet", "squid.service"); err != nil {
		return err
	}
	if err := squidReady(ctx); err != nil {
		return err
	}
	proxy, _ := url.Parse("http://127.0.0.1:3128")
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), TLSHandshakeTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return probeProxyRepositories(ctx, client, probes)
}

// A redirect proves neither access to the reviewed resource nor repository
// readiness. Copy the client so callers cannot accidentally enable following.
func probeProxyRepositories(ctx context.Context, client *http.Client, probes []string) error {
	if client == nil || len(probes) == 0 {
		return fmt.Errorf("repository probe client and reviewed URLs required")
	}
	bounded := *client
	bounded.Timeout = 10 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, raw := range probes {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
		if err != nil {
			return err
		}
		resp, err := bounded.Do(req)
		if err != nil {
			return fmt.Errorf("approved HTTPS repository probe failed: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("repository probe returned HTTP %d", resp.StatusCode)
		}
	}
	return nil
}
func proxyListenerStopped(ctx context.Context) error {
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(probe, "tcp", "127.0.0.1:3128")
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("proxy listener still active")
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return nil
	}
	return fmt.Errorf("cannot verify listener closure: %w", err)
}
func proxyAtomicFile(path string, body []byte, mode os.FileMode) error {
	var previous os.FileInfo
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("managed file must not be a symlink or special file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		previous = info
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".controlone-proxy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if previous != nil {
		err = preserveProxyOwner(f, previous)
	}
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		_, err = f.Write(body)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncPatchJournalDirectory(filepath.Dir(path))
}
func proxyReadConfig(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("regular Squid configuration at most 1 MiB required")
	}
	return os.ReadFile(path)
}
func (e managedProxyExecutor) Execute(ctx context.Context, action string, p patchsupport.ProxyLifecycle) (proof patchsupport.ProxyLifecycleProof, err error) {
	if p.Revision <= 0 {
		return proof, fmt.Errorf("managed proxy revision required")
	}
	if err = p.Validate(action, time.Now().UTC()); err != nil {
		return proof, err
	}
	var marker managedProxyMarker
	retiredTransfer := false
	if info, statErr := os.Lstat(e.MarkerPath); statErr == nil {
		if !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return proof, fmt.Errorf("invalid ownership marker")
		}
		data, readErr := os.ReadFile(e.MarkerPath)
		if readErr != nil {
			return proof, readErr
		}
		if json.Unmarshal(data, &marker) != nil {
			return proof, fmt.Errorf("invalid proxy ownership marker")
		}
		retiredTransfer = marker.Removed && marker.NodeID == p.NodeID && marker.ProxyID != p.ProxyID && action == "squid.reconfigure"
		if (marker.ProxyID != p.ProxyID || marker.NodeID != p.NodeID || marker.Removed) && !retiredTransfer {
			return proof, fmt.Errorf("proxy ownership marker mismatch or service already removed")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return proof, statErr
	}
	if _, maskErr := os.Lstat("/etc/systemd/system/squid.service"); maskErr == nil && !marker.MaskOwned {
		return proof, fmt.Errorf("existing systemd override/mask requires operator review")
	}
	if _, maskErr := os.Lstat("/run/systemd/system/squid.service"); maskErr == nil {
		return proof, fmt.Errorf("existing runtime service override requires operator review")
	}
	if marker.MaskOwned {
		if target, readErr := os.Readlink("/etc/systemd/system/squid.service"); readErr == nil && target != "/dev/null" {
			return proof, fmt.Errorf("owned service mask was replaced; operator review required")
		}
	}
	protectMask := marker.MaskOwned
	stateCaptured, wasActive, wasEnabled := false, false, false
	defer func() {
		if err == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if protectMask {
			err = errors.Join(err, e.Run(cleanup, "systemctl", "stop", "squid.service"), e.Run(cleanup, "systemctl", "mask", "squid.service"))
			return
		}
		if stateCaptured {
			if !wasActive {
				err = errors.Join(err, e.Run(cleanup, "systemctl", "stop", "squid.service"))
			}
			if !wasEnabled {
				err = errors.Join(err, e.Run(cleanup, "systemctl", "disable", "squid.service"))
			}
		}
	}()
	if action == "squid.install" {
		if marker.ProxyID != "" {
			return proof, fmt.Errorf("proxy already managed; use reconfiguration")
		}
		if _, statErr := os.Lstat(e.ConfigPath); !errors.Is(statErr, os.ErrNotExist) {
			return proof, fmt.Errorf("installation refuses existing configuration; review adoption through reconfiguration")
		}
		if err = e.Run(ctx, "systemctl", "mask", "squid.service"); err != nil {
			return proof, err
		}
		protectMask = true

		if err = e.Run(ctx, "apt-get", "-o", "APT::Update::Error-Mode=any", "-o", "Acquire::AllowInsecureRepositories=false", "-o", "Acquire::AllowDowngradeToInsecureRepositories=false", "-o", "APT::Get::AllowUnauthenticated=false", "update", "-qq"); err != nil {
			return proof, err
		}
		if err = e.Run(ctx, "apt-get", "-o", "Acquire::AllowInsecureRepositories=false", "-o", "Acquire::AllowDowngradeToInsecureRepositories=false", "-o", "APT::Get::AllowUnauthenticated=false", "-y", "--no-remove", "--no-upgrade", "--no-install-recommends", "install", "squid="+p.SquidVersion); err != nil {
			return proof, err
		}
		if e.Version == nil {
			return proof, fmt.Errorf("installed-version verifier unavailable")
		}
		version, versionErr := e.Version(ctx)
		if versionErr != nil || version != p.SquidVersion {
			return proof, fmt.Errorf("installed Squid version does not match the reviewed version")
		}
	}
	current, err := proxyReadConfig(e.ConfigPath)
	if err != nil {
		return proof, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(current))
	carryMask := false
	if retiredTransfer {
		if digest != fmt.Sprintf("%x", sha256.Sum256(marker.Original)) {
			return proof, fmt.Errorf("retired proxy baseline changed; manual reconciliation required")
		}
		oldID, parseErr := uuid.Parse(marker.ProxyID)
		if parseErr != nil {
			return proof, fmt.Errorf("invalid retired ownership identity")
		}
		archive := filepath.Join(filepath.Dir(e.MarkerPath), "managed-proxy-retired-"+oldID.String()+".json")
		encoded, _ := json.Marshal(marker)
		f, archiveErr := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if archiveErr == nil {
			_, archiveErr = f.Write(encoded)
			if archiveErr == nil {
				archiveErr = f.Sync()
			}
			archiveErr = errors.Join(archiveErr, f.Close())
			if archiveErr == nil {
				archiveErr = syncPatchJournalDirectory(filepath.Dir(archive))
			}
		} else if errors.Is(archiveErr, os.ErrExist) {
			var data []byte
			info, statErr := os.Lstat(archive)
			if statErr != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
				return proof, fmt.Errorf("invalid retired ownership archive")
			}
			data, archiveErr = os.ReadFile(archive)
			if archiveErr == nil && string(data) != string(encoded) {
				archiveErr = fmt.Errorf("retired ownership archive differs")
			}
		}
		if archiveErr != nil {
			return proof, archiveErr
		}
		carryMask = marker.MaskOwned
		marker = managedProxyMarker{}
	}
	if marker.ProxyID != "" && marker.ConfigSHA256 != digest {
		return proof, fmt.Errorf("Squid configuration drift detected; manual reconciliation required")
	}
	if action == "squid.reconfigure" {
		activeErr := e.Run(ctx, "systemctl", "is-active", "--quiet", "squid.service")
		wasActive = activeErr == nil
		var exit interface{ ExitCode() int }
		if activeErr != nil && (!errors.As(activeErr, &exit) || exit.ExitCode() != 3) {
			return proof, fmt.Errorf("cannot establish prior service state: %w", activeErr)
		}
		enabledErr := e.Run(ctx, "systemctl", "is-enabled", "--quiet", "squid.service")
		wasEnabled = enabledErr == nil
		exit = nil
		if enabledErr != nil && (!errors.As(enabledErr, &exit) || exit.ExitCode() != 1) {
			return proof, fmt.Errorf("cannot establish prior enablement state: %w", enabledErr)
		}
		stateCaptured = true
	}
	if action == "squid.remove" {
		if marker.ProxyID == "" {
			return proof, fmt.Errorf("removal requires a matching ownership marker")
		}
		if err = e.Run(ctx, "systemctl", "stop", "squid.service"); err != nil {
			return proof, err
		}
		if err = e.Run(ctx, "systemctl", "disable", "squid.service"); err != nil {
			return proof, err
		}
		if err = e.Run(ctx, "systemctl", "mask", "squid.service"); err != nil {
			return proof, err
		}
		inactive := e.Run(ctx, "systemctl", "is-active", "--quiet", "squid.service")
		var code interface{ ExitCode() int }
		if !errors.As(inactive, &code) || code.ExitCode() != 3 {
			return proof, fmt.Errorf("service inactivity is not verified: %v", inactive)
		}
		if e.Stopped == nil {
			return proof, fmt.Errorf("listener closure probe unavailable")
		}
		if err = e.Stopped(ctx); err != nil {
			return proof, err
		}
		mode := os.FileMode(marker.OriginalMode) & 0777
		if mode == 0 {
			mode = 0600
		}
		if err = proxyAtomicFile(e.ConfigPath, marker.Original, mode); err != nil {
			return proof, err
		}
		marker.Removed = true
		marker.MaskOwned = true
		if err = ctx.Err(); err != nil {
			return proof, err
		}
		body, _ := json.Marshal(marker)
		if err = proxyAtomicFile(e.MarkerPath, body, 0600); err != nil {
			return proof, err
		}
		proof.Removed = true
		proof.ObservedAt = time.Now().UTC()
		return proof, nil
	}
	if marker.ProxyID == "" {
		info, statErr := os.Lstat(e.ConfigPath)
		if statErr != nil {
			return proof, statErr
		}
		marker = managedProxyMarker{ProxyID: p.ProxyID, NodeID: p.NodeID, Original: current, OriginalMode: uint32(info.Mode().Perm()), ConfigSHA256: digest, MaskOwned: action == "squid.install" || carryMask}
		body, _ := json.Marshal(marker)
		if err = proxyAtomicFile(e.MarkerPath, body, 0600); err != nil {
			return proof, err
		}
	}
	body, err := patchsupport.ManagedProxyConfig(p.Whitelist, p.ClientCIDRs)
	if err != nil {
		return proof, err
	}
	activations := 0
	run := func(ctx context.Context, args ...string) error {
		if len(args) > 1 && args[1] == "parse" {
			return e.Run(ctx, "squid", args...)
		}
		activations++
		if protectMask && activations > 1 {
			return fmt.Errorf("unverified installation remains masked after activation failure")
		}
		if marker.MaskOwned {
			if err := e.Run(ctx, "systemctl", "unmask", "squid.service"); err != nil {
				return err
			}
		}
		if err := e.Run(ctx, "systemctl", "enable", "squid.service"); err != nil {
			return err
		}
		return e.Run(ctx, "systemctl", "restart", "squid.service")
	}
	if err = replaceSquidConfig(ctx, e.ConfigPath, body, run, func(ctx context.Context) error { return e.Ready(ctx, p.ProbeURLs) }); err != nil {
		return proof, err
	}
	marker.ConfigSHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
	marker.MaskOwned = false
	encoded, _ := json.Marshal(marker)
	if err = proxyAtomicFile(e.MarkerPath, encoded, 0600); err != nil {
		return proof, err
	}
	if err = ctx.Err(); err != nil {
		return proof, err
	}
	proof = patchsupport.ProxyLifecycleProof{ConfigSHA256: marker.ConfigSHA256, ServiceActive: true, UpstreamVerified: true, ObservedAt: time.Now().UTC()}
	return proof, nil
}
