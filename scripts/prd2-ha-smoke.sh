#!/usr/bin/env bash
set -euo pipefail

STABLE_URL="${UNIROUTE_STABLE_URL:-http://localhost:8080}"
CANARY_URL="${UNIROUTE_CANARY_URL:-http://localhost:8081}"
PROXY_URL="${UNIROUTE_PROXY_URL:-}"
API_KEY="${UNIROUTE_API_KEY:-}"
MODEL="${UNIROUTE_MODEL:-gpt-4o}"
RUN_GATEWAY_SMOKE="${UNIROUTE_RUN_GATEWAY_SMOKE:-0}"
RUN_RATE_LIMIT_SMOKE="${UNIROUTE_RUN_RATE_LIMIT_SMOKE:-0}"
RATE_LIMIT_REQUESTS="${UNIROUTE_RATE_LIMIT_REQUESTS:-4}"
DB_DSN="${UNIROUTE_HA_DB_DSN:-${DB_DSN:-}}"
REQUEST_ID="${UNIROUTE_REQUEST_ID:-prd2-ha-smoke-$(date +%s)}"
RESPONSE_FILE="${UNIROUTE_HA_RESPONSE_FILE:-${TMPDIR:-/tmp}/uniroute-ha-smoke-response-$$.txt}"

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

sql_quote() {
  printf "%s" "$1" | sed "s/'/''/g"
}

expect_live() {
  local name="$1"
  local base_url="$2"
  local body
  body="$(curl -fsS "$base_url/health/live")"
  json_has "$body" '"status":"ok"' || {
    echo "$name health did not report live status ok: $body" >&2
    exit 1
  }
  echo "PASS $name /health/live $body"
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
  local request_id="${3:-$REQUEST_ID}"
  local status
  status="$(
    curl -sS -o "$RESPONSE_FILE" -w '%{http_code}' \
      "$base_url/v1/chat/completions" \
      -H "Authorization: Bearer $API_KEY" \
      -H "Content-Type: application/json" \
      -H "X-Request-ID: $request_id" \
      -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"ha smoke\"}],\"stream\":false}"
  )"
  if [[ "$status" != "200" && "$status" != "409" && "$status" != "429" ]]; then
    echo "$name gateway smoke unexpected HTTP $status" >&2
    sed -n '1,120p' "$RESPONSE_FILE" >&2 || true
    exit 1
  fi
  echo "PASS $name gateway smoke accepted HTTP $status request_id=$request_id"
}

db_gateway_evidence() {
  if [[ -z "$DB_DSN" ]]; then
    echo "SKIP DB gateway evidence; set UNIROUTE_HA_DB_DSN or DB_DSN to enable"
    return
  fi
  need_cmd psql
  local quoted_request_id
  quoted_request_id="$(sql_quote "$REQUEST_ID")"
  local freeze_rows
  local freeze_tx_rows
  local log_rows
  freeze_rows="$(psql "$DB_DSN" -v ON_ERROR_STOP=1 -Atc "SELECT COUNT(*) FROM freeze_idempotency WHERE request_id = '$quoted_request_id';")"
  freeze_tx_rows="$(psql "$DB_DSN" -v ON_ERROR_STOP=1 -Atc "SELECT COUNT(*) FROM transactions WHERE request_id = '$quoted_request_id' AND type = 'freeze';")"
  log_rows="$(psql "$DB_DSN" -v ON_ERROR_STOP=1 -Atc "SELECT COUNT(*) FROM call_logs WHERE request_id = '$quoted_request_id';")"
  if [[ "$freeze_rows" -gt 1 || "$freeze_tx_rows" -gt 1 ]]; then
    echo "FAIL duplicate idempotency evidence: freeze_rows=$freeze_rows freeze_tx_rows=$freeze_tx_rows request_id=$REQUEST_ID" >&2
    exit 1
  fi
  echo "PASS DB gateway evidence request_id=$REQUEST_ID freeze_rows=$freeze_rows freeze_tx_rows=$freeze_tx_rows call_logs=$log_rows"
}

rate_limit_smoke() {
  if [[ "$RUN_RATE_LIMIT_SMOKE" != "1" ]]; then
    echo "SKIP shared rate-limit smoke; set UNIROUTE_RUN_RATE_LIMIT_SMOKE=1 with a low-limit test API key to enable"
    return
  fi
  if [[ "$RUN_GATEWAY_SMOKE" != "1" || -z "$API_KEY" ]]; then
    echo "UNIROUTE_RUN_GATEWAY_SMOKE=1 and UNIROUTE_API_KEY are required for rate-limit smoke" >&2
    exit 2
  fi
  local seen_429=0
  local i
  for i in $(seq 1 "$RATE_LIMIT_REQUESTS"); do
    local target_name="stable"
    local target_url="$STABLE_URL"
    if (( i % 2 == 0 )); then
      target_name="canary"
      target_url="$CANARY_URL"
    fi
    local request_id="${REQUEST_ID}-rl-${i}"
    local status
    status="$(
      curl -sS -o "$RESPONSE_FILE" -w '%{http_code}' \
        "$target_url/v1/chat/completions" \
        -H "Authorization: Bearer $API_KEY" \
        -H "Content-Type: application/json" \
        -H "X-Request-ID: $request_id" \
        -d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"ha rate limit smoke\"}],\"stream\":false}"
    )"
    echo "RATE_LIMIT $i $target_name HTTP $status request_id=$request_id"
    if [[ "$status" == "429" ]]; then
      seen_429=1
      break
    fi
  done
  if [[ "$seen_429" != "1" ]]; then
    echo "shared rate-limit smoke did not observe HTTP 429; use a dedicated low-limit key or increase UNIROUTE_RATE_LIMIT_REQUESTS" >&2
    exit 1
  fi
  echo "PASS shared rate-limit smoke observed HTTP 429 across stable/canary"
}

need_cmd curl

expect_live "stable" "$STABLE_URL"
expect_ready "stable" "$STABLE_URL"
expect_live "canary" "$CANARY_URL"
expect_ready "canary" "$CANARY_URL"

if [[ -n "$PROXY_URL" ]]; then
  expect_live "proxy" "$PROXY_URL"
  expect_ready "proxy" "$PROXY_URL"
fi

if [[ "$RUN_GATEWAY_SMOKE" == "1" ]]; then
  if [[ -z "$API_KEY" ]]; then
    echo "UNIROUTE_API_KEY is required when UNIROUTE_RUN_GATEWAY_SMOKE=1" >&2
    exit 2
  fi
  gateway_call "stable" "$STABLE_URL"
  gateway_call "canary" "$CANARY_URL"
  if [[ -n "$PROXY_URL" ]]; then
    gateway_call "proxy" "$PROXY_URL" "${REQUEST_ID}-proxy"
  fi
  db_gateway_evidence
  rate_limit_smoke
else
  echo "SKIP gateway smoke; set UNIROUTE_RUN_GATEWAY_SMOKE=1 and UNIROUTE_API_KEY to enable"
fi

echo "HA smoke completed"
