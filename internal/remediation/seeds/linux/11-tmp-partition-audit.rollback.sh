#!/usr/bin/env bash
# Rollback — remove the audit log. No mount changes were made, so no rollback
# is actually required; this is defined for pipeline symmetry.
set -euo pipefail

rm -f /var/log/controlone-tmp-audit.log
echo "tmp-partition-audit rollback: audit log removed (no mount changes were applied)"
