//go:build darwin

package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchdServiceLabel = "com.cloudspacelab.controlone"

func init() {
	uninstallServiceHook = uninstallService
}

// installService registers the Control One agent as a launchd service on
// macOS. When executed as root (EUID 0) the plist is written to
// /Library/LaunchDaemons/ so the agent runs system-wide; otherwise it is
// written to ~/Library/LaunchAgents/ so a developer-mode install can happen
// without sudo. Modern launchctl uses bootstrap/bootout; load/unload remains
// as a fallback for older supported macOS releases.
func installService(configPath string) error {
	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "/usr/local/bin/controlone-agent"
	}

	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create launchd dir: %w", err)
	}
	logDir, err := launchdLogDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create launchd log dir: %w", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--config</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ProcessType</key>
    <string>Background</string>
    <key>LowPriorityIO</key>
    <true/>
    <key>Nice</key>
    <integer>5</integer>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
</dict>
</plist>
`, launchdServiceLabel, escapePlistString(binaryPath), escapePlistString(configPath),
		escapePlistString(filepath.Join(logDir, "stdout.log")), escapePlistString(filepath.Join(logDir, "stderr.log")))

	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	target, err := launchdServiceTarget()
	if err != nil {
		return err
	}

	// bootout is intentionally best-effort: a fresh install has no existing
	// job, while a re-install must discard the old definition before bootstrap.
	_ = runLaunchctl("bootout", target)
	if err := runLaunchctl("bootstrap", launchdDomain(), plistPath); err == nil {
		if err := runLaunchctl("kickstart", "-k", target); err != nil {
			return fmt.Errorf("launchctl kickstart: %w", err)
		}
		return nil
	} else if legacyErr := runLaunchctl("load", "-w", plistPath); legacyErr != nil {
		return fmt.Errorf("launchctl bootstrap failed: %v; legacy load fallback failed: %w", err, legacyErr)
	}

	return nil
}

// uninstallService unloads and deletes the launchd plist. Missing plists are
// tolerated so repeated uninstalls succeed. The CLI subcommand that exposes
// this to operators calls uninstallServiceHook, which is wired during init.
func uninstallService() error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	if target, targetErr := launchdServiceTarget(); targetErr == nil {
		// bootout tolerates a missing or already-unloaded job; we ignore errors.
		_ = runLaunchctl("bootout", target)
	}
	// Fallback cleanup for jobs loaded by legacy installers.
	_ = runLaunchctl("unload", plistPath)

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	return nil
}

// launchdPlistPath returns the location to write the plist: system-wide
// (/Library/LaunchDaemons) when running as root, otherwise per-user
// (~/Library/LaunchAgents).
func launchdPlistPath() (string, error) {
	filename := launchdServiceLabel + ".plist"
	if os.Geteuid() == 0 {
		return filepath.Join("/Library/LaunchDaemons", filename), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", filename), nil
}

func launchdDomain() string {
	if os.Geteuid() == 0 {
		return "system"
	}
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func launchdServiceTarget() (string, error) {
	domain := launchdDomain()
	if domain == "" {
		return "", fmt.Errorf("resolve launchd domain")
	}
	return domain + "/" + launchdServiceLabel, nil
}

func launchdLogDir() (string, error) {
	if os.Geteuid() == 0 {
		return "/var/log/control-one/nodeagent", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir for launchd logs: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "ControlOne"), nil
}

func runLaunchctl(args ...string) error {
	output, err := exec.Command("launchctl", args...).CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}

func escapePlistString(value string) string {
	var out strings.Builder
	if err := xml.EscapeText(&out, []byte(value)); err != nil {
		return value
	}
	return out.String()
}
