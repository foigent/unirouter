# UniRouter Agent Handoff

> Purpose: fast onboarding for Codex, OpenClaw Canary, or any other agent taking over this repository.
> Read this file first, then follow the linked current-state documents.

## First Read Order

1. `docs/ops/CURRENT-STATUS.md` - current verified project status.
2. `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` - active PRD3 cleanup/optimization acceptance state.
3. `docs/PRD-Phase3.md` - active PRD3 scope.
4. `docs/planning/PRD3-CONSISTENCY-AUDIT.md` - PRD1 -> PRD2 consistency findings behind PRD3.
5. `docs/issues/OPEN-ISSUES.md` - active unresolved issues and recently verified fixes.
6. `docs/agent-workflows/AGENT-PROTOCOL.md` - mandatory Codex/OpenClaw Canary/Sentinel/Atlas workflow.
7. `docs/agent-workflows/FIX-SIGNAL.md` and `docs/agent-workflows/VERIFY-RESULT.md` - current verification request/result.
8. `docs/CODEBASE-MAP.md` - code, route, data, test, and deployment map.
9. `docs/DOC-INVENTORY.md` and `docs/DOCS-GOVERNANCE.md` - which docs are current, frozen, historical, candidate, or reference-only.

## Current Project Shape

UniRouter is a local-deployable enterprise AI gateway/MaaS platform. PRD1 delivered the core gateway, admin API, admin frontend, billing, logs, wallet, pricing, admission controls, health checks, and monitoring foundation. PRD2 is frozen for new feature work after accepted product expansions such as project/RBAC, model governance, notification events, license foundation, OIDC, usage analytics, internal status, and error docs.

The active phase is PRD3. PRD3 is cleanup/optimization only: consistency, semantics, route/API contract, documentation lifecycle, module boundaries, frontend architecture, parity tests, and engineering governance. New capability design belongs in `docs/planning/PRD4-BACKLOG.md` unless the User explicitly reopens scope.

## What Is Current Truth

| Question | Current Source |
|----------|----------------|
| What is the active PRD3 scope? | `docs/PRD-Phase3.md` |
| What is actually verified now? | `docs/ops/CURRENT-STATUS.md` |
| Which PRD3 items are accepted or still pending evidence? | `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` |
| Which PRD2 items are frozen/accepted/deferred? | `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` |
| Which capabilities are PRD4 candidates? | `docs/planning/PRD4-BACKLOG.md` |
| What should be fixed next? | `docs/issues/OPEN-ISSUES.md` |
| How should Codex and OpenClaw coordinate? | `docs/agent-workflows/AGENT-PROTOCOL.md` |
| Which old docs may be stale? | `docs/DOC-INVENTORY.md` |

PRD1 and PRD2 documents remain useful requirements/evidence references, but they are not the active planning surface for new work. Do not treat files under `docs/testing/archive/` as current status. They are historical evidence unless reconciled into the current-state files above.

## Mandatory Agent Workflow

Code changes and acceptance-sensitive documentation changes must use the protocol:

1. Codex implements or edits.
2. Codex updates `docs/agent-workflows/FIX-SIGNAL.md`.
3. User asks OpenClaw Canary to verify that file.
4. OpenClaw writes `docs/agent-workflows/VERIFY-RESULT.md`.
5. Codex reads the result and either closes the loop or fixes failures.

User handoff sentence for OpenClaw:

```text
去 /Users/m4/Desktop/projects/UniRouter/docs/agent-workflows/FIX-SIGNAL.md，按里面的验收指令逐条验证，结果写入 VERIFY-RESULT.md。
```

## Current Verification Baseline

Latest accepted PRD3 route-contract baseline is PRD3 Batch 6 on 2026-07-05:

```bash
go test ./cmd/uniroute -run 'Test(OpenAPISpecCoversRegisteredRoutes|FrontendDocsPageCoversRegisteredRoutes|BackendDocsPageUsesCurrentContractLanguage|AdminRoutesRegisteredForFrontendClient)' -count=1 -v
npm --prefix frontend test -- --run ApiDocsPage client
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Results:

- Route parity target tests: PASS.
- Go tests: PASS.
- Frontend tests: PASS, 18 files / 97 tests.
- Frontend build: PASS with Vite large chunk warning.

Always check `docs/ops/CURRENT-STATUS.md` and `docs/agent-workflows/VERIFY-RESULT.md` for the latest accepted batch before making new claims.

## Worktree Warning

Always run `git status --short` before editing. The worktree may contain user or agent changes from another round. Do not revert them unless the user explicitly requests it. Avoid mixing unrelated cleanup with the current batch.

## No Bulk Deletion

Project instruction forbids bulk deletion of files or directories. If cleanup requires removing files, stop and ask the user. Only one explicit file path may be deleted at a time after approval.
