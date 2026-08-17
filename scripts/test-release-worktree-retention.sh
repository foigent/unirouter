#!/usr/bin/env bash
set -euo pipefail

test_stage="setup"
test_failed() {
  local exit_code="$?"
  local line="$1"
  printf 'release worktree retention tests: FAIL stage=%s line=%s exit=%s\n' \
    "$test_stage" "$line" "$exit_code" >&2
  exit "$exit_code"
}
trap 'test_failed "$LINENO"' ERR

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/unirouter-release-retention-test.XXXXXX")"
cleanup() {
  rm -rf -- "$fixture"
}
trap cleanup EXIT

source_repo="$fixture/source"
release_root="$fixture/releases"
fake_bin="$fixture/bin"
mkdir -p "$source_repo" "$release_root" "$fake_bin"
git -C "$source_repo" init --object-format=sha1 -q
git -C "$source_repo" config user.email test@example.invalid
git -C "$source_repo" config user.name "Retention Test"

for index in 1 2 3 4 5 6; do
  printf 'fixture-%s\n' "$index" >"$source_repo/fixture.txt"
  git -C "$source_repo" add fixture.txt
  git -C "$source_repo" commit -qm "fixture $index"
  eval "revision_$index=$(git -C "$source_repo" rev-parse HEAD)"
done

for revision in "$revision_1" "$revision_2" "$revision_3" "$revision_4" "$revision_6"; do
  git -C "$source_repo" worktree add --detach -q "$release_root/$revision" "$revision"
done
git clone -q --no-hardlinks "$source_repo" "$release_root/$revision_5"
git -C "$release_root/$revision_5" checkout --detach -q "$revision_5"
printf 'dirty\n' >"$release_root/$revision_4/untracked.txt"
if old_timestamp="$(date -u -d '20 days ago' +%Y%m%d%H%M.%S 2>/dev/null)"; then
  :
else
  old_timestamp="$(date -u -v-20d +%Y%m%d%H%M.%S)"
fi
touch -t "$old_timestamp" \
  "$release_root/$revision_1" "$release_root/$revision_2" "$release_root/$revision_3" \
  "$release_root/$revision_4" "$release_root/$revision_5" "$release_root/$revision_6"
printf '%s\n' "$revision_1" >"$fixture/keep"

cat >"$fake_bin/docker" <<EOF
#!/usr/bin/env bash
set -euo pipefail
case "\$*" in
  info) exit 0 ;;
  "container ls -q")
    if [[ -n "\${FAKE_KEEP_APPEND_REVISION:-}" ]]; then
      printf '  %s  \n' "\$FAKE_KEEP_APPEND_REVISION" >>"\${FAKE_KEEP_FILE:?}"
    fi
    printf 'running-container\n'
    ;;
  "container inspect --format {{ index .Config.Labels \"org.opencontainers.image.revision\" }} running-container") printf '%s\n' "$revision_2" ;;
  "container inspect --format {{range .Mounts}}{{println .Source}}{{end}} running-container")
    [[ -z "\${FAKE_FAIL_MOUNT_INSPECT:-}" ]] || exit 7
    printf '%s\n' "\${FAKE_MOUNT_SOURCE:-}"
    ;;
  *) echo "unexpected fake docker invocation: \$*" >&2; exit 9 ;;
esac
EOF
chmod +x "$fake_bin/docker"
cat >"$fake_bin/flock" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$fake_bin/flock"
export PATH="$fake_bin:$PATH"
export UNIROUTER_DOCKER_LIFECYCLE_LOCK="$fixture/docker-lifecycle.lock"
export FAKE_MOUNT_SOURCE="$release_root/$revision_6"

test_stage="running-container-inspect-fail-closed"
export FAKE_FAIL_MOUNT_INSPECT=true
if "$repo_root/scripts/release-worktree-retention-report.sh" \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >/dev/null 2>&1; then
  echo "report ignored a running-container mount inspection failure" >&2
  exit 1
fi
unset FAKE_FAIL_MOUNT_INSPECT

test_stage="standalone-alternates-fail-closed"
printf '%s/.git/objects\n' "$source_repo" >"$release_root/$revision_5/.git/objects/info/alternates"
alternate_report="$fixture/alternate-report.tsv"
"$repo_root/scripts/release-worktree-retention-report.sh" \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >"$alternate_report"
grep -F $'review-required\t20' "$alternate_report" | grep -F "$revision_5" | \
  grep -F 'standalone-repository-not-self-contained' >/dev/null
rm -f -- "$release_root/$revision_5/.git/objects/info/alternates"
touch -t "$old_timestamp" "$release_root/$revision_5"

test_stage="topology-and-running-reference-report"
report="$fixture/report.tsv"
"$repo_root/scripts/release-worktree-retention-report.sh" \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >"$report"
grep -F $'protected-keep\t20' "$report" | grep -F "$revision_1" >/dev/null
grep -F $'protected-running\t20' "$report" | grep -F "$revision_2" >/dev/null
grep -F $'candidate\t20' "$report" | grep -F "$revision_3" | grep -F 'clean-detached-linked-worktree' >/dev/null
grep -F $'candidate\t20' "$report" | grep -F "$revision_5" | grep -F 'clean-detached-standalone-repository' >/dev/null
grep -F $'protected-running\t20' "$report" | grep -F "$revision_6" | grep -F 'running-container-mount-source' >/dev/null
grep -F $'review-required\t20' "$report" | grep -F "$revision_4" | grep -F 'dirty-worktree' >/dev/null
grep -F $'SUMMARY\tcandidates=2' "$report" >/dev/null

test_stage="dry-run"
"$repo_root/scripts/release-worktree-retention-maintenance.sh" \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >/dev/null
[[ -d "$release_root/$revision_3" ]]
[[ -d "$release_root/$revision_5" ]]

test_stage="keep-file-drift-fail-closed"
export FAKE_KEEP_FILE="$fixture/keep"
export FAKE_KEEP_APPEND_REVISION="$revision_3"
if "$repo_root/scripts/release-worktree-retention-maintenance.sh" --apply \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >/dev/null 2>&1; then
  echo "apply ignored a keep-file change after its frozen report" >&2
  exit 1
fi
[[ -d "$release_root/$revision_3" ]]
[[ -d "$release_root/$revision_5" ]]
printf '%s\n' "$revision_1" >"$fixture/keep"
unset FAKE_KEEP_APPEND_REVISION FAKE_KEEP_FILE

test_stage="guarded-apply"
"$repo_root/scripts/release-worktree-retention-maintenance.sh" --apply \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >/dev/null
[[ ! -e "$release_root/$revision_3" ]]
[[ ! -e "$release_root/$revision_5" ]]
[[ -d "$release_root/$revision_1" && -d "$release_root/$revision_2" && -d "$release_root/$revision_4" && -d "$release_root/$revision_6" ]]
test_stage="post-apply-registration"
git -C "$source_repo" worktree list --porcelain | grep -F "$revision_3" && {
  echo "removed worktree remains registered" >&2
  exit 1
}

echo "release worktree retention tests: PASS"
