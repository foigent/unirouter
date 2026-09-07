# First PublicModel creation from published data

Pre-code review object, 2026-09-07. Base main
`ce3b6f2cd7b4e5666a128d0a16bb8b76ee2bb353`, tree
`a7236a3882d5d657e4ff46e8d1203c6829866ef2`, 1.2.64/M114.

## Active milestone and evidence

Close the remaining first-model creation dependency on the compiled Catalog.
A genuinely absent model using already installed exact implementations must be
published and first created from an immutable Definition, qualified, sealed,
activated and used by OpenClaw without another backend build or DDL after this
platform delivery. The first platform correction itself needs an Artifact;
it cannot be counted as a no-build model batch.

Qwen3.7-Flash already completed two real HTTP200 streamed Agent rounds, three
successful reads and final stop through Release57 / Profile1.8.0. Both requests
have one settled ProviderAttempt, completed delivery, matching provider usage,
and a unique cost-observation receipt. The model's answer was incorrect. It was
already compiled, so this is protocol/application evidence, not first-model
industrialization proof. Budget9/30 and CNY0.805295/5 conservative reservation.
The exact evidence is retained separately; no repeat calls are needed for this
platform design. No full Task/quality or invoice success is inferred.

This is an additive completion of the accepted Capability Delivery and Platform
Onboarding designs, not a second registry/writer or a wider provider project.
No new provider parser, behavior DSL, Pricing lifecycle, Messages, media,
Production, Gemini/Hermes, or legacy-writer retirement belongs to this slice.
User-owned checkout and research files remain untouched.

## Confirmed holes and affected chain

- `normalizeCanonicalApplicationCreate` requires an existing PublicModel for a
  Definition and resolves any explicit Profile through compiled Lookup.
- `ensureCanonicalPublicModel` can create only after that normalization; the
  data Definition is loaded after the PublicModel has already been resolved.
- Candidate attachment and per-product Definition browsing first require a
  compiled bootstrap Profile. Release authoring repeats that dependency before
  it considers the requested exact Definition.
- Runtime planning and dispatch already support a digest-pinned Definition;
  qualification uses exact operation/Definition bindings. Preserve those owners.
- The default onboarding list contains compiled profiles. Both detailed and
  batch creation must preserve a selected published digest through submission.
  Updating only the backend creation endpoint would leave users unable to use it.

## Authority and minimal representation

Keep M114 unchanged. `model_profile_definitions` already uniquely binds
`(profile_id, profile_version)` to immutable digest, canonical bytes, lineage and
Publication. No new bootstrap table or mutable current Profile pointer is needed.
A first data-created model stores that exact Profile address and lineage in the
existing `model_registry`; its Deployment/Operation and Release retain the exact
Definition digest through the existing fields. The creation receipt already
binds the normalized input including the Definition digest.

Introduce one explicit bootstrap-authoring reader in `internal/modelprofile`.
An exact known compiled bootstrap retains its compiled source even after a
same-address Definition with a different lineage is published. Selected data
revisions still undergo byte equivalence and independent lineage checks. For a
bootstrap address absent from the compiled Catalog, require recorded lineage,
look up the unique immutable Definition at that exact address and verify
bytes/digest/index/lineage. Missing/corrupt data, mismatched lineage or failed
queries cannot become unverified or select a newer revision. Legacy M113 reads
of compiled profiles do not query an absent Definition table.
Do not change `ResolvePinned` to try arbitrary Catalog/database fallbacks.

The bootstrap reader is used only by candidate attachment, catalog filtering and
Release authoring. A sealed Release still supplies execution interpretation;
mutable registry state cannot override it. If a future binary also embeds a
published address, the published bytes must still be checked for equivalence.

## Single creation transaction

Extend the existing `/model-deployments` command; no parallel create writer.
When `deployment.profile_definition_digest` is supplied with `public_model.id=0`:

1. Pure normalization validates syntax, exact digest and Chat operation, but does
   not require Catalog membership or invent the unverified Profile. Optional
   explicit Profile ID/version are assertions against the selected Definition.
2. In the existing repeatable-read create transaction, load the published exact
   Definition before materializing the PublicModel. Resolve omitted limits from
   its existing default/ceiling helpers; reject explicit incompatible limits or
   inert per-deployment default overrides using the same rules as legacy create.
   A normalized input hash retains the supplied digest and optional fields;
   only the new `id=0 + definition_digest` branch preserves nil versus explicit
   zero for public/deployment limits, preventing an invalid zero-limit retry
   from matching a successful omitted-default receipt.
   immutable Definition data makes omitted defaults deterministic. Do not replace
   an already issued fingerprint/receipt format or rehash historical requests.
3. Validate actual provider, endpoint class, region/access surface, upstream ID,
   operation and protocol using `ValidateProductBinding`. Reused connection and
   credential still require existing actor/org/revision/membership checks.
4. Create-or-exact-reuse PublicModel, with exact Profile address and product
   lineage, within the same transaction as Deployment, Operation, declared
   capabilities, credential link, audit and the existing typed create receipt.
   Natural-name conflict is an assertion conflict, never an update. Explicit IDs
   retain current assertion semantics. A rejected command rolls back all rows.
5. No Probe, Approval, Release or Slot is created by this command. Reuse the same
   idempotency key for ambiguous commit recovery; no guessed connection/key reuse.

Data revision attachment to existing models uses the same bootstrap reader and
SameStableProductSurface/lineage check. This slice does not widen those existing
product-compatibility decisions or replace them with a DSL. Cross-product and
retired-model negatives must keep their current behavior.

## Catalog and user path

Extend the existing organization-read Profile API with an explicit published
source query, preserving the default compiled response and existing stable-model
query. The published query reads only formally materialized Definitions; it has
no upload/write/qualification path. Use existing bounded pagination with stable
ordering and optional provider/upstream filters. Return the exact digest and
lineage with Profile data. Unsupported schema or invalid rows fail visibly;
unknown runtime implementations are not advertised as executable choices.

Both onboarding sheets consume the same API/client helpers and match reviewed
Definitions against the chosen preset/connection surface. Published revisions
are explicitly selectable by digest/version and remain candidates; do not set
`current=true`, choose the numerically latest revision, infer tools qualification,
or silently replace an existing user selection. Handle paginated results and
stale/failed catalog loads without falling back to an unverified submission.

For a new model, copy the selected exact Definition digest into the ordinary
create payload and display the existing next steps. Batch creation keeps one
connection/key, per-model identities/results and the existing exact retry logic.
No extra wizard, visual redesign, custom Agent catalog or automatic paid probing.
Update OpenAPI and generated API documentation/types where this contract changes.

## Release and runtime verification

Release authoring resolves the existing bootstrap through the shared reader,
then validates the explicitly selected Definition against lineage/product and
limits, exact routes and qualification decisions. Old Release rows are not
modified. Unknown bootstrap or Definition is a conflict, not an implicit upgrade.
Audit all reads from authoring through Probe/qualification, sealing, startup,
preflight, dispatch and evidence with a genuinely uncompiled fixture Profile ID.
Patch only remaining reachable dependencies necessary for this chain; do not
rewrite unrelated legacy model CRUD or successor behavior.

Runtime compatibility membership and Slot activation checks remain mandatory.
Current revoked credentials/connections, permissions and budget may still refuse
execution. Publication does not alter active Releases or grant capability proof.
Per-request consistency remains the promise; an Agent session is not pinned as a
whole. Pricing uses existing Freeze authority and metering/settlement boundaries.

## Tests and accepted compatibility limits

The following acceptance criteria were frozen before implementation:

- Real migrated PostgreSQL test using a Profile ID absent from compiled Catalog:
  Publication/Definition -> HTTP create -> exact replay/conflict -> Operation and
  capability Probe fixtures -> independent Approval -> sealed Release/Slot ->
  real Gateway preflight and dispatch with transport fixture -> usage/settlement.
  Fixture facts are never reported as Provider evidence.
- Missing/unpublished/corrupt digest, asserted-address mismatch, foreign org or
  binding, product/endpoint/upstream mismatch, invalid limits, retired identity,
  changed natural-key fields and commit ambiguity preserve fail-closed/rollback.
- Mixed existing compiled, existing data-revision and new data-bootstrap reads;
  legacy M113 path remains usable without querying absent M114 tables. Existing
  compiled/data same-address equivalence and published selector drift stay safe.
- Catalog auth/pagination/filter/stale response; detailed and batch payload digest
  retention; no automatic Probe/Release/Slot; no silent default revision selection.
- Affected Go race and integration suites, relevant frontend/API checks, release
  metadata and required CI. Reuse still-valid shared mechanics tests.

Historical M100–M114, Publications, immutable receipts, artifacts, existing
Releases and rollback evidence remain. 1.2.64 does not implement first-data-model
control-plane authoring. Before a Target write creates such a model, verify
whether its existing 1.2.64 runtime can still serve/read the new sealed form;
do not assume that shared M114 alone proves a full product rollback. Record any
remaining unsupported authoring path and require the new platform binary for
that workflow. Do not remove previously promised rollback artifacts or silently
change runtime compatibility requirements.

Retirement object: compiled-Catalog prerequisite in the first-Definition create,
attachment/catalog and Release authoring chain. Replacement: exact immutable
bootstrap reader plus the same create/Release owners. Exit condition: tests and
one genuinely uncompiled live model complete the chain. Remove the replaced
mandatory lookups from this path in this delivery. General legacy authoring,
history verification and compiled installation bootstrap remain supported and
are not retired by this acceptance.

## Live acceptance after platform rollout

Choose a real available model from the authorized Bailian connection whose full
contract matches existing typed implementations. Do not rename a fixture, invent
a revision or assume Qwen3.8-Flash equals Qwen3.7: official docs already show
different preserve-thinking defaults and effort/budget interaction. Current connection-scoped discovery also returned Qwen3.7-Max, absent from the
compiled Catalog. Its official text, hybrid-reasoning defaults and bounds match
the installed 3.7 implementation; full contract and live qualification checks
remain required. Discovery itself is not paid inference or capability proof. If no suitable model exists, report the live acceptance gap rather
than weaken contract validation. New wire work is separately identified.

Build/deploy the reviewed platform once, then freeze both backend image digests.
Publish the new model's independent formal CI Bundle without building/rolling a
backend; create the previously absent PublicModel through UI with existing
Connection14/binding14; qualify each required capability; author/activate a
sealed Release; run actual OpenClaw tools/history/usage. Capture before/after
images/schema, new identity, request IDs, execution/price identities, attempts,
terminal receipts, budget and answer quality. Each exact submodel stays within
30 calls/CNY5 across all this batch's interfaces/failures/diagnostics.

Only after this result may the first-model no-build acceptance close. Wider
Bailian families and native Messages follow; old writer retirement stays out.

## Independent pre-code review adjudication

Huygens reviewed the exact pre-code object SHA256
`35a9a60e89599ce94f48f26d67e2a84eeba49d6e21e62276dbd318538585069a`
against base `ce3b6f2c`, read-only, before implementation. Both required findings
are accepted above: DF-01 preserves presence only for newly supported data-first
fingerprints; DF-02 keeps compiled bootstrap source stable after unrelated
same-address Publication. Tests must cover successful omitted-default replay,
changed-to-zero rejection without new rows, compiled C@1 / lineage A candidate
followed by same-address lineage B Publication, and later embedding an equivalent
data address. A new bootstrap table or Runtime fallback rewrite was rejected.
No product decision or blocker remains before code. No implementation test result
is inferred from this review. One closing review will bind this object, its
adjudication, the exact resulting diff and test evidence.

## Local implementation validation

The first-data fixture now passes the actual create, exact replay/conflict,
Operation and independent capability approval, sealed Release/Slot, real Gateway
preflight and streaming transport, ProviderAttempt and unique terminal settlement
chain. Its upstream is isolated fixture transport, never live Provider evidence.
The original existing-model data upgrade test also passed after the DF-02
intervening same-address Publication. Frontend715 tests, types, API contracts,
production build and bundle-size gate passed. Affected Go race and full admin
integration checks passed; after the closing-review correction, authoring/admin/
cmd race and full admin Integration$ race passed again (91.995s). Earlier failed
fixture runs (transaction publication owner, replay status and outbox wiring)
remain retained; they are not reported as passing runs.

Runtime execution owners, schema, support manifest, Definition decoder and sealed
Release format are unchanged from1.2.64. The only Gateway diff is a startup
transport-injection setter used by the admin-to-Gateway fixture. This supports
source compatibility; exact old Artifact runtime/startup readback remains a
precondition before creating the first live uncompiled identity. 1.2.64 authoring
of that identity remains unsupported and is not claimed as a full rollback.

## Closing review and correction

The single closing review covered raw29-path patch SHA256
`dbcaad9c297dd8b56bc54e4d1ef0e3fa5f516a639fe63be604a604141ad3193d`,
the accepted DF-01/DF-02 findings and the later runbook clarification. DF-C1 was
validated: omitting Definition, limits and approvals could seal an uncompiled
Release without an execution snapshot. A real isolated HTTP regression returned
201 before correction. The authoring owner now rejects unknown bootstrap without
explicit data pins and requires each data operation to match that Definition.
No historical Release is changed. The same regression now returns409, leaves
Release/authoring receipt counts unchanged, and then completes the valid pinned
creation/activation/Gateway chain. The existing compiled-bootstrap data-operation
case and old compiled authoring regressions also pass.

Huygens verified corrected tracked diff SHA256
`ea72fa965df30908811f742d6f579f2fe924ab340ce9a548c9bf0619b3540730`
and independently reran both PostgreSQL chains plus Profile/frontend tests. No
code blocker or required finding remains. Formal CI, exact old Artifact runtime
verification and live same-image first-model acceptance remain separate delivery
gates. No additional architecture review is required for this corrected object.
经验文件无需变化.
