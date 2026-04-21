#!/usr/bin/env bash
# CIS 3.1.2 — Ensure IPv6 forwarding is disabled (unless host is a router)
# SOC2 CC6.6 — Restrict unintended L3 forwarding
set -euo pipefail

SYSCTL=/etc/sysctl.d/99-controlone-ipv6.conf
cat > "$SYSCTL" <<'SYSCTL_EOF'
# Managed by ControlOne — CIS 3.1.2
net.ipv6.conf.all.forwarding = 0
net.ipv6.conf.default.forwarding = 0
net.ipv4.ip_forward = 0
SYSCTL_EOF

sysctl --system >/dev/null
echo "IPv6 (and IPv4) forwarding disabled"
