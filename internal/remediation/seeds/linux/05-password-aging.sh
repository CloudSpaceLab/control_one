#!/usr/bin/env bash
# CIS 5.5.1.1/5.5.1.2/5.5.1.3 — Ensure password expiration, min days, and warning are configured
# SOC2 CC6.1 — Periodic credential rotation
set -euo pipefail

CONFIG="/etc/login.defs"
BACKUP="${CONFIG}.controlone.bak"

if [[ ! -f "$CONFIG" ]]; then
    echo "login.defs not found; nothing to do" >&2
    exit 0
fi

if [[ ! -f "$BACKUP" ]]; then
    cp -p "$CONFIG" "$BACKUP"
fi

declare -A SETTINGS=(
    [PASS_MAX_DAYS]=90
    [PASS_MIN_DAYS]=7
    [PASS_WARN_AGE]=14
)

for key in "${!SETTINGS[@]}"; do
    value="${SETTINGS[$key]}"
    if grep -qE "^\s*#?\s*${key}\s+" "$CONFIG"; then
        sed -i -E "s|^\s*#?\s*${key}\s+.*|${key}   ${value}|" "$CONFIG"
    else
        echo "${key}   ${value}" >> "$CONFIG"
    fi
done

# Apply to existing non-system users (UID >= 1000).
while IFS=: read -r user _ uid _ _ _ _; do
    if [[ "$uid" -ge 1000 && "$user" != "nobody" ]]; then
        chage --maxdays 90 --mindays 7 --warndays 14 "$user" 2>/dev/null || true
    fi
done < /etc/passwd

echo "Password aging policy applied (max 90, min 7, warn 14)"
