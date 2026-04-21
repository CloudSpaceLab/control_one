#!/usr/bin/env bash
# CIS 2.2.1.1 — Ensure time synchronization is in use
# SOC2 CC7.2 — Accurate timestamps for forensic logs
set -euo pipefail

if systemctl list-unit-files 2>/dev/null | grep -qE '^(chrony|chronyd)\.service'; then
    SERVICE=chronyd
    if ! systemctl list-unit-files 2>/dev/null | grep -q '^chronyd.service'; then
        SERVICE=chrony
    fi
elif systemctl list-unit-files 2>/dev/null | grep -q '^systemd-timesyncd.service'; then
    SERVICE=systemd-timesyncd
else
    # Install chrony as the preferred implementation if nothing is present.
    if command -v apt-get >/dev/null 2>&1; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y chrony
        SERVICE=chrony
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y chrony
        SERVICE=chronyd
    elif command -v yum >/dev/null 2>&1; then
        yum install -y chrony
        SERVICE=chronyd
    else
        echo "no package manager available to install chrony" >&2
        exit 1
    fi
fi

systemctl enable "$SERVICE"
systemctl start "$SERVICE"
systemctl is-active "$SERVICE"
echo "time sync active via ${SERVICE}"
