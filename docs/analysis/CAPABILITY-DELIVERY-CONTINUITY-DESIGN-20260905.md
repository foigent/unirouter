# Capability delivery and planned-change continuity

Status: exact pre-code review candidate; no implementation or deployment claim.
Source: main `0d2d14f80caddc162bc7598ce7f6572724ba203a`, tree
`999928d033148cea31d8a6669601b83a3976c76f`, version 1.2.51 / M113.

## Outcome and authorized boundary

Replace the high-cost model-onboarding and capability-delivery connections in
place. Preserve one database execution authority and historical identities.
An additional pure-data model using implemented wire contracts must be delivered
without DDL, capability SQL migration, backend rebuild/rollout, dated public
model identity, or Agent-specific code. Real OpenClaw and Hermes evidence stays
separate from fixture, protocol, answer-quality, billing and deployment evidence.

The user authorized continuous implementation, necessary independent review,
ordinary PR/CI/merge, development/test artifacts/publications/rollouts and the
previously bounded model calls. No Production, admin-account/session changes,
Token Plan, media/Computer Use, arbitrary fallback, Pricing lifecycle redesign,
new paid infrastructure, global traffic or remote bundle platform is included.
Existing per-exact-model request and CNY reservations cannot be reset. Missing
external credentials or environment access block their own live acceptance;
they do not justify fabricated evidence or block independent implementation.

The previously specified official-only supply-chain exception is separately
pending user approval. No product implementation depends on silently using it.

## Existing owners and changes

| Concern | Existing owner | Change |
|---|---|---|
| Profile semantics | `internal/modelprofile` | Immutable data definitions reference typed implementations |
| Capability publication | `internal/modelcapabilitypublication` | One delta materializer and separate append-only ledger |
| Release and Slot | `internal/runtimechange` | Exact execution pins across the complete runtime path |
| Canonical planning | `internal/modelrouting` | Read selected Release definitions and pinned qualification |
| Dispatch authority | `internal/modelexecution` | Preserve committed attempt owner and reject dynamic safety failures |
| Price/reservation/settlement | `internal/billing` | Reuse Freeze and its pricing identity; no second price authority |
| Process lifetime | `cmd/uniroute` | Admission, readiness, bounded HTTP drain and dependency shutdown |
| Ingress | existing Nginx/Compose | Explicit, versioned membership and observed old-worker withdrawal |

No ExecutionContractRevision, second production publication writer, Agent model
catalog, universal parser/compatibility DSL, generic workflow engine, Merkle
store or global cache-invalidation platform is introduced. Internal incompatible
wire formats receive honest new schema versions; the project label does not
constrain their version numbers.

## Execution authority and freeze

A sealed Release determines execution interpretation. Its canonical snapshot
fixes the Profile digest, operation/protocol and typed implementation references,
routes/limits, connection/deployment revisions, exact capability bindings, Probe
contracts, qualification/approval identities and referenced evidence digests.
Original observations remain under their own immutable facts, not copied into
another evidence database. Snapshot bytes or permanent immutable references are
retained with each digest. Shared Release data does not contain customer prices.

Authoring explicitly chooses an exact Profile revision and evidence. Same
ProductLineageID is necessary but insufficient: existing provider, access,
residency and customer restrictions still apply. Product lineage is explicit,
readable identity, not a hash of endpoint, operation or default parameters.
Those attributes belong to deployment/execution contracts. No generic lineage
rules language is needed. A compatibility decision is part of existing typed
authoring evidence rather than a separately mutable policy authority.

The complete change traverses authoring -> seal -> preflight -> planner ->
dispatch revalidation -> request evidence. A sealed new-format Release never
uses mutable registry Profile/limits or a current Probe selector to reinterpret
its contract. Registry/current-selector data remains authoring/bootstrap or
explicit legacy input. A dynamic safety restriction may deny an execution;
it may not substitute a different contract or silently widen/narrow the frozen
request interpretation. Limits that constitute a safety ceiling may reject a
request over the ceiling, not silently edit its outgoing payload.

The linearization point is the existing `Coordinator.Authorize` transaction
commit, which creates/claims the exact ProviderAttempt owner after revalidation.
Slot and safety changes ordered before this commit are checked under the domain
locks. After commit, the attempt keeps its execution identity, exact route and
effective request interpretation. Physical network send cannot participate in
the database transaction. Later revocation follows explicit cancellation policy;
the system must not claim an atomic check at the remote Provider socket.

On an unconfirmed commit, recover the same owned receipt. On upstream ambiguity,
do not mint another dispatch owner, automatically resend, invent a zero charge,
or report ordinary cancellation. Keep the existing pending/reconciliation path.
Confirmed unsent cancellation may release reservations according to billing's
existing contract. Local terminal settlement remains unique and idempotent;
universal exactly-once upstream execution is not promised.

RequestExecution records request ID, Release/snapshot digest, selected route,
runtime instance/artifact identity and the existing Freeze/pricing identity.
Actual amounts follow valid usage under those frozen rates and meter contracts.
Default consistency covers one HTTP request, not an entire Agent tool session.

## Definitions, runtime support and carriers

ProfileDefinition is immutable and content-addressed. Its schema contains the
existing pure Profile data (identity, lineage, provider/access product, limits,
defaults, documented capabilities, predecessor) and exact typed implementation
references. Mutable `Current` discovery metadata is excluded from the immutable
definition digest. No embedded credentials, executable snippets, arbitrary URLs
for loading code, or model-name-based parser selection are allowed.

The RuntimeSupportManifest is deterministically generated from actual registered
typed implementations and finite supported definition/materializer/schema
versions. It is immutable per artifact. Compatibility checks use requirement
set inclusion; digest equality only identifies objects. Valid individual IDs
do not authorize arbitrary compositions: the existing typed composition
validator must accept the combination. Unknown referenced versions/fields or
implementations fail closed. Unreferenced candidate data must not make an old
runtime fail global startup if its storage envelope is safely ignorable.

Runtime resolves exact definitions from the database, optionally caches by
digest, verifies bytes against digest and never substitutes a current revision.
Legacy compiled definitions remain explicit immutable bootstrap/history data,
not a startup writer or a second current-value authority. No requested new
data-model path may depend on editing `modelprofile.Lookup` catalog entries.

The first non-binary carrier is a bounded content-addressed Bundle file through
the existing controlled artifact transfer channel. A Bundle contains portable
definitions/deltas, schema/materializer versions and support requirements.
Source/provenance approval binds its exact digest to an authorized publisher.
The Target-specific Plan binds that Bundle to target/environment identity,
database predecessor and concrete local IDs. Target-specific identity is not
mixed into portable Bundle content. Publisher authenticity does not grant
Target mutation authorization. Embedded bundles remain bootstrap/recovery
carriers into the same materializer. No remote bundle management platform.

## Schema bridge and publication transaction

Historical M100-M113 files and receipts remain untouched. The bridge accepts
only an explicit finite matrix of legacy exact schema head and the reviewed
additive expand head, before initialization and HTTP bind. It never accepts an
arbitrary higher head. All potentially receiving instances must run the bridge
before the expand migration is applied. Large backfill is not part of DDL.

The expansion adds only structural storage needed for the separate capability
ledger, immutable Profile definitions, new Release pins and runtime support /
membership coordination. Existing Release, Slot, receipt and billing tables
keep their owners. New schema objects have explicit append-only constraints,
unique identities, FK/validation and guarded rollback requirements.

Each Target import records its actual legacy-through head and canonical folded
static projection digest in one new import event. It neither fabricates old
publication receipts nor assumes every Target has M113. Old authoring and new
authoring are mutually exclusive per Target under a durable ledger epoch and
database write enforcement. An application feature flag alone is insufficient
to stop an unaware old writer.

Plan schema v2 contains publication ID, expected predecessor, Bundle digest,
materializer version, canonical before/after static projection digests, binding
and selector delta, bounded deployment association delta, artifact/Target
identity and plan digest. It excludes whole historical ledgers, copied static
rows, historical NoOps, heartbeat, usage, later Probe facts and pricing rows.

Apply performs, in one transaction and existing domain/table lock order:

1. Revalidate Target, authorization, schema/authoring epoch, materializer and
   exact predecessor/Plan/Bundle identities.
2. Recompute the complete publisher-owned static projection from the real DB.
   Compare its canonical before digest; no digest cache is an authority.
3. Check that the delta cannot affect an active/selected/in-flight legacy
   Release which still follows a mutable selector. Never migrate it by guess.
4. Append immutable definitions/bindings and CAS explicit candidate selectors.
   Only traffic-neutral deployment associations may change, with row-level CAS.
   Never mutate qualification, safety status or execution revisions used by
   active Release dependencies merely because the before revision matches.
5. Recompute the canonical complete static after digest from the DB and compare.
6. Append typed capability ledger receipt and commit once. Slot is unchanged.

Every publication is DML-only. Existing V1 `ApplyProjectionPlan` drops/recreates
projection guard triggers and therefore cannot be reused unchanged online.
Guarded authorized DML paths are established at schema-expand time; the daily
writer cannot disable triggers or invoke DDL. Publication publishes contracts;
Probe/Approval creates new evidence; Release authoring binds exact existing
decisions. Observation success is never synthesized by a Bundle.

Commit ambiguity reads the same publication ID/Plan digest/receipt. Exact
committed replay returns the existing receipt; mismatched replay conflicts.
Compensation is a new append-only publication, validates the then-current
projection and may not remove data still referenced by active/historical
objects. It is not receipt deletion or blind inverse SQL. Binary rollback is
allowed only for a runtime supporting current schema and allocated Releases.

## Activation and ingress membership coordination

Three separate decisions are enforced:

| Action | Required compatibility |
|---|---|
| Publish inactive definitions | Materializer/storage can understand, old serving code can safely ignore |
| Activate/rollback Slot | Every potential receiver supports all Releases it may newly receive |
| Join generation to ingress | Joining generation supports current allocated Releases and schema |

Active requirements are derived from Slot/Release authority, optionally recorded
as a revision-bound projection, never independently editable. One database
coordination lock/CAS domain serializes allocation and ingress reservation.
Support is looked up by immutable artifact manifest. Leases carry instance,
artifact, compatibility epoch, state and TTL rather than a model catalog.

The compatibility set contains pending ingress joins, serving instances and
draining instances whose old proxy workers may still forward requests. Join
reserves membership under the database lock before external Nginx mutation.
The ingress writer uses versioned immutable desired configs and refuses a stale
generation/CAS. Only one controlled ingress writer may apply a revision. The
database cannot transact a Nginx reload: observed success/ambiguity is recorded
and recovered by config/worker readback. Unknown results retain the conservative
member set; they cannot remove an instance from activation checks.

Withdrawal stops new ingress assignment, then observes every old proxy worker
which could reference the backend. Only after those workers/connections have
ceased and the backend admission fence is confirmed may membership be removed.
Lease expiry alone does not prove withdrawal. An expired/unrenewable instance
self-fences for safety; any resulting client failure is recorded as continuity
failure, not hidden by upstream retry. Delayed joins cannot reapply stale config
after an incompatible Slot activation.

## Same-schema continuity: first implementation slice

This slice does not modify Publication/schema/Release. It provides an explicit
process instance identity, bounded HTTP lifetime, synchronized admission state,
readiness and dependency-ordered shutdown, and a bounded existing-Nginx/Compose
withdrawal procedure. Database compatibility leases join at the bridge slice;
an in-memory lifecycle is not represented as fleet compatibility authority.

The process lifecycle has serving -> draining -> stopped. SIGTERM/SIGINT starts
the admission fence and makes readiness false; already accepted handlers keep
their contexts and dependencies until completion or the explicit deadline.
Admission and active-handler accounting are synchronized to avoid a handler
entering between the zero-count check and fence. Health reads remain available
while draining. A generated instance ID plus configured build/image metadata
is exposed in safe health/runtime evidence. No public unauthenticated write
endpoint can trigger drain. Readiness also retains the current DB/Redis checks.

The load-balancer procedure precedes that signal:

1. Validate a healthy same-schema surviving/new backend, exact images/Compose
   chain and the supported-operation lifetime; refuse loss of last healthy
   backend or insufficient declared capacity.
2. Generate and syntax-check an exact Nginx config withdrawing the selected
   backend from every referring upstream; preserve TLS, header, route and
   requested model semantics. Record pre-state and config digest.
3. Gracefully reload Nginx. Observe new worker generation and retain the old
   backend while any pre-reload worker can forward buffered/accepted requests.
   Check process start identity as well as PID; a mere successful reload command
   or absence of new access log entries is not proof of withdrawal.
4. After old workers finish within the contract, fence/drain the backend and
   stop it. The other backend continues service. Replace the selected replica
   with the exact new artifact, verify readiness, then rejoin explicitly.
5. Repeat for the other replica only after the first is healthy and eligible.

Initial transition from today's non-draining binary is a documented bootstrap
boundary: no claim that an already running old binary acquired new semantics.
M110->M113 legacy schema change uses its existing maintenance contract; it is
not falsely counted as the new same-schema continuity proof.

Configure a finite accepted HTTP/operation lifetime that covers supported typed
provider timeouts, body/queue handling and post-provider finalization. Nginx
worker shutdown must cover ingress buffering plus accepted request lifetime;
application drain must cover admitted lifetime plus settlement/exit grace;
Compose stop_grace_period must exceed application drain plus persistence/exit
margin. Do not rely on the old uniform 120 seconds or Compose defaults.
Expiration explicitly cancels outstanding request contexts and closes remaining
connections after the deadline, retaining ambiguous upstream/settlement facts.
No detached TimeoutHandler buffering, broad proxy retries or second upstream
attempt is introduced to conceal failures.

HTTP handlers complete before stopping settlement recovery, Connector/outbox
delivery and shared dependencies. Workers stop according to responsibility:
stop admitting unrelated new background jobs, finish or durably release this
instance's owned work, then stop consumers/dependencies. Do not wait for global
outbox emptiness. Timeout paths log accurate unclosed responsibilities and run
cleanup instead of `log.Fatal` bypassing defers. Existing durable claim/lease
and idempotent settlement recovery remain the recovery authority.

## Delivery ordering and evidence

Retain the agreed A-H slices without introducing another roadmap:

A. Exact design, independent read-only review and adjudication before code.
B. Same-schema continuity and dependency shutdown; no publication change.
C. Finite compatibility bridge, structural expansion, per-Target legacy import.
D. Full Release pinning chain and honest equivalent active Release successors.
E. DML-only non-activating publication and non-binary Bundle carrier.
F. Symmetric activation/join checks and combined continuity validation.
G. Same-backend pure-data model with real qualification and Agent evidence.
H. Retire old writer after two real batches and verified migration conditions.

M2.7 and GLM-5.3 exact fixtures remain behavior-equivalence representatives.
Historical evidence which cannot reconstruct exact pins stays legacy. Online
publication is permitted only when its affected legacy dependencies are absent
or explicitly replaced via a new sealed Release and Slot action. Do not require
rewriting all historical Releases. Retiring obsolete code requires no active,
in-flight or promised-rollback references; permanent history readability does
not promise permanent historical executable support.

### Retirement scope and evidence (2026-09-06)

This elaborates slice H and directly related consolidation during earlier
slices. Source inspection is bound to `18f5e675ce94babf94af07b3420df3c413070255`
(1.2.56/M114). It confirms the entry points below, not absence of live consumers;
no Target inventory, migration completion or code removal is claimed here.
The objects are units to evaluate, not an approved bulk file-deletion list.

| Retirement unit | Replacement or retained authority | Observed dependency and remaining users to establish | Exit evidence |
|---|---|---|---|
| V1 publication authoring/execution: `cmd/uniroute/capability_contract_publication_cli.go` (`plan`, `apply`, `guarded-down`), `internal/modelcapabilitypublication/plan.go` and writer portions of `apply.go` | `plan_v2.go`, `materializer_v2.go` and the append-only `ledger_v2.go`; corrections use a new compensating Publication | Both CLI generations remain reachable in source. Establish the affected supported Target/import set, operator workflows and promised pre-import rollback needs. The M114 writer fence proves mutation exclusion, not implementation removal. | Keep H's two real data batches and verified migration conditions. Show that no supported daily-publication or rollback workflow needs each removed entry point, and preserve stale/legacy-writer rejection, replay/ambiguity handling and migration guards. Do not silently alias legacy down to compensation. |
| New capability SQL artifact generation: `cmd/uniroute-capability-contract-generate/main.go` and `WriteGeneratedMigrationFiles` in `generator.go` | Reviewed `capability-bundles` data, `scripts/prepare-capability-bundle.py` and the existing publisher | The generation command still calls the file writer. Inventory build, test and supported bootstrap/recovery callers; historical files remain immutable. | Prove the supported new data-delivery path no longer needs generated capability SQL; account for remaining callers before removing the command or writer. Retain required historical verification independently. |
| Shared historical verification/bootstrap: `VerifyGeneratedMigrationFiles` and `GenerateMigration` in `generator.go`, `runtime.go`, and `install-baseline` | Minimum sufficient historical artifact/projection verification and supported installation/import paths | `cmd/uniroute/main.go` calls `VerifyArtifactPublicationInventory`, which still calls generated-file verification; `plan_v2.go` still folds the legacy projection for import. Whole-file deletion would remove active dependencies. | Preserve the runbook's historical-reader acceptance gate and equivalent integrity, drift-rejection, startup, installation and import checks. Split duties before retiring unused generation/writer code; retaining history does not require retaining every authoring command. |
| Legacy Release/Profile execution resolution: `internal/modelrouting/profile.go`, `internal/modelexecution/revalidate.go` and the related authoring branch in `internal/runtimechange/deployment_release_authoring.go` | Exact definitions and sealed snapshots in `internal/releaseexecution`, consumed through the existing planning/dispatch owners | Source retains legacy Profile lookup and optional snapshot paths. Establish active/selected/in-flight Release/Slot references, supported predecessors and promised rollback consumers; missing Target access is not evidence of zero users. | No affected active, in-flight or promised-rollback references; any required successor Release/Slot transition must be covered by the current request or existing explicit authorization; do not request the same approval again. Preserve historical readability, exact request interpretation and billing/evidence identities. |
| Transitional Compose overlays and deployment wrappers | The existing reviewed effective Compose configuration and formal entry points, including `runtime-ingress.py`, `compatible-expand.py` and `publish-capability-bundle.py` | The current effective Target chain and exact redundant files still need inventory. Source filenames or a historical layer count do not establish redundancy. | Identify exact overlays/wrappers and consumers, then prove equivalent configuration, ingress withdrawal/drain, migration ownership, provenance and supported rollback. Retire only redundant resources within authorization; do not replace working formal owners with another orchestration system. |

Update this table with exact consumer and exit evidence as implementation
progresses; use the existing task report for transient details rather than
creating another ledger. Independently removable redundant logic may be retired
in earlier slices after its consumers and relevant checks are accounted for;
this does not waive H's old-writer gate or the historical-reader safeguards in
`docs/ops/CAPABILITY-DELIVERY.md`.

A deferred model, including Qwen, blocks only retirement units with a demonstrated
dependency on its supported consumers or an applicable accepted proof. Existing
model batches may prove the shared delivery properties they actually exercise;
they do not replace missing pure-data-model, Provider/Agent or whole-mission
acceptance. Keep those evidence claims separate and retain the agreed call budgets.

Report new-path availability, supported migration and implementation retirement
separately. Close each slice against its accepted criteria; close the overall
replacement only after its agreed retirement conditions are met. Scope-external
cleanup is not a new gate, and net deleted lines are not an acceptance metric.

Acceptance tests are behavior/failure tests, not copies of implementation:

- A real HTTP/SSE stream and short requests during a Nginx worker reload and
  backend replacement: no change-induced 5xx, interruption within lifetime,
  duplicate dispatch or duplicate terminal settlement.
- Cancellation/deadline after provider-send ambiguity retains proper pending
  evidence; worker shutdown never destroys confirmed settlement responsibility.
- Admission/fence race accepts once or refuses before work; no negative/leaked
  active counts; health remains readable, readiness reflects draining.
- Slot change or safety revoke before authorization is rejected/replanned under
  existing idempotency; committed attempts preserve owner and interpretation.
- Selector/static publication changes cannot change new-format old Release's
  admission, outbound parameters, dispatch revalidation or audit pins.
- Affected unresolved legacy Release blocks publication before mutation.
- Concurrent delayed join/Slot activation, stale ingress config, draining with
  old proxy still alive, expired lease and ambiguous reload remain fail-closed.
- Same Bundle bytes promote between Target-specific Plans; modified bytes,
  unknown implementation, forbidden field and unauthorized origin are rejected.
- Transaction before/after digest drift, duplicate command, commit ambiguity,
  concurrent projection CAS and compensating publication have exact outcomes.
- All supported schema pre-states, additive bridge, foreign/future/drifted
  ledgers and dry-run/check paths are exercised in isolated PostgreSQL.
- A real pure-data model works without changing backend image/instances; a
  separately started instance of that same image resolves it from durable data.
  Fixtures alone never count as that real-model/Agent proof.

## Review object and current evidence

Review binds this complete file digest, source commit/tree above and authorized
scope. Known blockers: supply-chain exception awaiting response; independent
new-model/Hermes credentials not yet established; Target currently M110. No
product tests run for this design yet. Existing code paths were read; static
inspection is not test execution. Reviewer reports only validated_blocker,
validated_required, valid_deferred, rejected or product_decision_required,
without modifying files or acquiring external authority. Main agent adjudicates
each finding and keeps the exact reviewed bytes. At most one closing review
is bound to implementation diff, prior findings and actual test results.
