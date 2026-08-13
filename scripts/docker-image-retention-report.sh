#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/docker-image-retention-report.sh --keep-file FILE [--min-age-days N]

Builds a read-only Docker image retention report. It never removes or untags
images. FILE must contain one exact image reference per line for every retained
rollback state, scheduled utility or build base; blank lines and lines
beginning with # are ignored.

The report separately protects images referenced by running and stopped
containers. Only unused UniRouter-owned images older than N days are classified
as deletion candidates. The default is 14 days.
EOF
}

keep_file=""
min_age_days=14

while [[ $# -gt 0 ]]; do
  case "$1" in
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
      echo "image retention report: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${keep_file}" || ! -r "${keep_file}" ]]; then
  echo "image retention report: --keep-file must name a readable rollback keep file" >&2
  exit 2
fi
if [[ ! "${min_age_days}" =~ ^[0-9]+$ ]] || (( min_age_days < 1 || min_age_days > 3650 )); then
  echo "image retention report: --min-age-days must be an integer from 1 to 3650" >&2
  exit 2
fi
for command_name in docker jq; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "image retention report: ${command_name} is required" >&2
    exit 2
  fi
done
if ! docker info >/dev/null 2>&1; then
  echo "image retention report: Docker daemon is unavailable" >&2
  exit 2
fi
if ! date -d '1970-01-01T00:00:00Z' +%s >/dev/null 2>&1; then
  echo "image retention report: GNU date with timezone-aware -d support is required" >&2
  exit 2
fi

report_dir="$(mktemp -d "${TMPDIR:-/tmp}/unirouter-image-report.XXXXXX")"
cleanup() {
  rm -rf -- "${report_dir}"
}
trap cleanup EXIT

running_container_ids_file="${report_dir}/running-container-image-ids"
stopped_container_ids_file="${report_dir}/stopped-container-image-ids"
keep_ids_file="${report_dir}/keep-image-ids"
all_ids_file="${report_dir}/all-image-ids"
all_images_json_file="${report_dir}/all-images.json"
: >"${running_container_ids_file}"
: >"${stopped_container_ids_file}"
: >"${keep_ids_file}"

while IFS= read -r container_id; do
  [[ -n "${container_id}" ]] || continue
  docker container inspect --format '{{.Image}}' "${container_id}" >>"${running_container_ids_file}"
done < <(docker container ls -q)
sort -u -o "${running_container_ids_file}" "${running_container_ids_file}"

while IFS= read -r container_id; do
  [[ -n "${container_id}" ]] || continue
  container_image_id="$(docker container inspect --format '{{.Image}}' "${container_id}")"
  if ! grep -Fxq "${container_image_id}" "${running_container_ids_file}"; then
    printf '%s\n' "${container_image_id}" >>"${stopped_container_ids_file}"
  fi
done < <(docker container ls -aq)
sort -u -o "${stopped_container_ids_file}" "${stopped_container_ids_file}"

keep_ref_count=0
while IFS= read -r raw_line || [[ -n "${raw_line}" ]]; do
  image_ref="$(printf '%s' "${raw_line}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
  [[ -n "${image_ref}" && "${image_ref:0:1}" != "#" ]] || continue
  if [[ "${image_ref}" =~ [[:space:]] ]]; then
    echo "image retention report: invalid whitespace in keep reference: ${image_ref}" >&2
    exit 2
  fi
  if ! image_id="$(docker image inspect --format '{{.Id}}' "${image_ref}" 2>/dev/null)"; then
    echo "image retention report: keep reference does not resolve locally: ${image_ref}" >&2
    exit 2
  fi
  printf '%s\n' "${image_id}" >>"${keep_ids_file}"
  keep_ref_count=$((keep_ref_count + 1))
done <"${keep_file}"

if (( keep_ref_count == 0 )); then
  echo "image retention report: keep file contains no protected image references" >&2
  exit 2
fi
sort -u -o "${keep_ids_file}" "${keep_ids_file}"
docker image ls -aq --no-trunc | sort -u >"${all_ids_file}"
mapfile -t all_image_ids <"${all_ids_file}"
if (( ${#all_image_ids[@]} > 0 )); then
  docker image inspect "${all_image_ids[@]}" >"${all_images_json_file}"
else
  printf '[]\n' >"${all_images_json_file}"
fi

now_epoch="$(date +%s)"
minimum_age_seconds=$((min_age_days * 86400))
candidate_count=0
candidate_bytes=0
protected_running_count=0
protected_stopped_count=0
protected_keep_count=0
young_count=0
unmanaged_count=0

is_unirouter_owned() {
  local tags_csv="$1" component="$2"
  [[ "${component}" == "backend" || "${component}" == "frontend" ]] && return 0
  [[ "${tags_csv}" =~ (^|,)([^,]*/)?uniroute(r)?[-/:] ]] && return 0
  [[ "${tags_csv}" =~ (^|,)[0-9a-f]{7,40}-(backend|backend-canary|frontend): ]]
}

printf 'META\tgenerated_at=%s\tmin_age_days=%d\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${min_age_days}"
printf 'STATUS\tCREATED\tAGE_DAYS\tVIRTUAL_SIZE_MIB\tIMAGE_ID\tTAGS\n'
while IFS= read -r image_json; do
  [[ -n "${image_json}" ]] || continue
  image_id="$(jq -r '.Id' <<<"${image_json}")"
  created="$(jq -r '.Created' <<<"${image_json}")"
  created_epoch="$(date -d "${created}" +%s)"
  size_bytes="$(jq -r '.Size' <<<"${image_json}")"
  tags="$(jq -r '.RepoTags // [] | sort | join(",")' <<<"${image_json}")"
  component="$(jq -r '.Config.Labels["io.unirouter.component"] // ""' <<<"${image_json}")"
  size_mib="$(awk -v bytes="${size_bytes}" 'BEGIN { printf "%.1f", bytes / 1048576 }')"
  age_seconds=$((now_epoch - created_epoch))
  age_days=$((age_seconds / 86400))

  if grep -Fxq "${image_id}" "${running_container_ids_file}"; then
    status="protected-running-container"
    protected_running_count=$((protected_running_count + 1))
  elif grep -Fxq "${image_id}" "${stopped_container_ids_file}"; then
    status="protected-stopped-container"
    protected_stopped_count=$((protected_stopped_count + 1))
  elif grep -Fxq "${image_id}" "${keep_ids_file}"; then
    status="protected-keep"
    protected_keep_count=$((protected_keep_count + 1))
  elif ! is_unirouter_owned "${tags}" "${component}"; then
    status="unmanaged"
    unmanaged_count=$((unmanaged_count + 1))
  elif (( age_seconds < minimum_age_seconds )); then
    status="young"
    young_count=$((young_count + 1))
  else
    status="candidate"
    candidate_count=$((candidate_count + 1))
    candidate_bytes=$((candidate_bytes + size_bytes))
  fi

  printf '%s\t%s\t%d\t%s\t%s\t%s\n' \
    "${status}" "${created}" "${age_days}" "${size_mib}" "${image_id}" "${tags:--}"
done < <(jq -c '.[]' "${all_images_json_file}")

candidate_mib="$(awk -v bytes="${candidate_bytes}" 'BEGIN { printf "%.1f", bytes / 1048576 }')"
printf '\nSUMMARY\trunning_protected=%d\tstopped_protected=%d\tkeep_protected=%d\tyoung=%d\tunmanaged=%d\tcandidates=%d\tcandidate_virtual_mib=%s\n' \
  "${protected_running_count}" "${protected_stopped_count}" "${protected_keep_count}" \
  "${young_count}" "${unmanaged_count}" "${candidate_count}" "${candidate_mib}"
echo "NOTE: virtual sizes share layers and must not be added as an exact reclaim estimate"
echo "READ_ONLY: no image was removed or untagged"
