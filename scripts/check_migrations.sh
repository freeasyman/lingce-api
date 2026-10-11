#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGRATION_DIR="$ROOT_DIR/internal/store/migrations"

if [[ ! -d "$MIGRATION_DIR" ]]; then
  echo "missing migration directory: $MIGRATION_DIR" >&2
  exit 1
fi

FILES="$(find "$MIGRATION_DIR" -maxdepth 1 -type f -name '*.sql' | sort)"
if [[ -z "$FILES" ]]; then
  echo "no migration files found in $MIGRATION_DIR" >&2
  exit 1
fi

prev=""
while IFS= read -r file; do
  base="$(basename "$file")"
  if [[ ! "$base" =~ ^([0-9]{8}_[0-9]{3})_([a-z0-9_]+)\.sql$ ]]; then
    echo "invalid migration filename: $base" >&2
    exit 1
  fi
  version="${BASH_REMATCH[1]}"
  if [[ -n "$prev" && "$version" == "$prev" ]]; then
    echo "duplicate migration version: $version" >&2
    exit 1
  fi
  if [[ -n "$prev" && "$version" < "$prev" ]]; then
    echo "migration versions are not sorted: $prev -> $version" >&2
    exit 1
  fi
  prev="$version"
done <<< "$FILES"

echo "migration files look consistent"
