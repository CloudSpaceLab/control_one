#!/usr/bin/env bash
# Rollback — drop the sysctl drop-in and let kernel defaults apply (0 for both).
set -euo pipefail

SYSCTL=/etc/sysctl.d/99-controlone-ipv6.conf
rm -f "$SYSCTL"
sysctl --system >/dev/null
echo "ControlOne IPv6-forwarding sysctl drop-in removed"
