# Governance Simulation V1

## Outcome

UniRouter now has one credential-free routing evaluation kernel shared by
Gateway runtime selection and the offline Governance Simulator. The simulator
compares immutable baseline/candidate route, policy and Provider Pack
snapshots; classifies change risk; and emits replayable progressive-release
evidence. It has no path that writes or promotes runtime state.

The contract is `unirouter.governance-simulation.v1`. Reports are
`unirouter.governance-simulation-report.v1`; release intents and receipts use
`unirouter.progressive-release.v1`.

## One Routing Semantics Source

`internal/routingpolicy.EvaluateSelection` is a pure function over a normalized,
credential-free `SelectionSnapshot`. It owns:

- channel/circuit/key eligibility and exact exclusion reasons;
- half-open deterministic sampling;
- deterministic eligible-key choice;
- priority-tier ordering and equal-priority static-weight ordering;
- candidate bounds and deterministic exclusion summaries.

`internal/gateway.Router.evaluateSnapshot` converts its internal snapshot into
that type and maps selected IDs back to the existing channel/key objects.
Secrets, base URLs and adapter configuration never enter the policy kernel or
the offline report. Existing Gateway snapshot loading, route result, mapped
model, failover and probe behavior remain unchanged.

## Strict Offline Input

The simulator accepts at most 2 MiB and rejects nested duplicate JSON keys,
unknown fields, trailing JSON, unsafe/missing identities and invalid policy
bounds. Route and key IDs are canonical positive int64 decimal strings. Times
must be UTC with exactly six fractional digits; sub-microsecond values fail.

Each bundle contains:

- the full validated `routing-config.v2` configuration;
- one credential-free route snapshot with a declared synthetic, fixture or
  sanitized-export source;
- sorted Provider Pack snapshots with exact schema, manifest and fixture
  SHA-256 identities.

Raw credentials, upstream base URLs, request payloads and provider responses
are not fields in the contract. HTTP preview uses `Cache-Control: no-store` and
does not persist the supplied body.

## Provider Change Firewall

The firewall produces one deterministic result per Pack:

| Change | Classification |
|---|---|
| no semantic or identity change | `unchanged` |
| versioned manifest-only compatible refresh | `compatible` |
| new Pack, endpoint, modality, auth profile or fixture | `review_required` |
| same-version drift, downgrade, major version, removal, fixture digest drift, adapter/provider/schema/usage/error semantic change | `breaking` |

Additions still require independent conformance before promotion. A breaking
advance is reported as `blocked_breaking_change`. Nothing is automatically
enabled.

## Progressive Release Evidence

Release lifecycle evidence is append-only and CAS-checked in the isolated
in-memory service:

`draft -> validated -> shadow -> canary -> approved_for_external_promotion`

Rollback creates a new revision and must point to a prior recorded revision
whose stage matches the requested target. Duplicate semantic simulations are
`duplicate_noop`; the same simulation ID with changed semantics conflicts.
Stale parent revision/stage conflicts fail closed.

`shadow`, `canary` and `enforced` are simulation target modes. They are not
runtime lifecycle stages and do not grant runtime authority. Every receipt
states:

- `runtime_mutation = none`;
- `runtime_promotion_authorized = false`;
- `promotion_gate = external_explicit_authority_required`.

## Surfaces

- `GET /api/v1/governance-simulation/offline-contract`
- `POST /api/v1/governance-simulation/offline-preview`
- `cmd/uniroute-governance-conformance`
- platform-only **变更防火墙** workspace with an always-visible Governance
  Inspector and automatic first-change selection

The endpoints are platform-admin only. The contract endpoint returns exact
artifact identities, credential-free current Provider Pack identities and one
synthetic request. The preview endpoint runs only the pure evaluator.

## Immutable Local Artifacts

| Artifact | SHA-256 |
|---|---|
| `governance-simulation.v1.schema.json` | `02e00f49039d813b7ef2a739c2c0f32200a7f9df68e0bd0356a84c1927d4e123` |
| `fixtures/route-pack-review.v1.json` | `0ee09c4965e659f1c004ab9c1eea9be45f354fe960a39a645a55f07e0e95f1c7` |
| `reports/core.v1.json` | `24ecba6ec88ee5901dd0dd6c017dd1269f3426933ccce645f6c793b2019291f1` |

The golden report contains 17 passing cases covering the shared route kernel,
determinism, exact current Pack identity, SemVer risk, runtime boundaries,
replay, progression, rollback, stale CAS, blocked-change non-advancement,
strict JSON/time negatives and route exhaustion.

## Non-Goals And Unchanged Behavior

- no production release-state persistence or automatic runtime promotion;
- no live shadow/canary traffic or provider/credential discovery;
- no routing, policy, channel, key, Provider Pack or database mutation;
- no customer billing or supplier-cost mutation;
- no change to completed-execution Feed source bytes, event/Outbox identity,
  endpoint, page, cursor, credentials or X-Insight compatibility.

The in-memory lifecycle service is conformance evidence, not a production
release controller.
