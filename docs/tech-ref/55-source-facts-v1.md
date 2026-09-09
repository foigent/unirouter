# UR work facts v1 — local candidate

This document describes the UR-A implementation candidate for product 1.3.0,
schema M116. It is not a released artifact or proof of XI integration. The
accepted design and remaining acceptance work are tracked in
[the execution record](../agent-workflows/EXECUTION-RECORD.md) and
[the exact accepted plan](../analysis/UR-WORKFACTS-LOCAL-PLAN-20260909.md).

## Scope and source authority

The source supplements the existing completed-execution feed (I-01). I-01 keeps
its original bytes, identifiers, financial meaning and consumer contract. The
new source is a metadata-only observation of UR's original authentication,
reservation, media-job and terminal-journal writers. It does not authorize a
model call or create a second financial ledger.

| Fixed profile | Facts | Excluded interpretation |
| --- | --- | --- |
| `unirouter.execution-context.v1` | Original org/project/request/key, logical credential/application when known, protocol, operation, authentication epoch and capture time | Client labels are not execution identity; context does not prove Provider success |
| `unirouter.execution-identity.v1` | Known account owner, grant subject/revision, Sponsor and validated Agent lineage when present | Sponsor is not the invoking person; `actual_human` remains `unknown` |
| `unirouter.cost-basis.v1` | Frozen billing mode/provenance, effective rates, price authority/version, unit rates, multiplier and original economic reference | No additional summable amount, vendor invoice or cash-payment claim |

Identity fields are absent from the context and cost profiles. Each credential
must receive the fixed identity profile explicitly. Permissions do not redact a
previously hashed document into a different document under the same identity.
Missing historical fields remain `not_captured`; new capture does not backfill
old account ownership, grants or prices from their current values.

Responses and Messages require the existing exact explicit access grant;
legacy-unbound credentials and wildcard/project-default grants do not acquire
these endpoints by enabling capture. Agent capabilities additionally inherit
the exact endpoint scope of their root grant; a bound root must be an active
`agent` workload using identity v2 to manage capabilities. A personal grant
cannot mint them. The older Chat/Embeddings paths
retain their existing legacy and grant authentication support.

## Capture and durability

Capture defaults off (`SOURCE_FACTS_ENABLED=false`) and does not require XI.
When enabled, `COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE` supplies the same stable
producer identity used for I-01 association. Enabling I-01 and enabling source
facts are independent settings. Startup requires the exact M116 schema and
rejects stored source data bound to a different producer identity.

The second successful authentication pass, after body retention and before
admission/idempotency, records context and identity in one local transaction.
This includes a later admission rejection, but excludes unauthenticated requests
and requests without a trusted project. A required capture failure returns the
existing model protocol's 503 error before Provider dispatch.

Freeze preserves small contemporaneous identity and price seeds on the original
reservation in its original transaction. Journal materialization runs I-01
first, then the source profiles, in that same journal transaction. Source write
failure uses the existing financial outbox recovery; it cannot authorize a
second dispatch or charge. An already-created journal does not rematerialize
expired source facts when an old outbox event is replayed.

Chat response-cache lookup occurs after the existing Freeze. A hit retains its
own zero-token/zero-calculated/zero-charged delivery and terminal record, with no
new Provider attempt or call. With I-01 enabled this produces the original
distinct zero terminal; the source's CostBasis refers to that exact snapshot.
It does not copy or charge the earlier cache miss's amount. This corrects the
accepted plan's original assumption about no Freeze/I-01 on a hit; it does not
change the existing financial path. Canonical routes and unsupported protocols
retain their existing response-cache bypass/refusal boundaries.

Chat SSE retains its original delivered-counter partial-settlement contract.
After partial delivery followed by upstream interruption or client cancellation,
its I-01 can be settled with usage authority `gateway_counter`, purpose
`customer_charge` and finality `estimated`. CostBasis refers to that exact
economic snapshot; it does not turn estimated usage into Provider-reported
usage. The separately qualified Messages/Responses V2 contracts keep their
own finality and pending-settlement rules.

Media admission and first dispatch preserve separate seeds on the job. Terminal
writers publish with the original job/charge-fact transaction. Repeat cancellation
of an already-terminal job does not recreate expired facts. Media does not
manufacture a text reservation or I-01 entry. A charge-fact reference has explicit
`pricing_pending_media_rate_card` state until the independent media economic
contract supplies pricing; a job without that fact has `not_eligible` state.
Later attempts and process history belong to UR-B.

If early facts expire before a dispatched job terminates, the original expiry
writer can still publish the terminal from the retained admission/dispatch
seed. Captured identity and time remain historical. Revocation of the ordinary
model key before reservation prevents dispatch and leaves admission facts as
observations of the earlier admission, not proof of later authorization.

An ordinary synchronous execution emits at most seven initial profile documents:
two authenticated, two admission, and three terminal. Media storage emits two
admission, two first-dispatch, and three terminal documents; HTTP authentication
can add two more. These are bounded stages, not per-token events. Context/basis
seeds are bounded to 32 KiB each; each published document is at most 64 KiB.

## Identity, canonical bytes and corrections

The bundled source schema is
[`contracts/source-facts/v1/source-fact.schema.json`](../../contracts/source-facts/v1/source-fact.schema.json).
Identifiers, revisions, rates and quantities are decimal strings where numeric;
consumers must not round them through IEEE-754 numbers. Numeric identifiers fit
positive signed int64. Timestamps are UTC with exactly six fractional digits.
Strict decoding rejects unknown properties, duplicate JSON keys, null values,
invalid UTF-8, control characters in bounded fields and noncanonical bytes.

`source_fact_id` is `uf1_` followed by lower-case SHA-256 of these UTF-8 strings,
each preceded by its unsigned 32-bit big-endian byte length, in this order:
schema version, source instance, org ID, project ID, profile, request ID, stage.
`event_id` uses the same algorithm on schema version plus `.revision`,
`source_fact_id`, and revision. Adapter version is not part of either identity.

The first revision is `1` with no predecessor. A subsequent revision needs the
immediately preceding revision, its source hash, and a bounded correction reason.
Same identity/revision and identical canonical bytes are idempotent. Different
bytes at that identity/revision conflict; committed rows cannot be updated.
Publication locks profile stream rows in sorted order before allocating sequence
numbers, so a later-committing writer cannot create an unseen hole behind an
already-issued high watermark. Business locks precede I-01 locks, which precede
these source stream locks.

Source bytes are hashed separately from the immutable Event Envelope's semantic
hash. The envelope uses the pinned Foigent 2.0.2 binding, F2 provenance,
`metadata_only` and `raw_payload_present=false`. `actor.type=unknown` is distinct
from the known account/grant evidence inside the authorized identity profile.
Page cursors, delivery time and receiver state do not change source identity.

The bundled `golden-pages.json` provides fourteen valid page fixtures, seven
invalid cases and the original R1–R5 I-01 examples. Go tests compare the frozen
envelopes with actual source projection. The independent stdlib Python checker
`python3 contracts/source-facts/v1/check_pages.py` verifies bounded page identity,
hashes and a durable SQLite consumer through both arrival orders, partial-page
restart, checkpoint failure, replay and late R5 arrival (calculated 100→105 CNY,
charged zero). Literal fixture cursors are not server credentials or signed
production cursors. This is offline interoperability evidence, not a general
JSON Schema validator, authenticated pull or XI qualification.

## Economic association

Text cost facts refer to the exact producer/org/project/request/revision-1 I-01
snapshot hash and its `money.calculated_cost_micros` and
`money.charged_cost_micros` paths. With I-01 enabled, the same transaction checks
the materialized hash. With I-01 disabled the reference is explicitly
`not_published_by_i01`; an expected hash is not proof that an I-01 row exists.

Consumers may receive either profile first. I-01 amounts remain usable without
cost-basis enrichment. A cost-basis fact arriving first remains pending until a
matching economic record exists. Enrichment does not add the amount a second
time. Calculated cost, charged cost, Provider-reported cost and cash remain
separate concepts.

New seeds distinguish priced-zero and unpriced. The `uo1_` interpretation is
limited to the existing legacy observation quote writer. A legacy reservation
can provide persisted rates but not recover its unrecorded authority or RateCard;
that basis reports `persisted_legacy`, `pricing_state=unknown`, and
`pricing_authority=not_captured`. An empty legacy pricing version is preserved.
Old outbox payloads lacking a billing mode report `legacy_interpretation.v1`;
new payloads report `journal_payload`.

## Read protocol and coverage

`GET /api/v1/integrations/xinsight/source-facts/capabilities` returns only the
credential's authorized profiles and coverage boundaries. It accepts no query
parameters. A staged credential may inspect capabilities while paused; it cannot
pull data. Capability discovery neither enables the credential nor proves XI
has persisted or indexed anything.

`GET /api/v1/integrations/xinsight/source-facts` accepts exactly one `profile`,
optional `cursor`, and optional `limit` from 1 to 50 (default 50). The dedicated
purpose is `xinsight.source.read`; existing feed and model credentials cannot
read these profiles. The reader validates current org/project, connection/key
status, network and the intersection of connection and key profile permissions.

An empty cursor returns an empty bootstrap page at the current high watermark.
Each following page is a repeatable-read snapshot, at most 50 events and strictly
less than 8 MiB. The reader admits at most eight concurrent requests with a
five-second deadline. Capacity or persistence failures return retryable 503.
These bounds do not constitute a throughput or availability promise.

The signed cursor binds producer, org, project, profile, source epoch, permission
scope generation and sequence. A retained same-scope cursor can continue after
credential rotation; changing permission scope invalidates the old binding.
An ahead cursor returns 409 and an expired cursor returns 410. Neither condition
silently resets to latest. The acknowledged position is the incoming cursor
actually supplied by the receiver, not the new page's outgoing cursor.

Retention is seven days from publication. Cleanup advances only the contiguous
expired prefix and its floor in one transaction. The reservation/job seeds
outlive that independently retained page, so a terminal event after a long hold
is self-contained even when early context pages have expired. Bootstrap,
late enablement, absent early capture and independently permitted profiles mean
the interface never promises a complete request timeline.

The existing X-Insight management page now includes a separate source connection.
Its platform-admin routes live under
`/api/v1/data-exchange/xinsight/source-connections/{project_id}`. `prepare`
registers a dedicated receiver verifier with an exact /32 or /128 network binding;
`rotate` inherits that binding and requires pause. `activate` and `cancel` require
the exact staged `credential_id` as a decimal string. Activation revokes the old
active credential atomically, and remains paused until `resume`. A stale target
ID or changed registration replay returns 409. Registration field names are exact;
duplicate, unknown, null and bearer fields are rejected. The server stores the
existing domain-separated verifier HMAC, never the receiver bearer.

`profiles` requires pause and an explicit sorted profile set. It revokes active
and staged credentials and advances the scope generation; re-registration and
forward-only bootstrap are required. `revoke` permanently closes the connection.
Ordinary rotation preserves its generation and retained cursors. Status and
pause/cancel/revoke remain available when capture or the Data Exchange entitlement
is unavailable, including an inactive project. Expansion requires all three.
All writes reauthorize the current administrator, role, auth epoch, exact live
session and applicable project state inside the committing transaction and audit
only safe identifiers. Reads share-lock the connection authorization row, so
scope changes cannot overtake the read and its acknowledgement commit.

The UI shows only credential prefixes and IDs. Its default field selection grants
context alone; identity and cost require explicit registration/profile choices.
It does not generate, persist or return receiver bearers. The independent source
connection neither upgrades an I-01 credential nor changes I-01 connection state.
This local implementation and its tests use only synthetic identities.

## Reproducible local validation

`requirements-schema.txt` pins the general `jsonschema` 4.25.1 Draft 2020-12
validator and all five transitive development dependencies, including official
PyPI wheel hashes for supported platforms. Use Python 3.11 or newer and
`pip --isolated install --only-binary=:all: --require-hashes --target <task-directory>
-r contracts/source-facts/v1/requirements-schema.txt`, then set `PYTHONPATH` to
that task directory and run `check_schema.py`. These are development checks;
no Python dependency is added to the gateway runtime or a shared interpreter.
The selected licenses are MIT, with PSF-2.0 for typing_extensions. Version,
license and upstream provenance come from the exact PyPI release metadata;
[jsonschema 4.25.1](https://pypi.org/project/jsonschema/4.25.1/) documents the
supported schema dialect.

`check_schema.py` validates the meta-schema, eight valid source vectors, five
schema-invalid vectors and valid static page payloads. Duplicate-key and
canonical-byte negatives are classified separately. Scope equality and the
signed int64 decimal-string upper bound remain required Go domain checks;
ordinary JSON Schema acceptance does not replace them. `--http-pages` can add
captured synthetic HTTP fixture directories to the same general validator.
The original Go/vector checks remain required alongside this CI step.

`check_http_pages.py <directory-or-file> --expected-cases <count>` runs the
independent SQLite consumer against actual authenticated local HTTP pages,
including both I-01 arrival orders, failed page/checkpoint restart and adapter
replay. `SOURCE_FACTS_PAGE_EVIDENCE_DIR` makes the integration helpers export only
those synthetic pages and original I-01 snapshots. No bearer or administrator
session is exported. The opt-in `SOURCE_FACTS_VERIFY_CONSUMER=1` runs that consumer
at the end of the same local case and records timing. Journal/publication ages
are measured from their database timestamps, not asserted commit-ACK timestamps.
The 32 sequential-ingress / four-worker outbox case is a bounded Local scenario;
it does not prove concurrent model-request capacity or XI's combined-page SLO.

## Installation, expansion and recovery

The new binary accepts the complete, exact M113/M114/M115/M116 ledgers. It does
not accept an arbitrary future head or a ledger with gaps. Capture remains off
on a predecessor schema. M116 preserves counter v2/v3, runtime lease/CLI and
capability-publication support; all relevant manifests must include the new
source implementation reference before compatible expansion.

The existing development/test/integration compatible-expand path has a fixed
two-instance fleet. Both bridge instances use capture off before M116 applies.
The named source-facts carrier validates the exact migration pair and immutable
digests. Busy authorization, financial, media or runtime owners cause migration
failure without aborting their accepted transactions. Bootstrap failure rolls
back DDL and its migration ledger together.

Customer Compose uses its existing installer/preflight/upgrade and maintenance
contract. Its configuration forwards the default-off flag and validates the
stable source identity when enabled. It is not covered by a claim of universal
online two-instance expansion or zero-downtime upgrade.

M116 also stops repeated tuple writes to the already-complete M36 micro-ledger
safety marker. All eleven original statement triggers, first-evidence semantics
and downgrade refusal remain. The migration takes the exact existing projects
parent lock with NOWAIT and replaces the marker function after dependent DDL;
busy project/marker writers cause complete migration rollback. A cold first
commit can still invalidate an older Repeatable Read snapshot, and independent
deployment/foreign-key conflicts are not claimed fixed by this change.

An empty unused M116 may be rolled back through its guarded down migration.
Any source stream, durable seed, source credential/connection or live serving/
recovery generation causes refusal. Recovery then requires a supported bridge
or forward repair; deleting evidence is not a rollback procedure. Product 1.2.77
cannot directly start against M116. Restored source/financial data and cursors
require separate verification; a receiver ahead of the restored high watermark
must explicitly handle the gap.

The opt-in `TestSourceFactsPGBackupRestorePreservesIdentityAndPendingJournalIntegration`
uses `SOURCE_FACTS_PG_BIN` for the existing PG16 tools. It dumps a task-owned
database and restores into a separate new database, verifies twelve table hashes
and retention state, then drains the original pending outbox exactly once.
That original test covers financial recovery. The additional
`TestSourceFactsCredentialAndSignedCursorPGRestoreIntegration` registers a
synthetic receiver through the real lifecycle, restores a separate PG16 archive,
then verifies the same credential, IP/profile restrictions and retained signed
cursor. A cursor issued after the backup returns 409 against the restored high;
there is no silent reset. This remains Local synthetic evidence.

The existing second-integration reset preview recognizes all five source tables
as protected. M116 with those tables empty keeps its existing reset path. Any
source row makes that older reset scope incomplete and prevents apply. Erasing
source history, credential bindings or cursor epochs needs a separately reviewed
reset lifecycle; this candidate does not silently add that erasure authority.

Local migration and gateway tests are recorded in the execution record. Docker
runtime, exact Neutral N1 artifact qualification, CI, Target, real Provider and
real XI integration are separate evidence requirements, not inferred from this
document or the local candidate.
