//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
)

const systemdUnitPath = "/etc/systemd/system/controlone-agent.service"

// installService registers the Control One agent as a systemd unit and starts
// it. Re-running against an already-installed unit rewrites the unit file
// (idempotent) and then reloads + re-enables the service, matching the
// semantics of `systemctl enable --now` on a pre-existing unit.
func installService(configPath string) error {
	// Find the agent binary path
	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "/usr/local/bin/controlone-agent"
	}

	unit := fmt.Sprintf(`[Unit]
Description=Control One Node Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s --config %s
Restart=on-failure
RestartSec=10
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`, binaryPath, configPath)

	if err := os.WriteFile(systemdUnitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "enable", "--now", "controlone-agent").Run(); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
	}

	return nil
}

// uninstallService stops the systemd unit, disables it, removes the unit file,
// and reloads systemd. Missing units are tolerated so this remains idempotent.
// The CLI subcommand that exposes this to operators is wired up by the
// installer-hardening worktree (feat/installer-idempotent-signed); this
// worktree only contributes the platform primitive.
//
//nolint:unused // wired up from the `uninstall` subcommand in a sibling worktree
func uninstallService() error {
	// `systemctl disable --now` tolerates an active or inactive unit; ignore
	// exit codes so that repeated uninstalls don't surface spurious errors.
	_ = exec.Command("systemctl", "disable", "--now", "controlone-agent").Run()

	if err := os.Remove(systemdUnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove service file: %w", err)
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}

	return nil
}
