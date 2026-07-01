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
| P2-F01 Project / Workspace data model | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports Fix #1 PASS and global Go tests PASS; User reported Sentinel completed | Migration-on-existing-DB evidence, E2E gateway call evidence |
| Existing PRD1 keys map to default project | Implemented not accepted | `docs/agent-workflows/VERIFY-RESULT.md` statically verifies default project creation and backfill SQL; User reported Sentinel completed | Migration test using existing-style org key |
| Project-scoped API keys | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports Fix #3 PASS; API key create/list/detail/update carry `project_id`; User reported Sentinel completed | Frontend flow, E2E create key, call gateway, verify project log |
| Project usage and logs | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports Fix #4/#5 PASS; relay writes `call_logs.project_id` and `/api/v1/logs?project_id=` filter exists | RBAC negative tests, UI verification, usage dashboard/API drill-down |
| P2-F02 RBAC expansion | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 2 5/5 PASS; User reported Sentinel completed | Route-level E2E with real JWTs, frontend navigation gating |
| Developer cannot access Admin Console by URL | Implemented not accepted | Batch 2 route gates deny developer role from platform-only/channel/admin-audit routes; Canary verified audit route excludes developer/viewer; User reported Sentinel completed | Browser or API E2E with developer token |
| Billing admin cannot edit provider channels | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports `TestRequireRoles_DeniesBillingAdminForPlatformOnlyRoute` PASS and channel routes protected by `platformRoute`; User reported Sentinel completed | API E2E through `/api/v1/channels` |
| Project admin cannot access other projects | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports project/key/log handlers require `project_members`, and project-scoped log tests PASS; User reported Sentinel completed | Cross-project API negative test with real project fixtures |
| P2-F03 Developer Console MVP | Planned | PRD2 defines project overview, keys, model catalog, usage, request troubleshooting, docs entry | Frontend spec, component tests, route parity, browser smoke |
| Developer can create project key and call gateway | Implemented not accepted | Batch 2 permits `developer` on project key creation only with explicit project membership and `project_id`; Canary verified relevant handler tests PASS; User reported Sentinel completed | E2E create key, call model, verify log/project usage |
| Request troubleshooting hides restricted data | Implemented not accepted | Batch 2 denies project-scoped roles from provider/channel-key/audit global routes and requires `project_id` for logs; Canary verified no frontend overclaim | API and UI negative tests |
| P2-F04 Model Registry | Unit tested | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD2 implementation batch 3 6/6 PASS; User reported Sentinel completed | Frontend UI, migration-on-existing-DB evidence |
| Approved Model Catalog hides unapproved models | Unit tested | Canary verified `ListApprovedModelCatalog` filters `approved_catalog = TRUE` and lifecycle `ga/deprecated`; User reported Sentinel completed | Developer catalog E2E with approved/unapproved fixtures |
| Model Set assignment controls project access | Unit tested | Canary verified project model-set assignment APIs and `modelgov.CheckAccess`; User reported Sentinel completed | Gateway E2E unauthorized model returns 403 with real project/key fixture |
| Model Alias stable names work | Unit tested | Canary verified alias resolution, gateway rewrite, and relay `requested_model` preservation; User reported Sentinel completed | End-to-end call log verification with alias and resolved model |
| Deprecated model shows migration recommendation | Implemented not accepted | Canary verified registry stores and returns `migration_recommendation`; approved catalog includes `deprecated` models | UI/API verification with deprecated fixture |
| Retired model cannot be called | Unit tested | Canary verified `modelgov.CheckAccess` rejects `retired` lifecycle and gateway 403 path exists; User reported Sentinel completed | Gateway E2E returns model-not-available style response |
| P2-F05 Provider Adapter Framework | Unit tested | Batch 4 adds `internal/adapters` interface, registry, `openai_compatible` implementation, channel adapter metadata, gateway adapter request path, channel-test adapter path, and focused adapter/admin/gateway tests | Canary verification, native adapter proof, full stream normalization through adapter boundary, real upstream E2E |
| `channels.adapter_type` exists | Unit tested | Batch 4 migration `000011_prd2_provider_adapter_foundation` adds `channels.adapter_type` and `channels.adapter_config`; channel create/update/list/get handlers and tests cover adapter metadata/default/validation | Migration-on-existing-DB evidence |
| Current relay works as `openai_compatible` | Unit tested | Batch 4 routes chat and embeddings request construction through the adapter registry; existing gateway relay/router mocks and focused tests pass locally | Real upstream regression, stream response normalization migration |
| Adapter output normalizes usage/error/meta | Unit tested | `openai_compatible` tests cover request rewrite, response usage/meta parsing, and HTTP status classification for client/auth/rate-limit/channel failures | Stream normalization, provider-specific native error mapping |
| P2-F06 Token Metering V2 | Planned | PRD2 requires provider usage normalization and usage_source granularity | Metering spec, implementation, reconciliation tests |
| Missing stream usage records non-upstream source | Planned | PRD1 has estimated stream paths; PRD2 expands source types | Stream mock test without usage |
| Estimated-vs-upstream variance visible | Planned | PRD2 requires variance report/alert | Reconciliation API/UI test |
| P2-F07 Audit schema upgrade | Implemented not accepted | `docs/agent-workflows/VERIFY-RESULT.md` reports Fix #5 PASS for `audit_logs.project_id` and `recordAuditForProject` | before/after schema, write-operation audit tests, UI/API filters |
| Model lifecycle changes audit before/after | Planned | PRD2 requires lifecycle audit | API test |
| Project key creation audit includes project | Implemented not accepted | `docs/agent-workflows/VERIFY-RESULT.md` reports API key create calls `recordAuditForProject` with `project_id` | API test |
| Developer cannot read global audit logs | Implemented not accepted | `docs/agent-workflows/VERIFY-RESULT.md` reports audit route allows only platform/org/billing and excludes developer/viewer | API E2E with developer JWT |
| P2-F12 Acceptance discipline | Unit tested | Batch 4 matrix records only local unit-test-backed Provider Adapter foundation evidence and keeps E2E/native adapter/stream-normalization gaps open | Canary verification for batch 4 |

## P1 Product Scope

| PRD2 Item | Status | Current Evidence | Gap To Acceptance |
|-----------|--------|------------------|-------------------|
| P2-F08 Notification/event foundation | Planned | PRD2 defines budget, key, model, provider, license events | Event schema, webhook tests, delivery failure visibility |
| Project budget alerts | Planned | PRD2 requires project budget configuration and threshold event | API/UI implementation and threshold E2E |
| P2-F09 HA readiness | Planned | PRD2 defines multi-instance readiness without mandatory HA stack | Runbook, two-instance smoke, idempotency/wallet safety test |
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
