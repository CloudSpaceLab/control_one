#!/usr/bin/env bash
# CIS 5.4.1 — Ensure password creation requirements are configured
# SOC2 CC6.1 — Enforce strong password complexity
set -euo pipefail

CONFIG="/etc/security/pwquality.conf"
BACKUP="${CONFIG}.controlone.bak"

mkdir -p /etc/security
[[ -f "$CONFIG" ]] || : > "$CONFIG"

if [[ ! -f "$BACKUP" ]]; then
    cp -p "$CONFIG" "$BACKUP"
fi

declare -A SETTINGS=(
    [minlen]=14
    [minclass]=4
    [dcredit]=-1
    [ucredit]=-1
    [ocredit]=-1
    [lcredit]=-1
    [maxrepeat]=3
    [retry]=3
)

for key in "${!SETTINGS[@]}"; do
    value="${SETTINGS[$key]}"
    if grep -qE "^\s*#?\s*${key}\s*=" "$CONFIG"; then
        sed -i -E "s|^\s*#?\s*${key}\s*=.*|${key} = ${value}|" "$CONFIG"
    else
        echo "${key} = ${value}" >> "$CONFIG"
    fi
done

echo "pwquality.conf updated with min length 14 and 4-class complexity"
