#!/usr/bin/env bash
# CIS 4.1.1.1 — Ensure auditd is installed and enabled
# SOC2 CC7.2 — System activity is monitored and logged
set -euo pipefail

if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get install -y auditd audispd-plugins
elif command -v dnf >/dev/null 2>&1; then
    dnf install -y audit audit-libs
elif command -v yum >/dev/null 2>&1; then
    yum install -y audit audit-libs
else
    echo "no supported package manager found" >&2
    exit 1
fi

# Ensure auditd starts at boot and is running now.
systemctl enable auditd
systemctl start auditd

# Minimum baseline rules — time changes, identity changes, privileged exec.
RULES=/etc/audit/rules.d/controlone-baseline.rules
cat > "$RULES" <<'RULES_EOF'
## ControlOne baseline rules (CIS 4.1.x)
-w /etc/shadow -p wa -k identity
-w /etc/passwd -p wa -k identity
-w /etc/group -p wa -k identity
-w /etc/gshadow -p wa -k identity
-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d/ -p wa -k scope
-a always,exit -F arch=b64 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b32 -S adjtimex,settimeofday -k time-change
-a always,exit -F arch=b64 -S clock_settime -k time-change
-w /etc/localtime -p wa -k time-change
RULES_EOF

if command -v augenrules >/dev/null 2>&1; then
    augenrules --load
else
    systemctl restart auditd
fi

echo "auditd installed, enabled, and baseline rules applied"
