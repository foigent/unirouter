# UniRouter Codebase Map

> Purpose: help agents locate implementation quickly without rediscovering the repository from scratch.
>
> For entity ownership, state authority, cross-page consumers, and golden journeys, read `docs/architecture/SYSTEM-FACT-MAP.md` first.

## Runtime Entry

| Area | Path | Notes |
|------|------|-------|
| Main server | `cmd/uniroute/main.go` | Loads config, connects Postgres/Redis, runs migrations, wires gateway/admin routes, starts background cleanup and alerts |
| Main tests | `cmd/uniroute/main_test.go` | Top-level route behavior tests |
| Config | `internal/config/config.go` | Environment config and validation, including secrets, optional license, trace/retention, Runtime Status integration metadata, stream transport, weighted workload admission and application-authoritative payload limits |
| Database | `internal/db/postgres.go` | pgx pool setup and health |
| Redis | `internal/redisx/redis.go` | Redis setup, health, wallet cache helpers |
| Migrations | `migrations/*.sql` | Ordered PostgreSQL schema history. The current source ceiling is `000105_glm_toggle_function_tools_qualification`; the exact development/test Target snapshot is recorded in `docs/architecture/SYSTEM-FACT-MAP.md`. `internal/schemaauthority/version.go` is the executable compatibility authority, and source identity must not be confused with Target evidence. After either migration execution or `SKIP_MIGRATION`, every backend calls the exact runtime-schema gate before customer baseline or HTTP bind: the complete ordered database ledger must equal the binary artifact ledger, so missing, extra, future, duplicate, reordered and empty histories all fail closed. Ceiling changes are non-rolling: stop the older fleet, let one migration owner advance the database, then start binaries matching that exact ledger; a secondary may skip migration only after the database matches. M75–M84 establish exact public execution, lifecycle, evidence, cost-observation and reviewed-pricing authorities; M85 binds immutable Plan snapshots to Freeze and attempts; M86 preserves exact v1/v2 public execution bindings; M87 adds the Messages tools v3 tuple; M88 adds append-only capability probes/approvals plus receipt-aware qualification; M89 versions the DeepSeek Flash SSE final-usage contract; M90 adds reasoning qualification; M91 adds tools/tool-choice/JSON qualification; M92 adds the immutable Profile 1.5 composition correction and exact bindings; M93 advances only the five Flash Profile 1.5 function-tool capabilities under two existing Chat tuples to Probe Contract v2; M94 publishes nine exact Pro Profile 1.5 Chat v4 capability contracts without a generic tuple; M95 publishes six exact Kimi K2.6 Profile 1.7 Chat v5 capability contracts without authorizing K2.7 or a generic tuple; M96 advances only that exact K2.6 streaming binding to Probe Contract v2 after the live v1 delivery-shape failure; M97 advances only that streaming binding to Probe Contract v3 for bounded choice-level terminal usage while preserving failed v1/v2 evidence; M98 advances only the five non-streaming K2.6 bindings to Probe Contract v2 for bounded cache/reasoning usage details while preserving failed v1 evidence; M99 advances only tools, function tools and automatic tool choice to Function Tools Probe Contract v3 for the existing bounded public delivery shape; M100 advances the same three exact bindings to Probe Contract v4 and shares the non-stream child tool-call index normalizer with runtime delivery; M101 publishes six exact K2.7 Code Profile 1.3 Chat v5 contracts through Publication V1 without a generic tuple or live qualification; M102 publishes eight exact K3 Profile 1.9 Chat v5 Core contracts, including reasoning effort and required tool choice, while excluding Dynamic Tools, JSON Schema, search and media; M103 publishes one exact Function Tools contract each for historical MiniMax M3 Token Plan and official-open-platform GLM 5.3 through one compatible plan while keeping separate Probe identities; M104 appends three exact MiniMax PAYG Function Tools bindings for M3, M2.7 and M2.7 Highspeed with separate M3/M2 Probe identities and without rewriting M103; M105 appends five exact GLM toggle-family pure-text Function Tools bindings over shared validator mechanics while preserving model-specific Profile, protocol, reasoning, evidence and approval identities. M88–M105 do not infer live support from a Profile, rewrite sealed Releases, change aliases, activate pricing or promote traffic. Historical sealed identities remain addressable, and downgrade guards reject only durable state the predecessor cannot interpret. Read the exact later migrations and `docs/architecture/SYSTEM-FACT-MAP.md` rather than inferring current authority from early PRD ranges. |

## Backend Packages

| Package | Responsibility |
|---------|----------------|
| `internal/admin` | Admin REST handlers including canonical model onboarding, Logs/trace, Data Exchange Control, audit, Runtime Status, tenant-scoped versioned distributed-limit policy control, Guardrail policy control and the M78 canonical release workspace. Model creation validates and persists an exact reviewed execution-profile pair or explicit `unverified.chat@1.0.0`; Profile aliases/defaults do not create probe, release, pricing or traffic authority. Release collection/detail reads expose tenant-scoped server baselines and action descriptors without a Gateway License; authoring/expansion writes require organization write authority plus License, while canary cancellation and legacy rollback retain organization-RBAC-only safety authority. Runtime Status includes app/dependency/traffic/host telemetry, external monitoring checks, typed effective configuration and a redacted current-process logical topology. Data Exchange Control is platform-admin-only and records source-side approvals/exchanges/deliveries/receipts without automatic export or X-Insight runtime dependency; receipt writes accept only the canonical v1 warning registry and reject unimplemented durable job/deep-link fields |
| `internal/adapters` | Provider adapter registry and OpenAI-compatible / Anthropic-native request/response conversion |
| `internal/auth` | JWT generation/validation, API key hashing, upstream key AES-GCM encryption/decryption |
| `internal/billing` | Cost calculation, freeze/settle/cancel, metadata-only provider-attempt financial authority, explicit V1/V2 route evidence, exact public binding/contract materialization, dispatch-owner and one-terminal-health fencing, price provider, project-scoped budget evaluation and tenant-scoped notification event service |
| `internal/agentcap` | Signed short Agent capabilities, explicit topology, shared revocation/root-authority checks, and atomic per-ancestor cost/token/rate/concurrency state |
| `internal/capapi` | Strict exchange/delegate/refresh/revoke HTTP control surface for model-gateway Agent authority |
| `internal/capbilling` | Joins normal wallet Freeze/Settle/Cancel terminals to Agent ancestor reservation without widening external authority |
| `internal/limitpolicy` | Indexed versioned global/org/project/API-key/model/label policy snapshots plus exact Redis RPM/RPS/TPM/concurrency admission, composed weighted workload admission, bounded last-known-safe failure state, terminal settlement and low-cardinality evidence metrics |
| `internal/workloadadmission` | Redis-time, cluster-safe, replica-invariant global/org weighted leases and bounded fair queues for interactive, stream and embedding work |
| `internal/policybilling` | Composes distributed policy admission and terminal usage around the existing Agent-ancestor and durable wallet billing providers |
| `internal/payloadpolicy` | Actual-read endpoint payload limits plus per-replica node/organization retained-byte budgets and one-request content envelopes |
| `internal/circuitbreaker` | Standalone circuit breaker implementation and tests |
| `internal/filter` | DFA sensitive-word filter |
| `internal/guardrail` | Immutable stacked Guardrail policies, deterministic PII/secret detection, sensitive-word compatibility, fixed-registry bounded external classifiers, fail-closed/degraded decisions and HMAC-only finding evidence |
| `internal/errdocs` | Error documentation registry used by gateway `doc_url` responses and API Docs |
| `internal/gateway` | Model routing, immutable weighted route snapshots, cross-replica probe/epoch health, upstream relay, chat completions, embeddings, strict M77 Responses/Messages non-stream orchestration, model listing, request logging, metadata-only request trace events, distributed/Guardrail policy evidence and protocol-specific errors, bounded request/non-stream response envelopes, and serialized bounded SSE transport. Public non-stream execution bypasses legacy Router/adapters/response-cache and delivers only endpointprotocol-whitelisted bodies |
| `internal/gatewayerror` | Operation-catalog-aware OpenAI/Anthropic public error envelopes used consistently by outer identity, payload, License, strict JSON, idempotency, rate and public execution boundaries |
| `internal/licensing` | Strict V1 commercial License signing/verification, deployment/time/feature/limit admission, legacy read compatibility and HTTP gates |
| `internal/licensestudio` | Loopback-only graphical vendor issuer, macOS native file dialogs, no-overwrite key/License file operations and browser assets |
| `internal/releasetrust` | Release-build embedded License/upgrade public rings; formal binaries reject runtime trust-root replacement |
| `internal/signing` | Shared strict Ed25519 key parsing/encoding and named public-key ring validation |
| `internal/upgradebundle` | Deterministic signed offline release bundle packing and streaming tamper/platform/sequence/schema verification |
| `deploy/customer` | Source-free customer Compose/env template for prebuilt signed release artifacts; public trust roots are embedded in the backend image and no `build:` context is present |
| `internal/logger` | Logging helpers |
| `internal/metering` | Usage-source constants and token-metering source classification |
| `internal/middleware` | Gateway/admin auth, exact OpenAI Bearer versus Anthropic x-api-key/version admission, retained-body live re-authentication, recursive strict JSON, metadata-only idempotency, rate/admission control, CORS, baseline security headers and Prometheus metrics |
| `internal/migrate` | SQL migration runner |
| `internal/endpointprotocol` | Closed endpoint-specific protocol contracts plus the separate exact eight-field public non-stream registry. Responses/Messages request and provider response shapes are strictly whitelisted; customer delivery restores only the public model and canonical bounded usage |
| `internal/modelgov` | Model governance helpers and access checks |
| `internal/modeldeployment` | Canonical ProviderConnection, ModelDeployment, CredentialBinding, operation/capability evidence, append-only probe, route-parity projection, and explicit root-customer legacy reconciler. Legacy channels are read only by explicitly enabled legacy/verify compatibility and never by canonical current-state admin projections |
| `internal/modelqualification` | Compiled half of the exact Responses/Messages support contract: operation, public path, endpoint-protocol profile/version, executor contract and conservative HTTPS-origin normalization. Mutable production qualification remains solely M75/PostgreSQL-owned |
| `internal/modelexecution` | Final pre-provider transaction authority: revalidates a selected `modelrouting` plan, claims mode-aware shared half-open leases, opens one commit-recoverable owner-fenced billing attempt, terminalizes undispatched receipts, and applies attempt-idempotent health CAS across legacy/verify/canonical projections. Ordinary `Authorize` rejects public evidence; narrow `AuthorizePublic` accepts only exact M77 EvidenceV2 and same-transaction release-item/binding/current-credential/probe authority |
| `internal/modelprofile` | Immutable versioned model-execution catalog and component contracts: tokenizer/reasoning/cache/protocol identities, provider maxima, provider-advertised capabilities, narrower UniRouter-verified capabilities, exact aliases, model-specific reasoning combinations and provider-owned cache-usage wire authority |
| `internal/tokenprofile` | Deterministic tokenizer admission implementations and frozen tokenizer artifacts; unknown models remain on the conservative byte upper bound |
| `internal/modelrouteauthority` | Explicit startup authority plus the two non-legacy safety contractions. Check is a read-only database fact in every mode; apply is verify-only and reconciles/seals all current v1 stable/canary manifests without moving slots (legacy/canonical reject it before database access). Pause and terminal credential revoke are actor/root-revalidated, transactionally audited, and remain available without a Gateway License |
| `internal/modelrouting` | Read-only mode-aware route planner and immutable execution-evidence producer; public candidates require an exact selected-item `(public_execution_binding_id, release_id, ordinal)` qualification join and freeze all four M77 binding/contract fields, but Planner never grants dispatch authority |
| `internal/executionevidence` | Explicitly disjoint normalized secret-free EvidenceV1 and EvidenceV2 plus digest/compact contracts. Public operations require V2 binding and three exact contract versions; V1 fields and historical hashes are unchanged |
| `internal/runtimechange` | Canonical runtime release authority. M78 exposes the sole atomic draft-to-validated v2 authoring command, durable authoring/lifecycle receipts, server-owned optimistic baselines/actions, the normal five-action lifecycle, and the explicit `cutover_legacy`/`rollback_legacy` bridge. Authoring never activates; cutover requires exact full Chat+Embeddings plan parity, while legacy rollback ignores broken v2 health and revalidates the retained sealed v1 parent instead |
| `internal/models` | Shared data model types |
| `internal/routingpolicy` | Validated immutable retry, circuit/probe, cooldown, snapshot/LKG, candidate-bound and provider-key health configuration plus credential-free preview DTOs |
| `internal/runtimeconfig` | Safe versioned effective-configuration registry for routing, stream transport and the active project budget-alert contract |
| `internal/streampolicy` | Immutable validated SSE line, write-idle, heartbeat and maximum-duration policy (`stream-transport.v1`) |
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
| Gateway | `POST /v1/chat/completions`, `POST /v1/embeddings`, strict text `POST /v1/responses` (exact Sol/Terra/Luna v2 SSE subset), strict non-stream `POST /v1/messages`, `GET /v1/models`. Responses use exact Bearer auth; Messages uses exact x-api-key plus `anthropic-version: 2023-06-01`; both use fixed 8 MiB retained-body admission, metadata-only at-most-once idempotency and response-cache bypass |
| Auth | `POST /api/v1/auth/login`, `GET /api/v1/auth/oidc/config`, `GET /api/v1/auth/oidc/start`, `GET /api/v1/auth/oidc/callback`, `GET /api/v1/me`, `PUT /api/v1/me/password` |
| Legacy channel compatibility | `GET /api/v1/channels`, `GET /api/v1/channels/{id}` and read-only provider-model discovery remain for history/migration diagnostics. `POST /api/v1/channels`, `PUT/DELETE /api/v1/channels/{id}` and `POST /api/v1/channels/{id}/test` are permanently retired after admin authentication/RBAC with `410 legacy_model_mutation_retired`; no route-authority mode re-enables them and the retirement handler performs no License/body/business-data/secret work |
| Canonical model deployments | `GET/POST /api/v1/model-deployments`, `GET /api/v1/model-deployments/{deploymentID}`, and `POST /api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes` for exact Chat Completions, Embeddings, Responses, or Messages evidence. POST may pin an exact repository-reviewed Model Execution Profile; missing selection is explicitly unverified, never inferred from the model name. Responses/Messages use the endpoint-protocol network contract, not a local fixture. Platform approval supplies M75 structural qualification only; public execution additionally requires a selected active M77-bound release item, current credential/link/latest-own-probe and per-request dispatch/delivery/settlement terminals. `POST /api/v1/provider-connections/{connectionID}/pause` and `POST /api/v1/provider-credential-bindings/{bindingID}/revoke` are License-independent emergency safety contractions |
| Canonical deployment releases | `GET/POST /api/v1/model-deployment-releases`, `GET /api/v1/model-deployment-releases/{releaseRef}`, and seven stage-specific lifecycle POST actions. Collection/detail reads are organization-scoped and License-independent. POST collection accepts only public-model identity, the exact server-derived baseline, deployment/operation selections and reason; it atomically returns a sealed validated release receipt and never activates traffic. The five normal actions retain M76 semantics. `cutover-legacy` requires exact sealed-v1 lineage plus complete Chat+Embeddings executable-plan parity; `rollback-legacy` is a no-License safety contraction that restores the current executable v1 parent without consulting broken v2 health. Every mutation requires a strict JSON object no larger than 16 KiB and a durable action-scoped `Idempotency-Key` |
| API keys | `GET/POST /api/v1/keys`, `GET/PUT /api/v1/keys/{id}`, `POST /api/v1/keys/{id}/disable` |
| Shared limit policies | `GET/POST /api/v1/limit-policies`, `PUT /api/v1/limit-policies/{id}` |
| Guardrail policies | `GET/POST /api/v1/guardrail-policies`, `PUT /api/v1/guardrail-policies/{id}` |
| Legacy credential compatibility | `GET /api/v1/channel-keys` remains a redacted historical/migration read. `POST /api/v1/channel-keys`, `PUT/DELETE /api/v1/channel-keys/{id}` and `POST /api/v1/channel-keys/{id}/rotate` are permanently retired with the same authenticated/RBAC-scoped `410`; canonical credentials are managed only through canonical deployment authority |
| Legacy runtime compatibility | `GET /api/v1/runtime-changes`, `/slot` and `/{ref}` remain read-only history. Create/validate/activate/shadow/canary/promote/rollback/reject mutations are permanently retired with `410`. Together with four channel, four channel-key and three model-endpoint lifecycle mutations, the retired matrix is exactly 19 routes in every authority mode. `GET /api/v1/model-endpoints` is the canonical-only compatibility projection `unirouter.model-endpoints.canonical-compat.v3`; legacy identity fields stay zero and explicit deployment/connection/operation IDs carry current identity |
| Wallet | `GET /api/v1/wallet`, `POST /api/v1/wallet/topup`, `POST /api/v1/wallet/adjust`, `GET /api/v1/wallet/transactions`, `GET /api/v1/wallet/transactions/export` |
| Logs/dashboard | `GET /api/v1/logs`, `GET /api/v1/logs/{id}`, `GET /api/v1/logs/{id}/trace`, `GET /api/v1/dashboard/stats`. Dashboard current health and window facts use canonical deployment+operation/application authority and `route_deployment_id + route_model_operation_id`; `route_channel_id` is historical attribution only |
| Model readiness | `GET /api/v1/model-readiness` returns `unirouter.model-readiness.v2`. Connection mode requires `deployment_id`; both modes expose the exact endpoint-derived `model_operation_id`, and caller mode explains the current canonical Planner/application projection without Router preview or external traffic |
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
| Shared UI utilities | `frontend/src/utils/` (`displayLabels`, `format`, `quota`, and `modelOnboarding` for provider/Profile composition without evidence inference) |
| Pages | `frontend/src/pages/*.tsx` |
| Shared error boundary | `frontend/src/components/ErrorBoundary.tsx` |
| Global CSS | `frontend/src/index.css` |
| Tests | `frontend/src/**/__tests__/*.test.tsx`, `frontend/src/**/__tests__/*.test.ts` |

Frontend route pages currently include dashboard, product-facing model onboarding (“模型接入”), canonical deployment/release detail workspaces, API keys, shared limits, content safety/Guardrail policy, developer console, usage analytics, internal status, notification center, model governance, wallet, logs, data exchange, users, model prices, org model prices, grants, audit logs, reconciliation, license, settings, API docs, access state, and login. The onboarding sheet composes a reviewed provider preset with the latest exact Profile per Profile ID, constrains safe aliases/limits/operation, and sends the exact Profile pin; manual/custom flow remains explicitly unverified. Legacy `/channels` resolves to this canonical-backed workspace and `/channel-keys` is a compatibility redirect; the retired channel-key page and runtime-change workspace are not mounted.

## Data Model

Base schema is in `migrations/000001_init_schema.up.sql`. Main tables:

| Domain | Tables |
|--------|--------|
| Tenant/user | `orgs`, `users` |
| Routing | Legacy `channels`, `channel_keys`; canonical `provider_connections`, `model_deployments`, `provider_credential_bindings`, `credential_binding_deployments`, `model_deployment_operations`, `model_deployment_capabilities`, `model_deployment_probes`, `model_deployment_create_receipts` |
| Client access | `api_keys` |
| Pricing | `model_prices`, `org_model_prices` |
| Billing | `wallets`, `transactions`, `grants`, `freeze_idempotency`, `billing_provider_attempts`, `provider_cost_reconciliations`, `request_delivery_receipts` |
| Observability/control | `call_logs`, `request_trace_events`, `audit_logs`, `integration_exchange_items`, `integration_exchanges`, `integration_exchange_members`, `integration_exchange_deliveries`, `integration_exchange_receipts` |
| Configuration | `settings`, `distributed_limit_policies`, `guardrail_policies` |

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

PRD7 B3 metadata-only OTLP/OpenInference export is implemented in
`internal/telemetryexport`, wired through `internal/gateway/trace.go`,
`internal/config/config.go` and `cmd/uniroute/main.go`. It observes only newly
created transactional Outbox trace facts, uses a fixed non-blocking queue and
bounded OTLP/HTTP worker, exports route-derived model identity plus explicit
provenance/capture gaps, and recursively rejects restricted content. It is
default off and does not alter request, billing, Outbox, completed-execution
Feed or cursor behavior. The exact contract is
`docs/tech-ref/32-otel-openinference-metadata-export.md`.

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
