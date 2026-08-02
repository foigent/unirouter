#!/usr/bin/env bash
set -euo pipefail

: "${UNIROUTER_BASE_URL:?Set UNIROUTER_BASE_URL without a trailing /v1 path, for example https://gateway.example.com}"
: "${UNIROUTER_API_KEY:?Set UNIROUTER_API_KEY}"
: "${UNIROUTER_MODEL:?Set UNIROUTER_MODEL}"

base_url="${UNIROUTER_BASE_URL%/}"
case "$base_url" in
  https://*) ;;
  *) printf 'FAIL: POC Gateway Endpoint must use HTTPS\n' >&2; exit 1 ;;
esac

require_status() {
  local name="$1"
  local expected="$2"
  shift 2
  local actual
  actual="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "$@")"
  if [[ "$actual" != "$expected" ]]; then
    printf 'FAIL: %s expected HTTP %s, got %s\n' "$name" "$expected" "$actual" >&2
    exit 1
  fi
  printf 'PASS: %s HTTP %s\n' "$name" "$actual"
}

require_status readiness 200 "$base_url/health/ready"
require_status invalid_auth 401 -H 'Authorization: Bearer invalid-poc-key' "$base_url/v1/models"
require_status model_catalog 200 -H "Authorization: Bearer $UNIROUTER_API_KEY" "$base_url/v1/models"
require_status chat_completion 200 \
  -H "Authorization: Bearer $UNIROUTER_API_KEY" \
  -H 'Content-Type: application/json' \
  --data "{\"model\":\"$UNIROUTER_MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly: unirouter-ok\"}],\"stream\":false}" \
  "$base_url/v1/chat/completions"

printf 'PASS: protocol preflight complete; run exact SDK clients before promoting evidence above protocol_compatible\n'
