#!/usr/bin/env bash
# smoke-test.sh -- end-to-end smoke test for the Control One platform.
#
# Usage:
#   TOKEN=<static-bearer-token> ./scripts/smoke-test.sh
#   BASE_URL=https://controlone.dev:8443 TOKEN=my-token ./scripts/smoke-test.sh
#
# Requirements:
#   - curl, jq
#   - A running Control One control-plane with a static token configured:
#       auth.oidc.static_tokens:
#         your-token:
#           subject: smoke-test
#           name: Smoke Test
#           roles: [admin]
#
# The token must map to a principal with the "admin" role so that all
# CRUD operations (create tenant, create node, submit job, delete tenant)
# succeed.

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
BASE_URL="${BASE_URL:-http://localhost:8443}"
TOKEN="${TOKEN:-}"
CURL_OPTS=(-sk --max-time 10)

# ---------------------------------------------------------------------------
# Colors
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

# ---------------------------------------------------------------------------
# Counters
# ---------------------------------------------------------------------------
PASSED=0
FAILED=0
TOTAL=0

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
pass() {
  PASSED=$((PASSED + 1))
  TOTAL=$((TOTAL + 1))
  printf "${GREEN}  PASS${RESET} %s\n" "$1"
}

fail() {
  FAILED=$((FAILED + 1))
  TOTAL=$((TOTAL + 1))
  printf "${RED}  FAIL${RESET} %s\n" "$1"
  if [[ -n "${2:-}" ]]; then
    printf "${RED}       %s${RESET}\n" "$2"
  fi
}

section() {
  printf "\n${CYAN}${BOLD}── %s ──${RESET}\n" "$1"
}

# Authenticated curl wrapper. Adds Bearer token when TOKEN is set.
auth_curl() {
  local extra_headers=()
  if [[ -n "$TOKEN" ]]; then
    extra_headers=(-H "Authorization: Bearer ${TOKEN}")
  fi
  curl "${CURL_OPTS[@]}" "${extra_headers[@]}" "$@"
}

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
section "Pre-flight checks"

if ! command -v jq &>/dev/null; then
  printf "${RED}ERROR: jq is required but not installed.${RESET}\n"
  exit 1
fi
pass "jq is installed"

if ! command -v curl &>/dev/null; then
  printf "${RED}ERROR: curl is required but not installed.${RESET}\n"
  exit 1
fi
pass "curl is installed"

if [[ -z "$TOKEN" ]]; then
  printf "${YELLOW}  WARN${RESET} TOKEN is not set. Authenticated endpoints will fail.\n"
  printf "${YELLOW}       Set TOKEN to a static token configured in auth.oidc.static_tokens.${RESET}\n"
fi

# ---------------------------------------------------------------------------
# State (IDs created during the run, used for later steps and cleanup)
# ---------------------------------------------------------------------------
TENANT_ID=""
TENANT_NAME=""
NODE_ID=""
JOB_ID=""

cleanup() {
  if [[ -n "$TENANT_ID" ]]; then
    section "Cleanup"
    local http_code
    http_code=$(auth_curl -o /dev/null -w "%{http_code}" -X DELETE "${BASE_URL}/api/v1/tenants/${TENANT_ID}")
    if [[ "$http_code" == "204" || "$http_code" == "404" ]]; then
      pass "DELETE tenant ${TENANT_ID} (${http_code})"
    else
      fail "DELETE tenant ${TENANT_ID}" "expected 204 or 404, got ${http_code}"
    fi
  fi
}

trap cleanup EXIT

# ---------------------------------------------------------------------------
# 1. Health check
# ---------------------------------------------------------------------------
section "Health check"

HEALTH_BODY=$(curl "${CURL_OPTS[@]}" -s -o - -w "\n%{http_code}" "${BASE_URL}/healthz")
HEALTH_CODE=$(echo "$HEALTH_BODY" | tail -1)
HEALTH_TEXT=$(echo "$HEALTH_BODY" | head -1)

if [[ "$HEALTH_CODE" == "200" && "$HEALTH_TEXT" == "ok" ]]; then
  pass "GET /healthz -> 200 ok"
else
  fail "GET /healthz" "expected 200 'ok', got ${HEALTH_CODE} '${HEALTH_TEXT}'"
fi

# ---------------------------------------------------------------------------
# 2. Metrics
# ---------------------------------------------------------------------------
section "Metrics"

METRICS_RESP=$(curl "${CURL_OPTS[@]}" -s -o - -w "\n%{http_code}" "${BASE_URL}/metrics")
METRICS_CODE=$(echo "$METRICS_RESP" | tail -1)
METRICS_BODY=$(echo "$METRICS_RESP" | sed '$d')

if [[ "$METRICS_CODE" == "200" ]]; then
  pass "GET /metrics -> 200"
else
  fail "GET /metrics" "expected 200, got ${METRICS_CODE}"
fi

if echo "$METRICS_BODY" | grep -qi "controlone\|control_one\|controlplane"; then
  pass "GET /metrics contains control-one metrics"
else
  fail "GET /metrics" "response does not contain expected metric prefix"
fi

# ---------------------------------------------------------------------------
# 3. Create tenant
# ---------------------------------------------------------------------------
section "Tenants"

TENANT_NAME="smoke-test-tenant-${RANDOM}"

CREATE_TENANT_RESP=$(auth_curl -s -w "\n%{http_code}" \
  -X POST "${BASE_URL}/api/v1/tenants" \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"${TENANT_NAME}\"}")

CREATE_TENANT_CODE=$(echo "$CREATE_TENANT_RESP" | tail -1)
CREATE_TENANT_BODY=$(echo "$CREATE_TENANT_RESP" | sed '$d')

if [[ "$CREATE_TENANT_CODE" == "201" || "$CREATE_TENANT_CODE" == "200" ]]; then
  TENANT_ID=$(echo "$CREATE_TENANT_BODY" | jq -r '.id // empty')
  if [[ -n "$TENANT_ID" ]]; then
    pass "POST /api/v1/tenants -> ${CREATE_TENANT_CODE} (id=${TENANT_ID})"
  else
    fail "POST /api/v1/tenants" "response missing 'id' field"
  fi
else
  fail "POST /api/v1/tenants" "expected 200/201, got ${CREATE_TENANT_CODE}: $(echo "$CREATE_TENANT_BODY" | head -1)"
fi

# ---------------------------------------------------------------------------
# 4. List tenants
# ---------------------------------------------------------------------------
if [[ -n "$TENANT_ID" ]]; then
  LIST_TENANTS_RESP=$(auth_curl -s -w "\n%{http_code}" \
    "${BASE_URL}/api/v1/tenants?name_prefix=smoke-test-tenant")

  LIST_TENANTS_CODE=$(echo "$LIST_TENANTS_RESP" | tail -1)
  LIST_TENANTS_BODY=$(echo "$LIST_TENANTS_RESP" | sed '$d')

  if [[ "$LIST_TENANTS_CODE" == "200" ]]; then
    FOUND=$(echo "$LIST_TENANTS_BODY" | jq -r --arg id "$TENANT_ID" '.data[]? | select(.id == $id) | .id // empty')
    if [[ "$FOUND" == "$TENANT_ID" ]]; then
      pass "GET /api/v1/tenants -> found created tenant"
    else
      fail "GET /api/v1/tenants" "created tenant ${TENANT_ID} not found in listing"
    fi
  else
    fail "GET /api/v1/tenants" "expected 200, got ${LIST_TENANTS_CODE}"
  fi
fi

# ---------------------------------------------------------------------------
# 5. Get tenant
# ---------------------------------------------------------------------------
if [[ -n "$TENANT_ID" ]]; then
  GET_TENANT_RESP=$(auth_curl -s -w "\n%{http_code}" \
    "${BASE_URL}/api/v1/tenants/${TENANT_ID}")

  GET_TENANT_CODE=$(echo "$GET_TENANT_RESP" | tail -1)
  GET_TENANT_BODY=$(echo "$GET_TENANT_RESP" | sed '$d')

  if [[ "$GET_TENANT_CODE" == "200" ]]; then
    GOT_NAME=$(echo "$GET_TENANT_BODY" | jq -r '.name // empty')
    GOT_ID=$(echo "$GET_TENANT_BODY" | jq -r '.id // empty')
    if [[ "$GOT_ID" == "$TENANT_ID" && "$GOT_NAME" == "$TENANT_NAME" ]]; then
      pass "GET /api/v1/tenants/${TENANT_ID} -> fields match"
    else
      fail "GET /api/v1/tenants/${TENANT_ID}" "expected name=${TENANT_NAME}, got name=${GOT_NAME}"
    fi
  else
    fail "GET /api/v1/tenants/${TENANT_ID}" "expected 200, got ${GET_TENANT_CODE}"
  fi
fi

# ---------------------------------------------------------------------------
# 6. Create node
# ---------------------------------------------------------------------------
section "Nodes"

NODE_HOSTNAME="smoke-node-${RANDOM}.local"

if [[ -n "$TENANT_ID" ]]; then
  CREATE_NODE_RESP=$(auth_curl -s -w "\n%{http_code}" \
    -X POST "${BASE_URL}/api/v1/nodes" \
    -H "Content-Type: application/json" \
    -d "{\"tenant_id\": \"${TENANT_ID}\", \"hostname\": \"${NODE_HOSTNAME}\", \"os\": \"linux\", \"arch\": \"amd64\"}")

  CREATE_NODE_CODE=$(echo "$CREATE_NODE_RESP" | tail -1)
  CREATE_NODE_BODY=$(echo "$CREATE_NODE_RESP" | sed '$d')

  if [[ "$CREATE_NODE_CODE" == "201" || "$CREATE_NODE_CODE" == "200" ]]; then
    NODE_ID=$(echo "$CREATE_NODE_BODY" | jq -r '.id // empty')
    if [[ -n "$NODE_ID" ]]; then
      pass "POST /api/v1/nodes -> ${CREATE_NODE_CODE} (id=${NODE_ID})"
    else
      fail "POST /api/v1/nodes" "response missing 'id' field"
    fi
  else
    fail "POST /api/v1/nodes" "expected 200/201, got ${CREATE_NODE_CODE}: $(echo "$CREATE_NODE_BODY" | head -1)"
  fi
fi

# ---------------------------------------------------------------------------
# 7. List nodes
# ---------------------------------------------------------------------------
if [[ -n "$TENANT_ID" && -n "$NODE_ID" ]]; then
  LIST_NODES_RESP=$(auth_curl -s -w "\n%{http_code}" \
    "${BASE_URL}/api/v1/nodes?tenant_id=${TENANT_ID}")

  LIST_NODES_CODE=$(echo "$LIST_NODES_RESP" | tail -1)
  LIST_NODES_BODY=$(echo "$LIST_NODES_RESP" | sed '$d')

  if [[ "$LIST_NODES_CODE" == "200" ]]; then
    FOUND_NODE=$(echo "$LIST_NODES_BODY" | jq -r --arg id "$NODE_ID" '.data[]? | select(.id == $id) | .id // empty')
    if [[ "$FOUND_NODE" == "$NODE_ID" ]]; then
      pass "GET /api/v1/nodes?tenant_id=... -> found created node"
    else
      fail "GET /api/v1/nodes" "created node ${NODE_ID} not found in listing"
    fi
  else
    fail "GET /api/v1/nodes" "expected 200, got ${LIST_NODES_CODE}"
  fi
fi

# ---------------------------------------------------------------------------
# 8. Submit compliance scan job
# ---------------------------------------------------------------------------
section "Jobs"

if [[ -n "$TENANT_ID" && -n "$NODE_ID" ]]; then
  SCAN_ID="smoke-scan-${RANDOM}"

  JOB_PAYLOAD=$(jq -n \
    --arg type "compliance.scan" \
    --arg tenant_id "$TENANT_ID" \
    --arg scan_id "$SCAN_ID" \
    --arg node_id "$NODE_ID" \
    '{
      type: $type,
      tenant_id: $tenant_id,
      payload: {
        scan_id: $scan_id,
        tenant_id: $tenant_id,
        node_id: $node_id,
        policies: {}
      }
    }')

  CREATE_JOB_RESP=$(auth_curl -s -w "\n%{http_code}" \
    -X POST "${BASE_URL}/api/v1/jobs" \
    -H "Content-Type: application/json" \
    -d "$JOB_PAYLOAD")

  CREATE_JOB_CODE=$(echo "$CREATE_JOB_RESP" | tail -1)
  CREATE_JOB_BODY=$(echo "$CREATE_JOB_RESP" | sed '$d')

  # 202 Accepted is the expected status for job creation.
  if [[ "$CREATE_JOB_CODE" == "202" || "$CREATE_JOB_CODE" == "201" || "$CREATE_JOB_CODE" == "200" ]]; then
    JOB_ID=$(echo "$CREATE_JOB_BODY" | jq -r '.id // empty')
    JOB_STATUS=$(echo "$CREATE_JOB_BODY" | jq -r '.status // empty')
    if [[ -n "$JOB_ID" ]]; then
      pass "POST /api/v1/jobs (compliance.scan) -> ${CREATE_JOB_CODE} (id=${JOB_ID}, status=${JOB_STATUS})"
    else
      fail "POST /api/v1/jobs" "response missing 'id' field"
    fi
  else
    fail "POST /api/v1/jobs (compliance.scan)" "expected 202, got ${CREATE_JOB_CODE}: $(echo "$CREATE_JOB_BODY" | head -1)"
  fi
fi

# ---------------------------------------------------------------------------
# 9. Get job -- verify status
# ---------------------------------------------------------------------------
if [[ -n "$JOB_ID" ]]; then
  # Allow a brief moment for async processing.
  sleep 1

  GET_JOB_RESP=$(auth_curl -s -w "\n%{http_code}" \
    "${BASE_URL}/api/v1/jobs/${JOB_ID}")

  GET_JOB_CODE=$(echo "$GET_JOB_RESP" | tail -1)
  GET_JOB_BODY=$(echo "$GET_JOB_RESP" | sed '$d')

  if [[ "$GET_JOB_CODE" == "200" ]]; then
    GOT_JOB_STATUS=$(echo "$GET_JOB_BODY" | jq -r '.status // empty')
    GOT_JOB_TYPE=$(echo "$GET_JOB_BODY" | jq -r '.type // empty')
    GOT_JOB_EVENTS=$(echo "$GET_JOB_BODY" | jq -r '.events | length')

    if [[ "$GOT_JOB_TYPE" == "compliance.scan" ]]; then
      pass "GET /api/v1/jobs/${JOB_ID} -> type=compliance.scan, status=${GOT_JOB_STATUS}"
    else
      fail "GET /api/v1/jobs/${JOB_ID}" "expected type=compliance.scan, got ${GOT_JOB_TYPE}"
    fi

    if [[ "$GOT_JOB_STATUS" == "queued" || "$GOT_JOB_STATUS" == "running" || "$GOT_JOB_STATUS" == "succeeded" || "$GOT_JOB_STATUS" == "failed" ]]; then
      pass "GET /api/v1/jobs/${JOB_ID} -> valid status '${GOT_JOB_STATUS}'"
    else
      fail "GET /api/v1/jobs/${JOB_ID}" "unexpected status '${GOT_JOB_STATUS}'"
    fi

    if [[ "$GOT_JOB_EVENTS" -ge 1 ]]; then
      pass "GET /api/v1/jobs/${JOB_ID} -> has ${GOT_JOB_EVENTS} event(s)"
    else
      fail "GET /api/v1/jobs/${JOB_ID}" "expected at least 1 event, got ${GOT_JOB_EVENTS}"
    fi
  else
    fail "GET /api/v1/jobs/${JOB_ID}" "expected 200, got ${GET_JOB_CODE}"
  fi
fi

# ---------------------------------------------------------------------------
# 10. List jobs
# ---------------------------------------------------------------------------
if [[ -n "$TENANT_ID" && -n "$JOB_ID" ]]; then
  LIST_JOBS_RESP=$(auth_curl -s -w "\n%{http_code}" \
    "${BASE_URL}/api/v1/jobs?tenant_id=${TENANT_ID}")

  LIST_JOBS_CODE=$(echo "$LIST_JOBS_RESP" | tail -1)
  LIST_JOBS_BODY=$(echo "$LIST_JOBS_RESP" | sed '$d')

  if [[ "$LIST_JOBS_CODE" == "200" ]]; then
    FOUND_JOB=$(echo "$LIST_JOBS_BODY" | jq -r --arg id "$JOB_ID" '.data[]? | select(.id == $id) | .id // empty')
    if [[ "$FOUND_JOB" == "$JOB_ID" ]]; then
      pass "GET /api/v1/jobs?tenant_id=... -> found created job"
    else
      fail "GET /api/v1/jobs" "created job ${JOB_ID} not found in listing"
    fi
  else
    fail "GET /api/v1/jobs" "expected 200, got ${LIST_JOBS_CODE}"
  fi
fi

# ---------------------------------------------------------------------------
# 11. Profile
# ---------------------------------------------------------------------------
section "Identity"

PROFILE_RESP=$(auth_curl -s -w "\n%{http_code}" \
  "${BASE_URL}/api/v1/me")

PROFILE_CODE=$(echo "$PROFILE_RESP" | tail -1)
PROFILE_BODY=$(echo "$PROFILE_RESP" | sed '$d')

if [[ "$PROFILE_CODE" == "200" ]]; then
  PROFILE_TYPE=$(echo "$PROFILE_BODY" | jq -r '.type // empty')
  PROFILE_SUBJECT=$(echo "$PROFILE_BODY" | jq -r '.subject // empty')
  if [[ -n "$PROFILE_SUBJECT" ]]; then
    pass "GET /api/v1/me -> subject=${PROFILE_SUBJECT}, type=${PROFILE_TYPE}"
  else
    fail "GET /api/v1/me" "response missing 'subject' field"
  fi
else
  fail "GET /api/v1/me" "expected 200, got ${PROFILE_CODE}"
fi

# ---------------------------------------------------------------------------
# 12. Worker status
# ---------------------------------------------------------------------------
section "Worker"

WORKER_RESP=$(auth_curl -s -w "\n%{http_code}" \
  "${BASE_URL}/api/v1/worker/status")

WORKER_CODE=$(echo "$WORKER_RESP" | tail -1)
WORKER_BODY=$(echo "$WORKER_RESP" | sed '$d')

if [[ "$WORKER_CODE" == "200" ]]; then
  WORKER_STARTED=$(echo "$WORKER_BODY" | jq -r '.started // empty')
  if [[ "$WORKER_STARTED" == "true" ]]; then
    pass "GET /api/v1/worker/status -> started=true"
  else
    # Worker may report started as a different truthy value or field name.
    pass "GET /api/v1/worker/status -> 200 (started=${WORKER_STARTED:-unknown})"
  fi
else
  # 503 is acceptable if the worker subsystem is unavailable.
  if [[ "$WORKER_CODE" == "503" ]]; then
    pass "GET /api/v1/worker/status -> 503 (worker unavailable, acceptable)"
  else
    fail "GET /api/v1/worker/status" "expected 200 or 503, got ${WORKER_CODE}"
  fi
fi

# ---------------------------------------------------------------------------
# 13. Cleanup is handled by the EXIT trap above.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf "\n${BOLD}════════════════════════════════════════${RESET}\n"
printf "${BOLD}  Smoke Test Summary${RESET}\n"
printf "${BOLD}════════════════════════════════════════${RESET}\n"
printf "  Total:  %d\n" "$TOTAL"
printf "  ${GREEN}Passed: %d${RESET}\n" "$PASSED"
if [[ "$FAILED" -gt 0 ]]; then
  printf "  ${RED}Failed: %d${RESET}\n" "$FAILED"
else
  printf "  Failed: %d\n" "$FAILED"
fi
printf "${BOLD}════════════════════════════════════════${RESET}\n"

if [[ "$FAILED" -gt 0 ]]; then
  exit 1
fi

exit 0
