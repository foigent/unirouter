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

load_effective_value RUNTIME_ENVIRONMENT true
runtime_environment="${effective_value:-production}"
if [[ "${runtime_environment}" != "production" ]]; then
  echo "customer preflight: formal customer deployments require RUNTIME_ENVIRONMENT=production so fresh bootstrap cannot bypass recorded release evidence" >&2
  exit 1
fi

load_effective_value BILLING_MODE true
billing_mode="${effective_value:-cost_observation}"
case "${billing_mode}" in
  cost_observation|prepaid_enforced) ;;
  budget_enforced)
    echo "customer preflight: BILLING_MODE=budget_enforced is reserved but not yet supported" >&2
    exit 1
    ;;
  *)
    echo "customer preflight: BILLING_MODE must be cost_observation or prepaid_enforced" >&2
    exit 1
    ;;
esac

load_effective_value MODEL_ROUTE_AUTHORITY true
model_route_authority="${effective_value:-canonical}"
load_effective_value MODEL_ROUTE_COMPATIBILITY_ENABLED true
model_route_compatibility_enabled="${effective_value:-false}"
case "${model_route_compatibility_enabled}" in
  true|false) ;;
  *)
    echo "customer preflight: MODEL_ROUTE_COMPATIBILITY_ENABLED must be exact true or false" >&2
    exit 1
    ;;
esac
load_effective_value ASYNC_MEDIA_ENABLED true
async_media_enabled="${effective_value:-false}"
case "${async_media_enabled}" in
  true|false) ;;
  *)
    echo "customer preflight: ASYNC_MEDIA_ENABLED must be exact true or false" >&2
    exit 1
    ;;
esac
case "${model_route_authority}" in
  legacy)
    if [[ "${model_route_compatibility_enabled}" != "true" ]]; then
      echo "customer preflight: MODEL_ROUTE_AUTHORITY=legacy requires MODEL_ROUTE_COMPATIBILITY_ENABLED=true" >&2
      exit 1
    fi
    echo "customer preflight: MODEL_ROUTE_AUTHORITY=legacy compatibility selected; on a fresh install this is one-time bootstrap/authoring authority and exposes the full legacy runtime, not a bootstrap-only listener; keep customer traffic blocked, create the customer root and canonical deployment/release/slot, prove a one-off canonical check, then switch to canonical with compatibility=false and restart" >&2
    ;;
  verify)
    if [[ "${model_route_compatibility_enabled}" != "true" ]]; then
      echo "customer preflight: MODEL_ROUTE_AUTHORITY=verify requires MODEL_ROUTE_COMPATIBILITY_ENABLED=true" >&2
      exit 1
    fi
    echo "customer preflight: MODEL_ROUTE_AUTHORITY=verify; while every current assignment is v1 run 'uniroute model-route-authority apply --customer-org-id <id>' and then 'check' before restart (after v2 assignment use check only), drain old legacy writers, and rely on the backend database startup gate (this config preflight is not readiness)" >&2
    ;;
  canonical)
    if [[ "${model_route_compatibility_enabled}" != "false" ]]; then
      echo "customer preflight: MODEL_ROUTE_AUTHORITY=canonical requires MODEL_ROUTE_COMPATIBILITY_ENABLED=false" >&2
      exit 1
    fi
    if [[ "${async_media_enabled}" != "false" ]]; then
      echo "customer preflight: MODEL_ROUTE_AUTHORITY=canonical requires ASYNC_MEDIA_ENABLED=false until async media uses canonical model execution" >&2
      exit 1
    fi
    echo "customer preflight: MODEL_ROUTE_AUTHORITY=canonical; run read-only 'uniroute model-route-authority check --customer-org-id <id>' before restart, drain old legacy writers, and rely on the backend database startup gate (this config preflight is not readiness); apply is verify-preparation only and is rejected in canonical mode" >&2
    ;;
  *)
    echo "customer preflight: MODEL_ROUTE_AUTHORITY must be legacy, verify, or canonical" >&2
    exit 1
    ;;
esac

load_effective_value REQUEST_PRICING_AUTHORITY true
request_pricing_authority="${effective_value:-legacy}"
case "${request_pricing_authority}" in
  legacy) ;;
  rate_card)
    echo "customer preflight: REQUEST_PRICING_AUTHORITY=rate_card selected; verify every routed model has one complete active supplier_contracted or provider_list Rate Card before restart" >&2
    ;;
  *)
    echo "customer preflight: REQUEST_PRICING_AUTHORITY must be legacy or rate_card" >&2
    exit 1
    ;;
esac

load_effective_value DOCKER_LOG_MAX_SIZE true
docker_log_max_size="${effective_value:-20m}"
if [[ ! "${docker_log_max_size}" =~ ^[1-9][0-9]*[kKmMgG]$ ]]; then
  echo "customer preflight: DOCKER_LOG_MAX_SIZE must be a positive k, m or g size such as 20m" >&2
  exit 1
fi
load_effective_value DOCKER_LOG_MAX_FILES true
docker_log_max_files="${effective_value:-3}"
if [[ ! "${docker_log_max_files}" =~ ^[1-9][0-9]*$ ]] || (( docker_log_max_files > 100 )); then
  echo "customer preflight: DOCKER_LOG_MAX_FILES must be an integer from 1 to 100" >&2
  exit 1
fi

load_effective_value SOURCE_FACTS_ENABLED true
source_facts_enabled="${effective_value:-false}"
case "${source_facts_enabled}" in
  false) ;;
  true)
    load_effective_value COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE true
    if [[ ! "${effective_value}" =~ ^[a-zA-Z0-9._:-]{1,128}$ ]]; then
      echo "customer preflight: source facts require a stable COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE" >&2
      exit 1
    fi
    echo "customer preflight: source capture requires the supported M116–M118 schema and preserves the existing producer identity; config validation does not prove database readiness" >&2
    ;;
  *) echo "customer preflight: SOURCE_FACTS_ENABLED must be true or false" >&2; exit 1 ;;
esac

load_effective_value SOURCE_OBSERVATIONS_ENABLED true
source_observations_enabled="${effective_value:-false}"
case "${source_observations_enabled}" in
  false) ;;
  true)
    if [[ "${source_facts_enabled}" != "true" ]]; then
      echo "customer preflight: SOURCE_OBSERVATIONS_ENABLED requires SOURCE_FACTS_ENABLED=true" >&2
      exit 1
    fi
    echo "customer preflight: B observations require M117 and explicit receiver profile grants; runtime verifies database readiness" >&2
    ;;
  *) echo "customer preflight: SOURCE_OBSERVATIONS_ENABLED must be true or false" >&2; exit 1 ;;
esac

load_effective_value SOURCE_BUDGET_SNAPSHOTS_ENABLED true
source_budget_snapshots_enabled="${effective_value:-false}"
case "${source_budget_snapshots_enabled}" in
  false) ;;
  true)
    if [[ "${source_facts_enabled}" != "true" ]]; then
      echo "customer preflight: SOURCE_BUDGET_SNAPSHOTS_ENABLED requires SOURCE_FACTS_ENABLED=true" >&2
      exit 1
    fi
    echo "customer preflight: D snapshots require M118 writer certification and explicit profile grants; runtime verifies restored coverage" >&2
    ;;
  *) echo "customer preflight: SOURCE_BUDGET_SNAPSHOTS_ENABLED must be true or false" >&2; exit 1 ;;
esac

load_effective_value XINSIGHT_CONNECTOR_AGENT_ENABLED true
connector_agent_enabled="${effective_value:-false}"
case "${connector_agent_enabled}" in
  false) ;;
  true)
    load_effective_value COMPLETED_EXECUTION_FEED_ENABLED true
    if [[ "${effective_value}" != "true" ]]; then
      echo "customer preflight: enabled Connector agent requires COMPLETED_EXECUTION_FEED_ENABLED=true" >&2
      exit 1
    fi
    for connector_key in \
      XINSIGHT_CONNECTOR_CONTROL_ORIGIN \
      XINSIGHT_CONNECTOR_POLL_MILLISECONDS \
      XINSIGHT_CONNECTOR_REQUEST_TIMEOUT_MILLISECONDS \
      XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON; do
      load_effective_value "${connector_key}" true
      if [[ -z "${effective_value}" || ( "${connector_key}" == "XINSIGHT_CONNECTOR_RECEIVER_BINDINGS_JSON" && "${effective_value}" == "[]" ) ]]; then
        echo "customer preflight: ${connector_key} is required when Connector agent is enabled" >&2
        exit 1
      fi
    done
    ;;
  *)
    echo "customer preflight: XINSIGHT_CONNECTOR_AGENT_ENABLED must be true or false" >&2
    exit 1
    ;;
esac

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
unset connector_agent_enabled
unset connector_key
unset billing_mode
unset request_pricing_authority
unset docker_log_max_size
unset docker_log_max_files

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

echo "customer preflight: PASS (runtime secrets, trusted proxy boundary, loopback bind, bounded Docker logs, rendered Compose, no build contexts, six digest-pinned images)"
