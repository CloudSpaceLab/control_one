#!/usr/bin/env bash
set -euo pipefail

WITH_UI=true

usage() {
  cat <<'EOF'
Run a one-click Control One demo stack (control plane + Redis + Postgres + Prometheus + Grafana, with embedded UI).

Usage: run_demo.sh [options]
  --no-ui           Skip building UI (backend only via docker-compose)
  -h, --help        Show this help

Examples:
  bash scripts/demo/run_demo.sh
  bash scripts/demo/run_demo.sh --no-ui
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-ui)
      WITH_UI=false
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

require_cmd() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    echo "ERROR: Required command '$name' is not installed or not in PATH" >&2
    exit 1
  fi
}

docker_compose() {
  if docker compose version >/dev/null 2>&1; then
    docker compose "$@"
  elif command -v docker-compose >/dev/null 2>&1; then
    docker-compose "$@"
  else
    echo "ERROR: docker compose is required (Docker Desktop or docker-compose plugin)" >&2
    exit 1
  fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CONFIG_PATH="$REPO_ROOT/controlplane/config/controlplane.dev.yaml"
EXAMPLE_CONFIG="$REPO_ROOT/controlplane/config/controlplane.example.yaml"
BUILD_DIR="$REPO_ROOT/build"

require_cmd docker
require_cmd bash

mkdir -p "$BUILD_DIR"

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "Creating dev config from example..."
  cp "$EXAMPLE_CONFIG" "$CONFIG_PATH"
fi

echo "Starting Control One backend stack via docker compose..."
docker_compose -f "$REPO_ROOT/docker-compose.dev.yml" up -d --remove-orphans

echo "Backend started. Services:" 
echo "  API & UI:   http://localhost:8080"
echo "  Postgres:   localhost:5432 (controlone/controlone)"
echo "  Redis:      localhost:6379"
echo "  Prometheus: http://localhost:9090"
echo "  Grafana:    http://localhost:3000 (admin/admin)"

if $WITH_UI; then
  require_cmd npm
  echo "Building UI for embedded serving..."
  pushd "$REPO_ROOT/ui" >/dev/null
  
  echo "Installing dependencies..."
  npm install >/dev/null
  
  echo "Building UI..."
  npx vite build --mode production || {
    echo "ERROR: UI build failed"
    popd >/dev/null
    exit 1
  }
  
  popd >/dev/null
  echo "UI built successfully and will be served from http://localhost:8080"
fi

echo "" 
echo "Demo is starting. It may take ~30-60s for the API and UI to finish warming up."
echo "When done, stop with:"
echo "  docker compose -f docker-compose.dev.yml down"
