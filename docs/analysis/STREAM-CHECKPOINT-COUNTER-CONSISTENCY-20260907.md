# Stream checkpoint counter consistency

Exact pre-code review object: this design on main
`a43c48792e14438fff38438dbd2414eaa74e9a07`, tree
`34648de94ea3f3806c9b221ab3c63f1a71f3b88d`, 1.2.67/M114.

## Active milestone and boundaries

The accepted productization continuation includes correcting conservative stream
accounting. This bounded first repair makes newly emitted delivery checkpoints
use the same existing customer estimate as live interruption settlement. It does
not claim to resolve the separate byte-based output admission ceiling.

PR404 is merged with all five CI checks passed. Its development/test source was
transferred and verified, but the 1.2.67 build has not started: the storage gate
reported 81% against the accepted 80% ceiling. A narrow decision about two old
BuildKit caches is pending. This local repair has no dependency on that cleanup.

Applicable existing contracts are the P6-F11 delivery/checkpoint/recovery contract
in `docs/ops/evidence/PRD6-B5-P6F11-stream-policy-termination-local-20260713.md`,
the counter description in `docs/tech-ref/12-token-metering-v2.md`, and current
attempt charge authority in `internal/billing/delivery.go`. Historical documents
do not override the latter's provider-final restriction.

## Verified source defect

`internal/gateway/relay.go` has two independent counters:

- `admittedOutputTokens` adds UTF-8 text bytes and guards disclosed output against
  the retained reservation/public cap. It is a conservative admission quantity.
- `deliveredCounter` accumulates the existing `StreamTokenCounter` estimate only
  after a complete downstream frame write. Live interruption settlement uses
  its `InputTokens()` and `OutputTokens()` under `unirouter.stream-counter.v2`.

The checkpoint closure currently writes the first counter into the field named
`CompletionTokens`; its token checkpoint cadence also uses that byte quantity.
`RecoverExpiredStreamDeliveries` later interprets those persisted integers as the
same counter-v2 customer estimate. Therefore a process loss can follow a
different numeric basis from live interruption for the same disclosed frames.
The existing integration recovery test supplies counters directly and cannot
catch the incorrect gateway producer.

The gateway never sets upstream usage on `deliveredCounter`. Provider-final
usage has a separate authoritative path. Frames include disclosed reasoning and
tool arguments through the existing normalized `frame.Text`; this repair must
retain that extraction and cannot invent another parser.

## Proposed exact repair

1. At each checkpoint decision, read the existing delivered counter's completion
   estimate. Use it for `StreamDeliveryCheckpoint.CompletionTokens`, the token
   cadence comparison and the saved checkpoint token position. Input already
   comes from the same counter. Delivered bytes/chunks retain their byte/frame
   meanings and continue independently to trigger a checkpoint.
2. Keep all admission/extension arithmetic, output stopping behavior, request
   and public limits, rate freezing, Provider usage extraction and live fallback
   estimator algorithm unchanged. Do not replace the guard with characters/4,
   infer a tokenizer, increase experimental maxTokens or retry a Provider call.
3. Keep existing lease/attempt/generation fences, forced-final checkpoint,
   idempotent duplicate handling and one terminal settlement. A partial frame
   cannot contribute a complete frame's estimated customer tokens.
4. Reuse the existing counter-v2 contract. This corrects the checkpoint producer
   to its already documented/live implementation; it does not introduce another
   estimator. No new table, migration, pricing policy or snapshot authority.
5. Preserve existing checkpoint rows, usage receipts and historical amounts.
   Historical byte-valued rows cannot be reconstructed without their original
   stream, so do not rewrite or label them retroactively corrected. Existing
   recovery remains a consumer of the recorded value; the new guarantee applies
   to checkpoints emitted by the fixed binary. A live request remains owned by
   one binary and cannot switch counter algorithms during a rolling change.

## Required verification

- A real gateway stream regression uses nontrivial Latin/CJK/emoji text,
  reasoning and tool arguments with a known frame sequence. Assert the persisted
  checkpoint token count matches the same existing counter used for that
  sequence, and differs from UTF-8 bytes where expected. Assert the actual
  Provider request and admission/output limit behavior remain unchanged.
- Include complete and partial downstream writes. A partially written frame's
  bytes may be evidence of disclosure, but it must not gain the full frame's
  customer token estimate. Checkpoint byte/chunk counters retain their current
  semantics.
- A real PostgreSQL gateway-to-recovery test must consume a checkpoint produced
  by the relay, rather than fabricate the number under test. Suppress live
  terminal usage persistence using the existing test fault boundary, expire the
  lease, recover and prove the same estimated count and at most one terminal
  settlement on repeated recovery. Retain exact attempt/fence checks.
- Run the affected provider-final-charge integration boundary: an estimated
  checkpoint still cannot authorize a charge when the frozen attempt requires
  Provider-final usage. Missing usage remains pending, not zero or final.
- Existing stream transport, output guard, counter-v2 and billing recovery tests
  remain required. Use fixtures/isolated DB, not additional paid model calls.

## Separate remaining accounting work

The output guard still compares a conservative byte upper bound to token limits.
Removing that comparison needs a separate exact design for the supported
Provider output parameter's token scope and possible overrun, reservation
exposure, resource bounds and final usage. Qwen's official Chat documentation
distinguishes total `max_completion_tokens` from answer-only `max_tokens`;
an input tokenizer alone is not proof of exact streaming output accounting.
This checkpoint repair neither widens nor resolves those contracts.

## Review context

No implementation or new tests have been written for this repair yet. Source
reads and recorded Qwen captures established the two-counter discrepancy; the
PR404 tests validate that earlier slice only. No new Provider request, schema
change, Target rollout, cleanup or credential mutation is part of this review.

One independent read-only pre-code review must adjudicate this exact object.
Findings use the repository's five allowed categories. Nonblocking work remains
deferred; the wider accepted mission is not closed by this repair.

## Pre-code review adjudication

The independent review of design SHA-256
`addd94b3d8165f684576b0230730ecf5d649c5b9f95b507413bc5ef2581f7ede`
found no validated blocker. All three validated requirements are accepted:
separate non-force cadence assertions; a complete first frame followed by a
partial second frame; and actual exactly-once recovery rather than merely an
upper bound. The test fault wraps only real Billing.PersistSettlementUsage.
Begin, checkpoints, delivery receipts and recovery use the isolated PostgreSQL
implementation. Recovery checks a missing terminal payload and active lease
before expiry, frozen prices after a mutable price-row change, the original
attempt, one settle transaction and one terminal receipt after repeated recovery,
and rejection of the original lease fence.

No new estimator/schema is required: the producer now matches the existing
live counter and recovery contract. Existing rows remain unmodified. The byte
admission guard is deferred to its own design. The mixed stream fixture preserves
the existing inclusion of tool ID and function name along with arguments:
8 Latin characters, 4 CJK characters, and a 26-rune tool frame yield 2+3+7=12
estimated tokens across the three frames; the same text occupies49 UTF-8 bytes.

The unit and real PostgreSQL regressions have passed with race detection.
Transport, counter, output-guard, recovery and Provider-final-charge validation
passed67 top-level tests across Gateway, Billing and tokenutil with race detection.
The two existing Messages terminal-receipt cases still deliberately defer their
shared recovery assertion to the actual Responses case, which passed; they were
not newly skipped by this repair. No paid Provider call was needed.

The four new regressions also ran against the exact old relay producer as a
negative control: all four failed on the expected counter/cadence differences
(including49 instead of12 and8 instead of2). The fixed producer was restored
byte-for-byte afterward. Full local logs are
`/private/tmp/unirouter-checkpoint-counter-regression-20260907.log` and
`/private/tmp/unirouter-checkpoint-counter-old-producer-20260907.log`.
The CI integration selection already includes all `TestP6F11` tests, including
the new real relay-to-recovery regression. No CI weakening or added skip.

Model-adaptation learning closeout: 经验文件无需变化. This is an executable repair
to the existing stream counter/recovery invariant, not a new Provider lesson.
Historical rows retain their original values. No new Target/Agent claim.


## Closing review and exact commands

The sole independent closing review bound patch
`2e2603f7f7274e864bad928f8e76e5ee7600b19214e5cc64918ea1ec17ae4ede`
(30,649 bytes, nine files) to the pre-code design and actual test logs. It found
no unresolved blocker/required item. It independently verified the three
additional output-bound fixtures below. This closing record is the only
post-review documentation addition; implementation/test bytes are unchanged.

```sh
go test -tags=integration -race ./internal/gateway ./internal/billing ./internal/tokenutil -run '^(TestP6F11|TestP6StreamTransport|TestP6F41B|TestStreamTokenCounter|TestEstimateTokens|TestPublicModelExecutionAuthorityStreamFailuresPreserveFinancialOutcomeIntegration)' -count=1 -v
go test -race ./internal/gateway -run '^(TestStreamPublicHardCapCountsReasoningAndToolArguments|TestImplicitOutputLimitUsesExactOpenAICompatibleWireContract|TestPlannerRequestDefaultOutputLimitIsTheImplicitHardReservation)$' -count=1 -v
```

The old-producer negative control used the four new tests selected by
`^TestP6F11(StreamCheckpoint|RelayCheckpointRecovery)` against main's relay.go,
then restored the fixed file exactly. All four failed as expected.
