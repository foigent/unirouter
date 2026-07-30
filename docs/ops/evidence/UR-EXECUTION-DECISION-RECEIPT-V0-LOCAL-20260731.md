# Execution Decision Receipt Workspace V0 — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRODUCT-ITERATION-RECEIPT-WORKSPACE-V0`
> Risk: R2 user-visible frontend behavior
> Status: Local verified
> Baseline: `48a88bccbe182a3d3a6aec315d422f47d879a364`
> (`origin/main`, tree `31b651fb1b2718a09efa9bd1c3a2abb244ebb44e`)
> Delivery branch: `codex/product-iteration-20260730`

## Accepted outcome

The Requests/Logs page is now an execution-decision receipt workspace instead
of a table that requires a second click to reveal a dismissible detail panel.
The newest record in the server's descending `created_at` result is selected by
default, the right inspector remains present, and `ArrowUp`/`ArrowDown` or a
`log_id` URL selects another record.

The receipt presents five bounded views:

1. `Decision` — execution result, authority/delegation and captured decisions.
2. `Attempts` — provider attempts, failover, status and performance.
3. `Policy & Guardrail` — only captured policy/guardrail evidence.
4. `Cost & Finality` — billing terminal evidence, exact retained micro-CNY
   values, the current sanitized-contract gap and provider-reported Prompt
   Cache usage.
5. `Provenance & Gaps` — trace identity, source evidence, capture modes,
   declared gaps and the event path.

## Evidence and non-inference rules

- The existing `GET /api/v1/logs/{id}/trace` response remains the data source.
  No backend endpoint, DTO, database schema, billing, routing, IAM, feed,
  outbox, cursor or protocol behavior changed.
- `billing.settle.succeeded`, `billing.settle.pending_recovery`,
  `billing.settlement_usage.failed` and
  `billing.cancelled.no_billable_output` are the only events interpreted as
  billing terminal evidence in this V0 view. Cancellation preserves the exact
  allowlisted `billing_status`, including cancellation failure.
- HTTP success and `call_logs.cost_micros` never imply `settled`. When no
  billing terminal event is captured, the receipt says `not captured`.
- The current trace sanitizer retains `actual_cost_micros`,
  `charged_cost_micros`, `billing_status`, `usage_source`,
  `usage_confidence`, `money_schema` and `pricing_version`. It does not retain
  `usage_authority`, `usage_finality` or `usage_purpose`; the UI states that
  fixed gap instead of using impossible fixtures or changing trace/hash
  semantics in this frontend slice.
- Missing actor, policy, route or gap evidence is shown as missing/undeclared;
  it is not converted into a negative claim.
- Trace loading, Trace failure and log-list failure are separate states. A
  failed refresh clears stale list/receipt data and offers a bounded retry.
- Zero declared capture gaps does not claim complete capture across products,
  the Reference Connector or external systems.
- Request and response bodies remain outside this view.
- Table columns are reference-stable across automatic trace-loading renders;
  this prevents the Semi Table nested-update failure found by the real-browser
  check.

## Files

- `frontend/src/pages/LogsPage.tsx`
- `frontend/src/index.css`
- `frontend/src/pages/__tests__/LogsPage.test.tsx`
- `docs/design/pages/logs-explorer-v2.md`
- `docs/design/README.md`
- `docs/design/spec-v1.md`
- `docs/agent-workflows/EXECUTION-RECORD.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/evidence/UR-EXECUTION-DECISION-RECEIPT-V0-LOCAL-20260731.md`

## Local verification

Commands were run from `frontend/` unless noted:

| Command | Actual result |
|---------|---------------|
| `npm test -- src/pages/__tests__/LogsPage.test.tsx --reporter=dot --silent=passed-only` | PASS — 1 file, 24 tests |
| `npm run type-check` | PASS |
| `npm test -- --reporter=dot --silent=passed-only` | PASS — 41 files, 299 tests |
| `npm run build` | PASS — TypeScript and Vite production build |
| `go test ./internal/traceutil ./internal/admin -count=1` | PASS — rerun with loopback permission after the sandbox correctly blocked `httptest` listen |
| Local Playwright mock at 1440×1000, 1024×900 and 390×844 | PASS — receipt is visible without a detail-open click; medium keeps list/receipt side by side; mobile stacks and keeps the receipt reachable; Cost & Finality renders exact retained fields and the explicit contract gap |
| `git diff --check` (repository root) | PASS |

The build retains the pre-existing Vite large-chunk warning. The full test
suite and local browser retain pre-existing Semi UI/React Router
`findDOMNode`/future-flag warning noise; neither produced a product runtime
error after the table-column fix. Browser screenshots are ignored local
artifacts under `output/playwright/`.

## Evidence boundary

This is local source, DOM-test, production-build and static-mock browser
evidence. It is not CI verified, a real-data/runtime acceptance, Target
verified or Production verified. No deployment, traffic, provider request,
credentials, database, Cursor/Checkpoint, X-Insight, Reference Connector or
protocol action occurred. The work neither reopens PRD5/PRD6 nor authorizes
another PRD7 candidate.
