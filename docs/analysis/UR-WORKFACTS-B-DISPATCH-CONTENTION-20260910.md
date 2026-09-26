# B dispatch contention: amendment v2, incremental review pending

Object: uncommitted B/D candidate based on accepted A `25f5c1fd`.
Accepted B/D v2 design digest:
`0065cf2f2c7342fc652b2a4017eaec23666eb1c7755d7368c61776421b9c5ec7`.
This proposal changes its direct pre-dispatch publication seam and its prohibition
on a new internal outbox event type. Other product and external authority stays
within the existing local synthetic scope. No implementation of this amendment
has started.

Coordinator accepted the durable-record/publish-later direction, with three
`validated_required` details below. V1 is retained at
`/private/tmp/ur-bd-workfacts-20260910/dispatch-contention-proposal-v1.md`, digest
`ff9ab1f5f33fd93b5ea166fa1047588eff60e647268cc4a84a3e8ce1fe209d8a`.

## Evidence and cause

`/private/tmp/ur-bd-workfacts-20260910/concurrent-v4-results.json` holds ten
symmetric groups, four distinct logical credentials on one project, 64 requests
per group, ten warmups, original fake Provider without sleep, original outbox
catch-up. Accepted A/off/D have 0–2 HTTP 503s; B/BD have 36–42. Mixed failures are
not successful-request p95 evidence. Every group recovers 74 original terminals
and leaves zero unresolved holds.

The diagnostic-only overlay records the actual underlying errors in
`concurrent-candidate-diagnostic-v2.log`: 33 failures at `record dispatch source
observation` and four at original `insert provider attempt`, all PostgreSQL
40001 after the existing single serialization retry. The B writer updates
project/profile `execution_source_streams.high_sequence` after Authorize's
Repeatable Read snapshot and authority locks. Concurrent source writers change
that row, so waiting for its lock cannot refresh the RR snapshot. Extra source
work also widens the original receipt contention window.

The pre-existing A failure needs its own attribution; this proposal addresses
the added B conflict and must not claim it fixes all baseline concurrency.

## Proposed minimum change

1. Preserve Authorize RR, attempt-first, revalidation/claim, owner/fence, the
   existing retry budget and original financial/Provider rules. At the exact
   current post-claim/pre-commit hook, build and validate the same three canonical
   B records but append one bounded `evidence.source_dispatch.v1` event with the
   existing `outbox.EnqueueTx`. Key binds source instance/org/request/attempt and
   canonical payload digest. Payload contains only these approved source records,
   not prompt, response, credentials, or an execution instruction. Enqueue failure
   rolls back the original attempt and prevents Provider invocation.
2. Register one handler on the existing internal outbox worker; add no worker,
   scheduler, queue table, endpoint or external traffic. It atomically publishes
   the frozen records in a bounded source transaction after the original dispatch
   transaction commits. Occurred/recorded identity remains the original accepted
   claim time; published time is actual later publication. A published registration
   still proves registration only, not a Provider call. Later observations may
   arrive earlier; no source sequence is relabeled as causal execution order.
3. Avoid retention resurrection without another ledger: in the same transaction
   as first publication, mark this specific live source event payload as
   materialized. The handler locks/re-reads its live row, validates the immutable
   record digest and expected event identity, and never republishes when marked.
   The existing worker still owns lease/ack/delete. After successful ack the row
   is absent; the original owned-attempt created guard prevents re-enqueue.
   Processing a stale/deleted event must not reconstruct a new source event.
   This marker is internal delivery state and is excluded from source hashes.
4. Use the existing ordinary-evidence priority/dead-letter contract, preserving
   financial priority and budget pending calculations (the new event is not a
   financial event). A queued or failed publication does not claim source or XI
   availability. Failures retain original outbox diagnostics; clients see only
   actually published source records. Recovery processes recognize this event
   even when new B capture is disabled; capture flags must not silently discard
   already committed evidence. Exact source instance and schema must match.
5. Include the event/handler support in the finite runtime manifest and customer
   upgrade/recovery checks. The unpublished M117/M118 candidate remains local;
   no accepted A archive or migration history is rewritten. Final package docs
   distinguish durable pre-dispatch recording from asynchronous source publication.

Rejected alternatives: increasing retries/timeouts; serializing whole project
requests or lowering load to hide the defect; changing Authorize isolation;
session/advisory locks before original financial locks; allocating visible source
sequence slots outside the atomic attempt. They either hide the failure or alter
broader authority, lock order or source cursor contracts.

## Review and verification required

Independent review must decide whether this durable-before-Provider / publish-later
boundary preserves the intended B requirement. If it does not, do not implement
it as a silent substitute. Review exact replay, lease loss, poison/malformed
payload, source instance, marker immutability, retention/late first publication,
disabled capture recovery and finite upgrade compatibility.

After agreement: verify enqueue failure before Provider; rollback and ambiguous
commit ownership; original worker recovery and marked replay after retention;
unchanged source hashes/identity and A bytes; no financial reclassification;
same ten-group serial/concurrent comparison and independent authenticated consumer
availability after original outbox catch-up. Keep every old failure. The browser
management chain already passed separately in `source-d-browser-v3.log`.

Source-only optional B observations also currently use RR. Their separate
contention/coverage must be measured. Any change there requires proof about the
exact original-state reads; this proposal does not silently relax those snapshots.

## Concrete choices resolving the three review requirements

### 1. Frozen bytes, size and monotonic marker

The event freezes three base64 strings of the exact `EncodeObservation` bytes,
each with its original event/fact identity and SHA-256, plus an aggregate digest
over the canonical immutable event body. Decode base64, compare per-record hash,
call `DecodeObservation` (including canonical-byte equality), and recompute all
scope/kind/attempt/event identities and the aggregate digest before use. Never
pass JSONB-rendered object bytes to the source decoder. The key binds the source,
org/request/attempt and aggregate digest; a digest does not replace original
attempt ownership or the created guard.

Each source record retains its 64KiB bound. The whole encoded event obeys the
existing 262144-byte JSONB-text constraint: before enqueue, measure PostgreSQL's
actual `octet_length(payload::jsonb::text)` and require at most 262144 minus 1024
bytes. The reserved 1024 bytes cover the fixed materialized marker/timestamp.
The final update is also subject to the original DB constraint. Three 64KiB
records cannot be assumed to fit after base64; over-limit batches are rejected
before Provider dispatch, with no partial record or attempt commit. Tests cover
both per-record and aggregate/marker limits.

M117 adds an event-specific insert/update trigger on the existing outbox table:
initial marker must be unmaterialized; immutable body/type/key/scope cannot change;
the only payload transition is unmaterialized to materialized. It validates the
three published source IDs/hashes/scope before accepting that transition. A true
marker cannot be reset, and existing true payload cannot be replaced by worker
snapshot bytes. Lease bookkeeping updates and original ack/delete retain their
existing behavior. The handler only updates the marker, never rewrites the body.

### 2. RC publication, lease and disabled-capture recovery

The new handler uses a bounded **Read Committed** transaction: lock the M117
schema-ledger row `FOR KEY SHARE`, then the exact live outbox row `FOR UPDATE`,
then the existing globally sorted source-stream locks. It does not read changing
business state or acquire original financial/authority locks. Use existing
source lock/statement bounds, not a new retry loop. This does not change Authorize
RR or any optional observation snapshot transaction.

Check live ID/type/key, actual `claimed_by`, unexpired lease and frozen identity;
read marker from that locked live row. Lost ownership, missing/deleted row or
expired lease returns a typed processing error without publication/reconstruction.
Before marker commit, recheck the same owner and lease deadline; failure rolls
back all three publications. If claim changes after commit but before original
ack, the later owner sees the marker and performs no source write. No handler
ack/delete replaces the worker's original conditional delete.

First publication after seven days is allowed as explicitly late evidence:
original occurred/recorded times remain frozen, actual published time starts
the normal seven-day source retention. A materialized event replay after source
retention only acknowledges its marker and cannot resurrect deleted facts.

Recovery bypasses capture admission flags only through a private validated
frozen-record publisher that calls the original sorted record writer directly;
it cannot use `publishTogetherTx`'s A/B capture filters. Handler registration and
support are unconditional. At startup, a read-only check for any retained source
dispatch event verifies required schema and exact configured source instance even
when both capture flags are off. Missing/mismatched source instance/schema stops
worker startup or returns a retained processing failure, never successful discard.
Already marked records still require correct event identity before ack.

Destination `dispatch_result` currently obtains its frozen origin from the
published registration. During publication lag, it must alternatively decode
the same verified live dispatch-event record under its existing RR snapshot.
It must never read a current connection URL. The publisher's source rows and
marker commit atomically, so tests must show a consistent reader can obtain the
frozen record before/after publish and ack. Only an actual Do measurement sample
authorizes a do/response-observed result, never presence of this event alone.

### 3. Pending-event down guard and actual handler support

M117 down first takes its schema-ledger row `FOR UPDATE NOWAIT`, then
`internal_outbox_events` in `SHARE MODE NOWAIT`, then its existing source locks.
It refuses **any** retained new event: pending, claimed, dead-letter or
materialized-awaiting-ack. Enqueue and handler both hold the ledger row
`FOR KEY SHARE` before touching this queue, so down cannot race between check
and insert; after down, absence of M117 rejects enqueue. The new event-specific
trigger is removed only after these guards succeed. Existing B facts/grants and
generation guards remain; no evidence is deleted to permit down.

Add the exact implementation reference
`{kind: source_dispatch, id: unirouter.source-dispatch-outbox.v1}` to the compiled
manifest and required references for M117/M118 expansion and startup. M117 up
checks every non-stopped serving/recovery generation, including expired/absent
recovery identities. An M117 runtime-generation insert/update guard rejects a
non-stopped generation whose immutable manifest lacks this handler/schema;
the original renewal/bootstrap paths therefore cannot admit an unsupported
worker after expansion. Existing compatible-expand CLI/script/fleet validation
checks the same reference. No new online-upgrade promise or alternate lifecycle
is introduced. Fixtures prove an unsupported old worker is rejected before it
can start claiming, and a supported capture-off worker drains existing events.

The incremental review is limited to these three choices and their dependencies.
No amendment code is authorized by an unreviewed draft; continue only after the
coordinator agrees on this v2 object.
