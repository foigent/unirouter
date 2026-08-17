#!/usr/bin/env bash

release_path_mtime() {
  if stat -c %Y "$1" >/dev/null 2>&1; then
    stat -c %Y "$1"
  else
    stat -f %m "$1"
  fi
}

release_git_metadata_path() {
  local path="$1"
  local selector="$2"
  local raw
  raw="$(git -C "$path" rev-parse --path-format=absolute "$selector" 2>/dev/null)" || return 1
  case "$raw" in
    /*) realpath "$raw" 2>/dev/null ;;
    *) realpath "$path/$raw" 2>/dev/null ;;
  esac
}

release_git_layout() {
  local path="$1"
  local git_marker="$path/.git"
  local git_dir common_git_dir
  git_dir="$(release_git_metadata_path "$path" --git-dir)" || return 1
  common_git_dir="$(release_git_metadata_path "$path" --git-common-dir)" || return 1

  if [[ -f "$git_marker" && ! -L "$git_marker" && "$common_git_dir" != "$git_marker" ]]; then
    case "$git_dir" in
      "$common_git_dir"/worktrees/*)
        printf '%s\n' "linked-worktree"
        return 0
        ;;
    esac
  fi
  if [[ -d "$git_marker" && ! -L "$git_marker" && "$git_dir" == "$git_marker" && "$common_git_dir" == "$git_marker" ]]; then
    printf '%s\n' "standalone-repository"
    return 0
  fi
  return 1
}

release_standalone_is_single_self_contained_worktree() {
  local path="$1"
  local expected_head="$2"
  local listing
  [[ -d "$path/.git" && ! -L "$path/.git" && ! -s "$path/.git/objects/info/alternates" ]] || return 1
  listing="$(git -C "$path" worktree list --porcelain 2>/dev/null)" || return 1
  [[ "$(printf '%s\n' "$listing" | awk '$1 == "worktree" { count++ } END { print count + 0 }')" == "1" ]] || return 1
  printf '%s\n' "$listing" | grep -Fxq "worktree $path" || return 1
  printf '%s\n' "$listing" | grep -Fxq "HEAD $expected_head" || return 1
  printf '%s\n' "$listing" | grep -Fxq "detached" || return 1
  ! printf '%s\n' "$listing" | grep -q '^branch ' || return 1
}

release_root_has_nonempty_alternates() {
  local release_root="$1"
  local alternate_file
  if ! alternate_file="$(find "$release_root" -type f -path '*/.git/objects/info/alternates' -size +0c -print -quit 2>/dev/null)"; then
    echo "release retention: failed to inspect Git alternates" >&2
    return 0
  fi
  [[ -n "$alternate_file" ]]
}

release_collect_running_references() {
  local revisions_file="$1"
  local paths_file="$2"
  local release_root="$3"
  local container_ids container_id revision mount_sources source resolved relative child
  : >"$revisions_file"
  : >"$paths_file"

  container_ids="$(docker container ls -q)" || {
    echo "release retention: failed to list running containers" >&2
    return 1
  }
  while IFS= read -r container_id; do
    [[ -n "$container_id" ]] || continue
    revision="$(docker container inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$container_id")" || {
      echo "release retention: failed to inspect running container revision: $container_id" >&2
      return 1
    }
    [[ "$revision" =~ ^[0-9a-f]{40}$ ]] && printf '%s\n' "$revision" >>"$revisions_file"

    mount_sources="$(docker container inspect --format '{{range .Mounts}}{{println .Source}}{{end}}' "$container_id")" || {
      echo "release retention: failed to inspect running container mounts: $container_id" >&2
      return 1
    }
    while IFS= read -r source; do
      [[ -n "$source" ]] || continue
      resolved="$(realpath "$source" 2>/dev/null)" || {
        echo "release retention: failed to resolve running container mount source: $source" >&2
        return 1
      }
      case "$resolved" in
        "$release_root"/*)
          relative="${resolved#"$release_root"/}"
          child="${relative%%/*}"
          [[ "$child" =~ ^[0-9a-f]{40}$ ]] && printf '%s/%s\n' "$release_root" "$child" >>"$paths_file"
          ;;
      esac
    done <<<"$mount_sources"
  done <<<"$container_ids"

  sort -u -o "$revisions_file" "$revisions_file"
  sort -u -o "$paths_file" "$paths_file"
}
