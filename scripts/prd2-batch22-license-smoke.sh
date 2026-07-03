#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ "${1:-}" == "--print-public-key" ]]; then
  go run ./scripts/prd2_batch22_license_seed.go -action print-public-key
  exit 0
fi

if ! command -v go >/dev/null 2>&1; then
  echo "go is required" >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required" >&2
  exit 1
fi

if ! command -v npx >/dev/null 2>&1; then
  echo "npx is required" >&2
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

node scripts/prd2_batch22_license_smoke.mjs
