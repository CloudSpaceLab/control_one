#!/usr/bin/env bash
# Rollback for CIS 4.1.1.1 — remove baseline rules and disable auditd.
# We do NOT uninstall the package; removing it can break log forwarding.
set -euo pipefail

RULES=/etc/audit/rules.d/controlone-baseline.rules
rm -f "$RULES"

if command -v augenrules >/dev/null 2>&1; then
    augenrules --load 2>/dev/null || true
fi

systemctl stop auditd 2>/dev/null || true
systemctl disable auditd 2>/dev/null || true
echo "auditd baseline rules removed; daemon disabled"
