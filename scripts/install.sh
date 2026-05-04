#!/usr/bin/env bash
set -euo pipefail

# Local on-host installer for the Control One agent. Use this when you've
# already fetched the agent binary into the current directory (e.g. from an
# offline bundle or a hand-copied build artifact). For network installs that
# fetch from a control plane, use scripts/install-agent.sh instead.

INSTALL_DIR="/usr/local/bin"
BIN_PATH="$INSTALL_DIR/controlone-agent"
CONFIG_DEST="/etc/control-one/nodeagent.yaml"
SERVICE_FILE="/etc/systemd/system/controlone-agent.service"

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

mkdir -p "$INSTALL_DIR" /var/lib/control-one/nodeagent /var/log/control-one/nodeagent

if [[ -f controlone-agent ]]; then
  cp controlone-agent "$BIN_PATH"
else
  echo "controlone-agent binary not found in current directory" >&2
  exit 1
fi

chmod 0755 "$BIN_PATH"

if [[ -f configs/example-config.yaml ]]; then
  mkdir -p "$(dirname "$CONFIG_DEST")"
  cp configs/example-config.yaml "$CONFIG_DEST"
  chmod 0640 "$CONFIG_DEST"
fi

if [[ -f build/controlone-agent.service ]]; then
  cp build/controlone-agent.service "$SERVICE_FILE"
  systemctl daemon-reload
  systemctl enable controlone-agent.service
  systemctl restart controlone-agent.service
else
  echo "systemd unit file not found; skipping service setup" >&2
fi

echo "Control One agent installed."
