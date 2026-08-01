# Async Multimodal Pre-Gate Contract

> Contract: `unirouter.async-media-job-plan.v1`
> Scope: PRD7 B13 / P7-F19 repository-local contract only
> Runtime state: `decision_gated_p7_d03`

## Outcome

B13 freezes only the provider-neutral facts that can be decided safely before
the media product/runtime choice:

- tenant-scoped opaque object identity;
- immutable content digest, media type and canonical decimal byte size;
- deterministic idempotency identity;
- explicit operation and output intent;
- a lifecycle boundary that proves no queue, lease, execution, cancellation
  or settlement state has started;
- metadata-only privacy and compatibility assertions;
- declared missing decisions and required threat classes.

It does not implement an asynchronous job service. The Schema is a pre-gate
planning and conformance artifact, not a public request API or a promise that
the eventual runtime will retain every field unchanged.

## Explicitly Unresolved Product Decisions

Every conforming plan must keep these four values at
`unresolved_p7_d03`:

1. media provider and supported production capabilities;
2. object store and ownership/region/encryption model;
3. retention, deletion, legal-hold and customer-control policy;
4. the customer charge trigger and cancellation/refund semantics.

The same four facts are represented as `missing` gaps with
`declared_by_product_gate` provenance. The video fixture adds a fifth explicit
gap because UniRouter has no video endpoint profile.

Any artifact that inserts an actual provider, store, retention window or
charge trigger fails conformance. This prevents a fixture or SDK from silently
making the User's product decision.

## Object And Idempotency Boundary

`MediaObjectRef` is deliberately non-locating. It contains:

- an opaque `media_*` identifier, never a URL or provider locator;
- exact `org_ref` and `project_ref`;
- immutable SHA-256 content identity;
- bounded media type and positive canonical decimal byte size.

No signed URL, bucket, key, raw media, prompt, credential or provider response
is admitted. The idempotency key is the canonical SHA-256 of tenant scope,
operation, intent hash, ordered immutable input metadata and output
expectation. A tenant, intent, object digest, media type or size change therefore
cannot reuse the same identity.

The eventual runtime still needs a separately reviewed authorization step on
every object create/read and must not trust an opaque reference merely because
its syntax is valid.

## Lifecycle Boundary

The two fixtures remain in one exact non-runtime state:

- admission: `contract_only`;
- queue and lease: `not_created`;
- execution: `not_started`;
- outcome: `unknown`;
- cancellation: `not_requested`;
- settlement: `not_started_decision_gated`.

This is not the future state machine. Queue fencing, lease renewal, retry,
callback, cancellation races, terminal finality and financial settlement must
be frozen only after P7-D03. The current contract prevents those states from
being invented early.

## Existing Endpoint Evidence

B13 pins the exact B6 endpoint-profile Schema, the decision-gated GPT Image
manifest and fixture, and the B6 golden report. The text-to-image plan says
`covered_by_decision_gated_profile`; that is fixture evidence, not runtime
support. The image-to-video plan says `no_endpoint_profile_available` and
declares the gap.

No new provider registry entry, Gateway route or vendor branch was created.

## Compatibility And Runtime Boundary

Every plan asserts and the executable validator enforces:

- no runtime registration, queue, lease or object persistence;
- no provider/callback traffic and no credential acceptance;
- no customer charge and runtime mutation `none`;
- existing request path, routing and billing behavior unchanged;
- completed-execution Feed wire, Outbox identity and Cursor behavior
  unchanged.

The offline CLI reads embedded synthetic artifacts and writes a report only to
the caller-supplied local path. It has no network, database or product-runtime
dependency.

## Evidence

The immutable Schema, two deterministic fixtures and golden conformance report
live under `internal/asyncmedia/artifacts`. The runner covers strict JSON,
canonical identity, tenant isolation, decision gates, B6 evidence pins,
privacy, threat classes, lifecycle and compatibility, including negative
cases that reject premature runtime/product semantics.

Runtime implementation must not begin until the User resolves P7-D03 and a
new version records the chosen semantics.
