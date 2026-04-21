#!/usr/bin/env bash
# Rollback — remove the report. Scan is non-mutating so no permissions change
# to undo.
set -euo pipefail

rm -f /var/log/controlone-world-writable.log
echo "world-writable scan rollback: report removed (no permission changes applied)"
