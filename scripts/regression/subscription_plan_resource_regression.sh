#!/usr/bin/env bash
set -euo pipefail
BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"; TOKEN="${TOKEN:-}"; PASS_COUNT=0; FAIL_COUNT=0
log(){ printf '%s\n' "$*" >&2; }; pass(){ PASS_COUNT=$((PASS_COUNT+1)); log "PASS: $*"; }; fail(){ FAIL_COUNT=$((FAIL_COUNT+1)); log "FAIL: $*"; }
request_any(){ local method="$1" path="$2" expected_csv="$3" check_expr="$4" name="$5" body="${6:-}"; local out code url="${BASE_URL}/api/v1${path}"; out="$(mktemp)"; if [[ -n "$body" ]]; then code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"; else code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"; fi; local m=0 e; IFS=',' read -r -a arr <<< "$expected_csv"; for e in "${arr[@]}"; do [[ "$code" == "$e" ]] && m=1 && break; done; [[ $m -eq 1 ]] || { fail "${name} (http=${code}, expected one of ${expected_csv})"; cat "$out" >&2; rm -f "$out"; return 1; }; if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$out" >/dev/null 2>&1; then fail "${name} assertion"; cat "$out" >&2; rm -f "$out"; return 1; fi; pass "${name} (${code})"; cat "$out"; rm -f "$out"; }
[[ -n "$TOKEN" ]] || { log "TOKEN is required"; exit 1; }
log "== subscription-plan resource regression =="
request_any GET "/subscription-plans?page=1&page_size=20" "200,403" '.' "subscription-plans-list"
request_any POST "/subscription-plans" "403,400" '.' "subscription-plans-create-forbidden" '{"name":"REG Plan","code":"reg_plan","duration_days":30,"price":1}'
request_any PUT "/subscription-plans/1" "403,404,400" '.' "subscription-plans-update-forbidden" '{"name":"REG Plan Updated"}'
log ""; log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"; [[ "$FAIL_COUNT" -eq 0 ]]
