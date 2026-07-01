# PRD2 Acceptance Matrix

> Purpose: track acceptance against `docs/PRD-Phase2.md`.
> Status values: `Planned`, `Implemented not accepted`, `Unit tested`, `E2E accepted`, `Production verified`, `Deferred`, `Out of scope`.
> Current phase: PRD2 planning.

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
| P2-F01 Project / Workspace data model | Unit tested | Batch 9 adds local migrated-DB E2E for 000009 default project backfill and developer project-key gateway call evidence; awaiting Canary | Canary verification for batch 9; usage/dashboard drill-down and frontend flow remain separate rows |
| Existing PRD1 keys map to default project | Unit tested | `TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration` seeds PRD1-style org/user/api_key/call_log/transaction before 000009 and verifies default project, project member, key/log/transaction backfill locally | Canary verification for batch 9 |
| Project-scoped API keys | Unit tested | `TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration` creates a project key as developer, calls `/v1/chat/completions`, and verifies `call_logs.project_id/api_key_id` locally | Canary verification for batch 9; frontend flow remains for Developer Console batch |
| Project usage and logs | Unit tested | Batch 9 gateway E2E verifies project-scoped call log write and project-admin cross-project log denial; earlier handler tests cover `/api/v1/logs?project_id=` | UI verification, usage dashboard/API drill-down |
| P2-F02 RBAC expansion | Unit tested | Batch 9 route-level local E2E uses real JWTs to deny developer/billing-admin platform routes and project-admin cross-project logs | Canary verification for batch 9; frontend navigation gating |
| Developer cannot access Admin Console by URL | Unit tested | Batch 9 route-level local E2E denies developer JWT on platform-only channel route and global audit route | Canary verification for batch 9; browser navigation gating remains for Developer Console batch |
| Billing admin cannot edit provider channels | Unit tested | Batch 9 route-level local E2E denies billing_admin JWT on platform-only channel write route | Canary verification for batch 9 |
| Project admin cannot access other projects | Unit tested | Batch 9 route-level local E2E uses project_admin JWT with membership only in one project and verifies `/api/v1/logs?project_id=<other>` returns 403 | Canary verification for batch 9 |
| P2-F03 Developer Console MVP | Planned | PRD2 defines project overview, keys, model catalog, usage, request troubleshooting, docs entry | Frontend spec, component tests, route parity, browser smoke |
| Developer can create project key and call gateway | Unit tested | Batch 9 developer local E2E creates a project key, calls a mock OpenAI-compatible upstream through real GatewayAuth/handler/Relay/Router, and verifies project log, upstream usage, idempotency settlement, and project audit | Canary verification for batch 9; frontend Developer Console flow remains separate |
| Request troubleshooting hides restricted data | Implemented not accepted | Batch 2 denies project-scoped roles from provider/channel-key/audit global routes and requires `project_id` for logs; Canary verified no frontend overclaim | API and UI negative tests |
| P2-F04 Model Registry | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 3 6/6 PASS; User reported Sentinel completed | Frontend UI, migration-on-existing-DB evidence |
| Approved Model Catalog hides unapproved models | Unit tested | Canary verified `ListApprovedModelCatalog` filters `approved_catalog = TRUE` and lifecycle `ga/deprecated`; User reported Sentinel completed | Developer catalog E2E with approved/unapproved fixtures |
| Model Set assignment controls project access | Unit tested | Batch 9 gateway local E2E assigns project model set with alias `company-chat`, then verifies unassigned `gpt-4o-mini` returns 403 `project_model_not_allowed` | Canary verification for batch 9; frontend assignment UI |
| Model Alias stable names work | Unit tested | Batch 9 gateway local E2E sends `company-chat`, mock upstream receives `gpt-4o`, and `call_logs` records `model=gpt-4o/requested_model=company-chat` | Canary verification for batch 9; real upstream regression |
| Deprecated model shows migration recommendation | Implemented not accepted | Canary verified registry stores and returns `migration_recommendation`; approved catalog includes `deprecated` models | UI/API verification with deprecated fixture |
| Retired model cannot be called | Unit tested | Batch 9 gateway local E2E verifies `legacy-model` returns 403 with `model_retired` reason before relay/billing | Canary verification for batch 9 |
| P2-F05 Provider Adapter Framework | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 4 6/6 PASS; User reported Sentinel completed | Native adapter proof, full stream normalization through adapter boundary, real upstream E2E |
| `channels.adapter_type` exists | Unit tested | Canary verified migration `000011_prd2_provider_adapter_foundation`, model fields, channel handlers, and channel adapter tests; User reported Sentinel completed | Migration-on-existing-DB evidence |
| Current relay works as `openai_compatible` | Unit tested | Batch 9 gateway local E2E reaches a mock OpenAI-compatible upstream via channel `adapter_type=openai_compatible`, decrypts channel key, parses usage, and writes `_uniroute` metadata | Canary verification for batch 9; real upstream regression, stream response normalization migration |
| Adapter output normalizes usage/error/meta | Unit tested | Canary verified `openai_compatible` interface, registry, response usage/meta parsing, and HTTP status classification tests | Stream normalization, provider-specific native error mapping |
| P2-F06 Token Metering V2 | Unit tested | Batch 9 mock-upstream E2E verifies non-stream upstream usage is written to `call_logs` as `usage_source=upstream` with `total_tokens=15`; batch 5 Canary verified V2 fields and fallback tests | Real upstream E2E, frontend source filter, full-query reconciliation aggregation, notification/dashboard event delivery |
| Missing stream usage records non-upstream source | Unit tested | Canary verified missing stream usage uses canonical `counter_v2` and gateway stream mock test covers no-upstream-usage fallback; User reported Sentinel completed | Native adapter stream fallback tests |
| Estimated-vs-upstream variance visible | Unit tested | Canary verified reconciliation usage_source filter, variance fields, source confidence, and page-scoped estimated-ratio alert summary; User reported Sentinel completed | UI filter, full-query aggregate, configured notification signal |
| P2-F07 Audit schema upgrade | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 6 6/6 PASS; User reported Sentinel completed | Frontend filters, real JWT/API E2E, broader before/after migration for all historical actions |
| Model lifecycle changes audit before/after | Unit tested | Canary verified batch 6 lifecycle audit before/after test and no overclaim; User reported Sentinel completed | API E2E with real JWT |
| Project key creation audit includes project | Unit tested | Batch 9 developer local E2E creates a project key and verifies `audit_logs.project_id`, `user_id`, `source=developer_console`, and `after_state.name` | Canary verification for batch 9 |
| Developer cannot read global audit logs | Unit tested | Batch 9 route-level local E2E denies developer JWT on `/api/v1/audit-logs` route | Canary verification for batch 9 |
| P2-F12 Acceptance discipline | Unit tested | Batch 7 Canary verified matrix did not overmark frontend/retry-worker/production webhook gaps; batch 8 Canary verified HA Readiness stayed at backend/ops foundation scope; batch 9 keeps local E2E evidence below accepted status until Canary verifies | Continue applying this discipline to later PRD2 batches |

## P1 Product Scope

| PRD2 Item | Status | Current Evidence | Gap To Acceptance |
|-----------|--------|------------------|-------------------|
| P2-F08 Notification/event foundation | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 7 7/7 PASS; User reported Sentinel completed | Frontend notification center, retry worker, production webhook E2E, remaining emitters for key/model/provider/license/estimated-usage events |
| Project budget alerts | Unit tested | Canary verified batch 7 project budget threshold event service, webhook route/delivery state tests, and budget before/after audit; User reported Sentinel completed | API/browser E2E, period-boundary integration test, production webhook delivery evidence |
| P2-F09 HA readiness | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 8 7/7 PASS; User reported Sentinel completed. Batch 8 added HA tech-ref, HA smoke script, runbook HA smoke/rollback steps, periodic sensitive-word reload, duplicate freeze race hardening, and shared-DB idempotency/wallet safety integration evidence | Two live-instance smoke in target env, load-balancer/proxy evidence, production backup/restore rehearsal |
| P2-F10 License foundation | Planned | PRD2 defines offline license, expiry, Admin page, audit | License verifier, invalid/expiring license tests |
| P2-F11 Prompt/Response storage policy | Planned | PRD2 says plaintext not stored by default and policy must be discussed | User decision, design doc, no implementation before approval |
| Native adapter proof | Planned | PRD2 default candidate is `anthropic_native` | Adapter-specific spec, mock upstream, stream usage test |
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
