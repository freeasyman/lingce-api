#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"
TENANT_ID="${TENANT_ID:-1}"
PHONE="${PHONE:-13900139000}"
PASSWORD="${PASSWORD:-123456}"
PASS_COUNT=0
FAIL_COUNT=0
log(){ printf '%s\n' "$*" >&2; }
pass(){ PASS_COUNT=$((PASS_COUNT+1)); log "PASS: $*"; }
fail(){ FAIL_COUNT=$((FAIL_COUNT+1)); log "FAIL: $*"; }
request_any(){
  local method="$1" path="$2" expected_csv="$3" check_expr="$4" name="$5" body="${6:-}" auth_mode="${7:-valid}"
  local url="${BASE_URL}/api/v1${path}" out code; out="$(mktemp)"
  if [[ "$auth_mode" == "none" ]]; then
    if [[ -n "$body" ]]; then code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Content-Type: application/json" -d "$body")"; else code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url")"; fi
  else
    if [[ -n "$body" ]]; then code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"; else code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"; fi
  fi
  local matched=0 expected; IFS=',' read -r -a arr <<< "$expected_csv"; for expected in "${arr[@]}"; do [[ "$code" == "$expected" ]] && matched=1 && break; done
  if [[ $matched -ne 1 ]]; then fail "${name} (http=${code}, expected one of ${expected_csv})"; cat "$out" >&2; rm -f "$out"; return 1; fi
  if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$out" >/dev/null 2>&1; then fail "${name} (body assertion failed: ${check_expr})"; cat "$out" >&2; rm -f "$out"; return 1; fi
  pass "${name} (${code})"; rm -f "$out"
}

command -v curl >/dev/null 2>&1 || exit 1
command -v jq >/dev/null 2>&1 || exit 1

log "== auth resource regression =="
request_any GET "/auth/captcha" "200" '.' "auth-captcha" '' none
request_any POST "/auth/login/institution" "200" '.access_token|type=="string"' "auth-login-institution" "{\"phone\":\"${PHONE}\",\"password\":\"${PASSWORD}\",\"tenant_id\":${TENANT_ID}}" none

if [[ -z "$TOKEN" ]]; then
  TOKEN="$(curl -sS -X POST "${BASE_URL}/api/v1/auth/login/institution" -H 'Content-Type: application/json' -d "{\"phone\":\"${PHONE}\",\"password\":\"${PASSWORD}\",\"tenant_id\":${TENANT_ID}}" | jq -r '.access_token // .token // empty')"
fi
[[ -n "$TOKEN" ]] || { log "TOKEN is required"; exit 1; }

request_any GET "/auth/me" "200" '.user_type|type=="string"' "auth-me"
request_any POST "/auth/change-password" "400" '.code=="BAD_REQUEST"' "auth-change-password-invalid" '{"old_password":"","new_password":""}'
request_any POST "/auth/sms/send" "400,500" '.' "auth-sms-send"
request_any POST "/auth/sms/login" "400,401" '.' "auth-sms-login-invalid" '{"phone":"13900000000","code":"000000"}'

log ""; log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"; [[ "$FAIL_COUNT" -eq 0 ]]
