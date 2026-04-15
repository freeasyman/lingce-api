#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"
TENANT_ID="${TENANT_ID:-1}"

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

request() {
  local method="$1"
  local path="$2"
  local expected_http="$3"
  local check_expr="$4"
  local name="$5"
  local body="${6:-}"

  local url="${BASE_URL}/api/v1${path}"
  local out
  out="$(mktemp)"
  local code

  if [[ -n "$body" ]]; then
    code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$body")"
  else
    code="$(curl -sS -o "$out" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")"
  fi

  if [[ "$code" != "$expected_http" ]]; then
    fail "${name} (http=${code}, expected=${expected_http})"
    cat "$out" >&2
    rm -f "$out"
    return 1
  fi

  if [[ -n "$check_expr" ]] && ! jq -e "$check_expr" "$out" >/dev/null 2>&1; then
    fail "${name} (body assertion failed: ${check_expr})"
    cat "$out" >&2
    rm -f "$out"
    return 1
  fi

  pass "$name"
  cat "$out"
  rm -f "$out"
}

main() {
  command -v curl >/dev/null 2>&1 || { log "curl is required"; exit 1; }
  command -v jq >/dev/null 2>&1 || { log "jq is required"; exit 1; }

  if [[ -z "$TOKEN" ]]; then
    log "TOKEN is required"
    exit 1
  fi

  local suffix
  suffix="$(date +%s)"

  log "== content resource regression =="

  request GET "/content-topics?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 '(.items|type=="array") and (.total >= 0)' "topics-list"
  request GET "/content-items?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 '(.items|type=="array") and (.total >= 0)' "contents-list"
  request GET "/content-seeds?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 '(.items|type=="array") and (.total >= 0)' "seeds-list"
  request GET "/content-items/prompts?tenant_id=${TENANT_ID}&page=1&page_size=20" 200 '(.items|type=="array") and (.total >= 0)' "content-prompts-list"

  local topic_create_json topic_id
  topic_create_json="$(request POST "/content-topics" 200 '.id > 0' "topic-create" "{\"title\":\"REG Topic ${suffix}\",\"description\":\"regression\",\"source\":\"manual\",\"tags\":[],\"extra_data\":{}}")"
  topic_id="$(echo "$topic_create_json" | jq -r '.id')"

  request GET "/content-topics/${topic_id}" 200 '(.id > 0) and ((.title|type)=="string")' "topic-detail"
  request PUT "/content-topics/${topic_id}" 200 '(.id > 0) and (.title=="REG Topic Updated")' "topic-update" '{"title":"REG Topic Updated"}'
  request POST "/content-topics/${topic_id}/actions/select" 200 '((.message|type)=="string") or (.status=="selected")' "topic-select" '{}'

  local content_create_json content_id
  content_create_json="$(request POST "/content-items?tenant_id=${TENANT_ID}" 200 '.id > 0' "content-create" "{\"topic_id\":${topic_id},\"title\":\"REG Content ${suffix}\",\"content\":\"hello regression\",\"tags\":[],\"images\":[],\"extra_data\":{}}")"
  content_id="$(echo "$content_create_json" | jq -r '.id')"

  request GET "/content-items/${content_id}" 200 '(.id > 0) and ((.title|type)=="string")' "content-detail"
  request PUT "/content-items/${content_id}" 200 '(.id > 0) and (.title=="REG Content Updated")' "content-update" '{"title":"REG Content Updated"}'
  request POST "/content-items/${content_id}/actions/publish" 200 '((.message|type)=="string") or (.status=="published")' "content-publish" '{}'
  request POST "/content-items/${content_id}/actions/unpublish" 200 '((.message|type)=="string") or (.status=="draft") or (.status=="unpublished")' "content-unpublish" '{}'

  local code="reg_tpl_${suffix}" clone_id tpl_id
  local tpl_create_json
  tpl_create_json="$(request POST "/content-items/prompts?tenant_id=${TENANT_ID}" 200 '.id > 0' "content-template-create" "{\"code\":\"${code}\",\"name\":\"REG Template ${suffix}\",\"template\":\"{{title}}\",\"variables\":[\"title\"]}")"
  tpl_id="$(echo "$tpl_create_json" | jq -r '.id')"

  request GET "/content-items/prompts/${tpl_id}" 200 '(.id > 0) and ((.code|type)=="string")' "content-template-detail"
  request PUT "/content-items/prompts/${tpl_id}" 200 '(.id > 0) and (.name=="REG Template Updated")' "content-template-update" '{"name":"REG Template Updated"}'

  local clone_json
  clone_json="$(request POST "/content-items/prompts/${tpl_id}/actions/clone?tenant_id=${TENANT_ID}" 200 '(.id > 0) and ((.code|type)=="string")' "content-template-clone" '{}')"
  clone_id="$(echo "$clone_json" | jq -r '.id')"

  request DELETE "/content-items/prompts/${clone_id}" 200 '.message|type=="string"' "content-template-delete-clone"
  request DELETE "/content-items/prompts/${tpl_id}" 200 '.message|type=="string"' "content-template-delete-original"

  request DELETE "/content-items/${content_id}" 200 '.message|type=="string"' "content-delete"
  request DELETE "/content-topics/${topic_id}" 200 '.message|type=="string"' "topic-delete"

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"
  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
