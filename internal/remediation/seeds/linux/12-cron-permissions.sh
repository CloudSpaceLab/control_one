#!/usr/bin/env bash
# CIS 5.1.2–5.1.6 — Ensure cron configuration files have restrictive permissions
# SOC2 CC6.1 — Control scheduled privileged execution
set -euo pipefail

fix_file_perm() {
    local path="$1"
    local mode="$2"
    if [[ -e "$path" ]]; then
        chown root:root "$path"
        chmod "$mode" "$path"
    fi
}

fix_dir_perm() {
    local path="$1"
    local mode="$2"
    if [[ -d "$path" ]]; then
        chown root:root "$path"
        chmod "$mode" "$path"
    fi
}

fix_file_perm /etc/crontab 600
fix_file_perm /etc/cron.deny 600 2>/dev/null || true
fix_file_perm /etc/cron.allow 600 2>/dev/null || true
fix_file_perm /etc/at.deny 600 2>/dev/null || true
fix_file_perm /etc/at.allow 600 2>/dev/null || true

fix_dir_perm /etc/cron.hourly 700
fix_dir_perm /etc/cron.daily 700
fix_dir_perm /etc/cron.weekly 700
fix_dir_perm /etc/cron.monthly 700
fix_dir_perm /etc/cron.d 700

# Files inside /etc/cron.d — CIS expects 600.
if [[ -d /etc/cron.d ]]; then
    find /etc/cron.d -type f -exec chown root:root {} \; -exec chmod 600 {} \;
fi

echo "cron file/dir permissions hardened"
