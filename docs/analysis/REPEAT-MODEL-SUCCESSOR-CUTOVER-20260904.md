# Repeatable stable request identity cutover

Status: pre-code design; no implementation or Target claim.

## Exact object and purpose

Authority is the current repository successor lifecycle at base
`f4a0acce2a6e35038cb6eaf3b756c655a0e57438` / tree
`49e212eefc536ff1b27d89e0428c5e29eb5098ff`, including M107 and M109.
The reviewed client-preflight object (`ce9a5dce151ac79ec62311eed06274c801916b95`)
does not change these lifecycle contracts. The separate unpublished M111
stream-tools candidate is preserved. This design does not reopen its review.

Product outcome: a stable user request name can move from its current immutable
execution identity to the next qualified identity repeatedly, without a second-
or third-upgrade special case. This is required before the M2.7 stream-tools
candidate can be activated; it is not a PublicModel redesign or an Alias editor.

## Compatibility and minimum change

- Extend the existing `ApplyModelSuccessorCutover` domain command and normal
  successor admin endpoint with an explicit `primary_alias` predecessor mode.
  Existing published-first and never-activated-staging modes remain unchanged.
- One new parent/child receipt version and fingerprint domain identify the new
  before-state contract. Existing request serialization and fingerprint domains
  remain byte-compatible: new optional fields are omitted on historical modes.
- Additional expected before-state is limited to primary Alias ID, predecessor
  slot version and predecessor stable Release ref. The existing stable name,
  predecessor/successor IDs, lifecycle/catalog flags and target sealed Release
  ref/version remain authoritative. No new revision column or routing layer.
- Freeze the observed Alias-before and predecessor execution snapshot in the
  new receipt. Reuse the existing immutable parent, child, model-set children,
  audit and recovery mechanism; no separate ledger or new current-pointer table.

## Preconditions and transaction

1. Before the transaction, the new identity may have Deployment, Probe,
   approvals and a sealed Release, but no executable Slot. No Provider I/O is
   performed inside the command.
2. Reuse current transaction isolation, writer-lock ordering and receipt claim.
   Historical idempotent replay is resolved before requiring current-state
   equality. An old replay returns the old receipt and never retargets the Alias.
3. Lock current primary Alias and old/new registry/Release/Slot states. Require
   the Alias ID/name/org/active/primary/target to match exactly; current target
   must equal the specified predecessor. No secondary active aliases may expose
   the new target. Reject wrong organization, missing/drifted source, active
   canary, target Slot, stale Release/version or noncanonical Release.
4. For repeat mode require the current and new sealed Release pins to use the
   same nonempty execution Profile ID (versions may differ). Both exact Profiles
   must Lookup successfully and must not be `UnverifiedProfileID`, which skips
   product identity validation. Reuse existing product-binding validation for
   the sealed route identities. This admits a governed revision of the same
   model product, not arbitrary custom upstream switching or current-only pins.
5. Existing stable-Alias model-set references are left untouched. Any explicit
   predecessor canonical model-set items are handled by the existing exact
   expected set snapshot, converting only those items to the stable Alias.
   No broad permission expansion or rewrite of unrelated project/key scopes.
6. Insert immutable new receipt evidence, activate the target's initial stable
   Slot using the existing domain transition, retire/unlist the predecessor,
   promote/list the successor, and conditionally retarget the primary Alias in
   one short transaction. Audit both before and after states. The final checks
   require the Alias to resolve to the exact active sealed target Slot.
7. The old Slot and Release evidence are retained. Retired governance prevents
   new public dispatch to the old canonical; no live historical record is
   rewritten, deleted or resurrected. A future rollback would need its own
   explicit qualified forward lifecycle, not a silent Alias reversal.

## Database protection

Use one append-only schema migration after the reserved unpublished M111 (the
exact final filename/ceiling is checked during integration). It changes only the
existing receipt constraints and successor protection functions needed here.

- Ordinary Alias CRUD still cannot update/delete primary Alias authority.
  Retarget is permitted only with its exact in-progress new-version parent and
  child whose recorded before-state matches the old Alias.
- Registry IDs and completed receipts remain durable. An identity that was a
  historical successor may become retired only as the exact predecessor of a
  valid in-progress repeat cutover. Once it is a completed predecessor, revival
  or re-listing remains forbidden. Current successor stays GA/listed until that
  controlled transition; history is not confused with permanent current state.
- Parent completion validates new Alias/Slot, identity, model-set delta and
  audit atomically. Historical v1/v2 child meanings and existing receipt replay
  are not reinterpreted against today's Alias target.
- Down refuses if durable new-version receipts exist; with none, it restores
  the actual supported prior constraints/functions. Test an existing A→B state,
  not just a fresh database. Do not modify M107/M109 or applied Publications.

## Execution race and recovery

Current coordinator locks/revalidates registry and Slot before authorization.
A request already authorized and frozen keeps its original execution identity,
usage and receipt. A merely prepared old plan crossing cutover fails normal
revalidation before dispatch; no exception allowing arbitrary retired models.
Test this boundary through existing coordinator facilities.

Pre-commit failure leaves old state intact. Commit outcome unknown uses the
existing exact receipt readback; no blind retry, new idempotency key or automatic
reverse cutover. Post-commit diagnostic failure leaves the committed new state
and reports its receipt for forward repair.

## Verification and delivery

Required targeted tests: historical published and staging first cutovers;
A→B→C→D; old A→B and B→C replays after D; concurrent competing next targets;
wrong Alias ID/target, cross-org, wrong Profile lineage, unsealed/new Slot,
drifted old Release/Slot, canary, direct canonical rejection, stable directory
uniqueness, unchanged Alias-only grants, exact canonical model-set conversion;
pre-commit failure/commit-unknown recovery; stale prepared vs authorized request;
upgrade/down with real supported historical state and refusal after new receipt.

Use isolated local PostgreSQL and deterministic fixtures, zero Provider calls.
One pre-code review and at most one closing review apply to this new object.
Integrate source version, schema ceiling, migration runner/embedded bytes,
OpenAPI and existing lifecycle docs as actually affected; obey release gate.
M111 and this migration must have one self-consistent cumulative order before
CI/Artifact, with no historical Publication replay or premature Target action.

The user's fourteen-model task and cumulative CNY10/30 upstream-request caps
remain open. No new Agent SDK, execution platform, Production, Pricing, Token
