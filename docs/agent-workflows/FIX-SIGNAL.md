# FIX-SIGNAL.md — PRD3 Batch 2 Verification Request

**日期：** 2026-07-05
**状态：** Fixed awaiting Canary

PRD3 Batch 1 passed Canary 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #63 at `9bed3be`; User reported Canary, Sentinel, and Atlas completed.

Codex has completed PRD3 Batch 2 for product glossary, Usage Analytics semantics, and Internal Status semantics.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- This batch does not implement runtime plugins, plugin marketplace, enterprise IaC/deployment expansion, project budget hard-blocking, runtime cache, new persistent roles, maintenance scheduling, SLA/SLO, upgrade communication, or an incident-operations platform.
- Usage Analytics changes must not rewrite settlement, reconciliation, wallet, billing, or `usage_source` accounting semantics.
- Internal Status must remain event-derived from existing `notification_events`; event lifecycle remains `open`, `acknowledged`, `resolved`.

## Verification Request

Please verify:

1. Product glossary:
   - `docs/tech-ref/20-prd3-product-glossary.md` defines provider/vendor/supplier, upstream channel, access credential, upstream credential, model, alias, usage source, audit source, incident, event status, severity, delivery state, internal status, maintenance window, and role terms.
   - `docs/tech-ref/README.md`, `docs/DOC-INVENTORY.md`, and `docs/README.md` index the glossary.
   - The glossary keeps runtime plugins, maintenance scheduling, SLA/SLO, budget hard-blocking, cache, enterprise IaC, and new authority surfaces in PRD4/later boundaries.
2. Usage Analytics semantics:
   - `internal/admin/prd2_product_handlers.go` accepts `group_by=overall` and returns a one-row aggregate labeled `全部请求`.
   - `frontend/src/pages/UsageAnalyticsPage.tsx` defaults to `总览（不分组）` and visibly defines provider as provider family/adapter dimension and upstream channel as a concrete configured route instance.
   - Existing filters and stable `usage_source` values remain compatible.
3. Internal Status semantics:
   - `frontend/src/pages/InternalStatusPage.tsx` explains the page is derived from notification events.
   - Open critical and open warning counters are explicitly labeled.
   - Event lifecycle labels use `未关闭 / 已确认 / 已解决`.
   - Webhook delivery state remains separate from event status.
   - Maintenance window scheduling is marked as not enabled/PRD4, not implemented as a new system.
4. Evidence and status docs are updated:
   - `docs/ops/evidence/PRD3-B02-usage-status-semantics-local-20260705.md`
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
   - `docs/issues/OPEN-ISSUES.md`
   - `docs/ops/CURRENT-STATUS.md`
5. Run these commands:

```bash
go test ./internal/admin -run 'TestUsageAnalyticsGroup|TestInternalStatusLevel|TestUsageAnalyticsUpstreamSourceSQLIncludesNull' -count=1
npm --prefix frontend test -- --run UsageAnalyticsPage InternalStatusPage
go test ./internal/admin -count=1
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Expected Result

- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3 Batch 2 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `docs/agent-workflows/VERIFY-RESULT.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B02-usage-status-semantics-local-20260705.md`
