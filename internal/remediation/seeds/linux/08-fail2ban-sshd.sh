#!/usr/bin/env bash
# CIS (host-hardening, non-numbered) — Deploy fail2ban for brute-force defence
# SOC2 CC6.6 — Detect and respond to unauthorized access attempts
set -euo pipefail

if ! command -v fail2ban-client >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y fail2ban
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y fail2ban
    elif command -v yum >/dev/null 2>&1; then
        yum install -y epel-release
        yum install -y fail2ban
    else
        echo "no supported package manager" >&2
        exit 1
    fi
fi

JAIL=/etc/fail2ban/jail.d/controlone-sshd.local
cat > "$JAIL" <<'JAIL_EOF'
[sshd]
enabled = true
port = ssh
filter = sshd
backend = systemd
maxretry = 5
findtime = 10m
bantime = 1h
JAIL_EOF

systemctl enable fail2ban
systemctl restart fail2ban
fail2ban-client status sshd >/dev/null 2>&1 || true
echo "fail2ban sshd jail active"
