# B observation contract — local candidate

B supplies finite observations of original execution boundaries. It does not
authorize execution, create charges, reconstruct missing timings, or prove
client receipt. A's identity, document bytes, descriptors and cursor domains stay
unchanged. B requires both source capture and `SOURCE_OBSERVATIONS_ENABLED=true`,
plus explicitly granted B profiles. Destination origin needs that profile grant.

`source-observation.schema.json`, `golden-observations.json` and `manifest.json`
bind the event contract. `runtime-current.schema.json` describes the separate
current snapshot; each page observes its own current lease inventory, not one
immutable multi-page snapshot. A routing Probe is not an execution lease.
Synchronous requests without an execution lease remain outside that inventory.

Admission rejection is recorded before its first 4xx header only when no original
reservation, provider attempt or media admission exists. Failure returns source
unavailable before Provider dispatch. Provider/delivery/hold observations follow
original commits in bounded independent transactions; failure cannot reverse
money, retry a Provider, or change a delivery result. Original financial journal
materialization retains its atomic A/B publication and original created guard.

Dispatch registration freezes exactly three canonical documents in one original
outbox event (`evidence.source_dispatch.v1`) inside Authorize's existing RR
transaction. Failure to enqueue prevents Provider dispatch. The original worker
publishes those documents in RC and marks that same event atomically, then owns
its acknowledgement. Registration can arrive after a result; consumers retain
the two different observation kinds rather than downgrade a known result.
Measurements read pending frozen input or published registration in the same RR
snapshot and never reconstruct an origin from current connection settings.

The handler contract is `unirouter.source-dispatch-outbox.v1`. Frozen bytes are
base64 encoded with per-document and aggregate hashes. Real PostgreSQL JSONB
size must leave 1024 bytes below the existing 256 KiB event limit; individual
documents still have the 64 KiB limit. A one-way materialized marker prevents
republication after source retention. Pending, claimed, dead-lettered and
materialized-but-unacknowledged events all block M117 down. Recovery remains
enabled when new A/B capture is off and requires the exact source instance and
supporting schema before the original worker starts.

`request_context_done` records only a cancelled or expired request context.
It identifies neither a cancellation actor nor a Provider timeout. Runtime,
Provider, local-delivery, financial and capture states remain separate.

Timing values use process-local monotonic intervals, with nanosecond numerical
representation rather than a clock-accuracy claim. First output requires a
meaningful protocol frame and successful Write and Flush. Tool output qualifies
through the existing protocol parser. Cache hits retain original Freeze/I-01,
complete-response timing and no Provider attempt. Old latency/TTFT fields keep
their original meaning. `measurement-vectors.json` contains synthetic T01–T10
arithmetic and grouping cases, not real workload measurements or an XI query
implementation. Compare only equal operation, stream mode, measurement kind,
version and completion evidence; failures and unknowns retain separate counts.

## Finite publication and storage bounds

These are conservative maxima of B revision-1 rows, not achievable simultaneous
states or performance measurements. Explicit future corrections would require
their own accepted writer/retention contract; current writers do not emit them.

Let N be committed text/public attempts, P the original policy aggregate limit
`3 * (256 + 1) = 771`, M media job attempts (at most 10), and R selected media
routes per job attempt (at most 128). Text/public runtime kinds contribute at
most 10 rows per request. Each text/public attempt has at most 10 attempt kinds,
one capability snapshot and two destination observations. Media has at most
five parent observations per job attempt, six per route and two per job.

| Original path | B row upper bound | Largest supported bound |
| --- | --- | --- |
| Chat / Embeddings | `10 + 13*N + P` | N ≤ max retries + 1 ≤ 11: 924 rows |
| Responses / Messages | `10 + 13*N + P` | N ≤ selected candidates ≤ 128: 2445 rows |
| Image job | `2 + M*(5 + 6*R)` | M ≤ 10 and R ≤ 128: 7732 rows |

The original limits are in `routingpolicy/policy.go`, `routingpolicy/evaluator.go`,
the relay's Provider-retry branch, public executor route loops, and
`mediajob/service.go`. Media does not apply the text retry count. Optional
publication can be absent after process loss or its bounded transaction timeout;
absence is a capture gap, not a successful outcome or a fabricated zero.

Each source document is limited to 65536 canonical bytes. Multiplying the above
row bounds by that hard document limit gives conservative raw-source ceilings
of 60,555,264 / 160,235,520 / 506,724,352 bytes respectively. These deliberately
loose ceilings exclude PostgreSQL row/index/WAL and envelope overhead, do not
describe typical traffic, and do not replace candidate runtime/storage benchmarks.
The supported 128 requirement / 64 capability-evidence collection test separately
checks the richest capability document without truncation. Pages contain at most
50 items and remain below 8 MiB. Runtime reads add no persisted rows. Existing
seven-day retention does not authorize historical backfill.

## Reproduction

`python3 generate_assets.py` regenerates schemas and independent identity/hash
vectors from the finite Go structs plus explicit field constraints. It does not
modify A. `python3 check_contract.py` requires the existing `jsonschema` package
and validates manifest pins, positive/negative schema cases and independent
source identities. `python3 check_measurement_vectors.py` uses only the standard
library. `check_http_pages.py PATH` validates exported synthetic B-containing
authenticated pages and original A/I-01 joins across restart/checkpoint/replay.
Actual native/public/media HTTP tests supply separate execution evidence.

D budget profiles and snapshots are not part of this B contract.
