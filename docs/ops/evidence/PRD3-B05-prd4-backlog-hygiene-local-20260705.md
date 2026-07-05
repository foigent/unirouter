# PRD3 Batch 5 PRD4 Backlog Hygiene - Local Evidence

Date: 2026-07-05
Owner: Codex
Scope: documentation and product-scope boundary only

## Summary

User confirmed that infrastructure environment information such as CPU, memory,
storage, network, server inventory, and cluster state should be handled in PRD4,
not implemented in PRD3.

This batch records that decision and keeps current Internal Status scoped to the
existing notification-event-derived semantics.

## Files Changed

- `docs/planning/PRD4-BACKLOG.md`
- `docs/PRD-Phase3.md`
- `docs/planning/PRD3-CONSISTENCY-AUDIT.md`
- `docs/tech-ref/20-prd3-product-glossary.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/README.md`
- `docs/DOC-INVENTORY.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

## Decisions Recorded

- `PRD4-INFRA-001` tracks infrastructure observability and runtime environment
  visibility as PRD4 candidate scope.
- Runtime environment and capacity health terms are added as PRD4 glossary
  candidates, not PRD3 API fields.
- Current Internal Status remains event-derived from notification severity and
  lifecycle state.
- Infrastructure visibility must define telemetry source of truth, freshness,
  RBAC/redaction, stale-data behavior, instance inventory, and the relationship
  between metric alerts and notification events before implementation.

## Non-Goals

- No runtime metrics collection.
- No backend API changes.
- No frontend Internal Status implementation.
- No deployment topology discovery.
- No new role or permission implementation.

## Local Verification

Commands run after the documentation update:

```bash
rg -n "PRD4-BACKLOG|PRD4-INFRA-001|Infrastructure observability|Runtime Environment|Capacity Health" docs
git diff --check
rg -n "[ \t]+$" docs/planning/PRD4-BACKLOG.md docs/ops/evidence/PRD3-B05-prd4-backlog-hygiene-local-20260705.md
```

Result:

- The grep command finds the PRD4 backlog file, PRD3 scope references, glossary
  terms, issue entry, status/matrix references, and docs index references.
- `git diff --check` exits successfully.
- The added-file trailing-whitespace check returns no matches.
