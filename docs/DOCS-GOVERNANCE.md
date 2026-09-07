# Documentation Governance

> Purpose: prevent status drift across PRD, tests, bug logs, and agent workflow files.

## Document Roles

| Role | Canonical File | Rule |
|------|----------------|------|
| Agent handoff | `docs/PROJECT-HANDOFF.md` | Task-based navigation; dated checkpoints below are historical, not live status |
| System fact map | `docs/architecture/SYSTEM-FACT-MAP.md` | Canonical current entity, authority, writer, projection-consumer, impact, and golden-journey map; update it with any relationship change |
| Codebase map | `docs/CODEBASE-MAP.md` | High-level implementation map, not a substitute for reading code |
| Document inventory | `docs/DOC-INVENTORY.md` | Classifies current, reference, and historical docs |
| Active delivery scope | `docs/agent-workflows/EXECUTION-RECORD.md` | Current issue/corrective batch, baseline, non-goals, verification and promotion state; it cannot silently reopen a completed PRD |
| Product lineage | `docs/PRD-Phase1.md` through `docs/PRD-Phase8.md` | PRD8 governs forward product ownership; PRD1–PRD7 retain their accepted scope and historical evidence; current delivery scope is recorded separately |
| Current status | `docs/ops/CURRENT-STATUS.md` | Current verified summary for progress, tests, deployment, gaps and explicit non-claims |
| PRD acceptance | `docs/ops/PRD8-BOUNDARY-MATRIX.md`, `docs/ops/PRD7-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD6-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD-ACCEPTANCE-MATRIX.md` | Evidence-backed acceptance state for active, completed or frozen PRD checklist items |
| Historical PRD7 candidate input | `docs/planning/PRD7-CANDIDATE-BACKLOG.md` | Preserved candidate and prerequisite record; later accepted scope is authoritative in `docs/PRD-Phase7.md` and its matrix |
| Engineering governance | `docs/governance/README.md` | Minimum ADR, API compatibility, migration/rollback, release, incident, and deprecation rules |
| Active issues | `docs/issues/OPEN-ISSUES.md` | Only active unresolved issues and recently closed items |
| Agent protocol | `docs/agent-workflows/AGENT-PROTOCOL.md` | Single execution owner, approval and evidence; mandatory review triggers remain in root AGENTS.md |
| Current execution record | `docs/agent-workflows/EXECUTION-RECORD.md` | Maintained by the execution owner for the active batch |
| Independent reviews | `docs/agent-workflows/reviews/` | Exact-object review results; verified/adjudicated current blockers affect the relevant promotion, not unrelated scope |
| Legacy workflow files | `docs/agent-workflows/FIX-SIGNAL.md`, `docs/agent-workflows/VERIFY-RESULT.md` | v1.x compatibility entry and most recent historical verification result; not current serial gates |
| Runtime evidence | `docs/ops/evidence/` | Deployment, recovery, and runtime drill reports cited by current status or acceptance matrices |
| Historical evidence | `docs/testing/archive/`, `docs/archive/`, old reports | Read-only history unless explicitly reconciling |

## Phase Lifecycle

| Phase | Lifecycle | Planning Rule |
|-------|-----------|---------------|
| PRD1 | Historical/frozen for forward planning | Use for requirements lineage and historical evidence only. Current progress claims must be in `CURRENT-STATUS.md` or acceptance matrices. |
| PRD2 | Frozen for new feature work | Use as accepted/deferred product reference. Do not add new PRD2 feature scope. |
| PRD3 | Completed cleanup/optimization phase | Use as accepted cleanup/optimization reference. Do not add new PRD3 scope unless User explicitly reopens it. |
| PRD4 | Completed for the confirmed scope | Do not reopen unless the User explicitly changes the boundary. Deferred work is routed to PRD5. |
| PRD5 | Completed / Accepted Scope Complete | Preserve its scope and evidence; do not route new work back into PRD5. |
| PRD6 | Completed (Accepted Scope) | Preserve the 27/27 accepted-row closeout. Later issue-driven corrections are separate batches, not silent PRD6 reopening. |
| Post-PRD6 V1 corrections | Issue-driven delivery batches only | Use `EXECUTION-RECORD.md`, `OPEN-ISSUES.md` and new evidence. State exact non-goals and keep previous immutable closeout evidence unchanged. |
| PRD7 | Completed repository-local scope / historical | Preserve its matrix and implementation evidence; PRD8 governs forward ownership. Historical external gates apply to their recorded scope. |
| PRD8 | Accepted forward product boundary | `docs/PRD-Phase8.md` and `docs/ops/PRD8-BOUNDARY-MATRIX.md` govern product ownership; current tasks and external authority remain separately bounded. |

## Rules

1. Do not make archive files the current status source.
2. Do not duplicate active bug status in multiple places.
3. Do not reuse issue IDs.
4. Do not hard-code "latest" dates unless they are verified in the current pass.
5. Every `E2E accepted` or `Production verified` claim needs command/manual path, environment, expected result, actual result, and timestamp or commit/worktree reference.
6. Execution uses R0–R3 in `docs/agent-workflows/AGENT-PROTOCOL.md`. Maintain `EXECUTION-RECORD.md` when a task is long-running, crosses an external lifecycle or needs recovery; add evidence at applicable material risk boundaries. Root `AGENTS.md` defines mandatory pre-code independent reviews; other reviews are optional. Verified/adjudicated current blockers affect the relevant action; post-release fixes preserve historical evidence and do not silently reopen completed PRD scope.
7. Documentation-only changes use proportionate checks. Changes to status, issues, acceptance, phase boundaries, reader entrypoints or process rules must reconcile the affected sources; spelling and ordinary wording do not require a full engineering workflow.
8. Root and docs README files must route readers to the active delivery batch and customer/developer task first, then frozen/historical references.
9. Historical PRD7 candidates must not be described as implemented unless the
   PRD7 acceptance matrix contains the row and exact evidence. The User's
   2026-07-30/31 decisions accept the scope in `docs/PRD-Phase7.md`; they do
   not retroactively change old evidence or authorize runtime actions.
10. Before changing a cross-page product concept, use `docs/architecture/SYSTEM-FACT-MAP.md` to identify its authority, writers, and projection consumers. A local page fix is incomplete when an affected consumer is left inconsistent.

## Cleanup Policy

No bulk deletion or bulk directory removal is allowed. Historical files should remain in place unless the user explicitly approves deleting one exact file path at a time.

When a historical document conflicts with the current source of truth:

1. Do not rewrite the historical document by default.
2. Reconcile the current truth into `CURRENT-STATUS.md`, `PRD-ACCEPTANCE-MATRIX.md`, or `OPEN-ISSUES.md`.
3. If needed, add a short note in the current file explaining that older archive entries may be stale.

## Recommended Update Flow

1. Classify the change as R0/R1/R2/R3 using `AGENT-PROTOCOL.md`.
2. Implement or verify a change.
3. Update `OPEN-ISSUES.md` only if issue state changed.
4. Update the relevant acceptance matrix only if PRD acceptance evidence changed.
5. Update `CURRENT-STATUS.md` only when the current verified summary changed.
6. Update the existing execution record only where the recovery/lifecycle criteria apply; add immutable evidence at applicable important fact boundaries.
7. If an independent reviewer participates, store a new result under `docs/agent-workflows/reviews/`; do not overwrite another result or the legacy `VERIFY-RESULT.md`.

A draft Baseline is not an approved immutable requirement set. Navigation must reflect the document’s actual approval state and avoid copying changing model counts. Independently accepted user requirements remain binding within their scope.
