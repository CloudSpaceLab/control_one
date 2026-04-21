#!/usr/bin/env bash
# CIS 5.2.10 — Ensure SSH PasswordAuthentication is disabled
# SOC2 CC6.1 — Logical access: require key-based auth
set -euo pipefail

CONFIG="/etc/ssh/sshd_config"
BACKUP="${CONFIG}.controlone.pwauth.bak"

if [[ ! -f "$CONFIG" ]]; then
    echo "sshd_config not found; nothing to do" >&2
    exit 0
fi

if [[ ! -f "$BACKUP" ]]; then
    cp -p "$CONFIG" "$BACKUP"
fi

sed -i -E '/^\s*#?\s*PasswordAuthentication\s+/d' "$CONFIG"
sed -i -E '/^\s*#?\s*ChallengeResponseAuthentication\s+/d' "$CONFIG"
echo "PasswordAuthentication no" >> "$CONFIG"
echo "ChallengeResponseAuthentication no" >> "$CONFIG"

sshd -t
systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || service ssh reload
echo "SSH configured for key-only authentication"
