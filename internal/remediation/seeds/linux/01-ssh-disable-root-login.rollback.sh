#!/usr/bin/env bash
# Rollback for CIS 5.2.7 — restore original sshd_config
set -euo pipefail

CONFIG="/etc/ssh/sshd_config"
BACKUP="${CONFIG}.controlone.bak"

if [[ -f "$BACKUP" ]]; then
    cp -p "$BACKUP" "$CONFIG"
    rm -f "$BACKUP"
    sshd -t
    systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || service ssh reload
    echo "sshd_config restored from backup"
else
    # No backup: explicitly set PermitRootLogin prohibit-password (OpenSSH default).
    sed -i -E '/^\s*#?\s*PermitRootLogin\s+/d' "$CONFIG"
    echo "PermitRootLogin prohibit-password" >> "$CONFIG"
    sshd -t
    systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || service ssh reload
    echo "PermitRootLogin reverted to prohibit-password"
fi
