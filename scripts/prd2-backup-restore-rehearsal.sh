#!/usr/bin/env bash
set -euo pipefail

if [[ "$-" == *x* ]]; then
  echo "shell xtrace must be disabled before running the backup rehearsal" >&2
  exit 2
fi

umask 077

SOURCE_DSN="${UNIROUTE_BACKUP_SOURCE_DSN:-${DB_DSN:-}}"
RESTORE_DSN="${UNIROUTE_RESTORE_REHEARSAL_DSN:-}"
SOURCE_PASSWORD="${UNIROUTE_BACKUP_SOURCE_PASSWORD:-}"
RESTORE_PASSWORD="${UNIROUTE_RESTORE_REHEARSAL_PASSWORD:-}"
CONFIRM="${UNIROUTE_BACKUP_RESTORE_CONFIRM:-}"
DUMP_OVERRIDE="${UNIROUTE_BACKUP_DUMP_FILE:-}"

unset UNIROUTE_BACKUP_SOURCE_PASSWORD
unset UNIROUTE_RESTORE_REHEARSAL_PASSWORD
unset PGPASSWORD
export -n SOURCE_PASSWORD RESTORE_PASSWORD

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

dsn_contains_password() {
  local dsn="$1"
  local -a query_parameters
  local IFS
  local query_parameter
  local query_key

  if [[ "$dsn" =~ postgres(ql)?://[^/@[:space:]?]*:[^/@[:space:]?]*@ ]]; then
    return 0
  fi
  if [[ "$dsn" =~ (^|[?\&[:space:]])[Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd][[:space:]]*= ]]; then
    return 0
  fi

  if [[ "$dsn" == *\?* ]]; then
    IFS='&' read -r -a query_parameters <<< "${dsn#*\?}"
    for query_parameter in "${query_parameters[@]}"; do
      query_key="${query_parameter%%=*}"
      query_key="${query_key//%50/P}"
      query_key="${query_key//%70/p}"
      query_key="${query_key//%41/A}"
      query_key="${query_key//%61/a}"
      query_key="${query_key//%53/S}"
      query_key="${query_key//%73/s}"
      query_key="${query_key//%57/W}"
      query_key="${query_key//%77/w}"
      query_key="${query_key//%4F/O}"
      query_key="${query_key//%4f/O}"
      query_key="${query_key//%6F/o}"
      query_key="${query_key//%6f/o}"
      query_key="${query_key//%52/R}"
      query_key="${query_key//%72/r}"
      query_key="${query_key//%44/D}"
      query_key="${query_key//%64/d}"
      if [[ "$query_key" =~ ^[Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]$ ]]; then
        return 0
      fi
    done
  fi
  return 1
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
  if dsn_contains_password "$SOURCE_DSN"; then
    echo "source DSN must not contain a password; use UNIROUTE_BACKUP_SOURCE_PASSWORD" >&2
    exit 2
  fi
  if dsn_contains_password "$RESTORE_DSN"; then
    echo "restore DSN must not contain a password; use UNIROUTE_RESTORE_REHEARSAL_PASSWORD" >&2
    exit 2
  fi
  if [[ -n "$DUMP_OVERRIDE" ]]; then
    echo "UNIROUTE_BACKUP_DUMP_FILE is not supported; the script creates a private dump workspace" >&2
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

create_private_dump() {
  local tmp_root="${TMPDIR:-/tmp}"
  local workspace_template

  if [[ ! -d "$tmp_root" ]]; then
    echo "TMPDIR must name an existing directory" >&2
    exit 2
  fi
  if [[ "$tmp_root" == "/" ]]; then
    workspace_template="/uniroute-prd2-backup-rehearsal.XXXXXX"
  else
    workspace_template="${tmp_root%/}/uniroute-prd2-backup-rehearsal.XXXXXX"
  fi

  DUMP_WORKSPACE="$(mktemp -d "$workspace_template")"
  chmod 0700 "$DUMP_WORKSPACE"
  DUMP_FILE="$(mktemp "$DUMP_WORKSPACE/database.XXXXXX")"
  chmod 0600 "$DUMP_FILE"
}

run_restore_psql() {
  PGPASSWORD="$RESTORE_PASSWORD" psql "$RESTORE_DSN" "$@"
}

need_cmd pg_dump
need_cmd psql
need_cmd mktemp
require_safe_target
create_private_dump

echo "Backing up source database to $DUMP_FILE"
PGPASSWORD="$SOURCE_PASSWORD" pg_dump --no-owner --no-privileges "$SOURCE_DSN" > "$DUMP_FILE"
test -s "$DUMP_FILE"

echo "Restoring dump into the rehearsal database"
run_restore_psql -v ON_ERROR_STOP=1 -f "$DUMP_FILE" >/dev/null

echo "Validating restored schema"
schema_versions="$(run_restore_psql -v ON_ERROR_STOP=1 -Atc "SELECT COUNT(*) FROM schema_migrations;")"
core_tables="$(run_restore_psql -v ON_ERROR_STOP=1 -Atc "SELECT COUNT(*) FROM (VALUES ('orgs'), ('projects'), ('api_keys'), ('call_logs'), ('audit_logs'), ('transactions')) AS t(name) WHERE to_regclass('public.' || name) IS NOT NULL;")"

if [[ "$schema_versions" -lt 1 ]]; then
  echo "restore validation failed: schema_migrations has no rows" >&2
  exit 1
fi
if [[ "$core_tables" -ne 6 ]]; then
  echo "restore validation failed: restored core table count=$core_tables" >&2
  exit 1
fi

echo "PASS backup/restore rehearsal schema_versions=$schema_versions core_tables=$core_tables dump_file=$DUMP_FILE"
