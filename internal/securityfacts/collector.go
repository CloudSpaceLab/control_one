// Package securityfacts collects host security posture facts by running
// lightweight shell commands locally. Results are returned as a flat
// map[string]string of "true"/"false" values that the compliance engine
// merges into the evaluate payload as runtime facts.
//
// All checks have a 5-second timeout and fail-safe to "false" (unknown),
// so a missing tool never panics or blocks the agent.
package securityfacts

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Collect runs all security fact checks and returns a flat key→value map.
// Keys use the "security.*" namespace. Values are always "true" or "false".
// Safe to call on any Linux host; silently returns "false" for unavailable tools.
func Collect(ctx context.Context) map[string]string {
	c := &collector{ctx: ctx}
	return c.collect()
}

type collector struct {
	ctx context.Context
}

// shell runs a shell command and returns true if it exits 0.
func (c *collector) shell(cmd string) bool {
	tctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	err := exec.CommandContext(tctx, "sh", "-c", cmd).Run()
	return err == nil
}

// shellOut runs a shell command and returns trimmed stdout.
func (c *collector) shellOut(cmd string) string {
	tctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(tctx, "sh", "-c", cmd).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func tf(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (c *collector) collect() map[string]string {
	f := make(map[string]string, 32)

	// ── fail2ban ──────────────────────────────────────────────────────────────
	f2bInstalled := c.shell("command -v fail2ban-client >/dev/null 2>&1")
	f2bActive := f2bInstalled && c.shell("fail2ban-client ping >/dev/null 2>&1")
	f["security.fail2ban.installed"] = tf(f2bInstalled)
	f["security.fail2ban.active"] = tf(f2bActive)

	// ── firewall ──────────────────────────────────────────────────────────────
	ufwEnabled := c.shell("ufw status 2>/dev/null | grep -q 'Status: active'")
	firewalldRunning := c.shell("systemctl is-active --quiet firewalld 2>/dev/null")
	// iptables: non-trivial ruleset means at least one non-ACCEPT INPUT rule
	iptablesRules := c.shell(
		"iptables -L INPUT -n 2>/dev/null | grep -vqE '^(Chain|target|ACCEPT.*0\\.0\\.0\\.0|$)'")
	anyFirewall := ufwEnabled || firewalldRunning || iptablesRules
	f["security.firewall.ufw.enabled"] = tf(ufwEnabled)
	f["security.firewall.firewalld.running"] = tf(firewalldRunning)
	f["security.firewall.iptables.has_rules"] = tf(iptablesRules)
	f["security.firewall.any_enabled"] = tf(anyFirewall)

	// ── SSH config ────────────────────────────────────────────────────────────
	// Password auth enabled? (bad)
	sshPwAuth := c.shell(
		`grep -qE '^PasswordAuthentication[[:space:]]+yes' /etc/ssh/sshd_config 2>/dev/null`)
	// Root login enabled? (bad)
	sshRootLogin := c.shell(
		`grep -qE '^PermitRootLogin[[:space:]]+yes' /etc/ssh/sshd_config 2>/dev/null`)
	// Still on default port 22?
	sshDefaultPort := c.shell("ss -tlnp 2>/dev/null | grep -q ':22 '")
	f["security.ssh.password_auth"] = tf(sshPwAuth)
	f["security.ssh.root_login"] = tf(sshRootLogin)
	f["security.ssh.default_port"] = tf(sshDefaultPort)

	// ── open ports that should normally be closed on internet-facing hosts ────
	portOpen := func(port string) bool {
		return c.shell("ss -tlnp 2>/dev/null | grep -q ':" + port + " '")
	}
	f["security.port.21.open"] = tf(portOpen("21"))       // FTP
	f["security.port.23.open"] = tf(portOpen("23"))       // Telnet
	f["security.port.25.open"] = tf(portOpen("25"))       // SMTP
	f["security.port.110.open"] = tf(portOpen("110"))     // POP3
	f["security.port.143.open"] = tf(portOpen("143"))     // IMAP
	f["security.port.3306.open"] = tf(portOpen("3306"))   // MySQL / MariaDB
	f["security.port.5432.open"] = tf(portOpen("5432"))   // PostgreSQL
	f["security.port.6379.open"] = tf(portOpen("6379"))   // Redis
	f["security.port.27017.open"] = tf(portOpen("27017")) // MongoDB
	f["security.port.9200.open"] = tf(portOpen("9200"))   // Elasticsearch / OpenSearch
	f["security.port.3389.open"] = tf(portOpen("3389"))   // RDP
	f["security.port.5900.open"] = tf(portOpen("5900"))   // VNC

	// ── automatic security updates ────────────────────────────────────────────
	unattendedUpgrades := c.shell("command -v unattended-upgrades >/dev/null 2>&1") ||
		c.shell("systemctl is-enabled --quiet unattended-upgrades 2>/dev/null")
	f["security.unattended_upgrades.installed"] = tf(unattendedUpgrades)

	// ── mandatory access control (SELinux or AppArmor) ────────────────────────
	selinux := c.shell("getenforce 2>/dev/null | grep -qiE 'Enforcing|Permissive'")
	apparmor := c.shell("aa-status --enabled 2>/dev/null") ||
		c.shell("systemctl is-active --quiet apparmor 2>/dev/null")
	f["security.selinux.enabled"] = tf(selinux)
	f["security.apparmor.enabled"] = tf(apparmor)
	f["security.mac.enabled"] = tf(selinux || apparmor)

	// ── /etc/shadow permissions ───────────────────────────────────────────────
	// Should be 640 or tighter; world-readable = bad
	shadowPerms := c.shellOut("stat -c '%a' /etc/shadow 2>/dev/null")
	shadowWorldReadable := strings.HasSuffix(shadowPerms, "4") ||
		strings.HasSuffix(shadowPerms, "5") ||
		strings.HasSuffix(shadowPerms, "6") ||
		strings.HasSuffix(shadowPerms, "7")
	f["security.shadow.world_readable"] = tf(shadowWorldReadable)

	// ── nmap available (network scanning capability) ──────────────────────────
	f["security.nmap.installed"] = tf(c.shell("command -v nmap >/dev/null 2>&1"))

	// ── audit daemon ─────────────────────────────────────────────────────────
	f["security.auditd.running"] = tf(c.shell("systemctl is-active --quiet auditd 2>/dev/null"))

	// ── no-auth Redis (accessible without password) ───────────────────────────
	// Only check if Redis port is open
	if portOpen("6379") {
		redisNoAuth := c.shell(
			`redis-cli -h 127.0.0.1 ping 2>/dev/null | grep -q PONG`)
		f["security.redis.no_auth"] = tf(redisNoAuth)
	} else {
		f["security.redis.no_auth"] = "false"
	}

	return f
}
