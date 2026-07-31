# PRD7 B11 Provenance Passport — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B11-PROVENANCE-CONNECTOR`
> Requirements: P7-F15, P7-F16
> Result: Local verified
> Runtime/external authority: none

## Outcome

UniRouter now has one read-only Provenance & Capture-Gap Passport that maps
exact current completed-execution Source bytes through the existing Reference
compatibility bridge and keeps direct, derived, declared and missing evidence
separate.

The workspace also records the exact shared-authority LiteLLM Connector
Manifest/mapping/compatibility/conformance/vector identities and its
independent Go/TypeScript `20/20` case, `11/11` class result. LiteLLM remains
conformance-only: UniRouter does not register or implement its runtime and
does not own its credentials, Cursor or Checkpoint.

## Verified Semantics

- the embedded settled Source fixture is byte-identical to
  `contracts/completed-execution/v1/fixtures/valid-settled.json`;
- Source SHA, Envelope SHA and idempotency key remain the existing Golden
  values;
- the report groups 9 direct, 2 derived, 2 declared and 4 missing facts;
- every missing fact retains an exact Capture Gap ID, reason, observed time,
  payload policy and field-level direct/derived/declared provenance;
- current Feed endpoint, DTO, page schema, event identity, source bytes,
  credential and Cursor behavior remain unchanged;
- the current runtime registry still contains only
  `unirouter.completedfeed.reference-v1`;
- product CI remains pinned to the exact shared protocol authority commit;
- the B3 OpenInference Golden is recorded as a metadata observer, not Source
  truth or a Connector runtime;
- duplicate semantic input is a no-op and the same Passport ID with different
  semantics conflicts;
- duplicate keys, delivery fields, malformed base64, missing fixed Gaps, raw
  prompt fields, revision drift and projection-digest drift fail closed;
- every result states no synchronous dependency and
  `runtime_mutation=none`.

## Architecture Finding Closed During Verification

The first full Go run correctly failed
`TestPhase2CProductionEntryAllowsOnlyRuntimeBootstrap` because the initial
Passport draft imported `internal/eventconnectorshadow`. That package is
reserved for the production bootstrap.

The test was not weakened. The Passport now calls
`completedfeed.BuildReferenceEnvelopeV1`; existing three-path differential
tests prove that compatibility bridge and the neutral Adapter produce the same
canonical bytes, Envelope bytes, hashes and identities. The corrected full
suite passes and the neutral runtime Adapter again has exactly one importer:
`cmd/uniroute`.

## Immutable Artifacts

| Artifact | SHA-256 |
|---|---|
| `provenance-passport.v1.schema.json` | `4b577521cc5b7ce2a2e5ff6f38e21c34e367e7bde04153da399f02304fafe4db` |
| `settled-passport.v1.json` | `545b9555a83308c52ac62f6be728fb4ee2bae9571e0346857e716d95df0aee08` |
| `core.v1.json` Golden report | `aa4d5d6570d735ba90ca44b02aa1fad00b2f37fa4706c3a13dd4cd96913acc75` |

One fixture passes `23/23` deterministic conformance cases.

## Product Surface

- zero-network CLI conformance runner;
- no-store platform/org-admin contract and preview API;
- **来源与缺口 Passport** under **监控与用量**;
- auto-run safe fixture and auto-selected Reference Connector;
- persistent Passport Inspector at desktop and stacked narrow widths;
- in-place Reference/LiteLLM selection with no secondary detail-opening
  action.

## Verification

```text
go test ./internal/provenancepassport ./internal/admin ./cmd/uniroute \
  -run 'Test(Provenance|PinnedProtocol|ProductRuntime|FixtureContains|EvaluateBuilds|ConformanceAndGolden|MemoryReplay)' \
  -count=1
go test -race ./internal/provenancepassport -count=1
go test ./internal/eventconnectorshadow ./internal/provenancepassport -count=1
go run ./cmd/uniroute-provenance-conformance --report <a>
go run ./cmd/uniroute-provenance-conformance --report <b>
cmp <a> <b>
cmp <a> internal/provenancepassport/artifacts/reports/core.v1.json
go test ./... -count=1
go vet ./...
npm --prefix frontend test -- --run \
  src/pages/__tests__/ProvenancePassportPage.test.tsx \
  src/api/__tests__/client.test.ts src/__tests__/navigation.test.tsx
npm --prefix frontend test -- --run
npm --prefix frontend run type-check
npm --prefix frontend run build
```

Results:

- Provenance Passport ordinary and race tests: pass;
- API and route authority tests: pass;
- conformance: `23/23`, two runs byte-identical to Golden;
- targeted frontend: `3` files / `69` tests pass;
- full Go suite and `go vet ./...`: pass;
- full frontend suite: `46` files / `328` tests pass;
- frontend type-check and production build: pass;
- synthetic-browser verification at `1440x1000` and `390x844`: no horizontal
  overflow, one selected Connector, persistent inspector visible/reachable,
  Reference/LiteLLM evidence switches in place, no detail-opening action, and
  browser console `0` errors / `0` warnings.

The production build retains the repository's existing large-chunk warning;
it is not introduced by a failed build and is not a runtime verification.

## Compatibility And Non-Claims

- Completed-execution Source DTO bytes/hash, terminal facts, Outbox/event
  identity, Feed endpoint, credentials, page and Cursor behavior are
  unchanged.
- No shared Schema, Manifest, mapping, vector or conformance contract was
  copied or redefined in this product repository.
- No LiteLLM adapter, vendor branch, runtime process, credential, Cursor,
  Checkpoint or network call was added.
- No OpenInference collector call or claim of Source authority was added.
- No deployment, traffic, credentials, database, Cursor/Checkpoint, push, PR
  or merge occurred.
- This is Local verified, not CI, Target, External integration or Production
  evidence.
