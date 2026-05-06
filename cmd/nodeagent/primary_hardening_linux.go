//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	controlOneHardeningDir = "/etc/controlone/hardening"
	fail2banJailPath       = "/etc/fail2ban/jail.d/controlone-primary.conf"
	sysctlHardeningPath    = "/etc/sysctl.d/99-controlone-primary-hardening.conf"
)

// runPrimaryHardening applies a conservative baseline before the agent service
// starts. It is intentionally idempotent: re-enrollment should refresh the
// baseline without tearing down operator-owned firewall policy.
func runPrimaryHardening() []string {
	var warnings []string
	if os.Geteuid() != 0 {
		return []string{"primary hardening skipped: join must run as root to configure fail2ban and sysctl"}
	}

	if err := os.MkdirAll(controlOneHardeningDir, 0750); err != nil {
		warnings = append(warnings, fmt.Sprintf("create hardening dir: %v", err))
	}

	if err := ensureFail2banInstalled(); err != nil {
		warnings = append(warnings, fmt.Sprintf("install fail2ban: %v", err))
	} else {
		if err := writeFail2banJail(); err != nil {
			warnings = append(warnings, fmt.Sprintf("configure fail2ban: %v", err))
		}
		if err := enableFail2ban(); err != nil {
			warnings = append(warnings, fmt.Sprintf("start fail2ban: %v", err))
		}
	}

	if err := writeSysctlHardening(); err != nil {
		warnings = append(warnings, fmt.Sprintf("write sysctl hardening: %v", err))
	} else {
		_ = exec.Command("sysctl", "--system").Run()
	}

	if err := writePrimaryBlocklistReadme(); err != nil {
		warnings = append(warnings, fmt.Sprintf("write blocklist readme: %v", err))
	}
	return warnings
}

func ensureFail2banInstalled() error {
	if _, err := exec.LookPath("fail2ban-client"); err == nil {
		return nil
	}
	switch {
	case commandExists("apt-get"):
		_ = exec.Command("apt-get", "update").Run()
		return runInstall("apt-get", "install", "-y", "fail2ban")
	case commandExists("dnf"):
		return runInstall("dnf", "install", "-y", "fail2ban")
	case commandExists("yum"):
		return runInstall("yum", "install", "-y", "fail2ban")
	case commandExists("zypper"):
		return runInstall("zypper", "--non-interactive", "install", "fail2ban")
	case commandExists("apk"):
		return runInstall("apk", "add", "fail2ban")
	case commandExists("pacman"):
		return runInstall("pacman", "-Sy", "--noconfirm", "fail2ban")
	default:
		return fmt.Errorf("no supported package manager found")
	}
}

func runInstall(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeFail2banJail() error {
	if err := os.MkdirAll(filepath.Dir(fail2banJailPath), 0755); err != nil {
		return err
	}
	jail := `[DEFAULT]
bantime = 24h
findtime = 10m
maxretry = 3
backend = auto
ignoreip = 127.0.0.1/8 ::1

[sshd]
enabled = true
port = ssh
logpath = %(sshd_log)s
mode = aggressive
`
	return os.WriteFile(fail2banJailPath, []byte(jail), 0644)
}

func enableFail2ban() error {
	if commandExists("systemctl") {
		_ = exec.Command("systemctl", "enable", "fail2ban").Run()
		if out, err := exec.Command("systemctl", "restart", "fail2ban").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl restart fail2ban: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if commandExists("rc-service") {
		_ = exec.Command("rc-update", "add", "fail2ban", "default").Run()
		return exec.Command("rc-service", "fail2ban", "restart").Run()
	}
	if commandExists("service") {
		return exec.Command("service", "fail2ban", "restart").Run()
	}
	return nil
}

func writeSysctlHardening() error {
	if err := os.MkdirAll(filepath.Dir(sysctlHardeningPath), 0755); err != nil {
		return err
	}
	content := `# Managed by Control One primary hardening.
net.ipv4.tcp_syncookies = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv6.conf.default.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.send_redirects = 0
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1
`
	return os.WriteFile(sysctlHardeningPath, []byte(content), 0644)
}

func writePrimaryBlocklistReadme() error {
	content := `Control One primary hardening is active.

The control plane should be configured with AbuseIPDB and built-in threat feeds
so hostile peers are classified, surfaced on the node Connections tab, and
dispatched to the node firewall action channel. This host baseline installs
fail2ban immediately and leaves operator-owned allow/block policy intact.
`
	return os.WriteFile(filepath.Join(controlOneHardeningDir, "README"), []byte(content), 0644)
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
