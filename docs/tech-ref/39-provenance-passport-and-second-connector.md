# Provenance Passport And Second Connector

> Contract: `unirouter.provenance-passport.v1`
> Scope: PRD7 B11 / P7-F15 / P7-F16
> Runtime mutation: none

## 1. Product Boundary

The Passport is a read-only evidence projection. It receives exact base64
completed-execution Source bytes, decodes the current
`unirouter.completed-execution.v1` DTO and calls the existing
`completedfeed.BuildReferenceEnvelopeV1` compatibility bridge. That bridge is
already byte-differential-tested against the neutral runtime Adapter, while
preserving the rule that only the UniRouter runtime bootstrap imports the
neutral Adapter package. The Passport does not define a second Event Envelope,
Payload Schema, Manifest or Connector lifecycle.

The Source bytes are copied before projection and compared afterward. The
report preserves:

- Source schema ID and digest;
- Source instance and revision;
- exact Source byte length and SHA-256;
- current Envelope hash and idempotency key;
- terminal status, capture policy and timeline completeness.

Any invalid Source DTO, prohibited raw field, noncanonical base64, duplicate
JSON key, revision mismatch or incomplete four-Gap set fails closed.

## 2. Four Evidence Classes

The Passport does not infer provenance from display labels. It reads the
Reference Envelope's authoritative `provenance.field_groups` and renders four
separate collections:

| Passport group | Authority |
|---|---|
| `direct` | copied from Source or existing identity facts |
| `derived` | deterministically computed by the Reference Connector |
| `declared` | protocol/adapter policy rather than observed Source fact |
| `missing` | one explicit entry per current Capture Gap |

The settled fixture therefore has 9 direct fields, 2 derived fields, 2
declared fields and 4 missing claims. Each missing claim retains the exact gap
ID, reason, type, severity, observed time, payload policy and per-field
direct/derived/declared provenance.

## 3. Two Connector Roles

The two rows in the workspace intentionally have different runtime ownership.

### UniRouter completed-execution

`foigent.unirouter.completed-execution@1.0.0` remains the first Reference
Connector and the only Connector in UniRouter's product runtime registry. The
Passport directly proves the settled baseline:

- Source hash
  `sha256:5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27`;
- Envelope hash
  `sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2`;
- idempotency key
  `sha256:c2822f0d940c01de77e2e06af1a5f6e1fd10fc66269be20fceb0939150c17b2e`.

### LiteLLM spend logs

`foigent.litellm.spend-logs@1.0.0` is the heterogeneous second Connector
already released by the unique shared protocol authority. UniRouter pins its
Manifest, upstream mapping, compatibility matrix, conformance contract and
vector bundle. The exact authority release has independent Go and TypeScript
reports passing 20 cases across 11 required classes.

The Passport records that evidence; it does not copy or redefine the shared
Schema/Manifest, implement a LiteLLM adapter, add a supplier branch to
UniRouter's core, register a LiteLLM runtime, connect to LiteLLM or own its
credentials, Cursor or Checkpoint. The current runtime registry remains
exactly `unirouter.completedfeed.reference-v1`.

## 4. OpenInference Is An Observer

The B3 `unirouter.telemetry-export.v1` golden identity is pinned in the
Passport as `otel.openinference.metadata-only`. It remains an optional
Outbox-posted observer:

- it does not create Source truth;
- it is not the Foigent Connector runtime;
- it contains no raw prompt/response content;
- it owns no Connector Cursor or Checkpoint.

This distinction prevents an observability projection from silently acquiring
Source, delivery or execution authority.

## 5. Compatibility

The implementation calls the current Reference projector and leaves all
existing contracts untouched:

- endpoint
  `/api/v1/integrations/xinsight/completed-executions`;
- page schema `xinsight.completed-execution-feed.v1`;
- DTO and exact Source bytes;
- event ID, Envelope hash and idempotency identity;
- Feed wire, page, credential and Cursor behavior;
- Outbox, billing, finality and X-Insight availability boundaries.

Delivery-only state is rejected from the Passport input. No database, retained
Feed item, Cursor, Checkpoint or acknowledgement is read or written.

## 6. Product Surface And Verification

Platform and organization administrators receive no-store endpoints:

- `GET /api/v1/provenance-passport/offline-contract`;
- `POST /api/v1/provenance-passport/offline-preview`.

The **来源与缺口 Passport** page auto-runs the safe fixture, auto-selects the
Reference Connector, keeps the inspector visible and switches between
Reference runtime and heterogeneous conformance evidence without a secondary
detail-opening action.

The zero-network runner
`go run ./cmd/uniroute-provenance-conformance --report <path>` validates exact
Source/Envelope identity, provenance groups, fixed Gaps, privacy,
duplicate/conflict semantics, protocol pins, cross-language Connector
attestation and the no-runtime boundary.

## 7. Non-Claims

Local verification does not prove CI, target, external integration or
production behavior. It does not authorize deployment, traffic, credentials,
database mutation, Connector registration, Cursor/Checkpoint changes, a
universal plugin runtime, push, PR or merge.
