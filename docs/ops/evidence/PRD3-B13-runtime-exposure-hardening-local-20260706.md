# PRD3 Batch 13 Runtime Exposure Hardening Local Evidence

Date: 2026-07-06

## Scope

PRD3 Batch 13 closes `SEC-005` as an existing-surface hardening batch. It does
not add a new product module, new role surface, new deployment product, or PRD4
operations platform capability.

Inputs:

- User reported PRD3 Batch 12 Canary/Sentinel/Atlas completed.
- `docs/ops/evidence/COMPREHENSIVE-TEST-REPORT-20260705.md` identified three
  runtime exposure risks: wildcard CORS, no login-specific brute-force limiter,
  and unauthenticated `/metrics` exposure.
- `docs/issues/OPEN-ISSUES.md` tracked the risk as `SEC-005`.

## Implemented Policy

### Admin CORS

- `ADMIN_CORS_ALLOWED_ORIGINS` is now an explicit exact-origin allowlist.
- Wildcard CORS is no longer emitted by the backend middleware.
- Empty allowlist means no cross-origin admin browser access is allowed; same
  origin or reverse-proxy deployments continue to work without CORS headers.
- Origins must use `http` or `https` and include a host. A trailing slash is
  tolerated and normalized, but unsupported schemes, paths, query strings,
  fragments, and wildcard entries are rejected by config validation.

### Login Rate Limit

- `POST /api/v1/auth/login` is wrapped by a Redis-backed fixed-window limiter.
- Defaults:
  - `LOGIN_RATE_LIMIT_ENABLED=true`
  - `LOGIN_RATE_LIMIT_WINDOW_SECONDS=60`
  - `LOGIN_RATE_LIMIT_IP_REQUESTS=30`
  - `LOGIN_RATE_LIMIT_USER_REQUESTS=10`
- The limiter keys by backend `RemoteAddr` and by `RemoteAddr + normalized
  username`; it does not trust `X-Forwarded-For` or `X-Real-IP` because no
  trusted-proxy policy exists yet.
- Redis errors fail open to avoid turning Redis instability into an admin login
  outage.
- Oversized or malformed request bodies still pass through to the login handler;
  the limiter restores the request body after bounded inspection.

### Metrics Exposure

- `/metrics` remains registered for route and Prometheus compatibility.
- `METRICS_ENABLED=false` hides the endpoint behind a 404 response.
- `METRICS_ALLOWED_CIDRS` optionally restricts backend `RemoteAddr` access and
  returns 404 for disallowed clients.
- Production deployments should keep backend `:8080/metrics` off the public
  internet through network controls, reverse-proxy policy, or the CIDR/disable
  options above.
- Metrics basic auth, mTLS, proxy-auth integration, and full observability
  platform design are PRD4/deployment topics, not PRD3 scope.

## Files Changed

- Runtime/config:
  - `internal/config/config.go`
  - `cmd/uniroute/main.go`
  - `.env.example`
  - `docker-compose.yml`
- Middleware/tests:
  - `internal/middleware/cors.go`
  - `internal/middleware/login_ratelimit.go`
  - `internal/middleware/metrics_access.go`
  - `internal/config/config_test.go`
  - `internal/middleware/ratelimit_test.go`
  - `internal/middleware/utils_test.go`
- Docs/status:
  - `docs/tech-ref/05-openapi-spec.yaml`
  - `docs/tech-ref/08-prometheus-metrics.md`
  - `docs/ops/deployment-runbook.md`
  - `docs/ops/CURRENT-STATUS.md`
  - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
  - `docs/issues/OPEN-ISSUES.md`
  - `docs/agent-workflows/FIX-SIGNAL.md`

## Boundaries

- No new API route was added.
- No RBAC permission model, persistent role, user workflow, or UI page was
  added.
- No database migration was added.
- No infrastructure inventory, capacity dashboard, maintenance scheduler,
  SLA/SLO system, metrics authentication product, or trusted-proxy framework was
  implemented.
- PRD4 should decide trusted proxy headers, production metrics auth strategy,
  infrastructure observability source of truth, and any richer incident/SLO
  lifecycle.

## Local Validation

Commands run by Codex:

```bash
go test ./internal/config ./internal/middleware
go test ./cmd/uniroute -run 'TestAdminRoutesRegisteredForFrontendClient|TestOpenAPISpecCoversRegisteredRoutes|TestFrontendDocsPageCoversRegisteredRoutes'
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
npm --prefix frontend audit --audit-level=moderate
rg -n "Access-Control-Allow-Origin.*\\*|CORS returns middleware that sets permissive|HandleFunc\\(\\\"POST /api/v1/auth/login\\\"|mux.Handle\\(\\\"GET /metrics\\\", promhttp.Handler\\(\\)" internal cmd
git diff --check
```

Observed local result:

- Go targeted tests pass.
- Route/OpenAPI/frontend-doc parity tests pass.
- Full Go tests pass.
- Full frontend tests pass.
- Frontend build passes with only the existing large chunk warning.
- `govulncheck` reports 0 code-affecting vulnerabilities; it also reports 19
  vulnerabilities in required modules that this code does not call.
- `npm audit --audit-level=moderate` reports 0 vulnerabilities.
- Static grep finds no wildcard CORS setter, no stale permissive CORS comment,
  no direct unwrapped login registration, and no direct `/metrics`
  `promhttp.Handler()` registration.
- `git diff --check` passes.
