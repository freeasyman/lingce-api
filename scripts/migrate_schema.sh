#!/bin/bash
# 从生产库全量导出 schema 并导入目标库
# 用法: ./migrate_schema.sh <源库SSH主机> <目标连接串>
# 本地示例: ./migrate_schema.sh root@8.140.246.26 "postgresql://yiliiang@localhost:5432/lingce_clean"
# RDS 示例:  ./migrate_schema.sh root@8.140.246.26 "postgresql://lince:password@rds-host:5432/lingce_prod"

set -euo pipefail

SOURCE_HOST="${1:-root@8.140.246.26}"
TARGET_URL="${2:-}"

if [ -z "$TARGET_URL" ]; then
  echo "用法: $0 <源库SSH主机> <目标数据库连接串>"
  exit 1
fi

PROD_USER="lince"
PROD_DB="lince_medical"
PSQL=$(which psql 2>/dev/null || echo "/opt/homebrew/Cellar/postgresql@15/15.15_1/bin/psql")

echo "==> 启用扩展..."
$PSQL "$TARGET_URL" -c "CREATE EXTENSION IF NOT EXISTS pg_trgm; CREATE EXTENSION IF NOT EXISTS vector;" 2>&1

echo "==> 从生产全量导出 schema 并导入目标库..."
ssh "$SOURCE_HOST" "docker exec lince_postgres pg_dump -U $PROD_USER -d $PROD_DB \
  --schema-only --no-owner --no-privileges --no-comments 2>/dev/null" \
  | $PSQL "$TARGET_URL" 2>&1

echo "==> schema 导入完成"
TABLE_COUNT=$($PSQL "$TARGET_URL" -t -c "SELECT count(*) FROM pg_tables WHERE schemaname='public';" | tr -d ' ')
echo "==> 目标库表数量: $TABLE_COUNT"
