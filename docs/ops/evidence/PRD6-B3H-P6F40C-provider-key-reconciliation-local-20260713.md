# PRD6 Batch 3H P6-F40C Provider-Key Reconciliation Local Evidence

> Date: 2026-07-13
> Baseline: `468969df16ee3fb142d04605af17ba615a56b8b7`
> Branch: `codex/prd6-provider-key-reconciliation`
> Evidence level: Local verified; required PR CI pending
> Risk: R3/R4 paid admission, customer charging, operator authority and migration safety

## Accepted Boundary

D6-13 requires an explicit financial owner for an unresolved provider attempt.
An affected API key must stop admitting new paid Chat or Embeddings requests
after UniRouter has durable evidence of an ambiguous outcome, unusable response
or known usage that cannot yet be funded. A platform administrator must make one
durable collect, refund or bounded-subsidy decision before the key is unblocked.

D6-14 keeps the Customer Revenue Ledger separate from Provider COGS evidence.
The reconciliation API therefore accepts no amount. `collect` replays only the
stored authoritative `upstream` usage, immutable customer price and existing
billable-output marker. An aggregate provider bill, balance delta or
operator-entered value cannot create customer debt. `refund` and `subsidy`
release only the existing hold; subsidy is allowed only when the attempt already
contains a known bounded cost.

## Implemented Behavior

- Paid Chat and Embeddings admission checks the existing indexed PostgreSQL
  provider-attempt authority. Explicit `outcome_unknown`, `response_unusable`
  and `funding_pending` states block the affected root API key and every Agent
  capability derived from it. A database read failure denies admission with a
  typed 503 instead of bypassing financial state.
- Ordinary `dispatch_started` and `response_accepted` work does not serialize
  the whole key. Request-scoped attempt authority still prevents duplicate paid
  attempts for the same operation. Detecting a process crash while an otherwise
  valid long request remains in one of those pending states requires a later
  lease/heartbeat design; this slice does not guess from elapsed wall time.
- `POST /api/v1/provider-cost/reconciliations` is platform-admin only, accepts
  exactly one bounded JSON object and contains no amount field. It derives the
  actor from authenticated context and records reason/evidence with the
  immutable requested/completed phases.
- `GET /api/v1/provider-cost/pending?org_id=...` is platform-admin only, requires
  an explicit positive organization and returns at most 100 bounded metadata
  rows. It returns no prompt, response, credential or full provider URL.
- `collect` is available only for known upstream usage with an immutable
  settlement payload and an existing billable-output marker. An undelivered
  non-stream response cannot be charged. Insufficient cash leaves the decision
  and key block pending until the same immutable collect decision can complete;
  it creates no debt or terminal receipt.
- `refund` releases only the original cash/grant hold. `subsidy` uses the same
  release path but requires the stored known cost, making the platform loss
  bounded and auditable. Neither path invents provider usage or customer debt.
- All terminal mutations retain one wallet transaction, one attempt terminal
  receipt, one durable decision and two evidence phases under concurrency. A
  crash after collect settlement but before its completed phase is recovered
  only when the exact requested action, actor, reason, evidence and matching
  terminal transaction are already durable.
- Migration `000030` adds the append-only reconciliation evidence table and the
  `reconciled` attempt state. Its down path refuses after evidence, reconciled
  attempts or terminal payloads that depend on the new marker rule.

## Focused Verification

```bash
go test ./internal/billing -run '^TestP6F40C' -count=1
go test ./internal/admin ./internal/middleware ./cmd/uniroute \
  -run 'TestP6F40C|TestOpenAPISpecCoversRegisteredRoutes|TestFrontendDocsPageCoversRegisteredRoutes' -count=1
go test -tags=integration ./internal/migrate -run '^TestP6F40C' -count=1
go test -race ./internal/billing ./internal/middleware ./internal/admin \
  -run 'TestP6F40C' -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate \
  -run 'TestP6F40CProviderCostReconciliationMigrationUpDownGuardAndRollForward' -count=1
```

Current result: all commands `PASS`. The proof includes:

- no caller-supplied amount and rejection of extra/trailing JSON;
- authoritative-usage plus local-output collection and undelivered/estimated
  usage rejection;
- insufficient-cash pending followed by exact-decision replay;
- collect crash-gap recovery without a second transaction;
- refund and bounded subsidy behavior;
- 32 concurrent refunds with one terminal transaction and two evidence phases;
- organization-bound service access and platform-only route authority;
- ordinary in-flight requests do not block a key, while explicit ambiguity does
  and an authorized terminal decision clears the block; and
- migration up/down/refused-down/roll-forward behavior.

## Full Local Verification

```bash
go test ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Current result: all commands `PASS`; frontend reports 29 files / 144 tests. The
build retains its existing large-chunk warning and the macOS race binaries emit
the existing linker metadata warning; neither warning fails its command.

## Required CI

The branch still requires `Backend Tests`, `Frontend Build` and `Docker Build`
on the exact final PR head before P6-F40C or P6-F40 can be marked CI verified / closed.

## Rollout And Non-Claims

- This local evidence is not target, production, capacity or external-provider
  evidence. No deployment, credential, paid request, provider bill import or
  external database access occurred.
- Old binaries neither enforce the affected-key check nor preserve the new
  reconciliation state. A target activation must drain old application writers
  before migration `000030`; mixed-version financial safety is not claimed.
- The platform-admin API is the bounded MVP operator interface. A dedicated
  FinOps workflow UI, approval/supersession workflow, provider COGS control
  plane and supplier connectors remain future D7-COST candidates.
- P6-F41 remains separate: explicit usage authority/purpose/finality and richer
  local delivery outcome metadata are not claimed by this slice. No raw response
  retention, Cache Service, generalized media units or separate Cost Control
  Service is introduced.
