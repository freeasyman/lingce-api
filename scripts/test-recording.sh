#!/usr/bin/env bash
set -euo pipefail

API_URL="${API_URL:-http://localhost:18080}"
LOGIN_USERNAME="${LOGIN_USERNAME:-admin}"
PASSWORD="${PASSWORD:-}"
TENANT_ID="${TENANT_ID:-}"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

log() {
  printf '[recording-test] %s\n' "$*"
}

fail() {
  printf '[recording-test][FAIL] %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"
}

extract_json_field() {
  local file="$1"
  local expr="$2"
  python3 - "$file" "$expr" <<'PY'
import json, sys
path = sys.argv[2].split(".")
with open(sys.argv[1], "r", encoding="utf-8") as f:
    data = json.load(f)
cur = data
for p in path:
    if isinstance(cur, dict):
        cur = cur.get(p)
    elif isinstance(cur, list):
        try:
            idx = int(p)
            cur = cur[idx]
        except Exception:
            cur = None
            break
    else:
        cur = None
        break
if cur is None:
    print("")
elif isinstance(cur, (dict, list)):
    print(json.dumps(cur, ensure_ascii=False))
else:
    print(cur)
PY
}

request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local auth="${4:-yes}"
  local out="$TMP_DIR/resp.json"
  local status
  local curl_args=(-sS -o "$out" -w "%{http_code}" -X "$method" "$API_URL$path")
  if [[ "$auth" == "yes" ]]; then
    curl_args+=(-H "Authorization: Bearer $TOKEN")
  fi
  if [[ -n "$body" ]]; then
    curl_args+=(-H "Content-Type: application/json" -d "$body")
  fi
  status="$(curl "${curl_args[@]}")"
  printf '%s|%s\n' "$status" "$out"
}

assert_status() {
  local expected="$1"
  local actual="$2"
  local label="$3"
  if [[ "$actual" != "$expected" ]]; then
    fail "$label expected status $expected, got $actual"
  fi
  log "$label status=$actual"
}

assert_any_status() {
  local actual="$1"
  local label="$2"
  shift 2
  for expected in "$@"; do
    if [[ "$actual" == "$expected" ]]; then
      log "$label status=$actual"
      return 0
    fi
  done
  fail "$label expected one of [$*], got $actual"
}

require_cmd curl
require_cmd python3

log "health check"
health_status="$(curl -sS -o "$TMP_DIR/health.json" -w "%{http_code}" "$API_URL/healthz")"
assert_status "200" "$health_status" "GET /healthz"

if [[ -z "$PASSWORD" ]]; then
  fail "PASSWORD is required, run with PASSWORD=xxx LOGIN_USERNAME=admin scripts/test-recording.sh"
fi

log "login as admin"
login="$(request POST "/api/v1/auth/login" "{\"username\":\"$LOGIN_USERNAME\",\"password\":\"$PASSWORD\"}" "no")"
login_status="${login%%|*}"
login_file="${login##*|}"
assert_status "200" "$login_status" "POST /api/v1/auth/login"
TOKEN="$(extract_json_field "$login_file" "data.token")"
if [[ -z "$TOKEN" ]]; then
  TOKEN="$(extract_json_field "$login_file" "token")"
fi
[[ -n "$TOKEN" ]] || fail "token not found in login response"

if [[ -z "$TENANT_ID" ]]; then
  log "resolve tenant_id from /api/v1/tenants"
  tenants="$(request GET "/api/v1/tenants?page=1&page_size=1&sort=created_at" "")"
  tenants_status="${tenants%%|*}"
  tenants_file="${tenants##*|}"
  assert_status "200" "$tenants_status" "GET /api/v1/tenants"
  TENANT_ID="$(extract_json_field "$tenants_file" "items.0.id")"
fi
[[ -n "$TENANT_ID" ]] || fail "tenant_id is required (set TENANT_ID or ensure tenant exists)"
log "tenant_id=$TENANT_ID"

log "negative: unauthorized access"
noauth_status="$(curl -sS -o "$TMP_DIR/noauth.json" -w "%{http_code}" "$API_URL/api/v1/recordings")"
assert_status "401" "$noauth_status" "GET /api/v1/recordings without token"

log "negative: admin missing tenant_id"
no_tenant="$(request GET "/api/v1/recordings?page=1&page_size=5" "")"
no_tenant_status="${no_tenant%%|*}"
assert_status "400" "$no_tenant_status" "GET /api/v1/recordings admin no tenant_id"

log "create recording"
create_body="$(cat <<JSON
{
  "tenant_id": $TENANT_ID,
  "employee_id": 1,
  "patient_name": "回归测试用户",
  "recording_url": "https://example.com/recording-test.wav",
  "recording_duration": 120
}
JSON
)"
create_resp="$(request POST "/api/v1/recordings" "$create_body")"
create_status="${create_resp%%|*}"
create_file="${create_resp##*|}"
assert_status "200" "$create_status" "POST /api/v1/recordings"
RECORDING_ID="$(extract_json_field "$create_file" "data.id")"
if [[ -z "$RECORDING_ID" ]]; then
  RECORDING_ID="$(extract_json_field "$create_file" "id")"
fi
[[ -n "$RECORDING_ID" ]] || fail "recording id not found"
log "recording_id=$RECORDING_ID"

log "query recording detail"
detail_resp="$(request GET "/api/v1/recordings/$RECORDING_ID" "")"
detail_status="${detail_resp%%|*}"
assert_status "200" "$detail_status" "GET /api/v1/recordings/{id}"

log "recording tasks and dashboards"
resp="$(request GET "/api/v1/recording-tasks/stats?tenant_id=$TENANT_ID" "")"
assert_status "200" "${resp%%|*}" "GET /api/v1/recording-tasks/stats"
resp="$(request GET "/api/v1/recording-tasks/daily-briefing?tenant_id=$TENANT_ID" "")"
assert_status "200" "${resp%%|*}" "GET /api/v1/recording-tasks/daily-briefing"
resp="$(request GET "/api/v1/recordings/$RECORDING_ID/tasks?tenant_id=$TENANT_ID" "")"
assert_status "200" "${resp%%|*}" "GET /api/v1/recordings/{id}/tasks"

log "medical dashboards"
resp="$(request GET "/api/v1/medical-recordings?page=1&page_size=5&tenant_id=$TENANT_ID" "")"
assert_status "200" "${resp%%|*}" "GET /api/v1/medical-recordings"
resp="$(request GET "/api/v1/medical-recordings/quality-control?tenant_id=$TENANT_ID" "")"
assert_status "200" "${resp%%|*}" "GET /api/v1/medical-recordings/quality-control"
resp="$(request GET "/api/v1/medical-recordings/analysis?tenant_id=$TENANT_ID" "")"
assert_status "200" "${resp%%|*}" "GET /api/v1/medical-recordings/analysis"

log "advanced actions"
resp="$(request POST "/api/v1/recordings/$RECORDING_ID/transcribe" "{}")"
transcribe_status="${resp%%|*}"
assert_any_status "$transcribe_status" "POST /api/v1/recordings/{id}/transcribe" "200" "500"
resp="$(request POST "/api/v1/recordings/$RECORDING_ID/analyze" "{}")"
analyze_status="${resp%%|*}"
assert_any_status "$analyze_status" "POST /api/v1/recordings/{id}/analyze" "200" "500"
resp="$(request POST "/api/v1/recordings/$RECORDING_ID/clean" "{}")"
clean_status="${resp%%|*}"
assert_any_status "$clean_status" "POST /api/v1/recordings/{id}/clean" "200" "500"
resp="$(request GET "/api/v1/recordings/$RECORDING_ID/analysis" "")"
assert_any_status "${resp%%|*}" "GET /api/v1/recordings/{id}/analysis" "200" "404"

log "negative: invalid recording id"
resp="$(request GET "/api/v1/recordings/abc?tenant_id=$TENANT_ID" "")"
invalid_id_status="${resp%%|*}"
assert_status "400" "$invalid_id_status" "GET /api/v1/recordings/abc"

log "negative: bad batch payload"
resp="$(request POST "/api/v1/recordings/batch-delete" "{\"recording_ids\":[]}")"
bad_batch_status="${resp%%|*}"
assert_status "400" "$bad_batch_status" "POST /api/v1/recordings/batch-delete with empty ids"

log "cleanup: delete created recording"
resp="$(request DELETE "/api/v1/recordings/$RECORDING_ID" "")"
delete_status="${resp%%|*}"
assert_status "200" "$delete_status" "DELETE /api/v1/recordings/{id}"

log "recording regression finished successfully"
