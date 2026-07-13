# PRD6 Batch 3H P6-F41 Usage And Delivery Integrity Evidence

> Date: 2026-07-13
> Evidence level: CI verified / closed by PR #187 exact implementation/test head `6d0e6ba` / run `29229320432`
> Branch: `codex/prd6-usage-delivery-integrity`
> Scope: repository-only P6-F41A/P6-F41B; no deployment, real provider call,
> credential, raw-payload, target or production authorization

## Confirmed Pre-Fix Findings

1. A successful OpenAI-compatible or Anthropic response without a `usage`
   object was represented as provider-reported zero rather than as missing
   evidence. Explicit provider zero and absent usage were therefore
   indistinguishable at settlement.
2. Non-stream Chat and Embeddings reached terminal customer settlement before
   the local `ResponseWriter` accepted the response bytes. A downstream writer
   failure could therefore leave a customer charge without local delivery
   evidence.
3. Streaming used one counter for provider-reported usage and downstream
   delivery. If a stream was interrupted after partial delivery, the full
   provider usage could become customer-charge authority.
4. Idempotency receipts exposed operation state but not typed usage authority
   or bounded delivery outcome, and an underlying writer error could still
   leave the operation state as completed.

These are repository findings. They do not prove any supplier's final invoice
or a remote client's application-level consumption.

## Implemented Contract

### P6-F41A — Typed Usage Authority

- `unirouter.usage.v1` carries `authority`, `purpose`, `finality` and an
  optional `estimate_version` through adapters, gateway, billing reservations,
  call logs, the durable usage journal, recovery and idempotency readback.
- Provider-reported explicit zero remains `provider_reported/final`; an absent
  usage object becomes `none/provider_evidence/missing` and cannot be relabelled
  as customer-charge authority.
- Missing non-stream Chat usage uses the bounded reservation ceiling
  `unirouter.token-ceiling.v1`; Embeddings uses the bounded input-only estimate.
  Output is withheld unless the estimate is fully backed by prepaid funds.
- A prepared non-stream response reports `charged: 0`, a
  `projected_charge`, and `charge_finality: pending_local_delivery`; it does not
  claim a terminal customer debit before the write.

### P6-F41B — Metadata-Only Delivery Receipt

- Migration `000032` adds append-only, tenant-scoped
  `request_delivery_receipts` with at most three monotonic transitions:
  `write_started -> completed|write_failed` for non-stream and
  `write_started -> partial -> completed|interrupted` for stream.
- Receipts store only mode, outcome, bounded byte/chunk counters, HTTP status,
  error code and timestamp. They contain no prompt, response body, chunk,
  credential or arbitrary payload column.
- Non-stream delivery first persists usage and secures any cash-only top-up as
  a hold. Billing rejects settlement for a hold or `write_started` receipt and
  permits customer collection only after `completed` local delivery.
- A settlement carrying an upstream `attempt_id` cannot bypass that sequence by
  charging directly from the legacy `frozen` state; P6-F40 provider-attempt and
  reconciliation tests now exercise the same delivery preparation boundary.
- Streaming records `write_started` before the first model data frame and
  `partial` after the local writer accepts bytes. A normal complete stream may
  use final provider-reported usage for the customer charge; an earlier
  transport interruption uses the independent
  `unirouter.stream-counter.v2` delivered counter. Provider-reported usage is
  retained separately as provider evidence and cannot enlarge that interrupted
  customer charge.
- A first write failure creates no usage settlement and no customer charge. It
  retains the provider liability, blocks the affected key through the existing
  P6-F40 state machine, and requires the already authorized operator
  refund/subsidy path to release the hold.
- A short write of only part of an SSE event is `write_failed`, not `partial`;
  only a complete locally accepted event can become chargeable stream delivery.
- Idempotency readback includes typed usage and the latest bounded delivery
  outcome without replaying a response body. Underlying short writes or write
  errors mark the idempotent operation failed.

`delivered_bytes` is evidence that the local Go `ResponseWriter` accepted bytes;
the streaming receipt separately converges to completed or interrupted when the
subsequent flush/protocol terminal succeeds or fails. It is not proof that a
remote process consumed, rendered or retained those bytes. Stream counters
cover provider model `data:` frames and deliberately exclude UniRouter's final
cost extension and `[DONE]` marker.

## Data And Migration Safety

- `000031_prd6_usage_authority` adds bounded typed fields and rejects a down
  migration after typed financial evidence exists.
- `000032_prd6_delivery_receipt` rejects UPDATE/DELETE of receipt rows, sequence
  gaps, mode changes, decreasing counters and destructive down migration after
  delivery/idempotency receipt evidence exists.
- Predecessor rollback tests now remove `000032 -> 000031 -> 000030 -> 000029 ->
  000028` in strict successor order.
- Automatic recovery includes only historical `billable_output` or a terminal
  `delivery_completed` reason; `delivery_prepared`, `delivery_failed` and
  funding-pending rows are not guessed into customer charges.

## Local Verification

All commands below passed on 2026-07-13:

```text
go test ./...
go test -race ./...
go vet ./...
go test -race -tags=integration ./internal/billing -run 'TestP6F(09|18B|40|41)' -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/adapters ./internal/billing ./internal/gateway ./internal/middleware ./internal/capbilling ./internal/policybilling -run TestP6F41 -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate -run 'TestP6F41(AUsageAuthority|BDeliveryReceipt)MigrationUpDownGuardAndRollForward' -count=1
cd frontend && npm test -- --run
cd frontend && npm run type-check
cd frontend && npm run build
git diff --check
```

Observed frontend result: 29 files / 144 tests passed; type-check and production
build passed. Local Docker verification was unavailable because this host has no
`docker` executable; Docker Build remains a required CI gate rather than a local
claim.

Focused PostgreSQL/race evidence covers:

- OpenAI-compatible and Anthropic explicit-zero versus missing fixtures;
- conservative-estimate full-funding and withheld-output behavior;
- no settlement before non-stream `completed` delivery;
- stream partial delivery using only the durable gateway counter;
- first-write and post-write failure behavior;
- partial-frame short-write rejection and terminal funding-hold preservation;
- 32-way receipt replay and cross-tenant rejection;
- no payload columns;
- migration clean down, roll-forward, invalid transition, append-only mutation
  rejection and evidence-bearing down refusal;
- P6-F09, P6-F18B and P6-F40 financial/recovery compatibility.

## Required-CI And Promotion State

- Required CI workflow has explicit P6-F41 package and `000031/000032`
  migration gates.
- PR #187 exact implementation/test head `6d0e6ba` passed required run
  `29229320432`: Backend Tests, Frontend Build and Docker Build are green.
- P6-F41 is therefore `CI verified / closed`. This does not promote it to
  target verified or production verified.

## Explicit Non-Claims And Future Queue

This slice does not implement or claim:

- real-time confirmation of supplier charging or per-request supplier invoice
  truth;
- provider billing connectors, cloud cost export, controlled bill import,
  Provider COGS ledger or FinOps UI;
- response Cache Service, raw-body retention or response replay;
- generalized character/audio/image/video units or asynchronous media jobs;
- a separate Cost Control Service, managed/unified billing, target deployment or
  production verification.

Those uncovered product capabilities remain in
`docs/planning/PRD6-UPSTREAM-COST-RECONCILIATION-AND-PROVIDER-COVERAGE-20260713.md`
and `docs/planning/PRD7-CANDIDATE-BACKLOG.md`; this P6-F41 implementation does
not silently pull them into PRD6.
