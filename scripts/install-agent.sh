#!/usr/bin/env bash
# Control One Agent Installer
# Usage: curl -fsSL https://cp.example.com/api/v1/agent/install-script?token=TOKEN | bash
# Or:    bash install-agent.sh --token TOKEN --url https://cp.example.com
set -euo pipefail

# ─── Colors ─────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fatal() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

# ─── Defaults (may be baked in by the control plane) ────────────────
CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-}"
TOKEN="${TOKEN:-}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
NO_SERVICE="${NO_SERVICE:-false}"

# ─── Parse arguments ────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --token)
            TOKEN="$2"; shift 2 ;;
        --url)
            CONTROL_PLANE_URL="$2"; shift 2 ;;
        --install-dir)
            INSTALL_DIR="$2"; shift 2 ;;
        --no-service)
            NO_SERVICE="true"; shift ;;
        -h|--help)
            echo "Usage: $0 [--token TOKEN] [--url URL] [--install-dir DIR] [--no-service]"
            exit 0 ;;
        *)
            fatal "Unknown argument: $1" ;;
    esac
done

# ─── Validate required parameters ───────────────────────────────────
if [[ -z "$CONTROL_PLANE_URL" ]]; then
    fatal "CONTROL_PLANE_URL is required. Pass --url or set CONTROL_PLANE_URL env var."
fi

if [[ -z "$TOKEN" ]]; then
    fatal "TOKEN is required. Pass --token or set TOKEN env var."
fi

# Strip trailing slash from URL
CONTROL_PLANE_URL="${CONTROL_PLANE_URL%/}"

# ─── Detect platform ────────────────────────────────────────────────
detect_os() {
    local os
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$os" in
        linux)  echo "linux" ;;
        darwin) echo "darwin" ;;
        *)      fatal "Unsupported operating system: $os" ;;
    esac
}

detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              fatal "Unsupported architecture: $arch" ;;
    esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"
info "Detected platform: ${OS}/${ARCH}"

# ─── Check prerequisites ────────────────────────────────────────────
command -v curl >/dev/null 2>&1 || fatal "curl is required but not installed."

# ─── Root/sudo detection ────────────────────────────────────────────
SUDO=""
if [[ "$EUID" -ne 0 ]]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
        info "Not running as root; will use sudo for installation."
    else
        warn "Not running as root and sudo not available. Installation to ${INSTALL_DIR} may fail."
    fi
fi

# ─── Download binary ────────────────────────────────────────────────
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

BINARY_URL="${CONTROL_PLANE_URL}/api/v1/agent/binary?os=${OS}&arch=${ARCH}"
BINARY_PATH="${TMP_DIR}/controlone-agent"

info "Downloading agent binary from ${BINARY_URL}..."
HTTP_CODE=$(curl -fsSL -w "%{http_code}" -o "$BINARY_PATH" "$BINARY_URL" 2>/dev/null) || true

if [[ ! -f "$BINARY_PATH" ]] || [[ ! -s "$BINARY_PATH" ]]; then
    fatal "Failed to download agent binary (HTTP ${HTTP_CODE:-???}). Check that the control plane is reachable and has binaries for ${OS}/${ARCH}."
fi

if [[ "${HTTP_CODE}" != "200" ]]; then
    fatal "Binary download returned HTTP ${HTTP_CODE}. Expected 200."
fi

ok "Binary downloaded successfully."

# ─── Install binary ─────────────────────────────────────────────────
info "Installing agent to ${INSTALL_DIR}/controlone-agent..."
$SUDO mkdir -p "$INSTALL_DIR"
$SUDO install -m 0755 "$BINARY_PATH" "${INSTALL_DIR}/controlone-agent"
ok "Binary installed to ${INSTALL_DIR}/controlone-agent."

# ─── Enroll agent ───────────────────────────────────────────────────
ENROLL_ARGS=("--join" "$CONTROL_PLANE_URL" "--token" "$TOKEN")

if [[ "$NO_SERVICE" != "true" ]]; then
    ENROLL_ARGS+=("--install-service")
fi

info "Enrolling agent with control plane..."
if $SUDO "${INSTALL_DIR}/controlone-agent" "${ENROLL_ARGS[@]}"; then
    ok "Agent enrolled successfully."
else
    fatal "Agent enrollment failed. Check the logs above for details."
fi

# ─── Done ────────────────────────────────────────────────────────────
echo ""
ok "Control One agent installation complete!"
info "  Binary:  ${INSTALL_DIR}/controlone-agent"
info "  Server:  ${CONTROL_PLANE_URL}"
if [[ "$NO_SERVICE" != "true" ]]; then
    info "  Service: enabled and started"
fi
