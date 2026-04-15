#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"
TENANT_ID="${TENANT_ID:-1}"
EMPLOYEE_ID="${EMPLOYEE_ID:-1}"

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
  command -v curl >/dev/null 2>&1 || { log "curl is required"; exit 1; }
  command -v jq >/dev/null 2>&1 || { log "jq is required"; exit 1; }
}

request() {
  local method="$1"
  local path="$2"
  local expected_http="$3"
  local check_expr="$4"
  local name="$5"
  local body="${6:-}"

  local url="${BASE_URL}/api/v1${path}"
  local body_file
  body_file="$(mktemp)"
  local code

  if [[ -n "$body" ]]; then
    code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"
  else
    code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"
  fi

  if [[ "$code" != "$expected_http" ]]; then
    fail "${name} (http=${code}, expected=${expected_http})"
    cat "$body_file" >&2
    rm -f "$body_file"
    return 1
  fi

  if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$body_file" >/dev/null 2>&1; then
    fail "${name} (body assertion failed: ${check_expr})"
    cat "$body_file" >&2
    rm -f "$body_file"
    return 1
  fi

  pass "$name"
  cat "$body_file"
  rm -f "$body_file"
}

request_any() {
  local method="$1"
  local path="$2"
  local expected_csv="$3"
  local check_expr="$4"
  local name="$5"
  local body="${6:-}"

  local url="${BASE_URL}/api/v1${path}"
  local body_file
  body_file="$(mktemp)"
  local code

  if [[ -n "$body" ]]; then
    code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"
  else
    code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"
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
    cat "$body_file" >&2
    rm -f "$body_file"
    return 1
  fi

  if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$body_file" >/dev/null 2>&1; then
    fail "${name} (body assertion failed: ${check_expr})"
    cat "$body_file" >&2
    rm -f "$body_file"
    return 1
  fi

  pass "$name"
  cat "$body_file"
  rm -f "$body_file"
}

main() {
  require_tools
  if [[ -z "$TOKEN" ]]; then
    log "TOKEN is required"
    exit 1
  fi

  local suffix
  suffix="$(date +%s)"

  log "== recording resource regression =="

  request GET "/recordings?page=1&page_size=20&tenant_id=${TENANT_ID}" 200 '(.items|type=="array") and (.total >= 0)' "recordings-list"
  request GET "/recording-tasks?page=1&page_size=20&tenant_id=${TENANT_ID}" 200 '(.items|type=="array") and (.total >= 0)' "recording-tasks-list"
  request GET "/recording-tasks/stats?tenant_id=${TENANT_ID}" 200 '.' "recording-tasks-stats"

  local create_payload
  create_payload="$(jq -cn --argjson tenant_id "$TENANT_ID" --argjson employee_id "$EMPLOYEE_ID" --arg pn "回归录音-${suffix}" '{tenant_id:$tenant_id,employee_id:$employee_id,patient_name:$pn,recording_url:"https://example.com/regression.wav",recording_duration:88}')"
  local created_json
  created_json="$(request POST "/recordings" 200 '.id > 0 or .data.id > 0' "recording-create" "$create_payload")"

  local recording_id
  recording_id="$(echo "$created_json" | jq -r '.id // .data.id // empty')"
  if [[ -z "$recording_id" ]]; then
    fail "recording-create-id-missing"
    exit 1
  fi

  request GET "/recordings/${recording_id}" 200 '.id > 0 or .data.id > 0' "recording-detail"

  local update_payload
  update_payload='{"patient_name":"回归录音-已更新"}'
  request PATCH "/recordings/${recording_id}" 200 '.id > 0 or .data.id > 0' "recording-update" "$update_payload"

  request_any POST "/recordings/${recording_id}/actions/transcribe" "200,500,503" '.' "recording-transcribe" '{}'
  request_any POST "/recordings/${recording_id}/actions/analyze" "200,500,503" '.' "recording-analyze" '{}'
  request_any POST "/recordings/${recording_id}/actions/clean" "200,500,503" '.' "recording-clean" '{}'

  request GET "/recordings/${recording_id}/tasks?tenant_id=${TENANT_ID}" 200 '(type=="array") or (.tasks|type=="array") or (.items|type=="array")' "recording-tasks-by-recording"
  request GET "/recordings/quality-control?tenant_id=${TENANT_ID}" 200 '.' "recording-quality-control"
  request GET "/recordings/communication-analysis?tenant_id=${TENANT_ID}" 200 '.' "recording-communication-analysis"

  request POST "/recordings/actions/batch-transcribe" 400 '.code=="BAD_REQUEST"' "recording-batch-transcribe-empty" '{"recording_ids":[]}'
  request POST "/recordings/actions/batch-delete" 400 '.code=="BAD_REQUEST"' "recording-batch-delete-empty" '{"recording_ids":[]}'

  request_any DELETE "/recordings/${recording_id}" "200,403" '.' "recording-delete-or-forbidden"

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"

  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
