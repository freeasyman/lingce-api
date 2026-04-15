#!/usr/bin/env bash
set -euo pipefail

FRONTEND_URL="${FRONTEND_URL:-http://127.0.0.1:3004}"
API_BASE_URL="${API_BASE_URL:-http://127.0.0.1:18080}"
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

require_tools() {
  command -v curl >/dev/null 2>&1 || {
    log "curl is required"
    exit 1
  }
  command -v rg >/dev/null 2>&1 || {
    log "rg is required"
    exit 1
  }
}

check_page() {
  local path="$1"
  local code
  local body_file
  body_file="$(mktemp)"
  code="$(curl -sS -o "$body_file" -w "%{http_code}" "${FRONTEND_URL}${path}")"
  if [[ "$code" != "200" ]]; then
    fail "page ${path} http=${code}"
    cat "$body_file" >&2
    rm -f "$body_file"
    return 1
  fi
  if ! rg -q "<div id=\"root\">" "$body_file"; then
    fail "page ${path} missing root mount"
    cat "$body_file" >&2
    rm -f "$body_file"
    return 1
  fi
  pass "page ${path}"
  rm -f "$body_file"
}

check_runtime_module() {
  local body
  body="$(curl -sS "${FRONTEND_URL}/src/features/badges/api/badge-api.ts")"

  if ! printf '%s' "$body" | rg -q 'baseUrl: "?/api/v1"?'; then
    fail "badge-api runtime module missing /api/v1 baseUrl"
    return 1
  fi
  if ! printf '%s' "$body" | rg -q 'API_BASE = "?/badge-devices"?'; then
    fail "badge-api runtime module missing /badge-devices API_BASE"
    return 1
  fi
  if printf '%s' "$body" | rg -q '/api/v2/badges'; then
    fail "badge-api runtime module still contains /api/v2/badges"
    return 1
  fi
  pass "badge-api runtime module route mapping"
}

check_proxy_api() {
  if [[ -z "$TOKEN" ]]; then
    log "TOKEN not provided, skip proxy API checks"
    return
  fi

  local code1 code2 code3
  code1="$(curl -sS -o /tmp/fbadge1.json -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "${FRONTEND_URL}/api/v1/badge-devices?status=available&page=1&page_size=20")"
  code2="$(curl -sS -o /tmp/fbadge2.json -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "${FRONTEND_URL}/api/v1/badge-devices/manufacturers")"
  code3="$(curl -sS -o /tmp/fbadge3.json -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "${FRONTEND_URL}/api/v2/badges/devices?status=available&page=1&page_size=20")"

  if [[ "$code1" == "200" ]]; then
    pass "proxy v1 badge-devices"
  else
    fail "proxy v1 badge-devices http=${code1}"
    cat /tmp/fbadge1.json >&2
  fi

  if [[ "$code2" == "200" ]]; then
    pass "proxy v1 manufacturers"
  else
    fail "proxy v1 manufacturers http=${code2}"
    cat /tmp/fbadge2.json >&2
  fi

  if [[ "$code3" == "404" ]]; then
    pass "proxy v2 legacy returns 404"
  else
    fail "proxy v2 legacy expected 404 got ${code3}"
    cat /tmp/fbadge3.json >&2
  fi
}

main() {
  require_tools

  log "== frontend badge page smoke =="

  check_page "/badges-v2/dashboard"
  check_page "/badges-v2/inventory"
  check_page "/badges-v2/assignment"
  check_page "/badges-v2/monitoring"

  check_runtime_module
  check_proxy_api

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"

  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
