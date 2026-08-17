#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/release-worktree-retention-maintenance.sh [--apply] \
    --release-root DIR --keep-file FILE [--min-age-days N] [--report-out FILE]

Without --apply this is a read-only report. With --apply, only rows frozen as
candidate by the report are revalidated. Every candidate must pass a complete
preflight before deletion begins, then each is checked again immediately before
removal. Linked worktrees use `git worktree remove`; self-contained legacy
repositories use a bounded tree deletion. Drift, dirty state, a running
reference, Git alternates or keep-file changes stop the command. No timer is created.
EOF
}

apply=false
release_root=""
keep_file=""
min_age_days=14
report_out=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply) apply=true; shift ;;
    --release-root) [[ $# -ge 2 ]] || { usage >&2; exit 2; }; release_root="$2"; shift 2 ;;
    --keep-file) [[ $# -ge 2 ]] || { usage >&2; exit 2; }; keep_file="$2"; shift 2 ;;
    --min-age-days) [[ $# -ge 2 ]] || { usage >&2; exit 2; }; min_age_days="$2"; shift 2 ;;
    --report-out) [[ $# -ge 2 ]] || { usage >&2; exit 2; }; report_out="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "release retention maintenance: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
report_script="$script_dir/release-worktree-retention-report.sh"
common_script="$script_dir/release-retention-common.sh"
[[ -x "$report_script" ]] || {
  echo "release retention maintenance: report helper is not executable" >&2
  exit 2
}
[[ -r "$common_script" ]] || {
  echo "release retention maintenance: common helper is unavailable" >&2
  exit 2
}
source "$common_script"
command -v flock >/dev/null 2>&1 || {
  echo "release retention maintenance: flock is required" >&2
  exit 2
}
lifecycle_lock="${UNIROUTER_DOCKER_LIFECYCLE_LOCK:-/run/lock/unirouter-docker-lifecycle.lock}"
lock_parent="$(dirname "$lifecycle_lock")"
[[ -d "$lock_parent" && ! -L "$lock_parent" && ! -L "$lifecycle_lock" ]] || {
  echo "release retention maintenance: lifecycle lock parent must be a real directory" >&2
  exit 2
}
exec 9>"$lifecycle_lock"
if ! flock -n 9; then
  echo "release retention maintenance: another build or maintenance run holds the lifecycle lock" >&2
  exit 1
fi

report_dir="$(mktemp -d "${TMPDIR:-/tmp}/unirouter-release-maintenance.XXXXXX")"
cleanup() {
  rm -rf -- "$report_dir"
}
trap cleanup EXIT
frozen_report="$report_dir/report.tsv"

"$report_script" --release-root "$release_root" --keep-file "$keep_file" --min-age-days "$min_age_days" >"$frozen_report"
cat "$frozen_report"
if [[ -n "$report_out" ]]; then
  report_parent="$(dirname "$report_out")"
  [[ -d "$report_parent" && ! -L "$report_parent" ]] || {
    echo "release retention maintenance: report output parent must be a real directory" >&2
    exit 2
  }
  report_stage="${report_out}.tmp.$$"
  install -m 0644 "$frozen_report" "$report_stage"
  mv -f -- "$report_stage" "$report_out"
fi

candidate_count="$(awk -F '\t' '$1 == "candidate" { count++ } END { print count + 0 }' "$frozen_report")"
if [[ "$apply" != true ]]; then
  printf 'APPLY\tperformed=false\tcandidates=%s\n' "$candidate_count"
  exit 0
fi

release_root="$(realpath "$release_root")"
current_keep_revisions="$report_dir/current-keep-revisions"
current_running_revisions="$report_dir/current-running-revisions"
current_running_paths="$report_dir/current-running-paths"
build_current_keep_revisions() {
  : >"$current_keep_revisions"
  while IFS= read -r raw_line || [[ -n "$raw_line" ]]; do
    revision="$(printf '%s' "$raw_line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    [[ -n "$revision" && "${revision:0:1}" != "#" ]] || continue
    [[ "$revision" =~ ^[0-9a-f]{40}$ ]] || {
      echo "release retention maintenance: keep entry must be a complete lowercase commit: $revision" >&2
      exit 2
    }
    printf '%s\n' "$revision" >>"$current_keep_revisions"
  done <"$keep_file"
  sort -u -o "$current_keep_revisions" "$current_keep_revisions"
  [[ -s "$current_keep_revisions" ]] || {
    echo "release retention maintenance: keep file contains no protected revisions" >&2
    exit 2
  }
}

refresh_current_protections() {
  release_collect_running_references "$current_running_revisions" "$current_running_paths" "$release_root"
  build_current_keep_revisions
}

validate_candidate() {
  local frozen_mtime="$1"
  local frozen_head="$2"
  local path="$3"
  local frozen_detail="$4"
  local current_path current_layout common_git_dir=""
  if [[ -d "$path" && ! -L "$path" ]]; then
    current_path="$(realpath "$path")"
  else
    current_path=""
  fi
  [[ -n "$current_path" && "$(dirname "$current_path")" == "$release_root" && "$(basename "$current_path")" == "$frozen_head" ]] || {
    echo "release retention maintenance: candidate path drift: $path" >&2
    exit 1
  }
  [[ "$(release_path_mtime "$current_path")" == "$frozen_mtime" ]] || {
    echo "release retention maintenance: candidate mtime drift: $current_path" >&2
    exit 1
  }
  [[ "$(git -C "$current_path" rev-parse --verify HEAD)" == "$frozen_head" ]] || {
    echo "release retention maintenance: candidate HEAD drift: $current_path" >&2
    exit 1
  }
  [[ -z "$(git -C "$current_path" symbolic-ref -q --short HEAD 2>/dev/null || true)" ]] || {
    echo "release retention maintenance: candidate is now attached: $current_path" >&2
    exit 1
  }
  [[ -z "$(git -C "$current_path" status --porcelain=v1 --untracked-files=all)" ]] || {
    echo "release retention maintenance: candidate is now dirty: $current_path" >&2
    exit 1
  }
  ! grep -Fxq "$frozen_head" "$current_keep_revisions" || {
    echo "release retention maintenance: candidate is now retained: $current_path" >&2
    exit 1
  }
  ! grep -Fxq "$frozen_head" "$current_running_revisions" || {
    echo "release retention maintenance: candidate revision is now used by a running container: $current_path" >&2
    exit 1
  }
  ! grep -Fxq "$current_path" "$current_running_paths" || {
    echo "release retention maintenance: candidate path is now mounted by a running container: $current_path" >&2
    exit 1
  }

  current_layout="$(release_git_layout "$current_path")" || {
    echo "release retention maintenance: candidate Git layout is unsupported: $current_path" >&2
    exit 1
  }
  [[ "$frozen_detail" == "clean-detached-$current_layout" ]] || {
    echo "release retention maintenance: candidate Git layout drift: $current_path" >&2
    exit 1
  }
  if [[ "$current_layout" == "linked-worktree" ]]; then
    common_git_dir="$(release_git_metadata_path "$current_path" --git-common-dir)"
    [[ -d "$common_git_dir" && ! -L "$common_git_dir" && "$common_git_dir" != "$current_path/.git" ]] || {
      echo "release retention maintenance: linked worktree common Git directory unavailable: $current_path" >&2
      exit 1
    }
  else
    release_standalone_is_single_self_contained_worktree "$current_path" "$frozen_head" || {
      echo "release retention maintenance: standalone repository is not self-contained: $current_path" >&2
      exit 1
    }
    ! release_root_has_nonempty_alternates "$release_root" || {
      echo "release retention maintenance: Git alternates require manual dependency review: $current_path" >&2
      exit 1
    }
  fi

  validated_path="$current_path"
  validated_layout="$current_layout"
  validated_common_git_dir="$common_git_dir"
}

validated_candidates="$report_dir/validated-candidates.tsv"
: >"$validated_candidates"
refresh_current_protections
while IFS=$'\t' read -r status _age _size frozen_mtime frozen_head path frozen_detail; do
  [[ "$status" == "candidate" ]] || continue
  validate_candidate "$frozen_mtime" "$frozen_head" "$path" "$frozen_detail"
  printf '%s\t%s\t%s\t%s\n' "$frozen_mtime" "$frozen_head" "$path" "$frozen_detail" >>"$validated_candidates"
done <"$frozen_report"

removed=0
while IFS=$'\t' read -r frozen_mtime frozen_head path frozen_detail; do
  refresh_current_protections
  validate_candidate "$frozen_mtime" "$frozen_head" "$path" "$frozen_detail"
  if [[ "$validated_layout" == "linked-worktree" ]]; then
    git --git-dir="$validated_common_git_dir" worktree remove -- "$validated_path"
  else
    find "$validated_path" -depth -delete
  fi
  [[ ! -e "$validated_path" && ! -L "$validated_path" ]] || {
    echo "release retention maintenance: checkout removal did not complete: $validated_path" >&2
    exit 1
  }
  removed=$((removed + 1))
  printf 'REMOVED\t%s\t%s\n' "$frozen_head" "$validated_path"
done <"$validated_candidates"

printf 'APPLY\tperformed=true\tcandidates=%s\tremoved=%d\n' "$candidate_count" "$removed"
