#!/usr/bin/env bash
set -euo pipefail

FRONTEND_BASE_URL="${FRONTEND_BASE_URL:-http://8.130.18.85:3000}"
BACKEND_BASE_URL="${BACKEND_BASE_URL:-http://8.130.18.85:8080}"
PROMETHEUS_BASE_URL="${PROMETHEUS_BASE_URL:-http://8.130.18.85:9090}"
GRAFANA_BASE_URL="${GRAFANA_BASE_URL:-http://8.130.18.85:3001}"
RUN_DEFAULT_ADMIN_CHECK="${RUN_DEFAULT_ADMIN_CHECK:-1}"
RUN_ADMIN_READ_SMOKE="${RUN_ADMIN_READ_SMOKE:-0}"

body_file="$(mktemp)"

pass() {
  printf 'PASS %s\n' "$1"
}

fail() {
  printf 'FAIL %s\n' "$1" >&2
  exit 1
}

expect_status() {
  local label="$1"
  local method="$2"
  local url="$3"
  local expected="$4"
  local output="$body_file"
  : > "$output"
  local status
  status="$(curl -sS -o "$output" -w '%{http_code}' -X "$method" "$url" || true)"
  if [[ "$status" != "$expected" ]]; then
    printf 'Expected %s for %s, got %s\n' "$expected" "$url" "$status" >&2
    sed -n '1,20p' "$output" >&2 || true
    fail "$label"
  fi
  pass "$label"
}

expect_body() {
  local label="$1"
  local url="$2"
  local pattern="$3"
  local output="$body_file"
  : > "$output"
  curl -fsS "$url" -o "$output"
  if ! grep -Fq "$pattern" "$output"; then
    printf 'Expected response from %s to contain: %s\n' "$url" "$pattern" >&2
    sed -n '1,20p' "$output" >&2 || true
    fail "$label"
  fi
  pass "$label"
}

expect_body "frontend-root" "$FRONTEND_BASE_URL/" '<div id="root"></div>'
expect_body "backend-live" "$BACKEND_BASE_URL/health/live" '"status":"ok"'
expect_body "backend-ready-prd-shape" "$BACKEND_BASE_URL/health/ready" '"db":"ok"'
expect_body "backend-metrics" "$BACKEND_BASE_URL/metrics" 'go_goroutines'
expect_body "backend-docs" "$BACKEND_BASE_URL/docs" 'UniRouter API Documentation'
expect_body "prometheus-healthy" "$PROMETHEUS_BASE_URL/-/healthy" 'Prometheus Server is Healthy'
expect_status "grafana-login" GET "$GRAFANA_BASE_URL/login" 200
expect_status "gateway-missing-key-401" GET "$FRONTEND_BASE_URL/v1/models" 401
expect_status "admin-missing-token-401" GET "$FRONTEND_BASE_URL/api/v1/channels?page=1&page_size=1" 401

if [[ "$RUN_DEFAULT_ADMIN_CHECK" == "1" ]]; then
  login_body="$body_file"
  : > "$login_body"
  login_status="$(curl -sS -o "$login_body" -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"admin123456"}' \
    "$FRONTEND_BASE_URL/api/v1/auth/login" || true)"
  if [[ "$login_status" == "200" ]]; then
    printf 'Default public admin password still works on %s\n' "$FRONTEND_BASE_URL" >&2
    fail "default-admin-password-rejected"
  fi
  if [[ "$login_status" != "401" && "$login_status" != "403" ]]; then
    printf 'Expected default admin login to be rejected with 401/403, got %s\n' "$login_status" >&2
    sed -n '1,20p' "$login_body" >&2 || true
    fail "default-admin-password-rejected"
  fi
  pass "default-admin-password-rejected"
fi

if [[ "$RUN_ADMIN_READ_SMOKE" == "1" ]]; then
  token="${ADMIN_TOKEN:-}"
  if [[ -z "$token" ]]; then
    if [[ -z "${ADMIN_USERNAME:-}" || -z "${ADMIN_PASSWORD:-}" ]]; then
      fail "admin-read-smoke requires ADMIN_TOKEN or ADMIN_USERNAME/ADMIN_PASSWORD"
    fi
    login_body="$body_file"
    : > "$login_body"
    login_status="$(curl -sS -o "$login_body" -w '%{http_code}' \
      -H 'Content-Type: application/json' \
      -d "{\"username\":\"$ADMIN_USERNAME\",\"password\":\"$ADMIN_PASSWORD\"}" \
      "$FRONTEND_BASE_URL/api/v1/auth/login" || true)"
    if [[ "$login_status" != "200" ]]; then
      fail "admin-login"
    fi
    token="$(jq -r '.token // empty' "$login_body")"
    if [[ -z "$token" ]]; then
      fail "admin-login-token"
    fi
    pass "admin-login"
  fi

  for endpoint in \
    '/api/v1/me' \
    '/api/v1/dashboard/stats' \
    '/api/v1/channels?page=1&page_size=5' \
    '/api/v1/keys?page=1&page_size=5' \
    '/api/v1/logs?page=1&page_size=5' \
    '/api/v1/wallet' \
    '/api/v1/audit-logs?page=1&page_size=5' \
    '/api/v1/settings'; do
    output="$body_file"
    : > "$output"
    status="$(curl -sS -o "$output" -w '%{http_code}' \
      -H "Authorization: Bearer $token" \
      "$FRONTEND_BASE_URL$endpoint" || true)"
    if [[ "$status" != "200" ]]; then
      printf 'Expected 200 for %s, got %s\n' "$endpoint" "$status" >&2
      sed -n '1,20p' "$output" >&2 || true
      fail "admin-read-$endpoint"
    fi
    pass "admin-read-$endpoint"
  done
fi

printf 'PRD1 cloud smoke completed.\n'
printf 'Last response body kept at %s\n' "$body_file"
