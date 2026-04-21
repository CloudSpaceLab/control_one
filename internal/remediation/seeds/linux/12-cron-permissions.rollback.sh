#!/usr/bin/env bash
# Rollback — restore distro-typical permissions (644 for /etc/crontab, 755 for dirs).
# These are the defaults shipped by cron packages on Debian/RHEL.
set -euo pipefail

restore_file() {
    local path="$1"
    local mode="$2"
    if [[ -e "$path" ]]; then
        chown root:root "$path"
        chmod "$mode" "$path"
    fi
}

restore_dir() {
    local path="$1"
    local mode="$2"
    if [[ -d "$path" ]]; then
        chown root:root "$path"
        chmod "$mode" "$path"
    fi
}

restore_file /etc/crontab 644
restore_dir /etc/cron.hourly 755
restore_dir /etc/cron.daily 755
restore_dir /etc/cron.weekly 755
restore_dir /etc/cron.monthly 755
restore_dir /etc/cron.d 755

if [[ -d /etc/cron.d ]]; then
    find /etc/cron.d -type f -exec chmod 644 {} \;
fi

echo "cron permissions restored to distribution defaults"
