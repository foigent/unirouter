# PRD2 Completion Plan

> Purpose: track the remaining work required before PRD2 can be honestly closed as fully implemented and usable.
> Scope decision date: 2026-07-02.

## Completion Standard

PRD2 is not complete when implementation batches merely pass local or unit tests. PRD2 can close only after:

1. Every PRD2 P0/P1/P2 item in `docs/PRD-Phase2.md` is either implemented and accepted, or explicitly recorded as a user-approved later-phase item.
2. User-confirmed P2 items are included: P2-F13 Playground/docs integration, P2-F14 Advanced Usage Analytics, and P2-F15 Cache Research.
3. Acceptance evidence follows `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`: exact command or manual path, environment, expected result, actual result, and timestamp or commit/worktree reference.
4. Browser-dependent product workflows are verified against a real backend, not only component tests or mocked browser data.
5. Provider-dependent workflows have real upstream evidence where credentials are available, and any credential-blocked gap is clearly marked.
6. `docs/issues/OPEN-ISSUES.md` has no PRD2 blocker or unclassified PRD2 open item left.
7. The final round completes the Codex -> Canary -> Sentinel -> Atlas protocol in `docs/agent-workflows/AGENT-PROTOCOL.md`.

## Confirmed Product Decisions

| Decision | Current Direction | Tracking |
|----------|-------------------|----------|
| P2-F13/F14/F15 | All are mandatory for complete PRD2 scope | Batch 23 completed per Canary/Sentinel/Atlas; matrix P2 product rows are now accepted for PRD2 scope |
| Cache | Complete PRD2 scope means a cache research spike, not default runtime cache | P2-F15 |
| SSO | OIDC first is mandatory, but later in the remaining plan | `PRD2-AUTH-001` |
| Prompt/response storage | Default no plaintext storage; optional storage needs formal design and user approval before implementation | P2-F11 |
| Developer key creation | `developer` can create and manage permitted project keys by default | P2-F03/P2-F01 |
| Project budget | PRD2 first release remains alert-only; hard-blocking budget enforcement is a later item | `PRD2-BUDGET-001` |
| HA validation | Staging/docker-compose two-instance validation first; production HA remains not fully implemented until target evidence exists | `PRD2-HA-001` |
| Claude via OpenRouter | Valid for `openai_compatible` Claude E2E, not a substitute for direct `anthropic_native` protocol E2E | P2-F05/native adapter proof |
| Extension/plugin boundary | PRD2 uses core product features plus static code/config extension points; it does not include a runtime plugin platform | `PRD3-PLUGIN-001` |
| OIDC boundary | OIDC first SSO should be implemented as core authentication infrastructure, not as an arbitrary third-party plugin | Batch 24 / `PRD2-AUTH-001` |
| PRD3 deferrals | Runtime plugins, plugin marketplace/third-party installation, enterprise IaC packaging, and full engineering governance are later unfinished items | `PRD3-PLUGIN-001`, `PRD3-DEPLOY-001`, `PRD3-ENG-001` |

## Why Cache Is Research-Only In PRD2

`docs/PRD-Phase2.md` defines P2-F15 as a research spike and says runtime cache must not be enabled by default. This is intentional:

- Prompt and response caching intersects with P2-F11 storage policy, privacy, retention, encryption, and read audit.
- Semantic cache can leak information across org/project/key/model boundaries unless isolation rules are designed and verified.
- Cached responses can become stale or incorrect, which changes auditability and enterprise responsibility.
- Billing becomes ambiguous unless cache-hit pricing, provider-cost evidence, and usage records are defined.
- Streaming, tool calls, vision payloads, system prompts, temperature, and model alias changes make cache correctness non-trivial.

PRD2 completion therefore requires a research note with risks, constraints, and an optional future architecture. Runtime cache implementation needs a separate user approval.

## Why Prompt/Response Storage Defaults To Off

UniRouter routes potentially sensitive enterprise prompts and responses. Storing plaintext by default would turn the gateway into a sensitive-content repository. The PRD2-safe policy is:

- no plaintext prompt/response storage by default;
- optional org/project-level enablement only after user approval;
- encrypted storage if enabled;
- retention controls;
- RBAC on every read;
- audit event for every read;
- redaction guidance before exposing content to support or admin users.

Until this policy is approved, request troubleshooting must use metadata only: request id, project, key prefix, model/alias, status, tokens, cost, latency, provider family, channel display name where allowed, normalized error, and docs link.

## Architecture Decisions To Finalize Before Continuing

These decisions should be settled now because they affect Batch 20-24 implementation boundaries, but they should not expand PRD2 into a runtime plugin platform:

1. OIDC first SSO is a core auth feature for PRD2. Batch 24 should implement provider config, login/callback, claim-to-user/org/role mapping, provisioning policy, RBAC tests, audit behavior, and operator docs. LDAP/SAML and arbitrary auth plugins remain out of scope unless the user expands scope later.
2. PRD2 extension points stay static and controlled: Go interfaces plus database/config-driven selection for provider adapters, notification/event routes, exporters, and documentation/status connectors. There is no PRD2 runtime loading of third-party code.
3. Plugin-suitable candidates for PRD3 are provider adapters, notification sinks, audit/SIEM exporters, usage/report exporters, model-catalog sync, status connectors, and error-doc connectors.
4. Security/accounting boundaries stay core, not generic plugins: OIDC/SSO, RBAC, API-key hashing, wallet/billing settlement, license verification, prompt/response storage policy, and runtime cache policy.
5. Cache remains P2-F15 research only in PRD2. No default runtime cache is enabled until privacy, tenant isolation, invalidation, billing, and audit semantics are approved.
6. Deployment docs remain Docker Compose/self-hosted plus two-instance validation for PRD2. Kubernetes, Helm, Terraform, and managed-HA references are PRD3/later.
7. Documentation closeout must update current status, acceptance matrix, open issues, and handoff docs before final PRD2 closure.

## Remaining Execution Batches

### Batch 16: Real Backend Browser And Developer Console E2E

Goal: prove the P0 developer path works against a real backend.

Required work:

- Add real-backend browser smoke for login, project selector, project overview, project API keys, approved models, usage, request lookup, and docs entry.
- Verify a developer can create a project key, copy plaintext once, call `/v1/chat/completions`, and then see the resulting project usage/log evidence.
- Verify direct Admin Console URLs are blocked for developer users.
- Verify request lookup does not expose provider keys, full API keys, prompt plaintext, response plaintext, or other-project data.
- Verify cross-project negative cases through API and browser paths where practical.

Target rows:

- P2-F01 Project / Workspace frontend flow and usage/log drill-down.
- P2-F02 real-browser RBAC navigation.
- P2-F03 Developer Console MVP.
- Request troubleshooting restricted-data behavior.

Status update on 2026-07-02:

- Codex implemented the Batch 16 real-backend browser smoke and supporting seed command:
  - `scripts/prd2-developer-console-smoke.sh`
  - `scripts/prd2_developer_console_seed.go`
  - `scripts/prd2_developer_console_smoke.mjs`
- Local environment used for evidence: current-code backend on `http://localhost:18080`, existing Vite frontend on `http://localhost:3000`, local PostgreSQL/Redis, Playwright Chromium, and a script-owned mock OpenAI-compatible upstream.
- Local command passed:

```bash
set -a; source .env.production; set +a; \
UNIROUTE_SMOKE_BACKEND_URL='http://localhost:18080' \
UNIROUTE_SMOKE_FRONTEND_URL='http://localhost:3000' \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-developer-console-smoke.sh
```

- Actual result: `status=PASS`, `request_id=batch16-browser-1782957796339`, `model_alias=company-chat`, `upstream_calls=1`, screenshot `output/playwright/prd2-batch16-developer-console.png`.
- Defects found and fixed during this batch:
  - Developer Console crashed on array `capability_tags`/`aliases` from the model catalog; fixed by accepting array or CSV values.
  - Request Lookup failed on successful logs with NULL `upstream_error`; fixed by making `GetLogDetail` use `COALESCE(upstream_error, '')` and adding `TestGetLogDetail_NullSafeUpstreamError`.
- Current protocol state: User reported Canary, Sentinel, and Atlas completed; `main`/`origin/main` includes PR #48 at `6876746`.

### Batch 17: Model Governance And Usage/Metering Completion

Goal: make model governance and usage evidence usable through the product UI.

Required work:

- Verify Model Registry create/edit/list and Model Set assignment against a real backend.
- Verify Approved Catalog hides unapproved models with fixtures visible in Developer Console.
- Verify deprecated migration recommendation is visible in the developer catalog.
- Verify retired models remain blocked through gateway.
- Add usage-source filtering to frontend reconciliation/usage surfaces where missing.
- Move estimated-vs-upstream variance from page-scoped evidence toward a full-query aggregate where required by product UX.
- Wire high estimated-usage ratio into notification/dashboard evidence.

Target rows:

- P2-F04 Model Governance.
- P2-F06 Token Metering V2.
- P2-F12 acceptance discipline for matrix upgrades.

Status update on 2026-07-02:

- Codex implemented Batch 17 locally:
  - Reconciliation summary now aggregates the full filtered query, not the current page.
  - Reconciliation API summary includes preflight estimated tokens, actual tokens, variance, source confidence, estimated ratio, alert threshold, and `estimated_ratio_scope=filtered_query`.
  - High estimated-ratio signal emits `token_metering.estimated_ratio.high` notification events through the existing event service.
  - Logs and Reconciliation frontend surfaces include `usage_source` filtering.
  - Developer Console renders deprecated approved-catalog migration recommendations.
  - Added Batch 17 seed/smoke scripts:
    - `scripts/prd2-batch17-modelgov-usage-smoke.sh`
    - `scripts/prd2_batch17_modelgov_usage_seed.go`
    - `scripts/prd2_batch17_modelgov_usage_smoke.mjs`
- Local environment used for evidence: current-code backend on `http://localhost:18080`, existing Vite frontend on `http://localhost:3000`, local PostgreSQL/Redis, and Playwright Chromium.
- Local command passed:

```bash
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18080 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch17-modelgov-usage-smoke.sh
```

- Actual result: `status=PASS`, `project_id=27`, `model_set_id=13`, `retired_model=batch17-retired-chat`, `reconciliation_model=batch17-recon-metering`, `estimated_ratio=0.8`, `estimated_ratio_scope=filtered_query`, screenshot `output/playwright/prd2-batch17-modelgov-usage.png`.
- Smoke coverage:
  - Model Registry create/edit/list against real backend.
  - Project model-set assignment via real backend.
  - Approved Catalog hides unapproved and retired fixtures.
  - Deprecated migration recommendation visible in Developer Console.
  - Retired model gateway call returns 403 with `model_retired`.
  - Reconciliation full-query aggregate remains correct with paginated data.
  - High estimated-ratio notification event is persisted and queryable.
- Current protocol state: `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS with independent smoke SKIP only because Canary had no running backend; User reported Sentinel and Atlas completed; `main`/`origin/main` includes PR #49 at `f6bafb5`.

### Batch 18: Provider Adapter Real Upstream Acceptance

Goal: separate OpenRouter Claude E2E from direct Anthropic native protocol E2E.

Required work:

- Batch 18A: configure Claude through OpenRouter as `openai_compatible` and run non-stream plus stream gateway E2E.
- Batch 18B: configure direct Anthropic credentials for `anthropic_native` and run non-stream plus stream protocol E2E when credentials are available.
- Verify normalized usage, error, metadata, stream final `_uniroute`, and missing-usage fallback behavior.
- Verify provider-specific 401/429/5xx classification with real or controlled upstream behavior where practical.

Acceptance boundary:

- OpenRouter Claude validates real Claude model access through UniRouter's OpenAI-compatible adapter path.
- OpenRouter does not replace direct Anthropic Messages API validation for `anthropic_native`, because OpenRouter exposes a normalized OpenAI-compatible API.

Target rows:

- P2-F05 Provider Adapter Framework.
- Native adapter proof.
- P2-F06 real upstream usage evidence.

Status update on 2026-07-02:

- Current shell environment does not expose `OPENROUTER_API_KEY` or `ANTHROPIC_API_KEY`.
- Local `.env.production` currently has `API_KEY_HASH_SECRET` but no OpenRouter or Anthropic upstream credential variables.
- User explicitly deferred Batch 18 on 2026-07-02. It remains an unfinished later-plan item and should not block Batch 19+ execution.
- Batch 18 can proceed later with credential-gated smoke tooling and mock syntax checks, but real upstream acceptance cannot be marked complete until keys/model slugs are provided.
- Required inputs:
  - `OPENROUTER_API_KEY` plus an OpenRouter Claude model slug for Batch 18A.
  - `ANTHROPIC_API_KEY` plus a direct Anthropic model slug for Batch 18B.

### Batch 19: Notification/Event And Project Budget Completion

Goal: make notification events visible, retryable, and production-verifiable.

Required work:

- Add frontend notification center for events, delivery state, and webhook routes.
- Add background retry worker for failed webhook deliveries.
- Wire remaining emitters: key disabled/revoked, model deprecated/retired, provider/channel failure, license warning, and high estimated-usage ratio.
- Add project budget browser/API E2E.
- Add period-boundary integration test for daily/monthly budgets.
- Produce production or production-like webhook delivery evidence.

Target rows:

- P2-F08 Notification/event foundation.
- Project budget alerts.
- License warning event dependency for P2-F10.

Status update on 2026-07-02:

- Codex implemented Batch 19 locally:
  - frontend Notification Center at `/notifications` with event, delivery, and webhook-route tabs;
  - notification API client methods and navigation/topbar entry;
  - webhook create/edit UI for route configuration;
  - background retry loop in `cmd/uniroute/main.go` plus `EventService.RetryDueDeliveries`;
  - retry state aggregation so an event is marked delivered only when all deliveries for that event are delivered;
  - emitters for `api_key.disabled`, `model.deprecated`, `model.retired`, `provider.channel.failure`, and `license.warning`;
  - project budget daily/monthly period-boundary integration test;
  - Batch 19 real-backend smoke scripts:
    - `scripts/prd2-batch19-notifications-budget-smoke.sh`
    - `scripts/prd2_batch19_notifications_budget_seed.go`
    - `scripts/prd2_batch19_notifications_budget_smoke.mjs`
- Codex local validation passed:

```bash
go test ./internal/billing
go test -tags=integration ./internal/billing -run TestEventService_CheckProjectBudgetPeriodBoundaries_Integration -count=1 -v
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

- Local real-backend smoke command passed:

```bash
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18080 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch19-notifications-budget-smoke.sh
```

- Actual result: `status=PASS`, `project_id=34`, `event_id=10`, `webhook_id=5`, `webhook_receipts=1`, `budget_cents=1000`, `spent_cents=1200`, screenshot `output/playwright/prd2-batch19-notifications-budget.png`.
- Current protocol state: `docs/agent-workflows/VERIFY-RESULT.md` reports Batch 19 PASS 5/5 with one real-backend smoke SKIP because Canary had no running backend. User reported Canary, Sentinel, and Atlas completed; `main`/`origin/main` includes PR #51 at `5bbbf07`.
- Remaining acceptance boundary: production webhook delivery evidence is still not claimed; this batch provides local production-like receiver evidence only.

### Batch 20: Audit Completion And PRD2 Hardening Issues

Goal: close known PRD2 audit/migration/governance hardening gaps.

Required work:

- Add frontend audit filters for org, project, actor, action, resource, time range, and source.
- Add real JWT/API E2E for audit filters and before/after responses.
- Add richer channel create/update/delete before/after audit details.
- Add `channel.test` audit event for success/failure without leaking provider keys.
- Decide and implement/document historical audit `project_id` behavior.
- Decide rollback fidelity for migration 000012 `usage_source` normalization.
- Keep PRD1 compatibility for unregistered models while adding admin/developer warning, audit, or notification signal unless user later chooses strict blocking.

Target rows/issues:

- P2-F07 Audit schema upgrade.
- `PRD2-MIG-001`.
- `PRD2-MIG-002`.
- `PRD2-GOV-001`.
- `PRD2-AUDIT-001`.

Status update on 2026-07-02:

- Codex implemented Batch 20 locally:
  - Audit Logs frontend filters for org, project, actor, action, resource, resource id, source, start time, and end time;
  - audit API client filter parameters;
  - real JWT/API E2E for audit filters and before/after response fields;
  - channel create/update/delete before/after audit snapshots;
  - `channel.test` success/failure audit without provider key, prompt, response, or curl example leakage;
  - 000009 historical `audit_logs.project_id` default-project backfill;
  - 000012 down migration reverse mapping for `usage_source`;
  - gateway audit signal `model.unregistered.call` for unregistered model calls while keeping PRD1 compatibility.
- Local validation passed:

```bash
go test ./internal/admin -run 'Test(CreateChannel_AuditIncludesAfterState|UpdateChannel_AuditIncludesBeforeAfterState|DeleteChannel_AuditIncludesBeforeState|TestChannel_WritesAuditWithoutSecret|ListAuditLogs_Filters)' -count=1
go test ./cmd/uniroute -run Test -count=1
go test -tags=integration ./internal/migrate -run 'TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration|TestMigration000012DownRestoresLegacyUsageSourceValues_Integration' -count=1 -v
go test -tags=integration ./cmd/uniroute -run 'TestPRD2AuditFiltersJWTBeforeAfterE2E_Integration|TestGatewayUnregisteredModelAllowedButAudited_Integration' -count=1 -v
go test ./...
go test -tags=integration ./cmd/uniroute ./internal/migrate -count=1
npm --prefix frontend test -- --run AuditLogsPage client
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

- Current protocol state: Batch 20 passed Canary 7/7 and Sentinel merged PR #53 at `6b1f5cc`; current status and matrix now treat the Batch 20 audit/migration/governance rows as accepted within their documented scope.

### Batch 21: HA And Deployment Validation

Goal: prove two-instance readiness in a safe environment and record production gaps honestly.

Required work:

- Run staging/docker-compose two-instance smoke against shared PostgreSQL/Redis.
- Verify health, gateway call, idempotency, wallet safety, rate-limit/cooldown shared state, and rollback steps.
- Verify load-balancer/proxy routing evidence where the target environment provides it.
- Run backup/restore rehearsal in a safe non-production environment.
- Preserve `.env`/`.env.production` and record post-deploy API key smoke output.

Target rows/issues:

- P2-F09 HA readiness.
- `PRD2-HA-001`.
- `PRD2-DEPLOY-001` archive readiness after operator confirmation.

Status update on 2026-07-02:

- Codex implemented Batch 21 local tooling/docs:
  - `docker-compose.canary.yml` now mounts HTTP-only `nginx/nginx.http-canary.conf` by default, so local/staging canary smoke does not require TLS certificates.
  - `backend-canary` exposes `http://localhost:8081` for direct HA smoke while proxy traffic still goes through Nginx.
  - `scripts/prd2-ha-smoke.sh` now checks `/health/live` and `/health/ready` for stable/canary/proxy, supports optional real gateway smoke, optional DB idempotency evidence for a shared request id, and optional shared rate-limit evidence with a low-limit test key.
  - `scripts/prd2-backup-restore-rehearsal.sh` performs a guarded `pg_dump` plus restore into an explicitly confirmed non-production database and validates migration/core-table presence. It does not drop or truncate the target database.
  - `docs/ops/deployment-runbook.md` and `docs/tech-ref/15-ha-readiness.md` document the exact compose/smoke/rehearsal flow.
- Local validation passed:

```bash
bash -n scripts/prd2-ha-smoke.sh
bash -n scripts/prd2-backup-restore-rehearsal.sh
ruby -e 'require "yaml"; YAML.load_file("docker-compose.canary.yml"); YAML.load_file("docker-compose.yml"); puts "yaml ok"'
go test ./...
git diff --check
```

- Local environment limitation: this Codex shell has no `docker` or `nginx` command, so actual compose config/startup, Nginx runtime syntax, two-instance HA smoke, rollback check, and backup/restore rehearsal must be run by Canary/Atlas or another Docker-enabled environment before P2-F09 moves to E2E accepted.
- Initial protocol state on 2026-07-02: fixed locally by Codex. This was superseded by the 2026-07-03 update below: User reported Canary, Sentinel, and Atlas completed, and the backup/recovery drill evidence was recorded.

Status update on 2026-07-03:

- User reported Canary, Sentinel, and Atlas completed.
- Canary result: `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 5/5 with Docker runtime SKIP handed to Atlas.
- Atlas backup/recovery drill evidence moved from repo root to `docs/ops/evidence/PRD2-B21-backup-recovery-drill-20260703.md`.
- Backup/recovery drill result: PASS for PostgreSQL backup creation, `pg_restore -l` integrity, isolated drill DB restore, migration/table/core row/FK/index/query checks, cleanup, and WAL archiving in Alibaba Cloud ECS dev/test.
- Remaining HA/DR gaps explicitly not closed by the drill: backup encryption, NAS sync, cron auto-backup, Feishu backup failure alerts, PITR recovery, remote restore, application-level verification against the drill DB, Patroni/local DB HA, and app DB failover reconnect behavior.
- Current acceptance state: P2-F09 moves to E2E accepted for PRD2 self-hosted/staging readiness, but not Production verified.

### Batch 22: License Production Acceptance

Goal: move License Foundation from unit-tested foundation to real-backend/production accepted.

Required work:

- Provide or generate a test offline license and matching `LICENSE_PUBLIC_KEY`.
- Run real-backend browser smoke for `/license`.
- Verify missing, invalid, active, expiring, and expired statuses where practical.
- Verify production or approved staging license import/status evidence.
- Verify `license.update` audit summary and no raw signature leakage.
- Verify license warning event after Batch 19 emitter support exists.

Target rows:

- P2-F10 License foundation.
- Notification dependency for license warning.

Status update on 2026-07-03:

- Codex implemented Batch 22 local tooling and smoke coverage:
  - `scripts/prd2-batch22-license-smoke.sh`
  - `scripts/prd2_batch22_license_seed.go`
  - `scripts/prd2_batch22_license_smoke.mjs`
  - `docs/ops/evidence/PRD2-B22-license-local-smoke-20260703.md`
- The seed/generator provides a test offline license set and matching `LICENSE_PUBLIC_KEY`; it also has a guarded `UNIROUTE_BATCH22_ALLOW_LICENSE_RESET=non-production-license-reset` path before clearing or overwriting global `settings.license.offline`.
- Local validation passed:

```bash
go run ./scripts/prd2_batch22_license_seed.go -action print-public-key
go test ./internal/licensing ./internal/admin -run 'TestVerifier|Test(GetLicense|UpdateLicense)' -count=1
npm --prefix frontend test -- --run LicensePage client navigation
node --check scripts/prd2_batch22_license_smoke.mjs
bash -n scripts/prd2-batch22-license-smoke.sh
go test ./...
npm --prefix frontend run build
```

- Local real-backend/browser smoke passed against current-code backend `http://localhost:18082`, frontend `http://localhost:3000`, local PostgreSQL/Redis, and generated test public key:

```bash
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18082 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_DB_DSN=postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable \
UNIROUTE_BATCH22_ALLOW_LICENSE_RESET=non-production-license-reset \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch22-license-smoke.sh
```

- Actual result: `status=PASS`, `org_id=41`, `user_id=27`, `active_deployment_id=prd2-b22-active`, `expiring_deployment_id=prd2-b22-expiring`, `expired_deployment_id=prd2-b22-expired`, `warning_event_id=12`, screenshot `output/playwright/prd2-batch22-license.png`. The smoke default cleanup cleared the local `license.offline` fixture afterward; set `UNIROUTE_BATCH22_KEEP_LICENSE=true` only when an approved target environment should retain the test license for inspection.
- Smoke coverage:
  - missing, invalid, active, expiring, and expired license statuses;
  - invalid license import rejects with HTTP 400;
  - real-browser `/license` imports active, expiring, and expired licenses;
  - `license.update` audit rows exist for successful imports and do not include raw signatures;
  - `license.warning` notification event is emitted for the expiring license.
- Current protocol state: Batch 22 passed Canary 5/5 in `docs/agent-workflows/VERIFY-RESULT.md`; real backend/browser smoke was SKIP only because Canary had no running environment. User reported Canary and Sentinel completed.
- Atlas/deployment state: User reported Atlas reviewed the 16-file Batch 22 change set as docs plus smoke scripts only, with no application code changes; therefore no application deployment is required for this batch.
- Remaining acceptance boundary: production or approved-staging license import/status evidence is still required before claiming `Production verified`. Atlas/runtime smoke is required only if the goal is to promote P2-F10 to target-environment acceptance, and the guarded reset smoke must not be run blindly against production.

### Batch 23: P2 Product Features

Goal: implement the user-confirmed mandatory P2 product scope.

Required work:

- P2-F13 Playground integrated with approved catalog:
  - project-scoped keys only;
  - no shared platform key exposure;
  - approved model aliases only;
  - stream output, request id, cost, tokens, latency, and copyable code;
  - no prompt/response history by default.
- Error docs with `doc_url`:
  - normalized error schema update;
  - docs pages for common gateway/RBAC/model/billing/provider errors;
  - API/browser tests.
- Internal status page / incident history:
  - status schema/API/UI;
  - incident list and timeline;
  - access control.
- P2-F14 Advanced Usage Analytics:
  - drill-down by org, project, key, model, alias, provider, channel, status, and usage_source;
  - CSV export respecting role scope;
  - estimated usage ratio visibility.
- P2-F15 Cache Research:
  - exact cache and semantic cache comparison;
  - privacy, billing, audit, correctness, tenant isolation, and invalidation analysis;
  - future opt-in architecture recommendation;
  - no runtime cache enabled by default.

Target rows:

- P2-F13.
- Error docs with `doc_url`.
- Internal status page / incident history.
- P2-F14.
- P2-F15.

Status update on 2026-07-04:

- Codex implemented Batch 23 locally and handed it to Canary via `docs/agent-workflows/FIX-SIGNAL.md`.
- Backend/API:
  - added `internal/errdocs/errdocs.go`;
  - added Batch 23 product handlers in `internal/admin/prd2_product_handlers.go`;
  - registered `GET /api/v1/usage/analytics`, `GET /api/v1/internal-status`, and `GET /api/v1/error-docs`;
  - added route parity and targeted handler tests;
  - extended GatewayAuth and Relay errors with `doc_url`.
- Frontend:
  - added `/usage-analytics` and `/status` pages;
  - added Developer Console Playground using project API keys and approved model/alias selection;
  - added API Docs error registry rendering;
  - updated role-aware navigation and frontend API client/tests.
- Docs/research:
  - added `docs/tech-ref/17-cache-research.md`;
  - indexed the cache research doc in `docs/tech-ref/README.md` and `docs/DOC-INVENTORY.md`;
  - recorded local smoke evidence in `docs/ops/evidence/PRD2-B23-product-local-smoke-20260704.md`.
- Smoke tooling:
  - `scripts/prd2-batch23-product-smoke.sh`;
  - `scripts/prd2_batch23_product_seed.go`;
  - `scripts/prd2_batch23_product_smoke.mjs`.
- Local verification passed:

```bash
go test ./internal/errdocs ./internal/middleware ./internal/admin ./internal/gateway ./cmd/uniroute
npm --prefix frontend test -- --run navigation client DeveloperConsolePage UsageAnalyticsPage InternalStatusPage
bash -n scripts/prd2-batch23-product-smoke.sh
node --check scripts/prd2_batch23_product_smoke.mjs
```

- Local current-code real backend/browser smoke passed against backend `http://localhost:18083`, frontend `http://localhost:3000`, local PostgreSQL/Redis, and mock upstream:

```json
{
  "status": "PASS",
  "batch": 23,
  "project_id": 35,
  "model_alias": "batch23-company-chat",
  "provider": "batch23-mock",
  "upstream_calls": 1,
  "estimated_ratio": 0.2597402597402597,
  "internal_status": "warning",
  "screenshot": "/Users/m4/Desktop/projects/unirouter/output/playwright/prd2-batch23-product.png"
}
```

- At initial Codex handoff, acceptance state remained `Implemented not accepted` until Canary/Sentinel completed. Batch 18 real-provider upstream evidence and P2-F10 target-environment License evidence were kept as separate open gaps.

Post-verification update on 2026-07-04:

- `docs/agent-workflows/VERIFY-RESULT.md` reports Batch 23 PASS (6/6).
- User reported Canary, Sentinel, and Atlas completed.
- Matrix P2 product rows were advanced to accepted state for PRD2 scope.
- Canary noted one non-blocking future hardening item: `internal/errdocs` has no independent unit test file and is currently covered through gateway/middleware integration tests. This does not reopen Batch 23; add direct unit tests if the registry grows.
- Batch 18 real-provider upstream evidence and P2-F10 target-environment License evidence remain separate open gaps.

### Batch 24: OIDC First Authentication Integration

Goal: implement the user-confirmed SSO path after core PRD2 product completion.

Required work:

- Add OIDC provider configuration.
- Implement login initiation and callback.
- Map OIDC identity claims to UniRouter users/orgs/roles.
- Define first-login provisioning rules.
- Add RBAC mapping tests and negative tests.
- Add operator docs for provider setup.

Boundary:

- OIDC is first.
- LDAP/SAML are not first unless user later expands scope.

Target issue:

- `PRD2-AUTH-001`.

Status update on 2026-07-04:

- Codex implemented Batch 24 locally and handed it to Canary via `docs/agent-workflows/FIX-SIGNAL.md`.
- Backend/auth:
  - added `internal/oidc` with OIDC discovery, authorization URL construction, token exchange, JWKS loading, RS256 `id_token` verification, issuer/audience/expiry/nonce checks, and claim mapping;
  - added `GET /api/v1/auth/oidc/config`, `GET /api/v1/auth/oidc/start`, and `GET /api/v1/auth/oidc/callback`;
  - added HMAC-signed HttpOnly SameSite=Lax state/nonce cookie handling;
  - added callback JWT issuance using the existing UniRouter admin JWT format;
  - added `login.oidc` audit without raw token/provider-response leakage.
- Data/config:
  - added migration `000015_prd2_oidc_sso` for `users.email`, `users.auth_provider`, `users.oidc_issuer`, and `users.oidc_subject`;
  - added OIDC env/config variables and Docker Compose pass-through;
  - auto-provision mode requires an active default org and can assign project-scoped roles to the default project.
- Frontend/docs:
  - LoginPage reads public OIDC config and shows an SSO button only when enabled;
  - API client includes OIDC config/start helpers;
  - added `docs/tech-ref/18-oidc-sso.md`, deployment runbook notes, and local evidence `docs/ops/evidence/PRD2-B24-oidc-local-smoke-20260704.md`;
  - added `scripts/prd2-batch24-oidc-smoke.sh`.
- Local verification passed:

```bash
go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute -count=1
npm --prefix frontend test -- --run LoginPage client navigation
scripts/prd2-batch24-oidc-smoke.sh
```

- Current acceptance state: `PRD2-AUTH-001` is `Fixed awaiting Canary`; matrix row `OIDC first SSO integration` is `Implemented not accepted`.
- Real enterprise IdP browser login remains target-environment evidence and requires approved issuer/client credentials.

### Batch 25: Final PRD2 Closeout

Goal: close the phase only after all preceding batches are accepted.

Required work:

- Reconcile every matrix row to `E2E accepted`, `Production verified`, `Deferred`, or `Out of scope`.
- Ensure no PRD2 `Open` or `Fixed awaiting Canary` issues remain without explicit user decision.
- Run global backend/frontend tests and browser smoke.
- Run final Canary verification.
- Sentinel merges final closeout PR.
- Atlas deploys and records final target-environment evidence.

## PRD3 / Later Unimplemented Items

These items are intentionally not part of the PRD2 closeout implementation unless the user later changes scope. They are tracked as active unfinished work in `docs/issues/OPEN-ISSUES.md`:

| Item | Tracking | PRD2 Boundary |
|------|----------|---------------|
| Runtime plugin platform | `PRD3-PLUGIN-001` | PRD2 may define extension boundaries, but does not add plugin manifests, dynamic loading, sandboxing, plugin permissions, lifecycle management, or third-party plugin installation |
| Plugin marketplace / third-party install path | `PRD3-PLUGIN-001` | PRD2 remains local-deploy enterprise product, not an app marketplace |
| Enterprise deployment packaging and IaC | `PRD3-DEPLOY-001` | PRD2 keeps Docker Compose/self-hosted runbooks and two-instance validation; Kubernetes, Helm, Terraform, and managed-HA references are PRD3/later |
| Full engineering handbook and release governance | `PRD3-ENG-001` | PRD2 uses existing `CONTRIBUTING.md`, agent protocol, runbooks, and acceptance matrix; ADR policy, API versioning, migration policy, release checklist, incident response, and deprecation policy are PRD3/later |

## External Inputs Needed Later

| Input | Needed For | Can Proceed Without It? |
|-------|------------|--------------------------|
| OpenRouter API key and Claude model slug | Batch 18A | Yes, but only mock/OpenAI-compatible local evidence can be recorded |
| Direct Anthropic API key | Batch 18B | No, direct native E2E remains open without it |
| Test offline license and public key | Batch 22 | Can generate local/staging sample, but production evidence needs target key/license |
| Webhook receiver endpoint | Batch 19 | Can use local receiver first; production evidence needs target endpoint |
| Staging/two-instance target | Batch 21 | Local docker-compose can start first |
| OIDC provider details | Batch 24 | Can design first, implementation needs provider metadata/client credentials |

## Agent Loop

Every implementation batch follows:

1. Codex implements and updates `docs/agent-workflows/FIX-SIGNAL.md`.
2. Canary reads `FIX-SIGNAL.md`, verifies independently, and overwrites `docs/agent-workflows/VERIFY-RESULT.md`.
3. If Canary fails, Codex fixes and repeats.
4. If Canary passes, Sentinel handles PR/CI/merge.
5. Atlas deploys and verifies target-environment evidence where the batch needs deployment.
