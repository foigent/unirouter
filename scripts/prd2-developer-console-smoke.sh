#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v go >/dev/null 2>&1; then
  echo "go is required" >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required" >&2
  exit 1
fi

if [[ -z "${ENCRYPTION_KEY:-}${UNIROUTE_SMOKE_ENCRYPTION_KEY:-}" ]]; then
  echo "ENCRYPTION_KEY or UNIROUTE_SMOKE_ENCRYPTION_KEY must be set to the running backend's 32-byte key" >&2
  exit 1
fi

: "${UNIROUTE_SMOKE_BACKEND_URL:=http://localhost:8080}"
: "${UNIROUTE_SMOKE_FRONTEND_URL:=http://localhost:3000}"

export UNIROUTE_SMOKE_BACKEND_URL
export UNIROUTE_SMOKE_FRONTEND_URL

if [[ ! -d frontend/node_modules/playwright ]]; then
  npm --prefix frontend install
fi

if [[ "${UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL:-false}" != "true" ]]; then
  frontend/node_modules/.bin/playwright install chromium
fi

node scripts/prd2_developer_console_smoke.mjs
