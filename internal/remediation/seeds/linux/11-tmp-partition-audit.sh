#!/usr/bin/env bash
# CIS 1.1.2.x — Ensure /tmp is on a separate partition with noexec,nosuid,nodev
# SOC2 CC6.8 — Restrict execution of untrusted code
#
# REPORT-ONLY: Remounting /tmp live is disruptive and can break running services
# (e.g. systemd user units). This script reports the state so an operator can
# fix it during a maintenance window rather than auto-remounting.
set -euo pipefail

REPORT=/var/log/controlone-tmp-audit.log
{
    echo "===== ControlOne /tmp hardening audit — $(date -Is) ====="

    if ! findmnt /tmp >/dev/null 2>&1; then
        echo "FAIL: /tmp is not a distinct mount point (still on /)."
        exit_code=1
    else
        echo "PASS: /tmp is a distinct mount point."
        exit_code=0
    fi

    mount_opts=$(findmnt -n -o OPTIONS /tmp 2>/dev/null || echo "")
    echo "Current /tmp mount options: ${mount_opts:-<none>}"

    for opt in noexec nosuid nodev; do
        if echo ",${mount_opts}," | grep -q ",${opt},"; then
            echo "PASS: ${opt} is set"
        else
            echo "FAIL: ${opt} is NOT set on /tmp"
            exit_code=1
        fi
    done

    echo "Recommended /etc/fstab entry:"
    echo "  tmpfs /tmp tmpfs defaults,noexec,nosuid,nodev,size=2G 0 0"
    echo
    echo "Exit code: ${exit_code}"
    exit ${exit_code}
} | tee "$REPORT"
