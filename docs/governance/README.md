# UniRouter Engineering Governance

> Status: PRD3 Batch 8 minimum governance baseline.
> Scope: existing code, docs, release, and compatibility workflow. This is not a
> new product capability.

## Purpose

This document defines the minimum engineering rules that must be checked before
PRD4 expands UniRouter with larger plugin, deployment, role, or operations
surfaces.

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

Current proposed PRD4 ADRs:

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

Before a release or Sentinel merge that changes code or acceptance-sensitive
docs:

- `git status --short` was checked before editing.
- Scope stays inside the approved PRD/batch.
- Relevant tests pass locally or are explicitly delegated with reason.
- `git diff --check` passes.
- `docs/agent-workflows/FIX-SIGNAL.md` describes verification steps.
- `docs/agent-workflows/VERIFY-RESULT.md` is updated by Canary after verification.
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

PRD3 Internal Status remains notification-event-derived. A full maintenance
window, SLA/SLO, and incident operations product model stays in PRD4 unless the
User explicitly approves earlier scope.

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
