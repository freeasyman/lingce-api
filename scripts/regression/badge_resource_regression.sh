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
  command -v curl >/dev/null 2>&1 || {
    log "curl is required"
    exit 1
  }
  command -v jq >/dev/null 2>&1 || {
    log "jq is required"
    exit 1
  }
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

  if [[ -n "${body}" ]]; then
    code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"
  else
    code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"
  fi

  if [[ "$code" != "$expected_http" ]]; then
    fail "${name} (http=${code}, expected=${expected_http})"
    cat "$body_file" >&2
    echo >&2
    rm -f "$body_file"
    return 1
  fi

  if [[ -n "$check_expr" ]]; then
    if ! jq -e "$check_expr" "$body_file" >/dev/null 2>&1; then
      fail "${name} (body assertion failed: ${check_expr})"
      cat "$body_file" >&2
      echo >&2
      rm -f "$body_file"
      return 1
    fi
  fi

  pass "$name"
  cat "$body_file"
  rm -f "$body_file"
}

ensure_token() {
  if [[ -n "${TOKEN}" ]]; then
    return
  fi
  log "TOKEN is required"
  log "Example: TOKEN=xxxxx BASE_URL=http://127.0.0.1:18080 bash scripts/regression/badge_resource_regression.sh"
  exit 1
}

main() {
  require_tools
  ensure_token

  local suffix
  suffix="$(date +%s)"

  log "== badge resource regression =="

  local manufacturers_json
  manufacturers_json="$(request GET "/badge-devices/manufacturers" 200 '(.items|type=="array")' "manufacturers-list")"

  local manufacturer_code manufacturer_name
  manufacturer_code="$(echo "$manufacturers_json" | jq -r '.items[0].code // empty')"
  manufacturer_name="$(echo "$manufacturers_json" | jq -r '.items[0].name // empty')"
  if [[ -z "$manufacturer_code" ]]; then
    log "No manufacturer available, abort"
    exit 1
  fi
  if [[ -z "$manufacturer_name" ]]; then
    manufacturer_name="$manufacturer_code"
  fi

  local device_no_1="REG-${manufacturer_code}-${suffix}-001"
  local device_no_2="REG-${manufacturer_code}-${suffix}-002"

  local import_payload
  import_payload="$(jq -cn --arg c "$manufacturer_code" --arg n "$manufacturer_name" --arg d1 "$device_no_1" --arg d2 "$device_no_2" '{manufacturer_code:$c, manufacturer_name:$n, devices:[{device_no:$d1, hardware_model:"R1"},{device_no:$d2, hardware_model:"R1"}]}')"
  local import_json
  import_json="$(request POST "/badge-devices/actions/import" 200 '.success >= 1' "devices-import" "$import_payload")"

  local list_json
  list_json="$(request GET "/badge-devices?page=1&page_size=20&device_no=REG-${manufacturer_code}-${suffix}" 200 '(.items|type=="array") and ((.items|length)>=1)' "devices-list-by-prefix")"

  local device_id_1 device_id_2
  device_id_1="$(echo "$list_json" | jq -r '.items[0].id')"
  device_id_2="$(echo "$list_json" | jq -r '.items[1].id // .items[0].id')"

  request GET "/badge-devices/${device_id_1}" 200 '(.device.id > 0) and (.logs|type=="array")' "device-detail"

  request PATCH "/badge-devices/${device_id_1}" 200 '.message=="ok"' "device-patch" '{"metadata":{"regression":"badge_resource_regression"}}'

  local accept_payload
  accept_payload="$(jq -cn --argjson id1 "$device_id_1" --argjson id2 "$device_id_2" '{device_ids:[$id1,$id2],skip_health_check:true}')"
  request POST "/badge-devices/actions/batch-accept" 200 '.success >= 1' "devices-batch-accept" "$accept_payload"

  local assign_payload
  assign_payload="$(jq -cn --argjson id1 "$device_id_1" --argjson id2 "$device_id_2" --argjson tid "$TENANT_ID" --argjson eid "$EMPLOYEE_ID" '{device_ids:[$id1,$id2],tenant_id:$tid,tenant_name:"回归租户",employee_id:$eid,employee_name:"回归员工"}')"
  request POST "/badge-devices/actions/batch-assign" 200 '.success >= 1' "devices-batch-assign" "$assign_payload"

  request GET "/badge-devices/dashboard" 200 '(.total >= 0) and (.by_status|type=="object")' "devices-dashboard"

  local reclaim_payload
  reclaim_payload="$(jq -cn --argjson id1 "$device_id_1" --argjson id2 "$device_id_2" '{device_ids:[$id1,$id2],reason:"regression"}')"
  request POST "/badge-devices/actions/batch-reclaim" 200 '.success >= 1' "devices-batch-reclaim" "$reclaim_payload"

  request GET "/badge-devices/${device_id_1}/logs?page=1&page_size=20" 200 '(.items|type=="array")' "device-logs"

  local ticket_payload
  ticket_payload="$(jq -cn --argjson did "$device_id_1" --arg dno "$device_no_1" --arg s "$suffix" '{type:"maintenance",device_id:$did,device_no:$dno,title:("REG Ticket " + $s),description:"badge resource regression"}')"
  local ticket_json
  ticket_json="$(request POST "/badge-tickets" 200 '.id > 0 and .status=="pending"' "ticket-create" "$ticket_payload")"

  local ticket_id
  ticket_id="$(echo "$ticket_json" | jq -r '.id')"

  request GET "/badge-tickets?page=1&page_size=20" 200 '(.items|type=="array")' "ticket-list"
  request GET "/badge-tickets/my?page=1&page_size=20" 200 '(.items|type=="array") and (.total >= 1)' "ticket-my-list"
  request GET "/badge-tickets/${ticket_id}" 200 '.id > 0 and .status=="pending"' "ticket-detail"

  request POST "/badge-tickets/${ticket_id}/actions/review" 403 '.code=="FORBIDDEN"' "ticket-review-forbidden" '{"approved":true,"notes":"regression review"}'
  request POST "/badge-tickets/${ticket_id}/actions/execute" 403 '.code=="FORBIDDEN"' "ticket-execute-forbidden" '{"notes":"regression execute"}'
  request GET "/badge-tickets/${ticket_id}" 200 '.id > 0 and .status=="pending"' "ticket-detail-still-pending"

  local by_device_payload
  by_device_payload="$(jq -cn --arg dno "$device_no_1" --arg s "$suffix" '{type:"exception",device_no:$dno,title:("REG ByDevice " + $s),description:"ticket by-device regression"}')"
  request POST "/badge-tickets/by-device" 200 '.id > 0' "ticket-create-by-device" "$by_device_payload"

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"

  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
