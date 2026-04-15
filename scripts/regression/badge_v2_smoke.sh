#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"

if [[ -z "${TOKEN}" ]]; then
  echo "TOKEN is required. Example:"
  echo "  TOKEN=xxxxx BASE_URL=http://127.0.0.1:18080 bash scripts/regression/badge_v2_smoke.sh"
  exit 1
fi

auth_header=(-H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json")
SUFFIX="$(date +%s)"

echo "[0] resolve manufacturer"
manufacturers_resp="$(curl -sS "${auth_header[@]}" "${BASE_URL}/api/v1/badge-devices/manufacturers")"
manufacturer_code="$(echo "${manufacturers_resp}" | jq -r '.items[0].code // empty')"
manufacturer_name="$(echo "${manufacturers_resp}" | jq -r '.items[0].name // empty')"
if [[ -z "${manufacturer_code}" ]]; then
  echo "No available manufacturers. Response: ${manufacturers_resp}"
  exit 1
fi
if [[ -z "${manufacturer_name}" ]]; then
  manufacturer_name="${manufacturer_code}"
fi

device_no_1="SMOKE-${manufacturer_code}-${SUFFIX}-001"
device_no_2="SMOKE-${manufacturer_code}-${SUFFIX}-002"

echo "[1] import devices"
import_resp="$(curl -sS "${auth_header[@]}" \
  -X POST "${BASE_URL}/api/v1/badge-devices/actions/import" \
  -d "{
    \"manufacturer_code\":\"${manufacturer_code}\",
    \"manufacturer_name\":\"${manufacturer_name}\",
    \"devices\":[
      {\"device_no\":\"${device_no_1}\",\"hardware_model\":\"X1\"},
      {\"device_no\":\"${device_no_2}\",\"hardware_model\":\"X1\"}
    ]
  }")"
echo "${import_resp}" | head -c 300; echo

echo "[2] list devices"
list_resp="$(curl -sS "${auth_header[@]}" "${BASE_URL}/api/v1/badge-devices?page=1&page_size=20&device_no=SMOKE-${manufacturer_code}-${SUFFIX}")"
echo "${list_resp}" | head -c 300; echo

device_ids="$(echo "${list_resp}" | jq -r '.items[]?.id' | tr '\n' ',' | sed 's/,$//')"
if [[ -z "${device_ids}" ]]; then
  echo "No device ids found from list result"
  exit 1
fi

echo "[3] batch accept"
accept_resp="$(curl -sS "${auth_header[@]}" \
  -X POST "${BASE_URL}/api/v1/badge-devices/actions/batch-accept" \
  -d "{\"device_ids\":[${device_ids}],\"skip_health_check\":true}")"
echo "${accept_resp}" | head -c 300; echo

echo "[4] batch assign"
assign_resp="$(curl -sS "${auth_header[@]}" \
  -X POST "${BASE_URL}/api/v1/badge-devices/actions/batch-assign" \
  -d "{
    \"device_ids\":[${device_ids}],
    \"tenant_id\":1,
    \"tenant_name\":\"测试租户\",
    \"employee_id\":1,
    \"employee_name\":\"测试员工\"
  }")"
echo "${assign_resp}" | head -c 300; echo

echo "[5] dashboard"
dashboard_resp="$(curl -sS "${auth_header[@]}" "${BASE_URL}/api/v1/badge-devices/dashboard")"
echo "${dashboard_resp}" | head -c 300; echo

echo "[6] batch reclaim"
reclaim_resp="$(curl -sS "${auth_header[@]}" \
  -X POST "${BASE_URL}/api/v1/badge-devices/actions/batch-reclaim" \
  -d "{\"device_ids\":[${device_ids}],\"reason\":\"smoke test\"}")"
echo "${reclaim_resp}" | head -c 300; echo

echo "[7] export csv"
curl -sS "${auth_header[@]}" "${BASE_URL}/api/v1/badge-devices/export?status=returned" | head -c 200; echo

echo "badge_v2_smoke done"
