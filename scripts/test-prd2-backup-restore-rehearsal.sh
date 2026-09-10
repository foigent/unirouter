#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REHEARSAL_SCRIPT="${UNIROUTE_TEST_REHEARSAL_SCRIPT:-$SCRIPT_DIR/prd2-backup-restore-rehearsal.sh}"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/uniroute-backup-security-test.XXXXXX")"
FAKE_BIN="$TEST_ROOT/fake-bin"
FAILURES=0

mkdir -m 0700 "$FAKE_BIN"

cat > "$FAKE_BIN/mktemp" <<'FAKE_MKTEMP'
#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -eq 2 && "$1" == "-d" ]]; then
  template="$2"
  path="${template%XXXXXX}fake"
  [[ "$template" == *XXXXXX && ! -e "$path" && ! -L "$path" ]]
  mkdir "$path"
elif [[ "$#" -eq 1 ]]; then
  template="$1"
  path="${template%XXXXXX}fake"
  [[ "$template" == *XXXXXX && ! -e "$path" && ! -L "$path" ]]
  : > "$path"
else
  printf '%s\n' 'unsupported fake mktemp invocation' >&2
  exit 64
fi

printf '%s\n' "$*" >> "$UNIROUTE_TEST_STATE_DIR/mktemp_calls"
printf '%s\n' "$path"
FAKE_MKTEMP

cat > "$FAKE_BIN/pg_dump" <<'FAKE_PG_DUMP'
#!/usr/bin/env bash
set -euo pipefail

argv_secret=0
for arg in "$@"; do
  case "$arg" in
    *source-password-marker*|*restore-password-marker*) argv_secret=1 ;;
  esac
done

dsn_preserved=0
if [[ "$#" -eq 3 && "$1" == "--no-owner" && "$2" == "--no-privileges" && "$3" == "$UNIROUTE_TEST_EXPECTED_SOURCE_DSN" ]]; then
  dsn_preserved=1
fi

password_scoped=0
if [[ "${PGPASSWORD:-}" == "source-password-marker" ]]; then
  password_scoped=1
fi

secret_inputs_unset=0
if [[ -z "${UNIROUTE_BACKUP_SOURCE_PASSWORD+x}" && -z "${UNIROUTE_RESTORE_REHEARSAL_PASSWORD+x}" ]]; then
  secret_inputs_unset=1
fi

internal_passwords_unset=0
if [[ -z "${SOURCE_PASSWORD+x}" && -z "${RESTORE_PASSWORD+x}" ]]; then
  internal_passwords_unset=1
fi

printf 'pg_dump argv_secret=%s password_scoped=%s secret_inputs_unset=%s internal_passwords_unset=%s dsn_preserved=%s\n' \
  "$argv_secret" "$password_scoped" "$secret_inputs_unset" "$internal_passwords_unset" "$dsn_preserved" >> "$UNIROUTE_TEST_STATE_DIR/calls"
printf '%s\n' '-- synthetic rehearsal dump'
FAKE_PG_DUMP

cat > "$FAKE_BIN/psql" <<'FAKE_PSQL'
#!/usr/bin/env bash
set -euo pipefail

argv_secret=0
for arg in "$@"; do
  case "$arg" in
    *source-password-marker*|*restore-password-marker*) argv_secret=1 ;;
  esac
done

dsn_preserved=0
if [[ "$#" -ge 2 && "$1" == "$UNIROUTE_TEST_EXPECTED_RESTORE_DSN" ]]; then
  dsn_preserved=1
fi

password_scoped=0
if [[ "${PGPASSWORD:-}" == "restore-password-marker" ]]; then
  password_scoped=1
fi

secret_inputs_unset=0
if [[ -z "${UNIROUTE_BACKUP_SOURCE_PASSWORD+x}" && -z "${UNIROUTE_RESTORE_REHEARSAL_PASSWORD+x}" ]]; then
  secret_inputs_unset=1
fi

internal_passwords_unset=0
if [[ -z "${SOURCE_PASSWORD+x}" && -z "${RESTORE_PASSWORD+x}" ]]; then
  internal_passwords_unset=1
fi

printf 'psql argv_secret=%s password_scoped=%s secret_inputs_unset=%s internal_passwords_unset=%s dsn_preserved=%s\n' \
  "$argv_secret" "$password_scoped" "$secret_inputs_unset" "$internal_passwords_unset" "$dsn_preserved" >> "$UNIROUTE_TEST_STATE_DIR/calls"

case " $* " in
  *source-budget-restore-rebase.sql*)
    [[ -f "${@: -1}" ]] || exit 42
    count="$(wc -l < "$UNIROUTE_TEST_STATE_DIR/calls" | tr -d '[:space:]')"
    [[ "$count" == 3 ]] || exit 43
    printf '%s\n' rebase-before-validation > "$UNIROUTE_TEST_STATE_DIR/rebase"
    ;;
  *schema_migrations*) printf '%s\n' 1 ;;
  *to_regclass*) printf '%s\n' 6 ;;
esac
FAKE_PSQL

chmod 0700 "$FAKE_BIN/mktemp" "$FAKE_BIN/pg_dump" "$FAKE_BIN/psql"

record_failure() {
  printf 'not ok - %s\n' "$1" >&2
  FAILURES=$((FAILURES + 1))
}

new_case() {
  local name="$1"
  local case_dir="$TEST_ROOT/$name"
  mkdir -m 0700 "$case_dir" "$case_dir/state"
  mkdir -m 0755 "$case_dir/tmp"
  printf '%s\n' "$case_dir"
}

invoke_rehearsal() {
  local case_dir="$1"
  local source_dsn="$2"
  local restore_dsn="$3"
  local confirm="$4"
  local dump_override="${5:-__unset__}"
  local caller_umask="${6:-022}"
  local bash_xtrace="${7:-0}"

  (
    umask "$caller_umask"
    export PATH="$FAKE_BIN:$PATH"
    export TMPDIR="$case_dir/tmp"
    export UNIROUTE_TEST_STATE_DIR="$case_dir/state"
    export UNIROUTE_TEST_EXPECTED_SOURCE_DSN="$source_dsn"
    export UNIROUTE_TEST_EXPECTED_RESTORE_DSN="$restore_dsn"
    export UNIROUTE_BACKUP_SOURCE_DSN="$source_dsn"
    export UNIROUTE_RESTORE_REHEARSAL_DSN="$restore_dsn"
    export UNIROUTE_BACKUP_SOURCE_PASSWORD=source-password-marker
    export UNIROUTE_RESTORE_REHEARSAL_PASSWORD=restore-password-marker
    export SOURCE_PASSWORD=preexisting-exported-source-value
    export RESTORE_PASSWORD=preexisting-exported-restore-value
    export UNIROUTE_BACKUP_RESTORE_CONFIRM="$confirm"
    export PGPASSWORD=inherited-password-marker
    if [[ "$dump_override" == "__unset__" ]]; then
      unset UNIROUTE_BACKUP_DUMP_FILE
    else
      export UNIROUTE_BACKUP_DUMP_FILE="$dump_override"
    fi
    if [[ "$bash_xtrace" == 1 ]]; then
      bash -x "$REHEARSAL_SCRIPT"
    else
      bash "$REHEARSAL_SCRIPT"
    fi
  ) > "$case_dir/stdout" 2> "$case_dir/stderr"
}

assert_no_calls() {
  local case_dir="$1"
  if [[ -s "$case_dir/state/calls" ]]; then
    record_failure "$(basename "$case_dir") invoked a database client before rejection"
  fi
  if [[ -s "$case_dir/state/mktemp_calls" ]]; then
    record_failure "$(basename "$case_dir") created a dump workspace before rejection"
  fi
}

assert_secret_not_logged() {
  local case_dir="$1"
  if grep -Eq 'source-password-marker|restore-password-marker|inherited-password-marker' \
    "$case_dir/stdout" "$case_dir/stderr"; then
    record_failure "$(basename "$case_dir") printed a password marker"
  fi
}

expect_rejected() {
  local case_dir="$1"
  local source_dsn="$2"
  local restore_dsn="$3"
  local confirm="$4"
  local expected_error="$5"
  local dump_override="${6:-__unset__}"

  if invoke_rehearsal "$case_dir" "$source_dsn" "$restore_dsn" "$confirm" "$dump_override"; then
    record_failure "$(basename "$case_dir") was accepted"
  fi
  if ! grep -Fq "$expected_error" "$case_dir/stderr"; then
    record_failure "$(basename "$case_dir") did not report the expected rejection"
  fi
  assert_no_calls "$case_dir"
  assert_secret_not_logged "$case_dir"
}

file_mode() {
  local path="$1"
  if stat -f '%Lp' "$path" >/dev/null 2>&1; then
    stat -f '%Lp' "$path"
  else
    stat -c '%a' "$path"
  fi
}

case_dir="$(new_case source-uri-password)"
expect_rejected \
  "$case_dir" \
  'postgresql://backup-user:source-password-marker@db.invalid/source?sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?sslmode=require' \
  non-production-restore-target \
  'source DSN must not contain a password'

case_dir="$(new_case source-keyword-password)"
expect_rejected \
  "$case_dir" \
  'host=db.invalid dbname=source user=backup password=source-password-marker sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?sslmode=require' \
  non-production-restore-target \
  'source DSN must not contain a password'

case_dir="$(new_case source-nested-uri-password)"
expect_rejected \
  "$case_dir" \
  'dbname=postgresql://backup-user:source-password-marker@db.invalid/source?sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?sslmode=require' \
  non-production-restore-target \
  'source DSN must not contain a password'

case_dir="$(new_case restore-query-password)"
expect_rejected \
  "$case_dir" \
  'postgresql://backup-user@db.invalid/source?sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?password=restore-password-marker' \
  non-production-restore-target \
  'restore DSN must not contain a password'

case_dir="$(new_case restore-encoded-query-password)"
expect_rejected \
  "$case_dir" \
  'postgresql://backup-user@db.invalid/source?sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?pass%77ord=restore-password-marker' \
  non-production-restore-target \
  'restore DSN must not contain a password'

case_dir="$(new_case restore-uppercase-query-password)"
expect_rejected \
  "$case_dir" \
  'postgresql://backup-user@db.invalid/source?sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?PASSWORD=restore-password-marker' \
  non-production-restore-target \
  'restore DSN must not contain a password'

case_dir="$(new_case dump-override)"
printf '%s\n' 'synthetic sentinel must remain unchanged' > "$case_dir/sentinel"
ln -s "$case_dir/sentinel" "$case_dir/synthetic-existing-destination"
expect_rejected \
  "$case_dir" \
  'postgresql://backup-user@db.invalid/source?sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?sslmode=require' \
  non-production-restore-target \
  'UNIROUTE_BACKUP_DUMP_FILE is not supported' \
  "$case_dir/synthetic-existing-destination"
if [[ "$(cat "$case_dir/sentinel")" != 'synthetic sentinel must remain unchanged' ]]; then
  record_failure 'dump override rejection modified the synthetic symlink target'
fi

case_dir="$(new_case confirmation)"
expect_rejected \
  "$case_dir" \
  'postgresql://backup-user@db.invalid/source?sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?sslmode=require' \
  not-confirmed \
  'set UNIROUTE_BACKUP_RESTORE_CONFIRM=non-production-restore-target'

case_dir="$(new_case same-target)"
expect_rejected \
  "$case_dir" \
  'postgresql://backup-user@db.invalid/source?sslmode=require' \
  'postgresql://backup-user@db.invalid/source?sslmode=require' \
  non-production-restore-target \
  'source and restore DSN must be different'

case_dir="$(new_case xtrace)"
if invoke_rehearsal \
  "$case_dir" \
  'postgresql://backup-user@db.invalid/source?sslmode=require' \
  'postgresql://restore-user@db.invalid/restore?sslmode=require' \
  non-production-restore-target \
  __unset__ \
  022 \
  1; then
  record_failure 'xtrace-enabled invocation was accepted'
fi
if ! grep -Fq 'shell xtrace must be disabled before running the backup rehearsal' "$case_dir/stderr"; then
  record_failure 'xtrace-enabled invocation did not report the expected rejection'
fi
assert_no_calls "$case_dir"
assert_secret_not_logged "$case_dir"

for caller_umask in 022 002 077; do
  case_dir="$(new_case "success-$caller_umask")"
  if ! invoke_rehearsal \
    "$case_dir" \
    'postgresql://backup-user@db.invalid/source?sslmode=require' \
    'host=db.invalid dbname=restore user=restore-user sslmode=require' \
    non-production-restore-target \
    __unset__ \
    "$caller_umask" \
    0; then
    record_failure "password-free rehearsal with scoped passwords failed under umask $caller_umask"
  else
    call_count="$(wc -l < "$case_dir/state/calls" | tr -d '[:space:]')"
    if [[ "$call_count" != 5 ]]; then
      record_failure "expected five database client calls, got $call_count"
    fi
    if [[ "$(cat "$case_dir/state/rebase" 2>/dev/null)" != "rebase-before-validation" ]]; then
      record_failure 'coverage rebase must run after restore and before validation'
    fi
    if grep -Evq 'argv_secret=0 password_scoped=1 secret_inputs_unset=1 internal_passwords_unset=1 dsn_preserved=1$' "$case_dir/state/calls"; then
      record_failure 'database client arguments or environment did not satisfy the password boundary'
    fi
    mktemp_call_count=0
    if [[ -f "$case_dir/state/mktemp_calls" ]]; then
      mktemp_call_count="$(wc -l < "$case_dir/state/mktemp_calls" | tr -d '[:space:]')"
    fi
    if [[ "$mktemp_call_count" != 2 ]]; then
      record_failure 'expected one private workspace and one dump-file mktemp call'
    fi
    assert_secret_not_logged "$case_dir"

    dump_file="$(sed -n 's/^PASS .* dump_file=//p' "$case_dir/stdout")"
    if [[ -z "$dump_file" || ! -f "$dump_file" || -L "$dump_file" ]]; then
      record_failure 'retained dump is not a regular file'
    else
      workspace="$(dirname "$dump_file")"
      case "$workspace" in
        "$case_dir/tmp"/uniroute-prd2-backup-rehearsal.*) ;;
        *) record_failure 'dump workspace is not a unique child of TMPDIR' ;;
      esac
      if [[ "$(file_mode "$workspace")" != 700 ]]; then
        record_failure "dump workspace mode is not 0700 under caller umask $caller_umask"
      fi
      if [[ "$(file_mode "$dump_file")" != 600 ]]; then
        record_failure "dump file mode is not 0600 under caller umask $caller_umask"
      fi
      if [[ "$(cat "$dump_file")" != '-- synthetic rehearsal dump' ]]; then
        record_failure 'retained dump did not contain the synthetic producer output'
      fi
    fi
  fi
done

if [[ "$FAILURES" -ne 0 ]]; then
  printf 'FAIL prd2 backup security regression failures=%s artifacts=%s\n' "$FAILURES" "$TEST_ROOT" >&2
  exit 1
fi

printf 'PASS prd2 backup security regression artifacts=%s\n' "$TEST_ROOT"
