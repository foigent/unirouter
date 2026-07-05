# FIX-SIGNAL.md - PRD3 Batch 5 PRD4 Backlog Hygiene

**Date:** 2026-07-05
**Status:** Fixed awaiting Canary

PRD3 Batch 4 Dashboard taxonomy passed Canary 5/5 in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #67 at `6cbc5b8`;
User reported Canary, Sentinel, and Atlas completed.

Codex has completed PRD3 Batch 5 as a documentation-only scope hygiene update.
User confirmed that UniRouter infrastructure environment visibility such as CPU,
memory, storage, network, server inventory, and cluster state belongs in PRD4,
not PRD3.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- This batch adds PRD4 backlog routing and documentation boundaries only.
- No backend API, frontend UI, telemetry collection, deployment discovery,
  metrics ingestion, role/permission, or Internal Status runtime behavior changed.
- Current Internal Status remains notification-event-derived. Infrastructure
  observability, runtime environment visibility, host inventory, cluster topology,
  and capacity health are PRD4 candidate scope.

## Verification Request

Please verify:

1. `docs/planning/PRD4-BACKLOG.md` exists and includes:
   - `PRD4-INFRA-001`;
   - an Infrastructure Observability Boundary;
   - Runtime Environment / Infrastructure Snapshot and Capacity Health guidance;
   - role/access and sensitive-infrastructure redaction guidance;
   - implementation prerequisites.
2. PRD3 docs route the infrastructure question to PRD4 without implying PRD3
   implementation:
   - `docs/PRD-Phase3.md`;
   - `docs/planning/PRD3-CONSISTENCY-AUDIT.md`;
   - `docs/tech-ref/20-prd3-product-glossary.md`;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`;
   - `docs/issues/OPEN-ISSUES.md`.
3. Documentation indexes reference the PRD4 backlog:
   - `docs/README.md`;
   - `docs/DOC-INVENTORY.md`;
   - `docs/ops/CURRENT-STATUS.md`.
4. Batch 4 status is advanced from awaiting verification to complete:
   - `docs/ops/CURRENT-STATUS.md`;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`;
   - `docs/issues/OPEN-ISSUES.md`.
5. Local evidence exists:
   - `docs/ops/evidence/PRD3-B05-prd4-backlog-hygiene-local-20260705.md`.
6. Run these commands:

```bash
rg -n "PRD4-BACKLOG|PRD4-INFRA-001|Infrastructure observability|Runtime Environment|Capacity Health" docs
git diff --check
```

## Expected Result

- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3
  Batch 5 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `docs/planning/PRD4-BACKLOG.md`
- `docs/PRD-Phase3.md`
- `docs/planning/PRD3-CONSISTENCY-AUDIT.md`
- `docs/tech-ref/20-prd3-product-glossary.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B05-prd4-backlog-hygiene-local-20260705.md`
