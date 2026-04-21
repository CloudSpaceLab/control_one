#!/usr/bin/env bash
# Rollback for CIS 5.2.16 — remove idle timeout directives
set -euo pipefail

CONFIG="/etc/ssh/sshd_config"
BACKUP="${CONFIG}.controlone.idle.bak"

if [[ -f "$BACKUP" ]]; then
    cp -p "$BACKUP" "$CONFIG"
    rm -f "$BACKUP"
else
    sed -i -E '/^\s*#?\s*ClientAliveInterval\s+/d' "$CONFIG"
    sed -i -E '/^\s*#?\s*ClientAliveCountMax\s+/d' "$CONFIG"
fi

sshd -t
systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || service ssh reload
echo "SSH idle timeout directives reverted"
