# PRD3 Acceptance Matrix

> Purpose: track acceptance for `docs/PRD-Phase3.md`.
> Date: 2026-07-05
> Status: Planning baseline accepted; PRD3 Batch 1 accepted; PRD3 Batch 2 accepted; PRD3 Batch 3 accepted; Batch 3 follow-up accepted; PRD3 Batch 4 Dashboard taxonomy fixed awaiting Canary.
> Scope rule: PRD3 is cleanup/optimization of already implemented functionality only. New capabilities are PRD4 backlog unless User explicitly reopens scope.
> Last verified: PRD3 Batch 3 follow-up Canary PASS 5/5 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #66 at `8a0f670`; User reported Canary/Sentinel/Atlas completed on 2026-07-05.

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
| P3-F01 | Product Glossary | Canary passed | Canonical CN/EN definitions exist for provider/vendor/supplier, upstream channel, model, alias, access credential, upstream credential, usage source, audit source, incident, event status, delivery state, internal status, and maintenance window | Batch 2 adds `docs/tech-ref/20-prd3-product-glossary.md` and indexes it in docs. Canary PASS 5/5; Sentinel merged PR #64 at `a1baf8f`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B02-usage-status-semantics-local-20260705.md` | Keep glossary aligned with future label/API changes |
| P3-F02 | Role And Navigation Parity | Canary passed | Frontend navigation, route guards, backend route authorization, role labels, default landing, and tests agree for all current roles | Batch 1 removes org-admin platform/billing-only nav, adds `/access` default for `org_viewer`/unknown roles, hides notification affordance for roles that cannot open it, and adds navigation/Login tests. Canary PASS 6/6; Sentinel merged PR #63 at `9bed3be`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B01-role-navigation-local-20260705.md` | Keep role/nav parity tests in future route changes |
| P3-F03 | Control Panel Semantics | Fixed awaiting Canary | Every active second-level page has clear scope, filters, empty states, placeholders, and role-appropriate actions | Batch 1 fixed notification affordance mismatch. Batch 2 clarifies Usage Analytics and Internal Status semantics. Batch 3 wires topbar search to existing Logs search, removes the hard-coded production environment label, and records a page-level control semantics scan. Batch 3 and its empty-search follow-up passed Canary/Sentinel/Atlas. Batch 4 aligns Dashboard header taxonomy from `Gateway overview` / `AI Gateway 控制台` to `Platform Overview` / `平台总览`. Evidence: `docs/ops/evidence/PRD3-B04-dashboard-taxonomy-local-20260705.md` | Canary verify Dashboard taxonomy source/test/evidence; broader all-page taxonomy cleanup remains separate PRD3 work |
| P3-F04 | API Contract Refresh | Planned | Backend routes, frontend API client, backend `/docs`, frontend `/docs`, and OpenAPI agree for existing PRD1/PRD2 routes | Current audit finds backend `/docs` says PRD1, frontend says PRD2, and OpenAPI misses multiple current PRD2 routes | Refresh OpenAPI/docs and add lightweight route/API parity guard |
| P3-F05 | Usage Analytics Semantics | Canary passed | Users can see current filtered overview and explicit all/overall breakdown; provider and upstream channel meanings are documented and visible | Batch 2 adds `group_by=overall`, defaults frontend to `总览（不分组）`, and clarifies provider/channel/filter semantics without changing accounting or stable `usage_source` values. Canary PASS 5/5; Sentinel merged PR #64 at `a1baf8f`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B02-usage-status-semantics-local-20260705.md` | Keep accounting and `usage_source` compatibility as non-regression gates |
| P3-F06 | Internal Status Semantics | Canary passed | Status page clearly reflects notification event lifecycle and no longer implies an implemented maintenance-window system | Batch 2 labels open critical/warning event counters, separates event status from webhook delivery state, and marks maintenance window scheduling as not enabled/PRD4. Canary PASS 5/5; Sentinel merged PR #64 at `a1baf8f`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B02-usage-status-semantics-local-20260705.md` | Keep maintenance scheduling/SLA/SLO in PRD4 unless scope is reopened |
| P3-F07 | User Management Role Workflow | Canary passed | User create/edit workflow exposes supported roles or documents which roles are assigned via project membership/SSO | Batch 1 exposes canonical non-legacy backend-supported roles in User Management and documents that project roles still require project membership. Canary PASS 6/6; Sentinel merged PR #63 at `9bed3be`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B01-role-navigation-local-20260705.md` | Keep role option list aligned with backend-supported role groups |
| P3-F08 | Document Lifecycle Cleanup | Planned | First-read docs, README, DOC-INVENTORY, phase states, and active issue routing match PRD2 freeze and PRD3 planning state | `PRD3-CONSISTENCY-AUDIT.md` and `PRD-Phase3.md` exist; older docs still contain PRD1/PRD2 draft/implementation wording | Reconcile current truth docs and index PRD3 acceptance state |
| P3-F09 | Backend Module Boundary Cleanup | Planned | Backend admin/domain helpers are clearer without changing external routes or response contracts | `internal/admin` is broad and mixes auth, users, channels, billing, audit, model governance, license, OIDC, notifications, and PRD2 product handlers | Split only where tests can guard behavior; preserve security/accounting invariants |
| P3-F10 | Frontend Architecture Cleanup | Planned | Shared role, label, filter, table, form, empty/error/loading, and formatting patterns reduce duplication | Prior frontend audit warned page-level code and API/UI patterns were not centralized; PRD2 added more large pages | Extract shared helpers/components with frontend tests and no broad rewrite |
| P3-F11 | Test And Parity Guards | Planned | Focused route/nav/RBAC/OpenAPI/usage/status parity tests guard clarified semantics | Existing tests cover many flows, but PRD3-specific semantic parity is not complete. Batch 3 follow-up adds explicit empty topbar search routing coverage after Canary's non-blocking suggestion. Batch 4 adds Dashboard header taxonomy regression coverage | Continue adding targeted tests per implementation batch; full API/OpenAPI parity remains P3-F04/P3-F11 work |
| P3-F12 | Engineering Governance Minimum | Planned | ADR, API versioning, migration rollback, release checklist, deprecation, and incident-response basics are documented and indexed | `CONTRIBUTING.md` and agent protocol exist; full governance is incomplete | Add concise engineering governance docs as cleanup, not plugin/deploy expansion |
| P3-F13 | PRD4 Backlog Hygiene | Planned | Deferred/new capabilities are explicitly recorded as PRD4 candidates and do not leak into PRD3 implementation | Previous `OPEN-ISSUES.md` labels plugin/deploy as PRD3; User clarified they should move to PRD4 | Update issue routing and PRD3/PRD4 backlog boundaries |
| P3-F14 | Evidence Hygiene | Planned | E2E accepted and Production verified boundaries remain explicit; inherited PRD2 evidence is not overstated | PRD2 status tracks local/mock/staging vs production evidence gaps. The 2026-07-05 comprehensive Canary test report has been promoted from `docs/testing/archive/` to `docs/ops/evidence/`, with reproduced risks tracked in `OPEN-ISSUES.md` rather than treated as accepted status | Keep status docs precise; do not leave active findings only in archive |

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
