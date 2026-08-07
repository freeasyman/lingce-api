#!/usr/bin/env bash
set -euo pipefail

# Optional environment variables:
#   DEPLOY_HOST               (default: root@8.140.246.26)
#   DEPLOY_REMOTE_DIR         (default: /opt/lingce/bin)
#   DEPLOY_SERVICE_NAME       (default: lingce-api)
#   DEPLOY_HEALTH_URL         (default: http://127.0.0.1:18080/healthz)
#   DEPLOY_HEALTH_TIMEOUT_SEC (default: 30)
#   DEPLOY_LOG_PATH           (default: /var/log/lingce-deploy.log)
#   DEPLOY_ACTOR              (default: $USER)

REMOTE_HOST="${DEPLOY_HOST:-root@8.140.246.26}"
REMOTE_DIR="${DEPLOY_REMOTE_DIR:-/opt/lingce/bin}"
SERVICE_NAME="${DEPLOY_SERVICE_NAME:-lingce-api}"
HEALTH_URL="${DEPLOY_HEALTH_URL:-http://127.0.0.1:18080/healthz}"
HEALTH_TIMEOUT_SEC="${DEPLOY_HEALTH_TIMEOUT_SEC:-30}"
DEPLOY_LOG_PATH="${DEPLOY_LOG_PATH:-/var/log/lingce-deploy.log}"
DEPLOY_ACTOR="${DEPLOY_ACTOR:-${USER:-unknown}}"
LOCAL_BIN="bin/${SERVICE_NAME}"

VERSION="${VERSION:-$(cat VERSION 2>/dev/null || echo "1.0.6")}"
GIT_SHA="${GIT_SHA:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
BUILD_TIME="${BUILD_TIME:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
RELEASE_NOTE="${RELEASE_NOTE:-迁移体系门禁上线：启动只校验版本，DB变更必须同步版本化迁移}"

echo "==> Building ..."
VERSION="${VERSION}" GIT_SHA="${GIT_SHA}" BUILD_TIME="${BUILD_TIME}" RELEASE_NOTE="${RELEASE_NOTE}" bash scripts/build.sh

echo "==> Uploading ${LOCAL_BIN} to ${REMOTE_HOST}:${REMOTE_DIR}/${SERVICE_NAME}.new ..."
ssh "${REMOTE_HOST}" "mkdir -p ${REMOTE_DIR}"
scp "${LOCAL_BIN}" "${REMOTE_HOST}:${REMOTE_DIR}/${SERVICE_NAME}.new"

echo "==> Deploying on ${REMOTE_HOST} ..."
ssh "${REMOTE_HOST}" \
  "REMOTE_DIR='${REMOTE_DIR}' SERVICE_NAME='${SERVICE_NAME}' HEALTH_URL='${HEALTH_URL}' HEALTH_TIMEOUT_SEC='${HEALTH_TIMEOUT_SEC}' DEPLOY_LOG_PATH='${DEPLOY_LOG_PATH}' DEPLOY_ACTOR='${DEPLOY_ACTOR}' VERSION='${VERSION}' GIT_SHA='${GIT_SHA}' BUILD_TIME='${BUILD_TIME}' RELEASE_NOTE='${RELEASE_NOTE}' bash -s" <<'REMOTE_SCRIPT'
set -euo pipefail

cd "${REMOTE_DIR}"
STAMP="$(date +%Y%m%d%H%M%S)"
BACKUP="${SERVICE_NAME}.bak.${STAMP}"

restart_service() {
  if command -v sudo >/dev/null 2>&1; then
    sudo systemctl restart "${SERVICE_NAME}"
  else
    systemctl restart "${SERVICE_NAME}"
  fi
}

append_deploy_log() {
  local status="$1"
  local line
  line="$(date '+%F %T') service=${SERVICE_NAME} version=${VERSION} release_note=${RELEASE_NOTE} git_sha=${GIT_SHA} build_time=${BUILD_TIME} actor=${DEPLOY_ACTOR} status=${status}"
  if [ -w "${DEPLOY_LOG_PATH}" ] || [ ! -e "${DEPLOY_LOG_PATH}" ]; then
    echo "${line}" >> "${DEPLOY_LOG_PATH}" || true
  elif command -v sudo >/dev/null 2>&1; then
    echo "${line}" | sudo tee -a "${DEPLOY_LOG_PATH}" >/dev/null || true
  fi
}

if [ -f "${SERVICE_NAME}" ]; then
  cp "${SERVICE_NAME}" "${BACKUP}"
fi

mv "${SERVICE_NAME}.new" "${SERVICE_NAME}"
chmod +x "${SERVICE_NAME}"

restart_service

ok=0
for _ in $(seq 1 "${HEALTH_TIMEOUT_SEC}"); do
  if curl -fsS "${HEALTH_URL}" >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 1
done

if [ "${ok}" -eq 1 ]; then
  echo "==> Deploy success, health check passed: ${HEALTH_URL}"
  append_deploy_log "success"
  exit 0
fi

echo "==> Deploy failed, rolling back ..."
if [ -f "${BACKUP}" ]; then
  cp "${BACKUP}" "${SERVICE_NAME}"
  chmod +x "${SERVICE_NAME}"
  restart_service
fi

append_deploy_log "failed"

exit 1
REMOTE_SCRIPT

echo "==> Done"
