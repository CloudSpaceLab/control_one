#!/usr/bin/env bash
# CIS 5.2.7 — Ensure SSH root login is disabled
# SOC2 CC6.1 — Logical access: restrict privileged access
set -euo pipefail

CONFIG="/etc/ssh/sshd_config"
BACKUP="${CONFIG}.controlone.bak"

if [[ ! -f "$CONFIG" ]]; then
    echo "sshd_config not found; nothing to do" >&2
    exit 0
fi

# Back up only once per run; keep the original untouched on subsequent applies.
if [[ ! -f "$BACKUP" ]]; then
    cp -p "$CONFIG" "$BACKUP"
fi

# Remove any existing PermitRootLogin lines (commented or not) then append.
sed -i -E '/^\s*#?\s*PermitRootLogin\s+/d' "$CONFIG"
echo "PermitRootLogin no" >> "$CONFIG"

# Validate before reloading so we don't lock ourselves out.
sshd -t
systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || service ssh reload
echo "PermitRootLogin set to no"
