#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/configs/.env"

unset INTERNAL_WORKER_TOKEN
unset LINGCE_WORKER_URL
unset LINGCE_WORKER_TOKEN
unset RECORDING_WORKER_URL
unset RECORDING_WORKER_TOKEN
unset LLM_GATEWAY_URL
unset LLM_GATEWAY_API_KEY
unset BADGE_MIDDLEWARE_URL
unset BADGE_MIDDLEWARE_TOKEN
unset DATABASE_URL
unset SERVER_PORT
unset SERVER_HOST

if [[ -f "${ENV_FILE}" ]]; then
  set -a
  source "${ENV_FILE}"
  set +a
fi

cd "${ROOT_DIR}"
exec ./bin/lingce-api
