# FIX-SIGNAL.md — PRD2 Docs Boundary Sync And PRD3 Deferred Items

**修复者：** Codex
**日期：** 2026-07-02
**修复轮次：** PRD2 documentation sync after Batch 19

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Runtime code | Unchanged | This round only updates planning/status documents |
| Batch 18 | Open / deferred | User explicitly deferred real-upstream provider acceptance |
| Production webhook evidence | Not claimed | Batch 19 has local production-like webhook evidence only |
| `VERIFY-RESULT.md` | Historical Batch 19 record remains unchanged | It records what Canary verified during Batch 19 and should not be rewritten by Codex |

---

## Fix #1: Batch 19 status docs no longer say awaiting Canary

**Files:**

- `docs/ops/CURRENT-STATUS.md:4`
- `docs/ops/CURRENT-STATUS.md:17`
- `docs/ops/CURRENT-STATUS.md:23`
- `docs/issues/OPEN-ISSUES.md:43`
- `docs/ops/PRD2-COMPLETION-PLAN.md:257`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:67`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:68`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:72`

**Original issue:**

The user reported Canary, Sentinel, and Atlas completed for Batch 19, and `main`/`origin/main` contains PR #51 at `5bbbf07`, but current docs still described Batch 19 as fixed locally and awaiting Canary.

**Current state:**

- `CURRENT-STATUS.md` records Batch 19 as Canary/Sentinel/Atlas complete per user report.
- `OPEN-ISSUES.md` marks `PRD2-B19-001` as `Canary passed`.
- `PRD2-ACCEPTANCE-MATRIX.md` moves P2-F08 Notification/event foundation and Project budget alerts to `E2E accepted`.
- Production webhook delivery evidence remains an explicit gap before `Production verified`.
- Batch 18 remains open/deferred and is not marked complete.

**Verification method:**

```bash
rg -n "PRD2-B19-001.*Fixed awaiting|Batch 19.*awaiting Canary|P2-F08 Notification/event foundation \\| Implemented not accepted|Project budget alerts \\| Implemented not accepted" \
  docs/ops/CURRENT-STATUS.md \
  docs/issues/OPEN-ISSUES.md \
  docs/ops/PRD2-COMPLETION-PLAN.md \
  docs/ops/PRD2-ACCEPTANCE-MATRIX.md

rg -n "PRD2-B19-001|Batch 19 PASS|PR #51|Production webhook delivery evidence" \
  docs/ops/CURRENT-STATUS.md \
  docs/issues/OPEN-ISSUES.md \
  docs/ops/PRD2-COMPLETION-PLAN.md \
  docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

Expected:

- First command returns no matches.
- Second command shows Batch 19 PASS/PR #51/current evidence and still shows production webhook evidence as a remaining gap.

---

## Fix #2: PRD2 architecture decisions to finalize are documented

**File:**

- `docs/ops/PRD2-COMPLETION-PLAN.md:30`
- `docs/ops/PRD2-COMPLETION-PLAN.md:31`
- `docs/ops/PRD2-COMPLETION-PLAN.md:60`

**Original issue:**

The user asked whether plugin support, OIDC/SSO, deployment docs, and development standards should be discussed now or delayed to PRD3. The repo did not yet state the recommended PRD2 boundary in one place.

**Current state:**

- PRD2 keeps core product features plus static code/config extension points.
- OIDC first SSO is documented as core authentication infrastructure, not an arbitrary runtime plugin.
- Runtime loading of third-party plugin code is not part of PRD2.
- Plugin-suitable PRD3 candidates are provider adapters, notification sinks, audit/SIEM exporters, usage/report exporters, model-catalog sync, status connectors, and error-doc connectors.
- Security/accounting boundaries remain core: OIDC/SSO, RBAC, API-key hashing, wallet/billing settlement, license verification, prompt/response storage policy, and runtime cache policy.

**Verification method:**

```bash
rg -n "Extension/plugin boundary|OIDC boundary|Architecture Decisions To Finalize|runtime loading of third-party code|Security/accounting boundaries stay core" \
  docs/ops/PRD2-COMPLETION-PLAN.md
```

Expected:

- Matches show the PRD2 boundary and the OIDC-as-core decision.

---

## Fix #3: PRD3/later unimplemented items are tracked

**Files:**

- `docs/issues/OPEN-ISSUES.md:44`
- `docs/issues/OPEN-ISSUES.md:45`
- `docs/issues/OPEN-ISSUES.md:46`
- `docs/ops/PRD2-COMPLETION-PLAN.md:391`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:100`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:101`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:102`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md:103`

**Original issue:**

The user asked that PRD3 topics not implemented now be recorded as unfinished work rather than lost in discussion.

**Current state:**

- `PRD3-PLUGIN-001` tracks the missing runtime plugin platform.
- `PRD3-DEPLOY-001` tracks missing enterprise deployment packaging and IaC.
- `PRD3-ENG-001` tracks missing full engineering handbook and release governance.
- The PRD2 acceptance matrix explicitly defers runtime plugins, plugin marketplace/third-party install, Kubernetes/Helm/Terraform packaging, and full engineering governance.

**Verification method:**

```bash
rg -n "PRD3-PLUGIN-001|PRD3-DEPLOY-001|PRD3-ENG-001|Runtime plugin platform|Plugin marketplace|Kubernetes / Helm / Terraform|Full engineering handbook" \
  docs/issues/OPEN-ISSUES.md \
  docs/ops/PRD2-COMPLETION-PLAN.md \
  docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

Expected:

- Matches show all three issue IDs and all deferred PRD3 categories.

---

## Global Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
git status --short
```

Expected:

- `git diff --check` exits 0.
- Only documentation files are modified.

---

## Expected Canary Output Format

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

1. PASS/FAIL for Fix #1 Batch 19 status sync.
2. PASS/FAIL for Fix #2 architecture-boundary documentation.
3. PASS/FAIL for Fix #3 PRD3/later unfinished item tracking.
4. Command results for the listed `rg` checks and `git diff --check`.
5. A conclusion stating whether the docs are self-consistent and whether this docs-only round can move to Sentinel if the user wants a PR.
