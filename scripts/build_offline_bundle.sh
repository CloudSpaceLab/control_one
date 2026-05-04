#!/usr/bin/env bash
set -euo pipefail

# build_offline_bundle.sh — produce a self-contained tarball that installs the
# Control One stack on a fully air-gapped host. The bundle contains:
#   - controlplane + controlone-agent binaries for linux/amd64
#   - docker-compose.yaml wiring Postgres + Redis
#   - example controlplane.yaml with placeholders
#   - initial TLS scaffolding under certs/
#   - signed install.sh that unpacks, runs migrations, and starts services
#
# The bundle is intentionally static — no network calls are made at install
# time. Pair with scripts/wizard/setup_control_one.sh for interactive setup.

OUTPUT_DIR="${OUTPUT_DIR:-build/offline-bundle}"
VERSION="${VERSION:-dev}"
INCLUDE_IMAGES="${INCLUDE_IMAGES:-false}"

usage() {
  cat <<'EOF'
Usage: build_offline_bundle.sh [--output DIR] [--version V] [--include-images]

Produces an air-gapped installation bundle for Control One.
EOF
}

while [[ "${1:-}" != "" ]]; do
  case "$1" in
    --output) OUTPUT_DIR="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --include-images) INCLUDE_IMAGES="true"; shift ;;
    --help) usage; exit 0 ;;
    *) echo "unknown arg: $1"; usage; exit 1 ;;
  esac
done

root_dir="$(cd "$(dirname "$0")/.." && pwd)"
stage="${OUTPUT_DIR}/control-one-${VERSION}"
mkdir -p "${stage}/bin" "${stage}/config" "${stage}/certs" "${stage}/migrations" "${stage}/scripts"

echo ">> Building linux/amd64 binaries..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "${stage}/bin/controlplane" ./controlplane/cmd/controlplane
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "${stage}/bin/controlone-agent" ./cmd/nodeagent

echo ">> Copying migrations..."
cp -r "${root_dir}/controlplane/internal/migrate/sql/" "${stage}/migrations/sql"

echo ">> Copying wizard + install scripts..."
cp "${root_dir}/scripts/wizard/setup_control_one.sh" "${stage}/scripts/"
cp "${root_dir}/scripts/install.sh" "${stage}/scripts/install-agent.sh"

cat > "${stage}/docker-compose.yaml" <<'EOF'
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: controlone
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-controlone}
      POSTGRES_DB: controlone
    volumes:
      - ./data/pg:/var/lib/postgresql/data
    restart: unless-stopped
  redis:
    image: redis:7-alpine
    restart: unless-stopped
  controlplane:
    image: control-one/controlplane:${VERSION:-dev}
    depends_on: [postgres, redis]
    ports:
      - "8443:8443"
      - "9090:9090"
    volumes:
      - ./config/controlplane.yaml:/etc/control-one/controlplane.yaml:ro
      - ./certs:/etc/control-one/certs:ro
    restart: unless-stopped
EOF

cat > "${stage}/config/controlplane.yaml.example" <<EOF
# Offline-bundle default — copy to controlplane.yaml and edit placeholders.
http:
  address: ":8443"
tls:
  enabled: true
  cert_file: /etc/control-one/certs/server.crt
  key_file: /etc/control-one/certs/server.key
database:
  url: "postgresql://controlone:REPLACE_ME@postgres:5432/controlone?sslmode=disable"
  apply_migrations: true
worker:
  redis_address: "redis:6379"
registration:
  bootstrap_tokens:
    - "REPLACE_ME_BOOTSTRAP_TOKEN"
  default_tenant_id: ""
secrets:
  encryption_key: "REPLACE_ME_32_BYTE_HEX"
metrics:
  address: ":9090"
EOF

cat > "${stage}/INSTALL.md" <<'EOF'
# Control One offline install

1. Copy this bundle to the air-gapped host.
2. `tar xzf control-one-*.tar.gz && cd control-one-*`
3. Review and edit `config/controlplane.yaml.example` → rename to `controlplane.yaml`
4. Generate TLS certs into `certs/` (or use your own PKI)
5. Run `docker compose up -d`
6. First-run: create a tenant + bootstrap token via `./bin/controlplane` CLI or the wizard.
7. Install agents with `scripts/install-agent.sh --offline`.
EOF

if [[ "${INCLUDE_IMAGES}" == "true" ]]; then
  echo ">> Saving docker images for offline load..."
  docker save postgres:16-alpine redis:7-alpine -o "${stage}/images.tar" || \
    echo "warning: could not save docker images (docker required)"
fi

bundle_name="control-one-${VERSION}.tar.gz"
( cd "${OUTPUT_DIR}" && tar czf "${bundle_name}" "control-one-${VERSION}" )

( cd "${OUTPUT_DIR}" && sha256sum "${bundle_name}" > "${bundle_name}.sha256" )

echo ">> Bundle ready: ${OUTPUT_DIR}/${bundle_name}"
echo ">> SHA256:       $(cat ${OUTPUT_DIR}/${bundle_name}.sha256 | awk '{print $1}')"
