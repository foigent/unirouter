#!/usr/bin/env bash
set -euo pipefail

SOURCE_DSN="${UNIROUTE_BACKUP_SOURCE_DSN:-${DB_DSN:-}}"
RESTORE_DSN="${UNIROUTE_RESTORE_REHEARSAL_DSN:-}"
CONFIRM="${UNIROUTE_BACKUP_RESTORE_CONFIRM:-}"
DUMP_FILE="${UNIROUTE_BACKUP_DUMP_FILE:-${TMPDIR:-/tmp}/uniroute-prd2-backup-rehearsal-$(date +%Y%m%d%H%M%S).sql}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

require_safe_target() {
  if [[ -z "$SOURCE_DSN" ]]; then
    echo "UNIROUTE_BACKUP_SOURCE_DSN or DB_DSN is required" >&2
    exit 2
  fi
  if [[ -z "$RESTORE_DSN" ]]; then
    echo "UNIROUTE_RESTORE_REHEARSAL_DSN is required and must point to an empty non-production database" >&2
    exit 2
  fi
  if [[ "$SOURCE_DSN" == "$RESTORE_DSN" ]]; then
    echo "source and restore DSN must be different" >&2
    exit 2
  fi
  if [[ "$CONFIRM" != "non-production-restore-target" ]]; then
    echo "set UNIROUTE_BACKUP_RESTORE_CONFIRM=non-production-restore-target to confirm the restore target is safe" >&2
    exit 2
  fi
}

need_cmd pg_dump
need_cmd psql
require_safe_target

echo "Backing up source database to $DUMP_FILE"
pg_dump --no-owner --no-privileges "$SOURCE_DSN" > "$DUMP_FILE"
test -s "$DUMP_FILE"

echo "Restoring dump into the rehearsal database"
psql "$RESTORE_DSN" -v ON_ERROR_STOP=1 -f "$DUMP_FILE" >/dev/null

echo "Validating restored schema"
schema_versions="$(psql "$RESTORE_DSN" -v ON_ERROR_STOP=1 -Atc "SELECT COUNT(*) FROM schema_migrations;")"
core_tables="$(psql "$RESTORE_DSN" -v ON_ERROR_STOP=1 -Atc "SELECT COUNT(*) FROM (VALUES ('orgs'), ('projects'), ('api_keys'), ('call_logs'), ('audit_logs'), ('transactions')) AS t(name) WHERE to_regclass('public.' || name) IS NOT NULL;")"

if [[ "$schema_versions" -lt 1 ]]; then
  echo "restore validation failed: schema_migrations has no rows" >&2
  exit 1
fi
if [[ "$core_tables" -ne 6 ]]; then
  echo "restore validation failed: restored core table count=$core_tables" >&2
  exit 1
fi

echo "PASS backup/restore rehearsal schema_versions=$schema_versions core_tables=$core_tables dump_file=$DUMP_FILE"
