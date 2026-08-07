#!/usr/bin/env bash
set -euo pipefail

APP_NAME="lingce-api"
OUTPUT_DIR="bin"
OUTPUT_BIN="${OUTPUT_DIR}/${APP_NAME}"
VERSION="${VERSION:-$(cat VERSION 2>/dev/null || echo "1.0.6")}"
GIT_SHA="${GIT_SHA:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
BUILD_TIME="${BUILD_TIME:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
RELEASE_NOTE="${RELEASE_NOTE:-迁移体系门禁上线：启动只校验版本，DB变更必须同步版本化迁移}"

echo "==> Building ${APP_NAME} for linux/amd64 ..."
mkdir -p "${OUTPUT_DIR}"

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-X main.version=${VERSION} -X main.gitSHA=${GIT_SHA} -X main.buildTime=${BUILD_TIME} -X main.releaseNote=${RELEASE_NOTE}" \
  -o "${OUTPUT_BIN}" \
  ./cmd/lingce-api

echo "==> Build complete: ${OUTPUT_BIN}"
echo "==> Metadata: version=${VERSION} release_note=${RELEASE_NOTE} git_sha=${GIT_SHA} build_time=${BUILD_TIME}"
ls -lh "${OUTPUT_BIN}"
