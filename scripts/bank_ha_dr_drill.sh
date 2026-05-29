#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: bank_ha_dr_drill.sh <backup|restore-smoke|failover-smoke>

Environment:
  ARTIFACT_DIR                 Output directory for drill evidence.
  DRILL_ID                     Optional drill identifier. Defaults to timestamp.
  POSTGRES_URL                 Source Postgres URL for backup mode.
  POSTGRES_DUMP                Existing pg_dump file for restore-smoke mode.
  RESTORE_POSTGRES_URL         Isolated restore database URL.
  ALLOW_RESTORE=true           Required before restore-smoke writes to RESTORE_POSTGRES_URL.
  OFFLINE_CONTENT_ROOT         Offline content root to archive in backup mode.
  OFFLINE_CONTENT_ARCHIVE      Offline content archive to list in restore-smoke mode.
  CONTROL_ONE_CONFIG           Config file to checksum as restore evidence.
  CONTROL_ONE_URL              Control-plane URL for failover-smoke health checks.
  CONTROL_ONE_TOKEN            Optional bearer token for tenant-scoped smoke checks.
  TENANT_ID                    Tenant used for tenant-scoped smoke checks.
  DORIS_FE_HTTP_URL            Optional Doris FE HTTP URL for health evidence.

The script is non-destructive unless ALLOW_RESTORE=true and RESTORE_POSTGRES_URL
are set for restore-smoke.
EOF
}

mode="${1:-backup}"
case "${mode}" in
  backup|restore-smoke|failover-smoke) ;;
  --help|-h) usage; exit 0 ;;
  *) usage >&2; exit 2 ;;
esac

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
drill_id="${DRILL_ID:-${timestamp}}"
artifact_dir="${ARTIFACT_DIR:-build/ha-dr-drills/${drill_id}}"
mkdir -p "${artifact_dir}"
evidence_file="${artifact_dir}/evidence.ndjson"

json_escape() {
  local value="${1:-}"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/ }"
  value="${value//$'\r'/ }"
  printf '%s' "${value}"
}

record() {
  local check="$1"
  local status="$2"
  local detail="${3:-}"
  printf '{"timestamp":"%s","drill_id":"%s","mode":"%s","check":"%s","status":"%s","detail":"%s"}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    "$(json_escape "${drill_id}")" \
    "$(json_escape "${mode}")" \
    "$(json_escape "${check}")" \
    "$(json_escape "${status}")" \
    "$(json_escape "${detail}")" | tee -a "${evidence_file}" >/dev/null
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    record "$1" "failed" "required command not found"
    exit 1
  fi
}

sha_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
  else
    shasum -a 256 "${file}" | awk '{print $1}'
  fi
}

backup_postgres() {
  if [[ -z "${POSTGRES_URL:-}" ]]; then
    record "postgres_backup" "skipped" "POSTGRES_URL not set"
    return
  fi
  require_cmd pg_dump
  local out="${artifact_dir}/postgres-${drill_id}.dump"
  pg_dump --format=custom --no-owner --no-privileges --file "${out}" "${POSTGRES_URL}"
  record "postgres_backup" "ok" "${out} sha256=$(sha_file "${out}")"

  if command -v pg_dumpall >/dev/null 2>&1; then
    local globals="${artifact_dir}/postgres-globals-${drill_id}.sql"
    pg_dumpall --globals-only --dbname "${POSTGRES_URL}" --file "${globals}" || record "postgres_globals" "warning" "pg_dumpall failed; verify role export separately"
    [[ -f "${globals}" ]] && record "postgres_globals" "ok" "${globals} sha256=$(sha_file "${globals}")"
  else
    record "postgres_globals" "warning" "pg_dumpall not found"
  fi
}

backup_offline_content() {
  if [[ -z "${OFFLINE_CONTENT_ROOT:-}" ]]; then
    record "offline_content_backup" "skipped" "OFFLINE_CONTENT_ROOT not set"
    return
  fi
  if [[ ! -d "${OFFLINE_CONTENT_ROOT}" ]]; then
    record "offline_content_backup" "failed" "root not found: ${OFFLINE_CONTENT_ROOT}"
    exit 1
  fi
  require_cmd tar
  local out="${artifact_dir}/offline-content-${drill_id}.tar.gz"
  tar -C "${OFFLINE_CONTENT_ROOT}" -czf "${out}" active bundles
  record "offline_content_backup" "ok" "${out} sha256=$(sha_file "${out}")"
}

checksum_control_plane_config() {
  if [[ -z "${CONTROL_ONE_CONFIG:-}" ]]; then
    record "control_plane_config_checksum" "skipped" "CONTROL_ONE_CONFIG not set"
    return
  fi
  if [[ ! -f "${CONTROL_ONE_CONFIG}" ]]; then
    record "control_plane_config_checksum" "failed" "config not found: ${CONTROL_ONE_CONFIG}"
    exit 1
  fi
  record "control_plane_config_checksum" "ok" "${CONTROL_ONE_CONFIG} sha256=$(sha_file "${CONTROL_ONE_CONFIG}")"
}

restore_smoke_postgres() {
  local dump="${POSTGRES_DUMP:-}"
  if [[ -z "${dump}" ]]; then
    dump="$(find "${artifact_dir}" -maxdepth 1 -name 'postgres-*.dump' -print -quit 2>/dev/null || true)"
  fi
  if [[ -z "${dump}" || ! -f "${dump}" ]]; then
    record "postgres_restore_manifest" "skipped" "POSTGRES_DUMP not set and no dump found in artifact dir"
    return
  fi
  require_cmd pg_restore
  pg_restore --list "${dump}" > "${artifact_dir}/postgres-restore-list-${drill_id}.txt"
  record "postgres_restore_manifest" "ok" "${dump} list sha256=$(sha_file "${artifact_dir}/postgres-restore-list-${drill_id}.txt")"

  if [[ "${ALLOW_RESTORE:-false}" == "true" && -n "${RESTORE_POSTGRES_URL:-}" ]]; then
    pg_restore --exit-on-error --no-owner --no-privileges --dbname "${RESTORE_POSTGRES_URL}" "${dump}"
    record "postgres_restore_apply" "ok" "restored into isolated RESTORE_POSTGRES_URL"
  else
    record "postgres_restore_apply" "skipped" "set ALLOW_RESTORE=true and RESTORE_POSTGRES_URL for isolated restore apply"
  fi
}

restore_smoke_offline_content() {
  if [[ -z "${OFFLINE_CONTENT_ARCHIVE:-}" ]]; then
    record "offline_content_restore_manifest" "skipped" "OFFLINE_CONTENT_ARCHIVE not set"
    return
  fi
  if [[ ! -f "${OFFLINE_CONTENT_ARCHIVE}" ]]; then
    record "offline_content_restore_manifest" "failed" "archive not found: ${OFFLINE_CONTENT_ARCHIVE}"
    exit 1
  fi
  require_cmd tar
  tar -tzf "${OFFLINE_CONTENT_ARCHIVE}" > "${artifact_dir}/offline-content-list-${drill_id}.txt"
  record "offline_content_restore_manifest" "ok" "${OFFLINE_CONTENT_ARCHIVE} list sha256=$(sha_file "${artifact_dir}/offline-content-list-${drill_id}.txt")"
}

failover_smoke_control_plane() {
  if [[ -z "${CONTROL_ONE_URL:-}" ]]; then
    record "control_plane_health" "skipped" "CONTROL_ONE_URL not set"
    return
  fi
  require_cmd curl
  local base="${CONTROL_ONE_URL%/}"
  curl -fsS "${base}/healthz" > "${artifact_dir}/healthz-${drill_id}.txt"
  record "control_plane_health" "ok" "${base}/healthz"
  if curl -fsS "${base}/healthz/deep" > "${artifact_dir}/healthz-deep-${drill_id}.txt"; then
    record "control_plane_deep_health" "ok" "${base}/healthz/deep"
  else
    record "control_plane_deep_health" "warning" "deep health failed; inspect restored dependencies"
  fi
}

failover_smoke_private_access() {
  if [[ -z "${CONTROL_ONE_URL:-}" || -z "${CONTROL_ONE_TOKEN:-}" || -z "${TENANT_ID:-}" ]]; then
    record "private_access_reconcile_smoke" "skipped" "CONTROL_ONE_URL, CONTROL_ONE_TOKEN, and TENANT_ID not all set"
    return
  fi
  require_cmd curl
  local base="${CONTROL_ONE_URL%/}"
  curl -fsS -X POST \
    -H "Authorization: Bearer ${CONTROL_ONE_TOKEN}" \
    "${base}/api/v1/private-access/exposure/reconcile?tenant_id=${TENANT_ID}" \
    > "${artifact_dir}/private-access-reconcile-${drill_id}.json"
  record "private_access_reconcile_smoke" "ok" "response sha256=$(sha_file "${artifact_dir}/private-access-reconcile-${drill_id}.json")"
}

failover_smoke_doris() {
  if [[ -z "${DORIS_FE_HTTP_URL:-}" ]]; then
    record "doris_fe_health" "skipped" "DORIS_FE_HTTP_URL not set"
    return
  fi
  require_cmd curl
  curl -fsS "${DORIS_FE_HTTP_URL%/}/api/bootstrap" > "${artifact_dir}/doris-fe-${drill_id}.txt" || {
    record "doris_fe_health" "warning" "Doris FE health endpoint failed; verify FE quorum manually"
    return
  }
  record "doris_fe_health" "ok" "${DORIS_FE_HTTP_URL%/}/api/bootstrap"
}

record "drill_started" "ok" "artifact_dir=${artifact_dir}"
case "${mode}" in
  backup)
    backup_postgres
    backup_offline_content
    checksum_control_plane_config
    ;;
  restore-smoke)
    restore_smoke_postgres
    restore_smoke_offline_content
    checksum_control_plane_config
    ;;
  failover-smoke)
    failover_smoke_control_plane
    failover_smoke_doris
    failover_smoke_private_access
    ;;
esac
record "drill_completed" "ok" "evidence=${evidence_file}"
echo "Evidence written to ${evidence_file}"
