#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/docker-storage-preflight.sh [--path PATH] [--minimum-free-gib N] [--maximum-use-percent N]

Fails closed before an internal Target build when the filesystem containing
PATH has less than N GiB available or exceeds N percent used. Defaults:
PATH=/var/lib/docker, minimum free=8 GiB, maximum used=80 percent.
EOF
}

storage_path="/var/lib/docker"
minimum_free_gib=8
maximum_use_percent=80

while [[ $# -gt 0 ]]; do
  case "$1" in
    --path)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      storage_path="$2"
      shift 2
      ;;
    --minimum-free-gib)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      minimum_free_gib="$2"
      shift 2
      ;;
    --maximum-use-percent)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      maximum_use_percent="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "docker storage preflight: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! -e "${storage_path}" ]]; then
  echo "docker storage preflight: path does not exist: ${storage_path}" >&2
  exit 2
fi
if [[ ! "${minimum_free_gib}" =~ ^[1-9][0-9]*$ ]] ||
   [[ ! "${maximum_use_percent}" =~ ^[1-9][0-9]*$ ]] ||
   (( maximum_use_percent > 99 )); then
  echo "docker storage preflight: thresholds must be positive integers; maximum use must be below 100" >&2
  exit 2
fi

read -r filesystem_blocks used_blocks available_blocks use_percent mountpoint < <(
  df -Pk "${storage_path}" | awk 'NR == 2 { gsub(/%/, "", $5); print $2, $3, $4, $5, $6 }'
)
minimum_free_kib=$((minimum_free_gib * 1024 * 1024))

printf 'storage_path=%s mountpoint=%s used_percent=%s available_gib=%.2f minimum_free_gib=%d maximum_use_percent=%d\n' \
  "${storage_path}" "${mountpoint}" "${use_percent}" \
  "$(awk -v kib="${available_blocks}" 'BEGIN { print kib / 1024 / 1024 }')" \
  "${minimum_free_gib}" "${maximum_use_percent}"

if (( use_percent > maximum_use_percent )); then
  echo "docker storage preflight: refused because filesystem use exceeds the approved threshold" >&2
  exit 1
fi
if (( available_blocks < minimum_free_kib )); then
  echo "docker storage preflight: refused because available space is below the approved threshold" >&2
  exit 1
fi

echo "docker_storage_preflight=pass"
