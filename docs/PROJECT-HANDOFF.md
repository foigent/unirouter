# UniRouter Agent Handoff

> Purpose: fast onboarding for Codex, OpenClaw Canary, or any other agent taking over this repository.
> Read this file first, then follow the linked current-state documents.

## First Read Order

1. `docs/ops/CURRENT-STATUS.md` - current verified project status.
2. `docs/ops/PRD-ACCEPTANCE-MATRIX.md` - Phase 1 acceptance progress and missing evidence.
3. `docs/ops/PRD1-CLOSEOUT-PLAN.md` - closeout plan from PRD1 to PRD2, including UI.
4. `docs/ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md` - final strict closeout gate before PRD1 can close.
5. `docs/issues/OPEN-ISSUES.md` - active unresolved issues.
6. `docs/agent-workflows/AGENT-PROTOCOL.md` - mandatory Codex/OpenClaw/Sentinel/Atlas workflow.
7. `docs/CODEBASE-MAP.md` - code, route, data, test, and deployment map.
8. `docs/DOC-INVENTORY.md` - which docs are current, historical, or reference-only.

## Current Project Shape

UniRouter is a local-deployable enterprise AI gateway/MaaS platform. The Phase 1 scope is an OpenAI-compatible gateway, admin API, admin frontend, pre-freeze billing, call logs, wallet management, model pricing, rate limiting, circuit breaker/failover, sensitive-word filtering, idempotency, health checks, and monitoring.

The codebase is already beyond scaffold stage. Most Phase 1 modules exist and local tests/builds pass, but full PRD delivery still needs current end-to-end acceptance evidence.

## What Is Current Truth

| Question | Current Source |
|----------|----------------|
| What is the product supposed to do? | `docs/PRD-Phase1.md` |
| What is actually verified now? | `docs/ops/CURRENT-STATUS.md` |
| Which PRD items are accepted or still pending evidence? | `docs/ops/PRD-ACCEPTANCE-MATRIX.md` |
| What must close before PRD2? | `docs/ops/PRD1-CLOSEOUT-PLAN.md` |
| What should be fixed next? | `docs/issues/OPEN-ISSUES.md` |
| How should Codex and OpenClaw coordinate? | `docs/agent-workflows/AGENT-PROTOCOL.md` |
| Which old docs may be stale? | `docs/DOC-INVENTORY.md` |

Do not treat files under `docs/testing/archive/` as current status. They are historical evidence unless reconciled into the current-state files above.

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

Verified in the local workspace on 2026-07-01:

```bash
go test ./...
cd frontend && npm test -- --run
cd frontend && npm run build
```

Results:

- Go tests: PASS.
- Frontend tests: PASS, 33 tests.
- Frontend build: PASS with Vite large chunk warning.

This does not equal Phase 1 delivery acceptance. E2E provider calls, browser validation, concurrency, failover, interrupted streaming, default-admin remediation deployment, and deployment evidence still need current verification.

## Worktree Warning

The worktree currently contains pre-existing uncommitted changes in business code and historical docs. Do not revert them unless the user explicitly requests it. New agents should run `git status --short` before editing and avoid mixing unrelated cleanup with feature work.

## No Bulk Deletion

Project instruction forbids bulk deletion of files or directories. If cleanup requires removing files, stop and ask the user. Only one explicit file path may be deleted at a time after approval.
