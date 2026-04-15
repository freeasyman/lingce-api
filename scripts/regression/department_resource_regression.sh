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

  log "== department resource regression =="

  local list_json
  list_json="$(request_any GET "/departments?tenant_id=${TENANT_ID}&page=1&page_size=20" "200" '(.items|type=="array") and (.total >= 0)' "departments-list")"
  request_any GET "/departments/health" "200" '.status=="ok"' "departments-health"

  local dept_id
  dept_id="$(echo "$list_json" | jq -r '.items[0].id // empty')"
  if [[ -n "$dept_id" ]]; then
    request_any GET "/departments/${dept_id}" "200" '.id > 0' "department-detail"
    request_any GET "/departments/${dept_id}/performance?tenant_id=${TENANT_ID}" "200,403" '.' "department-performance"
  else
    pass "department-detail/performance skipped (empty list)"
  fi

  local suffix
  suffix="$(date +%s)"
  local create_payload
  create_payload="$(jq -cn --argjson tenant_id "$TENANT_ID" --arg code "REG_DEPT_${suffix}" --arg name "REG Department ${suffix}" '{tenant_id:$tenant_id,code:$code,name:$name}')"

  local create_json create_code
  create_json="$(mktemp)"
  create_code="$(curl -sS -o "$create_json" -w "%{http_code}" -X POST "${BASE_URL}/api/v1/departments" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$create_payload")"
  if [[ "$create_code" == "200" ]]; then
    pass "department-create (200)"
    local created_id
    created_id="$(jq -r '.id // empty' "$create_json")"
    if [[ -n "$created_id" ]]; then
      request_any PUT "/departments/${created_id}" "200" '.id > 0' "department-update" '{"name":"REG Department Updated"}'
      request_any DELETE "/departments/${created_id}" "200" '.message|type=="string"' "department-delete"
    else
      fail "department-create-id-missing"
    fi
  elif [[ "$create_code" == "403" ]]; then
    pass "department-create forbidden for non-admin (403)"
  else
    fail "department-create unexpected status ${create_code}"
    cat "$create_json" >&2
  fi
  rm -f "$create_json"

  request_any POST "/departments/actions/sync-from-visits" "200,403" '.' "departments-sync-from-visits" "{\"tenant_id\":${TENANT_ID}}"

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"
  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
