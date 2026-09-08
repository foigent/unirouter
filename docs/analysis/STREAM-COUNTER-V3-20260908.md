# Versioned cumulative interruption estimate — exact pre-code object

2026-09-08. Baseline main07761d7371299a4e435ffd5f4b838e77672d6ad9,
tree8c7d16849ecc0822934beb704969b31dc2cb175b. PR410 head6847fcfd
concentrated qualification changes no metering, schema, relay or recovery code;
rebase on its exact merge before delivering this separate platform slice.

## Accepted outcome and exclusions

The user accepted correcting the chunk-sensitive interruption estimator after
Qwen v8 and concentrated onboarding. Preserve marked estimated charging, current
rates, provider-final priority, output/resource contracts, budgets and historical
bills. No new Provider/model, price lifecycle, generic metering/workflow platform,
automatic retry or inferred Provider exactly-once guarantee. This is a shared
platform correction, not a model data batch, and must not claim no backend build.

Current AddOutputDelta rounds each extracted text frame independently. hello
once is2; five single-letter deltas are5. Live interruption and checkpoints consume
this counter; crash recovery hardcodes counter_v2. M31 CHECK constraints also
allow gateway_counter only with unirouter.stream-counter.v2. Merely changing the
formula or label is insufficient and would silently reinterpret recovery.

## New arithmetic; preserve actual scope

Introduce explicit counter_v3 / unirouter.stream-counter.v3. Cumulative rune
weights: existing CJK class8 units/rune and other rune3 units; token estimate is
ceil(totalUnits/12), using checked integer arithmetic. Keep the existing CJK
classification and input estimate algorithm; change only cumulative rounding of
successfully locally delivered extracted output text. Accumulate bounded counts,
not response text. No per-frame ceilings. Same concatenated valid decoded text
has identical estimate under arbitrary legal stream partitions, including mixed
CJK/Latin, whitespace, emoji/combining runes and tool argument strings. Wire
UTF-8/SSE fragmentation is handled by the existing parser; do not invent a token
parser. Unit tests distinguish arbitrary wire fragments from invalid partial
UTF-8 strings passed directly as already-decoded text. A failed/partial write
retains existing disclosure rules; do not newly count bytes never represented by
a full accepted frame. This is still an estimate, not a supplier tokenizer or a
cap on hidden reasoning/billable tokens.

Legacy v2 constructor/implementation remains available for legacy live leases,
fixtures and recovery. New constructor requires a known explicit version.
Unknown version is an error, never normalized to v2. Final provider usage remains
its own authoritative path and is not added into the delivered counter.

## Persist one immutable version per stream, not a second usage authority

M115 is a real additive structure/constraint migration, not capability SQL.
Add counter_version to stream_delivery_leases, defaulting old/new unspecified
legacy rows to exact v2. Pin it when BeginStreamDelivery creates the lease, before
any model bytes. A trigger prevents changing it with owner/generation/state
updates. Checkpoint rows belong to this lease by existing FK, so do not duplicate
version per checkpoint or store full estimator text/state. Checkpoint commands
carry expected counter version and verify it under the existing lease lock;
monotonicity/idempotency include the version. Recovery joins the immutable lease
version, uses the exact persisted token totals and corresponding source/version,
and never re-estimates historical content. Unknown/mismatched versions fail
closed without debit or Provider dispatch. Old checkpoint counts remain untouched.

Extend only the existing gateway-counter CHECK alternatives in request_logs,
usage/billing tables discovered from exact M31/current schema to permit the two
known consistent versions. Preserve all other authority/finality/purpose/cache
invariants. Validate complete current constraints, not just M31 text. Do not add a
permissive nonempty-version clause. Canonical APIs, metering, adapters, Billing,
receipts, replay/analytics classification and recovery accept v2/v3 explicitly.
Provider-final hold remains excluded from checkpoint auto-settlement.

The shared Begin/Checkpoint/Settle owners verify matching lease/source/version
under the existing financial fence. A caller cannot settle v3 counts under v2
by supplying an arbitrary SettleRequest. Complete streams with provider-final
usage may correctly use provider authority instead of the estimate; the lease
counter pin is an estimate identity, not a mandate to charge estimated usage.

## Safe finite rollout; one image supports both versions

Preserve historical M100–M114 and capability Publication authority. Extend the
finite schema bridge from supported full M113/M114 ledgers to the exact known
M115 suffix as needed for existing supported bootstrap paths; no arbitrary
future heads or number-only checks. Runtime lease, ingress join, publication,
Release activation and formal rollout must accept only that finite full ledger
matrix. M115 is executed solely by the existing schema owner after proving the
serving AND background-recovery fleet supports both counter versions. Add an
exact stream-recovery implementation reference generated by the runtime registry;
manifest digest is identity, membership is compatibility evidence. The M115
precondition is checked under the existing compatibility lock and also against
actual development/test process/container inventory. Old binaries are stopped
before expand; after expand they cannot restart against unsupported schema.

A single bridge artifact supports v2/v3 reading and recovery but defaults its
new-stream writer to v2. A precise runtime configuration selects v3 only after
M115; v3 with M114 fails startup/readiness, never dispatches first. The existing
formal rollout carries this configuration, not a new control-plane writer.
Sequence: build one exact bridge artifact -> same-schema rolling/drain with v2
on stable and canary -> confirm all old processes stopped -> additive M115 via
formal schema owner -> same-artifact rolling configuration change selecting v3.
Both v2 and v3 bridge processes can recover either persisted version during the
second roll. Freeze version at stream creation; in-flight v2 remains v2. Reverting
writer config affects new requests only. Binary rollback is limited to artifacts
that support current schema and all persisted recovery versions. Do not promise
rollback to pre-bridge72 once M115/new v3 records exist.

M115 down refuses if v3 leases/usage exist; no deletion, bill rewrite or downgrade
of versioned records. Keep forward recovery as normal. Since this is reversible
writer selection and additive schema, no new product or price decision is
required within the existing development/test architecture authorization.

## Required tests and real acceptance

- Arithmetic fixtures hello/CJK/mixed/emoji/tool-text; partition property tests
  and bounded overflow; v2 historical fixtures unchanged.
- Actual relay SSE frame partitioning and guardrail batch sizes yield the same
  v3 delivered estimate; partial write/cancel and final usage precedence.
- Real isolated DB: migrate exact M114 with historical v2 lease/checkpoints;
  historical rows/counts/receipts unchanged; v2/v3 lease/checkpoint/settlement
  match; mismatches rejected; one financial terminal under replay/concurrency/
  stale ownership/commit ambiguity; held provider-final evidence never estimated.
- Expired lease recovery produces the same source/version/counts as live cancel;
  old v2 recovery still v2; unknown version/unsupported downgrade fail closed.
- Compatibility: bridge onM114 writesv2; v3config rejected; M115 expand rejects
  any old recovery/serving member; bridge reads full valid ledgers only; new
  version survives configrollback; migration check/dry-run has no side effects.
- Exact CI/Artifact/Target readback separated. One authorized bounded cancellation
  may validate real v3 if remaining per-model budget permits. Forced crash/overrun
  stays isolated local integration unless separately necessary and authorized.
  No need rerun every already-proven normal Agent task or invent model revisions.
- Review the exact schema/config design before code; one closing review after
  stable patch. Report accepted new path, historical compatibility and retained
  v2 reader, not blanket retirement or provider tokenizer accuracy.

## Review focus

Validate whether the single immutable lease version is sufficient for all live,
checkpoint, recovery and settle paths; whether any current constraint/normalizer
can silently relabel v3; and whether existing schema-owner/fleet gates can express
the finite expand sequence without a second writer or unsafe old-worker window.
If a simpler existing immutable field genuinely owns this meaning, identify it
with call/constraint evidence; do not overload guardrail policy_set_version,
pricing identity or final provider usage to avoid necessary schema work.

## Pre-code clarifications from verified bridge call sites

Current CHECK owner is M56, covering freeze_idempotency, call_logs,
usage_finalization_journal AND request_idempotency. Retain each table's distinct
usage.v2 local-cache alternative. Existing history is not restamped.

M114 has no lease counter_version column. Begin/Checkpoint/Recover/Settle must
explicitly select the old SQL plus implicit historical v2 on the validated M114
ledger, or version-aware SQL on exact M115. A v2 writer default alone does not
make a query for a missing column safe. Lease creation receives an explicit known
writer version, while recovery ignores the current writer setting and uses the
persisted lease version. A counter settlement rechecks the immutable pin and
counts under its existing fence. This per-stream estimate choice is recorded as
metering identity; it does not alter sealed Profile/Protocol or provider usage.

RunCompatibleExpand and its CLI are currently M114-specific. Extend the SAME
schema owner with a finite target selector/M115 hooks; never call the M114
BootstrapVerifiedBridge INSERT for already-registered generations. M115's
in-transaction checks use the existing compatibility lock after its normal
schema-owner lock, and recheck the complete known predecessor plus all registered
potential serving/recovery actors against the supported manifest. Target check
also accounts for actual stopped old containers/processes and absent/draining
members, not only the two live ingress ports. Startup may not start financial
recovery before its artifact/schema compatibility has been verified and its
required actor registration succeeds. Lease loss stops new recovery claims;
already-owned work remains fenced. Unknown or unregistered recovery actors are
not assumed safe merely because ingress is clear. The supported single-host
process inventory must be explicit; no cluster membership platform is added.

The existing formal rollout owner refuses any migration tree difference and
any config delta beyond Artifact identity. Extend it with exactly two named,
validated transitions, preserving the normal default restrictions:
(1) pending M115 carrier: new artifact differs only by the precise reviewed
additive migration pair while actual DB remains M114 and complete existing
migration bytes are identical; ordinary rollout still never applies DDL;
(2) same bridge image writer config v2->v3 (or v3->v2): only the designated
STREAM_COUNTER_VERSION enum changes, requires exact M115 for v3 and both-version
recovery support. Retain predecessor digest/CAS, storage, backup, plan/check,
explicit ingress withdrawal, old-proxy wait, application drain, final health,
protected Release/Slot/Publication equality and commit ambiguity readback. No
manual overlay bypass. Normal arbitrary schema/config changes remain rejected.
Migration check/dry-run may neither bootstrap/upsert generations nor modify
writer configuration. Configuration rollback preserves all v3 lease/receipt
identity and uses a bridge binary capable of recovering them.

Additional exact compatibility/replay boundaries: on the supported M113 bridge
predecessor there is no runtime_generations table. Keep v2 live-stream old SQL
for M113/M114, but do not start background financial recovery in the M113
transition; enable it after verified M114-or-M115 and successful runtime actor
registration. Recheck this gate before new recovery claims. A bridge process
started on M114 must not permanently cache the old SQL selection: after M115
commit, every recovery/lease/settlement transaction resolves the committed feature
head and reads the immutable pin, even when its own new-writer setting is v2.
The artifact support manifest remains byte-identical across config/schema changes
and includes both count/recovery implementations and its complete finite heads.

For v3-aware leases, perform counter source/estimate/pin agreement validation
before Billing.Settle's already-settled fast return, not only before a new debit.
Conflicting v2/v3 replay of a v3 lease is rejected without touching its receipt;
correct replay returns the exact historical receipt. Preserve supported legacy
v2 historical replay and amounts without re-estimation. PendingSettlement already
retains source/version; reuse that payload rather than adding another journal.

Pre-code adjudication: independent stream_checkpoint_precode_review found no new
product decision. Original bridge/rollout blockers are addressed by the exact
addenda; its M113/recovery-refresh/manifest/replay/M56 requirements are accepted
and will be implemented against the final design4921f65121909a54c75990713a414e3f1634a253a829440ade2e63ec5e5b9f82.
This entry records requirements, not an implementation or Target result.

Implementation verification: request_idempotency has no usage_source column. Its
M115 CHECK therefore admits only the two exact estimate versions; the existing
financial owner and the three source-bearing tables enforce source/version pairs.
Do not invent a duplicate source field or weaken other M56 branches.

The current generation ledger retains absent, expired historical rows; no normal
stop owner marks every such row stopped. M115 checks every ingress-associated or
unexpired recovery actor under the compatibility lock. The host owner additionally
holds the existing Docker lifecycle lock and checks all actual uniroute processes
against the exact two inspected containers. Thus expired/absent old actors are not
assumed stopped from TTL alone, while historical rows remain immutable evidence.


Local validation checkpoint (2026-09-08): core ten-package race, tagged billing
version/fence/hold-commit tests, M114→M115 isolated history/negative actor tests,
52 formal rollout tests and 42 affected frontend tests pass. Frontend production
build and bundle budget pass. The first full admin integration run found one
fixture still registering M114; its two fixture pins now follow the installed
exact schema/support set and the full data-first model-to-Gateway test passes.
Original failed runs remain retained. Complete Go regression passes (98 packages,
23 packages without tests); go vet also passes. The two historical Release/bridge
fixtures now follow their explicit installed or M113→M114 preconditions, and
their tagged race tests pass. No Target73/M115, writer activation, supplier
result or paid cancellation is inferred.

The single closing independent review completed against patch
f20aec007990b3ef6bf8bc930673f17bc13b6cbc0fdc6a43c957df3244950915
(204465 bytes,55 paths). No validated blocker or new product decision remains;
the pre-code requirements are satisfied. Independent real-DB race verification
had zero skips/failures and all52 formal rollout tests passed. A future combined
relay→Postgres v3 fixture is valid deferred coverage, not a current blocker;
actual relay and database recovery/version paths are separately verified here.
This closing record is documentation added after the reviewed patch; it changes
no implementation. CI, exact Artifact and Target acceptance remain separate.

Subsequent deployment preparation exposed a concrete online-M115 lock inversion
in initial commit7044411e: Settle locks its reservation before the stream relation,
whereas M115 altered the stream table before the reservation table. The isolated
concurrent test reproduced SQLSTATE40P01; its original failure is retained. This
corrects the prior no-blocker conclusion for the online expand boundary. The
same independent closing review validated the blocker and the minimal correction:
preserve Release→Slot→compatibility order but use NOWAIT/transaction try-lock,
then acquire all five financial DDL table locks with one NOWAIT command before
any ALTER. Busy state aborts only the migration through its existing transaction
owner. No Billing lock change, new writer, automatic retry or product decision.
Tests require the accepted runtime transaction to survive, unchanged schema and
ledger on refusal, all eight busy-owner cases, and successful explicit expansion
after contention ends. Target is still71/M114; no73 Artifact has been created or
sent, so no installed migration bytes are rewritten. The superseded first CI run
34171413688 was cancelled; the corrected commit requires fresh complete CI.
The corrected contention test passes, and the affected full real-DB race suites
pass: migrate31.394s, billing101.226s, publication40.669s, runtimegeneration28.473s.
No Target change or paid call was used to reproduce or correct this issue.
