# FIX-SIGNAL.md - PRD4 Batch 1N Developer Console Self Usage And Model Cost

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R2 user-visible frontend/product semantics
**Scope:** P4-F02 Developer Console self usage and model cost visibility

## Verification Request

Please verify PRD4 Batch 1N: Developer Console Self Usage And Model Cost.

This batch extends the accepted `P4-F02` usage/cost attribution foundation with
the concrete self-service view tracked as `PRD4-USAGE-002`:

- Developer Console now loads `GET /api/v1/usage/analytics` with
  `scope=my`, `project_id`, and `group_by=alias` for the selected project;
- the top KPI tiles use the current user's owned API-key usage aggregate when
  available, instead of summing only the latest 20 project log rows;
- a new "我的模型用量" table shows the current user's model/alias breakdown for
  requests, success/failure count, tokens, cost, and average latency;
- the existing project keys, approved models, playground, recent calls, and
  request lookup workflows remain unchanged;
- historical rows with no owner attribution remain excluded from `scope=my`
  instead of being fabricated.

## Scope To Verify

Product/test files:

- `frontend/src/pages/DeveloperConsolePage.tsx`
- `frontend/src/pages/__tests__/DeveloperConsolePage.test.tsx`

Status/docs files:

- `docs/agent-workflows/FIX-SIGNAL.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `docs/PRD-Phase4.md`

## Expected Behavior

- A developer or project member can see their own current project usage and
  corresponding cost directly on Developer Console.
- Model-level cost visibility is grouped by model alias/requested model through
  the existing Usage Analytics API.
- KPI totals are owner-attributed when the analytics response is available.
- If the self-usage analytics request fails, Developer Console should still
  render project keys, approved models, recent calls, playground, and lookup
  flows, falling back to the existing recent-log KPI snapshot.
- The recent-call table remains project-log based and still shows recent request
  detail for operational debugging.
- No prompt/response plaintext, full API keys, upstream credentials, or hidden
  log payloads are added to the page.

## Non-Scope

Do not expect this batch to:

- change backend usage analytics SQL, owner attribution semantics, billing cost
  calculation, wallet settlement, API key ownership rules, RBAC, or routes;
- add CSV export to Developer Console;
- add budget hard-block enforcement, Scheduler V2, TPM enforcement, new roles,
  new persistent owner backfill, or prompt/response plaintext storage;
- change Usage Analytics page behavior outside the existing API call reuse;
- claim target-environment `Production verified` evidence.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "我的模型用量|scope: 'my'|group_by: 'alias'|PRD4-USAGE-002|P4-F02|Batch 1N" frontend/src/pages/DeveloperConsolePage.tsx frontend/src/pages/__tests__/DeveloperConsolePage.test.tsx docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/PRD-Phase4.md

go test ./internal/admin -run 'TestAnalyticsConditionsMyScope|TestAnalyticsConditionsRejectsConflictingOwnerScope' -count=1
npm --prefix frontend test -- --run DeveloperConsolePage
npm --prefix frontend test -- --run UsageAnalyticsPage client
npm --prefix frontend test -- --run
npm --prefix frontend run build

git diff --check
```

## Acceptance Criteria

1. Developer Console requests usage analytics with `scope=my`, selected
   `project_id`, and `group_by=alias`.
2. KPI request/token/cost/error-rate values use the self-owned aggregate when
   usage analytics returns a summary.
3. The new model usage table displays owner-attributed model/alias breakdown
   including requests, success/failure, tokens, cost, and latency.
4. Self-usage analytics failure does not block existing Developer Console
   project data from rendering.
5. Existing Developer Console key/model/playground/log lookup flows still pass.
6. Usage Analytics client/page regressions still pass.
7. Existing backend `scope=my` conditions still reject conflicting owner filters.
8. No backend route, migration, billing/wallet settlement, API key ownership,
   RBAC, prompt/response storage, trace, runtime status, SSO, upstream, or
   provider-dispatch behavior changes.
9. Focused frontend tests, full frontend tests, frontend build, focused backend
   analytics tests, and `git diff --check` pass.
