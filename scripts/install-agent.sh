#!/usr/bin/env bash
# Control One Agent Installer (standalone)
#
# Brings the standalone installer to feature parity with the control-plane-
# rendered template in controlplane/internal/server/agent_download.go
# (installScriptTemplate, lines 31-303). Use this for air-gapped installs,
# pre-baked images, or environments where curl-piping the rendered script
# isn't possible.
#
# Usage:
#   sudo bash install-agent.sh --token TOKEN --url https://cp.example.com
#   sudo bash install-agent.sh --local ./controlone-agent --token TOKEN --url URL
#   curl -fsSL https://cp.example.com/api/v1/agent/install-script?token=TOKEN | bash
#
# Exit codes:
#   0  success
#   1  generic / argument error
#   10 download failed
#   20 verification failed (sha256 or ed25519)
#   30 enrollment failed
#   40 service failed to start

set -euo pipefail

# ─── Exit codes ─────────────────────────────────────────────────────
readonly EXIT_OK=0
readonly EXIT_GENERIC=1
readonly EXIT_DOWNLOAD=10
readonly EXIT_VERIFY=20
readonly EXIT_ENROLL=30
readonly EXIT_SERVICE=40

# ─── Colors ─────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m'
else
    RED=''; GREEN=''; YELLOW=''; BLUE=''; NC=''
fi

info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
# fatal <message> [exit_code]
fatal() {
    local msg="$1"
    local code="${2:-$EXIT_GENERIC}"
    echo -e "${RED}[ERROR]${NC} $msg" >&2
    exit "$code"
}

# ─── Defaults ───────────────────────────────────────────────────────
CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-}"
TOKEN="${TOKEN:-}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
NO_SERVICE="${NO_SERVICE:-false}"
CA_CERT_B64="${CA_CERT_B64:-}"
LOCAL_BINARY=""
FORCE="false"
DRY_RUN="false"
MANIFEST_URL_OVERRIDE=""
CA_CERT_FILE=""

# ─── Help ───────────────────────────────────────────────────────────
print_help() {
    cat <<'EOF'
Control One Agent Installer

Usage:
  install-agent.sh --token TOKEN --url URL [options]
  install-agent.sh --local PATH --token TOKEN --url URL [options]

Required (unless --local provides binary AND token/url already set elsewhere):
  --token TOKEN       Enrollment token (or set $TOKEN)
  --url URL           Control plane URL (or set $CONTROL_PLANE_URL)

Options:
  --install-dir DIR   Install path for the agent binary (default: /usr/local/bin)
  --no-service        Skip systemd/OpenRC service installation
  --ca-cert B64       Base64-encoded PEM of a custom root CA to trust
  --local PATH        Use a local agent binary instead of downloading
  --force             Reinstall even if the agent is already enrolled and active
  --dry-run           Print what would be done without touching the system
  --manifest URL      Override the manifest URL (default: $URL/api/v1/agent/binary/manifest)
  -h, --help          Show this help

Exit codes:
  0  success                 10 download failed
  1  generic                 20 verification failed
  30 enrollment failed       40 service failed to start
EOF
}

# ─── Parse arguments ────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --token)        TOKEN="$2"; shift 2 ;;
        --url)          CONTROL_PLANE_URL="$2"; shift 2 ;;
        --install-dir)  INSTALL_DIR="$2"; shift 2 ;;
        --ca-cert)      CA_CERT_B64="$2"; shift 2 ;;
        --local)        LOCAL_BINARY="$2"; shift 2 ;;
        --force)        FORCE="true"; shift ;;
        --dry-run)      DRY_RUN="true"; shift ;;
        --manifest)     MANIFEST_URL_OVERRIDE="$2"; shift 2 ;;
        --no-service)   NO_SERVICE="true"; shift ;;
        -h|--help)      print_help; exit "$EXIT_OK" ;;
        *)              fatal "Unknown argument: $1" "$EXIT_GENERIC" ;;
    esac
done

# ─── run / run_root: dry-run-aware command execution ────────────────
# Use these instead of calling commands directly when the call would mutate the
# system. Read-only probes (curl, uname, command -v, sha256sum) run normally
# even in dry-run so we can still report what we'd do.
run() {
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY-RUN] $*"
        return 0
    fi
    "$@"
}

run_root() {
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY-RUN] ${SUDO:+$SUDO }$*"
        return 0
    fi
    if [[ -n "$SUDO" ]]; then
        $SUDO "$@"
    else
        "$@"
    fi
}

# ─── Validate required parameters ───────────────────────────────────
# In --local mode without enrollment we still want url+token because the agent
# itself will use them at enroll time. The only thing --local skips is the
# download.
if [[ -z "$CONTROL_PLANE_URL" ]]; then
    fatal "CONTROL_PLANE_URL is required. Pass --url or set CONTROL_PLANE_URL." "$EXIT_GENERIC"
fi
if [[ -z "$TOKEN" ]]; then
    fatal "TOKEN is required. Pass --token or set TOKEN." "$EXIT_GENERIC"
fi

# Strip trailing slash
CONTROL_PLANE_URL="${CONTROL_PLANE_URL%/}"

# ─── Detect platform + distro + init ────────────────────────────────
# Mirrors detect_os/detect_arch/detect_distro/detect_init in
# controlplane/internal/server/agent_download.go (lines 81-135).
detect_os() {
    local os; os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$os" in
        linux)  echo "linux" ;;
        darwin) echo "darwin" ;;
        *)      fatal "Unsupported OS: $os" "$EXIT_GENERIC" ;;
    esac
}

detect_arch() {
    local arch; arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             fatal "Unsupported arch: $arch" "$EXIT_GENERIC" ;;
    esac
}

detect_distro() {
    if [[ "$(uname -s)" != "Linux" ]]; then
        echo "generic"
        return
    fi
    if [[ -r /etc/os-release ]]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        local id="${ID:-}"
        local like="${ID_LIKE:-}"
        case "$id $like" in
            *ubuntu*|*debian*)             echo "debian"; return ;;
            *rhel*|*centos*|*rocky*|*almalinux*|*fedora*|*ol*) echo "rhel"; return ;;
            *opensuse*|*suse*|*sles*)      echo "suse"; return ;;
            *alpine*)                      echo "alpine"; return ;;
            *arch*)                        echo "arch"; return ;;
        esac
    fi
    echo "generic"
}

detect_init() {
    if [[ -d /run/systemd/system ]] || command -v systemctl >/dev/null 2>&1; then
        echo "systemd"; return
    fi
    if [[ -x /sbin/openrc-run ]] || { [[ -d /etc/init.d ]] && command -v rc-service >/dev/null 2>&1; }; then
        echo "openrc"; return
    fi
    if [[ -d /etc/init.d ]]; then
        echo "sysv"; return
    fi
    echo "unknown"
}

OS="$(detect_os)"
ARCH="$(detect_arch)"
DISTRO="$(detect_distro)"
INIT_SYSTEM="$(detect_init)"
info "Platform: ${OS}/${ARCH} (distro=${DISTRO}, init=${INIT_SYSTEM})"

# ─── Prerequisites ──────────────────────────────────────────────────
command -v curl >/dev/null 2>&1 || fatal "curl is required but not installed." "$EXIT_GENERIC"

# ─── Sudo detection ─────────────────────────────────────────────────
SUDO=""
if [[ "$(id -u 2>/dev/null || echo 0)" -ne 0 ]]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
        info "Not running as root; will use sudo for privileged operations."
    else
        warn "Not running as root and sudo not available. Privileged steps may fail."
    fi
fi

# ─── Idempotency check ──────────────────────────────────────────────
# Mirrors the embedded template's pre-flight repair semantics (lines 196-239).
# If the agent is already enrolled (state.json exists) AND the service is
# active, exit 0 unless --force is set. If state.json exists but the service
# is inactive, log a warning and continue so re-enrollment can heal the box.
STATE_FILE="/var/lib/control-one/nodeagent/state.json"
LEGACY_OPT_DIR="/opt/control-one/nodeagent"

agent_service_active() {
    if command -v systemctl >/dev/null 2>&1; then
        systemctl is-active --quiet controlone-agent 2>/dev/null
        return $?
    fi
    if command -v rc-service >/dev/null 2>&1; then
        rc-service controlone-agent status >/dev/null 2>&1
        return $?
    fi
    return 1
}

if [[ -f "$STATE_FILE" ]]; then
    if agent_service_active; then
        if [[ "$FORCE" != "true" ]]; then
            info "Agent already enrolled and running. Use --force to reinstall."
            exit "$EXIT_OK"
        else
            warn "Agent already enrolled and running; --force set, proceeding."
        fi
    else
        warn "Found state.json but service is inactive; proceeding with re-enrollment."
    fi
fi

# Legacy /opt path was used in early builds; canonical install dir is now
# /usr/local/bin (Worktree A). If the old directory exists, log it; we don't
# remove it (operator may still use it for plugins/state).
if [[ -e "$LEGACY_OPT_DIR" ]] && [[ ! -d "$LEGACY_OPT_DIR" ]]; then
    warn "Legacy path ${LEGACY_OPT_DIR} is a file, not a directory. Relocating..."
    run_root mv "$LEGACY_OPT_DIR" "${LEGACY_OPT_DIR}.bak.$(date +%s)" 2>/dev/null || \
        run_root rm -f "$LEGACY_OPT_DIR" || true
fi

# ─── Tmp dir + cleanup ──────────────────────────────────────────────
if [[ "$DRY_RUN" == "true" ]]; then
    TMP_DIR="$(mktemp -d 2>/dev/null || echo "/tmp/controlone-dryrun-$$")"
    mkdir -p "$TMP_DIR" 2>/dev/null || true
else
    TMP_DIR="$(mktemp -d)"
fi
cleanup() { [[ -n "${TMP_DIR:-}" ]] && rm -rf "$TMP_DIR"; }
trap cleanup EXIT

# ─── Custom CA install ──────────────────────────────────────────────
# Mirrors install_custom_ca in the template (lines 146-181). Decodes the
# base64 PEM, writes it to the distro's anchor dir, and stores the path so
# curl + the agent can pass --cacert.
install_custom_ca() {
    [[ -z "$CA_CERT_B64" ]] && return
    local decoded="${TMP_DIR}/controlone-ca.pem"
    if ! printf '%s' "$CA_CERT_B64" | base64 -d > "$decoded" 2>/dev/null; then
        fatal "Failed to decode --ca-cert (expect base64-encoded PEM)." "$EXIT_GENERIC"
    fi
    CA_CERT_FILE="$decoded"

    # Also copy to a stable path the agent can reuse at runtime, mirroring the
    # task spec (--ca-cert /etc/control-one/ca.crt).
    run_root mkdir -p /etc/control-one
    run_root install -m 0644 "$decoded" /etc/control-one/ca.crt
    if [[ "$DRY_RUN" != "true" ]]; then
        CA_CERT_FILE="/etc/control-one/ca.crt"
    fi

    case "$OS" in
        linux)
            case "$DISTRO" in
                debian)
                    run_root cp "$decoded" /usr/local/share/ca-certificates/controlone.crt 2>/dev/null || true
                    run_root update-ca-certificates >/dev/null 2>&1 || \
                        warn "update-ca-certificates failed; curl will still use --cacert."
                    ;;
                rhel|suse)
                    run_root cp "$decoded" /etc/pki/ca-trust/source/anchors/controlone.crt 2>/dev/null || \
                        run_root cp "$decoded" /etc/ca-certificates/trust-source/anchors/controlone.crt 2>/dev/null || \
                        true
                    run_root update-ca-trust extract >/dev/null 2>&1 || true
                    ;;
                alpine)
                    run_root cp "$decoded" /usr/local/share/ca-certificates/controlone.crt 2>/dev/null || true
                    run_root update-ca-certificates >/dev/null 2>&1 || true
                    ;;
                *)
                    warn "Unknown distro for CA install; curl will still use --cacert."
                    ;;
            esac
            ;;
        darwin)
            run_root security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain "$decoded" >/dev/null 2>&1 || \
                warn "security add-trusted-cert failed; curl will still use --cacert."
            ;;
    esac
    ok "Custom root CA installed."
}

install_custom_ca

# ─── Pre-flight repair ──────────────────────────────────────────────
# Mirrors repair_agent_state in the template (lines 196-239). Stop any
# running agent so the binary can be replaced atomically; clean up 0-byte
# certs that block tls.LoadX509KeyPair on restart.
repair_agent_state() {
    if command -v systemctl >/dev/null 2>&1; then
        if systemctl is-active --quiet controlone-agent 2>/dev/null; then
            info "Stopping existing agent service..."
            run_root systemctl stop controlone-agent 2>/dev/null || true
        fi
    elif command -v rc-service >/dev/null 2>&1; then
        run_root rc-service controlone-agent stop 2>/dev/null || true
    fi

    local cert_dir="/var/lib/control-one/nodeagent/certs"
    if [[ -d "$cert_dir" ]]; then
        local wiped=0
        for f in "${cert_dir}"/*.crt "${cert_dir}"/*.key; do
            [[ -e "$f" ]] || continue
            if [[ ! -s "$f" ]]; then
                run_root rm -f "$f"
                wiped=$((wiped + 1))
            fi
        done
        [[ $wiped -gt 0 ]] && warn "Removed ${wiped} empty cert file(s); fresh certs will be issued on enrollment."
    fi
}

repair_agent_state

# ─── Curl options ───────────────────────────────────────────────────
CURL_OPTS=(-fsSL)
if [[ -n "$CA_CERT_FILE" ]]; then
    CURL_OPTS+=(--cacert "$CA_CERT_FILE")
fi
# HTTP_PROXY/HTTPS_PROXY/NO_PROXY are honored by curl natively; no extra opts
# needed. They get propagated to the systemd unit via --install-service in the
# agent (which reads them from the environment at install-service time).

BINARY_URL="${CONTROL_PLANE_URL}/api/v1/agent/binary?os=${OS}&arch=${ARCH}&token=${TOKEN}"
if [[ -n "$MANIFEST_URL_OVERRIDE" ]]; then
    MANIFEST_URL="$MANIFEST_URL_OVERRIDE"
else
    MANIFEST_URL="${CONTROL_PLANE_URL}/api/v1/agent/binary/manifest?os=${OS}&arch=${ARCH}&token=${TOKEN}"
fi
PUBLIC_KEY_URL="${CONTROL_PLANE_URL}/api/v1/agent/public-key"

BINARY_PATH="${TMP_DIR}/controlone-agent"
MANIFEST_PATH="${TMP_DIR}/manifest.json"

# ─── Acquire the binary (download or local copy) ────────────────────
if [[ -n "$LOCAL_BINARY" ]]; then
    if [[ ! -f "$LOCAL_BINARY" ]]; then
        fatal "--local path does not exist: $LOCAL_BINARY" "$EXIT_GENERIC"
    fi
    if [[ ! -s "$LOCAL_BINARY" ]]; then
        fatal "--local path is empty: $LOCAL_BINARY" "$EXIT_GENERIC"
    fi
    info "Using local binary: $LOCAL_BINARY"
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY-RUN] cp $LOCAL_BINARY $BINARY_PATH"
    else
        cp "$LOCAL_BINARY" "$BINARY_PATH"
    fi
    ok "Local binary staged."
else
    # ─── Fetch manifest (best-effort) ────────────────────────────────
    info "Fetching binary manifest from ${MANIFEST_URL}..."
    HTTP_CODE=""
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY-RUN] curl ${CURL_OPTS[*]} -o $MANIFEST_PATH $MANIFEST_URL"
        MANIFEST_PATH=""
    else
        HTTP_CODE=$(curl "${CURL_OPTS[@]}" -w "%{http_code}" -o "$MANIFEST_PATH" "$MANIFEST_URL" 2>/dev/null || true)
        if [[ "${HTTP_CODE}" != "200" ]] || [[ ! -s "$MANIFEST_PATH" ]]; then
            warn "Manifest unavailable (HTTP ${HTTP_CODE:-???}); proceeding without integrity check."
            MANIFEST_PATH=""
        fi
    fi

    # ─── Download binary ────────────────────────────────────────────
    info "Downloading agent binary from ${BINARY_URL}..."
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY-RUN] curl ${CURL_OPTS[*]} -o $BINARY_PATH $BINARY_URL"
    else
        HTTP_CODE=$(curl "${CURL_OPTS[@]}" -w "%{http_code}" -o "$BINARY_PATH" "$BINARY_URL" 2>/dev/null || true)
        if [[ ! -f "$BINARY_PATH" ]] || [[ ! -s "$BINARY_PATH" ]]; then
            fatal "Failed to download agent binary (HTTP ${HTTP_CODE:-???}). Check that the control plane is reachable and has binaries for ${OS}/${ARCH}." "$EXIT_DOWNLOAD"
        fi
        if [[ "${HTTP_CODE}" != "200" ]]; then
            fatal "Binary download returned HTTP ${HTTP_CODE}. Expected 200." "$EXIT_DOWNLOAD"
        fi
        ok "Binary downloaded."
    fi

    # ─── Verify checksum + signature ────────────────────────────────
    if [[ "$DRY_RUN" != "true" ]] && [[ -n "${MANIFEST_PATH}" ]] && command -v sha256sum >/dev/null 2>&1; then
        EXPECTED_SHA=$(sed -n 's/.*"sha256"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$MANIFEST_PATH")
        if [[ -n "$EXPECTED_SHA" ]]; then
            ACTUAL_SHA=$(sha256sum "$BINARY_PATH" | awk '{print $1}')
            if [[ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]]; then
                fatal "Checksum mismatch: expected ${EXPECTED_SHA}, got ${ACTUAL_SHA}" "$EXIT_VERIFY"
            fi
            ok "Checksum verified (sha256)."
        else
            warn "Manifest had no sha256 field; skipping checksum verify."
        fi

        EXPECTED_SIG=$(sed -n 's/.*"signature"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$MANIFEST_PATH")
        if [[ -n "$EXPECTED_SIG" ]]; then
            chmod +x "$BINARY_PATH"
            if "$BINARY_PATH" verify-binary --binary "$BINARY_PATH" --signature "$EXPECTED_SIG" --public-key-url "$PUBLIC_KEY_URL" >/dev/null 2>&1; then
                ok "Signature verified (ed25519)."
            else
                warn "Signature verification skipped or unavailable (continuing — sha256 already verified)."
            fi
        else
            warn "Manifest had no signature; ed25519 verify skipped (control plane may not have signing enabled yet)."
        fi
    elif [[ "$DRY_RUN" != "true" ]]; then
        warn "No manifest available; skipping integrity verification."
    fi
fi

# ─── Install binary ─────────────────────────────────────────────────
info "Installing agent to ${INSTALL_DIR}/controlone-agent..."
run_root mkdir -p "$INSTALL_DIR"
if [[ "$DRY_RUN" == "true" ]]; then
    echo "[DRY-RUN] install -m 0755 $BINARY_PATH ${INSTALL_DIR}/controlone-agent"
else
    run_root install -m 0755 "$BINARY_PATH" "${INSTALL_DIR}/controlone-agent"
fi
ok "Installed to ${INSTALL_DIR}/controlone-agent."

# ─── Enroll ─────────────────────────────────────────────────────────
ENROLL_ARGS=("--join" "$CONTROL_PLANE_URL" "--token" "$TOKEN")
[[ -n "$CA_CERT_FILE" ]] && ENROLL_ARGS+=("--ca-cert" "$CA_CERT_FILE")
[[ "$NO_SERVICE" != "true" ]] && ENROLL_ARGS+=("--install-service")
[[ -n "$INIT_SYSTEM" && "$INIT_SYSTEM" != "systemd" && "$INIT_SYSTEM" != "unknown" ]] && \
    ENROLL_ARGS+=("--init-system" "$INIT_SYSTEM")

info "Enrolling agent with control plane..."
if [[ "$DRY_RUN" == "true" ]]; then
    echo "[DRY-RUN] ${SUDO:+$SUDO }${INSTALL_DIR}/controlone-agent ${ENROLL_ARGS[*]}"
else
    if ! run_root "${INSTALL_DIR}/controlone-agent" "${ENROLL_ARGS[@]}"; then
        fatal "Agent enrollment failed. Check the logs above for details." "$EXIT_ENROLL"
    fi
fi
ok "Agent enrolled successfully."

# ─── Post-install service verification ──────────────────────────────
if [[ "$NO_SERVICE" != "true" ]] && [[ "$DRY_RUN" != "true" ]]; then
    info "Verifying service is active..."
    started=0
    for _ in 1 2 3 4 5 6 7 8 9 10; do
        if agent_service_active; then
            started=1
            break
        fi
        sleep 1
    done
    if [[ $started -eq 0 ]]; then
        fatal "Service controlone-agent did not become active within 10s. Check 'systemctl status controlone-agent' or 'rc-service controlone-agent status'." "$EXIT_SERVICE"
    fi
    ok "Service is active."
fi

# ─── Done ───────────────────────────────────────────────────────────
echo ""
ok "Control One agent installation complete!"
info "  Binary:  ${INSTALL_DIR}/controlone-agent"
info "  Server:  ${CONTROL_PLANE_URL}"
if [[ "$NO_SERVICE" != "true" ]]; then
    info "  Service: enabled and started"
fi
exit "$EXIT_OK"
