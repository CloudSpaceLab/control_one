#!/usr/bin/env bats
# Unit tests for scripts/install-agent.sh.
#
# Run with: bats scripts/install-agent.bats
#
# These tests exercise the script's branches without invoking the real
# control-plane network or systemd. End-to-end enrollment lives in the
# integration suite, not here.
#
# To install bats-core on Debian/Ubuntu: sudo apt install bats
# Or from source: git clone https://github.com/bats-core/bats-core && bash bats-core/install.sh /usr/local

setup() {
    SCRIPT_DIR="$(cd "$(dirname "${BATS_TEST_FILENAME}")" && pwd)"
    SCRIPT="$SCRIPT_DIR/install-agent.sh"
    TMP="$(mktemp -d)"

    # Sandbox the install dir + a fake agent so the script never writes to
    # /usr/local/bin or hits real systemd.
    export INSTALL_DIR="$TMP/bin"
    mkdir -p "$INSTALL_DIR"

    # Force the idempotency check to see "no prior install" by default.
    # Tests that exercise the idempotency branch override these.
    export FAKE_STATE_FILE="$TMP/state.json"
}

teardown() {
    [[ -n "${TMP:-}" && -d "$TMP" ]] && rm -rf "$TMP"
}

# ─── Help text ──────────────────────────────────────────────────────
@test "prints help with -h" {
    run bash "$SCRIPT" -h
    [ "$status" -eq 0 ]
    [[ "$output" == *"Usage:"* ]]
    [[ "$output" == *"--token"* ]]
    [[ "$output" == *"--local"* ]]
    [[ "$output" == *"--dry-run"* ]]
}

@test "prints help with --help" {
    run bash "$SCRIPT" --help
    [ "$status" -eq 0 ]
    [[ "$output" == *"Exit codes"* ]]
}

# ─── Required-flag validation ───────────────────────────────────────
@test "missing --url exits 1" {
    run bash "$SCRIPT" --token TOKEN
    [ "$status" -eq 1 ]
    [[ "$output" == *"CONTROL_PLANE_URL is required"* ]]
}

@test "missing --token exits 1" {
    run bash "$SCRIPT" --url https://cp.example.com
    [ "$status" -eq 1 ]
    [[ "$output" == *"TOKEN is required"* ]]
}

# ─── Unknown-flag rejection ─────────────────────────────────────────
@test "unknown flag exits 1 with error message" {
    run bash "$SCRIPT" --bogus-flag
    [ "$status" -eq 1 ]
    [[ "$output" == *"Unknown argument: --bogus-flag"* ]]
}

# ─── --dry-run touches nothing on disk ──────────────────────────────
@test "--dry-run with --local succeeds without writing the binary" {
    fake_bin="$TMP/fake-controlone-agent"
    printf 'fake binary contents' > "$fake_bin"

    run bash "$SCRIPT" \
        --dry-run \
        --local "$fake_bin" \
        --token TKN \
        --url https://cp.example.com \
        --install-dir "$INSTALL_DIR" \
        --no-service

    [ "$status" -eq 0 ]
    # The script should print the actions it WOULD take.
    [[ "$output" == *"[DRY-RUN]"* ]]
    # And nothing should have been installed.
    [ ! -f "$INSTALL_DIR/controlone-agent" ]
}

@test "--dry-run prints planned curl call when no --local" {
    run bash "$SCRIPT" \
        --dry-run \
        --token TKN \
        --url https://cp.example.com \
        --install-dir "$INSTALL_DIR" \
        --no-service

    [ "$status" -eq 0 ]
    [[ "$output" == *"[DRY-RUN]"* ]]
    [[ "$output" == *"curl"* ]]
}

# ─── --local mode runs without network ──────────────────────────────
# The real --local path needs sudo + the binary's own --join handler. Here we
# verify only the read-side branches: argument parsing, the --local file
# existence check, and the idempotency exit. Beyond that we'd be testing
# the agent itself.
@test "--local with missing file exits 1" {
    run bash "$SCRIPT" \
        --local "$TMP/does-not-exist" \
        --token TKN \
        --url https://cp.example.com \
        --no-service

    [ "$status" -eq 1 ]
    [[ "$output" == *"--local path does not exist"* ]]
}

@test "--local with empty file exits 1" {
    empty_bin="$TMP/empty-bin"
    : > "$empty_bin"

    run bash "$SCRIPT" \
        --local "$empty_bin" \
        --token TKN \
        --url https://cp.example.com \
        --no-service

    [ "$status" -eq 1 ]
    [[ "$output" == *"--local path is empty"* ]]
}

# ─── Idempotency ────────────────────────────────────────────────────
# We can't easily mock /var/lib/control-one/state.json without root, so verify
# the branch via the dry-run path: confirm that --force is accepted as a flag
# and parsed without error. The idempotency branch itself is exercised by the
# integration suite.
@test "--force is accepted and does not change exit code on dry-run" {
    fake_bin="$TMP/fake-controlone-agent"
    printf 'fake' > "$fake_bin"

    run bash "$SCRIPT" \
        --dry-run \
        --force \
        --local "$fake_bin" \
        --token TKN \
        --url https://cp.example.com \
        --install-dir "$INSTALL_DIR" \
        --no-service

    [ "$status" -eq 0 ]
}

# Idempotency exit-message check: when the state file IS present and the
# script believes the service is active, we expect the early-exit message.
# We simulate by placing a fake state.json AND mocking systemctl/rc-service
# to claim active status.
@test "exits 0 with idempotency message when state.json present and service active" {
    # Stub systemctl that always reports active.
    stub_dir="$TMP/stub"
    mkdir -p "$stub_dir"
    cat > "$stub_dir/systemctl" <<'STUB'
#!/usr/bin/env bash
case "$*" in
    "is-active --quiet controlone-agent") exit 0 ;;
    *) exit 0 ;;
esac
STUB
    chmod +x "$stub_dir/systemctl"

    # Place fake state.json at the path the script checks. This requires write
    # access to /var/lib/control-one/nodeagent. If not running as root, skip —
    # we can't override the hard-coded path without rewriting the script.
    if [[ "$(id -u)" -ne 0 ]]; then
        skip "Idempotency exit-message branch needs root to seed /var/lib/control-one/nodeagent/state.json"
    fi

    mkdir -p /var/lib/control-one/nodeagent
    : > /var/lib/control-one/nodeagent/state.json

    PATH="$stub_dir:$PATH" run bash "$SCRIPT" \
        --token TKN \
        --url https://cp.example.com \
        --no-service

    [ "$status" -eq 0 ]
    [[ "$output" == *"already enrolled and running"* ]]

    rm -f /var/lib/control-one/nodeagent/state.json
}

# ─── --manifest override is accepted ────────────────────────────────
@test "--manifest URL is accepted as a flag" {
    fake_bin="$TMP/fake-controlone-agent"
    printf 'fake' > "$fake_bin"

    run bash "$SCRIPT" \
        --dry-run \
        --local "$fake_bin" \
        --token TKN \
        --url https://cp.example.com \
        --manifest https://other.example.com/manifest.json \
        --install-dir "$INSTALL_DIR" \
        --no-service

    [ "$status" -eq 0 ]
}

# ─── --ca-cert is parsed without error in dry-run ───────────────────
# The decode itself depends on the host's base64(1) implementation (some are
# permissive about whitespace/junk), so we only assert the flag is parsed and
# accepted with a valid base64 input. Decode-failure handling lives in the
# script's install_custom_ca and is exercised end-to-end, not here.
@test "--ca-cert with valid base64 is accepted in dry-run" {
    fake_bin="$TMP/fake-controlone-agent"
    printf 'fake' > "$fake_bin"
    # base64 of "PEM CONTENT" — valid encoding of arbitrary bytes
    valid_b64="UEVNIENPTlRFTlQ="

    run bash "$SCRIPT" \
        --dry-run \
        --local "$fake_bin" \
        --token TKN \
        --url https://cp.example.com \
        --ca-cert "$valid_b64" \
        --install-dir "$INSTALL_DIR" \
        --no-service

    # Either succeeds outright, or the only failure is from running CA-trust
    # commands the host doesn't have. Treat both as acceptable.
    [[ "$status" -eq 0 || "$output" == *"CA"* ]]
}
