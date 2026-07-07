# UniRouter Agent Handoff

> Purpose: fast onboarding for Codex, OpenClaw Canary, or any other agent taking over this repository.
> Read this file first, then follow the linked current-state documents.

## First Read Order

1. `docs/ops/CURRENT-STATUS.md` - current verified project status.
2. `docs/PRD-Phase4.md` - proposed formal PRD4 scope.
3. `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` - PRD4 acceptance rows and evidence boundaries.
4. `docs/planning/PRD5-BACKLOG.md` - capabilities intentionally deferred beyond PRD4.
5. `docs/planning/PRD4-BACKLOG.md` - PRD4 candidate backlog source record.
6. `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` - completed PRD3 cleanup/optimization acceptance state.
7. `docs/PRD-Phase3.md` - completed PRD3 scope.
8. `docs/planning/PRD3-CONSISTENCY-AUDIT.md` - PRD1 -> PRD2 consistency findings behind PRD3.
9. `docs/issues/OPEN-ISSUES.md` - active unresolved issues and recently verified fixes.
10. `docs/agent-workflows/AGENT-PROTOCOL.md` - mandatory Codex/OpenClaw Canary/Sentinel/Atlas workflow.
11. `docs/agent-workflows/FIX-SIGNAL.md` and `docs/agent-workflows/VERIFY-RESULT.md` - current verification request/result.
12. `docs/CODEBASE-MAP.md` - code, route, data, test, and deployment map.
13. `docs/DOC-INVENTORY.md` and `docs/DOCS-GOVERNANCE.md` - which docs are current, frozen, historical, candidate, or reference-only.

## Current Project Shape

UniRouter is a local-deployable enterprise AI gateway/MaaS platform. PRD1 delivered the core gateway, admin API, admin frontend, billing, logs, wallet, pricing, admission controls, health checks, and monitoring foundation. PRD2 is frozen for new feature work after accepted product expansions such as project/RBAC, model governance, notification events, license foundation, OIDC, usage analytics, internal status, and error docs.

PRD3 is complete for the confirmed scope. It was cleanup/optimization only: consistency, semantics, route/API contract, documentation lifecycle, module boundaries, frontend architecture, parity tests, and engineering governance.

PRD4 is now proposed as the commercial observability and control foundation phase. Its formal scope draft is `docs/PRD-Phase4.md`: API key owner usage/cost, quota vocabulary, Flow/Event/Trace foundation, PlungeIn observation-first ports, Status Center runtime observability, external monitoring integration, real upstream evidence, real SSO evidence, auth UX hardening, production evidence promotion, and PRD5 backlog consolidation. `docs/planning/PRD4-BACKLOG.md` remains the candidate-source record, not the formal implementation contract.

## What Is Current Truth

| Question | Current Source |
|----------|----------------|
| What is the PRD4 formal scope? | `docs/PRD-Phase4.md` |
| What is the PRD4 acceptance plan? | `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` |
| Which items are explicitly deferred to PRD5? | `docs/planning/PRD5-BACKLOG.md` |
| What is actually verified now? | `docs/ops/CURRENT-STATUS.md` |
| Which PRD3 items are accepted? | `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` |
| Which PRD2 items are frozen/accepted/deferred? | `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` |
| Which capabilities were PRD4 candidates before formal scoping? | `docs/planning/PRD4-BACKLOG.md` |
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

Latest accepted PRD4 hardening baseline is PRD4 Preflight Hardening on 2026-07-07:

- `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 6/6.
- User reported Sentinel/Atlas completed after `e335a9e` / PR #83.
- PRD4 Batch 0 scope baseline is complete per User report after `e24731d` / PR #82; the Batch 0 `VERIFY-RESULT.md` content has since been overwritten by the Preflight result.

Latest accepted PRD4 implementation batch is PRD4 Batch 1D Runtime Observability
Foundation:

- User reported Canary/Sentinel completed after `349ac75` / PR #87.
- Atlas deployment has not been reported for Batch 1D.
- Batch 1D adds `GET /api/v1/runtime/status` for platform/org/billing roles,
  runtime metadata config, app memory, DB/Redis health, recent traffic summary,
  Prometheus/Grafana configuration state, and `/status` Runtime Observability
  cards while keeping current Internal Status event-derived.
- Host CPU utilization, disk, network, cluster inventory, active
  Prometheus/Grafana probes, embedded dashboards, OpenTelemetry export, and
  metric-alert routing are not in Batch 1D.

Current active verification signal: PRD4 Batch 1E Quota Vocabulary And Key Quota
Foundation is fixed awaiting Canary in `docs/agent-workflows/FIX-SIGNAL.md`.

- Batch 1E standardizes user-facing RPM/RPS/TPM/tokens/s vocabulary, exposes
  preferred `rps_limit` and structured key `quota` response data, and preserves
  legacy `qps_limit` compatibility.
- Scheduler V2, key-level TPM enforcement, budget hard-blocking, quota audit
  reason/idempotency fields, `qps_limit` removal, and schema migration are not
  in Batch 1E.

The latest accepted route-contract baseline remains PRD3 Batch 6 on 2026-07-05:

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
