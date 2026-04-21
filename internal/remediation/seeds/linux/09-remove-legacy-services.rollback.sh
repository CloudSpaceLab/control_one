#!/usr/bin/env bash
# Rollback — we DO NOT re-install these insecure packages automatically.
# This script logs the rollback intent; an operator must explicitly reinstall
# if a workflow genuinely needs telnet/rsh/tftp.
set -euo pipefail

cat <<'EOF' >&2
Rollback for legacy-services remediation is a no-op by design.
ControlOne will not auto-reinstall telnet/rsh/talk/ypbind/tftp. If a specific
workload requires one of these packages, an operator must install it manually
and add an exemption in the compliance policy.
EOF
exit 0
