# PRD2 Batch 28 Product Consistency Local Evidence

Date: 2026-07-04

## Scope

Batch 28 is a post-closeout product consistency and operability polish round. It does not reopen PRD2 feature acceptance and does not rewrite billing, settlement, usage calculation, or reconciliation semantics.

Implemented changes:

- Added shared frontend display mappings in `frontend/src/utils/displayLabels.ts` for usage sources, usage analytics groups, request status, audit sources, notification event types, severity, delivery state, and internal status.
- Updated Usage Analytics to expose the backend-supported filter surface: `project_id`, `key_id`, `model`, `alias`, `provider`, `channel`, `status`, `usage_source`, `start_time`, and `end_time`.
- Kept API filter values stable while changing visible labels. Example: UI shows `流式计数`, but still sends `counter_v2`.
- Localized usage-source display in Request Logs and Reconciliation without changing the existing filtering contract.
- Localized audit source, notification event type/severity/delivery state, and internal status displays.
- Replaced remaining visible product terminology on touched enterprise surfaces: access credentials, upstream credentials, request debugger, request lookup, API docs, channel/key page eyebrows, and related descriptions.
- Updated `internal/admin/prd2_product_handlers.go` group labels for Usage Analytics output/CSV from English display labels to Chinese display labels. The `group_by.key` values and SQL whitelist expressions are unchanged.

## Accounting And Compatibility Boundary

No changes were made to:

- `internal/metering.NormalizeUsageSource`
- billing settlement paths
- stream/non-stream token calculation
- reconciliation query semantics
- gateway relay behavior
- API route contracts

The existing compatibility rules remain in place:

- legacy `api` is still normalized to `upstream` where existing backend logic normalizes sources;
- legacy `stream_estimate` is still normalized to `counter_v2`;
- reconciliation and logs still filter by the stable source values, not by display labels.

## Verification

Commands run locally:

```bash
npm --prefix frontend test -- --run
npm --prefix frontend run build
go test ./internal/admin -count=1
go test ./... -count=1
git diff --check
```

Results:

- Frontend tests: PASS, 16 files / 88 tests.
- Frontend build: PASS, existing Vite large chunk warning only.
- `go test ./internal/admin -count=1`: PASS.
- `go test ./... -count=1`: PASS.
- `git diff --check`: PASS.

## Notes For Canary

- Please treat Batch 27 as already Canary PASS per `docs/agent-workflows/VERIFY-RESULT.md` and the user's report that Canary/Sentinel/Atlas completed.
- Batch 28 should be verified as a new post-closeout polish round.
- Expected focus: display labels, filter parity, stable API values, and no billing/accounting rewrite.
