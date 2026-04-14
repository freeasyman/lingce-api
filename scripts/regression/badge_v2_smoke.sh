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

echo "[1] import devices"
import_resp="$(curl -sS "${auth_header[@]}" \
  -X POST "${BASE_URL}/api/v1/badge-devices/actions/import" \
  -d '{
    "manufacturer_code":"xiaomi",
    "manufacturer_name":"小米",
    "devices":[
      {"device_no":"SMOKE-XM-001","hardware_model":"X1"},
      {"device_no":"SMOKE-XM-002","hardware_model":"X1"}
    ]
  }')"
echo "${import_resp}" | head -c 300; echo

echo "[2] list devices"
list_resp="$(curl -sS "${auth_header[@]}" "${BASE_URL}/api/v1/badge-devices?page=1&page_size=20&device_no=SMOKE-XM")"
echo "${list_resp}" | head -c 300; echo

device_ids="$(echo "${list_resp}" | jq -r '.items[].id' | tr '\n' ',' | sed 's/,$//')"
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
