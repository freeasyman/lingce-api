#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"
TENANT_ID="${TENANT_ID:-1}"

PASS_COUNT=0
FAIL_COUNT=0

log() {
  printf '%s\n' "$*" >&2
}

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  log "PASS: $*"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  log "FAIL: $*"
}

request() {
  local method="$1"
  local path="$2"
  local expected_http="$3"
  local check_expr="$4"
  local name="$5"
  local body="${6:-}"

  local url="${BASE_URL}/api/v1${path}"
  local out
  out="$(mktemp)"
  local code

  if [[ -n "$body" ]]; then
    code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"
  else
    code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"
  fi

  if [[ "$code" != "$expected_http" ]]; then
    fail "${name} (http=${code}, expected=${expected_http})"
    cat "$out" >&2
    rm -f "$out"
    return 1
  fi

  if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$out" >/dev/null 2>&1; then
    fail "${name} (body assertion failed: ${check_expr})"
    cat "$out" >&2
    rm -f "$out"
    return 1
  fi

  pass "$name"
  cat "$out"
  rm -f "$out"
}

main() {
  command -v curl >/dev/null 2>&1 || { log "curl is required"; exit 1; }
  command -v jq >/dev/null 2>&1 || { log "jq is required"; exit 1; }

  if [[ -z "$TOKEN" ]]; then
    log "TOKEN is required"
    exit 1
  fi

  log "== customer resource regression =="

  local list_json
  list_json="$(request GET "/customers?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 '(.items|type=="array") and (.total >= 0)' "customers-list")"

  local customer_id
  customer_id="$(echo "$list_json" | jq -r '.items[0].id // empty')"
  if [[ -n "$customer_id" ]]; then
    request GET "/customers/${customer_id}" 200 '.id > 0' "customer-detail"
    request GET "/customers/${customer_id}/momentum-history" 200 '.' "customer-momentum-history"
    request GET "/customers/${customer_id}/consultation-records?page=1&page_size=10" 200 '.' "customer-consultation-records"
    request GET "/customers/${customer_id}/emr-records?page=1&page_size=10" 200 '.' "customer-emr-records"
  else
    pass "customer-detail-related skipped (empty list)"
  fi

  request GET "/customers/stats/overview?tenant_id=${TENANT_ID}" 200 '.' "customers-stats-overview"
  request GET "/customers/duplicates?tenant_id=${TENANT_ID}&phone=13900139000" 200 '.' "customers-duplicates-by-phone"
  request POST "/customers/actions/merge" 400 '.code=="BAD_REQUEST"' "customers-merge-invalid" '{"target_id":0,"source_ids":[]}'

  local tags_json groups_json
  tags_json="$(request GET "/customers/tags?tenant_id=${TENANT_ID}&limit=200" 200 '(.items|type=="array") and (.total >= 0)' "customer-tags-list")"
  groups_json="$(request GET "/customers/groups?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 '(.items|type=="array") and (.total >= 0)' "customer-groups-list")"

  request GET "/customers/tags/stats?tenant_id=${TENANT_ID}" 200 '.' "customer-tags-stats"
  request POST "/customers/tags/batch?tenant_id=${TENANT_ID}" 400 '.code=="BAD_REQUEST"' "customer-tags-batch-invalid" '{"customer_ids":[],"tag_ids":[],"action":"add"}'
  request POST "/customers/groups/rules/validate" 200 '.' "customer-groups-rules-validate" '{"rules":{}}'
  request POST "/customers/groups/rules/preview?tenant_id=${TENANT_ID}" 400 '.code=="BAD_REQUEST"' "customer-groups-rules-preview-invalid" '{"rules":{}}'

  local group_id
  group_id="$(echo "$groups_json" | jq -r '.items[0].id // empty')"
  if [[ -n "$group_id" && -n "$customer_id" ]]; then
    request GET "/customers/groups/${group_id}/members?page=1&page_size=20" 200 '.' "customer-group-members-list"
    request POST "/customers/groups/${group_id}/members" 200 '.' "customer-group-members-add" "{\"customer_ids\":[${customer_id}]}"
    request DELETE "/customers/groups/${group_id}/members" 200 '.' "customer-group-members-remove" "{\"customer_ids\":[${customer_id}]}"
  else
    pass "customer-group-members tests skipped (missing group/customer)"
  fi

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"
  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
