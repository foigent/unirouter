# PRD6 Batch 0C P6-F06 Multiplier-Aware Freeze Evidence

> Date: 2026-07-11
> Scope: UniRouter organization-multiplier reservation, settlement and cancellation invariants
> Risk: R3 tenant financial correctness
> Evidence level: CI verified / closed
> Deployment/target/production claim: none

## 1. Confirmed finding and repaired foundation

The original `P6-BILL-002` finding was valid: before P6-F18A, Freeze could
reserve from the base model price while settlement applied an organization
multiplier. A request with exact funds could therefore be accepted and later
produce multiplier-created uncovered cost.

Current source reconciliation shows that P6-F18A already removed that defect
mechanism:

- `queryPriceQuoteForOrg` reads base input/output prices and the organization
  multiplier together, then calculates effective prices before Freeze;
- Freeze calculates the hold from that effective quote and persists the
  effective input/output prices, multiplier and pricing version in the
  reservation;
- Settle calculates token cost from the immutable reservation snapshot rather
  than a later catalog value or caller-supplied amount;
- wallet debit, reservation and freeze ledger creation remain one transaction;
  settlement/cancellation remain single-terminal under the reservation row
  lock.

This is a confirmed current-code fact, not a target-runtime claim. P6-F06 does
not change the intentional general overrun rule: actual token use can exceed a
properly calculated hold for reasons unrelated to organization multiplier, and
that separate case remains explicitly recorded as `uncovered_cost`.

## 2. Missing acceptance evidence added

`internal/billing/billing_multiplier_freeze_integration_test.go` creates an
isolated database, applies every current migration, and fixes the model price
at 10 cents per 1,000 output tokens with a `2x` organization multiplier.

| Scenario | Expected invariant | Verified result |
|----------|--------------------|-----------------|
| 19-cent wallet, 20-cent effective hold | reject atomically | `insufficient_balance`; balance remains 19; frozen remains 0; no reservation or freeze ledger row |
| exact 20-cent wallet, 32 concurrent identical settlements | one terminal settlement with no multiplier loss | every caller reads the same receipt; one settle row; charged 20; `uncovered_cost=0`; wallet spent 20 and frozen 0 |
| exact 20-cent wallet, cancellation repeated | refund the multiplier-adjusted hold once | balance returns to 20; frozen/spent remain 0; one cancel terminal row |

The prior `TestP6F18HundredWaySettleCancelRaceHasOneTerminalEvent` remains the
stronger mixed Settle/Cancel race guard. The new P6-F06 test adds the exact
multiplier-specific cost assertions rather than duplicating or weakening that
foundation.

## 3. Required CI gate

The Backend required job now includes a separately named fail-closed step:

```text
REQUIRE_TEST_DATABASE=1 go test -race ./internal/billing \
  -run TestP6F06 -count=1
```

If PostgreSQL is unavailable, `REQUIRE_TEST_DATABASE=1` makes the step fail; a
skipped integration test cannot create a green P6-F06 result.

## 4. Local verification

```text
REQUIRE_TEST_DATABASE=1 go test -race ./internal/billing \
  -run TestP6F06 -count=10
```

Result: PASS in 9.082 seconds for ten repetitions, including 320 concurrent
settlement callers in aggregate. No race was reported.

```text
go vet ./...
go test -race ./... -count=1
```

Result: PASS for every package. The billing package passed in 9.701 seconds.
The known non-failing macOS `LC_DYSYMTAB` linker warning remains; it did not
change the test result and the race detector reported no race.

```text
npm run type-check
npm test -- --run
npm run build
```

Result: PASS for type-check, 27 files / 137 tests and production build. Existing
React/Semi and bundle-size warnings remain non-failing. Frontend source and
product behavior are unchanged; Docker build remains a required CI gate for the
exact PR head.

## 5. Required CI, merge and post-merge evidence

PR #160 exact head `7873cf24a16d7462bc588f0edd8b91a66a3c48f5`
passed required run `29161370623`: Backend Tests passed in 3m48s, Frontend
Build passed in 3m12s and Docker Build passed in 1m01s. Backend explicitly
passed the full race suite, isolated admin, routing-policy, budget-alert and new
multiplier-freeze PostgreSQL gates.

PR #160 was squash-merged as
`15ea8a27a0a514189c5af6f676af1e987f5c6763`. Post-merge main run
`29161527988` passed Backend Tests in 3m44s, Frontend Build in 3m14s and Docker
Build in 1m03s, including the P6-F06 PostgreSQL gate. The historical branch is
retained. The non-failing Node-version and Go-cache-restore annotations do not
change the successful job conclusions.

P6-F06 is CI verified / closed for the accepted repository scope. Neither run
promotes it to target or production verification.

This batch adds no pricing API, multiplier mutation, wallet formula, database
migration, provider call, response cache, deployment or target change. It does
not claim that arbitrary actual-usage overruns are impossible, and it does not
reopen P6-F18 or PRD5.
