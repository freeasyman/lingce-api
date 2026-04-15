#!/usr/bin/env bash
set -euo pipefail
BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"; TOKEN="${TOKEN:-}"; TENANT_ID="${TENANT_ID:-1}"; PASS_COUNT=0; FAIL_COUNT=0
log(){ printf '%s\n' "$*" >&2; }; pass(){ PASS_COUNT=$((PASS_COUNT+1)); log "PASS: $*"; }; fail(){ FAIL_COUNT=$((FAIL_COUNT+1)); log "FAIL: $*"; }
request_any(){ local method="$1" path="$2" expected_csv="$3" check_expr="$4" name="$5" body="${6:-}"; local out code url="${BASE_URL}/api/v1${path}"; out="$(mktemp)"; if [[ -n "$body" ]]; then code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"; else code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"; fi; local m=0 e; IFS=',' read -r -a arr <<< "$expected_csv"; for e in "${arr[@]}"; do [[ "$code" == "$e" ]] && m=1 && break; done; [[ $m -eq 1 ]] || { fail "${name} (http=${code}, expected one of ${expected_csv})"; cat "$out" >&2; rm -f "$out"; return 1; }; if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$out" >/dev/null 2>&1; then fail "${name} assertion"; cat "$out" >&2; rm -f "$out"; return 1; fi; pass "${name} (${code})"; cat "$out"; rm -f "$out"; }
[[ -n "$TOKEN" ]] || { log "TOKEN is required"; exit 1; }
log "== employee resource regression =="
local_json="$(request_any GET "/employees?tenant_id=${TENANT_ID}&page=1&page_size=20" "200" '(.items|type=="array") and (.total>=0)' "employees-list")"
emp_id="$(echo "$local_json" | jq -r '.items[0].id // empty')"
if [[ -n "$emp_id" ]]; then request_any GET "/employees/${emp_id}" "200" '.id>0' "employee-detail"; request_any POST "/employees/${emp_id}/actions/reset-password" "403" '.code=="FORBIDDEN"' "employee-reset-password-forbidden" '{"new_password":"123456"}'; else pass "employee-detail/reset-password skipped"; fi
request_any POST "/employees" "403" '.code=="FORBIDDEN"' "employee-create-forbidden" '{"tenant_id":1,"username":"x","full_name":"x","phone":"13900000001","password":"123456"}'
log ""; log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"; [[ "$FAIL_COUNT" -eq 0 ]]
