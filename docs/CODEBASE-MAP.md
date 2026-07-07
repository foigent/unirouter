# UniRouter Codebase Map

> Purpose: help agents locate implementation quickly without rediscovering the repository from scratch.

## Runtime Entry

| Area | Path | Notes |
|------|------|-------|
| Main server | `cmd/uniroute/main.go` | Loads config, connects Postgres/Redis, runs migrations, wires gateway/admin routes, starts background cleanup and alerts |
| Main tests | `cmd/uniroute/main_test.go` | Top-level route behavior tests |
| Config | `internal/config/config.go` | Environment config and validation, including `JWT_SECRET`, `API_KEY_HASH_SECRET`, `ENCRYPTION_KEY`, optional `LICENSE_PUBLIC_KEY`, PRD4 trace event sampling/retention config, and Runtime Status metadata/Prometheus/Grafana URLs |
| Database | `internal/db/postgres.go` | pgx pool setup and health |
| Redis | `internal/redisx/redis.go` | Redis setup, health, wallet cache helpers |
| Migrations | `migrations/*.sql` | Base schema plus wallet/schema/PRD alignment migrations, including PRD4 API key owner usage attribution in `000016_prd4_key_owner_usage` and request trace events in `000017_prd4_request_trace_events` |

## Backend Packages

| Package | Responsibility |
|---------|----------------|
| `internal/admin` | Admin REST handlers: auth, channels, channel keys, API keys, wallet, grants, logs, dashboard, prices, users, settings, audit, sensitive words, channel tests, and Runtime Status. Runtime Status includes PRD4 app/dependency/traffic health, host CPU/disk/network summaries, and Prometheus/Grafana active checks. API key handlers expose PRD4 `rps_limit` / structured `quota` fields while preserving legacy `qps_limit` compatibility. Shared package-local helpers are split into `http_helpers.go`, `event_helpers.go`, and `model_helpers.go` |
| `internal/adapters` | Provider adapter registry and OpenAI-compatible / Anthropic-native request/response conversion |
| `internal/auth` | JWT generation/validation, API key hashing, upstream key AES-GCM encryption/decryption |
| `internal/billing` | Cost calculation, freeze/settle/cancel, price provider, budget alert, notification event service |
| `internal/circuitbreaker` | Standalone circuit breaker implementation and tests |
| `internal/filter` | DFA sensitive-word filter |
| `internal/errdocs` | Error documentation registry used by gateway `doc_url` responses and API Docs |
| `internal/gateway` | Model routing, upstream relay, chat completions, embeddings, model listing, request logging, metadata-only request trace events |
| `internal/licensing` | Offline license document parsing, Ed25519 signature verification, expiry status, and warning calculation |
| `internal/logger` | Logging helpers |
| `internal/metering` | Usage-source constants and token-metering source classification |
| `internal/middleware` | Gateway auth, admin auth, rate limit, admission control, CORS, Prometheus metrics |
| `internal/migrate` | SQL migration runner |
| `internal/modelgov` | Model governance helpers and access checks |
| `internal/models` | Shared data model types |
| `internal/oidc` | OIDC discovery, token exchange, JWKS verification, and identity extraction |
| `internal/testutil` | Mock DB and test DB helpers |
| `internal/tokenutil` | Token estimation and stream counting |

## Main Backend Routes

Defined in `cmd/uniroute/main.go`.

| Route Group | Routes |
|-------------|--------|
| Health | `GET /`, `GET /health/live`, `GET /health/ready` |
| Metrics | `GET /metrics` |
| Gateway | `POST /v1/chat/completions`, `POST /v1/embeddings`, `GET /v1/models` |
| Auth | `POST /api/v1/auth/login`, `GET /api/v1/auth/oidc/config`, `GET /api/v1/auth/oidc/start`, `GET /api/v1/auth/oidc/callback`, `GET /api/v1/me`, `PUT /api/v1/me/password` |
| Channels | `GET/POST /api/v1/channels`, `GET/PUT/DELETE /api/v1/channels/{id}`, `POST /api/v1/channels/{id}/test` |
| API keys | `GET/POST /api/v1/keys`, `GET/PUT /api/v1/keys/{id}`, `POST /api/v1/keys/{id}/disable` |
| Channel keys | `GET/POST /api/v1/channel-keys`, `PUT/DELETE /api/v1/channel-keys/{id}` |
| Wallet | `GET /api/v1/wallet`, `POST /api/v1/wallet/topup`, `POST /api/v1/wallet/adjust`, `GET /api/v1/wallet/transactions`, `GET /api/v1/wallet/transactions/export` |
| Logs/dashboard | `GET /api/v1/logs`, `GET /api/v1/logs/{id}`, `GET /api/v1/logs/{id}/trace`, `GET /api/v1/dashboard/stats` |
| Product status/analytics/docs | `GET /api/v1/usage/analytics`, `GET /api/v1/internal-status`, `GET /api/v1/runtime/status` for runtime/capacity/external monitoring state, `GET /api/v1/error-docs` |
| Projects/model governance | Project CRUD, project model-set bindings, model registry, model aliases, model sets, approved model catalog |
| Pricing | `GET/POST /api/v1/model-prices`, `PUT/DELETE /api/v1/model-prices/{id}`, `GET /api/v1/model-names`, `GET/POST /api/v1/org-model-prices`, `PUT/DELETE /api/v1/org-model-prices/{id}` |
| Notifications | Notification events, deliveries, and webhook CRUD |
| Users/access | User CRUD, grants, audit logs |
| Settings/filter | Settings CRUD and sensitive-word CRUD |
| License | `GET/PUT /api/v1/license` |
| Docs | `GET /docs` simple HTML endpoint |

Contract guard: `cmd/uniroute/route_parity_test.go` checks frontend client-required routes, OpenAPI route coverage, and frontend `/docs` route coverage against registered backend route strings.

OpenAPI snapshot: `docs/tech-ref/05-openapi-spec.yaml` is the current route-level contract for active UniRouter routes. Schema detail remains intentionally generic where no stable typed response contract has been defined.

## Smoke And Evidence Scripts

| Script | Responsibility |
|--------|----------------|
| `scripts/prd4-real-upstream-smoke.sh` | PRD4 Batch 1H wrapper for real upstream evidence. `--preflight` is safe without credentials and does not mutate the database or call providers |
| `scripts/prd4_real_upstream_seed.go` | Guarded target-evidence fixture seed for OpenRouter-compatible and direct Anthropic-native channels, model governance rows, wallet, and API key |
| `scripts/prd4_real_upstream_smoke.mjs` | Real-provider gateway runner that checks non-stream/stream calls, call logs, billing settlement, trace events, and sanitized JSON evidence |
| `scripts/prd4-real-sso-smoke.sh` | PRD4 Batch 1I wrapper for real SSO evidence. `--preflight` is safe without target credentials and does not drive a browser, call an IdP, or read DB |
| `scripts/prd4_real_sso_smoke.mjs` | Real SSO browser runner for Logto or another enterprise OIDC target; checks OIDC config/discovery, browser login, `/me`, audit mapping, and sanitized JSON evidence |
| `scripts/prd4_real_sso_audit_check.go` | Guarded read-only DB evidence check for OIDC user mapping and `login.oidc` audit redaction |

## Frontend Map

| Area | Path |
|------|------|
| App routes | `frontend/src/main.tsx` |
| Sidebar/navigation shell | `frontend/src/App.tsx` |
| API client | `frontend/src/api/client.ts` |
| Auth store | `frontend/src/store/auth.ts` |
| Shared UI primitives | `frontend/src/ui/` (`PageHeader`, `Surface`, `EmptyState`, `MoneyText`, `LatencyText`, `StatusTag`, `PagedTable`) |
| Shared UI utilities | `frontend/src/utils/` (`displayLabels`, `format`, `quota`) |
| Pages | `frontend/src/pages/*.tsx` |
| Shared error boundary | `frontend/src/components/ErrorBoundary.tsx` |
| Global CSS | `frontend/src/index.css` |
| Tests | `frontend/src/**/__tests__/*.test.tsx`, `frontend/src/**/__tests__/*.test.ts` |

Frontend route pages currently include dashboard, channels, channel keys, API keys, developer console, usage analytics, internal status, notification center, model governance, wallet, logs, users, model prices, org model prices, grants, audit logs, reconciliation, license, test page, settings, API docs, access state, and login.

## Data Model

Base schema is in `migrations/000001_init_schema.up.sql`. Main tables:

| Domain | Tables |
|--------|--------|
| Tenant/user | `orgs`, `users` |
| Routing | `channels`, `channel_keys` |
| Client access | `api_keys` |
| Pricing | `model_prices`, `org_model_prices` |
| Billing | `wallets`, `transactions`, `grants`, `freeze_idempotency` |
| Observability | `call_logs`, `request_trace_events`, `audit_logs` |
| Configuration | `settings` |

Later migrations add wallet features, org pricing and alert fields, schema gap fixes, and PRD alignment fields like `call_logs.requested_model`, `call_logs.ip_address`, `audit_logs.source`, and `wallets.freeze_expire_at`. PRD4 migration `000016_prd4_key_owner_usage` adds `api_keys.owner_user_id`, `api_keys.key_type`, `call_logs.owner_user_id`, and `call_logs.key_type`; gateway request logging snapshots owner/type at request time so historical usage does not drift when key ownership changes. PRD4 migration `000017_prd4_request_trace_events` adds metadata-only request trace events keyed by `request_id`, with org/project/key/owner snapshots and stage/severity/provider/status/latency metadata.

Request trace lookup is implemented by `internal/admin/log_handlers.go` as `GET /api/v1/logs/{id}/trace`. It loads the call log first, applies existing project usage RBAC, then filters trace events by `request_id`, `org_id`, and `project_id`. Frontend support is in `frontend/src/api/client.ts` (`getLogTrace`) and `frontend/src/pages/LogsPage.tsx` (request path card in the log detail inspector).

Usage analytics is implemented in `internal/admin/prd2_product_handlers.go` and now supports project/key/model/provider/channel/status/source filters plus PRD4 `scope=my`, `owner_user_id`, `key_type`, `group_by=owner`, and `group_by=key_type`.

API key quota visibility keeps `api_keys.qps_limit` as the compatibility
storage name for per-key RPS. PRD4 Batch 1E API responses also expose preferred
`rps_limit` and a structured `quota` payload; frontend key pages render
RPM/RPS/TPM via `frontend/src/utils/quota.ts`.

## Security Notes

Current code uses AES-GCM helpers in `internal/auth/auth.go` for upstream key encryption/decryption, and config enforces a 32-byte `ENCRYPTION_KEY`. API Key `key_hash` uses a stable HMAC secret from `API_KEY_HASH_SECRET`; if the variable is unset, config falls back to `JWT_SECRET` for backward compatibility. Historical docs that mention XOR encryption or API Key hashes based on `ENCRYPTION_KEY` are stale unless current code proves otherwise.

Admin auth and gateway auth are separate:

- Admin: JWT via `middleware.AdminAuth`.
- Gateway: API key via `middleware.GatewayAuthWithAPIKeyHashSecret`.

## Deployment And Operations

| File | Purpose |
|------|---------|
| `Dockerfile` | Builds single Go backend binary |
| `frontend/Dockerfile` | Builds frontend static site served by nginx |
| `docker-compose.yml` | Postgres, Redis, backend, frontend, Prometheus, Grafana |
| `docker-compose.canary.yml` | Nginx and canary backend override |
| `nginx/nginx.conf` | Reverse proxy/canary routing |
| `infra/prometheus/prometheus.yml` | Prometheus scrape config |
| `infra/grafana/dashboards/unirouter-overview.json` | Grafana dashboard |
| `infra/loadtest/k6-chat-completions.js` | k6 load test |

## Verification Commands

```bash
go test ./... -count=1
go vet ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Potentially environment-dependent tests use a real database only when configured by test helpers. Read `internal/testutil/testdb.go` before assuming integration tests always run.
