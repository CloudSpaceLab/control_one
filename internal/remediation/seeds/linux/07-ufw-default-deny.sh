#!/usr/bin/env bash
# CIS 3.5.1.x — Ensure a host-based firewall is configured (UFW)
# SOC2 CC6.6 — Network boundary protection
set -euo pipefail

if ! command -v ufw >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y ufw
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y ufw
    else
        echo "ufw not available and no supported package manager; skipping" >&2
        exit 1
    fi
fi

# Always allow SSH BEFORE enabling, otherwise remote operators lose access.
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp comment 'ControlOne: SSH'
yes | ufw enable

ufw status verbose
echo "UFW enabled with default-deny inbound and SSH permitted"
