# UniRouter Product Requirements Document - Phase 3

> Version: Phase 3 Draft
> Date: 2026-07-05
> Status: Complete for confirmed PRD3 scope; Batch 17 final evidence hygiene passed Canary.
> Source: `docs/planning/PRD3-CONSISTENCY-AUDIT.md`, `docs/PRD-Phase1.md`, `docs/PRD-Phase2.md`, PRD1 UI findings, PRD2 freeze evidence, and current source-code review.
> Purpose: define PRD3 as a consistency, semantics, contract, and architecture cleanup phase for already implemented UniRouter functionality.

---

## 1. Product Positioning

PRD3 is not a new-feature expansion phase.

PRD3 turns the PRD1 -> PRD2 implementation into a cleaner enterprise product baseline by reconciling product semantics, role boundaries, navigation, documentation, API contracts, and module structure around already implemented capabilities.

Core product sentence:

> UniRouter PRD3 makes the existing PRD1 and PRD2 product coherent enough for PRD4 expansion by clarifying roles, control-panel semantics, API contracts, status models, terminology, and code/module boundaries without adding new product capability.

## 2. Scope Principle

PRD3 may:

- clarify and align behavior already present in code;
- rename, relabel, hide, disable, or route existing UI surfaces so they match actual backend permissions and product meaning;
- refactor code and modules without changing external behavior;
- update tests, OpenAPI, docs, indexes, and acceptance evidence rules;
- convert unconfirmed or placeholder behavior into explicit PRD4 backlog.

PRD3 must not:

- implement runtime plugin loading, marketplace, sandboxing, or third-party install flows;
- add Helm, Terraform, Kubernetes, managed HA, or new deployment products;
- implement project budget hard-block enforcement;
- implement runtime cache or prompt/response plaintext storage;
- implement a maintenance-window scheduling system, SLA/SLO system, or incident operations platform beyond aligning current event semantics;
- introduce public SaaS, reseller, payment, invoice, campaign, marketplace, or support-ticket CRM features;
- add a full SDK suite before the OpenAPI contract is current.

## 3. Role Model Baseline

PRD3 uses existing roles as the implementation baseline and clarifies their product meaning.

| Role | Scope | PRD3 Meaning | PRD3 Direction |
|------|-------|--------------|----------------|
| `platform_admin` / legacy `admin` | deployment | Full deployment administration | Keep full access; document `admin` as compatibility alias only |
| `org_admin` | organization | Organization governance, users, projects, model access | Remove default finance mutation access unless backend and PRD explicitly allow it |
| `billing_admin` | organization | Wallet, budgets, usage, reconciliation, finance audit | Align frontend navigation and backend route access |
| `project_admin` | project | Project members, project keys, model access, project usage | Keep project-scoped; require project membership checks |
| `developer` | project | Project member who can use approved models and manage permitted project keys | Land in project/developer workbench |
| `viewer` | project by implementation | Project-scoped read-only member | Document as project-scoped viewer unless later split |
| `org_viewer` | organization compatibility | Organization-level read-only compatibility role | Keep as org-level read-only; do not silently map to project-only navigation |

Terminology:

- Avoid "normal user" as a durable B2B role name.
- Use "project member" or "developer" for ordinary enterprise users who call models.
- Future role candidates such as `audit_viewer`, `support_operator`, `ops_operator`, and `service_account` belong to PRD4 unless PRD3 can express them with existing permissions and no new authority surface.

## 4. PRD3 Scope Summary

### P0

| ID | Workstream | Summary |
|----|------------|---------|
| P3-F01 | Product Glossary | Define canonical terms for provider, upstream channel, model, alias, access credential, upstream credential, usage source, audit source, incident, and maintenance window |
| P3-F02 | Role And Navigation Parity | Align role definitions, frontend navigation, route guards, backend authorization, tests, and default landing behavior |
| P3-F03 | Control Panel Semantics | Review second-level panels for contradictory labels, missing all/overall semantics, placeholders, and misleading controls |
| P3-F04 | API Contract Refresh | Make backend routes, frontend API client, `/docs`, and OpenAPI agree for existing routes |
| P3-F05 | Usage Analytics Semantics | Clarify provider vs upstream channel, filtered overview vs grouped breakdown, and "all" semantics |
| P3-F06 | Internal Status Semantics | Align status page to notification event lifecycle and remove or label maintenance placeholders |
| P3-F07 | User Management Role Workflow | Align user create/edit UI with supported roles or document why some roles are assigned elsewhere |
| P3-F08 | Document Lifecycle Cleanup | Reconcile PRD1/PRD2/PRD3 phase states, current truth docs, handoff, inventory, and indexes |

### P1

| ID | Workstream | Summary |
|----|------------|---------|
| P3-F09 | Backend Module Boundary Cleanup | Reduce admin god-package drift through internal service/query/helper boundaries without API changes |
| P3-F10 | Frontend Architecture Cleanup | Extract shared page, filter, table, form, role, label, empty/error, and formatting patterns |
| P3-F11 | Test And Parity Guards | Add route/nav/RBAC/OpenAPI parity guards and focused regression tests |
| P3-F12 | Engineering Governance Minimum | Define ADR, API versioning, migration rollback, release checklist, deprecation, and incident-response basics |

### P2

| ID | Workstream | Summary |
|----|------------|---------|
| P3-F13 | PRD4 Backlog Hygiene | Convert deferred or guessed behavior into explicit PRD4 candidates with source evidence |
| P3-F14 | Evidence Hygiene | Keep E2E accepted vs Production verified boundaries explicit and avoid overstating target-environment readiness |

## 5. Functional Requirements

### P3-F01: Product Glossary

Requirements:

- Define the canonical Chinese and English label for each product concept used in UI, API docs, CSV, and PRD docs.
- Provider/vendor/supplier must mean provider family, adapter family, or upstream vendor dimension, such as OpenAI, Anthropic, DeepSeek, or Mimo.
- Upstream channel must mean one concrete configured route instance with endpoint, credentials, weight/priority, cooldown state, adapter type, and configured model list.
- Access credential must mean downstream UniRouter API key.
- Upstream credential must mean provider key or provider credential used by UniRouter.
- Usage source must preserve stable API values and only localize display labels.
- Incident/status terminology must map to existing notification event fields.

Acceptance:

- One glossary source is referenced by PRD3 and implementation tasks.
- Navigation labels, page headers, filter labels, table labels, CSV labels, and docs use the same terms.
- No API enum is renamed in a backward-incompatible way for display-only cleanup.

### P3-F02: Role And Navigation Parity

Requirements:

- Align role semantics across `internal/auth`, backend route groups, frontend navigation, route guards, tests, and PRD docs.
- `org_admin` must not see or use wallet adjustment, reconciliation, or grant operations unless backend authorization intentionally allows it.
- `billing_admin` should own finance governance surfaces.
- Project-scoped roles should only see project/developer surfaces they can actually use.
- `org_viewer` should not silently fall into an API Docs landing because frontend normalization missed a legacy role.
- Unknown or invalid roles should land in a restricted/no-access state, not API Docs.

Default landing recommendation:

| Actor | Landing |
|-------|---------|
| `platform_admin` | Admin dashboard |
| `org_admin` | Org/admin dashboard |
| `billing_admin` | Finance overview or dashboard filtered to finance signals |
| `project_admin` | Project workbench |
| `developer` | Developer/project workbench |
| `viewer` | Project read-only overview |
| `org_viewer` | Organization read-only overview or audit/usage read-only page |
| no project membership | No-access or request-access state |
| unknown role | Restricted unsupported-role state |

Acceptance:

- Frontend navigation never exposes pages that backend will categorically reject for that role.
- Direct URL access is still backend-enforced.
- Navigation and route guard tests cover every supported role.
- User-facing role labels match PRD3 role definitions.

### P3-F03: Control Panel Semantics

Requirements:

- Review every second-level control-panel page for labels, filters, empty states, placeholders, permissions, and default landing behavior.
- Remove, disable, or clearly mark inert controls, especially global search, environment label, notification bell access, and placeholder panels.
- Avoid UI text that implies unimplemented product capability.
- Ensure each page says whether data is global, org-scoped, project-scoped, filtered, or current-role scoped.

Acceptance:

- Each active menu item maps to a usable current capability.
- Placeholder-only surfaces are removed from normal navigation or explicitly marked as planned future scope.
- Role-inappropriate actions are not shown.
- Empty filters visibly mean all/current scope where applicable.

### P3-F04: API Contract Refresh

Requirements:

- Refresh `docs/tech-ref/05-openapi-spec.yaml` for existing backend routes.
- Align backend `/docs`, frontend `/docs`, and API Docs page wording so they do not mix PRD1 and PRD2 route states.
- Ensure frontend API client methods correspond to real backend routes.
- Add a lightweight parity guard so future route drift is visible.

Acceptance:

- OpenAPI includes current existing PRD1/PRD2 admin, developer, usage analytics, internal status, notification, model governance, OIDC, license, and error-docs routes where applicable.
- Backend and frontend docs no longer conflict on PRD phase or endpoint availability.
- Parity tests or scripts fail when a critical frontend client route is missing on the backend.

### P3-F05: Usage Analytics Semantics

Requirements:

- Preserve existing API values and billing/accounting behavior.
- Make KPI cards explicitly represent current filtered overview.
- Add explicit "overall/all" semantics for the breakdown table, either as a grouping mode or total row.
- Distinguish provider/vendor from upstream channel instance in filters, table columns, docs, and CSV.
- Make empty filters visibly mean current scope/all values.

Acceptance:

- Users can find total usage without guessing that empty filters imply total scope.
- Provider and upstream channel definitions are visible in docs and reflected in UI labels.
- Tests prove API filtering/grouping behavior remains backward compatible.
- No settlement, reconciliation, or `usage_source` logic changes as a side effect.

### P3-F06: Internal Status Semantics

Requirements:

- Define Internal Status as an event-derived status page over existing `notification_events`.
- Use the existing event lifecycle: `open`, `acknowledged`, `resolved`.
- Define severity: `info`, `warning`, `critical`.
- Define delivery state separately from incident status: `not_configured`, `pending`, `delivered`, `failed`.
- Current status calculation may remain derived from open critical and warning events.
- Maintenance windows must be hidden, disabled, or labeled as planned PRD4 scope unless implemented.

Acceptance:

- Internal Status page copy matches the actual backend aggregation.
- Critical and warning event counters are clearly "open critical" and "open warning" event counts.
- Maintenance-window UI does not imply a working maintenance scheduling system.
- Incident/status docs link to or align with notification event foundation.

### P3-F07: User Management Role Workflow

Requirements:

- Decide which existing roles can be created or edited in User Management.
- If project roles are assigned through project membership rather than direct user role edits, document and expose that workflow clearly.
- If backend accepts a role, the UI must either expose it intentionally or explain why it is managed elsewhere.
- OIDC role/group mapping docs should use the same canonical role names.

Acceptance:

- User Management no longer exposes only a partial role list without explanation.
- Role create/edit tests cover all supported choices or the documented excluded workflow.
- OIDC provisioning and local user creation do not create contradictory role semantics.

### P3-F08: Document Lifecycle Cleanup

Requirements:

- Reconcile phase state labels in PRD1, PRD2, PRD3, acceptance matrices, handoff docs, docs inventory, and README.
- Clearly distinguish requirements, current truth, evidence, historical archive, and draft planning documents.
- Move unconfirmed behavior into PRD3 cleanup tasks or PRD4 backlog items.
- Keep E2E accepted and Production verified evidence states separate.

Acceptance:

- First-read docs point to the correct current truth.
- PRD2 frozen state is not contradicted by stale draft/implementation labels.
- PRD3 scope is discoverable from README and DOC-INVENTORY.

### P3-F09: Backend Module Boundary Cleanup

Requirements:

- Refactor only where tests can preserve behavior.
- Reduce drift in `internal/admin` by extracting domain-specific helpers or query/service boundaries.
- Keep external HTTP routes and response contracts compatible unless PRD3 explicitly changes labels or authorization.
- Preserve security/accounting invariants in auth, RBAC, API-key hashing, upstream-key encryption, wallet settlement, license, audit, and cache/storage policy.

Acceptance:

- `go test ./...` passes after refactors.
- Domain-specific handlers have clearer boundaries.
- No behavior change occurs without a PRD3 requirement and test update.

### P3-F10: Frontend Architecture Cleanup

Requirements:

- Extract shared patterns for resource pages, filters, tables, drawers/forms, empty/error/loading states, formatting, labels, and role checks.
- Split large page modules where extraction reduces drift.
- Keep Semi UI and existing design layer; do not rewrite the frontend.
- Add tests around extracted role/navigation/label behavior.

Acceptance:

- Frontend tests and build pass.
- Reused role and label helpers reduce duplicated product vocabulary.
- Existing PRD2 pages remain reachable and behaviorally equivalent except for intentional PRD3 consistency fixes.

### P3-F11: Test And Parity Guards

Requirements:

- Add focused tests for role navigation, default landing, route guard, and backend authorization parity.
- Add usage analytics tests for provider/channel/all semantics.
- Add Internal Status tests for event-derived counters and lifecycle labels.
- Add OpenAPI or route parity checks for existing route documentation.

Acceptance:

- PRD3 implementation batches must include relevant tests before claiming acceptance.
- Route/API/doc drift is caught by at least one automated or scripted check.

### P3-F12: Engineering Governance Minimum

Requirements:

- Define lightweight ADR policy.
- Define API versioning and deprecation policy.
- Define migration and rollback expectations.
- Define release checklist and incident-response minimum.
- Keep guidance short enough to be used by future implementation batches.

Acceptance:

- Engineering governance docs are indexed and referenced by PRD3 implementation tasks.
- Release and compatibility claims have a documented gate.

## 6. PRD4 Candidate Backlog

These items are intentionally outside PRD3 unless User explicitly reopens scope:

- Runtime plugin platform: manifest, version compatibility, lifecycle, sandbox, permissions, signing, install/uninstall, and marketplace.
- Helm, Kubernetes, Terraform, managed HA, and new enterprise deployment packaging.
- Production-grade HA/DR automation beyond documenting current gaps.
- Project budget hard-block enforcement.
- Runtime cache or prompt/response plaintext storage.
- Maintenance-window scheduling, SLA/SLO, upgrade communication workflow, and incident operations platform beyond current event semantics.
- Infrastructure observability and runtime environment visibility such as CPU, memory, disk, network, instance inventory, cluster topology, and capacity health.
- Public SaaS, reseller, payment, invoice, coupon, campaign, marketplace, or support-ticket CRM.
- Full SDK suite.
- New persistent roles such as audit viewer, support/operator, service account, or explicit project viewer if they require new permission surfaces.
- Production evidence promotion for real upstream provider, real enterprise IdP, and production license unless handled as separate verification work.

The detailed candidate backlog is tracked in `docs/planning/PRD4-BACKLOG.md`.

## 7. Compatibility And Evidence Gates

PRD3 changes must preserve:

- OpenAI-compatible downstream API behavior.
- Existing gateway auth and API-key compatibility.
- Billing freeze, settlement, reconciliation, and wallet safety.
- `usage_source` canonical values and current accounting semantics.
- Prompt/response plaintext disabled by default.
- Runtime cache disabled by default.
- Security/accounting core inside product code.
- E2E accepted vs Production verified distinction.

Minimum verification per implementation batch:

- Relevant backend tests.
- Relevant frontend tests and build for UI changes.
- Role/navigation negative tests for RBAC-sensitive changes.
- `git diff --check`.
- Documentation/index updates when scope or current truth changes.

## 8. Acceptance Matrix Entry Point

Before PRD3 implementation starts, create or update an acceptance matrix with rows for:

- role/navigation/backend authorization parity;
- role-aware default landing;
- glossary and product label consistency;
- Usage Analytics overview/all/provider/channel semantics;
- Internal Status event lifecycle and maintenance placeholder behavior;
- User Management role workflow;
- OpenAPI/docs/backend/frontend route parity;
- document lifecycle cleanup;
- backend module refactor parity;
- frontend architecture extraction parity;
- engineering governance minimum.

## 9. Implementation Notes

- PRD3 should be implemented in small batches with narrow tests.
- Refactors should follow the existing repo style rather than introducing a new framework.
- Any proposed new product behavior discovered during PRD3 must be written down as PRD4 backlog unless User explicitly approves it for PRD3.
- Current PRD2 closeout evidence should not be rewritten as PRD3 evidence unless rerun or clearly labeled as inherited historical evidence.
