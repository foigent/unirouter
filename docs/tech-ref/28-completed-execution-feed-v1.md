# Completed-Execution Feed V1 Contract

Status: accepted V1 contract and implementation

## Boundary

V1 emits one immutable `revision="1"` item for each project-scoped,
authenticated model-gateway request that reaches the existing billing terminal
state `settled` or `cancelled`. It does not cover requests rejected before
billing admission and it does not publish late revisions.

The transport is a bounded private HTTP pull. X-Insight polls UniRouter;
UniRouter never calls X-Insight from the request path.

## Wire identities

- Page envelope: `xinsight.completed-execution-feed.v1`
- Snapshot object: `unirouter.completed-execution.v1`
- Money: `micro-cny.v1`, `CNY`, scale `1000000`
- Revision: the string `"1"`
- Page bounds: at most 50 items and less than 8 MiB decoded JSON
- Snapshot bound: at most 64 KiB decoded JSON

The normative snapshot schema is
`contracts/completed-execution/v1/unirouter.completed-execution.v1.schema.json`.
Golden fixtures live in `contracts/completed-execution/v1/fixtures/`.

## Completeness and gaps

`timeline_complete=true` means that the immutable V1 evidence profile is
final; it does not claim that every richer observability source exists. V1
always records the unavailable-at-terminal call-log, request-trace,
client-IP-presence and upstream-error-presence facts as the fixed
`capture_gaps` set. X-Insight must surface those gaps rather than infer missing
events or wait for a revision that V1 will never publish.

## Data policy

The source object is an allowlisted typed structure. It contains request,
project, billing-terminal, usage, micro-CNY and capture-gap metadata only.
Raw prompt, response, Authorization, API key, credential, IP address and raw
upstream error fields are prohibited. Unknown fields fail closed.

## Cursor and delivery

The endpoint exposes an authenticated opaque project-bound keyset cursor over
the retained feed sequence. It never exposes internal-outbox IDs. An empty
cursor is the activation-forward handshake: UniRouter returns no historical
items and advances the client to the current project high-watermark. Later
polls return only items after that cursor. Replays are at-least-once and
content-stable; a cursor older than the seven-day retained floor fails
explicitly. A valid cursor ahead of the current source high-watermark returns
permanent `409 completed_execution_feed.cursor_regressed`; it is never treated
as an idle empty page and never auto-resets or backfills.

The endpoint accepts only an active, project-bound `service` API key with the
dedicated purpose `xinsight.feed.read` and a non-empty matching network/IP
allowlist. That purpose is denied on every model-gateway route.

Internally, an enabled feed credential uses the reserved database status
`feed_active`; the Admin API still renders it as `active`. Binaries predating
purpose-aware authorization only accept database status `active`, so they
continue to reject a feed credential even if an operator skips the supported
rollback preflight. A database constraint also prevents a purpose-scoped key
from being changed to ordinary `active`.

## Publication and retention ordering

The billing terminal journal and feed item are materialized in one outbox
handler transaction. Before allocating the retained `BIGSERIAL` item ID, the
transaction locks the project row in `completed_execution_feed_states`.
Same-project publication ID order is therefore commit order even when the
internal outbox has multiple workers.

Seven-day Development retention uses feed publication `created_at`, not the
possibly out-of-order billing-terminal time. Each retention transaction locks
one project state row, deletes only that project's contiguous expired ID
prefix, and advances `retained_after_id` in the same transaction. A concurrent
retention worker cannot skip a lower row and move the floor past it.

## Binary and schema rollback

Rolling the V1 application binary back to a version without purpose semantics
is intentionally supported only after:

1. stop the X-Insight poller;
2. disable the exact `xinsight.feed.read` key through the current UniRouter
   control plane;
3. run `./uniroute upgrade preflight-legacy-rollback` from the current binary;
4. start the older binary only after the preflight reports zero active
   purpose-scoped credentials.

Schema rollback is stricter than binary rollback. Migration `000043` refuses
down while any purpose-scoped key or retained feed state exists. Normal
recovery is roll-forward; removing the retired exact key and feed state for a
schema down is a separately controlled data-lifecycle action.
