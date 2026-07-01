# PRD2 Acceptance Matrix

> Purpose: track acceptance against `docs/PRD-Phase2.md`.
> Status values: `Planned`, `Implemented not accepted`, `Unit tested`, `E2E accepted`, `Production verified`, `Deferred`, `Out of scope`.
> Current phase: PRD2 implementation.

## Acceptance Rule

An item should not move to `E2E accepted` unless evidence includes:

1. Exact command or manual path.
2. Environment used.
3. Expected result.
4. Actual result.
5. Timestamp or commit/worktree reference.

An item should not move to `Production verified` unless the evidence comes from the deployment target or an explicitly approved production-like environment.

## Planning And Governance

| PRD2 Item | Status | Current Evidence | Gap To Acceptance |
|-----------|--------|------------------|-------------------|
| PRD2 formal PRD exists | E2E accepted | `docs/agent-workflows/VERIFY-RESULT.md` reports Fix #1 PASS; User reported Sentinel confirmed PRD2 planning round 1 complete and CI green | None for planning round 1 |
| PRD2 acceptance matrix exists | E2E accepted | `docs/agent-workflows/VERIFY-RESULT.md` reports Fix #2 PASS; User reported Sentinel confirmed PRD2 planning round 1 complete and CI green | Update rows as implementation starts |
| PRD1/PRD2 phase boundary is clear | E2E accepted | `docs/agent-workflows/VERIFY-RESULT.md` reports Fix #6 PASS and `CURRENT-STATUS.md` Phase Status points to PRD2 docs | None for planning round 1 |
| Sentinel GitHub guidance is documented | E2E accepted | `docs/planning/PRD2-DISCOVERY.md` documents PR separation, branch names, PR description, merge gate; round passed Canary and Sentinel CI | None for planning round 1 |

## P0 Product Scope

| PRD2 Item | Status | Current Evidence | Gap To Acceptance |
|-----------|--------|------------------|-------------------|
| P2-F01 Project / Workspace data model | E2E accepted | `docs/agent-workflows/VERIFY-RESULT.md` reports batch 9 8/8 PASS; migrated-DB E2E verifies 000009 default project backfill and developer project-key gateway call evidence | Usage/dashboard drill-down and frontend flow remain separate rows |
| Existing PRD1 keys map to default project | E2E accepted | Canary verified `TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration`: PRD1-style org/user/api_key/call_log/transaction before 000009, then default project, project member, key/log/transaction backfill | None for migration E2E; deep review Low M-1 tracks historical audit project_id policy |
| Project-scoped API keys | E2E accepted | Canary verified `TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration`: developer creates project key, calls `/v1/chat/completions`, and `call_logs.project_id/api_key_id` are correct. Batch 10 Canary also verified Developer Console create flow sends selected `project_id` and `GET /keys?project_id=` is permission-checked | Real-backend browser verification for Developer Console flow |
| Project usage and logs | Unit tested | Batch 9 gateway E2E verifies project-scoped call log write and project-admin cross-project log denial; earlier handler tests cover `/api/v1/logs?project_id=`; Batch 10 Canary verified Developer Console loads logs with selected `project_id` | Browser usage dashboard/API drill-down |
| P2-F02 RBAC expansion | Unit tested | Batch 9 route-level E2E with real JWTs denies developer/billing-admin platform routes and project-admin cross-project logs; Batch 10 Canary verified navigation tests blocking developer access to admin paths and routing project users to `/developer` | Real-browser navigation smoke |
| Developer cannot access Admin Console by URL | E2E accepted | Canary verified route-level E2E denying developer JWT on platform-only channel route and global audit route; Batch 10 Canary verified frontend route helper blocks `/channels` for `developer` | Real-browser navigation gating smoke remains |
| Billing admin cannot edit provider channels | E2E accepted | Canary verified route-level E2E denying billing_admin JWT on platform-only channel write route | None for backend route gate |
| Project admin cannot access other projects | E2E accepted | Canary verified route-level E2E where project_admin has membership only in one project and `/api/v1/logs?project_id=<other>` returns 403 | None for backend route gate |
| P2-F03 Developer Console MVP | Unit tested | Batch 10 Canary verified 5/5 PASS for one-login role-aware Developer Console inside the existing frontend: project overview, project-scoped API keys, approved model catalog, recent usage, Request Lookup, and quickstart docs entry. Evidence includes `DeveloperConsolePage.test.tsx`, `navigation.test.tsx`, `client.test.ts`, `npm --prefix frontend test`, `npm --prefix frontend run build`, targeted Go tests, and `go test ./... -count=1`; User reported Sentinel completed. Codex Playwright mock-browser smoke passed for login, unauthenticated `/developer` redirect, and mocked `/developer` desktop/mobile rendering | Real-backend browser smoke before E2E accepted |
| Developer can create project key and call gateway | E2E accepted | Canary verified batch 9 developer E2E: project key creation, mock OpenAI-compatible upstream call through real GatewayAuth/handler/Relay/Router, project log, upstream usage, idempotency settlement, and project audit. Canary verified Batch 9.5 settle retry and cancel-on-failure for `PRD2-ERR-001` | Frontend Developer Console flow remains separate |
| Request troubleshooting hides restricted data | Unit tested | Batch 2 denies project-scoped roles from provider/channel-key/audit global routes and requires `project_id` for logs; Batch 10 Canary verified Developer Console lookup uses `/logs?project_id=<id>&request_id=<id>` then `/logs/{id}` and UI only renders status/model/provider/channel/tokens/cost/latency/error summary, not provider key/full key/prompt/response | Browser negative test, API negative tests for cross-project lookup |
| P2-F04 Model Registry | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 3 6/6 PASS and Batch 11 4/4 PASS; User reported Sentinel completed. Batch 11 verifies admin `/model-governance` UI coverage for registry listing, lifecycle/catalog fields, migration recommendation rendering, create/edit form wiring, and frontend API client methods | Migration-on-existing-DB evidence; real-browser verification |
| Approved Model Catalog hides unapproved models | Unit tested | Canary verified `ListApprovedModelCatalog` filters `approved_catalog = TRUE` and lifecycle `ga/deprecated`; Batch 10 Canary verified Developer Console loads `/model-catalog?project_id=<id>` and renders lifecycle/aliases/capability/data-residency fields. Batch 11 Canary verified admin controls for `approved_catalog` and registry filtering by approved-only | Developer catalog E2E with approved/unapproved fixtures; real-browser verification |
| Model Set assignment controls project access | E2E accepted | Canary verified batch 9 gateway E2E assigning project model set with alias `company-chat` and denying unassigned `gpt-4o-mini` as 403 `project_model_not_allowed`. Batch 11 Canary verified admin project model-set assignment page and API client coverage for `GET/PUT /projects/{id}/model-sets` | Real-browser verification for assignment UI |
| Model Alias stable names work | E2E accepted | Canary verified batch 9 gateway E2E where request `company-chat` reaches mock upstream as `gpt-4o` and `call_logs` records `model=gpt-4o/requested_model=company-chat` | Real upstream regression |
| Deprecated model shows migration recommendation | Unit tested | Canary verified registry stores and returns `migration_recommendation`; approved catalog includes `deprecated` models. Batch 11 Canary verified `ModelGovernancePage.test.tsx` renders a migration recommendation fixture | Deprecated approved-catalog browser fixture |
| Retired model cannot be called | E2E accepted | Canary verified batch 9 gateway E2E returning 403 with `model_retired` reason for `legacy-model` before relay/billing | None for backend gateway path |
| P2-F05 Provider Adapter Framework | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 4 6/6 PASS; User reported Sentinel completed. Batch 12 Canary verified `anthropic_native` non-stream proof, default registry registration, adapter-driven failure classification, and admin channel UI support; Sentinel merge is reflected in `main` commit `24e0abf`; User reported Atlas completed. Batch 13 Anthropic Native Stream Translation passed Canary/Sentinel/Atlas per User report and PR #44 deployed; User reported the Anthropic stream feature is online | Real upstream Anthropic E2E with configured credentials |
| `channels.adapter_type` exists | Unit tested | Canary verified migration `000011_prd2_provider_adapter_foundation`, model fields, channel handlers, and channel adapter tests; User reported Sentinel completed | Migration-on-existing-DB evidence |
| Current relay works as `openai_compatible` | E2E accepted | Canary verified batch 9 gateway E2E reaching a mock OpenAI-compatible upstream via `adapter_type=openai_compatible`, decrypting channel key, parsing usage, and writing `_uniroute` metadata | Real upstream regression, stream response normalization migration |
| Adapter output normalizes usage/error/meta | Unit tested | Canary verified `openai_compatible` interface, registry, response usage/meta parsing, and HTTP status classification tests. Batch 12 Canary verified Anthropic native request conversion, response conversion to OpenAI-compatible body, upstream usage mapping, channel test behavior, and provider-specific 400/401/429/5xx/529 failure classification. Batch 13 verified Anthropic stream event conversion to OpenAI-compatible chunks, upstream stream usage mapping, and final `_uniroute` ordering; User reported Canary/Sentinel/Atlas completed and PR #44 deployed | Real upstream Anthropic E2E with configured credentials |
| P2-F06 Token Metering V2 | Unit tested | Canary verified batch 9 mock-upstream E2E writes non-stream upstream usage to `call_logs` as `usage_source=upstream` with `total_tokens=15`; batch 5 Canary verified V2 fields and fallback tests; batch 1-9 regression sweep passed global/unit/integration/frontend checks. Canary verified Batch 9.5 failed-settlement retry/cancel handling and stale frozen cleanup | Real upstream E2E, frontend source filter, full-query reconciliation aggregation, notification/dashboard event delivery |
| Missing stream usage records non-upstream source | Unit tested | Canary verified missing stream usage uses canonical `counter_v2` and gateway stream mock test covers no-upstream-usage fallback; User reported Sentinel completed. Batch 13 adapter and gateway tests verify Anthropic content deltas without `message_delta.usage` remain eligible for stream counter fallback and settle with `counter_v2`; User reported Canary/Sentinel/Atlas completed | Real upstream Anthropic stream without usage metadata |
| Estimated-vs-upstream variance visible | Unit tested | Canary verified reconciliation usage_source filter, variance fields, source confidence, and page-scoped estimated-ratio alert summary; User reported Sentinel completed | UI filter, full-query aggregate, configured notification signal |
| P2-F07 Audit schema upgrade | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 6 6/6 PASS; User reported Sentinel completed | Frontend filters, real JWT/API E2E, broader before/after migration for all historical actions |
| Model lifecycle changes audit before/after | Unit tested | Canary verified batch 6 lifecycle audit before/after test and no overclaim; User reported Sentinel completed | API E2E with real JWT |
| Project key creation audit includes project | E2E accepted | Canary verified batch 9 developer E2E creates a project key and verifies `audit_logs.project_id`, `user_id`, `source=developer_console`, and `after_state.name` | Channel CRUD audit completeness tracked as Low follow-up in `OPEN-ISSUES.md` |
| Developer cannot read global audit logs | E2E accepted | Canary verified batch 9 route-level E2E denying developer JWT on `/api/v1/audit-logs` route | None for backend route gate |
| P2-F12 Acceptance discipline | Unit tested | Batch 7 Canary verified matrix did not overmark frontend/retry-worker/production webhook gaps; batch 8 Canary verified HA Readiness stayed at backend/ops foundation scope; batch 9 advanced only Canary-verified backend E2E rows and kept broader product gaps open; batch 1-9 full-context dynamic regression sweep passed `go test ./...`, PRD2 integration tests, billing concurrency integration, frontend tests/build, and `git diff --check`; Batch 9.5 Canary passed and User reported Sentinel completed. PR #44 deployment incident is tracked as `PRD2-DEPLOY-001` instead of being hidden under feature success | Continue applying this discipline to later PRD2 batches; keep remaining Low hardening items and Batch 14 deployment hardening open until fixed and reverified |

## P1 Product Scope

| PRD2 Item | Status | Current Evidence | Gap To Acceptance |
|-----------|--------|------------------|-------------------|
| P2-F08 Notification/event foundation | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 7 7/7 PASS; User reported Sentinel completed | Frontend notification center, retry worker, production webhook E2E, remaining emitters for key/model/provider/license/estimated-usage events |
| Project budget alerts | Unit tested | Canary verified batch 7 project budget threshold event service, webhook route/delivery state tests, and budget before/after audit; User reported Sentinel completed | API/browser E2E, period-boundary integration test, production webhook delivery evidence |
| P2-F09 HA readiness | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 8 7/7 PASS; User reported Sentinel completed. Batch 8 added HA tech-ref, HA smoke script, runbook HA smoke/rollback steps, periodic sensitive-word reload, duplicate freeze race hardening, and shared-DB idempotency/wallet safety integration evidence. PR #44 deployment incident showed release sync can still delete production `.env`; Batch 14 local work adds `.env`/`.env.production` protection guidance and API-key hash secret separation | Two live-instance smoke in target env, load-balancer/proxy evidence, production backup/restore rehearsal, Canary verification for Batch 14 deployment-secret hardening |
| Deployment secret persistence and API key hash secret separation | Implemented not accepted | Batch 14 local work adds `API_KEY_HASH_SECRET` with fallback to `JWT_SECRET`, wires Admin API key creation and Gateway API key verification to that secret, updates compose/env examples, and documents safe `rsync --delete` exclusions plus PR #44 incident handling in the deployment runbook | Canary verification, Sentinel/Atlas deployment, server `.env`/`.env.production` preservation evidence, and post-deploy API Key smoke using a key created under the stable hash secret |
| P2-F10 License foundation | Planned | PRD2 defines offline license, expiry, Admin page, audit | License verifier, invalid/expiring license tests |
| P2-F11 Prompt/Response storage policy | Planned | PRD2 says plaintext not stored by default and policy must be discussed | User decision, design doc, no implementation before approval |
| Native adapter proof | Unit tested | Batch 12 Canary verified `anthropic_native` for non-stream `/v1/chat/completions`: OpenAI-compatible downstream request conversion to Anthropic `/v1/messages`, native response normalization back to OpenAI-compatible body, upstream usage mapping, adapter-specific error classification, channel test support, and admin UI adapter selection. Sentinel merge is reflected in `main` commit `24e0abf`; User reported Atlas completed. Batch 13 adds `stream:true` request conversion, Anthropic native SSE translation, stream usage mapping, and final `_uniroute` ordering tests; User reported Canary/Sentinel/Atlas completed and PR #44 deployed | Real upstream Anthropic E2E with configured credentials |
| SSO/LDAP/OIDC integration | Planned | PRD2 reserves schema/interface, P1 implementation | Provider choice, config spec, login/RBAC mapping tests |

## P2 Product Scope

| PRD2 Item | Status | Current Evidence | Gap To Acceptance |
|-----------|--------|------------------|-------------------|
| P2-F13 Playground integrated with approved catalog | Planned | PRD2 defines project-key playground with no shared key | UI/API implementation, browser smoke |
| Error docs with `doc_url` | Planned | PRD2 defines error-to-doc linkage | Error schema update, docs pages, API tests |
| Internal status page / incident history | Planned | PRD2 defines internal status page as P2 | Status schema/API/UI and incident tests |
| P2-F14 Advanced usage analytics | Planned | PRD2 defines drill-down by org/project/key/model/alias/provider/channel/status/usage_source | Analytics API/UI tests and export RBAC |
| P2-F15 Cache research spike | Planned | PRD2 defines research only, not production default | Research note with privacy/billing/audit/correctness evaluation |

## Explicitly Deferred / Out Of Scope For PRD2

| Item | Status | Reason |
|------|--------|--------|
| Reseller / agent channel system | Deferred | User confirmed not PRD2 |
| Payment / invoice / real-name / coupon / voucher / campaign | Deferred | Public SaaS and growth operations scope |
| Public SaaS dual Portal | Deferred | PRD2 remains local-deploy enterprise product |
| Cross-model fallback | Out of scope | PRD1/PRD2 billing pre-freeze mismatch risk |
| Full intelligent routing across unrelated models | Deferred | Requires later routing policy and quality model |
| Full official SDK suite | Deferred | Continue OpenAI SDK compatibility first |
| Complex semantic cache as runtime default | Deferred | Needs privacy, correctness, audit, and billing research |
| Full support-ticket CRM | Deferred | Request troubleshooting is enough for PRD2 core |
| Online license marketplace | Deferred | PRD2 license is local-deploy foundation only |

## Verification Commands For Planning Docs

These commands validate the planning/documentation round:

```bash
cd /Users/m4/Desktop/projects/UniRouter
test -f docs/PRD-Phase2.md
test -f docs/ops/PRD2-ACCEPTANCE-MATRIX.md
test -f docs/tech-ref/10-provider-adapter-framework.md
test -f docs/tech-ref/11-project-rbac-model.md
test -f docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md
rg -n "PRD2" docs/DOC-INVENTORY.md docs/ops/CURRENT-STATUS.md docs/tech-ref/README.md
git diff --check
```
