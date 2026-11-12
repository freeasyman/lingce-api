#!/usr/bin/env bash
# 计费缴费「第 0 阶段」本地测试库重建脚本：跑通项目自身的迁移机制并校验计费表。
#
# 为什么不是从空库直接 --migrate-only：
#   1) 历史模式的 baseline（internal/store/migrations/20260807_001_baseline.sql）只是 marker，
#      早期迁移依赖 2026-08 之前的历史 schema（如 employees / institution_employee_roles），
#      本仓库不提供该基线 dump，空库重放会在 20260830_003 直接失败；
#   2) 收敛基线的入口是 --converge-only，但它要求目标库已有生产同构 schema
#      （例如 emr_quality_rule_definitions），也不是空库可跑。
#   所以本脚本按本地已有的「生产 schema 快照」重建：
#     prod 同构 schema 快照（SOURCE_DB）
#       → --converge-only 应用审计收敛基线 20261019_008（项目自身机制）
#       → 补记基线时点的 migration 账本（155 个版本）
#       → --migrate-only 应用基线之后的增量迁移（含计费 20261110_002/004、20261112_001/002）
#
# 用法：
#   bash scripts/billing/setup-billing-test-db.sh
#   TARGET_DB=lingce_billing_test SOURCE_DB=lingce_prod_schema_base_20261019_008 \
#     PGUSER=yiliiang PGHOST=127.0.0.1 PGPORT=5432 bash scripts/billing/setup-billing-test-db.sh
#
# 依赖：psql / createdb / dropdb / pg_dump / shasum / go。psql 不在 PATH 时自动用
# Homebrew libpq 的路径（/opt/homebrew/opt/libpq/bin）。
#
# 说明：
#   - 只操作本地数据库；SOURCE_DB 只读（pg_dump --schema-only）。
#   - 脚本可重复执行：每次都会 DROP 并重建 TARGET_DB。
#   - 脚本会按 TARGET_DB / PG* 生成临时配置（退出时删除），不依赖 configs/ 里的固定库名。
#   - 本地基线缺少迁移 20261109_002 依赖的非版本化表 deal_cards_v2，脚本会补一个
#     最小占位表（仅本地测试用途），详见文件末尾说明。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

TARGET_DB="${TARGET_DB:-lingce_billing_test}"
SOURCE_DB="${SOURCE_DB:-lingce_prod_schema_base_20261019_008}"
BASELINE_VERSION="${BASELINE_VERSION:-20261019_008}"

export PGHOST="${PGHOST:-127.0.0.1}"
export PGPORT="${PGPORT:-5432}"
export PGUSER="${PGUSER:-$(whoami)}"
SSLMODE="${PGSSLMODE:-disable}"

if ! command -v psql >/dev/null 2>&1; then
  if [[ -x /opt/homebrew/opt/libpq/bin/psql ]]; then
    export PATH="/opt/homebrew/opt/libpq/bin:$PATH"
  else
    echo "找不到 psql，请安装 libpq 或把它加入 PATH" >&2
    exit 1
  fi
fi

say() { printf '\n==> %s\n' "$*"; }

say "检查源库 $SOURCE_DB 是否存在"
if [[ "$(psql -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname='$SOURCE_DB'")" != "1" ]]; then
  cat >&2 <<EOF
源库 $SOURCE_DB 不存在。这个脚本需要一个「生产 schema 快照」作为迁移起点：
  SOURCE_DB=your_prod_schema_snapshot bash scripts/billing/setup-billing-test-db.sh
本机可用的候选：
$(psql -d postgres -Atc "SELECT '  - '||datname FROM pg_database WHERE datname LIKE 'lingce%' ORDER BY 1")
EOF
  exit 1
fi

CONFIG_DIR="$(mktemp -d "${TMPDIR:-/tmp}/lingce-billing-test.XXXXXX")"
trap 'rm -rf "$CONFIG_DIR"' EXIT

say "生成临时迁移配置（目标库 $TARGET_DB @ $PGHOST:$PGPORT）"
cat > "$CONFIG_DIR/secrets.toml" <<EOF
[database]
password = "${PGPASSWORD:-billing_local_test}"

[jwt]
secret = "billing-local-test-jwt-secret-not-for-real-use"

[external]
badge_middleware_token = "billing-local-test-internal-token"
EOF

write_config() {
  local schema_block="$1" path="$2"
  cat > "$path" <<EOF
[app]
name = "lingce-api"
env = "test"
secrets_file = "$CONFIG_DIR/secrets.toml"

[server]
port = "18099"
host = "127.0.0.1"

[database]
host = "$PGHOST"
port = $PGPORT
name = "$TARGET_DB"
user = "$PGUSER"
sslmode = "$SSLMODE"
max_conns = 10
min_conns = 1

[schema]
$schema_block

[jwt]
expiry_hours = 24

[external]
llm_gateway_url = "http://localhost:8080"
badge_middleware_url = "http://localhost:18082"

[log]
level = "info"
EOF
}

write_config "mode = \"baseline\"
baseline_version = \"$BASELINE_VERSION\"
convergence_file = \"$BASELINE_VERSION.sql\"
max_migration_version = \"\"" "$CONFIG_DIR/baseline.toml"
write_config "max_migration_version = \"\"" "$CONFIG_DIR/history.toml"

say "重建目标库 $TARGET_DB（只重建本地测试库）"
dropdb --if-exists "$TARGET_DB"
createdb "$TARGET_DB"

say "从 $SOURCE_DB 导入 schema（只导结构，不导数据）"
# 新版 pg_dump 会带 transaction_timeout 参数，旧版服务端会报 unrecognized parameter；
# 该报错不影响后续 schema 导入，这里只把噪声过滤掉。
pg_dump --schema-only --no-owner --no-privileges -d "$SOURCE_DB" 2>/dev/null \
  | psql -q -d "$TARGET_DB" 2>&1 | grep -v 'unrecognized configuration parameter "transaction_timeout"' || true

say "应用审计收敛基线 $BASELINE_VERSION（--converge-only）"
go run ./cmd/lingce-api --config "$CONFIG_DIR/baseline.toml" --converge-only

say "补建本地迁移链依赖的非版本化占位表 deal_cards_v2"
# 迁移 20261109_002 会把 deal_cards_v2 改名为 consultation_analyses，并直接 UPDATE 它；
# 本地基线没有这张表（历史上由非版本化 SQL 建在 dev 库），不补会中断迁移。
psql -q -d "$TARGET_DB" <<'SQL'
CREATE TABLE IF NOT EXISTS deal_cards_v2 (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT,
    status TEXT,
    card JSONB,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
SQL

say "补记基线时点的 migration 账本（<= $BASELINE_VERSION）"
psql -q -d "$TARGET_DB" <<'SQL'
CREATE TABLE IF NOT EXISTS api_schema_migrations (
    version TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT NOW()
);
SQL

{
  echo "BEGIN;"
  while IFS= read -r file; do
    base="$(basename "$file")"
    [[ "$base" =~ ^([0-9]{8}_[0-9]{3})_([a-z0-9_]+)\.sql$ ]] || continue
    version="${BASH_REMATCH[1]}"
    name="${BASH_REMATCH[2]}"
    [[ "$version" > "$BASELINE_VERSION" ]] && continue
    checksum="$(shasum -a 256 "$file" | awk '{print $1}')"
    printf "INSERT INTO api_schema_migrations(version,name,checksum,applied_at) VALUES ('%s','%s','%s',NOW()) ON CONFLICT (version) DO NOTHING;\n" \
      "$version" "$name" "$checksum"
  done < <(ls internal/store/migrations/*.sql | sort)
  echo "COMMIT;"
} | psql -q -d "$TARGET_DB"

say "应用基线之后的增量迁移（--migrate-only）"
go run ./cmd/lingce-api --config "$CONFIG_DIR/history.toml" --migrate-only

say "校验：计费 4 张表 + 价目 12 行 + 迁移账本"
BILLING_TABLES="$(psql -d "$TARGET_DB" -Atc "
SELECT count(*) FROM information_schema.tables
WHERE table_schema='public'
  AND table_name IN ('product_catalog','product_entitlements','entitlement_ledger','billing_orders')")"
CATALOG_ROWS="$(psql -d "$TARGET_DB" -Atc "SELECT count(*) FROM product_catalog")"
MIGRATIONS="$(psql -d "$TARGET_DB" -Atc "SELECT count(*) || ' (max ' || max(version) || ')' FROM api_schema_migrations")"
SERVICE_COLUMNS="$(psql -d "$TARGET_DB" -Atc "
SELECT count(*) FROM information_schema.columns
WHERE table_schema='public' AND table_name='tenant_subscriptions'
  AND column_name IN ('started_on','expired_on')")"
TABLES_TOTAL="$(psql -d "$TARGET_DB" -Atc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")"

echo "  表总数:            $TABLES_TOTAL"
echo "  计费表:            $BILLING_TABLES/4"
echo "  product_catalog:   $CATALOG_ROWS 行（期望 12）"
echo "  服务期列:          $SERVICE_COLUMNS/2（started_on, expired_on）"
echo "  迁移账本:          $MIGRATIONS"

if [[ "$BILLING_TABLES" != "4" || "$CATALOG_ROWS" != "12" || "$SERVICE_COLUMNS" != "2" ]]; then
  echo "校验失败：计费表/价目/服务期列不符合预期" >&2
  exit 1
fi

say "完成。跑端到端冒烟："
cat <<EOF
  BILLING_TEST_DATABASE_URL='postgresql://${PGUSER}@${PGHOST}:${PGPORT}/${TARGET_DB}?sslmode=disable' \\
    go test ./internal/billing/ -run TestBillingEndToEnd -count=1 -v
EOF
