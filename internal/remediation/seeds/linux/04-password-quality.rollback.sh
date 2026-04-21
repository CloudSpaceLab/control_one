#!/usr/bin/env bash
# Rollback for CIS 5.4.1 — restore prior pwquality config
set -euo pipefail

CONFIG="/etc/security/pwquality.conf"
BACKUP="${CONFIG}.controlone.bak"

if [[ -f "$BACKUP" ]]; then
    cp -p "$BACKUP" "$CONFIG"
    rm -f "$BACKUP"
    echo "pwquality.conf restored from backup"
else
    # No backup — strip the keys we manage so the distro defaults apply.
    for key in minlen minclass dcredit ucredit ocredit lcredit maxrepeat retry; do
        sed -i -E "/^\s*${key}\s*=/d" "$CONFIG" 2>/dev/null || true
    done
    echo "pwquality.conf directives removed"
fi
