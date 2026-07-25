# Completed-Execution Feed V1 Contract

Status: accepted contract, implementation in progress

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
explicitly.

The endpoint accepts only an active, project-bound `service` API key with the
dedicated purpose `xinsight.feed.read` and a non-empty matching network/IP
allowlist. That purpose is denied on every model-gateway route.
