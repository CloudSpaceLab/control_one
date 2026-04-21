#!/usr/bin/env bash
# CIS 6.1.10 — Ensure no world-writable files exist
# SOC2 CC6.1 — Logical access: report anomalous permissions
#
# REPORT-ONLY: auto-chmod'ing world-writable files can break applications that
# legitimately rely on shared-write dirs. This script enumerates offenders and
# exits non-zero when any are found; an operator reviews the log and decides.
set -euo pipefail

REPORT=/var/log/controlone-world-writable.log
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

# Prune pseudo filesystems to keep the scan bounded.
find / \
    -xdev \
    -type f \
    -perm -0002 \
    -not -path '/proc/*' \
    -not -path '/sys/*' \
    -not -path '/dev/*' \
    -not -path '/run/*' \
    -print 2>/dev/null > "$TMP" || true

count=$(wc -l < "$TMP")

{
    echo "===== ControlOne world-writable scan — $(date -Is) ====="
    echo "Found: ${count} world-writable file(s)."
    if [[ "$count" -gt 0 ]]; then
        echo "--- offenders ---"
        cat "$TMP"
    fi
} | tee "$REPORT"

if [[ "$count" -gt 0 ]]; then
    exit 1
fi
exit 0
