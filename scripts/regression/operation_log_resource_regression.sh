#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"

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

request_any() {
  local method="$1"
  local path="$2"
  local expected_csv="$3"
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

  local matched=0
  local expected
  IFS=',' read -r -a expected_arr <<< "$expected_csv"
  for expected in "${expected_arr[@]}"; do
    if [[ "$code" == "$expected" ]]; then
      matched=1
      break
    fi
  done

  if [[ "$matched" -ne 1 ]]; then
    fail "${name} (http=${code}, expected one of ${expected_csv})"
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

  pass "${name} (${code})"
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

  log "== operation-log resource regression =="

  local list_json
  list_json="$(request_any GET "/operation-logs?page=1&page_size=20" "200,403" '.' "operation-logs-list")"
  request_any GET "/operation-logs/stats" "200,403" '.' "operation-logs-stats"
  request_any GET "/operation-logs/data-browser/tables" "200,403" '.' "data-browser-tables"
  request_any GET "/operation-logs/data-browser/statistics" "200,403" '.' "data-browser-statistics"

  local list_code
  list_code="$(echo "$list_json" | jq -r 'if .code == "FORBIDDEN" then "403" else "200" end' 2>/dev/null || echo "200")"
  if [[ "$list_code" == "200" ]]; then
    local log_id
    log_id="$(echo "$list_json" | jq -r '.items[0].id // empty')"
    if [[ -n "$log_id" ]]; then
      request_any GET "/operation-logs/${log_id}" "200" '.id > 0' "operation-logs-detail"
    else
      pass "operation-logs-detail skipped (empty list)"
    fi
  else
    pass "operation-logs-detail skipped (forbidden role)"
  fi

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"
  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
