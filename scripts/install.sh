#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="/opt/control-one/nodeagent"
BIN_PATH="$INSTALL_DIR/nodeagent"
CONFIG_DEST="/etc/control-one/nodeagent.yaml"
SERVICE_FILE="/etc/systemd/system/control-one-nodeagent.service"

TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

mkdir -p "$INSTALL_DIR" /var/lib/control-one/nodeagent /var/log/control-one/nodeagent

if [[ -f nodeagent ]]; then
  cp nodeagent "$BIN_PATH"
else
  echo "nodeagent binary not found in current directory" >&2
  exit 1
fi

chmod 0750 "$BIN_PATH"

if [[ -f configs/example-config.yaml ]]; then
  mkdir -p "$(dirname "$CONFIG_DEST")"
  cp configs/example-config.yaml "$CONFIG_DEST"
  chmod 0640 "$CONFIG_DEST"
fi

if [[ -f build/nodeagent.service ]]; then
  cp build/nodeagent.service "$SERVICE_FILE"
  systemctl daemon-reload
  systemctl enable control-one-nodeagent.service
  systemctl restart control-one-nodeagent.service
else
  echo "systemd unit file not found; skipping service setup" >&2
fi

echo "Control One node agent installed."
