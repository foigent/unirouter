# FIX-SIGNAL.md - PRD3 Batch 13 Runtime Exposure Hardening

**Date:** 2026-07-06
**Status:** Fixed awaiting Canary

PRD3 Batch 12 dependency vulnerability maintenance passed Canary 7/7 in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #76 at `977b036`;
User reported Canary/Sentinel/Atlas completed.

Codex has completed PRD3 Batch 13 as a narrow `SEC-005` existing-surface
hardening batch. It removes wildcard admin CORS, adds login-specific Redis
throttling, and makes `/metrics` runtime-configurable without adding new
product capability.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- No new API route, UI page, persistent role, RBAC authority, database
  migration, or product module was added.
- `/metrics` remains registered for route parity and Prometheus compatibility,
  but can be disabled or CIDR-restricted by runtime config.
- Login throttling uses backend `RemoteAddr`; it intentionally does not trust
  `X-Forwarded-For` or `X-Real-IP` because trusted-proxy policy is not defined.
- Metrics auth, trusted proxy headers, infrastructure inventory/capacity health,
  maintenance scheduling, SLA/SLO, and full observability platform behavior stay
  in PRD4/deployment design.
- No files were deleted.

## Verification Request

Please verify:

1. CORS is no longer wildcard/permissive:
   - `internal/middleware/cors.go` only emits the request origin when it exactly
     matches `ADMIN_CORS_ALLOWED_ORIGINS`;
   - disallowed preflight requests return 403;
   - config validation rejects wildcard, missing scheme/host, unsupported
     schemes, paths, queries, fragments, and invalid origins.
2. Login has an explicit brute-force limiter:
   - `POST /api/v1/auth/login` is wrapped with `middleware.LoginRateLimit`;
   - limiter config is exposed through `LOGIN_RATE_LIMIT_*` env vars;
   - limiter restores the request body after bounded username inspection;
   - Redis failures fail open.
3. `/metrics` exposure is runtime-configurable:
   - `METRICS_ENABLED=false` hides `/metrics` behind 404;
   - `METRICS_ALLOWED_CIDRS` restricts access by backend `RemoteAddr`;
   - docs clearly say public internet exposure should be blocked by deployment
     network policy, CIDR controls, or disabling the endpoint.
4. Docs/status are self-consistent:
   - `docs/issues/OPEN-ISSUES.md` marks `SEC-004` as `Canary passed` and
     `SEC-005` as `Fixed awaiting Canary`;
   - `docs/ops/CURRENT-STATUS.md` shows Batch 13 as the current verification
     request and Batch 12 as complete per User report;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` records Batch 13 and keeps trusted
     proxy/metrics auth/infra observability in PRD4 boundary.
5. Evidence exists at
   `docs/ops/evidence/PRD3-B13-runtime-exposure-hardening-local-20260706.md`.
6. Run these commands:

```bash
go test ./internal/config ./internal/middleware
go test ./cmd/uniroute -run 'TestAdminRoutesRegisteredForFrontendClient|TestOpenAPISpecCoversRegisteredRoutes|TestFrontendDocsPageCoversRegisteredRoutes'
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
npm --prefix frontend audit --audit-level=moderate
rg -n "ADMIN_CORS_ALLOWED_ORIGINS|LOGIN_RATE_LIMIT|METRICS_ENABLED|METRICS_ALLOWED_CIDRS" .env.example docker-compose.yml docs/ops/deployment-runbook.md docs/tech-ref/08-prometheus-metrics.md internal/config/config.go cmd/uniroute/main.go
rg -n "Access-Control-Allow-Origin.*\\*|CORS returns middleware that sets permissive|HandleFunc\\(\\\"POST /api/v1/auth/login\\\"|mux.Handle\\(\\\"GET /metrics\\\", promhttp.Handler\\(\\)" internal cmd
rg -n "SEC-005 \\| Medium \\| Fixed awaiting Canary|PRD3 Batch 13 Runtime Exposure Hardening|PRD3-B13-runtime-exposure-hardening" docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD3-B13-runtime-exposure-hardening-local-20260706.md
git diff --check
```

## Expected Result

- Go targeted tests pass.
- Route/OpenAPI/frontend-doc parity tests pass.
- Full Go tests pass.
- Full frontend tests pass.
- Frontend build passes with only the existing large chunk warning.
- `govulncheck` reports 0 code-affecting/reachable vulnerabilities; module
  vulnerabilities that are not called by this code should be recorded as
  non-affecting.
- `npm audit --audit-level=moderate` reports 0 vulnerabilities.
- Runtime exposure env/doc grep returns expected matches.
- Static anti-regression grep returns no matches.
- Status/evidence grep returns expected Batch 13 and `SEC-005` matches.
- `git diff --check` passes.
- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3
  Batch 13 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `internal/middleware/cors.go`
- `internal/middleware/login_ratelimit.go`
- `internal/middleware/metrics_access.go`
- `internal/config/config.go`
- `cmd/uniroute/main.go`
- `.env.example`
- `docker-compose.yml`
- `docs/tech-ref/05-openapi-spec.yaml`
- `docs/tech-ref/08-prometheus-metrics.md`
- `docs/ops/deployment-runbook.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B13-runtime-exposure-hardening-local-20260706.md`
- `docs/agent-workflows/FIX-SIGNAL.md`
