#!/bin/bash
# 从生产库全量导出数据并导入目标库（schema 必须已存在）
# 用法: ./migrate_data.sh <源库SSH主机> <目标连接串>
# 本地示例: ./migrate_data.sh root@8.140.246.26 "postgresql://yiliiang@localhost:5432/lingce_clean"
# RDS 示例:  ./migrate_data.sh root@8.140.246.26 "postgresql://lince:password@rds-host:5432/lingce_prod"

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

echo "==> 从生产全量导出数据并导入目标库..."
ssh "$SOURCE_HOST" "docker exec lince_postgres pg_dump -U $PROD_USER -d $PROD_DB \
  --data-only --no-owner --no-privileges --disable-triggers 2>/dev/null" \
  | $PSQL "$TARGET_URL" --set ON_ERROR_STOP=off 2>&1 | grep "^ERROR" | sort | uniq -c | sort -rn || true

echo "==> 修复序列值..."
$PSQL "$TARGET_URL" -t -c "
SELECT 'SELECT setval(' || quote_literal(s.sequence_name) || ', COALESCE((SELECT MAX(id) FROM ' || quote_ident(t.table_name) || '), 1));'
FROM information_schema.sequences s
JOIN information_schema.columns c ON c.column_default LIKE '%' || s.sequence_name || '%'
JOIN information_schema.tables t ON t.table_name = c.table_name
WHERE s.sequence_schema = 'public' AND c.column_name = 'id'
" 2>/dev/null | $PSQL "$TARGET_URL" 2>/dev/null || true

echo "==> 数据导入完成"
echo "==> 各主要表行数（前20）:"
$PSQL "$TARGET_URL" -c "
SELECT relname AS table_name, n_live_tup AS rows
FROM pg_stat_user_tables
WHERE schemaname = 'public' AND n_live_tup > 0
ORDER BY n_live_tup DESC LIMIT 20;" 2>&1
