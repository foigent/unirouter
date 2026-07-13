# PRD6 Batch 5 P6-F11 Stream Policy And Termination — Repository Evidence

> Date: 2026-07-13
> Decisions: D6-10 Scheme A, D6-12 and D6-16 S-A accepted
> Branch: `codex/prd6-stream-policy-termination`
> Base: post-P6-F10 `main` at
> `f9925779f1da7681b07282be5d500773a687d3b9`
> Evidence level: repository/local verified; CI, merge and post-merge evidence
> are pending
> Scope: UniRouter repository only; no deployment, target mutation, real or
> paid provider/detector call, credential change, raw-payload persistence,
> exporter activation, additional database scope or X-Insight runtime work

## 1. Confirmed Pre-Change Findings

The following findings were confirmed from the base source, not inferred from
the planning discussion:

1. Guardrail policy stages stopped at `input` and `nonstream_output`; stream
   model-data frames had no pinned policy evaluation before disclosure.
2. The stream loop forwarded each accepted normalized or OpenAI-compatible
   data frame immediately. It had bounded line and write transport, but no
   200 ms / 4 KiB synchronous inspection window, detector lookbehind or safe
   transform boundary.
3. Long streams polled the wallet balance every 30 seconds. That was neither a
   reservation extension nor an atomic guarantee across the distributed TPM,
   wallet and Agent-ancestor budgets.
4. Billable-output marking and the first `write_started` receipt were separate
   database operations. There was no process-owner lease or post-write
   checkpoint for a process that died between frames.
5. Final settlement could recover after final usage persistence, but a process
   lost in the middle of a stream had no bounded proof of its latest safely
   delivered counters. Transparent replay or stream migration was therefore
   not a truthful claim.

These are repository findings. They do not establish a production disclosure
rate, detector precision, provider invoice result or target HA capacity.

## 2. Enforced Stream Guardrail Contract

- `stream_output` is an independently selectable policy stage. The migration
  creates no policy and enables no detector.
- One request keeps the same pinned policy and detector registry used by the
  input path. Built-in sensitive-word and deterministic PII/secret detectors
  declare bounded synchronous capability. An external detector is
  post-disclosure shadow/audit unless its fixed process registration explicitly
  declares bounded synchronous stream capability and the policy explicitly
  requests enforcement.
- An enforced batch contains complete normalized model-data JSON frames. It
  closes at 200 ms or 4 KiB, whichever comes first. A single normalized frame
  above 4 KiB is terminated before disclosure; the larger SSE transport-line
  ceiling does not widen the enforcement buffer.
- Model text, text parts, refusal text, reasoning text, legacy completion text
  and function or tool argument strings are inspected. Sparse choice/tool
  indexes use their semantic stream indexes rather than local array positions.
  Unknown top-level/delta text shapes and uninspected log-probability/media
  structures fail closed under enforcement; bounded usage-only metadata
  remains valid. Without an enforced stream policy, unknown shapes retain the
  existing compatibility pass-through.
- Detector-declared lookbehind preserves a UTF-8-safe suffix across delivered
  batches. A block withholds the entire current batch. A redaction may rewrite
  only the undisclosed batch; a transform that would rewrite an already
  disclosed prefix or structured arguments terminates instead.
- A block, unavailable fail-closed detector, unsafe shape/transform, frame
  overflow or hard-budget denial closes the accepted upstream body. It does not
  retry or fail over to another paid channel and emits an explicit non-success
  SSE terminal without `[DONE]`.
- Shadow/audit work runs only after disclosure in a bounded 64-slot pool with a
  two-second task deadline. It carries at most 4 KiB of in-memory, per-path
  disclosed context so split values can still be evaluated without retaining
  response bodies. Trace evidence labels
  `pre_verdict_disclosure=true`; saturation drops advisory work explicitly
  rather than spawning an unbounded goroutine population.
- The implementation does not claim zero leakage. A value whose identifying
  prefix was already released in an earlier 200 ms / 4 KiB batch can be stopped
  later but cannot be recalled. The declared lookbehind and residual disclosure
  boundary remain operator-visible.

OpenAI-compatible frames and provider-native Anthropic events use the same
normalized pre-flush enforcement. The repository tests prove both termination
paths without making a live-provider claim.

## 3. Three-Layer Long-Stream Budget Extension

Before another guarded batch can flush beyond the retained output guard band,
the gateway requests one monotonic extension sequence through the existing
wrapper order:

1. every matching shared Redis TPM policy;
2. the PostgreSQL wallet reservation at its immutable frozen output price; and
3. every signed Agent ancestor's token and cost reservation, when the request
   carries an Agent capability.

Each successful layer is idempotent for the same sequence and amount. A denial
at any layer stops before the next model-data flush. If an earlier independent
layer succeeded before a later layer denied or became unavailable, its hold is
retained conservatively; terminal settlement/cancellation replaces/releases
the reservation from authoritative actual usage. No compensating rollback can
temporarily reopen capacity and allow the unsafe batch through.

`billing_reservation_extensions` preserves successful and insufficient-wallet
receipts. Frozen prices and grant/cash source allocation do not change during
extension. Thirty-two concurrent retries of the same sequence create one
wallet charge and one receipt. An explicit `max_tokens` remains a hard output
ceiling; only requests without that explicit ceiling may use dynamic extension.

## 4. Delivery Lease, Checkpoint And Recovery

- `BeginStreamDelivery` atomically moves the reservation to billable output,
  appends `write_started`, updates idempotency readback and creates one owner
  lease before the first model-data write.
- The process renews a 45-second lease every 15 seconds. It renews a two-minute
  terminal-finalization lease before stopping stream heartbeats, preventing a
  recovery replica from settling concurrently with a still-authoritative
  gateway. Loss of that lease stops local settlement and hands authority to
  checkpoint recovery.
- Post-write checkpoints are append-only and monotonic for attempt, policy
  version, delivered bytes/chunks and gateway prompt/completion counters. They
  occur after the first accepted model-data write and then at one second,
  64 KiB or 128 completion-token cadence, plus a final forced checkpoint.
- An expired stream is claimed with `FOR UPDATE SKIP LOCKED`. Claiming changes
  owner and grants a bounded recovery lease; each item renews that authority
  before mutation, but claiming itself is not terminal. If that recovery
  process also dies, another replica can reclaim after expiry. A pre-existing
  `interrupted`, `write_failed` or `completed` delivery receipt is reused rather
  than followed by a conflicting second terminal receipt. Only successful
  interruption/settlement or a recovered completed receipt makes the lease
  terminal.
- A checkpointed process loss records an interrupted delivery and charges no
  more than the latest checkpoint's gateway-counter usage. A stream with no
  checkpoint records an explicit zero-delivery interruption, charges the
  customer zero and retains provider-cost uncertainty for reconciliation.
- An expired original owner cannot resurrect, checkpoint or finish its lease.
  Exact duplicate interruption receipts, usage persistence and settlement are
  idempotent, so a retry after an intermediate recovery failure cannot create a
  second customer charge.
- The original Idempotency-Key is never re-executed or transparently migrated.
  Its 409 `original_failed` receipt includes the original request ID, HTTP
  status, delivery mode/outcome and bounded delivered byte/chunk counters.

Checkpoints contain counters and policy identity only. They do not contain
Prompt, response text, provider credential or detector endpoint.

## 5. Migration And Operator Boundary

Migration `000034_prd6_stream_policy_termination` adds:

- the `stream_output` guardrail stage;
- immutable-price, append-only, monotonic reservation extension sequence and
  receipts;
- stream owner leases and append-only checkpoints; and
- an explicit zero-counter `interrupted` delivery outcome.

An unused schema can down and roll forward. Any stream policy, extension
receipt, lease or checkpoint refuses destructive down. Activation is
non-rolling: drain old gateway/control-plane binaries, apply `000034`, start
only binaries that understand the new state, verify the no-policy compatibility
path and recovery, then begin with shadow policies. This document does not
authorize that activation.

The policy list API exposes approved detectors, fixed stream capabilities,
200 ms / 4 KiB bounds and checkpoint cadence. The operator UI explains in
plain language that shadow verdicts can follow disclosure, a single enforced
frame above 4 KiB stops, interruption is not replay, undelivered content is not
customer-charged and upstream COGS may still require reconciliation.

## 6. Four-Quadrant Closure View

### Known Knowns

- Repository-local pre-flush enforcement, fixed bounds, no retry/failover,
  monotonic three-layer extension and checkpoint recovery are executable and
  covered by deterministic, race and PostgreSQL tests.
- Customer charging uses only safe local delivery/checkpoint evidence;
  provider evidence remains a separate COGS authority.

### Known Unknowns

- Production detector precision/recall, real provider stream shapes, target
  p95/p99 latency, Redis/PostgreSQL capacity and provider invoice matching need
  separately authorized target evidence.
- The residual disclosure rate across already-closed windows is workload and
  detector dependent; no universal zero-leakage percentage is claimed.

### Unknown Knowns

- The old wallet poll looked like a budget guard but did not reserve any
  distributed or financial capacity. Making the three authoritative layers
  explicit removes that hidden assumption.
- Existing provider adapters already normalized native streams. Reusing that
  boundary gives one enforcement contract instead of provider-specific policy
  forks.

### Unknown Unknowns

- Future multimodal frames, video/long-job protocols and new provider event
  shapes can exceed today's text-frame assumptions. Enforcement therefore
  fails closed on unknown text shapes and oversized frames instead of treating
  them as inspected. Object-reference media jobs and broader adapters remain
  later scope.
- Process, database and network failures can occur between any two local
  operations. Renewable ownership, conservative holds, append-only checkpoints
  and idempotent terminal state bound the result without claiming an impossible
  atomic downstream-socket/database write.

## 7. Local Verification

The exact working tree passed the following local gates on 2026-07-13:

```text
go test ./...
go vet ./...
go test -race ./... -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin ./internal/agentcap ./internal/limitpolicy -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate -run TestP6F11StreamPolicyTerminationMigrationUpDownGuardAndRollForward -count=1
go test -race ./internal/billing ./internal/gateway ./internal/agentcap ./internal/limitpolicy ./internal/guardrail ./internal/admin ./internal/middleware ./internal/capbilling ./internal/policybilling -run TestP6F11 -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run type-check
npm --prefix frontend run build
OpenAPI YAML parse
git diff --check
```

All commands passed. The frontend test run covered 30 files / 148 tests. The
production frontend build passed with its existing large-chunk advisory. The
Go race and PostgreSQL integration runs emitted the host toolchain's known
macOS `LC_DYSYMTAB` linker warning but returned success. This host has no
`docker` executable, so the image build remains a required CI gate rather than
a local claim.

Focused evidence includes: 200 ms measured release; exact 4 KiB batch close;
single-frame overflow; within-batch, enforced lookbehind and bounded advisory
cross-frame context; safe redaction/unsafe cross-boundary termination; sparse
semantic choice indexes; reasoning text, unknown top-level/delta shapes,
non-enforced compatibility and usage-only metadata; OpenAI and Anthropic
termination; budget denial before disclosure; concurrent extension
idempotency and append-only receipts; wallet, shared-TPM and all-Agent-ancestor
extension order; expired-owner rejection; finalization lease duration;
multi-replica single recovery; recovery-claimer crash/reclaim; reuse of existing
interrupted/completed terminal receipts; latest-checkpoint settlement;
zero-checkpoint no-charge provider hold; idempotent client readback; and guarded
migration down/roll-forward/refusal.

## 8. Explicit Non-Claims

- No deployment, target environment, production availability or 100x capacity
  verification.
- No real/paid provider or detector call and no provider-invoice reconciliation
  result.
- No zero-leakage, transparent stream migration, response replay or exactly-once
  network delivery claim.
- No real external semantic detector, raw Prompt/response retention, Cache
  Service, media job plane, public SDK/adapter, exporter or X-Insight runtime
  dependency.
- No PRD5 reopening and no implicit promotion of PRD7 or target-evidence queue
  items.

P6-F11 remains repository/local verified only. It may move to CI-verified
closure after its exact commit passes all required PR jobs, is merged, and the
merge commit passes post-merge `main` CI.
