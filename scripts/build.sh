#!/usr/bin/env bash
set -euo pipefail

APP_NAME="lingce-api"
OUTPUT_DIR="bin"
OUTPUT_BIN="${OUTPUT_DIR}/${APP_NAME}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
GIT_SHA="${GIT_SHA:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
BUILD_TIME="${BUILD_TIME:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"

echo "==> Building ${APP_NAME} for linux/amd64 ..."
mkdir -p "${OUTPUT_DIR}"

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-X main.version=${VERSION} -X main.gitSHA=${GIT_SHA} -X main.buildTime=${BUILD_TIME}" \
  -o "${OUTPUT_BIN}" \
  ./cmd/lingce-api

echo "==> Build complete: ${OUTPUT_BIN}"
echo "==> Metadata: version=${VERSION} git_sha=${GIT_SHA} build_time=${BUILD_TIME}"
ls -lh "${OUTPUT_BIN}"
