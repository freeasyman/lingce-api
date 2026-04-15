#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
API_PREFIX="${API_PREFIX:-/api/v1}"
TENANT_ID="${TENANT_ID:-1}"
SESSION_VERSION="${SESSION_VERSION:-47}"
JWT_EXPIRY_HOURS="${JWT_EXPIRY_HOURS:-24}"

PASS_COUNT=0
FAIL_COUNT=0

log() {
  printf '%s\n' "$*"
}

gen_dev_token() {
  local secret="$1"
  python3 - "$secret" "$SESSION_VERSION" "$JWT_EXPIRY_HOURS" <<'PY'
import base64, hashlib, hmac, json, sys, time
secret = sys.argv[1].encode()
session_version = int(sys.argv[2])
expiry_hours = int(sys.argv[3])
header = {"alg":"HS256","typ":"JWT"}
now = int(time.time())
payload = {
  "sub": 1,
  "type": "admin",
  "session_version": session_version,
  "iat": now,
  "exp": now + expiry_hours * 3600,
}
def b64url(v: bytes) -> bytes:
  return base64.urlsafe_b64encode(v).rstrip(b"=")
segments = [
  b64url(json.dumps(header, separators=(",", ":")).encode()),
  b64url(json.dumps(payload, separators=(",", ":")).encode()),
]
sig = hmac.new(secret, b".".join(segments), hashlib.sha256).digest()
segments.append(b64url(sig))
print(b".".join(segments).decode())
PY
}

ensure_token() {
  if [[ -n "${TOKEN:-}" ]]; then
    return
  fi

  if [[ -n "${JWT_SECRET:-}" ]]; then
    TOKEN="$(gen_dev_token "$JWT_SECRET")"
    return
  fi

  if [[ -f "configs/.env" ]]; then
    # shellcheck disable=SC1091
    source "configs/.env"
    if [[ -n "${JWT_SECRET:-}" ]]; then
      TOKEN="$(gen_dev_token "$JWT_SECRET")"
      return
    fi
  fi

  log "ERROR: TOKEN/JWT_SECRET not provided."
  log "Set TOKEN directly, or provide JWT_SECRET (env or configs/.env)."
  exit 1
}

request() {
  local method="$1"
  local path="$2"
  local expected="$3"
  local category="$4"
  local name="$5"
  local data="${6:-}"
  local auth_mode="${7:-valid}" # valid | none | invalid

  local url="${BASE_URL}${API_PREFIX}${path}"
  local body_file
  body_file="$(mktemp)"
  local code
  local attempts=3
  local delay=1
  local i

  for ((i = 1; i <= attempts; i++)); do
    case "$auth_mode" in
      valid)
        if [[ -n "$data" ]]; then
          code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" -d "$data")" && break
        else
          code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer ${TOKEN}")" && break
        fi
        ;;
      invalid)
        if [[ -n "$data" ]]; then
          code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer invalid-token" -H "Content-Type: application/json" -d "$data")" && break
        else
          code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Authorization: Bearer invalid-token")" && break
        fi
        ;;
      none)
        if [[ -n "$data" ]]; then
          code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url" -H "Content-Type: application/json" -d "$data")" && break
        else
          code="$(curl -sS -o "$body_file" -w "%{http_code}" -X "$method" "$url")" && break
        fi
        ;;
      *)
        log "ERROR: unsupported auth_mode=$auth_mode"
        rm -f "$body_file"
        exit 1
        ;;
    esac
    if [[ "$i" -lt "$attempts" ]]; then
      sleep "$delay"
    fi
  done
  if [[ -z "${code:-}" ]]; then
    log "FAIL [$category] $name => request failed after retries"
    log "  path: $path"
    rm -f "$body_file"
    exit 1
  fi

  if [[ "$code" == "$expected" ]]; then
    PASS_COUNT=$((PASS_COUNT + 1))
    log "PASS [$category] $name => $code"
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
    log "FAIL [$category] $name => expected $expected got $code"
    log "  path: $path"
    log "  body: $(head -c 240 "$body_file")"
  fi
  rm -f "$body_file"
}

pick_ids() {
  local customers_json tags_json groups_json
  local attempts=3
  local delay=1
  local i

  for ((i = 1; i <= attempts; i++)); do
    customers_json="$(curl -sS "${BASE_URL}${API_PREFIX}/customers?tenant_id=${TENANT_ID}&page=1&page_size=20" -H "Authorization: Bearer ${TOKEN}")" && break
    if [[ "$i" -lt "$attempts" ]]; then
      sleep "$delay"
    fi
  done
  for ((i = 1; i <= attempts; i++)); do
    tags_json="$(curl -sS "${BASE_URL}${API_PREFIX}/customers/tags?tenant_id=${TENANT_ID}&page=1&page_size=20" -H "Authorization: Bearer ${TOKEN}")" && break
    if [[ "$i" -lt "$attempts" ]]; then
      sleep "$delay"
    fi
  done
  for ((i = 1; i <= attempts; i++)); do
    groups_json="$(curl -sS "${BASE_URL}${API_PREFIX}/customers/groups?tenant_id=${TENANT_ID}&page=1&page_size=20" -H "Authorization: Bearer ${TOKEN}")" && break
    if [[ "$i" -lt "$attempts" ]]; then
      sleep "$delay"
    fi
  done

  read -r CUSTOMER_ID CUSTOMER_PHONE TAG_ID GROUP_ID < <(
    python3 - "$customers_json" "$tags_json" "$groups_json" <<'PY'
import json, sys
customers = json.loads(sys.argv[1]).get("items", [])
tags = json.loads(sys.argv[2]).get("items", [])
groups = json.loads(sys.argv[3]).get("items", [])

customer_id = ""
customer_phone = ""
for c in customers:
  if c.get("id") is not None and c.get("phone"):
    customer_id = str(c["id"])
    customer_phone = str(c["phone"])
    break
if not customer_id and customers:
  customer_id = str(customers[0].get("id", ""))

tag_id = str(tags[0].get("id", "")) if tags else ""
group_id = str(groups[0].get("id", "")) if groups else ""

print(customer_id, customer_phone, tag_id, group_id)
PY
  )

  if [[ -z "${CUSTOMER_ID}" || -z "${TAG_ID}" || -z "${GROUP_ID}" ]]; then
    log "ERROR: failed to pick test IDs (customer/tag/group)."
    log "customer_id=${CUSTOMER_ID} tag_id=${TAG_ID} group_id=${GROUP_ID}"
    exit 1
  fi
  if [[ -z "${CUSTOMER_PHONE}" ]]; then
    # duplicates endpoint requires phone or email.
    CUSTOMER_PHONE="13900002222"
  fi
}

main() {
  ensure_token
  pick_ids

  log "Using tenant_id=${TENANT_ID} customer_id=${CUSTOMER_ID} tag_id=${TAG_ID} group_id=${GROUP_ID}"

  # Success cases
  request GET "/customers/${CUSTOMER_ID}/momentum-history" 200 "success" "momentum-history"
  request GET "/customers/duplicates?tenant_id=${TENANT_ID}&phone=${CUSTOMER_PHONE}" 200 "success" "duplicates-by-phone"
  request GET "/customers/${CUSTOMER_ID}/consultation-records?page=1&page_size=5" 200 "success" "consultation-records"
  request GET "/customers/${CUSTOMER_ID}/emr-records?page=1&page_size=5" 200 "success" "emr-records"
  request POST "/customers/tags/batch?tenant_id=${TENANT_ID}" 200 "success" "batch-tag-add" "{\"customer_ids\":[${CUSTOMER_ID}],\"tag_ids\":[${TAG_ID}],\"action\":\"add\"}"
  request GET "/customers/tags/stats?tenant_id=${TENANT_ID}" 200 "success" "tag-stats"
  request GET "/customers/groups/${GROUP_ID}/members?page=1&page_size=5" 200 "success" "group-members-list"
  request POST "/customers/groups/${GROUP_ID}/members" 200 "success" "group-members-add" "{\"customer_ids\":[${CUSTOMER_ID}]}"
  request DELETE "/customers/groups/${GROUP_ID}/members" 200 "success" "group-members-remove" "{\"customer_ids\":[${CUSTOMER_ID}]}"
  request POST "/customers/groups/rules/validate" 200 "success" "rules-validate-empty" "{\"rules\":{}}"

  # Parameter error cases
  request GET "/customers/duplicates?tenant_id=${TENANT_ID}" 400 "param_error" "duplicates-missing-params"
  request POST "/customers/actions/merge" 400 "param_error" "merge-target-equals-source" "{\"target_id\":${CUSTOMER_ID},\"source_ids\":[${CUSTOMER_ID}]}"
  request POST "/customers/groups/rules/preview?tenant_id=${TENANT_ID}" 400 "param_error" "rules-preview-empty" "{\"rules\":{}}"

  # Permission/auth error cases
  request GET "/customers/${CUSTOMER_ID}/momentum-history" 401 "permission_error" "momentum-history-no-auth" "" "none"
  request GET "/customers/tags/stats?tenant_id=${TENANT_ID}" 401 "permission_error" "tag-stats-invalid-token" "" "invalid"

  log ""
  log "Summary: pass=${PASS_COUNT} fail=${FAIL_COUNT}"
  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
}

main "$@"
