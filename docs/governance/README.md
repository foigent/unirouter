# UniRouter Engineering Governance

> Status: Current minimum engineering governance baseline; originally introduced in PRD3 Batch 8.
> Scope: existing code, docs, release, and compatibility workflow. This is not a
> new product capability.

## Purpose

This document defines engineering rules applied according to the actual task, change risk and accepted scope; it is not a checklist for every question or edit.
Completed PRD scope remains historical; current execution authority comes from
the active issue/batch record and explicit User decisions.

## Active Milestone And Proportionate Verification

Keep exactly one Active Milestone: the smallest independently useful outcome
that advances the complete authorized goal. Newly discovered work is classified
as required for that milestone, blocking it, or a separate backlog item; only
the first two enter the current diff.

For ordinary product changes, the default economical path is:

1. inspect the applicable authority and establish the current acceptance criteria; use an existing matrix when the task needs one;
2. complete the coherent diff and focused regression tests;
3. stabilize the diff, complete the applicable risk-proportionate checks, and
   push coherent batches only when authorized; new changes, failures or unresolved
   risks justify additional verification, not an arbitrary one-attempt limit;
4. reuse still-valid successful evidence and rerun only failed jobs when all
   relevant inputs are unchanged and the failure is demonstrably external.

Migration, security, identity/authorization, financial, and irreversible
changes elevate verification: add the relevant negative states, rollback or
recovery proof, independent review, and target evidence. UI wording, normal
API composition, and other reversible product behavior do not receive that
ceremony by default. Root `AGENTS.md` defines mandatory pre-code independent
review triggers, including exact Profile identity and Release binding changes;
data-only changes reuse accepted architecture but are not automatically exempt.
Provider-generic protocol evidence is authoritative for
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
- `docs/governance/adr/ADR-0004-explicit-nonexpiring-upgrade-packages.md` (Proposed; local candidate)

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

`scripts/check-release-metadata.sh` implements conservative path classification,
not semantic approval of a change. Known Go `*_test.go`, verified frontend
`frontend/src/*.test.ts` / `*.test.tsx` paths (including subdirectories), architecture
Markdown and `docs/CODEBASE-MAP.md` do not mechanically require a product version
bump. The exact known internal verification-tool exceptions are:

- `scripts/check-release-metadata.sh` and `scripts/test-release-metadata.py`;
- `scripts/test-backend-integration.sh`, `scripts/test-backend-integration-runner.py`
  and `scripts/summarize-go-test.py`, which orchestrate and record test execution.

These exceptions do not include customer installation, signed-package verification
or recovery tools. Other scripts, fixtures, frontend build/config/dependency inputs, migrations,
OpenAPI and non-Markdown architecture inputs retain their existing treatment.
If a nominal test or document changes a shipped runtime, public API, installation,
upgrade, verification or recovery contract, it is not a pure test/document edit
and still needs the corresponding product version and review. Do not bypass an
unexempted check or broaden exemptions merely from a filename.

Independent capability Bundles retain their own identities, digests and evidence.
They do not imply a backend rebuild when implementation is unchanged; the current
CI still runs their existing Backend coverage. Required Backend/Frontend/Docker
checks, exact-main protocol evidence, and unknown-path fail-safe remain intact.
This policy does not authorize a smaller replacement for required regression,
a budget increase, or an ECS self-hosted runner.

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

Operational rules for the existing shared-DB stable/canary startup-migration
topology follow. A different explicitly accepted migration topology uses its
applicable runbook and single migration owner; these examples do not authorize
a topology change or an additional migration writer.

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
- Update only affected status, acceptance or issue sources when their facts change;
  do not rewrite every document merely because another source changed.
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
Apply root `AGENTS.md` retirement boundaries and the accepted design/runbook’s
finite exit criteria. History readability does not promise permanent legacy
execution; supported consumers and rollback promises still require evidence.
Use the retirement table in the applicable existing design or runbook rather
than creating another ledger. File and user-data deletion approvals remain intact.

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
