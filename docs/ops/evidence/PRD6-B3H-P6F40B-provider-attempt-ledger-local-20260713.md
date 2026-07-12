# PRD6 Batch 3H P6-F40B Provider Attempt Ledger Local Evidence

> Date: 2026-07-13
> Baseline: `060a71458f99c5218dbdeb656a30367bb0944a25`
> Branch: `codex/prd6-provider-attempt-ledger`
> Evidence level: Local verified; required PR CI is pending
> Risk: R3 provider cost, retry/cancel authority, billing decorators and migration safety

## Accepted Boundary

D6-13 keeps provider-cost enforcement inside the current UniRouter modular
monolith. P6-F40B adds a durable attempt boundary before every network dispatch
that may create provider liability. Availability retryability and financial
retry authority are separate: a transport error after dispatch is financially
ambiguous and cannot trigger another paid request, while a durable HTTP
rejection may retain the existing bounded adapter/policy failover behavior.

This slice does not add affected-key admission blocking or operator
collect/refund/bounded-subsidy actions; those remain P6-F40C. It also does not
store prompts, response bodies, credentials or complete provider URLs, add a
separate Cost Control Service, deploy, or send a real/paid request.

## Confirmed Pre-Fix Findings

Source inspection and existing gateway tests confirmed that:

- Chat and Embeddings retried classified transport failures even though a
  timeout/disconnect cannot prove that the provider did not accept or charge
  the request;
- HTTP 200 followed by a body-read, adapter-parse or normalized-JSON failure
  invoked ordinary cancellation and could erase a real provider liability;
- attempt IDs existed in trace/call-log evidence but had no durable financial
  state or terminal transaction link; and
- the production `policybilling -> capbilling -> billing` decorator chain had
  no attempt-authority contract because the contract did not yet exist.

These are repository/source findings. No provider invoice or target behavior
was inspected or inferred.

## Implemented State Machine

Migration `000029_prd6_provider_attempt_ledger` adds one bounded PostgreSQL row
per `(org_id, request_id, attempt_id)`, including customer API key, channel,
channel key, route sequence, endpoint class, provider/channel labels, HTTP
status, sanitized provider request ID, retry authority and terminal cost/receipt
links. It stores no request or response content.

| State | Financial meaning | Automatic paid retry | Ordinary cancel |
|-------|-------------------|----------------------|-----------------|
| `dispatch_started` | durable row exists before network dispatch; outcome not yet known | no | converted to provider-cost hold |
| `rejected` | a bounded HTTP response was classified as rejected | only when adapter classification authorizes it and route policy has capacity | allowed after all attempts are rejected |
| `response_accepted` | a 2xx response exists; usage/settlement is not terminal | no | converted to provider-cost hold |
| `outcome_unknown` | transport ended without authoritative provider outcome | no | already non-cancellable |
| `response_unusable` | a 2xx body could not be read or normalized | no | already non-cancellable |
| `funding_pending` | usage/cost is known but prepaid funding is insufficient | no | non-cancellable |
| `settled` | cost and transaction receipt are terminal | no | terminal replay only |

`BeginProviderAttempt` locks the financial reservation, is idempotent only for
the same still-undispatched attempt envelope, and refuses a second attempt while
any prior attempt is unresolved. `CompleteProviderAttempt` permits only
monotonic transitions and atomically changes ambiguous/unusable outcomes to
`settlement_pending` with a provider-cost marker. Generic cancellation now
locks attempt rows and converts `dispatch_started`/`response_accepted` evidence
to a hold instead of refunding it.

Successful settlement and funding-pending transitions update the matching
attempt in the same transaction as wallet/reservation truth. Recovery callers
without an in-memory attempt ID may resolve the single unresolved attempt for
the operation; multiple unresolved rows fail closed. Automatic settlement
recovery now claims only `billable_output` rows, never an attempt-reconciliation
hold.

The gateway persists the attempt before `http.Client.Do` for Chat and
Embeddings, completes a 2xx authority before body/stream handling, carries the
attempt ID into settlement, and does not retry or cancel ambiguous transport
outcomes. The production `policybilling` and `capbilling` decorators pass the
attempt envelope through unchanged.

## Migration And Rollout Safety

- `000029` is additive but is not an old/new mixed-writer contract. A future
  activation must drain old application writers first.
- Its down migration is permitted only while no attempt row or new pending
  reason exists; otherwise it refuses destructive rollback.
- `000028` down now refuses while the `000029` successor schema exists, so a
  rollback cannot remove provider-cost columns before removing the dependent
  authority in reverse order.
- No migration was applied outside isolated local test databases.

## Local Verification

The focused financial and gateway gate passes with the race detector:

```bash
REQUIRE_TEST_DATABASE=1 go test -race \
  ./internal/billing ./internal/gateway \
  ./internal/capbilling ./internal/policybilling \
  -run TestP6F40B -count=1
```

The proof includes:

- durable-before-dispatch ordering and three-attempt bounded ledger retry;
- dispatch suppression when pre-dispatch persistence fails;
- response withholding and no cancellation when 2xx authority persistence
  cannot be confirmed;
- 24 concurrent idempotent completion callers;
- rejected-response failover followed by one safe terminal cancel;
- ambiguous transport, accepted-response generic cancellation and malformed
  2xx responses all retaining the reservation without a terminal transaction;
- successful and insufficient-funding settlements updating the matching
  provider attempt atomically; and
- unchanged attempt identity across both production billing decorators.

Migration verification passes:

```bash
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate \
  -run 'TestP6F40(BProviderAttemptLedger|ProviderCostPending)' -count=1
```

The complete local repository, vet, command-contract and frontend gates also
pass after the production decorator path was connected:

```bash
go test ./... -count=1
go vet ./...
go test ./cmd/uniroute \
  -run 'Test(OpenAPISpec|FrontendDocs|BackendDocs|AdminRoutes)' -count=1
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Frontend verification reports 29 files / 144 tests. The build retains the
existing large-chunk warning and macOS race binaries retain the existing linker
metadata warning; the commands do not fail.

## Known Boundary And Remaining Work

For this slice, an adapter-classified non-2xx response is the durable
known-rejection authority. Some provider commercial contracts may still bill
particular error responses; P6-F40B does not claim universal provider invoice
semantics. The retained attempt/status/provider-request metadata permits later
provider-specific reconciliation without storing bodies.

P6-F40 remains open until P6-F40C adds tenant-safe affected-key admission
blocking and explicit operator collect/refund/bounded-subsidy reconciliation.
P6-F41 separately owns typed missing/explicit-zero usage authority and local
delivery outcome. No target, production, paid-provider, credential or external
integration claim is made.
