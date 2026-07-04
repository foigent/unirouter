# PRD3 Batch 2 Usage And Status Semantics Local Evidence

> Date: 2026-07-05
> Scope: P3-F01 Product Glossary, P3-F05 Usage Analytics Semantics, P3-F06 Internal Status Semantics
> Status: Codex local validation passed; awaiting Canary.

## Summary

PRD3 Batch 2 clarifies existing product semantics without adding PRD4 capabilities.

Implemented changes:

- Added `docs/tech-ref/20-prd3-product-glossary.md` as the canonical glossary for provider/vendor/supplier, upstream channel, access credential, upstream credential, model, alias, usage source, audit source, incident, event status, delivery state, internal status, maintenance window, and role labels.
- Usage Analytics now has an explicit `总览（不分组）` grouping:
  - backend accepts `group_by=overall` and returns one aggregate row labeled `全部请求`;
  - frontend defaults to the overall grouping so "all" is visible instead of implicit in KPI cards;
  - provider and upstream channel definitions are visible in the filter panel;
  - prompt/response retention and CSV filtered-query scope remain visible.
- Internal Status now states that the page is event-derived:
  - current status is derived from open critical/warning notification events;
  - event status uses `open`, `acknowledged`, `resolved`;
  - severity and delivery state are separate concepts;
  - maintenance window scheduling is marked as not enabled and remains PRD4.

## Files Changed

- `docs/tech-ref/20-prd3-product-glossary.md`
- `docs/tech-ref/README.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `internal/admin/prd2_product_handlers.go`
- `internal/admin/prd2_product_handlers_test.go`
- `frontend/src/utils/displayLabels.ts`
- `frontend/src/pages/UsageAnalyticsPage.tsx`
- `frontend/src/pages/InternalStatusPage.tsx`
- `frontend/src/pages/__tests__/UsageAnalyticsPage.test.tsx`
- `frontend/src/pages/__tests__/InternalStatusPage.test.tsx`

## Verification

```bash
go test ./internal/admin -run 'TestUsageAnalyticsGroup|TestInternalStatusLevel|TestUsageAnalyticsUpstreamSourceSQLIncludesNull' -count=1
npm --prefix frontend test -- --run UsageAnalyticsPage InternalStatusPage
go test ./internal/admin -count=1
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Results:

- Targeted backend PRD2 product handler tests: PASS.
- Targeted Usage Analytics and Internal Status frontend tests: PASS, 2 files / 2 tests.
- Full `internal/admin` backend tests: PASS.
- Full Go tests: PASS.
- Full frontend tests: PASS, 16 files / 92 tests.
- Frontend build: PASS, with the existing Vite large chunk warning only.
- `git diff --check`: PASS.

## Boundaries

- No settlement, reconciliation, wallet, billing, or `usage_source` accounting logic was changed.
- No maintenance-window scheduling, SLA/SLO, upgrade communication, or incident-operations workflow was implemented.
- No runtime plugin, cache, budget hard-block, enterprise IaC, or new persistent role capability was implemented.
- Internal Status still uses the existing `notification_events` source and existing event lifecycle.
