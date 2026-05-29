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

: "${OPENZITI_CONTROLLER_URL:?OPENZITI_CONTROLLER_URL is required}"

OPENZITI_INSTALLER_URL="${OPENZITI_INSTALLER_URL:-https://get.openziti.io/install.bash}"
OPENZITI_INSTALL_SCRIPT="${OPENZITI_INSTALL_SCRIPT:-./openziti-install.bash}"
FETCH_OPENZITI_INSTALLER="${FETCH_OPENZITI_INSTALLER:-false}"
RUN_OPENZITI_INSTALLER="${RUN_OPENZITI_INSTALLER:-false}"
CONTROL_ONE_URL="${CONTROL_ONE_URL:-http://localhost:8080}"
CONTROL_ONE_TENANT_ID="${CONTROL_ONE_TENANT_ID:-}"
CONTROL_ONE_TOKEN="${CONTROL_ONE_TOKEN:-}"
CONTROL_ONE_CREDENTIAL_ID="${CONTROL_ONE_CREDENTIAL_ID:-}"
OPENZITI_PROVIDER_ACCOUNT_ID="${OPENZITI_PROVIDER_ACCOUNT_ID:-openziti-prod}"
OPENZITI_DISPLAY_NAME="${OPENZITI_DISPLAY_NAME:-OpenZiti production}"
IMPORT_INTERVAL_SECONDS="${IMPORT_INTERVAL_SECONDS:-900}"
IMPORT_ENABLED="${IMPORT_ENABLED:-true}"
APPLY_CONTROL_ONE_PROVIDER="${APPLY_CONTROL_ONE_PROVIDER:-false}"
OUTPUT_FILE="${OUTPUT_FILE:-./control-one-provider-account.json}"

if [[ ! -f "${OPENZITI_INSTALL_SCRIPT}" ]]; then
  if is_true "${FETCH_OPENZITI_INSTALLER}"; then
    require_cmd curl
    echo "Downloading official OpenZiti installer to ${OPENZITI_INSTALL_SCRIPT}"
    curl -fsSL "${OPENZITI_INSTALLER_URL}" -o "${OPENZITI_INSTALL_SCRIPT}"
    chmod 0755 "${OPENZITI_INSTALL_SCRIPT}"
  else
    echo "OpenZiti installer not found at ${OPENZITI_INSTALL_SCRIPT}" >&2
    echo "Set FETCH_OPENZITI_INSTALLER=true to stage it, or pre-place the reviewed script." >&2
    exit 1
  fi
fi

if is_true "${RUN_OPENZITI_INSTALLER}"; then
  echo "Running reviewed OpenZiti installer for ${OPENZITI_CONTROLLER_URL}"
  bash "${OPENZITI_INSTALL_SCRIPT}"
else
  echo "OpenZiti installer staged at ${OPENZITI_INSTALL_SCRIPT}; set RUN_OPENZITI_INSTALLER=true to execute it."
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
  --arg provider "openziti" \
  --arg account_id "${OPENZITI_PROVIDER_ACCOUNT_ID}" \
  --arg display_name "${OPENZITI_DISPLAY_NAME}" \
  --arg endpoint_url "${OPENZITI_CONTROLLER_URL}" \
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
        identities: "/edge/management/v1/identities",
        services: "/edge/management/v1/services",
        service_policies: "/edge/management/v1/service-policies",
        connector_health: "/edge/management/v1/edge-routers",
        audit_events: "/edge/management/v1/events"
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
  echo "Applied Control One OpenZiti provider account."
fi
