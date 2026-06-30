# Documentation Governance

> Purpose: prevent status drift across PRD, tests, bug logs, and agent workflow files.

## Document Roles

| Role | Canonical File | Rule |
|------|----------------|------|
| Agent handoff | `docs/PROJECT-HANDOFF.md` | Read first when taking over the project |
| Codebase map | `docs/CODEBASE-MAP.md` | High-level implementation map, not a substitute for reading code |
| Document inventory | `docs/DOC-INVENTORY.md` | Classifies current, reference, and historical docs |
| Product requirements | `docs/PRD-Phase1.md` | Requirements and acceptance criteria only |
| Current status | `docs/ops/CURRENT-STATUS.md` | One-page current truth for progress, tests, deployment, and gaps |
| PRD acceptance | `docs/ops/PRD-ACCEPTANCE-MATRIX.md` | Evidence-backed acceptance state for each PRD checklist item |
| Active issues | `docs/issues/OPEN-ISSUES.md` | Only active unresolved issues and recently closed items |
| Agent protocol | `docs/agent-workflows/AGENT-PROTOCOL.md` | Required workflow for Codex and OpenClaw Canary |
| Current verification request | `docs/agent-workflows/FIX-SIGNAL.md` | Overwritten by Codex for each verification round |
| Current verification result | `docs/agent-workflows/VERIFY-RESULT.md` | Overwritten by OpenClaw Canary for each verification round |
| Historical evidence | `docs/testing/archive/`, `docs/archive/`, old reports | Read-only history unless explicitly reconciling |

## Rules

1. Do not make archive files the current status source.
2. Do not duplicate active bug status in multiple places.
3. Do not reuse issue IDs.
4. Do not hard-code "latest" dates unless they are verified in the current pass.
5. Every `E2E accepted` or `Production verified` claim needs command/manual path, environment, expected result, actual result, and timestamp or commit/worktree reference.
6. Agent verification is mandatory for code changes: Codex writes `FIX-SIGNAL.md`; OpenClaw Canary writes `VERIFY-RESULT.md`.
7. Documentation-only changes may still use the protocol when they alter current status, issue state, acceptance state, or process rules.

## Cleanup Policy

No bulk deletion or bulk directory removal is allowed. Historical files should remain in place unless the user explicitly approves deleting one exact file path at a time.

When a historical document conflicts with the current source of truth:

1. Do not rewrite the historical document by default.
2. Reconcile the current truth into `CURRENT-STATUS.md`, `PRD-ACCEPTANCE-MATRIX.md`, or `OPEN-ISSUES.md`.
3. If needed, add a short note in the current file explaining that older archive entries may be stale.

## Recommended Update Flow

1. Implement or verify a change.
2. Update `OPEN-ISSUES.md` if issue state changed.
3. Update `PRD-ACCEPTANCE-MATRIX.md` if PRD acceptance evidence changed.
4. Update `CURRENT-STATUS.md` with the latest verified summary.
5. If code or acceptance-sensitive docs changed, update `FIX-SIGNAL.md` for OpenClaw Canary.
