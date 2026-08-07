#!/usr/bin/env bash
set -euo pipefail

NAME="${1:-}"
MIGRATION_DIR="internal/store/migrations"

if [[ -z "$NAME" ]]; then
  echo "usage: $0 <snake_case_name>" >&2
  exit 1
fi

if [[ ! "$NAME" =~ ^[a-z0-9_]+$ ]]; then
  echo "migration name must be snake_case using lowercase letters, numbers, and underscores" >&2
  exit 1
fi

mkdir -p "$MIGRATION_DIR"

TODAY="$(date +%Y%m%d)"
LAST_SEQ="$(
  find "$MIGRATION_DIR" -maxdepth 1 -type f -name "${TODAY}_*.sql" \
    | sed -E "s#^.*/${TODAY}_([0-9]{3})_.*\\.sql#\\1#" \
    | sort -n \
    | tail -n 1
)"

if [[ -z "$LAST_SEQ" ]]; then
  NEXT_SEQ="001"
else
  NEXT_SEQ="$(printf "%03d" "$((10#$LAST_SEQ + 1))")"
fi

TARGET="${MIGRATION_DIR}/${TODAY}_${NEXT_SEQ}_${NAME}.sql"
if [[ -e "$TARGET" ]]; then
  echo "migration already exists: $TARGET" >&2
  exit 1
fi

cat > "$TARGET" <<SQL
-- ${TODAY}_${NEXT_SEQ}_${NAME}
-- Add the schema/data migration SQL below.

SQL

echo "$TARGET"
