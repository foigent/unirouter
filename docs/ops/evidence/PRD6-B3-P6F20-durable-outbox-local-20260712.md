# PRD6 Batch 3 P6-F20 Durable Outbox — Repository Evidence

> Date: 2026-07-12
> Owner: Codex
> Evidence level: CI verified / closed; D6-12 accepted; no target or production claim
> Base: `bd1692ef7d4f546fde2dbd0d5a35fe2bc92cb148`
> Implementation branch: `codex/prd6-durable-outbox`, retained at exact head
> `1beefee54d7af2cb565faa2894efb5f090eb3279`
> Integration: PR #177, squash merge
> `30d1e8f00cfcd45879f9650a3a98f178e844c945`

## 1. Accepted Scope

The CI-verified P6-F20 implementation adds a repository-local PostgreSQL transactional outbox for minimal
usage/finalization, detailed evidence and tenant-scoped notification delivery.
It also closes the previously recorded cross-process gap between authoritative
financial terminal state and Redis-backed Agent-ancestor, distributed-policy
and weighted-workload reservations.

This batch does not add Kafka, NATS, Redis Streams, a public adapter, an
external exporter, continuous ingestion, X-Insight transport, response caching,
raw prompt/response persistence, a deployment or a target-capacity claim.

D6-12 is accepted: earlier planning text that assigned a "per-chunk process-kill
journal" to P6-F20 is superseded. D6-10/P6-F11 owns reservation-aware streaming,
bounded checkpoints, explicit interruption and client recovery. This implementation
does not guess usage when a process dies mid-stream before final usage is
persisted; the existing conservative financial hold remains.

## 2. Confirmed Implementation

### 2.1 Bounded internal outbox

- `internal/outbox/outbox.go` defines `internal-outbox.v1`, a 256-KiB payload
  bound, 1-16 workers, 1-100 event batches, leased `FOR UPDATE SKIP LOCKED`
  claims, exponential retry, idempotent delete-after-materialization and
  Prometheus enqueue/process/retry/dead-letter/lag metrics.
- The default is two workers, 25 events per batch, a one-second poll, a
  30-second lease and a 30-second trace-lag SLO. Three quarters of each claim
  batch are reserved for billing/audit/call-log priority and one quarter for
  lower-priority work, preventing trace starvation.
- Minimal financial journals and runtime terminal hooks retry until success;
  a runtime hook resolves safely at its exact Redis state expiry. Detailed
  evidence and notification work dead-letter after the bounded attempt count.
- `internal/runtimeconfig/config.go` exposes the credential-free effective
  contract and explicitly reports `external_exporter_enabled=false`.

### 2.2 Financial truth and incremental budget accounting

- `internal/billing/billing.go`, `internal/billing/settlement_recovery.go` and
  `internal/billing/outbox.go` enqueue prepared, settled and cancelled usage
  envelopes in the same transaction as their authoritative financial change.
- `usage_finalization_journal` retains only model, normalized usage, immutable
  price version and cost facts. It contains no prompt, response, provider key,
  webhook URL or credential.
- Migration `000027_prd6_durable_internal_outbox` establishes the current-period
  pre-outbox budget baseline once while old writers are drained. Runtime
  settlement then performs one atomic increment only after a unique settled
  journal insert. It never scans `call_logs`, so a call log that arrives before
  another request's settlement cannot cause double counting.
- Budget evaluation and Webhook delivery are separate outbox events. Webhook
  requests carry `Idempotency-Key: unirouter-notification-delivery-<id>`.

### 2.3 Evidence and audit materialization

- Gateway call logs and sanitized trace events enqueue bounded payloads and
  materialize through destination `outbox_event_key` uniqueness. A mixed-version
  direct call-log row with the same organization/request identity is not
  duplicated.
- Central admin audit writes and the gateway unregistered-model audit use the
  high-priority audit worker. Request-correlated duplicates collapse; identical
  uncorrelated background events remain distinct facts.
- If an evidence enqueue fails before acceptance, the current request path
  falls back to the existing direct insert. Financial terminal truth never
  depends on that fallback.

### 2.4 Durable Agent/policy/workload terminal coupling

- `runtime_terminal_hooks` stores server-owned reservation handles and their
  hashes, never an Agent bearer capability or root API key.
- `internal/terminalhook/service.go` reconciles every hook from
  `freeze_idempotency`, the authoritative financial state, and queues one
  `runtime.terminal.v1` event per hook.
- `internal/capbilling`, `internal/policybilling`, `internal/agentcap`,
  `internal/limitpolicy` and `internal/workloadadmission` persist/restore the
  minimum terminal-only reservation state. Successful request processes retain
  the existing immediate idempotent Redis release for throughput; the durable
  event is the crash/outage recovery path rather than the latency path.
- An ambiguous Settle/Cancel boundary never guesses whether to refund. The
  worker reads the committed financial terminal state and applies settlement or
  cancellation accordingly.
- A child capability still has a five-minute maximum token, but an admitted
  operation may use its independently bounded invocation lease up to the
  existing 24-hour maximum. The durable hook expires with the actual reservation
  state, not at the capability-token lifetime.

### 2.5 Upgrade and rollback boundary

- Migration `000027` adds the outbox, financial journal, budget ledger and
  runtime-hook tables plus idempotency columns on log/audit/delivery
  destinations.
- It migrates only eligible pre-existing pending/failed Webhook deliveries;
  disabled, unverified or quarantined routes are not silently replayed.
- Down succeeds only before any new writer activates the schema. After an
  outbox row, journal, budget baseline, runtime hook or destination receipt
  exists, down refuses and the release must roll forward.
- Activation is non-rolling: drain old writers, apply `000027`, then start only
  binaries that understand the new financial/evidence contract. This document
  does not authorize that activation on any target.

## 3. Fault, Replay and Ordering Evidence

The P6-F20 integration matrix proves:

1. a claimed event survives a simulated process death and expired lease;
2. duplicate enqueue and destination replay materialize one fact;
3. billing/audit/call-log priority does not starve reserved trace work;
4. ordinary poison evidence reaches a bounded dead letter;
5. a financial settlement persists before asynchronous journal, budget event
   and Webhook materialization;
6. two already-materialized call logs whose settlement journals arrive later
   increment project spend exactly twice, not four times;
7. a terminal Redis executor failure remains durable even with
   `MaxAttempts=1`, then a newly constructed service instance completes it;
8. replay after completion creates no new terminal work;
9. Agent, distributed-policy and workload reservation snapshots survive JSON
   round trip and reject invalid restoration; and
10. unused migration down, roll-forward baseline, activated-down refusal and
    destination/schema readback all pass on an isolated database.

## 4. Red/Green Findings During Implementation

- The first materializer draft used partial unique indexes with
  `ON CONFLICT(column)`. PostgreSQL correctly rejected that conflict target.
  The migration now uses nullable full unique indexes, and the real migration
  plus every materializer test passes.
- The first call-log `INSERT ... SELECT` draft left parameter types ambiguous.
  Explicit identity casts removed the ambiguity and the gateway integration
  gate passes.
- Review found that lazy runtime `SUM(call_logs)` bootstrap could double count
  another request whose log arrived before its settlement journal. The runtime
  scan was removed, `000027` now creates the one-time pre-outbox baseline, and
  `TestP6F20IncrementalBudgetDoesNotDoubleCountAheadOfJournalCallLogs` proves
  the adverse ordering produces spend `2`, not `4`.
- Review also found that worker-only Redis finalization would retain capacity
  for up to the poll interval. The normal immediate idempotent release was kept,
  with the durable hook used for process/outage recovery.

## 5. Local Verification

All commands completed successfully on 2026-07-12:

```text
REQUIRE_TEST_DATABASE=1 go test -race ./internal/outbox ./internal/billing ./internal/gateway ./internal/admin ./internal/terminalhook ./internal/capbilling ./internal/policybilling ./internal/limitpolicy ./internal/workloadadmission -run TestP6F20 -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate -run TestP6F20DurableOutboxMigrationUpDownGuardAndRollForward -count=1
go test ./...
go vet ./...
go test -race ./... -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run type-check
npm --prefix frontend run build
git diff --check
```

Observed results:

- all nine focused P6-F20 package gates passed under the race detector;
- the isolated migration up/down/roll-forward gate passed;
- every Go package passed both the ordinary and race-enabled full repository
  suites, and the isolated admin integration suite passed;
- frontend passed 29 files / 144 tests, type-check and production build;
- no application frontend source changed; and
- macOS race-linker `LC_DYSYMTAB` warnings were emitted, but every race binary
  linked, ran and returned exit status zero.

The local tests use isolated PostgreSQL databases, in-process/fake Redis
terminal executors where fault injection is required, and `httptest` Webhooks.
They send no provider or paid request and do not access a target environment.

## 6. Required CI, Integration And Closure

The complete integration truth chain is:

1. Exact implementation head
   `1beefee54d7af2cb565faa2894efb5f090eb3279` passed PR #177 required run
   `29193900584`: Backend Tests 6m40s, Frontend Build 3m43s and Docker Build
   1m04s. Backend visibly passed the explicit P6-F20 outbox/terminal-recovery
   race gate and migration rollback gate.
2. PR #177 was squash-merged as
   `30d1e8f00cfcd45879f9650a3a98f178e844c945`; the implementation branch is
   retained at the exact reviewed head.
3. The merge commit passed post-merge `main` run `29194150134`: Backend Tests
   6m19s, Frontend Build 3m26s and Docker Build 1m00s, including both explicit
   P6-F20 PostgreSQL/race gates.

`.github/workflows/ci.yml` makes the P6-F20 race and migration gates explicit
and fail-closed on missing PostgreSQL. P6-F20 is therefore **CI verified /
closed**, and Batch 3 is closed. P6-F10 and P6-F11 remain separate accepted
rows.

## 7. Explicit Non-Claims

Even after CI promotion, this evidence will not prove 100x target capacity,
production deployment, external-message-broker operation, public exporter
compatibility, target Webhook delivery, X-Insight ingestion, retention sizing or
response caching. Those require separately accepted and authorized evidence.

This evidence specifically does not claim recovery of exact delivered usage
when the process dies after a stream chunk is written but before final usage is
persisted. That network/database boundary cannot be made atomic by an outbox
alone.

The accepted boundary and competitor/HA research are retained in
`docs/planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md`.
