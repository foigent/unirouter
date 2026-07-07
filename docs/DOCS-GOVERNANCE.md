# Documentation Governance

> Purpose: prevent status drift across PRD, tests, bug logs, and agent workflow files.

## Document Roles

| Role | Canonical File | Rule |
|------|----------------|------|
| Agent handoff | `docs/PROJECT-HANDOFF.md` | Read first when taking over the project |
| Codebase map | `docs/CODEBASE-MAP.md` | High-level implementation map, not a substitute for reading code |
| Document inventory | `docs/DOC-INVENTORY.md` | Classifies current, reference, and historical docs |
| Active product scope | `docs/PRD-Phase4.md` | Active PRD4 implementation scope and evidence boundaries |
| Product lineage | `docs/PRD-Phase1.md`, `docs/PRD-Phase2.md`, `docs/PRD-Phase3.md` | Frozen/completed requirements references, not current-progress sources |
| Current status | `docs/ops/CURRENT-STATUS.md` | One-page current truth for progress, tests, deployment, and gaps |
| PRD acceptance | `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD-ACCEPTANCE-MATRIX.md` | Evidence-backed acceptance state for active, completed, or frozen PRD checklist items |
| PRD4 candidates | `docs/planning/PRD4-BACKLOG.md` | Candidate backlog source record; formal PRD4 scope is `docs/PRD-Phase4.md` |
| Engineering governance | `docs/governance/README.md` | Minimum ADR, API compatibility, migration/rollback, release, incident, and deprecation rules |
| Active issues | `docs/issues/OPEN-ISSUES.md` | Only active unresolved issues and recently closed items |
| Agent protocol | `docs/agent-workflows/AGENT-PROTOCOL.md` | Required workflow for Codex and OpenClaw Canary |
| Current verification request | `docs/agent-workflows/FIX-SIGNAL.md` | Overwritten by Codex only when a risk gate requires Canary verification |
| Current verification result | `docs/agent-workflows/VERIFY-RESULT.md` | Overwritten by OpenClaw Canary only when a risk gate requires Canary verification |
| Runtime evidence | `docs/ops/evidence/` | Deployment, recovery, and runtime drill reports cited by current status or acceptance matrices |
| Historical evidence | `docs/testing/archive/`, `docs/archive/`, old reports | Read-only history unless explicitly reconciling |

## Phase Lifecycle

| Phase | Lifecycle | Planning Rule |
|-------|-----------|---------------|
| PRD1 | Historical/frozen for forward planning | Use for requirements lineage and historical evidence only. Current progress claims must be in `CURRENT-STATUS.md` or acceptance matrices. |
| PRD2 | Frozen for new feature work | Use as accepted/deferred product reference. Do not add new PRD2 feature scope. |
| PRD3 | Completed cleanup/optimization phase | Use as accepted cleanup/optimization reference. Do not add new PRD3 scope unless User explicitly reopens it. |
| PRD4 | Active implementation phase | Use `docs/PRD-Phase4.md` and `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` as the current scope and acceptance contract. |

## Rules

1. Do not make archive files the current status source.
2. Do not duplicate active bug status in multiple places.
3. Do not reuse issue IDs.
4. Do not hard-code "latest" dates unless they are verified in the current pass.
5. Every `E2E accepted` or `Production verified` claim needs command/manual path, environment, expected result, actual result, and timestamp or commit/worktree reference.
6. Agent verification is risk-tiered by `docs/agent-workflows/AGENT-PROTOCOL.md`: R2/R3/R4 changes require `FIX-SIGNAL.md` / `VERIFY-RESULT.md`; R1 changes may be grouped into one gate; R0 local docs cleanup does not require signal files.
7. Documentation-only changes use the protocol only when they alter current status, issue state, acceptance state, phase boundaries, reader entrypoints, or process rules.
8. Root and docs README files must route readers to the active phase first, then frozen/historical references.
9. PRD4 backlog candidates must not be described as implemented, accepted, or scheduled unless `docs/PRD-Phase4.md`, `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`, or a later approved PRD says so.

## Cleanup Policy

No bulk deletion or bulk directory removal is allowed. Historical files should remain in place unless the user explicitly approves deleting one exact file path at a time.

When a historical document conflicts with the current source of truth:

1. Do not rewrite the historical document by default.
2. Reconcile the current truth into `CURRENT-STATUS.md`, `PRD-ACCEPTANCE-MATRIX.md`, or `OPEN-ISSUES.md`.
3. If needed, add a short note in the current file explaining that older archive entries may be stale.

## Recommended Update Flow

1. Classify the change as R0/R1/R2/R3/R4 using `AGENT-PROTOCOL.md`.
2. Implement or verify a change.
3. Update `OPEN-ISSUES.md` only if issue state changed.
4. Update the relevant acceptance matrix only if PRD acceptance evidence changed.
5. Update `CURRENT-STATUS.md` only when the current verified summary changed.
6. Update `FIX-SIGNAL.md` only when the risk gate requires OpenClaw Canary.
