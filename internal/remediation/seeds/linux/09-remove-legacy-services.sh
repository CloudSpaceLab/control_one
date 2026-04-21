#!/usr/bin/env bash
# CIS 2.3.x — Ensure telnet / rsh / talk / ypbind / tftp clients are not installed
# SOC2 CC6.6 — Minimize attack surface
set -euo pipefail

LEGACY=(telnet telnetd rsh-client rsh-server rsh-redone-client rsh-redone-server talk talkd ypbind tftp tftp-server)

if command -v apt-get >/dev/null 2>&1; then
    for pkg in "${LEGACY[@]}"; do
        if dpkg -l "$pkg" 2>/dev/null | grep -q '^ii'; then
            DEBIAN_FRONTEND=noninteractive apt-get purge -y "$pkg" || true
        fi
    done
elif command -v dnf >/dev/null 2>&1; then
    for pkg in "${LEGACY[@]}"; do
        if rpm -q "$pkg" >/dev/null 2>&1; then
            dnf remove -y "$pkg" || true
        fi
    done
elif command -v yum >/dev/null 2>&1; then
    for pkg in "${LEGACY[@]}"; do
        if rpm -q "$pkg" >/dev/null 2>&1; then
            yum remove -y "$pkg" || true
        fi
    done
fi

echo "Legacy insecure services purged"
