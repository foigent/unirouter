# PRD7 B12 Risk Advice And Evidence-to-Control Local Evidence

> Date: 2026-07-31
> Evidence level: Local verified
> Runtime/external evidence: none
> Promotion gate: P7-D04 remains open

## Implemented

- Added `internal/riskcontrol` as a composition-only package over B8
  `governancesim`, B10 `agenttrust` and B11 `provenancepassport`.
- Added strict `unirouter.risk-control-review.v1` decoding, exact artifact
  dependency pins and evidence-hash binding.
- Added an exact hash over Source-local `org_id`, `project_id` and
  `source_instance`; the absent B10 tenant-to-Source scope mapping is surfaced
  as `unverified_product_local_mapping` and a mandatory IAM review proposal,
  rather than being inferred.
- Added categorical authority/change/provenance/financial dimensions and
  deterministic `allow_baseline`, `shadow_only` and `deny_advice` results.
- Added a five-minute, tenant/Audience/epoch-bound
  `unirouter.bounded-risk-assertion.v1` whose authority effect is
  `downscope_only`.
- Added evidence-derived, simulated, human-review-required control proposals
  with action-owner separation and no self-promotion.
- Added replay duplicate-noop/conflict behavior in caller-owned memory for
  isolated contract tests.
- Added no-store org-admin API endpoints and a persistent
  `/risk-control` inspector. The safe fixture auto-runs and the right-hand
  inspector is present without a detail-open click.

## Immutable Local Artifacts

| Artifact | SHA-256 |
|----------|---------|
| `internal/riskcontrol/artifacts/risk-control-review.v1.schema.json` | `1881dce71817e4d76d89809c96f6a74b764a67d8ba20a3e88d3f58813bf1fb40` |
| `internal/riskcontrol/artifacts/fixtures/shadow-control-review.v1.json` | `0386af70534b11c4c010192184f013c2b8db3199629cd2923817f228a2933469` |
| `internal/riskcontrol/artifacts/reports/core.v1.json` | `19e757b8d65013c316f4d3d0109521158c5cdf92ab56ee514b94683e978f3360` |

The fixture pins nine exact B8/B10/B11 Schema, fixture and golden-report
dependencies. Conformance result: 1 fixture, 28/28 cases and 32 distinct
classes.

## Verified Behaviors

- strict JSON, duplicate-key, unknown/secret field and sub-microsecond
  rejection;
- tenant, Audience, TTL, epoch and evaluation-time binding;
- B10 signature-tamper and semantic-hash rejection;
- B11 Source/Envelope hash and Source-byte tamper rejection;
- exact Source-local scope extraction and scope-hash tamper rejection;
- explicit proof that the tenant-to-Source scope binding is still unverified,
  with no privilege-bearing inference;
- exact current settled Source hash
  `sha256:5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27`;
- exact projected Envelope hash
  `sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2`;
- B8 evaluator reuse rather than a second routing implementation;
- deterministic report/assertion/proposal hashes;
- duplicate-noop, semantic conflict and concurrent replay behavior;
- org-admin API authority and no-store responses;
- frontend contract-body pass-through, role navigation and persistent
  inspector interaction.

## Commands

The following repository-local checks were executed after implementation:

```text
GOCACHE=/private/tmp/unirouter-go-cache go test ./internal/riskcontrol -count=1
GOCACHE=/private/tmp/unirouter-go-cache go test ./internal/riskcontrol ./internal/admin ./cmd/uniroute -run 'RiskControl|riskcontrol' -count=1
GOCACHE=/private/tmp/unirouter-go-cache go run ./cmd/uniroute-risk-control-conformance --report /private/tmp/unirouter-risk-control-report-a.json
GOCACHE=/private/tmp/unirouter-go-cache go run ./cmd/uniroute-risk-control-conformance --report /private/tmp/unirouter-risk-control-report-b.json
cmp /private/tmp/unirouter-risk-control-report-a.json /private/tmp/unirouter-risk-control-report-b.json
cmp /private/tmp/unirouter-risk-control-report-a.json internal/riskcontrol/artifacts/reports/core.v1.json
GOCACHE=/private/tmp/unirouter-go-cache go test ./...
GOCACHE=/private/tmp/unirouter-go-cache go vet ./...
npm test -- --run src/pages/__tests__/RiskControlPage.test.tsx src/api/__tests__/client.test.ts src/__tests__/navigation.test.tsx
npm run type-check
npm run build
node node_modules/vitest/vitest.mjs run --maxWorkers=1 --reporter json --outputFile /private/tmp/unirouter-b12-vitest-serial.json
```

- risk-control package: pass;
- focused Admin/route tests: pass;
- frontend focused suites: 71/71 pass;
- risk-control CLI reports: byte-identical across two runs and byte-identical
  to the committed golden report;
- full Go suite: pass. The first sandboxed run could not bind the loopback
  ports required by existing `httptest.NewServer` cases; the same exact
  command passed outside that sandbox restriction;
- `go vet ./...`: pass;
- full frontend serial suite: 124/124 suites and 332/332 tests pass;
- TypeScript type-check: pass.
- production frontend build: pass. Vite reports the pre-existing large-chunk
  advisory and emits the bundle successfully.

## Browser Verification

The repository page was loaded through a temporary localhost Vite harness with
synthetic B12 API responses. Playwright verified:

- desktop `1440×1000`: title, advice, unsigned assertion and P7-D04 gate
  visible; persistent inspector visible; four proposal rows; explicit
  unverified tenant-to-Source mapping; zero
  detail-open buttons; body `scrollWidth=clientWidth=1440`;
- selecting `tenant-source-scope-binding-review` updates the inspector in
  place to the explicit product-local mapping review and still shows no
  enforcement/promotion authority;
- mobile `390×844`: persistent inspector visible at 364 px; body and main
  `scrollWidth=clientWidth=390`; zero detail-open buttons;
- browser console: 0 errors and 0 warnings.

The harness used synthetic fixture data and was stopped after verification. It
did not authenticate to or mutate any product runtime.

## Explicit Non-Claims

- The risk assertion is `not_issued_offline`; it is not a signed production
  policy or trust-root assertion.
- The B10 fixture receipt is cryptographically valid but remains
  `fixture_local` and `runtime_trusted=false`.
- The B10 opaque tenant and B11 numeric org/project scopes are not claimed to
  be authoritatively mapped; this remains an explicit product-local evidence
  gap and blocks promotion.
- No runtime policy was read, written, promoted or enforced.
- No provider, X-Insight, live/paid shadow, credential, database, Cursor or
  Checkpoint action occurred.
- No deployment, push, PR or merge occurred.
- Risk-routing effectiveness, KPI attainment and production safety are not
  claimed. They remain gated by P7-D04.
