# V1 Micro-Precision Ledger Plan — 2026-07-14

## Decision and classification

- Decision: User selected option B, a source-preserving micro-precision ledger,
  on 2026-07-14.
- Issue: `V1-FIN-001`.
- Classification: confirmed bug/regression; release-blocking financial
  correctness defect, not a new product feature.
- Priority: P0 / Critical before the first formal V1 release.
- Owner: UniRouter billing, wallet, control-plane API and frontend. X-Insight has
  no synchronous authority or implementation dependency for this correction.
- PRD boundary: PRD6 remains `Completed (Accepted Scope)`. This V1 remediation
  does not reopen any accepted PRD6 row or promote target/production evidence.

## Verified defect

`internal/billing/billing.go` currently calculates customer cost with binary
floating-point arithmetic, converts the result to an integer number of cents
and clamps every result below one cent to one cent. The reservation settlement
path repeats the same terminal clamp. Existing unit and integration tests
explicitly require both a sub-cent call and a zero-token call to cost one cent.

For a configured input rate of `0.2` CNY cents per 1,000 tokens, one input token
has an exact source cost of `0.0002` cent, or `2` micro-CNY. The current path
charges `1` cent, or `10,000` micro-CNY: 5,000 times the configured amount. A
zero-token calculation is also clamped from zero to one cent.

The problem is system-wide rather than display-only. Integer-cent terminal
amounts flow through wallets, transactions, grants, reservations, settlement
and recovery, usage journals, project budget usage, provider-attempt evidence,
runtime terminal hooks, Agent Redis counters, outbox events, call logs and
frontend/API summaries. Decimal price storage alone cannot recover precision
after the terminal amount has already been truncated and clamped.

## Accepted money contract

### Canonical units

1. Canonical ledger amount: signed `int64` micro-CNY.
2. `1 CNY = 1,000,000 micro-CNY`.
3. `1 cent = 10,000 micro-CNY`.
4. Public model-price configuration remains CNY per 1,000,000 tokens, as
   established by PR #197. Existing decimal database price columns may remain
   an internal compatibility source, but a reservation must freeze integer
   `*_price_micros_per_million` snapshots before any money calculation.
5. Customer balances and all terminal/reserved money amounts use explicit
   `*_micros` names. No unqualified `amount`, `cost` or `balance` field may
   silently change unit.

### Calculation and rounding

1. No customer money calculation may use `float32` or `float64`.
2. Exact component numerators are `tokens * price_micros_per_million`; checked
   integer/rational arithmetic must reject overflow instead of wrapping.
3. The authoritative terminal total is the sum of exact component numerators,
   rounded once to the nearest micro-CNY using half-up rounding.
4. There is no minimum terminal charge. Zero authoritative usage costs zero;
   one token at `2 CNY / 1M` costs exactly `2` micro-CNY.
5. Component displays and persisted breakdowns must sum exactly to the rounded
   terminal total. Allocate any rounding remainder deterministically by largest
   fractional remainder, with a fixed component tie order.
6. Reservation estimates are conservative holds, not charges. The hold uses
   exact price arithmetic, a decimal/rational safety factor and ceiling to the
   next micro-CNY, then applies the configured minimum hold after converting
   that cents setting exactly to micros. Unused hold is refunded.
7. `MIN_FREEZE_AMOUNT` and `FREEZE_SAFETY_FACTOR` retain their current default
   policy values. The implementation must wire those configured values into the
   active relay path; changing the policy itself needs a separate decision.

### API and UI contract

1. Canonical monetary outputs are explicit `*_micros` decimal strings plus
   `currency: "CNY"` and `amount_scale: 1000000`. Strings prevent JavaScript
   `Number` precision loss beyond `2^53 - 1`.
2. The frontend parses monetary strings with `BigInt` and formats up to six
   fractional CNY digits without converting through `Number`.
3. Exact configuration inputs that are intentionally in cents may remain so:
   wallet top-up/adjustment, grants, project/key budget ceilings and low-balance
   thresholds. They are converted to micros with checked multiplication at the
   boundary and must stay visibly named `*_cents`.
4. Computed cost, balance, reservation and settlement outputs do not retain a
   rounded integer-cent field as a second authority. If a compatibility display
   is needed, it is derived and labelled non-authoritative.
5. OpenAPI, client types, UI labels, CSV/export fields, audit details and trace
   metadata change together; ambiguous legacy names are not reinterpreted.

## Persistence and compatibility scope

Migration `000036` is reserved for this change after PR #197 supplies
`000035`. It is a non-rolling, fail-closed migration. The activation scope
includes:

1. `wallets`: balance, granted balance, frozen balance and total spent.
2. `transactions`: amount and balance-after.
3. `grants`: amount and remaining balance.
4. `call_logs`: terminal cost, immutable effective price snapshots and cache
   cost breakdown.
5. `freeze_idempotency`: frozen/reserved cash and grant amounts, minimum hold,
   calculated/charged/uncovered/top-up/refund/forfeit/balance-after values,
   effective price snapshots and component costs.
6. `billing_reservation_grant_allocations`: reserved, consumed and refunded
   amounts.
7. `usage_finalization_journal`: calculated and charged amounts.
8. `project_budget_usage`: materialized spend. Project budget configuration
   remains an explicitly named cents input.
9. `runtime_terminal_hooks`: actual terminal cost.
10. `billing_provider_attempts`: calculated and charged customer cost.
11. `billing_reservation_extensions`: total, cash and grant reservation values.
12. Backend models, wallet/billing/gateway/capability/policy wrappers, recovery,
    provider reconciliation, admin handlers, outbox payloads, trace evidence,
    OpenAPI, frontend clients and all affected tests.

Canonical integer-cent columns are renamed to explicit `*_micros`, widened to
`BIGINT` and multiplied by `10,000`. Decimal effective price snapshots are
converted to integer micro-CNY per million tokens. Existing decimal component
cost evidence is converted to micro-CNY with the same declared half-up rule.
Old binaries must fail on renamed columns rather than writing a mixed unit.

### Durable outbox

- New billing/call-log payloads and event types use a v2 schema with explicit
  micro fields.
- Workers register both versions during the upgrade window. A pending v1 event
  is decoded as cents and converted exactly by multiplying by `10,000`; it is
  never reinterpreted as micros.
- v2 evidence is not downgraded into a rounded v1 customer amount.

### Agent Redis authority

- Runtime keys move from `ur:agentcap:v1` and `cost_*_cents` to an isolated v2
  namespace with `cost_*_micros`.
- Public `cost_budget_cents` capability input may remain for compatibility, but
  exchange/delegation converts it exactly to micros and rejects values that
  would exceed Redis Lua's exact integer range.
- Capability schema/signing authority is versioned so an old token cannot write
  a v1 counter after v2 activation. Old v1 keys expire naturally; no bulk Redis
  deletion is part of the rollout.

## Migration and rollback gates

Before `000036` up:

1. drain all old data-plane and control-plane binaries;
2. refuse activation while active reservations/provider attempts or pending
   terminal hooks could be written by an old process;
3. back up the database and record row counts plus monetary checksums by table;
4. verify the exact application image and migration sequence, including
   `000035`; and
5. keep target/production execution behind an explicit User approval gate.

After up, verify row counts, converted sums, constraints, one deterministic
micro-charge request fixture, one zero-use fixture, cash/grant settlement and
outbox/Redis v2 readback before reopening traffic.

Down migration is allowed only when it is lossless. It refuses if any canonical
amount is not divisible by `10,000`, any effective price cannot be represented
exactly in the prior decimal scale, or v2-only outbox/runtime evidence exists.
Once real micro-CNY terminal evidence is written, rollback is roll-forward by
default; it must not round customer balances back to cents.

## Implementation order

1. Merge the independent V1 wallet atomicity correction in PR #196.
2. Merge the independent public pricing-unit/fail-closed correction in PR #197.
3. Add an `internal/money` checked arithmetic kernel and red/green unit tests
   without changing runtime behavior.
4. Implement `000036`, exact price snapshots and the atomic billing/wallet/
   recovery cutover on one branch.
5. Upgrade outbox, Agent Redis, provider reconciliation, traces, APIs, OpenAPI
   and frontend formatting in the same activation contract.
6. Pass focused money/migration tests, full Go and race suites, frontend tests/
   build, Docker build and all required GitHub checks.
7. Stop for User approval before merging/deploying the R3 non-rolling migration
   or mutating any target database.

## Required acceptance evidence

- Exact arithmetic, rounding, overflow and component-conservation unit tests.
- `2`-micro one-token charge and zero-token/zero-price zero-charge tests.
- Cash/grant freeze, top-up, settle, refund, cancel and replay invariants in a
  migrated PostgreSQL database under concurrency.
- `000036` up, guarded down, refusal and roll-forward tests with non-divisible
  micro evidence.
- v1 outbox compatibility and v2 idempotent materialization tests.
- Project/key/Agent budget threshold crossing at sub-cent precision.
- Provider-attempt, reconciliation and terminal-recovery exactness.
- Redis v2 lineage reservation/settlement/extension exactness at the declared
  numeric boundary.
- OpenAPI/client/frontend string-micro contract and six-decimal formatting.
- Required GitHub Backend Tests, Frontend Build and Docker Build on the exact
  implementation head.

No target, production, live-provider or migration safety claim is made by this
plan. Those evidence levels require separate authorized execution.
