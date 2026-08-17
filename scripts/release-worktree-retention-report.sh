#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/release-worktree-retention-report.sh \
    --release-root DIR --keep-file FILE [--min-age-days N]

Builds a read-only report for commit-addressed deployment worktrees. FILE must
contain one complete 40-character Git commit per line. Running container image
revision labels and the keep file are protected. Only clean, detached, direct
child worktrees whose directory name equals HEAD and whose age is at least N
days are candidates. This command never removes a worktree.
EOF
}

release_root=""
keep_file=""
min_age_days=14

while [[ $# -gt 0 ]]; do
  case "$1" in
    --release-root)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      release_root="$2"
      shift 2
      ;;
    --keep-file)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      keep_file="$2"
      shift 2
      ;;
    --min-age-days)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      min_age_days="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "release retention report: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$release_root" || ! -d "$release_root" || -L "$release_root" ]]; then
  echo "release retention report: --release-root must name a real directory" >&2
  exit 2
fi
if [[ -z "$keep_file" || ! -r "$keep_file" ]]; then
  echo "release retention report: --keep-file must name a readable file" >&2
  exit 2
fi
if [[ ! "$min_age_days" =~ ^[0-9]+$ ]] || (( min_age_days < 1 || min_age_days > 3650 )); then
  echo "release retention report: --min-age-days must be an integer from 1 to 3650" >&2
  exit 2
fi
for command_name in docker git realpath stat find sort du awk date; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "release retention report: $command_name is required" >&2
    exit 2
  }
done
docker info >/dev/null 2>&1 || {
  echo "release retention report: Docker daemon is unavailable" >&2
  exit 2
}

release_root="$(realpath "$release_root")"
case "$release_root" in
  /|/opt|/opt/unirouter)
    echo "release retention report: release root is too broad: $release_root" >&2
    exit 2
    ;;
esac

path_mtime() {
  if stat -c %Y "$1" >/dev/null 2>&1; then
    stat -c %Y "$1"
  else
    stat -f %m "$1"
  fi
}

report_dir="$(mktemp -d "${TMPDIR:-/tmp}/unirouter-release-report.XXXXXX")"
cleanup() {
  rm -rf -- "$report_dir"
}
trap cleanup EXIT

keep_revisions="$report_dir/keep-revisions"
running_revisions="$report_dir/running-revisions"
: >"$keep_revisions"
: >"$running_revisions"

while IFS= read -r raw_line || [[ -n "$raw_line" ]]; do
  revision="$(printf '%s' "$raw_line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
  [[ -n "$revision" && "${revision:0:1}" != "#" ]] || continue
  if [[ ! "$revision" =~ ^[0-9a-f]{40}$ ]]; then
    echo "release retention report: keep entry must be a complete lowercase commit: $revision" >&2
    exit 2
  fi
  printf '%s\n' "$revision" >>"$keep_revisions"
done <"$keep_file"
sort -u -o "$keep_revisions" "$keep_revisions"
if [[ ! -s "$keep_revisions" ]]; then
  echo "release retention report: keep file contains no protected revisions" >&2
  exit 2
fi

while IFS= read -r container_id; do
  [[ -n "$container_id" ]] || continue
  revision="$(docker container inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$container_id" 2>/dev/null || true)"
  [[ "$revision" =~ ^[0-9a-f]{40}$ ]] && printf '%s\n' "$revision" >>"$running_revisions"
done < <(docker container ls -q)
sort -u -o "$running_revisions" "$running_revisions"

now_epoch="$(date +%s)"
minimum_age_seconds=$((min_age_days * 86400))
candidate_count=0
candidate_bytes=0
protected_keep_count=0
protected_running_count=0
young_count=0
review_count=0

printf 'META\tgenerated_at=%s\trelease_root=%s\tmin_age_days=%d\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$release_root" "$min_age_days"
printf 'STATUS\tAGE_DAYS\tDISK_MIB\tMTIME_EPOCH\tHEAD\tPATH\tDETAIL\n'
while IFS= read -r -d '' path; do
  status=""
  detail=""
  head="-"
  age_days=0
  mtime_epoch="$(path_mtime "$path" 2>/dev/null || printf '0')"
  size_kib="$(du -sk "$path" 2>/dev/null | awk '{print $1}')"
  size_bytes=$((${size_kib:-0} * 1024))
  size_mib="$(awk -v bytes="$size_bytes" 'BEGIN { printf "%.1f", bytes / 1048576 }')"
  if (( mtime_epoch > 0 && now_epoch >= mtime_epoch )); then
    age_days=$(((now_epoch - mtime_epoch) / 86400))
  fi

  name="$(basename "$path")"
  if [[ -L "$path" || ! -d "$path" ]]; then
    status="review-required"
    detail="non-directory-or-symlink"
  elif [[ ! "$name" =~ ^[0-9a-f]{40}$ ]]; then
    status="review-required"
    detail="noncanonical-directory-name"
  elif ! head="$(git -C "$path" rev-parse --verify HEAD 2>/dev/null)"; then
    status="review-required"
    detail="not-a-git-worktree"
    head="-"
  elif [[ "$(git -C "$path" rev-parse --show-toplevel 2>/dev/null || true)" != "$path" ]]; then
    status="review-required"
    detail="worktree-root-mismatch"
  elif [[ "$head" != "$name" ]]; then
    status="review-required"
    detail="directory-head-mismatch"
  elif [[ "$(git -C "$path" symbolic-ref -q --short HEAD 2>/dev/null || true)" != "" ]]; then
    status="review-required"
    detail="attached-worktree"
  elif [[ -n "$(git -C "$path" status --porcelain=v1 --untracked-files=all)" ]]; then
    status="review-required"
    detail="dirty-worktree"
  elif grep -Fxq "$head" "$running_revisions"; then
    status="protected-running"
    detail="running-container-revision"
    protected_running_count=$((protected_running_count + 1))
  elif grep -Fxq "$head" "$keep_revisions"; then
    status="protected-keep"
    detail="retention-keep-file"
    protected_keep_count=$((protected_keep_count + 1))
  elif (( now_epoch - mtime_epoch < minimum_age_seconds )); then
    status="young"
    detail="below-minimum-age"
    young_count=$((young_count + 1))
  else
    status="candidate"
    detail="clean-detached-unreferenced"
    candidate_count=$((candidate_count + 1))
    candidate_bytes=$((candidate_bytes + size_bytes))
  fi

  [[ "$status" != "review-required" ]] || review_count=$((review_count + 1))
  printf '%s\t%d\t%s\t%s\t%s\t%s\t%s\n' "$status" "$age_days" "$size_mib" "$mtime_epoch" "$head" "$path" "$detail"
done < <(find "$release_root" -mindepth 1 -maxdepth 1 -print0)

candidate_mib="$(awk -v bytes="$candidate_bytes" 'BEGIN { printf "%.1f", bytes / 1048576 }')"
printf 'SUMMARY\tcandidates=%d\tcandidate_disk_mib=%s\tprotected_running=%d\tprotected_keep=%d\tyoung=%d\treview_required=%d\n' \
  "$candidate_count" "$candidate_mib" "$protected_running_count" "$protected_keep_count" "$young_count" "$review_count"
