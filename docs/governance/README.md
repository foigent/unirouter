# UniRouter Engineering Governance

> Status: Current minimum engineering governance baseline; originally introduced in PRD3 Batch 8.
> Scope: existing code, docs, release, and compatibility workflow. This is not a
> new product capability.

## Purpose

This document defines the minimum engineering rules for any change to UniRouter.
Completed PRD scope remains historical; current execution authority comes from
the active issue/batch record and explicit User decisions.

## Active Milestone And Proportionate Verification

Keep exactly one Active Milestone: the smallest independently useful outcome
that advances the complete authorized goal. Newly discovered work is classified
as required for that milestone, blocking it, or a separate backlog item; only
the first two enter the current diff.

For ordinary product changes, the default economical path is:

1. inspect the current authority and freeze the acceptance matrix;
2. complete the coherent diff and focused regression tests;
3. freeze the diff, run one risk-proportionate repository gate, then make at
   most one normal push/CI cycle when that external action is authorized;
4. rerun only a failed job when the exact commit is unchanged and the failure
   is demonstrably external.

Migration, security, identity/authorization, financial, and irreversible
changes elevate verification: add the relevant negative states, rollback or
recovery proof, independent review, and target evidence. UI wording, normal
API composition, and other reversible product behavior do not receive that
ceremony by default. Provider-generic protocol evidence is authoritative for
UniRouter behavior; named Agent frameworks receive bounded compatibility smoke
tests and are not billing or usage authorities.

Browser UI is reserved for visual/interaction evidence. Deterministic drafts
and repeated configuration are produced through code or supported APIs; an
existing signed-in browser is used only where human authentication or approval
is actually required.

## ADR Policy

Use an Architecture Decision Record when a change:

- changes a public API, storage format, authorization model, deployment model,
  billing/accounting invariant, or cross-module ownership boundary;
- introduces a new long-lived subsystem or external integration contract;
- deliberately accepts a tradeoff that future contributors must understand.

ADRs live under `docs/governance/adr/` and should use
`docs/governance/ADR-TEMPLATE.md`. Draft ADRs may be committed as `Proposed`;
accepted ADRs must reference the PR, evidence file, or issue that validates the
decision.

Existing ADR register (preserve each file's own Accepted/Proposed status):

- `docs/governance/adr/ADR-0001-prd4-plungein-ports.md`
- `docs/governance/adr/ADR-0002-prd4-api-key-owner-usage.md`
- `docs/governance/adr/ADR-0003-prd4-status-center-observability.md`

## API Compatibility And Versioning

`docs/tech-ref/05-openapi-spec.yaml` is the route-level API contract. Backend
routes, frontend `/docs`, and the OpenAPI file are guarded by
`cmd/uniroute/route_parity_test.go`.

API changes must follow these rules:

- Additive fields, optional query parameters, and new response fields are
  generally compatible.
- Removing routes, changing required request fields, renaming enum/API values,
  changing authorization for existing callers, or changing response shape
  semantics is breaking and requires explicit approval plus migration guidance.
- UI label localization must not rename stable API enum values such as
  `usage_source`, role names, event statuses, or delivery states.
- New customer-facing route families should either fit `/api/v1` compatibility
  rules or be deliberately introduced under a new version prefix.
- OpenAPI, backend `/docs`, frontend `/docs`, API client tests, and route parity
  tests must be updated in the same batch as route changes.

The repository product version is the exact SemVer in root `VERSION` and must
match `frontend/package.json` and `frontend/package-lock.json`. Release-bearing
changes increment `MAJOR.MINOR.PATCH` and add a dated entry to `CHANGELOG.md`:

- `MAJOR`: deliberately incompatible customer-facing contract;
- `MINOR`: backward-compatible capability or material product behavior;
- `PATCH`: backward-compatible correction.

Runtime images also retain an immutable runtime version and exact commit. That
deployment identity is evidence for a build and must not be substituted for,
or allowed to silently regress, the product SemVer.

## Migration And Rollback Policy

Schema changes must include:

- paired `.up.sql` and `.down.sql` files when rollback is technically safe;
- data backfill notes when existing rows are transformed;
- idempotency or conflict handling when a migration may run in a partially
  applied environment;
- tests or smoke evidence for security, billing, RBAC, routing, and audit-impact
  migrations.

Operational rules:

- Only one stable instance should run migrations against a shared production DB.
- Canary instances sharing the same DB must use `SKIP_MIGRATION=true`.
- Rollback must start from backup/restore and explicit `.down.sql` review; do
  not assume automatic down migration is safe for data-changing migrations.
- For data-normalization migrations, the down migration must either reverse the
  value mapping or explicitly document why reversal is impossible.

## Release Checklist

Before a release or PR merge that changes code or acceptance-sensitive docs:

- `git status --short` was checked before editing.
- Scope stays inside the approved PRD/batch.
- Relevant tests pass locally or are explicitly delegated with reason.
- `git diff --check` passes.
- `VERSION`, frontend package metadata, and `CHANGELOG.md` pass
  `scripts/check-release-metadata.sh` for a release-bearing change.
- `docs/agent-workflows/EXECUTION-RECORD.md` is updated only when the change is
  long-running, crosses an external lifecycle, or needs resumable risk/evidence
  state; ordinary local changes do not create evidence ceremony by default.
- `Change Routing` and every applicable required CI job pass; docs-only changes
  may skip expensive jobs only when routing classifies them explicitly.
- `docs/ops/CURRENT-STATUS.md`, acceptance matrix, and `docs/issues/OPEN-ISSUES.md`
  are updated when acceptance state changes.
- Deployment-affecting changes include rollback notes and target-environment
  boundaries.

## Incident Response Minimum

For engineering incidents, record:

- incident time window and affected environment;
- user-visible impact and affected roles/projects/orgs where known;
- trigger or suspected trigger;
- mitigation, rollback, or disablement steps;
- evidence command/output summary;
- follow-up issue and owner.

Notification lifecycle and runtime observability remain separate operational
concepts. A broader maintenance-window, SLA/SLO, or incident-operations product
model requires its own accepted scope; it must not be inferred from historical
PRD labels.

## Deprecation Policy

Deprecation is required before removing or breaking a current integration path.

Minimum deprecation record:

- what is deprecated and what replaces it;
- affected API routes, UI surfaces, env vars, config keys, role names, or enum
  values;
- compatibility behavior during the deprecation window;
- audit/log signal if applicable;
- removal condition and rollback plan.

Model lifecycle deprecation (`staging`, `ga`, `deprecated`, `retired`) remains a
model-governance concept. It does not automatically deprecate UniRouter API
routes or product capabilities.

## Verification

Governance-only batches should verify:

```bash
rg -n "ADR|API Compatibility|Migration And Rollback|Release Checklist|Incident Response|Deprecation" docs/governance docs/README.md docs/DOC-INVENTORY.md docs/DOCS-GOVERNANCE.md
git diff --check
```
