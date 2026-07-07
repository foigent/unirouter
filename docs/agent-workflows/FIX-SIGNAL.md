# FIX-SIGNAL.md - Protocol Risk-Gated Workflow Update And Status Reconciliation

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R2 process-rule and reader-entrypoint change
**Scope:** Agent workflow protocol plus stale Batch 1I status reconciliation; no product runtime code changed

## Verification Request

Please verify the protocol update that replaces the old "every round updates
FIX-SIGNAL / Canary overwrites VERIFY-RESULT" behavior with a risk-gated
workflow, plus the minimal status reconciliation required after Batch 1I was
verified and merged.

This is intentionally a focused process/documentation gate:

- No backend code changed.
- No frontend code changed.
- No migrations changed.
- PRD4 Batch 1I harness status is reconciled from `Fixed awaiting Canary` to
  Canary/Sentinel complete based on `VERIFY-RESULT.md` PASS 11/11 and commit
  `2c02430` / PR #92.
- `P4-F09` remains below `E2E accepted` / `Production verified`; no real IdP
  browser login evidence is claimed.

## Files To Verify

- `docs/agent-workflows/AGENT-PROTOCOL.md`
- `docs/DOCS-GOVERNANCE.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/agent-workflows/README.md`
- `README.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/PRD-Phase4.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

## Expected Protocol Semantics

The updated protocol should say:

1. Verification is risk-tiered as R0/R1/R2/R3/R4.
2. R0 local cleanup does not require `FIX-SIGNAL.md` or
   `VERIFY-RESULT.md`.
3. R1 tooling/test/harness work can be grouped into one Canary gate.
4. R2/R3/R4 changes still require `FIX-SIGNAL.md` and `VERIFY-RESULT.md`.
5. R3 core paths such as auth, SSO, billing, wallet, routing, DB migration,
   secret handling, RBAC, admission, trace retention, and redaction require
   focused independent gates.
6. R4 target-environment or production claims require real target evidence;
   local/mock/preflight results cannot be promoted to `Production verified`.
7. Sentinel is not a substitute for Canary product verification.
8. Atlas is not mandatory for every batch; it is used for deployment, runtime
   environment, configuration, target evidence, or explicit user request.
9. Status docs are not batch ledgers and should only change when current facts,
   acceptance state, phase boundaries, or reader entrypoints change.
10. PRD alignment gates should run every 2-3 implementation batches or before a
    new workstream.

## Status Reconciliation To Verify

The docs should say:

1. Batch 1I Real SSO Evidence Harness passed Canary 11/11.
2. Sentinel merge is reflected by `2c02430` / PR #92.
3. Batch 1I acceptance is limited to harness correctness, guardrails, safe
   preflight behavior, evidence redaction, and docs consistency.
4. `P4-F09` is not `E2E accepted` and not `Production verified` until an
   approved real Logto or enterprise OIDC browser login passes with audit
   evidence.
5. The current active Codex signal is this protocol/status reconciliation gate,
   not the old Batch 1I signal.

## Non-Scope

Do not expect this verification to:

- implement or test PRD4 product functionality;
- change SSO, upstream, billing, trace, runtime observability, or quota code;
- claim real SSO E2E acceptance, real upstream E2E acceptance, Atlas deployment,
  or production verification status;
- require full Go/frontend regression tests.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "R0|R1|R2|R3|R4|风险分级|risk-gated|风险门|FIX-SIGNAL|VERIFY-RESULT|Atlas|Sentinel|PRD alignment" docs/agent-workflows/AGENT-PROTOCOL.md docs/DOCS-GOVERNANCE.md docs/PROJECT-HANDOFF.md docs/agent-workflows/README.md README.md docs/agent-workflows/FIX-SIGNAL.md

rg -n "Mandatory Agent Workflow|Code changes and acceptance-sensitive|for each verification round|mandatory for code changes|每轮|每次.*FIX-SIGNAL|Codex 开发 \\+ 必要测试 \\+ 更新 FIX-SIGNAL|实现后写 FIX-SIGNAL|当前 PRD3|Current PRD3 cleanup|PRD4 \\| Candidate backlog|Candidate backlog only|PRD4 candidate items" docs/agent-workflows/AGENT-PROTOCOL.md docs/agent-workflows/README.md docs/DOCS-GOVERNANCE.md docs/PROJECT-HANDOFF.md docs/README.md README.md

rg -n "Batch 1I|PRD4-SSO-001|P4-F09|2c02430|PR #92|Protocol Risk-Gated Workflow|E2E accepted|Production verified" docs/ops/CURRENT-STATUS.md docs/PROJECT-HANDOFF.md docs/README.md docs/DOC-INVENTORY.md docs/issues/OPEN-ISSUES.md docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/agent-workflows/FIX-SIGNAL.md

rg -n "Batch 1I Real SSO Evidence Harness is fixed awaiting Canary|Current active Codex signal is PRD4 Batch 1I|Current active verification signal: PRD4 Batch 1I|PRD4-SSO-001.*Fixed awaiting Canary|P4-F09 \\| Real SSO Evidence Track \\| Fixed awaiting Canary|Active verification signal: PRD4 Batch 1I" docs/ops/CURRENT-STATUS.md docs/PROJECT-HANDOFF.md docs/README.md docs/DOC-INVENTORY.md docs/issues/OPEN-ISSUES.md docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md

git diff -- README.md docs/DOCS-GOVERNANCE.md docs/PROJECT-HANDOFF.md docs/agent-workflows/AGENT-PROTOCOL.md docs/agent-workflows/README.md docs/agent-workflows/FIX-SIGNAL.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/PRD-Phase4.md docs/DOC-INVENTORY.md docs/README.md
git diff --check
```

The second and fourth `rg` commands are expected to return no matches.

## Acceptance Criteria

1. `AGENT-PROTOCOL.md` is versioned to 1.4 and defines R0/R1/R2/R3/R4.
2. `AGENT-PROTOCOL.md` no longer requires fixed `FIX-SIGNAL.md` /
   `VERIFY-RESULT.md` updates for every small change.
3. `DOCS-GOVERNANCE.md` uses the same risk-tiered verification rule and lists
   PRD4 as the active product scope.
4. `PROJECT-HANDOFF.md` and `docs/agent-workflows/README.md` route future
   agents to the risk-gated workflow.
5. Root `README.md` no longer presents PRD3 as the current active scope and
   includes PRD4 current scope/matrix entries.
6. Batch 1I stale `Fixed awaiting Canary` wording is removed from current PRD4
   status, matrix, issue row, handoff, and docs indexes.
7. Batch 1I is marked Canary/Sentinel complete only for harness/guardrail
   readiness, with `2c02430` / PR #92 evidence.
8. `P4-F09` remains below `E2E accepted` / `Production verified` in every
   current truth document.
9. No unrelated product code, migrations, or target-environment evidence claims
   changed.
10. `git diff --check` passes.
