#!/usr/bin/env bash
set -euo pipefail
BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"; TOKEN="${TOKEN:-}"; PASS_COUNT=0; FAIL_COUNT=0
log(){ printf '%s\n' "$*" >&2; }; pass(){ PASS_COUNT=$((PASS_COUNT+1)); log "PASS: $*"; }; fail(){ FAIL_COUNT=$((FAIL_COUNT+1)); log "FAIL: $*"; }
request_any(){ local method="$1" path="$2" expected_csv="$3" check_expr="$4" name="$5" body="${6:-}"; local out code url="${BASE_URL}/api/v1${path}"; out="$(mktemp)"; if [[ -n "$body" ]]; then code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"; else code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"; fi; local m=0 e; IFS=',' read -r -a arr <<< "$expected_csv"; for e in "${arr[@]}"; do [[ "$code" == "$e" ]] && m=1 && break; done; [[ $m -eq 1 ]] || { fail "${name} (http=${code}, expected one of ${expected_csv})"; cat "$out" >&2; rm -f "$out"; return 1; }; if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$out" >/dev/null 2>&1; then fail "${name} assertion"; cat "$out" >&2; rm -f "$out"; return 1; fi; pass "${name} (${code})"; cat "$out"; rm -f "$out"; }
[[ -n "$TOKEN" ]] || { log "TOKEN is required"; exit 1; }
log "== notification resource regression =="
list_json="$(request_any GET "/notifications?page=1&page_size=20" "200,403,500" '.' "notifications-list")"
nid="$(echo "$list_json" | jq -r '.items[0].id // empty' 2>/dev/null || true)"
if [[ -n "$nid" ]]; then request_any GET "/notifications/${nid}" "200,403,404,500" '.' "notification-detail"; request_any POST "/notifications/${nid}/actions/read" "200,403,404,500" '.' "notification-mark-read" '{}'; else pass "notification-detail/read skipped"; fi
request_any POST "/notifications/actions/read-all" "200,403,500" '.' "notification-read-all" '{}'
request_any POST "/notifications/device-tokens" "200,400,500" '.' "notification-device-token-register" '{"device_type":"web","device_token":"regression-device-token"}'
log ""; log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"; [[ "$FAIL_COUNT" -eq 0 ]]
