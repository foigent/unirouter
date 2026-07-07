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
10. `docs/agent-workflows/AGENT-PROTOCOL.md` - risk-gated Codex/OpenClaw Canary/Sentinel/Atlas workflow.
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

## Risk-Gated Agent Workflow

Use `docs/agent-workflows/AGENT-PROTOCOL.md` to classify each change before deciding the handoff path:

1. Codex implements or edits and classifies the change as R0/R1/R2/R3/R4.
2. R0 local cleanup does not require `FIX-SIGNAL.md` / `VERIFY-RESULT.md`.
3. R1 tooling/evidence changes may be grouped into one Canary gate.
4. R2/R3/R4 changes require Codex to update `docs/agent-workflows/FIX-SIGNAL.md`.
5. User asks OpenClaw Canary to verify that file when a risk gate is triggered.
6. OpenClaw writes `docs/agent-workflows/VERIFY-RESULT.md`.
7. Codex reads the result and either closes the loop or fixes failures.

User handoff sentence for OpenClaw when a risk gate is triggered:

```text
去 /Users/m4/Desktop/projects/UniRouter/docs/agent-workflows/FIX-SIGNAL.md，按里面的验收指令逐条验证，结果写入 VERIFY-RESULT.md。
```

## Current Verification Baseline

Latest accepted PRD4 hardening baseline is PRD4 Preflight Hardening on 2026-07-07:

- `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 6/6.
- User reported Sentinel/Atlas completed after `e335a9e` / PR #83.
- PRD4 Batch 0 scope baseline is complete per User report after `e24731d` / PR #82; the Batch 0 `VERIFY-RESULT.md` content has since been overwritten by the Preflight result.

Latest accepted PRD4 implementation batch is PRD4 Batch 1I Real SSO Evidence
Harness:

- Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `2c02430` / PR #92.
- Batch 1I adds guarded tooling for `P4-F09` real SSO evidence:
  `scripts/prd4-real-sso-smoke.sh`, `scripts/prd4_real_sso_smoke.mjs`,
  `scripts/prd4_real_sso_audit_check.go`, and
  `docs/tech-ref/24-prd4-real-sso-evidence.md`.
- Preflight mode is safe without target credentials: it reports `SKIP`, does
  not drive a browser, does not call an IdP, and does not read the database.
- Real mode requires approved Logto or enterprise OIDC target access, a test
  user, console/backend/issuer URLs, DB DSN, and
  `UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso`.
- Batch 1I does not mark `P4-F09` E2E accepted or Production verified without a
  successful approved target-IdP browser login and audit evidence.

Latest workflow verification gate is Protocol Risk-Gated Workflow Update And
Status Reconciliation:

- Canary PASS 10/10 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- No runtime code, migrations, PRD4 product behavior, or target-environment
  evidence changed.

Latest accepted real upstream evidence-harness batch is PRD4 Batch 1H:

- Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`.
- Sentinel merge is reflected by `d3acfa6` / PR #91.
- Atlas deployment has not been reported for Batch 1H.
- Batch 1H adds guarded tooling for `P4-F08` real upstream evidence:
  `scripts/prd4-real-upstream-smoke.sh`, `scripts/prd4_real_upstream_seed.go`,
  `scripts/prd4_real_upstream_smoke.mjs`, and
  `docs/tech-ref/23-prd4-real-upstream-evidence.md`.
- Preflight mode is safe without credentials: it reports `SKIP`, does not
  mutate the database, and does not call providers.
- Real mode requires approved OpenRouter and direct Anthropic credentials/model
  slugs, matching backend secrets, and
  `UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream`.
- Batch 1H does not mark `P4-F08` E2E accepted or Production verified without a
  successful approved target-provider run.

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
