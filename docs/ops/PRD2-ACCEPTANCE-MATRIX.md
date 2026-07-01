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
| P2-F01 Project / Workspace data model | Unit tested | `migrations/000009_prd2_project_foundation.*` adds `projects`, `project_members`, project FKs, default project backfill; `go test ./...` passed locally on 2026-07-01 | Canary verification, migration-on-existing-DB evidence, E2E gateway call evidence |
| Existing PRD1 keys map to default project | Implemented not accepted | Migration backfills `api_keys.project_id` and `call_logs.project_id` to each org default project | Migration test using existing-style org key and Canary verification |
| Project-scoped API keys | Unit tested | API key create/list/detail/update carry `project_id`; GatewayAuth injects project context; `go test ./internal/middleware ./internal/admin` passed locally on 2026-07-01 | Frontend flow, E2E create key, call gateway, verify project log |
| Project usage and logs | Unit tested | `call_logs.project_id` is written by relay and `/api/v1/logs?project_id=` filter/response is implemented; `go test ./internal/gateway ./internal/admin` passed locally on 2026-07-01 | RBAC negative tests, UI verification, usage dashboard/API drill-down |
| P2-F02 RBAC expansion | Planned | PRD2 role table defines platform/org/billing/project/developer/viewer roles | Backend middleware/handler checks, route parity, negative tests |
| Developer cannot access Admin Console by URL | Planned | PRD2 acceptance requires backend source of truth | Browser or API E2E with developer token |
| Billing admin cannot edit provider channels | Planned | PRD2 role model separates billing from provider operations | API negative test |
| Project admin cannot access other projects | Planned | PRD2 role model scopes project admin to project | Cross-project negative test |
| P2-F03 Developer Console MVP | Planned | PRD2 defines project overview, keys, model catalog, usage, request troubleshooting, docs entry | Frontend spec, component tests, route parity, browser smoke |
| Developer can create project key and call gateway | Planned | PRD2 Developer Console acceptance states create-key-to-call path | E2E create key, call model, verify log/project usage |
| Request troubleshooting hides restricted data | Planned | PRD2 says no provider key/admin data exposure | API and UI negative tests |
| P2-F04 Model Registry | Planned | PRD2 defines registry fields and lifecycle | Schema/API/UI implementation, lifecycle tests |
| Approved Model Catalog hides unapproved models | Planned | PRD2 rejects long default marketplace list | Developer catalog E2E with approved/unapproved fixtures |
| Model Set assignment controls project access | Planned | PRD2 defines model sets and project allowed models | Project allowlist test; unauthorized model returns 403 |
| Model Alias stable names work | Planned | PRD2 defines aliases such as `company-chat-fast` | Alias resolution test; call log records alias and resolved model |
| Deprecated model shows migration recommendation | Planned | PRD2 requires replacement guidance | UI/API verification |
| Retired model cannot be called | Planned | PRD2 requires retired model unavailable | Gateway E2E returns model-not-available |
| P2-F05 Provider Adapter Framework | Planned | PRD2 defines downstream OpenAI-compatible and upstream adapter abstraction | Tech spec, interface, registry, openai_compatible adapter tests |
| `channels.adapter_type` exists | Planned | PRD2 requires adapter_type | Migration and channel CRUD tests |
| Current relay works as `openai_compatible` | Planned | Current code has HTTP relay through base_url/model_mapping | Refactor verification with existing provider regression |
| Adapter output normalizes usage/error/meta | Planned | PRD2 requires billing/logs/circuit consume normalized result | Mock upstream tests for success, 401, 429, 5xx, stream |
| P2-F06 Token Metering V2 | Planned | PRD2 requires provider usage normalization and usage_source granularity | Metering spec, implementation, reconciliation tests |
| Missing stream usage records non-upstream source | Planned | PRD1 has estimated stream paths; PRD2 expands source types | Stream mock test without usage |
| Estimated-vs-upstream variance visible | Planned | PRD2 requires variance report/alert | Reconciliation API/UI test |
| P2-F07 Audit schema upgrade | Implemented not accepted | `audit_logs.project_id` added and project/key create paths can write project context | before/after schema, write-operation audit tests, UI/API filters |
| Model lifecycle changes audit before/after | Planned | PRD2 requires lifecycle audit | API test |
| Project key creation audit includes project | Implemented not accepted | API key create calls `recordAuditForProject` with `project_id` | API test and Canary verification |
| Developer cannot read global audit logs | Planned | PRD2 role model restricts audit | RBAC negative test |
| P2-F12 Acceptance discipline | Implemented not accepted | This matrix is updated for PRD2 implementation batch 1 and `FIX-SIGNAL.md` is prepared for Canary handoff | Canary PASS |

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
