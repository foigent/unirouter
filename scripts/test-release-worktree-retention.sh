#!/usr/bin/env bash
set -euo pipefail

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
git -C "$source_repo" init -q
git -C "$source_repo" config user.email test@example.invalid
git -C "$source_repo" config user.name "Retention Test"

for index in 1 2 3 4; do
  printf 'fixture-%s\n' "$index" >"$source_repo/fixture.txt"
  git -C "$source_repo" add fixture.txt
  git -C "$source_repo" commit -qm "fixture $index"
  eval "revision_$index=$(git -C "$source_repo" rev-parse HEAD)"
done

for revision in "$revision_1" "$revision_2" "$revision_3" "$revision_4"; do
  git -C "$source_repo" worktree add --detach -q "$release_root/$revision" "$revision"
done
printf 'dirty\n' >"$release_root/$revision_4/untracked.txt"
if old_timestamp="$(date -u -d '20 days ago' +%Y%m%d%H%M.%S 2>/dev/null)"; then
  :
else
  old_timestamp="$(date -u -v-20d +%Y%m%d%H%M.%S)"
fi
touch -t "$old_timestamp" "$release_root/$revision_1" "$release_root/$revision_2" "$release_root/$revision_3" "$release_root/$revision_4"
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

report="$fixture/report.tsv"
"$repo_root/scripts/release-worktree-retention-report.sh" \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >"$report"
grep -F $'protected-keep\t20' "$report" | grep -F "$revision_1" >/dev/null
grep -F $'protected-running\t20' "$report" | grep -F "$revision_2" >/dev/null
grep -F $'candidate\t20' "$report" | grep -F "$revision_3" >/dev/null
grep -F $'review-required\t20' "$report" | grep -F "$revision_4" | grep -F 'dirty-worktree' >/dev/null
grep -F $'SUMMARY\tcandidates=1' "$report" >/dev/null

"$repo_root/scripts/release-worktree-retention-maintenance.sh" \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >/dev/null
[[ -d "$release_root/$revision_3" ]]

export FAKE_KEEP_FILE="$fixture/keep"
export FAKE_KEEP_APPEND_REVISION="$revision_3"
if "$repo_root/scripts/release-worktree-retention-maintenance.sh" --apply \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >/dev/null 2>&1; then
  echo "apply ignored a keep-file change after its frozen report" >&2
  exit 1
fi
[[ -d "$release_root/$revision_3" ]]
printf '%s\n' "$revision_1" >"$fixture/keep"
unset FAKE_KEEP_APPEND_REVISION FAKE_KEEP_FILE

"$repo_root/scripts/release-worktree-retention-maintenance.sh" --apply \
  --release-root "$release_root" --keep-file "$fixture/keep" --min-age-days 14 >/dev/null
[[ ! -e "$release_root/$revision_3" ]]
[[ -d "$release_root/$revision_1" && -d "$release_root/$revision_2" && -d "$release_root/$revision_4" ]]
git -C "$source_repo" worktree list --porcelain | grep -F "$revision_3" && {
  echo "removed worktree remains registered" >&2
  exit 1
}

echo "release worktree retention tests: PASS"
