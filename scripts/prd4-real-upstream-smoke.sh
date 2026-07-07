#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v go >/dev/null 2>&1; then
  echo "go is required" >&2
  exit 1
fi

if ! command -v node >/dev/null 2>&1; then
  echo "node is required" >&2
  exit 1
fi

if [[ "${1:-}" == "--preflight" ]]; then
  export UNIROUTE_PRD4_UPSTREAM_PREFLIGHT_ONLY=true
  shift
fi

node scripts/prd4_real_upstream_smoke.mjs "$@"
