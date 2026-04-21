#!/usr/bin/env bash
# Rollback for CIS 5.5.1.x — restore login.defs (does not revert per-user chage).
set -euo pipefail

CONFIG="/etc/login.defs"
BACKUP="${CONFIG}.controlone.bak"

if [[ -f "$BACKUP" ]]; then
    cp -p "$BACKUP" "$CONFIG"
    rm -f "$BACKUP"
    echo "login.defs restored from backup"
else
    # Shadow-utils defaults.
    sed -i -E 's|^\s*PASS_MAX_DAYS\s+.*|PASS_MAX_DAYS   99999|' "$CONFIG" 2>/dev/null || true
    sed -i -E 's|^\s*PASS_MIN_DAYS\s+.*|PASS_MIN_DAYS   0|' "$CONFIG" 2>/dev/null || true
    sed -i -E 's|^\s*PASS_WARN_AGE\s+.*|PASS_WARN_AGE   7|' "$CONFIG" 2>/dev/null || true
    echo "login.defs reverted to shadow-utils defaults"
fi
