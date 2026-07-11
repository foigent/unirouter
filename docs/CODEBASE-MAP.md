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
| Migrations | `migrations/*.sql` | Base schema plus wallet/schema/PRD alignment migrations, including PRD4 API key owner usage attribution in `000016`, request trace events in `000017`, PRD5 structural evidence fields in `000018`, PRD5 Data Exchange Control ledger in `000019`, and PRD6 historical Webhook scope quarantine in `000020` |

## Backend Packages

| Package | Responsibility |
|---------|----------------|
| `internal/admin` | Admin REST handlers including Logs/trace, Data Exchange Control, audit, and Runtime Status. Runtime Status includes app/dependency/traffic/host telemetry, external monitoring checks, and a redacted current-process logical topology. Data Exchange Control is platform-admin-only and records source-side approvals/exchanges/deliveries/receipts without automatic export or X-Insight runtime dependency; receipt writes accept only the canonical v1 warning registry and reject unimplemented durable job/deep-link fields |
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
| `internal/middleware` | Gateway auth, admin auth, rate limit, admission control, CORS, baseline security headers, Prometheus metrics |
| `internal/migrate` | SQL migration runner |
| `internal/modelgov` | Model governance helpers and access checks |
| `internal/models` | Shared data model types |
| `internal/traceutil` | Shared request trace metadata sanitizer and PRD5 capture-gap helper used by gateway trace writes and admin trace responses |
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
| Data exchange | Platform-admin approval candidates and exchange/delivery/receipt/delete ledger under `/api/v1/data-exchange/*` |
| Docs | `GET /docs` simple HTML endpoint |

Contract guard: `cmd/uniroute/route_parity_test.go` checks frontend client-required routes, OpenAPI route coverage, and frontend `/docs` route coverage against registered backend route strings.

OpenAPI snapshot: `docs/tech-ref/05-openapi-spec.yaml` is the current route-level contract for active UniRouter routes. Schema detail remains intentionally generic where no stable typed response contract has been defined.

## Smoke And Evidence Scripts

| Script | Responsibility |
|--------|----------------|
| `scripts/prd4-real-upstream-smoke.sh` | PRD4 Batch 1H wrapper for real upstream evidence harness. `--preflight` is safe without credentials and does not mutate the database or call providers; real target execution is deferred to PRD5 |
| `scripts/prd4_real_upstream_seed.go` | Guarded target-evidence fixture seed for OpenRouter-compatible and direct Anthropic-native channels, model governance rows, wallet, and API key |
| `scripts/prd4_real_upstream_smoke.mjs` | Real-provider gateway runner that checks non-stream/stream calls, call logs, billing settlement, trace events, and sanitized JSON evidence |
| `scripts/prd4-real-sso-smoke.sh` | PRD4 Batch 1I wrapper for real SSO evidence harness. `--preflight` is safe without target credentials and does not drive a browser, call an IdP, or read DB; real target execution is deferred to PRD5 |
| `scripts/prd4_real_sso_smoke.mjs` | Real SSO browser runner for future Logto or another enterprise OIDC target evidence; checks OIDC config/discovery, browser login, `/me`, audit mapping, and sanitized JSON evidence when PRD5 target execution is reopened |
| `scripts/prd4_real_sso_audit_check.go` | Guarded read-only DB evidence check for OIDC user mapping and `login.oidc` audit redaction |
| `scripts/prd5_xinsight_snapshot.go` | PRD5 Batch 1A read-only sanitized snapshot generator for explicit `request_id` selections from `call_logs` and `request_trace_events`; Batch 1K adds optional DSN-free Markdown handoff-note output; Batch 1N adds `candidate_capture_gaps`; Batch 1Q adds `--approval-manifest`; Batch 1T adds `exchange_id`, exact-byte `approval_manifest_sha256`, and manifest-preserving handoff commands; no broad table export, raw prompt/response, API key, raw IP, raw upstream error, DSN, or credential output |
| `scripts/prd5_xinsight_snapshot_test.go` | PRD5 Batch 1T focused tests for exchange ID format/uniqueness, snapshot-envelope hash boundary, exact manifest hash, source-instance binding, trailing JSON rejection, and manifest-mode handoff output |
| `docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md` | PRD5 Batch 1R non-technical request ID approval runbook for Gate A sanitized snapshot data-minimization decisions |
| `docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md` | PRD5 Batch 1T runbook and Batch 1V accepted authorization `UR-XI-GATE-A-20260710-01` for Atlas-only scenario execution using unique `X-Request-ID` values without broad discovery |
| `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md` | PRD5 Batch 1S product/architecture decision for a future UniRouter-owned Data Exchange Control surface covering outbound approval/export ledger/import receipts and excluding X-Insight read-side audit/investigation UI |

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

Frontend route pages currently include dashboard, channels, channel keys, API keys, developer console, usage analytics, internal status, notification center, model governance, wallet, logs, data exchange, users, model prices, org model prices, grants, audit logs, reconciliation, license, test page, settings, API docs, access state, and login.

## Data Model

Base schema is in `migrations/000001_init_schema.up.sql`. Main tables:

| Domain | Tables |
|--------|--------|
| Tenant/user | `orgs`, `users` |
| Routing | `channels`, `channel_keys` |
| Client access | `api_keys` |
| Pricing | `model_prices`, `org_model_prices` |
| Billing | `wallets`, `transactions`, `grants`, `freeze_idempotency` |
| Observability/control | `call_logs`, `request_trace_events`, `audit_logs`, `integration_exchange_items`, `integration_exchanges`, `integration_exchange_members`, `integration_exchange_deliveries`, `integration_exchange_receipts` |
| Configuration | `settings` |

Later migrations add wallet features, org pricing and alert fields, schema gap fixes, and PRD alignment fields like `call_logs.requested_model`, `call_logs.ip_address`, `audit_logs.source`, and `wallets.freeze_expire_at`. PRD4 migration `000016_prd4_key_owner_usage` adds `api_keys.owner_user_id`, `api_keys.key_type`, `call_logs.owner_user_id`, and `call_logs.key_type`; gateway request logging snapshots owner/type at request time so historical usage does not drift when key ownership changes. PRD4 migration `000017_prd4_request_trace_events` adds metadata-only request trace events keyed by `request_id`, with org/project/key/owner snapshots and stage/severity/provider/status/latency metadata. PRD5 migration `000018_prd5_structural_evidence_fields` adds nullable/default trace/span/source/capture/decision/actor/session/evidence/payload fields to request trace events and selected call-log evidence fields.

Request trace lookup is implemented by `internal/admin/log_handlers.go` as `GET /api/v1/logs/{id}/trace`. It loads the call log first, applies existing project usage RBAC, filters trace events by `request_id`, `org_id`, and `project_id`, then sanitizes metadata through `internal/traceutil` before returning it. PRD5 Batch 1B/1C extends that existing route with structural evidence fields and renders them in `frontend/src/pages/LogsPage.tsx` as request-level evidence quality plus per-event tags. Frontend support is in `frontend/src/api/client.ts` (`getLogTrace`) and `frontend/src/pages/LogsPage.tsx`.

PRD5 Batch 1D upstream request correlation is implemented in
`internal/gateway/relay.go` and `internal/gateway/trace.go`. UniRouter extracts
provider-returned upstream request IDs from a bounded response-header allowlist,
sanitizes the value, then writes it to provider response trace events and
`call_logs.upstream_request_id`. It does not trust inbound public request
headers as evidence authority.

PRD5 Batch 1E event capture-gap emission is implemented in
`internal/traceutil/metadata.go` and `internal/gateway/trace.go`. The trace
metadata sanitizer preserves safe scalar allowlisted metadata and returns a
`redacted` capture gap when restricted keys, unsupported value shapes, or
truncated strings are dropped. Gateway trace writes persist that gap in
`request_trace_events.capture_gap` and include it in the trace source-event
hash.

PRD5 Batch 1F payload capture mode configuration is implemented in
`internal/traceutil/payload.go`, `internal/config/config.go`,
`cmd/uniroute/main.go`, `internal/gateway/relay.go`, and
`internal/gateway/trace.go`. `PAYLOAD_CAPTURE_MODE` defaults to `metadata_only`,
accepts only `metadata_only` and `disabled`, and rejects unsupported values.
Gateway trace/log writers persist the configured `payload_capture_mode` and
include it in source-event hashes. This does not enable raw prompt/response
capture.

PRD5 Batch 1G trusted evidence context intake is implemented in
`internal/traceutil/evidence_context.go`, `internal/config/config.go`,
`cmd/uniroute/main.go`, `internal/gateway/relay.go`, and
`internal/gateway/trace.go`. `EVIDENCE_CONTEXT_TRUST_SECRET` defaults empty,
which makes UniRouter ignore inbound evidence context headers. When configured,
`X-UniRouter-Evidence-Auth` must match before UniRouter accepts bounded
`X-UniRouter-*` trace/session/actor/delegation headers or trusted `Traceparent`
fallback. Trace/log writers persist those fields into existing structural
columns and include them in source-event hashes.

PRD5 Batch 1H gateway span hierarchy is implemented in
`internal/gateway/trace.go`, `internal/gateway/relay.go`, and focused gateway
tests. Gateway request contexts carry an in-memory span state: the first
UniRouter-emitted trace event becomes the request gateway root span, preserves a
trusted inbound parent span when present, and later UniRouter trace events parent
to the gateway root. This uses existing `parent_span_id` fields and does not add
a migration, route, frontend behavior, exporter/outbox, raw payload capture, or
X-Insight runtime dependency.

PRD5 Batch 1I payload policy decision evidence is implemented in
`internal/gateway/trace.go`, `internal/gateway/relay.go`,
`internal/gateway/relay_mock_test.go`, and `internal/traceutil/metadata.go`.
Chat and embeddings requests emit a `payload.policy.applied` trace event after
gateway request acceptance. The event writes sanitized policy metadata,
`decision_kind=payload_capture`, `raw_payload_stored=false`, and
`payload_ref_present=false`; it does not inspect, persist, hash, export, or
reference raw prompt/response bodies.

PRD5 Batch 1J provider upstream capture-gap evidence is implemented in
`internal/gateway/trace.go` and covered by `internal/gateway/relay_mock_test.go`.
Provider response trace events now write `capture_gap=provider_not_returned`
when no sanitized provider-returned `upstream_request_id` is available. Existing
returned upstream request IDs still populate `upstream_request_id`, and existing
metadata redaction gaps keep `capture_gap=redacted`.

PRD5 Batch 1K Gate A snapshot handoff-note support is implemented in
`scripts/prd5_xinsight_snapshot.go`. The snapshot tool can write an optional
Markdown handoff note with selected request IDs, scenario labels, counts, safety
confirmation, snapshot output path, and command shape without DSN. It rejects
stdout for note output, reuses `--overwrite`, and does not change runtime
request handling, routes, frontend behavior, migrations, read-only credential
handoff, exporter/outbox, or X-Insight runtime dependency.

PRD5 Batch 1L component span refinement is implemented in
`internal/gateway/trace.go` and covered by
`internal/gateway/relay_mock_test.go`. Gateway trace state now keeps the request
gateway root plus first-span-per-`source_component` entries. First component
events parent to the gateway root; repeated same-component events parent to that
component's first span. This refines UniRouter-local evidence hierarchy without
adding schema, route, frontend, exporter/outbox, raw payload, or X-Insight
runtime dependency.

PRD5 Batch 1M provider failure capture gaps are implemented in
`internal/gateway/trace.go` and `internal/gateway/relay.go`, with focused
coverage in `internal/gateway/relay_mock_test.go`. Provider response events keep
`capture_gap=provider_not_returned` when a response lacks a usable upstream
request ID; provider-stage failures before a response source exists write
`capture_gap=not_available_from_source`; metadata redaction gaps keep priority.

PRD5 Batch 1N snapshot capture-gap candidates are implemented in
`scripts/prd5_xinsight_snapshot.go`. Generated Gate A snapshot gap records now
include optional `candidate_capture_gaps`, and Markdown handoff notes include a
capture-gap table. Missing trace rows with an existing call log list
`sampled_out`, `retention_expired`, and `not_instrumented` as candidates without
asserting one definitive cause.

PRD5 Batch 1Q Gate A request approval manifest support is implemented in
`scripts/prd5_xinsight_snapshot.go`. The snapshot tool can use
`--approval-manifest <path>` with schema
`unirouter.prd5.xinsight.approval.v1`; only rows marked `approved` with concrete
request IDs are exported, while `pending` and `unavailable` rows are not
exported. Manifest mode is mutually exclusive with direct `--request-id`,
`--request-ids`, and `--scenario` flags.

PRD5 Batch 1R Gate A request ID approval runbook is documented in
`docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md`, with the shared approval
worksheet at
`/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md`.
It explains request IDs as single-request receipts, the `approved` / `pending` /
`unavailable` meanings, and why auto-discovery, read-only DB/view, and
exporter/outbox are deferred beyond the first Gate A sample approval.

PRD5 Batch 1S X-Insight Data Exchange Control Surface is documented in
`docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md`, with the shared X-Insight
review request at
`/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-data-exchange-control-surface-review.md`.
It recommends a future UniRouter control surface for outbound approvals, export
ledger, handoff artifacts, import receipts, and deep links. It does not add UI
code, API routes, migrations, snapshot artifacts, read-only DB/view access,
shared ECS staging, exporter/outbox, X-Insight runtime dependency, or PRD4
changes.

PRD5 Batch 1T Gate A exchange identity is implemented in
`scripts/prd5_xinsight_snapshot.go`, with focused explicit-file tests in
`scripts/prd5_xinsight_snapshot_test.go`. Snapshots carry a random source-created
`exchange_id`; manifest-selected snapshots carry the SHA-256 of the exact
approval manifest bytes; and generated handoff commands keep
`--approval-manifest`. Newly generated scenarios follow
`docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md`. User authorization
`UR-XI-GATE-A-20260710-01` produced immutable target exchange
`uxchg_f7de31e541516893a0d9b31cc0f3892a`; X-Insight import remains pending and
the denied scenario has an explicit correlation/source-trace gap.

PRD5 Batch 1P model permission decision evidence is implemented in
`internal/gateway/relay.go` and `internal/gateway/trace.go`, with focused
coverage in `internal/gateway/relay_mock_test.go`. Existing chat
`allowed_models` rejects now emit `auth.model_not_allowed` trace evidence before
the 403 response, using `source_component=auth`,
`capture_gap=permission_denied`, `decision_kind=auth`, and
`decision_result=rejected`. The denial still happens before billing, routing, or
provider dispatch.

PRD5 Batch 1U Embeddings model-permission parity is implemented in
`internal/gateway/relay.go`, with focused coverage in
`internal/gateway/relay_mock_test.go`. Chat and Embeddings now share the same
API-key `allowed_models` predicate for unrestricted, wildcard, resolved-model,
and requested-alias cases. Embeddings denial returns the existing 403 and
`X-Request-ID`, emits the same auth decision evidence as Chat, and stops before
billing freeze, routing, or provider dispatch.

PRD5 Batch 1V Gate A target execution is coordinated through
`/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-atlas-execution.md`.
It binds Atlas to the confirmed non-production target, exactly two scenarios,
verified request-ID sealing, sanitized snapshot/receipt evidence, and a
seven-day post-reconciliation cleanup deadline. The actual result records one
successful evidence chain and one denied request with no returned correlation
header or source trace row. X-Insight externally verified the immutable
snapshot import with a reconciled `persisted` receipt: 10 accepted, first
PostgreSQL persistence 10 inserted, rerun 10 duplicate/no-op, 0 conflict, and
readback of 10 events/3 gaps. X-Insight merged the consumer detector fix as
`4a562a6` / PR #9 and closeout as `5f0fa49` / PR #10. This does not prove the
missing denial source event or X-Insight ECS target runtime.

PRD5 Batch 1W fixes that live denial path in
`cmd/uniroute/gateway_handlers.go`. The wrapper no longer duplicates Relay's
API-key `allowed_models` check after model-governance resolution. Coverage in
`cmd/uniroute/prd2_p0_e2e_integration_test.go` proves the full handler now
returns `X-Request-ID`, persists the accepted auth decision trace, and performs
no call-log write or billing freeze for the denial.

PRD5 Batch 1X repository-wide audit and P5-F12 screening are recorded in
`docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`. The repo-local
code fixes in this batch make `GET /api/v1/logs?status_code=2xx|4xx|5xx`
apply backend status range filters in `internal/admin/log_handlers.go`, with
regression coverage in `internal/admin/admin_mock_test.go`, and make frontend
`npm run type-check` an enforced CI step through `frontend/package.json` and
`.github/workflows/ci.yml`.

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
| `Dockerfile` | Builds single Go backend binary with release version/commit `ldflags` |
| `frontend/Dockerfile` | Builds frontend static site served by nginx |
| `docker-compose.yml` | Postgres, Redis, backend, frontend, Prometheus, Grafana; backend has `/health/ready` healthcheck |
| `docker-compose.canary.yml` | Nginx and canary backend override |
| `nginx/nginx.conf` | Reverse proxy/canary routing |
| `infra/prometheus/prometheus.yml` | Prometheus scrape config |
| `infra/grafana/dashboards/unirouter-overview.json` | Grafana dashboard |
| `infra/loadtest/k6-chat-completions.js` | k6 load test |

## Verification Commands

```bash
go test ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Potentially environment-dependent tests use a real database only when configured by test helpers. Read `internal/testutil/testdb.go` before assuming integration tests always run.
