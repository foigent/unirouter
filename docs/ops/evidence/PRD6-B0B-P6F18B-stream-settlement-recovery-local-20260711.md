# PRD6 Batch 0B P6-F18B Stream Settlement Recovery — Local Evidence

> Date: 2026-07-11
> Scope: UniRouter repository only
> Evidence level: Local verified; required PR CI pending
> Implementation commit: `32bf08e78d2a9904a64b8009ee9923de280730e9`
> Deployment/target/production claim: none

## 1. Confirmed pre-fix defect

Before this batch, `relayStreamRequest` forwarded model chunks and then called
the same settlement helper used by non-stream responses. After three failed
settlement attempts that helper called `Cancel`, so already delivered output
could be refunded. The F18A database state included the reserved
`settlement_pending` value but no request-path transition, final-usage payload,
claim lease or restart worker used it.

## 2. Implemented invariant

The stream path now enforces this order:

1. receive and buffer the first upstream data chunk;
2. durably transition `frozen -> settlement_pending` and set
   `output_started_at`;
3. only after the marker commits, write the first billable data chunk;
4. after the upstream stream ends, durably persist immutable prompt/completion
   tokens, usage source and calculated cost;
5. attempt settlement without any cancellation fallback;
6. on immediate failure, retain the reservation for a leased, bounded restart
   worker; the same `Settle` row lock and unique terminal index still produce
   at most one terminal ledger event.

Pre-output marker failure withholds model content and safely cancels the still
`frozen` reservation. A stream with no upstream data is also pre-output and can
be cancelled. Once the output marker exists, `Cancel` returns
`ErrSettlementPending` and cannot release the hold.

The recovery claim uses `FOR UPDATE SKIP LOCKED`, a 30-second lease, a batch
limit and bounded exponential retry. Recovery attempts and the last failure are
durable. A recovered settlement also converges an existing best-effort call log
to the authoritative token/cost receipt.

## 3. Fault evidence

Automated tests prove:

- marker persistence happens before content and usage persistence happens
  before settlement;
- a marker DB failure emits no model content and performs one safe pre-output
  cancellation;
- final-usage persistence failure after output performs no cancellation and
  omits `[DONE]` instead of claiming a durable terminal receipt;
- exhausted post-stream settlement retries return
  `settlement_pending_recovery`, do not call `Cancel`, and may still complete
  the client stream with an explicit pending status;
- closing the request process's DB pool after final usage persistence leaves
  the row pending; a new `Billing` instance recovers it;
- twenty concurrent recovery workers claim/settle that row exactly once and
  create exactly one settle/cancel terminal transaction;
- deterministic recovery failure releases the lease, records an error,
  schedules backoff, suppresses an immediate hot retry and later settles;
- a pending row without final usage is not guessed, charged at the reservation
  ceiling or refunded; it remains held for explicit reconciliation;
- migration `000023` applies, can roll back only before billable evidence, rolls
  forward, and refuses down after an output marker exists.

## 4. Verification commands and results

Passed:

```text
go test ./internal/billing ./internal/gateway ./internal/migrate -count=1
go test ./internal/billing ./internal/gateway ./internal/migrate ./internal/errdocs ./cmd/uniroute -count=1
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
cd frontend && npm test -- --reporter=dot
cd frontend && npm run type-check
cd frontend && npm run build
git diff --check
```

Frontend result: 27 files / 137 tests passed. The production frontend build
passed with the existing bundle-size warning. The Go race run passed with the
known macOS linker `LC_DYSYMTAB` warnings; no race was reported.

Local Docker verification was not performed because this host has no local
Docker CLI. Docker Build remains a required CI gate and no substitute claim is
made here.

## 5. Migration and release boundary

Migration `000023_prd6_stream_settlement_recovery` must be applied before the
new writer serves traffic. After any `output_started_at` or final-usage evidence
exists, its down migration refuses: an old binary could otherwise refund
billable output. Rollback is therefore drain/finalize plus forward repair, not
an old-binary schema rollback.

The worker is safe in every replica, but this is source/local evidence only.
No multi-instance target, rolling deployment, real provider, paid request,
credential, ECS/security-group, exporter/outbox or external database action was
performed.

## 6. Explicit P6-F20 boundary

F18B owns only the financial reservation and its final settlement payload. If a
process dies after model output begins but before exact final usage can be
persisted, the hold remains and no automatic charge/refund guess is made.
Per-chunk/process-kill usage journaling, guaranteed creation of missing call
logs, trace/audit replay and general notification/evidence outbox semantics
remain P6-F20. This batch must not be used to claim that broader outbox closed.
