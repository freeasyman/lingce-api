#!/usr/bin/env bash
# HIS P1 全链路 E2E（真实 API + lingce_test 数据库 + 种子数据）。
# 前置：API 运行于 127.0.0.1:18081；/tmp/e2e-token.txt 含 employee JWT。
# 可重复运行（每次用新的日期/手机号）。
set -euo pipefail
BASE="http://127.0.0.1:18081"
TOKEN="$(cat /tmp/e2e-token.txt)"
H="Authorization: Bearer $TOKEN"
PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); echo "✅ $1"; }
bad() { FAIL=$((FAIL+1)); echo "❌ $1"; }
jqok() {
  if echo "$2" | jq -e "$3" >/dev/null 2>&1; then ok "$1"; else bad "$1"; fi
}
json() { curl -s -H "$H" "$BASE$1"; }
post() { curl -s -X POST -H "$H" -H 'Content-Type: application/json' ${2:+-d "$2"} "$BASE$1"; }

WORK_DATE="$(date -v+3d +%F 2>/dev/null || date -d '+3 days' +%F)"
PHONE="138$(date +%H%M%S)"
echo "工作日期=$WORK_DATE 手机号=$PHONE"

echo "== 1. 主数据（种子）=="
jqok "药品>=17" "$(json /api/v1/his/master/drugs)" 'length >= 17'
jqok "收费项目>=11" "$(json /api/v1/his/master/charge-items)" 'length >= 11'
jqok "打包产品>=3" "$(json /api/v1/his/master/bundles)" 'length >= 3'
jqok "打包价 68" "$(json /api/v1/his/master/bundles)" '.[0].price == 68'
jqok "病历模板>=4" "$(json /api/v1/his/workbench/templates)" 'length >= 4'

echo "== 2. 医生与排班 =="
DOCTORS="$(json /api/v1/his/master/doctors)"
DOCTOR_ID="$(echo "$DOCTORS" | jq -r '.[0].id // empty')"
if [ -z "$DOCTOR_ID" ]; then
  DOCTOR_ID="$(post /api/v1/his/master/doctors '{"employee_id":1,"title":"主治医师"}' | jq -r '.id')"
fi
jqok "医生可用" "$([ -n "$DOCTOR_ID" ] && echo true)" 'true'
SCHED="$(post /api/v1/his/schedules "{\"doctor_id\":$DOCTOR_ID,\"work_date\":\"$WORK_DATE\",\"period\":\"morning\",\"start_time\":\"09:00\",\"end_time\":\"09:30\",\"slot_interval_minutes\":15}")"
SCHED_ID="$(echo "$SCHED" | jq -r '.id // empty')"
jqok "排班+号源生成(2)" "$SCHED" '.total_slots == 2'

echo "== 3. 建档与挂号 =="
PATIENT="$(post /api/v1/his/patients/register "{\"name\":\"E2E测试患者\",\"phone\":\"$PHONE\",\"gender\":\"female\",\"age\":30,\"allergy\":\"无\"}")"
CUSTOMER_ID="$(echo "$PATIENT" | jq -r '.customer_id')"; MRN="$(echo "$PATIENT" | jq -r '.mrn')"
jqok "建档+病案号" "$PATIENT" '.mrn != null'
REG="$(post /api/v1/his/registrations "{\"customer_id\":$CUSTOMER_ID,\"schedule_id\":$SCHED_ID,\"reg_type\":\"walkin\",\"fee\":0}")"
REG_ID="$(echo "$REG" | jq -r '.id')"; ENC_ID="$(echo "$REG" | jq -r '.encounter_id')"
jqok "挂号+encounter 生产" "$REG" '.encounter_id != null'

echo "== 4. 接诊上下文 =="
CTX="$(json "/api/v1/his/workbench/context?encounter_id=$ENC_ID")"
jqok "上下文含病案号" "$CTX" ".customer.mrn == \"$MRN\""
jqok "上下文过敏史=无" "$CTX" '.customer.allergy == "无"'

echo "== 5. 处方 =="
RX="$(post /api/v1/his/prescriptions "{\"encounter_id\":$ENC_ID,\"diagnosis\":\"阴道炎\",\"items\":[{\"drug_id\":1,\"dose\":\"0.2g\",\"frequency\":\"bid\",\"quantity\":1}]}")"
RX_ID="$(echo "$RX" | jq -r '.id')"
jqok "处方草稿" "$RX" '.status == "draft"'
RX_ISSUED="$(post "/api/v1/his/prescriptions/$RX_ID/issue")"
jqok "处方开具(P号)" "$RX_ISSUED" '.prescription_no | startswith("P")'

echo "== 6. 打包收费 =="
ORDER="$(post /api/v1/his/orders "{\"encounter_id\":$ENC_ID,\"discount_total\":0,\"items\":[{\"item_type\":\"bundle\",\"ref_id\":1,\"quantity\":1},{\"item_type\":\"drug\",\"ref_id\":1,\"quantity\":1,\"prescription_id\":$RX_ID}]}")"
ORDER_ID="$(echo "$ORDER" | jq -r '.id')"; PAYABLE="$(echo "$ORDER" | jq -r '.payable_total')"
jqok "收费单(68+6.5=74.5)" "$ORDER" '.items_total == 74.5'
PAID="$(post "/api/v1/his/orders/$ORDER_ID/payments" "{\"method\":\"wechat\",\"amount\":$PAYABLE}")"
jqok "收款->paid" "$PAID" '.status == "paid"'
jqok "处方联动 charged" "$(json "/api/v1/his/prescriptions?encounter_id=$ENC_ID")" '.[0].status == "charged"'

echo "== 7. 药房发药 =="
post "/api/v1/his/pharmacy/stock/inbound" '{"drug_id":1,"batch_no":"B20261101","quantity":10}' > /dev/null
DISP="$(post "/api/v1/his/pharmacy/prescriptions/$RX_ID/dispense")"
jqok "发药->dispensed" "$DISP" '.status == "dispensed"'
jqok "库存扣减后>=9" "$(json "/api/v1/his/pharmacy/stock")" '.[0].quantity >= 9'

echo "== 8. 文书 =="
DOC_DOC="$(post /api/v1/his/documents/generate "{\"doc_type\":\"门诊处方\",\"ref_id\":$RX_ID}")"
DOC_ID="$(echo "$DOC_DOC" | jq -r '.id // empty')"
jqok "处方文书生成" "$DOC_DOC" '.id != null and .content_snapshot.prescription_no != null'
PRINTED="$(post "/api/v1/his/documents/$DOC_ID/print")"
jqok "打印标记" "$PRINTED" '.status == "printed"'

echo "== 9. 报表 =="
RPT="$(json "/api/v1/his/reports/summary?date_from=2026-11-01&date_to=2027-01-01")"
jqok "报表收入>=74.5" "$RPT" '.revenue >= 74.5'
jqok "报表门诊量>=1" "$RPT" '.visits >= 1'
jqok "支付方式含微信" "$RPT" '.payment_methods | map(.method) | index("wechat") != null'

echo "== 10. 开放接口 =="
CLIENT="$(post /api/v1/his/open-clients "{\"name\":\"E2E小程序$(date +%s)\",\"rate_limit\":60}")"
API_KEY="$(echo "$CLIENT" | jq -r '.api_key // empty')"
jqok "创建开放客户端" "$([ -n "$API_KEY" ] && echo true)" 'true'
jqok "开放排班(>=1条)" "$(curl -s -H "X-API-Key: $API_KEY" "$BASE/open/his/v1/schedules?date=$WORK_DATE")" 'length >= 1'
OPEN_APPT="$(curl -s -X POST -H "X-API-Key: $API_KEY" -H 'Content-Type: application/json' -d "{\"patient_name\":\"小程序患者\",\"patient_phone\":\"13900001111\",\"schedule_id\":$SCHED_ID}" "$BASE/open/his/v1/appointments")"
jqok "开放预约 reserved" "$OPEN_APPT" '.status == "reserved"'
jqok "错误 Key 拒绝(401)" "$(curl -s -o /dev/null -w '%{http_code}' -H 'X-API-Key: lk_wrong' "$BASE/open/his/v1/schedules?date=$WORK_DATE")" '. == 401'

echo ""
echo "结果：$PASS 通过 / $FAIL 失败"
[ "$FAIL" = "0" ]
