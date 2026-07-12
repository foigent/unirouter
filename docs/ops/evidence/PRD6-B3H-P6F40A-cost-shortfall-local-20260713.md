# PRD6 Batch 3H P6-F40A Cost Shortfall Local Evidence

> Date: 2026-07-13
> Baseline: `f1e08e6a793cb2ea0580d256fe75bd2d3d798c3f`
> Branch: `codex/prd6-cost-shortfall`
> Evidence level: Local verified; required CI pending
> Risk: R3 billing, wallet, settlement recovery and migration safety

## Accepted Boundary

D6-13 keeps financial control inside the current UniRouter modular monolith.
A reservation is an admission hold, not a terminal charge ceiling. Known actual
cost above the hold may consume existing prepaid cash atomically but may not
overdraft, create automatic debt or become a silent platform subsidy. If cash
is insufficient, the operation remains non-terminal and non-cancellable with
durable usage and provider-cost-pending evidence. Raw response retention,
deployment, real provider calls and a separate Cost Control Service are not in
this slice.

P6-F40A is the first independently reviewable slice. Provider-attempt identity,
ambiguous retry, affected-key admission blocking and operator reconciliation
remain P6-F40B/P6-F40C and are not claimed complete here.

## Pre-Fix Reproduction

The real migrated-PostgreSQL billing boundary reproduced both branches before
the patch:

```text
freeze=100 actual=500 charged=100 uncovered=400 status=settled
```

`TestP6F40SettlementShortfallUsesAvailablePrepaidCash` failed because the
terminal charge was 100 instead of 500. The insufficient-cash control also
failed because the reservation became terminal instead of remaining pending.

Command:

```bash
go test ./internal/billing -run '^TestP6F40SettlementShortfall' -count=1 -v
```

Pre-fix result: `FAIL` on both tests at the baseline revision.

## Implemented Behavior

- `Billing.Settle` calculates the complete known cost and derives a cash-only
  top-up after the original grant/cash reservation source split.
- The wallet row is locked once; available cash is consumed in the same
  transaction as the single terminal settlement. Successful terminal rows now
  have `charged_cost=calculated_cost`, `uncovered_cost=0` and a durable
  `topup_amount`.
- Insufficient cash stores the immutable settlement payload, calculated cost
  and shortfall under `settlement_pending` with
  `provider_cost_pending_at`/`insufficient_topup`. It creates no settle/cancel
  terminal transaction and ordinary cancellation returns
  `ErrSettlementPending`.
- Funding-pending rows are excluded from the existing automatic stream
  recovery queue so an undelivered non-stream response is not charged later by
  an unrelated worker policy.
- Gateway settlement recognizes the typed funding-pending result and does not
  invoke the ordinary cancellation fallback.
- Successful Chat, Embeddings and stream receipts expose
  `_uniroute.cost.prepaid_topup`; trace settlement metadata exposes
  `prepaid_topup_cents`.
- Migration `000028` distinguishes provider-cost pending from billable output,
  preserves the old stream recovery index semantics and refuses destructive
  down migration after pending/top-up evidence exists.

Migration `000028` is additive for schema reads, but an old binary does not
understand the new funding-pending state and can still execute the former
post-dispatch cancellation path. A future target rollout must drain old
binaries before activating the new writer; this local slice does not claim a
mixed-version or target cutover.

## Focused Verification

```bash
go test ./internal/billing -run '^TestP6F40' -count=1 -v
go test ./internal/gateway -run '^TestP6F40' -count=1 -v
go test -tags=integration ./internal/migrate \
  -run '^TestP6F40ProviderCostPendingMigration' -count=1 -v
go test ./internal/billing ./internal/gateway -count=1
```

Current result: all commands above `PASS`.

The billing proof includes:

- available cash: 100 hold + 400 atomic top-up = 500 charged, zero uncovered;
- 32 concurrent settlement callers: one terminal transaction and one 500-cent
  charge;
- insufficient cash: balance 0, frozen 100, spent 0, no terminal transaction,
  non-cancellable funding-pending state; and
- automatic recovery claims zero funding-pending rows.

## Full Local Verification

```bash
go test ./... -count=1
go vet ./...
go test -race ./internal/billing ./internal/gateway -run TestP6F40 -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate \
  -run TestP6F40ProviderCostPendingMigrationUpDownGuardAndRollForward -count=1
go test ./cmd/uniroute \
  -run 'Test(OpenAPISpec|FrontendDocs|BackendDocs|AdminRoutes)' -count=1
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Current result: all commands `PASS`; frontend reports 29 files / 144 tests.
The build retains its existing large-chunk warning and the macOS race binaries
emit a linker metadata warning; neither command failed.

## Remaining Gates

- PR `Backend Tests`, `Frontend Build` and `Docker Build`;
- P6-F40B/P6-F40C and P6-F41 remain separate unclosed work.

No target, production, paid-provider, credential or external-integration claim
is made.
