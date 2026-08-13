#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/docker-image-retention-maintenance.sh --keep-file FILE [options]

Options:
  --min-image-age-days N  Delete only report candidates at least N days old (default: 14)
  --cache-until Nh        Prune unused BuildKit cache older than Nh (default: 168h)
  --report-out FILE       Atomically write the final read-only report to FILE
  --apply                 Apply guarded image and BuildKit cache cleanup
  -h, --help              Show this help

Without --apply this command only prints the current read-only report. It never
removes containers, volumes, database data, backups or release directories.
EOF
}

keep_file=""
min_image_age_days=14
cache_until="168h"
report_out=""
apply=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep-file)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      keep_file="$2"
      shift 2
      ;;
    --min-image-age-days)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      min_image_age_days="$2"
      shift 2
      ;;
    --cache-until)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      cache_until="$2"
      shift 2
      ;;
    --report-out)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      report_out="$2"
      shift 2
      ;;
    --apply)
      apply=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "docker image maintenance: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${keep_file}" || ! -r "${keep_file}" ]]; then
  echo "docker image maintenance: --keep-file must name a readable file" >&2
  exit 2
fi
if [[ ! "${min_image_age_days}" =~ ^[1-9][0-9]*$ ]] || (( min_image_age_days > 3650 )); then
  echo "docker image maintenance: --min-image-age-days must be from 1 to 3650" >&2
  exit 2
fi
if [[ ! "${cache_until}" =~ ^[1-9][0-9]*h$ ]]; then
  echo "docker image maintenance: --cache-until must use a positive integer hour value such as 168h" >&2
  exit 2
fi
for command_name in docker jq flock; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "docker image maintenance: ${command_name} is required" >&2
    exit 2
  fi
done
if ! docker info >/dev/null 2>&1; then
  echo "docker image maintenance: Docker daemon is unavailable" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
report_script="${script_dir}/docker-image-retention-report.sh"
if [[ ! -x "${report_script}" ]]; then
  echo "docker image maintenance: report script is missing or not executable: ${report_script}" >&2
  exit 2
fi

exec 9>/run/lock/unirouter-docker-lifecycle.lock
if ! flock -n 9; then
  echo "docker image maintenance: another build or maintenance run holds the lifecycle lock" >&2
  exit 1
fi

maintenance_dir="$(mktemp -d "${TMPDIR:-/tmp}/unirouter-image-maintenance.XXXXXX")"
cleanup() {
  rm -rf -- "${maintenance_dir}"
}
trap cleanup EXIT

report_file="${maintenance_dir}/report.tsv"
candidate_file="${maintenance_dir}/candidates.tsv"
keep_ids_file="${maintenance_dir}/keep-image-ids"

build_report() {
  "${report_script}" --keep-file "${keep_file}" --min-age-days "${min_image_age_days}" >"${report_file}"
}

write_report() {
  if [[ -n "${report_out}" ]]; then
    report_parent="$(dirname "${report_out}")"
    [[ -d "${report_parent}" ]] || {
      echo "docker image maintenance: report directory does not exist: ${report_parent}" >&2
      exit 2
    }
    report_stage="${report_out}.tmp.$$"
    install -m 0640 "${report_file}" "${report_stage}"
    mv -f -- "${report_stage}" "${report_out}"
  fi
}

build_keep_ids() {
  : >"${keep_ids_file}"
  while IFS= read -r raw_line || [[ -n "${raw_line}" ]]; do
    image_ref="$(printf '%s' "${raw_line}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    [[ -n "${image_ref}" && "${image_ref:0:1}" != "#" ]] || continue
    docker image inspect --format '{{.Id}}' "${image_ref}" >>"${keep_ids_file}"
  done <"${keep_file}"
  sort -u -o "${keep_ids_file}" "${keep_ids_file}"
}

container_references_image() {
  local expected_id="$1" container_id
  while IFS= read -r container_id; do
    [[ -n "${container_id}" ]] || continue
    if [[ "$(docker container inspect --format '{{.Image}}' "${container_id}")" == "${expected_id}" ]]; then
      return 0
    fi
  done < <(docker container ls -aq)
  return 1
}

unirouter_owned_image() {
  local tags_csv="$1" component="$2" tag
  if [[ "${tags_csv}" == "-" ]]; then
    [[ "${component}" == "backend" || "${component}" == "frontend" ]]
    return
  fi
  IFS=',' read -r -a tag_array <<<"${tags_csv}"
  for tag in "${tag_array[@]}"; do
    if [[ ! "${tag}" =~ (^|/)(uniroute|unirouter)[^/:]*[-/:] ]] &&
       [[ ! "${tag}" =~ ^[0-9a-f]{7,40}-(backend|backend-canary|frontend): ]]; then
      return 1
    fi
  done
  return 0
}

build_report
cat "${report_file}"

if [[ "${apply}" != true ]]; then
  write_report
  echo "DRY_RUN: no image, tag or BuildKit cache was removed"
  exit 0
fi

build_keep_ids
awk -F '\t' '$1 == "candidate" { print $0 }' "${report_file}" >"${candidate_file}"
candidate_count="$(wc -l <"${candidate_file}" | tr -d ' ')"
printf 'APPLY_START candidate_count=%s cache_until=%s\n' "${candidate_count}" "${cache_until}"

while IFS=$'\t' read -r status reported_created reported_age reported_size image_id reported_tags; do
  [[ "${status}" == "candidate" ]] || continue
  if ! image_json="$(docker image inspect "${image_id}" 2>/dev/null)"; then
    echo "SKIP_ABSENT image_id=${image_id}"
    continue
  fi
  current_id="$(jq -r '.[0].Id' <<<"${image_json}")"
  current_created="$(jq -r '.[0].Created' <<<"${image_json}")"
  current_tags="$(jq -r '.[0].RepoTags // [] | sort | join(",")' <<<"${image_json}")"
  current_tags="${current_tags:--}"
  current_component="$(jq -r '.[0].Config.Labels["io.unirouter.component"] // ""' <<<"${image_json}")"
  if [[ "${current_id}" != "${image_id}" || "${current_created}" != "${reported_created}" || "${current_tags}" != "${reported_tags}" ]]; then
    echo "REFUSED_DRIFT image_id=${image_id}" >&2
    exit 1
  fi
  if container_references_image "${image_id}"; then
    echo "REFUSED_CONTAINER_REFERENCE image_id=${image_id}" >&2
    exit 1
  fi
  if grep -Fxq "${image_id}" "${keep_ids_file}"; then
    echo "REFUSED_KEEP_REFERENCE image_id=${image_id}" >&2
    exit 1
  fi
  if ! unirouter_owned_image "${current_tags}" "${current_component}"; then
    echo "REFUSED_NON_UNIROUTER_TAG image_id=${image_id}" >&2
    exit 1
  fi
  if [[ "${current_tags}" != "-" ]]; then
    IFS=',' read -r -a current_tag_array <<<"${current_tags}"
  else
    current_tag_array=()
  fi
  for current_tag in "${current_tag_array[@]}"; do
    # Removing a repository tag can also remove its matching digest reference.
    # Re-check each reference so that this Docker side effect is treated as an
    # already-completed untag, while every remaining reference is still removed
    # explicitly.
    if ! docker image inspect "${current_tag}" >/dev/null 2>&1; then
      echo "SKIP_ALREADY_UNTAGGED image_id=${image_id} tag=${current_tag}"
      continue
    fi
    docker image rm -- "${current_tag}"
  done
  if docker image inspect "${image_id}" >/dev/null 2>&1; then
    if container_references_image "${image_id}"; then
      echo "REFUSED_LATE_CONTAINER_REFERENCE image_id=${image_id}" >&2
      exit 1
    fi
    docker image rm -- "${image_id}"
  fi
  printf 'REMOVED image_id=%s age_days=%s virtual_size_mib=%s tags=%s\n' \
    "${image_id}" "${reported_age}" "${reported_size}" "${reported_tags}"
done <"${candidate_file}"

docker builder prune --all --filter "until=${cache_until}" --force >/dev/null
build_report
write_report
cat "${report_file}"
echo "APPLY_COMPLETE: no container, volume, database, backup or release directory was removed"
