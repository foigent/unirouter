# Agent Trust Firewall, Signed Receipt And Digital Twin

> Contract: `unirouter.agent-trust-review.v1`
> Report: `unirouter.agent-trust-report.v1`
> Receipt: `unirouter.signed-decision-receipt.v1`
> Runtime authority: none

## Purpose

B10 turns the existing Agent capability kernel and B8 governance simulator
into one reviewable trust workspace without creating a second IAM system or a
remote risk dependency.

The implementation reuses:

- `agentcap.Limits.ValidateShape`, normalization and `IsAttenuationOf` for the
  same model-gateway capability ceiling used by runtime delegation;
- the current three model-gateway endpoint boundary;
- the B8 `governancesim.Evaluate` path, including the production routing-policy
  evaluator, immutable route/policy/Provider Pack comparison and explicit
  no-promotion result;
- Ed25519 primitives from the standard library and the existing strict signing
  key identifier rules.

## Authority Model

The request contains no capability token or root secret. Each node has a
portable digest identity, parent, depth, authority version, issued/expiry time,
optional revocation time and current `agentcap.Limits`.

Validation fails closed when:

- a child is not a strict subset/equal ceiling of its parent;
- topology, maximum depth, canonical ordering or endpoint scope is invalid;
- a child outlives its parent;
- a revocation is absent, duplicated, future-dated or does not match the node;
- the selected Agent is not in the snapshot.

The report separates:

- effective node state (`active`, `expired`, `revoked`,
  `ancestor_revoked`);
- attenuation fields and removed model/endpoint sets;
- the complete ancestor budget lineage used by the existing shared runtime
  counters;
- remaining TTL and exact revocation event evidence.

Inactive nodes have no effective limits in the authority tree. Their static
attenuated ceilings remain visible only in the budget proof so reviewers can
audit what was revoked.

## Signed Decision Receipt

The canonical metadata-only receipt binds:

- receipt/review identity;
- tenant, audience and selected Agent;
- decision and six-digit UTC decision time;
- authority snapshot, governance request, baseline/candidate policy and route,
  and Provider Pack set hashes;
- provenance reference;
- `synchronous_dependency=false`;
- `runtime_action_authorized=false`.

The Ed25519 signature input also binds algorithm, key ID, exact decimal key
version and trust profile. Payload, signature, key-version and audience/tenant
tampering all fail closed.

The immutable fixture contains a public verification key and signature only.
Its deterministic private test seed is not committed. A successful signature
therefore proves canonical bytes and cryptographic verification, while the
report deliberately says `runtime_trusted=false`. Production trust-root
selection and signing-key custody are not inferred by B10.

## Governance Digital Twin

The nested request is evaluated by the existing
`unirouter.governance-simulation.v1` implementation. B10 does not copy or
reinterpret the B8 schema. Its fixture pins:

- B8 schema SHA-256
  `02e00f49039d813b7ef2a739c2c0f32200a7f9df68e0bd0356a84c1927d4e123`;
- B8 route/Pack fixture SHA-256
  `0ee09c4965e659f1c004ab9c1eea9be45f354fe960a39a645a55f07e0e95f1c7`.

The local decision is deterministic:

- inactive selected authority or a breaking twin result: `deny`;
- a review-required twin result: `shadow_only`;
- otherwise: `allow`.

All outcomes remain advisory evidence. The API is no-store, has no synchronous
request-path dependency and cannot promote a release or enforce a decision.

## Product Surface

Platform and organization administrators can open **Agent Trust Firewall**
under **访问与安全**. The safe fixture auto-runs, the first authority node is
selected automatically and the Trust Inspector is always present. Selecting a
row changes the inspector directly; there is no secondary detail-open action.

The inspector exposes authority state, lineage, ancestor budget, TTL,
attenuation, revocation, Digital Twin risk, signed receipt verification and
the explicit non-runtime boundary. It never renders private key or capability
token material.

## Non-Goals

- no new root authority, token format, delegation or Redis counter;
- no private-key ingestion, custody, rotation or runtime trust-root selection;
- no live capability revocation or policy promotion;
- no remote risk service or synchronous request-path dependency;
- no provider, database, Outbox, Feed, cursor/checkpoint or billing mutation.
