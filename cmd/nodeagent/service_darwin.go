//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const launchdServiceLabel = "com.cloudspacelab.controlone"

func init() {
	uninstallServiceHook = uninstallService
}

// installService registers the Control One agent as a launchd service on
// macOS. When executed as root (EUID 0) the plist is written to
// /Library/LaunchDaemons/ so the agent runs system-wide; otherwise it is
// written to ~/Library/LaunchAgents/ so a developer-mode install can happen
// without sudo. The plist is loaded via `launchctl load -w`, which enables it
// at boot and starts it immediately.
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
    <key>StandardOutPath</key>
    <string>/var/log/control-one/nodeagent/stdout.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/control-one/nodeagent/stderr.log</string>
</dict>
</plist>
`, launchdServiceLabel, binaryPath, configPath)

	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// `launchctl load -w` rejects an already-loaded unit with a non-zero exit,
	// so unload first (ignoring errors) for idempotency.
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	if err := exec.Command("launchctl", "load", "-w", plistPath).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
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

	// Unload tolerates a missing or already-unloaded plist; we ignore errors.
	_ = exec.Command("launchctl", "unload", plistPath).Run()

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
