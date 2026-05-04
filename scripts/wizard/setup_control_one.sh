#!/usr/bin/env bash
set -euo pipefail

OUTPUT_DIR=""
NON_INTERACTIVE=false
GENERATE_CERTS=true
CONFIG_NAME="controlplane.yaml"
DEFAULT_DOMAIN="control-one.local"
DEFAULT_POSTGRES_URL="postgresql://controlone:controlone@localhost:5432/controlone?sslmode=disable"
DEFAULT_REGISTRATION_TOKEN="sample-bootstrap-token"
DEFAULT_TENANT_NAME="Default Tenant"
DEFAULT_PROM_ADDR=":9090"
DEFAULT_CONTROLPLANE_ADDR=":8443"
DEFAULT_NODEAGENT_ADDR="https://node.example.com"
DEFAULT_JOB_WORKERS=4
DEFAULT_JOB_QUEUE=256

usage() {
  cat <<'EOF'
Usage: setup_control_one.sh [options]

Guided setup wizard to prepare Control One binaries and configuration
for new environments. By default the wizard runs interactively.

Options:
  --output DIR           Output directory for generated assets (default: ./build/wizard)
  --config-name NAME     Name of generated control plane config file (default: controlplane.yaml)
  --non-interactive      Run with sensible defaults and skip interactive prompts
  --no-certs             Skip TLS self-signed certificate generation
  --help                 Show this help message

Examples:
  ./scripts/wizard/setup_control_one.sh
  ./scripts/wizard/setup_control_one.sh --output ./dist/wizard --non-interactive
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --config-name)
      CONFIG_NAME="$2"
      shift 2
      ;;
    --non-interactive)
      NON_INTERACTIVE=true
      shift
      ;;
    --no-certs)
      GENERATE_CERTS=false
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

if [[ -z "$OUTPUT_DIR" ]]; then
  OUTPUT_DIR="$(pwd)/build/wizard"
fi

mkdir -p "$OUTPUT_DIR"
SUMMARY_FILE="$OUTPUT_DIR/wizard-summary.txt"
CONFIG_PATH="$OUTPUT_DIR/$CONFIG_NAME"
CERT_DIR="$OUTPUT_DIR/certs"

require_tool() {
  local tool="$1"
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: Required tool '$tool' not found in PATH" >&2
    exit 1
  fi
}

prompt() {
  local message="$1"
  local default="$2"
  local var
  if $NON_INTERACTIVE; then
    echo "$default"
    return
  fi
  read -rp "$message [$default]: " var
  if [[ -z "$var" ]]; then
    var="$default"
  fi
  echo "$var"
}

append_summary() {
  echo "$1" >>"$SUMMARY_FILE"
}

require_tool go
require_tool openssl || true

CONTROLPLANE_ADDR=$(prompt "HTTP listen address for Control Plane" "$DEFAULT_CONTROLPLANE_ADDR")
PROM_ADDR=$(prompt "Prometheus listen address" "$DEFAULT_PROM_ADDR")
POSTGRES_URL=$(prompt "PostgreSQL connection URL" "$DEFAULT_POSTGRES_URL")
DEFAULT_ADMIN_EMAIL=$(prompt "Initial admin email" "admin@$DEFAULT_DOMAIN")
DEFAULT_ORG=$(prompt "Organization name for certificates" "Control One")
DEFAULT_DOMAIN_INPUT=$(prompt "Primary DNS name for Control Plane" "$DEFAULT_DOMAIN")
REGISTRATION_TOKEN=$(prompt "Bootstrap token for agents" "$DEFAULT_REGISTRATION_TOKEN")
DEFAULT_TENANT=$(prompt "Default tenant name" "$DEFAULT_TENANT_NAME")
NODE_AGENT_API=$(prompt "Control Plane public URL" "https://$DEFAULT_DOMAIN_INPUT")

AUTH_CONFIG=$(cat <<EOF
auth:
  oidc:
    enabled: false
    issuer_url: ""
    client_id: ""
    audience: []
    username_claim: email
    groups_claim: groups
    cache_ttl: 5m
  rbac:
    default_role: viewer
    role_mappings: {}
EOF
)

OBSERVABILITY_CONFIG=$(cat <<EOF
observability:
  enable_metrics: true
  metrics_path: /metrics
EOF
)

WORKER_CONFIG=$(cat <<EOF
worker:
  concurrency: $DEFAULT_JOB_WORKERS
  queue_size: $DEFAULT_JOB_QUEUE
EOF
)

JOBS_CONFIG=$(cat <<'EOF'
jobs:
  provisioning:
    api_base_url: ""
    token: ""
    template: ""
    provider: ""
    baselines: []
    auto_remediation: true
    tls:
      cert_file: ""
      key_file: ""
      ca_cert_file: ""
  compliance:
    api_base_url: ""
    token: ""
    region: ""
    rule_sets: []
    certifications: []
    auto_apply: true
    tls:
      cert_file: ""
      key_file: ""
      ca_cert_file: ""
EOF
)

REGISTRATION_CONFIG=$(cat <<EOF
registration:
  bootstrap_tokens:
    - "$REGISTRATION_TOKEN"
  default_tenant_id: ""
EOF
)

TLS_CERT="$CERT_DIR/control-plane.crt"
TLS_KEY="$CERT_DIR/control-plane.key"
CA_CERT="$CERT_DIR/control-plane-ca.crt"

if $GENERATE_CERTS; then
  if ! command -v openssl >/dev/null 2>&1; then
    echo "WARNING: openssl not found, skipping certificate generation" >&2
    GENERATE_CERTS=false
  fi
fi

if $GENERATE_CERTS; then
  mkdir -p "$CERT_DIR"
  cat >"$CERT_DIR/openssl.cnf" <<EOF
[ req ]
default_bits       = 4096
distinguished_name = req_distinguished_name
x509_extensions    = v3_req
prompt             = no

[ req_distinguished_name ]
C  = US
ST = Example
L  = Example
O  = $DEFAULT_ORG
CN = $DEFAULT_DOMAIN_INPUT

[ v3_req ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = $DEFAULT_DOMAIN_INPUT
DNS.2 = control-plane
IP.1 = 127.0.0.1
EOF

  openssl req -x509 -nodes -days 825 -newkey rsa:4096 \
    -keyout "$TLS_KEY" \
    -out "$TLS_CERT" \
    -config "$CERT_DIR/openssl.cnf" >/dev/null 2>&1
  cp "$TLS_CERT" "$CA_CERT"
  append_summary "Generated TLS certificate: $TLS_CERT"
else
  append_summary "TLS certificates skipped. Provide your own and update config manually."
fi

cat >"$CONFIG_PATH" <<EOF
http:
  address: "$CONTROLPLANE_ADDR"
  read_timeout: 15s
  write_timeout: 15s

tls:
  enabled: true
  cert_file: "$TLS_CERT"
  key_file: "$TLS_KEY"
  client_ca_file: "$CA_CERT"
  require_client_tls: true

$OBSERVABILITY_CONFIG

database:
  url: "$POSTGRES_URL"
  max_open_conns: 20
  max_idle_conns: 5
  conn_max_lifetime: 15m
  apply_migrations: true

$WORKER_CONFIG

$JOBS_CONFIG

$AUTH_CONFIG

$REGISTRATION_CONFIG
EOF

append_summary "Generated Control Plane config: $CONFIG_PATH"

if go env GOMOD >/dev/null 2>&1; then
  pushd "$(go env GOMOD | xargs dirname)" >/dev/null
else
  echo "ERROR: Unable to determine go module root" >&2
  exit 1
fi

BUILD_DIR="$OUTPUT_DIR/bin"
mkdir -p "$BUILD_DIR"

go build -o "$BUILD_DIR/controlplane" ./controlplane/cmd/controlplane
go build -o "$BUILD_DIR/controlone-agent" ./cmd/nodeagent

append_summary "Compiled controlplane binary: $BUILD_DIR/controlplane"
append_summary "Compiled controlone-agent binary: $BUILD_DIR/controlone-agent"

cat >"$OUTPUT_DIR/README.md" <<EOF
# Control One Wizard Output

This bundle was generated on $(date -u) using \
	the Control One deployment wizard.

## Contents

- \
	t	the Control Plane binary: \
  bin/controlplane
- `bin/controlone-agent`: Control One agent binary with bootstrap wizard support.
- `$CONFIG_NAME`: Suggested Control Plane configuration.
- `certs/`: Self-signed TLS assets (replace with production certificates).
- `wizard-summary.txt`: Recap of automated steps.

## Next Steps

1. Provision a PostgreSQL instance reachable by the Control Plane.
2. Update `$CONFIG_NAME` with production OIDC and RBAC mappings.
3. Deploy the Control Plane binary using your preferred process manager or container runtime.
4. Distribute the node agent package together with your bootstrap token.
5. Optional: Regenerate TLS certificates with an approved CA and update the config paths.

Refer to `docs/architecture.md` for a broader system overview.
EOF

cat <<EOF
============================================
Control One Wizard completed successfully.
Artifacts available under: $OUTPUT_DIR

Summary:
$(cat "$SUMMARY_FILE")
============================================
EOF

popd >/dev/null
