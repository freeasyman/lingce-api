#!/usr/bin/env bash
set -euo pipefail

FRONTEND_URL="${FRONTEND_URL:-http://127.0.0.1:3004}"
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
  if ! rg -q '<div id="root">' "$body_file"; then
    fail "page ${path} missing root mount"
    cat "$body_file" >&2
    rm -f "$body_file"
    return 1
  fi
  pass "page ${path}"
  rm -f "$body_file"
}

check_proxy() {
  local path="$1"
  local expected_http="$2"
  local name="$3"

  if [[ -z "$TOKEN" ]]; then
    fail "${name} skipped: TOKEN missing"
    return 1
  fi

  local out
  out="$(mktemp)"
  local code
  code="$(curl -sS -o "$out" -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "${FRONTEND_URL}${path}")"
  if [[ "$code" == "$expected_http" ]]; then
    pass "${name} (${code})"
  else
    fail "${name} http=${code} expected=${expected_http}"
    cat "$out" >&2
  fi
  rm -f "$out"
}

check_runtime_endpoints() {
  local content_mod customer_mod recording_mod
  content_mod="$(curl -sS "${FRONTEND_URL}/src/api/content.ts")"
  customer_mod="$(curl -sS "${FRONTEND_URL}/src/api/customers.ts")"
  recording_mod="$(curl -sS "${FRONTEND_URL}/src/api/doctor-recordings.ts")"

  if printf '%s' "$content_mod" | rg -q '/api/v1/content-prompt-templates'; then
    fail "runtime content.ts still contains legacy content-prompt-templates"
  else
    pass "runtime content.ts no legacy prompt route"
  fi

  if printf '%s' "$customer_mod" | rg -q '/customer-tags|/customer-groups'; then
    fail "runtime customers.ts still contains legacy customer-tags/groups"
  else
    pass "runtime customers.ts uses resource routes"
  fi

  if printf '%s' "$recording_mod" | rg -q '/medical-recordings'; then
    fail "runtime doctor-recordings.ts still contains legacy medical-recordings"
  else
    pass "runtime doctor-recordings.ts uses /recordings"
  fi
}

main() {
  command -v curl >/dev/null 2>&1 || { log "curl is required"; exit 1; }
  command -v rg >/dev/null 2>&1 || { log "rg is required"; exit 1; }

  log "== frontend ops page smoke =="

  check_page "/customers"
  check_page "/customers/segments"
  check_page "/content/article/generate"
  check_page "/content/topics"
  check_page "/doctor-recordings"
  check_page "/consultant-recordings"

  check_runtime_endpoints

  check_proxy "/api/v1/customers?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 "proxy customers list"
  check_proxy "/api/v1/customers/tags?tenant_id=${TENANT_ID}&limit=200" 200 "proxy customer tags"
  check_proxy "/api/v1/content-items/prompts?tenant_id=${TENANT_ID}&function_type=content_article&page=1&page_size=100" 200 "proxy content prompts"
  check_proxy "/api/v1/content-items?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 "proxy content items"
  check_proxy "/api/v1/recordings?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 "proxy recordings list"

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"
  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
