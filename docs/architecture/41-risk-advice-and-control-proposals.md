# Risk Advice And Evidence-to-Control Proposals

> Contract: `unirouter.risk-control-review.v1`
> Scope: deterministic repository-local advice and shadow evidence
> Runtime enforcement/promotion: not authorized

## Outcome

The B12 composition layer turns three already-versioned product facts into one
bounded review:

1. B8 Governance Digital Twin supplies the exact credential-free route, policy
   and Provider Pack change simulation.
2. B10 Agent Trust supplies effective authority, attenuation, TTL, revocation
   and a fixture-only cryptographically valid decision receipt.
3. B11 Provenance Passport supplies exact Source bytes/hash, the shared
   Reference projection identity and explicit Capture Gaps.

It produces categorical routing advice, a bounded assertion and a control
proposal that stops at human review. It does not introduce another route
evaluator, authority model, provenance model or editable protocol source.

## Composition Boundary

The request pins the exact B8, B10 and B11 schema, fixture and golden-report
artifacts. Nested B10 and B11 objects are decoded and validated by their
authoritative product evaluators; B12 does not copy their field semantics into
a competing schema.

The evidence binding includes:

- the provenance reference signed by the B10 receipt;
- the B10 input semantic hash and receipt-payload hash;
- the B11 input semantic hash;
- the exact completed-execution Source and projected Envelope hashes;
- the exact Source-local `org_id` / `project_id` / `source_instance` scope
  hash derived from the preserved Source Snapshot bytes.

Any B10 tenant, Audience, timestamp, receipt, Source, Envelope, Source-scope or
semantic-hash mismatch fails closed. There is intentionally no claim that the
B10 opaque `tenant_ref` is already mapped to B11's numeric Source
`org_id` / `project_id`. That product-local mapping has no authoritative
artifact in the current evidence chain, so B12 reports
`unverified_product_local_mapping` and always creates a separate IAM review
control. Missing mapping evidence cannot silently become a trusted binding.

## Bounded Assertion

`unirouter.bounded-risk-assertion.v1` contains:

- exact tenant and `unirouter-risk-control-review` Audience;
- subject, issue time and five-minute expiry;
- monotonic positive decimal risk epoch;
- `routing_advice_only` scope;
- categorical authority, change, provenance and financial dimensions;
- exact B8/B10/B11 evidence hashes;
- `downscope_only` authority effect;
- `missing_evidence_can_increase_privilege=false`;
- no synchronous dependency and no runtime-enforcement authority.

The local artifact is intentionally `not_issued_offline`. It is not a signed
production policy object. The B10 fixture receipt remains
`fixture_local`: cryptographic validity is evidence, not a production trust
root. A future signer, trust-root registry and runtime verifier remain separate
promotion work and cannot be inferred from this contract.

## Deterministic Advice

The advice order is fail closed:

1. invalid evidence never yields a report;
2. denied authority, breaking change or unknown financial finality yields
   `deny_advice`;
3. shadow-only authority, review-required change or known Capture Gap yields
   `shadow_only`;
4. only fully bounded low-risk evidence yields `allow_baseline`.

These are advice results, not Gateway decisions. Known Capture Gaps are not
called safe, and the fixed completed-execution gaps do not by themselves
become a production denial.

## Evidence-to-Control Lifecycle

The proposal lifecycle is:

1. `evidence_derived_draft`;
2. `offline_simulated`;
3. `human_review_required`.

The fixture derives four owner-specific suggestions:

- `unirouter.iam`: preserve or reduce current authority only;
- `unirouter.routing`: evaluate the candidate off path without client
  delivery;
- `unirouter.evidence`: review declared Capture Gaps before promotion.
- `unirouter.iam`: verify the product-local tenant-to-Source scope mapping
  before any promotion.

Every proposed control has `enforcement_authorized=false`. The proposal cannot
self-promote, cannot modify runtime state and records `P7-D04` as its external
promotion gate.

## Runtime And Product Ownership

- UniRouter remains the final owner of IAM, routing, policy, billing and local
  enforcement.
- No synchronous X-Insight or remote risk dependency is added.
- No provider request, paid or live shadow request, credentials, database,
  Cursor or Checkpoint is accepted.
- The completed-execution Feed endpoint, DTO, Source bytes/hash, event
  identity, page, Cursor and credential behavior are unchanged.
- Risk analysis can only reduce existing authority. It cannot grant a model,
  endpoint, budget, tenant or action absent from the baseline.
- Irreversible actions remain subject to checks by the component that owns the
  side effect.

## Promotion Gate

P7-D04 remains open. Before any enforcement, the User must choose the pilot
cohort, measurable KPI and threshold/rollback semantics. That later decision
must also define a production signing/trust-root lifecycle and prove shadow
evidence against representative workloads. B12 provides replay, advice,
simulation and proposal evidence only.
