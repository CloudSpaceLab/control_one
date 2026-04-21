#!/usr/bin/env bash
# Rollback — remove the sshd jail and stop fail2ban (package kept installed).
set -euo pipefail

JAIL=/etc/fail2ban/jail.d/controlone-sshd.local
rm -f "$JAIL"

if command -v fail2ban-client >/dev/null 2>&1; then
    systemctl stop fail2ban 2>/dev/null || true
    systemctl disable fail2ban 2>/dev/null || true
    echo "fail2ban stopped and jail removed"
else
    echo "fail2ban not present; nothing to do"
fi
