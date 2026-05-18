//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
)

const (
	systemdUnitPath  = "/etc/systemd/system/controlone-agent.service"
	openrcScriptPath = "/etc/init.d/controlone-agent"
	sysvScriptPath   = "/etc/init.d/controlone-agent"
)

func init() {
	uninstallServiceHook = uninstallService
}

// installService registers the Control One agent with the host init system
// (systemd by default; OpenRC and SysV init are also supported when systemd
// is absent) and starts it.
func installService(configPath string) error {
	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "/usr/local/bin/controlone-agent"
	}

	switch detectInitSystem() {
	case initOpenRC:
		return installOpenRC(binaryPath, configPath)
	case initSysV:
		return installSysV(binaryPath, configPath)
	default:
		return installSystemd(binaryPath, configPath)
	}
}

// uninstallService stops the agent and removes whichever init integration is
// in use. Missing units/scripts are tolerated so the call stays idempotent.
func uninstallService() error {
	switch detectInitSystem() {
	case initOpenRC:
		return uninstallOpenRC()
	case initSysV:
		return uninstallSysV()
	default:
		return uninstallSystemd()
	}
}

type initSystem int

const (
	initSystemd initSystem = iota
	initOpenRC
	initSysV
)

func detectInitSystem() initSystem {
	if override := os.Getenv("CONTROLONE_INIT_SYSTEM"); override != "" {
		switch override {
		case "systemd":
			return initSystemd
		case "openrc":
			return initOpenRC
		case "sysv":
			return initSysV
		}
	}
	if _, err := os.Stat("/run/openrc"); err == nil {
		return initOpenRC
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		if _, err := os.Stat("/run/systemd/system"); err == nil {
			return initSystemd
		}
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		return initSystemd
	}
	if _, err := exec.LookPath("rc-service"); err == nil {
		return initOpenRC
	}
	return initSysV
}

func installSystemd(binaryPath, configPath string) error {
	unit := systemdUnit(binaryPath, configPath)

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

func uninstallSystemd() error {
	_ = exec.Command("systemctl", "disable", "--now", "controlone-agent").Run()
	if err := os.Remove(systemdUnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove service file: %w", err)
	}
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return nil
}

func installOpenRC(binaryPath, configPath string) error {
	script := fmt.Sprintf(`#!/sbin/openrc-run
name="controlone-agent"
description="Control One Node Agent"
command=%q
command_args="--config %s"
command_background=yes
pidfile="/run/controlone-agent.pid"

depend() {
    need net
    after firewall
}
`, binaryPath, configPath)

	if err := os.WriteFile(openrcScriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("write openrc script: %w", err)
	}
	if err := exec.Command("rc-update", "add", "controlone-agent", "default").Run(); err != nil {
		return fmt.Errorf("rc-update add: %w", err)
	}
	if err := exec.Command("rc-service", "controlone-agent", "start").Run(); err != nil {
		return fmt.Errorf("rc-service start: %w", err)
	}
	return nil
}

func uninstallOpenRC() error {
	_ = exec.Command("rc-service", "controlone-agent", "stop").Run()
	_ = exec.Command("rc-update", "del", "controlone-agent", "default").Run()
	if err := os.Remove(openrcScriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove openrc script: %w", err)
	}
	return nil
}

func installSysV(binaryPath, configPath string) error {
	script := fmt.Sprintf(`#!/bin/sh
### BEGIN INIT INFO
# Provides:          controlone-agent
# Required-Start:    $network $remote_fs
# Required-Stop:     $network $remote_fs
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: Control One Node Agent
### END INIT INFO

DAEMON=%q
ARGS="--config %s"
PIDFILE=/var/run/controlone-agent.pid

case "$1" in
  start)
    echo "Starting controlone-agent"
    start-stop-daemon --start --background --make-pidfile --pidfile "$PIDFILE" --exec "$DAEMON" -- $ARGS
    ;;
  stop)
    echo "Stopping controlone-agent"
    start-stop-daemon --stop --pidfile "$PIDFILE"
    rm -f "$PIDFILE"
    ;;
  restart)
    $0 stop
    $0 start
    ;;
  status)
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      echo "running"
    else
      echo "stopped"
    fi
    ;;
  *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac
`, binaryPath, configPath)

	if err := os.WriteFile(sysvScriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("write sysv script: %w", err)
	}
	// update-rc.d is Debian/Ubuntu; chkconfig is RHEL-family. Try both.
	_ = exec.Command("update-rc.d", "controlone-agent", "defaults").Run()
	_ = exec.Command("chkconfig", "--add", "controlone-agent").Run()
	if err := exec.Command(sysvScriptPath, "start").Run(); err != nil {
		return fmt.Errorf("sysv start: %w", err)
	}
	return nil
}

func uninstallSysV() error {
	_ = exec.Command(sysvScriptPath, "stop").Run()
	_ = exec.Command("update-rc.d", "-f", "controlone-agent", "remove").Run()
	_ = exec.Command("chkconfig", "--del", "controlone-agent").Run()
	if err := os.Remove(sysvScriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove sysv script: %w", err)
	}
	return nil
}
