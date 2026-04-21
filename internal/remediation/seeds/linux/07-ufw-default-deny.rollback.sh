#!/usr/bin/env bash
# Rollback for CIS 3.5.1.x — disable UFW.
set -euo pipefail

if command -v ufw >/dev/null 2>&1; then
    ufw --force disable
    echo "UFW disabled"
else
    echo "ufw not installed; nothing to do"
fi
