# PRD6 Batch 0B — P6-F18A Billing Reservation Invariants

> Date: 2026-07-11
> Evidence level: Local verified on `codex/prd6-p6f18a-billing-invariants`; required PR CI pending
> Target/production status: Not deployed; no target or production verification claimed

## 1. Accepted Scope Covered Here

P6-F18 is intentionally split because its database-reservation foundation and
its post-output crash recovery have different failure surfaces.

This F18A batch covers:

- resolve the effective organization price before opening the wallet
  transaction, removing connection-pool re-entry;
- persist the effective input/output price, multiplier and immutable pricing
  version on the reservation;
- define `balance` and `granted_balance` as remaining spendable buckets after a
  hold, with `frozen` as a held-funds control total rather than a second
  deduction;
- reserve grants before cash, retain the exact source split, and refund only to
  the original source;
- decrement grant detail rows in earliest-expiry order and expire aggregate
  credit atomically with its detail rows;
- serialize the same tenant-operation identity, row-lock terminal transitions,
  and enforce one `settle|cancel` ledger event;
- make duplicate terminal calls return/no-op from the durable receipt rather
  than applying a second financial effect;
- align the wallet UI with the debit-on-hold contract.

This batch does **not** close the remainder of P6-F18:

- no durable pre-first-chunk `settlement_pending` marker yet;
- no persisted post-stream usage payload or restart recovery worker yet;
- the relay still cancels after exhausted settlement retries, so F18B must
  replace that behavior for billable/delivered output;
- no process-kill-after-last-chunk evidence is claimed;
- no P6-F20 general usage journal/outbox is claimed.

## 2. Confirmed Pre-Change Defects

1. `Freeze` began a PostgreSQL transaction and then queried price through the
   pool, so a one-connection pool could deadlock.
2. The wallet was debited at freeze time, but availability subtracted `frozen`
   again.
3. A grant-funded hold was cancelled into cash, silently changing the funding
   type.
4. `grants.balance` was not reduced when aggregate `granted_balance` was used;
   expiry cleanup could therefore deduct the same grant again.
5. `CreateGrant` added a grant to cash and left the grant detail balance at its
   default zero, contradicting the frozen PRD1 grant contract.
6. Settle and Cancel read an unlocked `frozen` row and could race into multiple
   terminal wallet/ledger effects.
7. Settlement recalculated against the live price catalog instead of the price
   accepted at reservation time.
8. Grant expiry updated the wallet and grant rows in separate autocommit
   statements, allowing a crash between them to make the next pass deduct the
   same grant again.

These are source-confirmed repository defects, not claims about a production
incident.

## 3. Implemented Invariants

### 3.1 Reservation and pricing

- `Freeze` resolves one effective price quote before `Begin`.
- `pricing_version` is a bounded `pv1_` SHA-256 identity over model, base price,
  multiplier, source and price-row versions.
- Settlement uses the stored effective prices whenever token usage is present;
  later catalog or multiplier edits cannot reprice the accepted operation.
- Same-operation concurrency uses a transaction-scoped advisory identity lock
  before reading the receipt. This handles the exact-funds case where a
  duplicate would otherwise observe zero remaining balance after waiting.

### 3.2 Wallet and grant conservation

- Freeze debits `granted_balance` first and then `balance`; `frozen` increases
  by the same total.
- `reserved_granted_amount + reserved_balance_amount = frozen_amount` is a
  database constraint.
- Active grant detail rows are reserved by nearest expiry and recorded in
  `billing_reservation_grant_allocations`.
- Settlement consumes reserved grants first. Unused cash and valid grant credit
  return to their original buckets.
- Expired credit held across the expiry boundary is recorded as forfeited and
  is never converted into cash.
- Scheduled and request-path grant expiry use wallet-first lock ordering and an
  atomic per-organization transaction.
- UI “current available” is `balance + granted_balance`; “managed funds” adds
  the separately displayed held amount only for explanation/progress.

### 3.3 Single terminal

- `freeze_idempotency` is locked `FOR UPDATE` before Settle/Cancel decisions.
- A partial unique index permits at most one transaction of type `settle` or
  `cancel` for `(org_id, request_id)`.
- The reservation stores terminal transaction ID, calculated/charged/uncovered
  cost, refund, balance-after and terminal timestamp.
- Repeated Settle returns the stored receipt; repeated Cancel or the losing
  mixed terminal operation has no second effect.
- `settlement_pending` is already non-cancellable in the state-machine helper,
  but F18B is responsible for creating and recovering that state after
  billable output.

## 4. Migration Boundary

Migration `000022_prd6_billing_reservation_invariants`:

- backfills historical reservations as cash-backed because the old schema did
  not preserve their source and no safe inference exists;
- fails closed if duplicate historical terminal ledger events exist;
- adds source, price, usage, terminal and grant-allocation evidence;
- refuses the manual down migration after any new-writer reservation evidence
  exists, preventing loss of source allocations or accepted price receipts;
- passes clean up, down and roll-forward verification.

This down path is not authorization to restart an old writer. A deployment
would require old-writer drain and non-rolling schema/application activation;
none occurred in this batch.

## 5. Local Verification

All commands ran from the repository unless a frontend working directory is
shown.

| Gate | Result |
|------|--------|
| `go test ./... -count=1` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./... -count=1` | PASS; known non-fatal macOS linker `LC_DYSYMTAB` warnings only |
| P6-F18 migrated tests | PASS: one-connection pool, exact-funds duplicate, price/multiplier pin, source refund, expired grant, 100-way mixed terminal race |
| migration up/down/roll-forward/down-refusal test | PASS |
| `frontend: npm test -- --run` | PASS: 27 files / 137 tests |
| `frontend: npm run type-check` | PASS |
| `frontend: npm run build` | PASS; existing bundle-size warning only |
| `git diff --check` | PASS |

The local host has no `docker` CLI (`command not found`), so no local image
claim is made. The repository's required `Docker Build` GitHub job remains a PR
merge gate.

The migrated P6-F18 tests are untagged so the required Backend Tests job runs
them against PostgreSQL. The legacy shared-database tagged suite remains the
separate recorded `P6-TEST-DB-001` engineering-integrity item.

## 6. Evidence Boundary And Next Batch

Current result is **Local verified F18A**, not CI, target or production
verified. P6-F18 remains open until F18B proves:

1. a durable billable-output marker before client-visible streaming output;
2. settlement payload persistence after the last chunk;
3. no automatic refund after a post-output settlement failure;
4. bounded restart recovery to exactly one settlement;
5. post-last-chunk database-failure/process-restart evidence.

No deployment, security-group/IAM change, paid/upstream request, credential
change, exporter/outbox activation, external database expansion or X-Insight
runtime change was performed.
