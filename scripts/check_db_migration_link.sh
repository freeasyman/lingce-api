#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_SHA="${BASE_SHA:-}"
HEAD_SHA="${HEAD_SHA:-${GITHUB_SHA:-HEAD}}"

if [[ -n "${1:-}" ]]; then
  BASE_SHA="$1"
fi
if [[ -n "${2:-}" ]]; then
  HEAD_SHA="$2"
fi

if [[ -z "$BASE_SHA" ]]; then
  if [[ -n "${GITHUB_EVENT_BEFORE:-}" && "${GITHUB_EVENT_BEFORE:-}" != "0000000000000000000000000000000000000000" ]]; then
    BASE_SHA="$GITHUB_EVENT_BEFORE"
  elif git -C "$ROOT_DIR" rev-parse --verify HEAD^ >/dev/null 2>&1; then
    BASE_SHA="HEAD^"
  else
    upstream=""
    if upstream="$(git -C "$ROOT_DIR" rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null)"; then
      BASE_SHA="$(git -C "$ROOT_DIR" merge-base HEAD "$upstream")"
    fi
  fi
fi

if [[ -z "$BASE_SHA" ]]; then
  echo "unable to determine base revision for migration diff check" >&2
  exit 1
fi

CHANGED_FILES=()
while IFS= read -r file; do
  CHANGED_FILES+=("$file")
done < <(git -C "$ROOT_DIR" diff --name-only "$BASE_SHA" "$HEAD_SHA")

if [[ "${#CHANGED_FILES[@]}" -eq 0 ]]; then
  echo "no files changed in range $BASE_SHA..$HEAD_SHA"
  exit 0
fi

db_related=()
migration_related=()

is_db_related_path() {
  case "$1" in
    internal/*/store.go|internal/*/store_*.go|internal/*/model.go|internal/*/model_*.go|internal/store/compat_migration.go|scripts/sql/*.sql|scripts/cmd/backfill*/main.go|scripts/migrate_*.sh)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_migration_file() {
  case "$1" in
    internal/store/migrations/*.sql)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

for file in "${CHANGED_FILES[@]}"; do
  if is_db_related_path "$file"; then
    db_related+=("$file")
  fi
  if is_migration_file "$file"; then
    migration_related+=("$file")
  fi
done

if [[ "${#db_related[@]}" -eq 0 ]]; then
  echo "no database-sensitive files changed; migration linkage check passed"
  exit 0
fi

if [[ "${#migration_related[@]}" -eq 0 ]]; then
  {
    echo "database-sensitive files changed but no versioned migration file was added"
    echo
    echo "changed db-sensitive files:"
    printf '  - %s\n' "${db_related[@]}"
    echo
    echo "add a new file under internal/store/migrations/ and keep the code change in the same PR."
  } >&2
  exit 1
fi

echo "db-sensitive files changed and versioned migration files are present"
