# PRD3 Acceptance Matrix

> Purpose: track acceptance for `docs/PRD-Phase3.md`.
> Date: 2026-07-05
> Status: Planning baseline accepted; no PRD3 implementation batch has started.
> Scope rule: PRD3 is cleanup/optimization of already implemented functionality only. New capabilities are PRD4 backlog unless User explicitly reopens scope.
> Last verified: Canary PASS 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`; User reported Sentinel completed on 2026-07-05.

## Status Values

| Status | Meaning |
|--------|---------|
| Planned | PRD3 scope is defined but implementation has not started |
| In progress | Codex is actively changing docs/code for the row |
| Fixed awaiting Canary | Codex completed changes and requested verification |
| Canary passed | Canary verified the requested evidence |
| E2E accepted | The row has implementation plus relevant local/runtime evidence |
| Deferred to PRD4 | Explicitly outside PRD3 implementation scope |

## PRD3 Matrix

| ID | Workstream | Status | Acceptance Target | Evidence / Current Finding | Next Step |
|----|------------|--------|-------------------|----------------------------|-----------|
| P3-F01 | Product Glossary | Planned | Canonical CN/EN definitions exist for provider/vendor/supplier, upstream channel, model, alias, access credential, upstream credential, usage source, audit source, incident, event status, delivery state, internal status, and maintenance window | `docs/planning/PRD3-CONSISTENCY-AUDIT.md` records provider/channel and control-panel taxonomy drift | Create glossary source and apply labels to UI/docs/CSV without changing stable API enums |
| P3-F02 | Role And Navigation Parity | Fixed awaiting Canary | Frontend navigation, route guards, backend route authorization, role labels, default landing, and tests agree for all current roles | Batch 1 removes org-admin platform/billing-only nav, adds `/access` default for `org_viewer`/unknown roles, hides notification affordance for roles that cannot open it, and adds navigation/Login tests. Evidence: `docs/ops/evidence/PRD3-B01-role-navigation-local-20260705.md` | Canary verify source, targeted tests, full frontend tests/build, and `git diff --check` |
| P3-F03 | Control Panel Semantics | Planned | Every active second-level page has clear scope, filters, empty states, placeholders, and role-appropriate actions | Current audit finds inert topbar search, fixed environment label, notification bell role mismatch, and placeholder semantics | Review all pages and either wire, disable, hide, or relabel misleading controls |
| P3-F04 | API Contract Refresh | Planned | Backend routes, frontend API client, backend `/docs`, frontend `/docs`, and OpenAPI agree for existing PRD1/PRD2 routes | Current audit finds backend `/docs` says PRD1, frontend says PRD2, and OpenAPI misses multiple current PRD2 routes | Refresh OpenAPI/docs and add lightweight route/API parity guard |
| P3-F05 | Usage Analytics Semantics | Planned | Users can see current filtered overview and explicit all/overall breakdown; provider and upstream channel meanings are documented and visible | Current code uses KPI summary for total and single-dimension table grouping; provider/channel filters lack definition | Add overview/all semantics and terminology without changing accounting or `usage_source` API values |
| P3-F06 | Internal Status Semantics | Planned | Status page clearly reflects notification event lifecycle and no longer implies an implemented maintenance-window system | Current backend derives status from open critical/warning notification events; `maintenance_windows` is always empty | Align labels/docs/tests to `open/acknowledged/resolved`, severity, delivery state; hide/mark maintenance placeholder |
| P3-F07 | User Management Role Workflow | Fixed awaiting Canary | User create/edit workflow exposes supported roles or documents which roles are assigned via project membership/SSO | Batch 1 exposes canonical non-legacy backend-supported roles in User Management and documents that project roles still require project membership. Evidence: `docs/ops/evidence/PRD3-B01-role-navigation-local-20260705.md` | Canary verify role option list, UsersPage copy, and frontend tests/build |
| P3-F08 | Document Lifecycle Cleanup | Planned | First-read docs, README, DOC-INVENTORY, phase states, and active issue routing match PRD2 freeze and PRD3 planning state | `PRD3-CONSISTENCY-AUDIT.md` and `PRD-Phase3.md` exist; older docs still contain PRD1/PRD2 draft/implementation wording | Reconcile current truth docs and index PRD3 acceptance state |
| P3-F09 | Backend Module Boundary Cleanup | Planned | Backend admin/domain helpers are clearer without changing external routes or response contracts | `internal/admin` is broad and mixes auth, users, channels, billing, audit, model governance, license, OIDC, notifications, and PRD2 product handlers | Split only where tests can guard behavior; preserve security/accounting invariants |
| P3-F10 | Frontend Architecture Cleanup | Planned | Shared role, label, filter, table, form, empty/error/loading, and formatting patterns reduce duplication | Prior frontend audit warned page-level code and API/UI patterns were not centralized; PRD2 added more large pages | Extract shared helpers/components with frontend tests and no broad rewrite |
| P3-F11 | Test And Parity Guards | Planned | Focused route/nav/RBAC/OpenAPI/usage/status parity tests guard clarified semantics | Existing tests cover many flows, but PRD3-specific semantic parity is not complete | Add targeted tests per implementation batch |
| P3-F12 | Engineering Governance Minimum | Planned | ADR, API versioning, migration rollback, release checklist, deprecation, and incident-response basics are documented and indexed | `CONTRIBUTING.md` and agent protocol exist; full governance is incomplete | Add concise engineering governance docs as cleanup, not plugin/deploy expansion |
| P3-F13 | PRD4 Backlog Hygiene | Planned | Deferred/new capabilities are explicitly recorded as PRD4 candidates and do not leak into PRD3 implementation | Previous `OPEN-ISSUES.md` labels plugin/deploy as PRD3; User clarified they should move to PRD4 | Update issue routing and PRD3/PRD4 backlog boundaries |
| P3-F14 | Evidence Hygiene | Planned | E2E accepted and Production verified boundaries remain explicit; inherited PRD2 evidence is not overstated | PRD2 status tracks local/mock/staging vs production evidence gaps | Keep status docs precise when PRD3 implementation starts |

## PRD4 Backlog Boundary

These are not PRD3 acceptance rows:

| Candidate | PRD3 Handling |
|-----------|---------------|
| Runtime plugin platform and plugin marketplace | Record boundary only; implementation moves to PRD4 |
| Helm/Kubernetes/Terraform or managed HA deployment package | Record current Docker Compose/self-hosted boundary only; implementation moves to PRD4 |
| Project budget hard-block enforcement | Keep as later product capability; PRD3 may document why alert-only exists |
| Runtime cache or prompt/response plaintext storage | Keep disabled/default-off boundaries; implementation moves to PRD4 |
| Maintenance-window scheduling, SLA/SLO, upgrade communication workflow | PRD3 only aligns current event-derived status page; full operations model moves to PRD4 |
| Public SaaS, reseller, payment, invoice, coupon, campaign, marketplace, support-ticket CRM | Out of PRD3 |
| Full SDK suite | Blocked until OpenAPI contract is current; likely PRD4/later |
| New persistent roles requiring new authority surfaces | PRD4 unless expressible as existing permissions with labels only |
| Real upstream/provider/IdP/license production evidence promotion | Separate verification work or PRD4, not PRD3 feature scope |

## Verification Entry

Initial planning closeout verification should check:

1. `docs/PRD-Phase3.md` exists and states PRD3 is cleanup/optimization only.
2. This matrix exists and contains no new-feature implementation rows.
3. `docs/planning/PRD3-CONSISTENCY-AUDIT.md` includes PRD1 -> PRD2 lineage findings and User-confirmed recommendations.
4. `docs/issues/OPEN-ISSUES.md`, `docs/ops/CURRENT-STATUS.md`, `docs/DOC-INVENTORY.md`, and `docs/README.md` route PRD3/PRD4 consistently.
5. `git diff --check` passes.
