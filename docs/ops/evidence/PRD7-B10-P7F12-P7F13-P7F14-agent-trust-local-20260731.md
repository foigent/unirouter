# PRD7 B10 Agent Trust — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B10-AGENT-TRUST`
> Requirements: P7-F12, P7-F13, P7-F14
> Result: Local verified
> Runtime/external authority: none

## Outcome

UniRouter now has one metadata-only Agent Trust review plane that combines the
existing Agent capability attenuation model, ancestor budget lineage,
TTL/revocation proof, an Ed25519 signed decision receipt and the existing B8
Governance Digital Twin evaluator.

It does not create a second capability runtime, receive signing keys, trust a
fixture key in production, add a synchronous risk dependency or authorize
runtime enforcement.

## Verified Semantics

- effective authority is a rooted acyclic parent-first tree;
- every child uses the current `agentcap.Limits` validation and may only retain
  or reduce parent depth, topology, cost, token, rate, concurrency, model and
  endpoint authority;
- child expiry cannot exceed parent expiry;
- inactive/expired/revoked/ancestor-revoked nodes have no effective authority;
- every direct revocation has one exact digest event and six-digit UTC time;
- ancestor budget evidence includes the full lineage used by shared runtime
  counters without claiming new runtime enforcement;
- the selected Agent and the B8 diff deterministically produce
  allow/shadow-only/deny advice;
- canonical signed bytes bind key ID/version, tenant, audience, decision,
  authority, route/policy/Pack hashes and provenance;
- payload, signature, key-version, audience, widening, TTL and revocation
  negatives fail closed;
- fixture signature validity remains explicitly different from production
  trust-root authorization;
- every report says no synchronous runtime dependency, no runtime enforcement
  authorization and `runtime_mutation=none`.

## Immutable Artifacts

| Artifact | SHA-256 |
|---|---|
| `agent-trust-review.v1.schema.json` | `1e375c8febfd93e9972cdaab88c15c1aecf08537fc56ee1a2dfc5e6b037c6a37` |
| `trust-firewall-review.v1.json` | `4a8bef5a7500149252516fc081b18547e8e6af8657830153aff125c221849dba` |
| `core.v1.json` golden report | `032dd42f8ed7971d19f6f921838cd5efac432bcbde2266d4222095c7732e1653` |
| pinned B8 schema | `02e00f49039d813b7ef2a739c2c0f32200a7f9df68e0bd0356a84c1927d4e123` |
| pinned B8 fixture | `0ee09c4965e659f1c004ab9c1eea9be45f354fe960a39a645a55f07e0e95f1c7` |

One fixture passes `22/22` deterministic conformance cases.

## Product Surface

- zero-network CLI conformance runner;
- no-store platform/org-admin contract and preview API;
- **Agent Trust Firewall** under **访问与安全**;
- auto-run safe fixture and auto-selected first authority node;
- persistent Trust Inspector at desktop and stacked narrow widths;
- no secondary “open details” interaction.

## Verification

Passed before closeout:

```text
go test ./internal/agenttrust -count=1
go test -race ./internal/agenttrust -count=1
go test ./internal/admin -run TestAgentTrust -count=1
go test ./cmd/uniroute -run TestAgentTrust -count=1
go run ./cmd/uniroute-agent-trust-conformance --report <a>
go run ./cmd/uniroute-agent-trust-conformance --report <b>
cmp <a> <b>
cmp <a> internal/agenttrust/artifacts/reports/core.v1.json
npm --prefix frontend test -- --run \
  src/pages/__tests__/AgentTrustPage.test.tsx \
  src/api/__tests__/client.test.ts src/__tests__/navigation.test.tsx
go test ./... -count=1
go vet ./...
npm --prefix frontend test -- --run
npm --prefix frontend run typecheck
npm --prefix frontend run build
```

Closeout results:

- Agent Trust ordinary and race tests: pass;
- API and route authority tests: pass;
- conformance: `22/22`, byte-exact golden;
- targeted frontend: `3` files / `67` tests pass.
- full Go suite and `go vet ./...`: pass;
- full frontend suite: `45` files / `324` tests pass;
- frontend type-check and production build: pass;
- synthetic-browser verification at `1440x1000` and `390x844`: no horizontal
  overflow, persistent inspector visible without a detail-opening action,
  selection and signed-receipt evidence rendered, and browser console
  `0` errors / `0` warnings.

## Compatibility And Non-Claims

- Existing `agentcap` token, delegation, ancestor counter, TTL, refresh and
  revocation behavior remains unchanged.
- Existing B8 route/policy evaluator and Provider Change Firewall artifacts
  remain unchanged and are called directly.
- Completed-execution Feed bytes/hash, event/Outbox identity, endpoint,
  credentials, page and cursor behavior remain unchanged.
- No signing private key was committed, accepted by an API or rendered in UI.
- No runtime trust root, enforcement threshold, policy promotion or pilot
  cohort was selected.
- No deployment, traffic, credentials, database, cursor/checkpoint, push, PR
  or merge occurred.
- This is Local verified, not CI, Target, External integration or Production
  evidence.
