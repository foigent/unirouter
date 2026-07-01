#!/usr/bin/env bash
set -euo pipefail

STABLE_URL="${UNIROUTE_STABLE_URL:-http://localhost:8080}"
CANARY_URL="${UNIROUTE_CANARY_URL:-http://localhost:8081}"
PROXY_URL="${UNIROUTE_PROXY_URL:-}"
API_KEY="${UNIROUTE_API_KEY:-}"
MODEL="${UNIROUTE_MODEL:-gpt-4o}"
RUN_GATEWAY_SMOKE="${UNIROUTE_RUN_GATEWAY_SMOKE:-0}"
REQUEST_ID="${UNIROUTE_REQUEST_ID:-prd2-ha-smoke-$(date +%s)}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

json_has() {
  local body="$1"
  local needle="$2"
  case "$body" in
    *"$needle"*) return 0 ;;
    *) return 1 ;;
  esac
}

expect_ready() {
  local name="$1"
  local base_url="$2"
  local body
  body="$(curl -fsS "$base_url/health/ready")"
  json_has "$body" '"status":"ok"' || {
    echo "$name health did not report status ok: $body" >&2
    exit 1
  }
  json_has "$body" '"db":"ok"' || {
    echo "$name health did not report db ok: $body" >&2
    exit 1
  }
  json_has "$body" '"redis":"ok"' || {
    echo "$name health did not report redis ok: $body" >&2
    exit 1
  }
  echo "PASS $name /health/ready $body"
}

gateway_call() {
  local name="$1"
  local base_url="$2"
  local status
  status="$(
    curl -sS -o /tmp/uniroute-ha-smoke-response.txt -w '%{http_code}' \
      "$base_url/v1/chat/completions" \
      -H "Authorization: Bearer $API_KEY" \
      -H "Content-Type: application/json" \
      -H "X-Request-ID: $REQUEST_ID" \
      -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"ha smoke\"}],\"stream\":false}"
  )"
  if [[ "$status" != "200" && "$status" != "409" && "$status" != "429" ]]; then
    echo "$name gateway smoke unexpected HTTP $status" >&2
    sed -n '1,120p' /tmp/uniroute-ha-smoke-response.txt >&2 || true
    exit 1
  fi
  echo "PASS $name gateway smoke accepted HTTP $status request_id=$REQUEST_ID"
}

need_cmd curl

expect_ready "stable" "$STABLE_URL"
expect_ready "canary" "$CANARY_URL"

if [[ -n "$PROXY_URL" ]]; then
  expect_ready "proxy" "$PROXY_URL"
fi

if [[ "$RUN_GATEWAY_SMOKE" == "1" ]]; then
  if [[ -z "$API_KEY" ]]; then
    echo "UNIROUTE_API_KEY is required when UNIROUTE_RUN_GATEWAY_SMOKE=1" >&2
    exit 2
  fi
  gateway_call "stable" "$STABLE_URL"
  gateway_call "canary" "$CANARY_URL"
else
  echo "SKIP gateway smoke; set UNIROUTE_RUN_GATEWAY_SMOKE=1 and UNIROUTE_API_KEY to enable"
fi

echo "HA smoke completed"
