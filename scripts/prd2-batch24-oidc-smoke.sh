#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute -count=1
npm --prefix frontend test -- --run LoginPage client navigation

cat <<'JSON'
{"status":"PASS","batch":24,"scope":"oidc_mock_provider_and_frontend_entry"}
JSON
