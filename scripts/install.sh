#!/usr/bin/env bash
# Deprecated: use scripts/install-agent.sh --local <binary> instead.
#
# This wrapper preserves the legacy "drop the binary in cwd and run install.sh"
# UX while routing all the actual work through install-agent.sh, which has
# proper distro/init detection, idempotency, signature verify, and the unified
# exit-code contract. See install-agent.sh --help for the full surface.
set -euo pipefail

echo "Note: scripts/install.sh is deprecated; forwarding to install-agent.sh --local ./controlone-agent" >&2

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The legacy install.sh expected ./controlone-agent in the current working
# directory. Preserve that contract.
if [[ ! -f "./controlone-agent" ]]; then
    echo "controlone-agent binary not found in current directory" >&2
    exit 1
fi

exec "$SCRIPT_DIR/install-agent.sh" --local "./controlone-agent" "$@"
