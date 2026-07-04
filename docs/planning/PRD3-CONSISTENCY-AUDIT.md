# PRD3 Consistency Audit

> Date: 2026-07-05
> Status: Scope confirmed by User; source audit for `docs/PRD-Phase3.md`.
> Purpose: audit product and architecture consistency after PRD2 freeze, then provide candidate input for PRD-Phase3 scope.

## 1. Audit Inputs

Reviewed current repo state, not only prior chat context.

Primary current-state inputs:

- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD2-COMPLETION-PLAN.md`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD2-B28-product-consistency-local-20260704.md`
- `docs/ops/evidence/PRD2-final-freeze-20260705.md`
- `docs/agent-workflows/AGENT-PROTOCOL.md`
- `docs/DOCS-GOVERNANCE.md`
- `docs/DOC-INVENTORY.md`
- `docs/PRD-Phase2.md`

Source-code and contract inputs:

- backend route wiring: `cmd/uniroute/main.go`
- frontend navigation and routes: `frontend/src/navigation.tsx`, `frontend/src/main.tsx`
- frontend API client: `frontend/src/api/client.ts`
- frontend display labels: `frontend/src/utils/displayLabels.ts`
- usage analytics handler: `internal/admin/prd2_product_handlers.go`
- RBAC helpers: `internal/auth/auth.go`, `internal/middleware/rbac.go`, `internal/admin/rbac_helpers.go`
- provider adapter registry: `internal/adapters/registry.go`, `internal/adapters/types.go`
- deployment assets: `Dockerfile`, `docker-compose.yml`, `docker-compose.canary.yml`, `nginx/`, `infra/`, `.github/workflows/ci.yml`
- OpenAPI snapshot: `docs/tech-ref/05-openapi-spec.yaml`

Spot-check commands used:

```bash
rg -n "PRD3|PRD-Phase3|Phase3|CONSISTENCY|AUDIT|Phase 3|PRD-Phase" -S .
rg -n "plugin|manifest|sandbox|marketplace|third-party|runtime loading|dynamic loading" --glob '!web/**' --glob '!frontend/package-lock.json' --glob '!docs/**' .
rg --files | rg '(^|/)(Chart\.yaml|values\.ya?ml|[^/]+\.tf|k8s|kubernetes|helm|terraform)(/|$)'
rg -n "usage/analytics|internal-status|error-docs|notification-events|notification-webhooks|model-registry|model-catalog|license|oidc|projects|model-sets" docs/tech-ref/05-openapi-spec.yaml
rg -n "PRD1 API index|PRD2 当前可用接口索引|API Documentation|API 文档" cmd/uniroute/main.go frontend/src/pages/ApiDocsPage.tsx docs/tech-ref/05-openapi-spec.yaml
```

## 2. Current Baseline

Verified from current repo files:

- PRD2 is frozen for new feature work within documented PRD2 scope.
- Batch 28 product consistency/filter parity is accepted and records shared display labels, Usage Analytics filter parity, stable `usage_source` API values, and no billing/accounting rewrite.
- `OPEN-ISSUES.md` tracks three explicit PRD3 open items: runtime plugin platform, enterprise deployment/IaC, and engineering handbook/release governance.
- PRD2 still keeps target-environment evidence gaps explicit: real upstream provider evidence, License Production verified evidence, production HA/DR, real enterprise IdP browser login, and project budget hard-block enforcement.
- Current code has static provider adapters (`openai_compatible`, `anthropic_native`) and configured notification/webhook extension points, but no runtime plugin manifest/loading/sandbox/permission/lifecycle system.
- Current deployment assets are Docker Compose, canary Nginx, Prometheus/Grafana, load-test scripts, and CI Docker build. No Helm, Terraform, Kubernetes manifests, or managed-HA package are present.

## 2.1 User-Confirmed PRD3 Boundary

User clarified after the first audit draft:

- PRD3 should not implement new product capabilities.
- PRD3 should optimize and reconcile already implemented functionality, code, modules, structure, panel semantics, labels, permissions, and documentation.
- Runtime plugins, enterprise IaC/deployment expansion, hard-block budget enforcement, and other new capabilities should move to PRD4 unless User explicitly reopens scope.
- PRD3 should identify ambiguous, guessed, unconfirmed, placeholder, or internally contradictory behavior so PRD4 can start from a cleaner baseline.

## 2.2 PRD1 To PRD2 Lineage Findings

PRD3 must account for the full PRD1 -> PRD2 product lineage, not only the PRD2 freeze state.

| ID | Area | Lineage Finding | Evidence | PRD3 Direction |
|----|------|-----------------|----------|----------------|
| PRD3-LF-01 | Product model | PRD1 was an org/admin MVP centered on channels, API keys, wallet, logs, settings, and three admin-oriented roles. PRD2 added project, developer console, model governance, OIDC, notifications, usage analytics, and internal status. The product model grew, but the original org/admin naming still leaks into new project/governance surfaces. | `docs/PRD-Phase1.md`; `docs/planning/PRD2-DISCOVERY.md`; `docs/PRD-Phase2.md`; `frontend/src/navigation.tsx`. | PRD3 should be framed as a PRD1 -> PRD2 consistency and architecture cleanup phase. |
| PRD3-LF-02 | Roles | PRD1 defined `platform_admin`, `org_admin`, and `org_viewer`. PRD2 added `billing_admin`, `project_admin`, `developer`, and `viewer`, but `viewer` is still ambiguous across org and project scopes. | `docs/PRD-Phase1.md`; `docs/tech-ref/11-project-rbac-model.md`; `internal/auth/auth.go`; `frontend/src/navigation.tsx`. | PRD3 should make org-level read-only and project-level read-only semantics explicit, and avoid treating "normal user" as a product role name. |
| PRD3-LF-03 | Provider/channel taxonomy | PRD1 used `provider` and `channel` in gateway metadata but did not fully define their product distinction. PRD2 added provider adapters and usage drill-down, making the ambiguity user-facing. | `docs/PRD-Phase1.md`; `docs/planning/PRD2-DISCOVERY.md`; `frontend/src/pages/UsageAnalyticsPage.tsx`; `internal/admin/prd2_product_handlers.go`. | PRD3 should define supplier/provider/vendor as the provider family or adapter dimension, and upstream channel as a concrete configured route instance. |
| PRD3-LF-04 | Developer experience | PRD1 treated developers mainly as API callers. PRD2 introduced Developer Console, but fallback navigation can still use API Docs as a landing page for unmapped roles. | `docs/PRD-Phase1.md`; `docs/PRD-Phase2.md`; `frontend/src/navigation.tsx`; `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md`. | PRD3 should make every role landing page intentional: project workbench, finance view, read-only overview, or no-access state. |
| PRD3-LF-05 | UI architecture | PRD1 UI fixes created a usable design layer, but earlier frontend audit already warned that page-level code, API contracts, and UI patterns were not centralized. PRD2 added more complex pages on top of that architecture. | `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md`; `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md`; `frontend/src/pages/*.tsx`. | PRD3 should extract shared role, label, filter, table, form, empty/error, and API contract patterns before PRD4 expands features. |
| PRD3-LF-06 | Operational status | PRD1 had health checks and monitoring. PRD2 added notification events and an internal status page, but maintenance windows and status semantics are not yet a full product model. | `docs/PRD-Phase1.md`; `docs/tech-ref/14-notification-event-foundation.md`; `frontend/src/pages/InternalStatusPage.tsx`; `migrations/000014_prd2_notification_event_foundation.up.sql`. | PRD3 should align the page to the existing event lifecycle (`open`, `acknowledged`, `resolved`) and move maintenance scheduling, SLA/SLO, and upgrade communication to PRD4. |
| PRD3-LF-07 | Document lifecycle | PRD1/PRD2 docs, acceptance matrices, API docs, and first-read files now mix old phase states and route snapshots. | `docs/DOC-INVENTORY.md`; `docs/README.md`; `docs/PROJECT-HANDOFF.md`; `docs/tech-ref/05-openapi-spec.yaml`; `cmd/uniroute/main.go`. | PRD3 should clean up phase state labels and define which files are requirements, current truth, evidence, or historical material. |

## 3. Findings

| ID | Severity | Area | Finding | Evidence | PRD3 Implication |
|----|----------|------|---------|----------|------------------|
| PRD3-CA-01 | High | Roles and navigation | Frontend and backend do not fully agree on `org_admin` financial access. Frontend gives `org_admin` the full admin navigation except License, including `wallet`, `reconciliation`, and `grants`; backend protects those with `billingRoute`, which allows only `platform_admin` and `billing_admin`. | `frontend/src/navigation.tsx`; `cmd/uniroute/main.go` `billingRoute`; `frontend/src/__tests__/navigation.test.tsx` says org admins keep full admin routes. | PRD3 should first settle role semantics, then align nav, route guards, tests, and PRD wording. |
| PRD3-CA-02 | High | Viewer semantics | PRD2 role table says `viewer` is read-only for audit, usage, and configuration access, but current frontend treats `viewer` as a project-console role with only `/developer` and `/docs`; backend denies global audit to project roles and only project-scopes usage/config style reads. This may be correct product intent, but the written role definition is broader than implementation. | `docs/PRD-Phase2.md`; `frontend/src/navigation.tsx`; `cmd/uniroute/main.go`; `internal/auth/auth.go`. | PRD3 should define whether `viewer` means org-level read-only, project-level read-only, or both as separate roles. |
| PRD3-CA-03 | High | API contract | API documentation is split and stale. Backend `/docs` still says "PRD1 API index" and lists a partial set; frontend `/docs` says "PRD2 当前可用接口索引"; OpenAPI only matched `/api/v1/projects` among sampled PRD2 routes and misses `usage/analytics`, `internal-status`, `error-docs`, notification routes, model governance routes, OIDC, and License. | `cmd/uniroute/main.go`; `frontend/src/pages/ApiDocsPage.tsx`; `docs/tech-ref/05-openapi-spec.yaml`; route list in `cmd/uniroute/main.go`. | PRD3 should include an API contract refresh before SDKs, plugins, or customer integration work. |
| PRD3-CA-04 | Medium | Current truth docs | Some first-read docs still describe earlier PRD1 state while `CURRENT-STATUS.md` says PRD2 is frozen. `PROJECT-HANDOFF.md` still says full PRD delivery needs Phase 1 evidence; `CODEBASE-MAP.md` frontend map omits PRD2 surfaces; `docs/README.md` still says tech-ref has 6 documents while `docs/tech-ref/README.md` lists 19. | `docs/PROJECT-HANDOFF.md`; `docs/CODEBASE-MAP.md`; `docs/README.md`; `docs/ops/CURRENT-STATUS.md`; `docs/tech-ref/README.md`. | PRD3 should include a documentation consistency pass before major new development. |
| PRD3-CA-05 | Medium | Requirements lifecycle | `docs/PRD-Phase2.md` still says `Draft for Review`, and `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` still says `Current phase: PRD2 implementation`, while current status/freeze docs say PRD2 is frozen. | `docs/PRD-Phase2.md`; `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`; `docs/ops/evidence/PRD2-final-freeze-20260705.md`. | PRD3 should formalize phase lifecycle states: draft, approved, implementation, frozen, superseded. |
| PRD3-CA-06 | Medium | Plugin boundary | Current code supports static in-process adapter registration and configured webhooks, but no plugin manifest, version compatibility, lifecycle, sandbox, permissions, signed package flow, install/uninstall path, or third-party plugin distribution. | `internal/adapters/registry.go`; `internal/adapters/types.go`; code search for plugin manifest/loading outside docs. | Runtime plugins should move to PRD4. PRD3 should only document the current static-adapter boundary and protect security/accounting invariants before future plugin work. |
| PRD3-CA-07 | Medium | Enterprise deployment | PRD2 deployment is Docker Compose/self-hosted plus canary smoke and backup/restore rehearsal. No Helm/Terraform/Kubernetes/IaC assets exist; HA docs explicitly do not cover production-grade HA/DR items such as backup encryption, off-server sync, PITR, remote restore, DB HA, or app DB failover reconnect behavior. | `docker-compose.yml`; `docker-compose.canary.yml`; `infra/`; `docs/tech-ref/15-ha-readiness.md`; no Helm/Terraform/K8s file matches. | New enterprise deployment/IaC assets should move to PRD4. PRD3 should only clean up current deployment docs and make gaps explicit. |
| PRD3-CA-08 | Medium | Engineering governance | `CONTRIBUTING.md`, `AGENT-PROTOCOL.md`, CI, and runbooks exist, but ADR policy, API versioning, migration/revert policy, release checklist, incident response, and deprecation policy are not formalized as a complete engineering handbook. | `CONTRIBUTING.md`; `docs/agent-workflows/AGENT-PROTOCOL.md`; `.github/workflows/ci.yml`; `OPEN-ISSUES.md` `PRD3-ENG-001`. | PRD3 should make release and compatibility rules explicit before plugin or enterprise packaging work increases blast radius. |
| PRD3-CA-09 | Medium | Billing and usage invariants | Batch 28 aligns UI labels and filters while preserving stable API values. Current backend still normalizes legacy `api -> upstream` and `stream_estimate -> counter_v2`; PRD3 changes must not rewrite settlement, reconciliation, or `usage_source` semantics accidentally. | `docs/ops/evidence/PRD2-B28-product-consistency-local-20260704.md`; `internal/metering/metering.go`; `internal/admin/prd2_product_handlers.go`; `frontend/src/utils/displayLabels.ts`. | Treat usage/billing compatibility as a PRD3 non-regression gate, especially for plugins, cache, and budget hard-blocking. |
| PRD3-CA-10 | Medium | Prompt/response and cache boundary | PRD2 explicitly keeps plaintext prompt/response storage disabled by default and runtime cache research-only. Gateway responses expose `cache_hit=false` metadata, but there is no runtime cache implementation in current source. | `docs/tech-ref/19-prompt-response-storage-policy.md`; `docs/tech-ref/17-cache-research.md`; `internal/gateway/relay.go`; code search outside docs. | PRD3 should not introduce runtime cache or prompt/response storage without explicit approval and audit-on-read controls. |
| PRD3-CA-11 | Low | Target-environment evidence | Several PRD2 items are E2E accepted locally or with mock/staging evidence but not Production verified. These are not hidden blockers, but they matter for commercial-readiness scope. | `CURRENT-STATUS.md`; `OPEN-ISSUES.md`; `PRD2-ACCEPTANCE-MATRIX.md`. | PRD3 should keep these evidence gaps visible without turning them into new feature scope. Production evidence promotion should be separate verification work or PRD4 scope. |
| PRD3-CA-12 | High | Usage Analytics semantics | `UsageAnalyticsPage` mixes filters and grouping in one panel. The table always shows the selected grouping, while overall totals live only in KPI cards. `provider` and `channel` are both free-text filters with no in-UI definition: backend groups `provider` from `call_logs.provider`, and `channel` from `call_logs.channel`. Users can reasonably confuse provider family/vendor with concrete upstream channel instance. | `frontend/src/pages/UsageAnalyticsPage.tsx`; `internal/admin/prd2_product_handlers.go`; `frontend/src/utils/displayLabels.ts`. | PRD3 should define and label provider vs upstream channel, decide whether an explicit "overall / all" row or grouping mode is needed, and make empty filters visibly mean "all". |
| PRD3-CA-13 | High | Internal Status semantics | Internal Status is implemented as an aggregation over notification events. `open_critical` and `open_warning` are open events by severity; `status` becomes `degraded`, `warning`, or `operational`. `maintenance_windows` is always an empty array and is labeled as a reserved placeholder. The product definition for these tiles is not visible enough in PRD/docs/UI. | `internal/admin/prd2_product_handlers.go`; `frontend/src/pages/InternalStatusPage.tsx`; `docs/tech-ref/14-notification-event-foundation.md`. | PRD3 should either document this as an event-derived status page and hide/label placeholders clearly, or remove placeholder-style semantics from the active panel until PRD4 implements maintenance windows. |
| PRD3-CA-14 | High | User role creation | Backend accepts `admin`, `platform_admin`, `org_admin`, `billing_admin`, `project_admin`, `developer`, `viewer`, and `org_viewer`; frontend User Management create/edit only exposes `admin`, `org_admin`, and `org_viewer`. PRD2 and Developer Console docs discuss `billing_admin`, `project_admin`, `developer`, and `viewer`, but the operator UI does not create them directly. | `internal/auth/auth.go`; `internal/admin/user_handlers.go`; `frontend/src/pages/UsersPage.tsx`; `docs/PRD-Phase2.md`; `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md`. | PRD3 should decide the canonical role taxonomy and creation workflow. This may be a UX/semantics fix, not a new feature, if it only exposes already-supported roles consistently. |
| PRD3-CA-15 | Medium | Non-admin landing | Current default route sends project-console roles to `/developer`; unknown/fallback roles go to `/docs`. The user's concern that "non-admin login goes directly to API docs" matches the fallback behavior and may also occur for legacy/unmapped roles. It is unclear whether docs should be a public/read-only landing or only a secondary developer reference. | `frontend/src/navigation.tsx`; `frontend/src/main.tsx`; `frontend/src/pages/LoginPage.tsx`; `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md`. | PRD3 should define default landing per role and unknown-role behavior. A safer current-product posture is probably a role-aware home/empty-state, not silent API-docs fallback. |
| PRD3-CA-16 | Medium | Topbar affordances | The topbar global search placeholder says it can search Request ID, model, and key, but the input has no wired behavior in `App.tsx`. The environment pill always says `生产环境`, and the bell always routes to `/notifications`, which project-console roles cannot access. | `frontend/src/App.tsx`; `frontend/src/navigation.tsx`. | PRD3 should remove misleading inert controls, wire them to existing routes, or restrict them per role. This is polish/consistency, not new product scope. |
| PRD3-CA-17 | Medium | Module structure | `internal/admin` is a broad god package with 35 Go files and mixed domains: auth, users, channels, billing, audit, model governance, license, OIDC, notifications, and PRD2 product handlers. Frontend has page-level components that carry substantial API, formatting, form, and presentation logic in one file (`DeveloperConsolePage.tsx`, `ModelGovernancePage.tsx`, `LogsPage.tsx`, `DashboardPage.tsx`). | `internal/admin/handlers.go`; `wc -l internal/admin/*.go frontend/src/pages/*.tsx`; `find internal -maxdepth 2`. | PRD3 should improve module boundaries and shared UI/domain helpers without changing behavior: split service/query helpers, extract role/label contracts, and reduce large page components where tests can guard parity. |
| PRD3-CA-18 | Medium | English/Chinese taxonomy drift | Several current surfaces still mix English and Chinese domain labels (`Billing`, `System`, `Governance`, `Approved Models`, `Gateway overview`, `PRD2`). Batch 28 improved many labels but did not make the panel taxonomy fully consistent. | `frontend/src/pages/*.tsx`; `frontend/src/navigation.tsx`; `docs/ops/evidence/PRD2-B28-product-consistency-local-20260704.md`. | PRD3 should define a single control-panel glossary and apply it across menu groups, page headers, filters, tables, docs, and CSV labels. |

## 4. Invariants To Preserve In PRD3

These are current product/architecture boundaries that should not be weakened by PRD3:

1. Local-deploy enterprise product remains the default product shape unless User explicitly approves public SaaS scope.
2. Downstream API stays OpenAI-compatible; upstream provider differences stay behind UniRouter.
3. Security/accounting core remains in core product code: auth, RBAC, API-key hashing, upstream-key encryption, wallet settlement, license verification, audit, and storage/cache policy.
4. `usage_source` canonical API values remain stable; display labels may localize, but filters and exports should stay contract-compatible.
5. Prompt/response plaintext is not stored by default.
6. Runtime cache remains disabled unless a future approved scope covers tenant isolation, billing, audit, correctness, retention, and purge.
7. PRD evidence claims keep the existing discipline: `E2E accepted` and `Production verified` are separate states.

## 5. Candidate PRD-Phase3 Scope

Recommended PRD3 should be a cleanup and optimization phase for existing shipped surfaces only.

### P3-P0: Consistency And Contract Foundation

- Fix role/navigation/backend authorization parity.
- Decide `org_admin`, `billing_admin`, `viewer`, and project-viewer semantics.
- Refresh OpenAPI/spec/docs so backend routes, frontend API client, `/docs`, and `docs/tech-ref/05-openapi-spec.yaml` agree.
- Reconcile first-read docs and phase state labels after PRD2 freeze.
- Add explicit compatibility rules for API versioning, deprecation, migration rollback, and `usage_source`.

### P3-P0: Control Panel Semantics

- Define a glossary for provider, upstream channel, model, alias, project, access credential, upstream credential, usage source, audit source, notification severity, internal status, and maintenance window.
- Review every second-level control-panel page for contradictory labels, placeholder behavior, missing "all" semantics, unclear filter behavior, and role-inappropriate actions.
- Fix non-admin/default landing behavior and unknown-role behavior.
- Remove or clearly mark inert UI affordances such as global search and placeholder maintenance windows if they are not implemented.
- Align User Management role creation with the actual supported role model, or document why some roles are created through project membership/SSO only.

### P3-P1: Code And Module Optimization

- Split or internally structure oversized backend admin domains without changing API behavior.
- Extract shared frontend form/filter/table/label conventions where duplication causes drift.
- Add tests around clarified role/navigation/page semantics before changing UI behavior.
- Strengthen route/API/OpenAPI parity tests so documentation cannot drift silently.
- Preserve billing, usage, audit, storage, and gateway behavior while refactoring.

### P3-P1: Documentation And Evidence Cleanup

- Update handoff, code map, docs index, PRD2 phase state labels, and active issue routing so they match the PRD2 freeze state.
- Convert unresolved PRD2 implementation ambiguities into explicit PRD3 cleanup items or PRD4 backlog items.
- Keep `E2E accepted` and `Production verified` boundaries intact.

## 5.1 PRD4 Candidate Backlog

These are intentionally outside PRD3 unless User reopens scope:

- Runtime plugin platform and plugin marketplace.
- Helm/Kubernetes/Terraform or other new enterprise deployment packaging.
- Production-grade HA/DR automation beyond documenting current gaps.
- Project budget hard-block enforcement.
- Runtime cache or prompt/response plaintext storage.
- Maintenance-window scheduling, SLA/SLO, upgrade communication workflow, and incident operations platform beyond current event semantics.
- New public SaaS, reseller, payment, invoice, campaign, or marketplace capabilities.
- Full SDK suite.
- New persistent roles such as audit viewer, support/operator, service account, or explicit project viewer if they require new permission surfaces.
- Real upstream/provider/IdP/license evidence-promotion work, unless treated as separate verification work rather than PRD3 feature scope.

## 6. Recommended Non-Goals Unless User Expands Scope

- Reseller / agent channel system.
- Public SaaS dual portal.
- Payment, invoice, real-name verification, coupon, voucher, campaign systems.
- Online license marketplace.
- Full support-ticket CRM.
- Full official SDK suite before OpenAPI contract is made current.
- Runtime semantic cache.
- Prompt/response plaintext storage.
- Arbitrary third-party untrusted code execution inside the gateway process.

## 7. User-Confirmed Recommendations For PRD3

User agreed on 2026-07-05 that PRD3 should proceed with these recommendations:

1. `org_admin` should not default to wallet adjustment, reconciliation, or grant operations; finance surfaces should align with `billing_admin` and `platform_admin`.
2. `viewer` semantics should be clarified by scope. `org_viewer` should remain the org-level read-only compatibility role; project read-only should be explicitly described as project-scoped viewer behavior.
3. Usage Analytics should become "overview plus multidimensional breakdown": KPI cards show current filtered overview, and the table should have an explicit overall/all grouping or row in addition to single-dimension drill-downs.
4. Product terminology should distinguish provider/vendor/supplier from upstream channel instance.
5. Internal Status should be defined as an event-derived status page using the existing notification lifecycle; maintenance windows, SLA/SLO, and upgrade communication are PRD4 unless implemented separately.
6. User Management should align with the existing backend-supported role model, while future roles such as audit viewer, support/operator, and service account remain PRD4 candidates unless implemented as pure labels over existing permissions.
7. Non-admin landing should be role-aware: developer/project users to project workbench, billing users to finance view, read-only users to overview/audit, and no-project users to a no-access/request-access state.
8. Misleading controls and placeholders should be removed, disabled, or clearly wired to existing behavior during PRD3.

## 8. Proposed Next Step

`docs/PRD-Phase3.md` has been created from this audit. Before implementation starts:

- create or update the PRD3 acceptance matrix;
- split PRD3 into small cleanup batches;
- update `OPEN-ISSUES.md`, `CURRENT-STATUS.md`, and `FIX-SIGNAL.md` when PRD3 implementation state actually changes;
- keep PRD4 backlog separate from PRD3 cleanup acceptance.
