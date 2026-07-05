# PRD3 Batch 8 Engineering Governance Local Evidence

> Date: 2026-07-05
> Scope: P3-F12 Engineering Governance Minimum / PRD3-ENG-001
> Status: Fixed awaiting Canary

## Summary

Batch 8 adds the minimum engineering governance needed before PRD4 expansion.
The work is documentation-only and does not change runtime code, API routes,
frontend behavior, database migrations, deployment configuration, or tests.

## Files Changed

- `docs/governance/README.md`
  - ADR policy
  - API compatibility and versioning rules
  - migration and rollback policy
  - release checklist
  - incident response minimum
  - deprecation policy
- `docs/governance/ADR-TEMPLATE.md`
  - Architecture Decision Record template
- `docs/README.md`
  - adds governance entrypoints
- `docs/DOC-INVENTORY.md`
  - records governance docs in the current documentation inventory
- `docs/DOCS-GOVERNANCE.md`
  - records the engineering governance doc role
- `CONTRIBUTING.md`
  - links governance and ADR expectations from contributor docs
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
  - marks P3-F12 fixed awaiting Canary
- `docs/issues/OPEN-ISSUES.md`
  - marks PRD3-ENG-001 fixed awaiting Canary
- `docs/ops/CURRENT-STATUS.md`
  - records Batch 8 as current verification work
- `docs/agent-workflows/FIX-SIGNAL.md`
  - requests Canary verification for Batch 8

## Decisions

- Keep PRD3 engineering governance as a concise minimum baseline, not a full
  handbook rewrite.
- Keep API compatibility tied to the existing route-level OpenAPI contract and
  route parity guard.
- Keep migration rollback guidance conservative: backups and explicit down-file
  review are required before treating rollback as safe.
- Keep PRD3 Internal Status event-derived; full maintenance-window, SLA/SLO,
  runtime environment, and capacity-health product models remain PRD4 unless
  explicitly approved earlier.

## Non-Goals

- No plugin runtime, plugin marketplace, or third-party installation work.
- No Kubernetes, Helm, Terraform, managed HA, or new deployment product.
- No new incident management product, SLA/SLO engine, or maintenance scheduler.
- No new persistent role or authority surface.
- No runtime code, API, frontend, migration, test, or build changes.

## Local Verification

Commands run after changes:

```bash
rg -n "ADR|API Compatibility|Migration And Rollback|Release Checklist|Incident Response|Deprecation" docs/governance docs/README.md docs/DOC-INVENTORY.md docs/DOCS-GOVERNANCE.md CONTRIBUTING.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md
rg -n "PRD3 Batch 7 document lifecycle cleanup fixed awaiting Canary|PRD3-DOCS-001 \\| Medium \\| Fixed awaiting Canary" docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md
git diff --check
```

Results:

- governance coverage grep returned matches in `docs/governance/README.md`,
  `docs/governance/ADR-TEMPLATE.md`, `docs/README.md`,
  `docs/DOC-INVENTORY.md`, `docs/DOCS-GOVERNANCE.md`, `CONTRIBUTING.md`,
  `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`, `docs/issues/OPEN-ISSUES.md`, and
  `docs/ops/CURRENT-STATUS.md`;
- stale Batch 7 awaiting-verification grep returned no matches;
- `git diff --check` passed.
