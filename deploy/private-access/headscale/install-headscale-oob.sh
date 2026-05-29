#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"
if [[ -f "${ENV_FILE}" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

is_true() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|y|Y) return 0 ;;
    *) return 1 ;;
  esac
}

require_cmd jq

: "${HEADSCALE_URL:?HEADSCALE_URL is required}"

HEADSCALE_INSTALLER_URL="${HEADSCALE_INSTALLER_URL:-https://raw.githubusercontent.com/juanfont/headscale/main/docs/setup/install/official.md}"
HEADSCALE_INSTALL_SCRIPT="${HEADSCALE_INSTALL_SCRIPT:-./headscale-install-notes.md}"
FETCH_HEADSCALE_INSTALLER="${FETCH_HEADSCALE_INSTALLER:-false}"
RUN_HEADSCALE_INSTALLER="${RUN_HEADSCALE_INSTALLER:-false}"
HEADSCALE_INSTALL_CMD="${HEADSCALE_INSTALL_CMD:-}"
CONTROL_ONE_URL="${CONTROL_ONE_URL:-http://localhost:8080}"
CONTROL_ONE_TENANT_ID="${CONTROL_ONE_TENANT_ID:-}"
CONTROL_ONE_TOKEN="${CONTROL_ONE_TOKEN:-}"
CONTROL_ONE_CREDENTIAL_ID="${CONTROL_ONE_CREDENTIAL_ID:-}"
HEADSCALE_PROVIDER_ACCOUNT_ID="${HEADSCALE_PROVIDER_ACCOUNT_ID:-headscale-prod}"
HEADSCALE_DISPLAY_NAME="${HEADSCALE_DISPLAY_NAME:-Headscale production}"
IMPORT_INTERVAL_SECONDS="${IMPORT_INTERVAL_SECONDS:-900}"
IMPORT_ENABLED="${IMPORT_ENABLED:-true}"
APPLY_CONTROL_ONE_PROVIDER="${APPLY_CONTROL_ONE_PROVIDER:-false}"
OUTPUT_FILE="${OUTPUT_FILE:-./control-one-provider-account.json}"

if [[ ! -f "${HEADSCALE_INSTALL_SCRIPT}" ]]; then
  if is_true "${FETCH_HEADSCALE_INSTALLER}"; then
    require_cmd curl
    echo "Downloading official Headscale install notes to ${HEADSCALE_INSTALL_SCRIPT}"
    curl -fsSL "${HEADSCALE_INSTALLER_URL}" -o "${HEADSCALE_INSTALL_SCRIPT}"
  else
    echo "Headscale install notes not found at ${HEADSCALE_INSTALL_SCRIPT}" >&2
    echo "Set FETCH_HEADSCALE_INSTALLER=true to stage them, or pre-place the reviewed notes." >&2
    exit 1
  fi
fi

if is_true "${RUN_HEADSCALE_INSTALLER}"; then
  if [[ -z "${HEADSCALE_INSTALL_CMD}" ]]; then
    echo "HEADSCALE_INSTALL_CMD is required when RUN_HEADSCALE_INSTALLER=true." >&2
    echo "This bundle avoids implicit package-manager changes; provide the bank-reviewed command explicitly." >&2
    exit 1
  fi
  echo "Running reviewed Headscale install command for ${HEADSCALE_URL}"
  bash -c "${HEADSCALE_INSTALL_CMD}"
else
  echo "Headscale install notes staged at ${HEADSCALE_INSTALL_SCRIPT}; set RUN_HEADSCALE_INSTALLER=true with HEADSCALE_INSTALL_CMD to execute a reviewed install."
fi

effective_import_enabled="${IMPORT_ENABLED}"
if is_true "${IMPORT_ENABLED}" && [[ -z "${CONTROL_ONE_CREDENTIAL_ID}" ]]; then
  echo "CONTROL_ONE_CREDENTIAL_ID is empty; generated provider account will have import_enabled=false." >&2
  effective_import_enabled="false"
fi

import_enabled_json="false"
if is_true "${effective_import_enabled}"; then
  import_enabled_json="true"
fi

jq -n \
  --arg provider "headscale" \
  --arg account_id "${HEADSCALE_PROVIDER_ACCOUNT_ID}" \
  --arg display_name "${HEADSCALE_DISPLAY_NAME}" \
  --arg endpoint_url "${HEADSCALE_URL}" \
  --arg credential_id "${CONTROL_ONE_CREDENTIAL_ID}" \
  --argjson import_enabled "${import_enabled_json}" \
  --argjson import_interval_seconds "${IMPORT_INTERVAL_SECONDS}" \
  '{
    provider: $provider,
    account_id: $account_id,
    display_name: $display_name,
    endpoint_url: $endpoint_url,
    config: {
      endpoints: {
        nodes: "/api/v1/node",
        routes: "/api/v1/routes",
        users: "/api/v1/user",
        acls: "/api/v1/policy"
      },
      timeout_seconds: 30,
      tls_skip_verify: false
    },
    import_enabled: $import_enabled,
    import_interval_seconds: $import_interval_seconds
  } + (if $credential_id == "" then {} else {credential_id: $credential_id} end)' \
  > "${OUTPUT_FILE}"

echo "Wrote Control One provider-account request to ${OUTPUT_FILE}"

if is_true "${APPLY_CONTROL_ONE_PROVIDER}"; then
  require_cmd curl
  if [[ -z "${CONTROL_ONE_TENANT_ID}" || -z "${CONTROL_ONE_TOKEN}" || -z "${CONTROL_ONE_CREDENTIAL_ID}" ]]; then
    echo "CONTROL_ONE_TENANT_ID, CONTROL_ONE_TOKEN, and CONTROL_ONE_CREDENTIAL_ID are required to apply." >&2
    exit 1
  fi
  curl -fsS \
    -H "Authorization: Bearer ${CONTROL_ONE_TOKEN}" \
    -H "Content-Type: application/json" \
    --data-binary "@${OUTPUT_FILE}" \
    "${CONTROL_ONE_URL%/}/api/v1/private-access/provider-accounts?tenant_id=${CONTROL_ONE_TENANT_ID}"
  echo
  echo "Applied Control One Headscale provider account."
fi
