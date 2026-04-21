#!/usr/bin/env bash
# CIS 5.2.16 — Ensure SSH idle timeout is configured
# SOC2 CC6.1 — Terminate idle sessions to limit unauthorized use
set -euo pipefail

CONFIG="/etc/ssh/sshd_config"
BACKUP="${CONFIG}.controlone.idle.bak"

if [[ ! -f "$CONFIG" ]]; then
    echo "sshd_config not found; nothing to do" >&2
    exit 0
fi

if [[ ! -f "$BACKUP" ]]; then
    cp -p "$CONFIG" "$BACKUP"
fi

sed -i -E '/^\s*#?\s*ClientAliveInterval\s+/d' "$CONFIG"
sed -i -E '/^\s*#?\s*ClientAliveCountMax\s+/d' "$CONFIG"
echo "ClientAliveInterval 300" >> "$CONFIG"
echo "ClientAliveCountMax 0" >> "$CONFIG"

sshd -t
systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || service ssh reload
echo "SSH idle timeout set to 300s"
