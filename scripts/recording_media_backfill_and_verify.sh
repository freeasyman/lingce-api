#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PSQL_BIN="${PSQL_BIN:-/opt/homebrew/Cellar/postgresql@15/15.15_1/bin/psql}"
CONFIG_PATH="${CONFIG_PATH:-${ROOT_DIR}/configs/dev.toml}"

if [[ ! -f "${CONFIG_PATH}" ]]; then
  echo "config file not found: ${CONFIG_PATH}"
  exit 1
fi

DATABASE_URL="$(
  cd "${ROOT_DIR}"
  go run - "${CONFIG_PATH}" <<'EOF'
package main

import (
	"fmt"
	"os"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
)

func main() {
	cfg, err := scriptutil.Load(os.Args[1])
	if err != nil {
		return
	}
	fmt.Print(cfg.DatabaseDSN())
}
EOF
)"

if [[ -z "${DATABASE_URL}" ]]; then
  echo "failed to resolve DATABASE_URL from ${CONFIG_PATH}"
  exit 1
fi

echo "[1/4] Pre-check coverage"
"${PSQL_BIN}" "${DATABASE_URL}" -f "${ROOT_DIR}/scripts/sql/recording_media_backfill_verify.sql"

if [[ "${RUN_BACKFILL:-0}" == "1" ]]; then
  echo "[2/4] Run backfill (DRY_RUN=${DRY_RUN:-false})"
  (cd "${ROOT_DIR}" && go run ./scripts/backfill_recording_media_to_oss.go --config "${CONFIG_PATH}" ${DRY_RUN:+--dry-run})
else
  echo "[2/4] Skip backfill (set RUN_BACKFILL=1 to execute)"
fi

echo "[3/4] Post-check coverage"
"${PSQL_BIN}" "${DATABASE_URL}" -f "${ROOT_DIR}/scripts/sql/recording_media_backfill_verify.sql"

echo "[4/4] Suggested pass criteria"
cat <<'EOF'
- vendor_dudutalk_cnt = 0
- has_oss_key_cnt / total_cnt >= 99%
- 异常项（缺 oss_key 但 file_url 非空）清单为空
EOF
