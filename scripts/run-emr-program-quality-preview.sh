#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
  echo "用法: $0 <jwt_token> <template_version_id> [content_json_file]" >&2
  exit 2
fi

token="$1"
template_version_id="$2"
content_file="${3:-}"

if [[ -n "$content_file" ]]; then
  content="$(cat "$content_file")"
else
  content='{"chief_complaint":"咳嗽3天","present_illness":"咳嗽3天，无发热","diagnosis":"上呼吸道感染"}'
fi

payload="$(jq -cn \
  --arg template_version_id "$template_version_id" \
  --argjson content "$content" \
  '{template_version_id:$template_version_id, visit_type:"初诊", specialty:"内科", content:$content}')"

curl -sS \
  -X POST \
  -H "Authorization: Bearer ${token}" \
  -H "Content-Type: application/json" \
  --data "$payload" \
  "http://127.0.0.1:18080/api/v1/emr/quality-check/program-preview?tenant_id=1"

printf '\n日志文件: /Users/yiliiang/Documents/lingce-api/data/log/emr_program_quality_preview.log\n'
