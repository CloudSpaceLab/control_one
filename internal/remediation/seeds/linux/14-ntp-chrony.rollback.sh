#!/usr/bin/env bash
# Rollback — stop and disable whichever time service is running. Do not
# uninstall packages. A host without time sync is a compliance problem, so
# operators should only rollback if they intend to configure a different
# time source.
set -euo pipefail

for unit in chronyd chrony systemd-timesyncd; do
    if systemctl is-enabled "$unit" >/dev/null 2>&1; then
        systemctl stop "$unit" 2>/dev/null || true
        systemctl disable "$unit" 2>/dev/null || true
        echo "disabled ${unit}"
    fi
done

echo "time sync services stopped/disabled"
