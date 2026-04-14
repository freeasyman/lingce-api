#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"
TENANT_ID="${TENANT_ID:-1}"

if [[ -z "${TOKEN}" ]]; then
  echo "TOKEN is required."
  echo "Example:"
  echo "  TOKEN=xxxxx BASE_URL=http://127.0.0.1:18080 TENANT_ID=1 bash scripts/regression/resource_dod_smoke.sh"
  exit 1
fi

auth_header=(-H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json")
fail_count=0

request_status() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  if [[ -n "${body}" ]]; then
    curl -sS -o /tmp/dod_smoke_resp.json -w "%{http_code}" "${auth_header[@]}" -X "${method}" "${BASE_URL}${path}" -d "${body}"
  else
    curl -sS -o /tmp/dod_smoke_resp.json -w "%{http_code}" "${auth_header[@]}" -X "${method}" "${BASE_URL}${path}"
  fi
}

check_not_missing() {
  local name="$1"
  local method="$2"
  local path="$3"
  local body="${4:-}"
  local status
  status="$(request_status "${method}" "${path}" "${body}")"
  if [[ "${status}" == "404" ]]; then
    echo "FAIL ${name}: ${method} ${path} -> ${status}"
    fail_count=$((fail_count + 1))
  else
    echo "PASS ${name}: ${method} ${path} -> ${status}"
  fi
}

echo "== DoD resource smoke =="
check_not_missing "auth me" GET "/api/v1/auth/me"
check_not_missing "tenants list" GET "/api/v1/tenants?page=1&page_size=1"
check_not_missing "departments list" GET "/api/v1/departments?tenant_id=${TENANT_ID}&page=1&page_size=1"
check_not_missing "employees list" GET "/api/v1/employees?tenant_id=${TENANT_ID}&page=1&page_size=1"
check_not_missing "customers list" GET "/api/v1/customers?tenant_id=${TENANT_ID}&page=1&page_size=1"
check_not_missing "recordings list" GET "/api/v1/recordings?tenant_id=${TENANT_ID}&page=1&page_size=1"
check_not_missing "recording-tasks list" GET "/api/v1/recording-tasks?tenant_id=${TENANT_ID}&page=1&page_size=1"
check_not_missing "badge-devices list" GET "/api/v1/badge-devices?page=1&page_size=1"
check_not_missing "badge-tickets list" GET "/api/v1/badge-tickets?page=1&page_size=1"
check_not_missing "content-topics list" GET "/api/v1/content-topics?page=1&page_size=1&tenant_id=${TENANT_ID}"
check_not_missing "content-items list" GET "/api/v1/content-items?page=1&page_size=1&tenant_id=${TENANT_ID}"
check_not_missing "content-seeds list" GET "/api/v1/content-seeds?page=1&page_size=1&tenant_id=${TENANT_ID}"
check_not_missing "roles list" GET "/api/v1/roles?scope=ops&page=1&page_size=1"
check_not_missing "menus list" GET "/api/v1/menus?scope=ops"
check_not_missing "notifications list" GET "/api/v1/notifications?page=1&page_size=1"
check_not_missing "operation-logs list" GET "/api/v1/operation-logs?page=1&page_size=1"
check_not_missing "subscription-plans list" GET "/api/v1/subscription-plans?page=1&page_size=1"
check_not_missing "feature-groups list" GET "/api/v1/feature-groups?page=1&page_size=1"
check_not_missing "llm models list" GET "/api/v1/llm/models?page=1&page_size=1"
check_not_missing "visits list" GET "/api/v1/visits?page=1&page_size=1&tenant_id=${TENANT_ID}"

echo "== Action endpoints =="
check_not_missing "notifications read-all action" POST "/api/v1/notifications/actions/read-all" "{}"
check_not_missing "recordings batch-delete action" POST "/api/v1/recordings/actions/batch-delete" "{\"recording_ids\":[]}"
check_not_missing "badge-devices batch-health-check action" POST "/api/v1/badge-devices/actions/batch-health-check" "{\"device_ids\":[]}"
check_not_missing "customers merge action" POST "/api/v1/customers/actions/merge" "{\"source_ids\":[]}"
check_not_missing "tenants renew action" POST "/api/v1/tenants/${TENANT_ID}/subscription/actions/renew" "{\"duration_days\":30}"
check_not_missing "recordings batch-transcribe action" POST "/api/v1/recordings/actions/batch-transcribe" "{\"recording_ids\":[]}"

echo "== Summary =="
if [[ "${fail_count}" -gt 0 ]]; then
  echo "FAILED: ${fail_count} route(s) returned 404"
  exit 1
fi
echo "PASSED: no 404 route failures"
