#!/usr/bin/env bash
set -euo pipefail
BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"
TENANT_ID="${TENANT_ID:-1}"
PASS_COUNT=0; FAIL_COUNT=0
log(){ printf '%s\n' "$*" >&2; }
pass(){ PASS_COUNT=$((PASS_COUNT+1)); log "PASS: $*"; }
fail(){ FAIL_COUNT=$((FAIL_COUNT+1)); log "FAIL: $*"; }
request_any(){ local method="$1" path="$2" expected_csv="$3" check_expr="$4" name="$5" body="${6:-}"; local out code url="${BASE_URL}/api/v1${path}"; out="$(mktemp)"; if [[ -n "$body" ]]; then code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"; else code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"; fi; local matched=0 e; IFS=',' read -r -a arr <<< "$expected_csv"; for e in "${arr[@]}"; do [[ "$code" == "$e" ]] && matched=1 && break; done; [[ $matched -eq 1 ]] || { fail "${name} (http=${code}, expected one of ${expected_csv})"; cat "$out" >&2; rm -f "$out"; return 1; }; if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$out" >/dev/null 2>&1; then fail "${name} assertion"; cat "$out" >&2; rm -f "$out"; return 1; fi; pass "${name} (${code})"; cat "$out"; rm -f "$out"; }
[[ -n "$TOKEN" ]] || { log "TOKEN is required"; exit 1; }
log "== tenant resource regression =="
request_any GET "/tenants?page=1&page_size=20" "200,403" '.' "tenants-list"
request_any GET "/tenants/${TENANT_ID}" "200,403" '.' "tenant-detail"
request_any GET "/tenants/${TENANT_ID}/subscription" "200,403" '.' "tenant-subscription"
request_any GET "/tenants/${TENANT_ID}/subscription/events" "200,403" '.' "tenant-subscription-events"
request_any GET "/tenants/${TENANT_ID}/features" "200,403" '.' "tenant-features"
request_any GET "/tenants/${TENANT_ID}/feature-overrides" "200,403" '.' "tenant-feature-overrides"
request_any GET "/tenants/${TENANT_ID}/profile" "200,403" '.' "tenant-profile"
request_any GET "/tenants/${TENANT_ID}/statistics" "200,403,500" '.' "tenant-statistics"
request_any GET "/tenants/${TENANT_ID}/medical-specialties" "200,403,500" '.' "tenant-medical-specialties"
request_any GET "/tenants/${TENANT_ID}/validity-logs" "200,403,500" '.' "tenant-validity-logs"
request_any POST "/tenants" "403" '.code=="FORBIDDEN"' "tenant-create-forbidden" '{"name":"x"}'
request_any POST "/tenants/${TENANT_ID}/subscription/actions/renew" "403,400" '.' "tenant-subscription-renew"
log ""; log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"; [[ "$FAIL_COUNT" -eq 0 ]]
