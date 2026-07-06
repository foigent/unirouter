# VERIFY-RESULT.md — PRD3 Batch 13 Runtime Exposure Hardening

**Date:** 2026-07-06
**Verdict:** ✅ PASS 7/7

## Verification Summary

| # | Check | Result | Evidence |
|---|-------|--------|----------|
| 1 | CORS no longer wildcard | ✅ PASS | `internal/middleware/cors.go`: uses exact-origin allowlist from `ADMIN_CORS_ALLOWED_ORIGINS`; disallowed preflight returns 403; no `*` pattern anywhere |
| 2 | Login has brute-force limiter | ✅ PASS | `cmd/uniroute/main.go` line 240: `POST /api/v1/auth/login` wrapped with `loginRateLimiter`; config via `LOGIN_RATE_LIMIT_*` env vars; Redis fails open; body restored after bounded inspection |
| 3 | `/metrics` runtime-configurable | ✅ PASS | `cmd/uniroute/main.go` lines 179-187: `METRICS_ENABLED=false` → 404; `METRICS_ALLOWED_CIDRS` restricts by RemoteAddr; docs warn against public internet exposure |
| 4 | Status docs self-consistent | ✅ PASS | OPEN-ISSUES: SEC-004 "Canary passed", SEC-005 "Fixed awaiting Canary"; CURRENT-STATUS: Batch 13 "Fixed awaiting Canary"; ACCEPTANCE-MATRIX: SEC-005 "Fixed awaiting Canary in Batch 13", trusted proxy/metrics auth in PRD4 boundary |
| 5 | Evidence file exists | ✅ PASS | `docs/ops/evidence/PRD3-B13-runtime-exposure-hardening-local-20260706.md` present; documents CORS/login/metrics policy, Go 1.25 boundary, PRD4 deferrals |
| 6 | All regression commands pass | ✅ PASS | See command table below |
| 7 | Anti-regression grep clean | ✅ PASS | No wildcard CORS, no stale permissive comment, no unwrapped login, no direct promhttp.Handler() |

## Commands

| Command | Result |
|---------|--------|
| `go test ./internal/config ./internal/middleware` | PASS ✅ |
| `go test ./cmd/uniroute -run 'TestAdminRoutesRegistered...\|TestOpenAPISpec...\|TestFrontendDocs...'` | 3/3 PASS ✅ |
| `go test ./...` | 20 packages PASS ✅ |
| `npm --prefix frontend test -- --run` | 20 files / 103 tests PASS ✅ |
| `npm --prefix frontend run build` | PASS (Vite 6.4.3, existing large chunk warning) ✅ |
| `govulncheck ./...` | 0 reachable vulnerabilities ✅ |
| `npm audit --audit-level=moderate` | 0 vulnerabilities ✅ |
| Env/config/docs grep for `ADMIN_CORS_ALLOWED_ORIGINS`, `LOGIN_RATE_LIMIT_*`, `METRICS_*` | Matches in .env.example, docker-compose.yml, deployment-runbook.md, prometheus-metrics.md, config.go, main.go ✅ |
| Anti-regression grep (wildcard CORS, unwrapped login, direct promhttp) | No matches ✅ |
| Status/evidence grep for Batch 13, SEC-005 | Matches in all 4 files ✅ |
| `git diff --check` | PASS (clean) ✅ |

## Boundaries Verified

- No new API route, UI page, persistent role, RBAC authority, migration, or product module added.
- `/metrics` remains registered for route parity; can be disabled or CIDR-restricted.
- Login throttling uses backend `RemoteAddr`; does not trust `X-Forwarded-For`/`X-Real-IP`.
- Trusted proxy, metrics auth, infra observability, SLA/SLO deferred to PRD4.
- No files deleted.

## New Issues

None.

## Conclusion

PRD3 Batch 13 PASS. Wildcard CORS removed, login brute-force limiter added, `/metrics` made runtime-configurable. All tests, security scans, and build pass. SEC-005 closed as existing-surface hardening; PRD4 boundaries preserved.
