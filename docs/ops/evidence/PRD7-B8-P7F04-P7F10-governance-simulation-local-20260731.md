# PRD7 B8 Governance Simulation And Change Firewall — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B8-GOVERNANCE-SIMULATION-CHANGE-FIREWALL`
> Requirements: P7-F04, P7-F10
> Result: Local verified
> External/runtime authority: none

## Outcome

UniRouter now has a strict offline route/policy simulator, Provider Change
Firewall and progressive-release evidence chain. Gateway runtime routing and
the simulator share one credential-free pure evaluator, so the simulation is
not a second semantic implementation. The product workspace keeps a right-side
Governance Inspector visible, automatically selects the first change and makes
the no-promotion boundary explicit.

## Implementation

- `internal/routingpolicy.EvaluateSelection` now owns channel/circuit/key
  eligibility, half-open sampling, deterministic key choice, priority/static
  weight ordering, candidate bounds and exclusion summaries.
- `internal/gateway.Router.evaluateSnapshot` adapts the existing loaded
  snapshot into that pure type and maps selected channel/key IDs back to the
  unchanged runtime objects. It does not expose secrets or change snapshot
  loading, mapping, failover or probe behavior.
- `internal/governancesim` validates at most 2 MiB of strict JSON, exact int64
  decimal IDs and six-digit UTC timestamps; evaluates baseline/candidate route
  and full policy snapshots; and computes stable content identities.
- Provider Change Firewall classifies Pack add/remove, SemVer movement,
  same-version drift, schema/manifest/fixture identity, endpoint/modality/auth
  changes and usage/error semantic changes as unchanged, compatible,
  review-required or breaking.
- The isolated in-memory service proves semantic duplicate/no-op, identity
  conflict, CAS progression, stale revision rejection and rollback-to-recorded
  revision. It is not wired as a production release controller.
- `shadow`, `canary` and `enforced` are only evaluation modes. Every report
  keeps `runtime_mutation=none`,
  `runtime_promotion_authorized=false` and
  `promotion_gate=external_explicit_authority_required`.
- Platform-only no-store contract/preview routes, OpenAPI/backend/frontend API
  docs and `cmd/uniroute-governance-conformance` are included.
- The **变更防火墙** workspace loads a synthetic safe example, accepts local
  JSON, shows Provider and route/policy diffs, and keeps the evidence/promotion
  inspector persistent without a second detail-open action.

## Immutable artifacts

| Artifact | SHA-256 |
|---|---|
| `governance-simulation.v1.schema.json` | `02e00f49039d813b7ef2a739c2c0f32200a7f9df68e0bd0356a84c1927d4e123` |
| `route-pack-review.v1.json` | `0ee09c4965e659f1c004ab9c1eea9be45f354fe960a39a645a55f07e0e95f1c7` |
| `core.v1.json` golden report | `24ecba6ec88ee5901dd0dd6c017dd1269f3426933ccce645f6c793b2019291f1` |

Two independent conformance invocations were byte-identical to each other and
to the embedded golden. The report records `1` fixture, `17` cases, `17`
passed and `0` failed.

## Verification

Passed:

```text
go test ./internal/governancesim ./internal/routingpolicy ./internal/gateway -run ... -count=1
go test ./internal/admin ./cmd/uniroute -run 'Test(Governance|PreviewGovernance|Conformance)' -count=1
go test ./cmd/uniroute -run 'Test(RouteParity|OpenAPI|GovernanceSimulation)' -count=1
go vet ./...
go test ./... -count=1
npm run test -- --maxWorkers=1 --fileParallelism=false
npm run type-check
npm run build
go run ./cmd/uniroute-governance-conformance --report <report-a>
go run ./cmd/uniroute-governance-conformance --report <report-b>
cmp <report-a> <report-b>
cmp <report-a> internal/governancesim/artifacts/reports/core.v1.json
git diff --check
```

Results:

- full Go package suite: pass;
- full Go vet: pass;
- frontend: `43` files / `316` tests pass;
- frontend type-check and production build: pass;
- targeted API/UI/navigation/docs tests: `71` tests pass;
- exact current OpenAI Pack schema/manifest/fixture identities match the
  Provider Pack catalog in conformance;
- existing P6-F08 Gateway deterministic-weight, snapshot fallback, safe
  preview and bounds tests pass after the shared-kernel extraction;
- conformance: `17/17` pass, including blocked-breaking-change
  non-advancement, and report bytes match the golden;
- `git diff --check`: pass at closeout.

## Real-browser synthetic verification

The Playwright CLI loaded the actual page/CSS with route-scoped synthetic
responses only:

- 1440×1000: page width `1440/1440`; explorer width `1144`; inspector visible
  at approximately `457` CSS px; first Provider row automatically selected.
- 390×844: page width `390/390`; explorer width `366`; inspector stacks after
  the main panel at `364` CSS px, remains directly reachable without an
  open-detail action, and retains the selected first Provider change.
- mobile inspector renders `runtime mutation = none` and no runtime promotion
  authorization.
- final browser console: `0` errors, `0` warnings.

The visual pass found one readability issue: the four-character overall-risk
value wrapped in the narrow KPI card. The KPI value was shortened to
“需评审” while the full “需要评审” label remains in tables and the inspector.

Screenshots:

- `output/playwright/.playwright-cli/page-2026-07-30T22-31-33-701Z.png`
- `output/playwright/.playwright-cli/page-2026-07-30T22-33-12-653Z.png`

This browser evidence is synthetic local UI evidence, not Target or
Production evidence.

## Compatibility and non-claims

- Existing Gateway route inputs/results, channel/key objects, mapped models,
  snapshot source/version, fallback, probe and failover semantics remain
  unchanged.
- No credential, upstream base URL, prompt, response or provider call enters
  the simulation contract.
- No route, policy, channel, key, Pack, release, database, customer ledger or
  supplier ledger state was changed.
- Existing completed-execution Feed endpoint, DTO, source bytes/hash,
  event/Outbox identity, credentials, page and cursor behavior remain
  unchanged.
- No deployment, traffic, credential, database, cursor/checkpoint, push, PR or
  merge action occurred.
- This is Local verified, not CI, Target, External integration or Production
  evidence.
- Runtime promotion remains an explicit later authority boundary.
