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

  local url="${BASE_URL}/api/v1${path}"
  local out
  out="$(mktemp)"
  local code

  code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"

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

  log "== visit resource regression =="

  request GET "/visits/health" 200 '.' "visits-health"

  local list_json
  list_json="$(request GET "/visits?page=1&page_size=20&tenant_id=${TENANT_ID}" 200 '(.items|type=="array") and (.total >= 0)' "visits-list")"

  request GET "/visits/statistics?tenant_id=${TENANT_ID}" 200 '.' "visits-statistics"
  request GET "/visits/filters?tenant_id=${TENANT_ID}" 200 '.' "visits-filters"

  local visit_id
  visit_id="$(echo "$list_json" | jq -r '.items[0].id // empty')"
  if [[ -n "$visit_id" ]]; then
    request GET "/visits/${visit_id}" 200 '.id != null' "visit-detail"
  else
    pass "visit-detail skipped (empty list)"
  fi

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"
  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
