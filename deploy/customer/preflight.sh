#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
compose_file="${script_dir}/docker-compose.yml"
env_file="${1:-${script_dir}/.env}"

if [[ ! -r "${env_file}" ]]; then
  echo "customer preflight: environment file is not readable: ${env_file}" >&2
  exit 2
fi
if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
  echo "customer preflight: Docker Compose v2 is required" >&2
  exit 2
fi

compose=(docker compose --env-file "${env_file}" -f "${compose_file}")
rendered="$("${compose[@]}" config)"
if ! interpolation_environment="$("${compose[@]}" config --environment)"; then
  echo "customer preflight: Docker Compose must support 'config --environment'" >&2
  exit 2
fi

load_effective_value() {
  local key="$1"
  local optional="${2:-false}"

  if ! effective_value="$(awk -v key="${key}" '
    index($0, key "=") == 1 {
      print substr($0, length(key) + 2)
      found = 1
      exit
    }
    END { if (!found) exit 1 }
  ' <<<"${interpolation_environment}")"; then
    if [[ "${optional}" == "true" ]]; then
      effective_value=""
      return 0
    fi
    echo "customer preflight: ${key} is missing from the effective Compose interpolation environment" >&2
    exit 1
  fi
  case "${effective_value}" in
    *REPLACE_*|*CHANGE_ME_*|*CHANGEME*)
      echo "customer preflight: ${key} still contains a placeholder" >&2
      exit 1
      ;;
  esac
}

require_release_secret() {
  local key="$1"
  local byte_count

  load_effective_value "${key}"
  byte_count="$(LC_ALL=C printf '%s' "${effective_value}" | wc -c | tr -d '[:space:]')"
  if [[ "${byte_count}" -lt 32 ]]; then
    echo "customer preflight: ${key} must be at least 32 bytes; effective value is ${byte_count} bytes" >&2
    exit 1
  fi
}

require_exact_bytes() {
  local key="$1"
  local required_bytes="$2"
  local byte_count

  load_effective_value "${key}"
  byte_count="$(LC_ALL=C printf '%s' "${effective_value}" | wc -c | tr -d '[:space:]')"
  if [[ "${byte_count}" -ne "${required_bytes}" ]]; then
    echo "customer preflight: ${key} must be exactly ${required_bytes} bytes; effective value is ${byte_count} bytes" >&2
    exit 1
  fi
}

require_configured_value() {
  load_effective_value "$1"
}

require_release_secret JWT_SECRET
require_release_secret API_KEY_HASH_SECRET
require_release_secret UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET
require_exact_bytes ENCRYPTION_KEY 32

load_effective_value RESPONSE_CACHE_INDEX_KEY true
response_cache_index_key="${effective_value}"
load_effective_value RESPONSE_CACHE_ENCRYPTION_KEY true
response_cache_encryption_key="${effective_value}"
load_effective_value RESPONSE_CACHE_KEY_VERSION true
response_cache_key_version="${effective_value}"
if [[ -n "${response_cache_index_key}" || -n "${response_cache_encryption_key}" || -n "${response_cache_key_version}" ]]; then
  response_cache_index_bytes="$(LC_ALL=C printf '%s' "${response_cache_index_key}" | wc -c | tr -d '[:space:]')"
  response_cache_encryption_bytes="$(LC_ALL=C printf '%s' "${response_cache_encryption_key}" | wc -c | tr -d '[:space:]')"
  if [[ "${response_cache_index_bytes}" -ne 32 || "${response_cache_encryption_bytes}" -ne 32 ]]; then
    echo "customer preflight: enabled Exact Response Cache requires independent exact 32-byte index and encryption keys" >&2
    exit 1
  fi
  if [[ ! "${response_cache_key_version}" =~ ^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$ ]]; then
    echo "customer preflight: RESPONSE_CACHE_KEY_VERSION must be a portable 1-128 character identifier" >&2
    exit 1
  fi
fi
require_configured_value POSTGRES_PASSWORD
require_configured_value DB_DSN
require_configured_value REDIS_PASSWORD
require_configured_value GRAFANA_ADMIN_PASSWORD
require_configured_value LICENSE_DEPLOYMENT_ID
require_configured_value UNIROUTER_INTERNAL_NETWORK_CIDR
internal_network_cidr="${effective_value}"
require_configured_value UNIROUTER_INTERNAL_NETWORK_GATEWAY
internal_network_gateway="${effective_value}"
require_configured_value UNIROUTER_FRONTEND_PROXY_IP
frontend_proxy_ip="${effective_value}"

if [[ ! "${internal_network_cidr}" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.0/24$ ]]; then
  echo "customer preflight: UNIROUTER_INTERNAL_NETWORK_CIDR must be a dedicated IPv4 /24 ending in .0/24" >&2
  exit 1
fi
network_a="${BASH_REMATCH[1]}"
network_b="${BASH_REMATCH[2]}"
network_c="${BASH_REMATCH[3]}"
if (( network_a > 255 || network_b > 255 || network_c > 255 )); then
  echo "customer preflight: UNIROUTER_INTERNAL_NETWORK_CIDR contains an invalid IPv4 octet" >&2
  exit 1
fi
network_prefix="${network_a}.${network_b}.${network_c}"

if [[ ! "${internal_network_gateway}" =~ ^${network_prefix//./\.}\.([0-9]{1,3})$ ]]; then
  echo "customer preflight: UNIROUTER_INTERNAL_NETWORK_GATEWAY must be inside ${internal_network_cidr}" >&2
  exit 1
fi
gateway_host="${BASH_REMATCH[1]}"
if (( gateway_host < 1 || gateway_host > 254 )); then
  echo "customer preflight: UNIROUTER_INTERNAL_NETWORK_GATEWAY cannot be the network or broadcast address" >&2
  exit 1
fi

if [[ ! "${frontend_proxy_ip}" =~ ^${network_prefix//./\.}\.([0-9]{1,3})$ ]]; then
  echo "customer preflight: UNIROUTER_FRONTEND_PROXY_IP must be inside ${internal_network_cidr}" >&2
  exit 1
fi
frontend_host="${BASH_REMATCH[1]}"
if (( frontend_host < 1 || frontend_host > 254 || frontend_host == gateway_host )); then
  echo "customer preflight: UNIROUTER_FRONTEND_PROXY_IP must be a usable non-gateway host address" >&2
  exit 1
fi

require_configured_value TRUSTED_PROXY_CIDRS
trusted_proxy_cidrs="${effective_value//[[:space:]]/}"
case ",${trusted_proxy_cidrs}," in
  *,0.0.0.0/0,*|*,::/0,*)
    echo "customer preflight: TRUSTED_PROXY_CIDRS must not trust every address" >&2
    exit 1
    ;;
esac
case ",${trusted_proxy_cidrs}," in
  *,"${frontend_proxy_ip}/32",*) ;;
  *)
    echo "customer preflight: TRUSTED_PROXY_CIDRS must contain the fixed frontend proxy ${frontend_proxy_ip}/32" >&2
    exit 1
    ;;
esac
require_configured_value UNIROUTER_LISTEN_ADDRESS
if [[ "${effective_value}" != "127.0.0.1" ]]; then
  echo "customer preflight: UNIROUTER_LISTEN_ADDRESS must remain 127.0.0.1 behind the approved TLS proxy" >&2
  exit 1
fi
unset interpolation_environment
unset effective_value
unset frontend_proxy_ip
unset trusted_proxy_cidrs
unset internal_network_cidr
unset internal_network_gateway
unset network_a
unset network_b
unset network_c
unset network_prefix
unset gateway_host
unset frontend_host
unset response_cache_index_key
unset response_cache_encryption_key
unset response_cache_key_version
unset response_cache_index_bytes
unset response_cache_encryption_bytes

if grep -Eq '^[[:space:]]*build:' <<<"${rendered}"; then
  echo "customer preflight: customer Compose must not contain build contexts" >&2
  exit 1
fi

image_count=0
digest_pattern='^[^[:space:]@]+@sha256:[0-9a-f]{64}$'
while IFS= read -r image; do
  [[ -z "${image}" ]] && continue
  image_count=$((image_count + 1))
  if [[ ! "${image}" =~ ${digest_pattern} ]]; then
    echo "customer preflight: image is not pinned to an immutable sha256 digest: ${image}" >&2
    exit 1
  fi
done <<<"$("${compose[@]}" config --images)"

if [[ "${image_count}" -ne 6 ]]; then
  echo "customer preflight: expected exactly six runtime images, found ${image_count}" >&2
  exit 1
fi

echo "customer preflight: PASS (runtime secrets, trusted proxy boundary, loopback bind, rendered Compose, no build contexts, six digest-pinned images)"
