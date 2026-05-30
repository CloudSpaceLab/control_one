#!/usr/bin/env bash
# Bootstrap script — runs on the host to bring up Control One end-to-end.
# Idempotent: re-run is safe.
set -euo pipefail

DOMAIN="${DOMAIN:-control-one.cloudspacetechs.com}"
FRAUDARCH_DOMAIN="${FRAUDARCH_DOMAIN:-nibss.cloudspacetechs.com}"
EMAIL="${LETSENCRYPT_EMAIL:-admin@cloudspacetechs.com}"
HERE="$(cd "$(dirname "$0")" && pwd)"

cd "${HERE}"

if [[ ! -f .env ]]; then
  echo ">> .env missing — copy .env.example to .env and fill in secrets, then re-run." >&2
  exit 1
fi

hostctl() {
  if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    echo ">> root or sudo is required to set Doris host prerequisites." >&2
    exit 1
  fi
}

ensure_doris_host_prereqs() {
  if command -v sysctl >/dev/null 2>&1; then
    current_map_count="$(sysctl -n vm.max_map_count 2>/dev/null || echo 0)"
    if [[ "${current_map_count:-0}" -lt 2000000 ]]; then
      echo ">> Setting vm.max_map_count=2000000 for Doris BE..."
      hostctl sysctl -w vm.max_map_count=2000000 >/dev/null
    fi
    if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
      printf 'vm.max_map_count=2000000\n' > /etc/sysctl.d/99-control-one-doris.conf
    elif command -v sudo >/dev/null 2>&1; then
      printf 'vm.max_map_count=2000000\n' | sudo tee /etc/sysctl.d/99-control-one-doris.conf >/dev/null
    fi
  fi

  if command -v swapon >/dev/null 2>&1 && swapon --noheadings --show | grep -q .; then
    echo ">> Disabling active host swap for Doris BE..."
    hostctl swapoff -a
  fi
}

doris_fe_cmdline() {
  docker compose exec -T doris-fe bash -lc 'for p in /proc/[0-9]*/cmdline; do cmd=$(tr "\0" " " < "$p" 2>/dev/null || true); case "$cmd" in *org.apache.doris.DorisFE*) printf "%s\n" "$cmd"; exit 0;; esac; done; exit 1'
}

ensure_doris_fe_heap_cap() {
  local cmdline
  cmdline="$(doris_fe_cmdline || true)"
  if [[ "${cmdline}" != *"-Xmx1500m"* || "${cmdline}" == *"-Xmx8192m"* ]]; then
    echo ">> Doris FE heap cap is not active; expected -Xmx1500m and no -Xmx8192m." >&2
    exit 1
  fi
}

echo ">> [1/5] Building images (this can take a few minutes the first time)..."
docker compose build

echo ">> [2/5] Starting Postgres + Redis..."
docker compose up -d postgres redis

# Wait for healthchecks to flip to healthy.
for svc in postgres redis; do
  for _ in {1..30}; do
    state=$(docker compose ps --format json "$svc" | head -1 | grep -o '"Health":"[a-z]*"' | head -1 | cut -d'"' -f4)
    if [[ "$state" == "healthy" ]]; then break; fi
    sleep 2
  done
done

echo ">> [3/5] Bootstrapping Doris (FE + BE)..."
ensure_doris_host_prereqs
docker compose up -d doris-fe doris-be
# Wait for FE health endpoint, then bootstrap database + add backend.
for _ in {1..60}; do
  if docker compose exec -T doris-fe curl -fs http://127.0.0.1:8030/api/health >/dev/null 2>&1; then
    break
  fi
  sleep 5
done
ensure_doris_fe_heap_cap
# Set the root password + create database + register the BE. All idempotent
# (BE-add silently fails on re-run; SET PASSWORD is no-op when same value).
DORIS_PASS="${DORIS_PASSWORD:-$(grep '^DORIS_PASSWORD=' .env | cut -d= -f2-)}"
docker compose exec -T doris-fe bash -lc "mysql -h127.0.0.1 -P9030 -uroot -e \"
  SET PASSWORD FOR 'root' = PASSWORD('${DORIS_PASS}');
  CREATE DATABASE IF NOT EXISTS controlone;
  ALTER SYSTEM ADD BACKEND 'doris-be:9050';
\" 2>&1" || true

echo ">> [4/5] Starting controlplane + console + landing..."
docker compose up -d controlplane console landing

echo ">> [5/5] Bootstrap nginx-edge with HTTP-only config so certbot can complete HTTP-01..."
# Use bootstrap config until certs exist.
if [[ ! -d "$(docker volume inspect -f '{{.Mountpoint}}' deploy_certbot-etc 2>/dev/null || echo /nonexistent)/live/${DOMAIN}" ]]; then
  cp -f nginx/edge-bootstrap.conf nginx/active.conf
else
  cp -f nginx/edge.conf nginx/active.conf
fi
# Swap nginx-edge mount to active.conf if not yet.
docker compose up -d nginx-edge

# Issue cert for control-one.cloudspacetechs.com if missing.
if ! docker compose run --rm certbot certbot certificates 2>/dev/null | grep -q "${DOMAIN}"; then
  echo ">> Requesting Let's Encrypt cert for ${DOMAIN}..."
  docker compose run --rm certbot certbot certonly --webroot \
    -w /var/www/certbot \
    -d "${DOMAIN}" \
    --email "${EMAIL}" \
    --agree-tos --no-eff-email \
    --non-interactive
  cp -f nginx/edge.conf nginx/active.conf
  docker compose exec nginx-edge nginx -s reload || docker compose restart nginx-edge
fi

# Issue cert for nibss.cloudspacetechs.com (FraudArch) if domain is set and cert missing.
if [[ -n "${FRAUDARCH_DOMAIN}" ]]; then
  if ! docker compose run --rm certbot certbot certificates 2>/dev/null | grep -q "${FRAUDARCH_DOMAIN}"; then
    echo ">> Requesting Let's Encrypt cert for ${FRAUDARCH_DOMAIN}..."
    docker compose run --rm certbot certbot certonly --webroot \
      -w /var/www/certbot \
      -d "${FRAUDARCH_DOMAIN}" \
      --email "${EMAIL}" \
      --agree-tos --no-eff-email \
      --non-interactive
  fi
  # Add FraudArch config to nginx if not already present
  if [[ -f nginx/fraudarch.conf ]] && ! grep -q "nibss.cloudspacetechs.com" nginx/active.conf; then
    echo ">> Adding FraudArch nginx configuration..."
    cat nginx/fraudarch.conf >> nginx/active.conf
    docker compose exec nginx-edge nginx -s reload || docker compose restart nginx-edge
  fi
fi

echo ">> [6/6] Starting certbot renewal sidecar..."
docker compose up -d certbot

echo ""
echo "Bootstrap complete."
echo "  Landing: https://${DOMAIN}/"
echo "  Console: https://${DOMAIN}/console/"
echo "  API:     https://${DOMAIN}/api/v1/ping"
