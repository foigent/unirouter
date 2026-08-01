# PRD8 P7-F04 / P7-F10 / P7-F12 Runtime Controls — Local Evidence

> Work item: `UR-PRD8-RUNTIME-CONTROLS`
> Date: 2026-08-02
> Status: Local verified; CI/Target/release evidence pending
> Baseline commit/tree: `c0182861c7ec1079e335e317863f7ec5bcabd731` /
> `0bfeed8938eb6235ec20db05450f2472e43460a3`

## Closed product gap

This batch closes the UniRouter-owned gap between the PRD7 offline fixtures and
the PRD8 product surfaces without importing X-Insight, Tool Gateway or MCP
Gateway authority:

1. P7-F04 now has an activation-time gate. A runtime candidate binds its exact
   Provider Pack version and Manifest SHA; breaking simulation, Pack drift,
   channel epoch drift and direct activation bypass fail closed.
2. P7-F10 now has a product-local progressive release controller for channel
   admission, priority and weight only. It supports draft, validation, shadow,
   deterministic canary, promotion and rollback.
3. P7-F12 is narrowed and renamed to Model Call Authorization. It is a real
   request PEP for model and Endpoint access, while the old Agent Trust fixture
   remains explicitly offline evidence.

P7-F17 risk assertions and P7-F18 Finding-to-proposal automation are not part
of this batch. They remain read-only/manual-stop evidence.

## Runtime release invariants

- Migration `000052_prd8_runtime_change_control` stores immutable candidates,
  hashes, exact Provider Pack pins, append-only transition events and the
  stable/canary slot for each model.
- Lifecycle transitions use expected revision checks. Canary is 1–5,000 basis
  points, one canary may exist per model, and the same model/request/release
  identity produces the same assignment.
- The overlay accepts only channel `admission`, `priority` and `weight`.
  Endpoint URL, credential, model mapping, retry, circuit breaker, IAM and
  pricing cannot be changed through this path.
- A channel marked `disabled` is excluded even when a release says `allow`.
  Stable-release references prevent direct active-channel mutation, and new
  channels remain staged until a release advances them.
- Rollback restores the parent stable assignment and remains available after
  emergency disable or channel epoch drift.

## Model authorization invariants

- Migration `000053_prd8_model_call_authorization` stores organization-scoped
  API Key, project, user and Agent policies with draft/active/revoked status,
  revision, actor, reason and optional validity window.
- Policies cover only the configured environment, requested/resolved model and
  supported UniRouter model Endpoint. Explicit deny wins. Allow rules union
  inside one subject and intersect across matching subjects.
- No matching policy returns `legacy_authority_unchanged`; the layer can only
  narrow already-passed API Key, project membership/model-set and Agent
  capability checks. Tool and MCP permissions are not representable.
- `GET /v1/models` and model-call admission use the same evaluator. Decisions
  add allowlisted policy references and reason metadata to the existing Trace;
  evaluation performs no runtime mutation.

## Verification performed

The following commands ran against the current working tree and passed:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

Frontend results: 50/50 test files and 347/347 tests passed. The production
build processed 4,313 modules with no 500 kB chunk warning; entry JavaScript is
275.35 kB minified / 87.11 kB gzip. The maximum minified JavaScript chunk is
376.63 kB / 100.87 kB gzip, and maximum gzip JavaScript payload is 103.75 kB.

The current UI was then loaded in a real in-app Chromium surface through the
local Vite application and a loopback-only synthetic API harness. The review
confirmed:

- all five desktop navigation groups and every second-level item are expanded
  in the same left column; the top row contains one utility toolbar rather than
  a duplicate primary navigation;
- Model Governance → Runtime Release renders the deterministic queue and the
  evidence/transition Inspector without an extra open action;
- Model Call Authorization defaults to the runtime policy view, keeps the old
  Agent fixture in a separate offline-evidence tab, and renders scope/reason/
  lifecycle in the always-present Inspector;
- at 1,440 px both workspaces keep a 480 px side-by-side Inspector; at 1,024 px
  and 768 px the same Inspector remains on the page below the primary panel
  rather than being clipped or hidden; and
- measured document width exactly matched the 1,440, 1,024 and 768 px viewport
  widths. The 1,024 px run initially exposed the pre-fix Inspector clipping;
  the `max-width: 1279px` governance breakpoint was corrected and all three
  widths were rechecked.

The harness contains only synthetic product data and does not establish Target,
Production, Provider, directory or ERP evidence. Vite development output also
contains known React Router future-flag and Semi UI `findDOMNode` deprecation
warnings; these are third-party development-mode warnings, not silently
reported as a zero-warning production run.

An isolated PostgreSQL 16 instance was created under `/private/tmp`, bound to
loopback only, stopped after the run, and used for the exact new CI selection:

```text
REQUIRE_TEST_DATABASE=1 TEST_DATABASE_URL=<isolated-loopback-test-dsn> \
go test -tags=integration -race \
  ./internal/orgdirectory ./internal/enterprisefinance \
  ./internal/runtimechange ./internal/migrate ./cmd/uniroute \
  -run '^(Test(PostgresStore|ProjectionStore|Enterprise|Allocation|PRD8RuntimeChange|PRD8RuntimeControl|PRD8GatewayModelAuthorization).*)$' \
  -count=1 -timeout=20m
```

Result: PASS for all five selected packages. Migration coverage includes
`000047` through `000053`, constraints, down guards and the legacy rollback
helpers. The first combined reproduction exposed that legacy tests tried to
cross migrations `000052`/`000053`; the helper was extended to roll those empty
tables back first, and the exact combined command then passed.

The race run emitted the existing macOS linker `LC_DYSYMTAB` warnings but
returned exit status 0 and all packages passed. These warnings are recorded as
toolchain output, not misreported as test failures or silently omitted.

## Compatibility conclusion

The implementation intentionally adds product-local management tables/APIs and
narrows route/model-call decisions only in the bounded surfaces above. It does
not change request DTOs or stored request records, request-time pricing,
reservation/settlement, customer ledger finality, Outbox identity,
completed-execution DTO/Source bytes/hash, Feed endpoint/ordering/credentials,
Cursor/Checkpoint behavior or the no-default-source-instance rule.

Runtime assignment metadata (`release_ref`, stage and assignment identity) and
Model Call Authorization decision metadata (reason and policy references) are
added only through the existing approved Trace metadata allowlist.

## Evidence boundary and remaining gates

- This file records Local evidence only. GitHub CI, exact deployed commit,
  target migration, health, authenticated UI and browser evidence must be
  recorded after those separately authorized steps actually pass.
- No real Provider call, directory/ERP integration, new credential,
  Cursor/Checkpoint change or external integration was performed during Local
  verification.
- `ORG-3` still needs a real customer HCM/IdP target. `FIN-4` still needs a real
  ERP/GL and chargeback requirement.
- P7-F17 and P7-F18 remain outside runtime enforcement and automatic proposal
  acceptance.
