# V1 Post-PRD6 Micro-Precision Ledger Local Evidence — 2026-07-14

> Promotion note: this file preserves the pre-activation local/R3 evidence.
> PR #202 later passed required run `29334221779`, merged as `ec0dde2`, and the
> User-authorized development/test ECS activation applied `000036`. PR #203
> then passed run `29339373907`, merged as final target release `545e514`, and
> corrected the frontend deployment label. Current target evidence is
> `docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-TARGET-20260714.md`.

## Status and boundary

- Status: Local verified; Draft PR required CI and R3 merge/deployment approval
  remain pending.
- Baseline: `main` / `origin/main`
  `8cd268549917bb548e8b58a53c4a0f2b135a5781`.
- Decision: the User selected option B, a source-preserving micro-CNY ledger.
- PRD6 remains `Completed (Accepted Scope)`. This release-blocking V1
  remediation does not reopen an accepted PRD6 row or promote target or
  Production evidence.
- No ECS process, target database, target Redis key, credential, provider or
  X-Insight runtime was changed while producing this evidence.

## Issue matrix

| ID | Classification | Impact / priority | Cause | Owner | Resolution and acceptance evidence |
|---|---|---|---|---|---|
| `V1-FIN-001` | confirmed bug/regression | Critical / P0; sub-cent calls could be overcharged and zero use could be charged one cent | Binary floating-point calculation was truncated to integer cents and both calculation and settlement applied a one-cent terminal minimum | UniRouter billing, wallet, gateway, control-plane API/UI | Canonical checked `int64` micro-CNY, exact price snapshots, one half-up total rounding, no terminal minimum, source-preserving cash/grant settlement, migrated PostgreSQL/race tests and real local API/UI readback |
| `V1-PRICE-RANGE-001` | confirmed bug/regression | High / P1 availability and financial-control integrity; an accepted price could make a model fail closed at quote time | Public base-price, cache-ratio and organization-multiplier fields were each bounded, but create/update did not validate their exact effective combination | UniRouter pricing authority API/database transaction | All four base-price/organization-multiplier create/update paths now take a common per-model transaction lock, validate the same exact half-up `int64` boundary as runtime, and return `400` with no write or audit on overflow; pricing pages expose the server reason instead of replacing it with a generic failure. Migrated integration coverage proves each rollback and a concurrent cross-table race permits only one safe commit |
| `V1-PRICE-RENAME-001` | confirmed bug/regression | High / P1 routing and billing availability; renaming one price row can leave the routed model unpriced, orphan organization multipliers or attach pre-existing multipliers to the new name | The price update API treated `model_name` as mutable, while billing joins base and organization prices by exact name and model governance, aliases, model sets and channel mappings own independent references with no rename cascade | UniRouter pricing authority API/UI, with routing and model-governance boundaries | The User selected immutable V1 identity. PUT now rejects a different name before any price write or audit, accepts an exact same-name compatibility field only alongside mutable updates, and the edit UI renders the identity outside the submitted form. Migrated PostgreSQL proves rejection leaves the base row, organization multipliers, billing quote and audit count unchanged; frontend behavior coverage proves the disabled identity and name-free update payload. A rename now requires explicit create plus separately reviewed reference migration |
| `V1-MIG-SEQ-001` | confirmed bug/regression | High / P1; the first organization creation on a fresh install could fail once with duplicate `orgs.id=1` | Historical migration `000001` inserted the platform organization with an explicit id without advancing `orgs_id_seq` | UniRouter schema/migration | `000036` advances but never rewinds the sequence to at least the current maximum; integration coverage asserts the next generated organization id is beyond all existing ids |
| `V1-UI-MONEY-001` | confirmed bug/regression | High / P1 financial-display accuracy; balances containing micro-CNY could look unchanged | API values were exact strings but default wallet/dashboard formatting rounded them to two decimals | UniRouter frontend | BigInt formatting remains compact for cent-aligned values and automatically renders six decimals when sub-cent evidence exists; browser readback displayed `10000.000002` exactly |
| `V1-REPO-UI-ARTIFACT-001` | confirmed documentation/release-hygiene defect | Medium / P2; developers or packagers could mistake an obsolete PRD1 bundle with cent-based UI for the current release | Three tracked `web/admin` build artifacts survived the move to an independent frontend image, while the frontend technical reference still claimed Go embedding | UniRouter build/docs | Source and Docker search proved the current backend never references or copies `web/admin`; the independent frontend image rebuilds `frontend/dist` into Nginx. The three obsolete files were removed one explicit path at a time and the technical reference now states the real build boundary |
| `V1-TEST-DB-CLEANUP-001` | confirmed bug/regression | Medium / P2 developer/CI hygiene; green integration runs could silently leave isolated PostgreSQL databases | Test cleanup discarded `DROP DATABASE` errors | UniRouter test infrastructure | Three inactive, explicitly named test databases were observed and removed individually. Cleanup now force-drops only its generated exact database name and reports failure to the test; a fresh migrated Admin integration run passed and left zero `uniroute_it_*` databases |
| `V1-TARGET-LEDGER-001` | target environment configuration | Critical / P0 rollout gate; mixed old/new writers would corrupt unit authority | `000036` deliberately renames canonical columns and Redis/outbox schemas rather than supporting mixed units | UniRouter deployment owner / Atlas execution | Non-rolling drain, database backup/checksums, migration preflight, exact image verification, Redis v1 workflow drain and post-up financial smoke are required before traffic reopens; not yet executed |
| `V1-XI-LEDGER-001` | X-Insight coordination | High / P1 before the next snapshot transfer; a consumer expecting `cost_cents` would misread or reject v2 data | X-Insight snapshot schema now carries `cost_micros`, `currency`, `amount_scale` and an explicit version | X-Insight consumer owner with UniRouter evidence owner | UniRouter generator and contract are updated locally; X-Insight must acknowledge and test v2 before a new cross-system exchange. No external acceptance is claimed here |
| `V1-CENT-INPUT-001` | misunderstanding or no modification | No defect; retain cent-granularity operator inputs | Wallet top-up/adjustment, grants, project/key ceilings and low-balance thresholds are policy inputs, not computed terminal money | UniRouter product/billing | Keep visibly named `*_cents` request fields and convert with checked multiplication at the boundary. They are not silently reinterpreted as micros |

## Implemented contract

1. `internal/money` owns exact decimal parsing, checked signed addition and
   subtraction, cents-to-micros conversion, exact effective-price conversion,
   conservative reservation ceiling and largest-remainder component
   conservation.
2. A terminal amount is calculated without `float32`/`float64` money
   arithmetic. Exact component numerators are summed and rounded once to the
   nearest micro-CNY, half up. Zero usage costs zero; one token at
   `2 CNY / 1M` costs `2` micro-CNY.
3. Wallets, transactions, grants, call logs, reservations, grant allocations,
   finalization journals, materialized project spend, terminal hooks, provider
   attempts and reservation extensions use explicit `*_micros` `BIGINT`
   authority after migration `000036`.
4. Immutable effective reservation prices use
   `*_price_micros_per_million`. Public configuration remains CNY per million
   tokens. Every supported API mutation is transactionally serialized by model
   and rejected before audit/commit unless the base, cache and every current
   organization-effective price fit signed `int64` using the runtime's exact
   half-up rule.
5. API money outputs are decimal strings with `currency: "CNY"` and
   `amount_scale: 1000000`. The frontend uses `BigInt`; computed costs render
   six decimals and default balance formatting reveals sub-cent evidence while
   keeping cent-aligned values at two decimals.
6. Billing/call-log outbox payloads and runtime terminal hooks use v2 schema
   identifiers. Pending v1 outbox values are decoded explicitly as cents and
   multiplied by `10,000`; they are never reinterpreted as micros.
7. Agent-capability runtime money moves to the isolated `ur:agentcap:v2`
   namespace and signed micro-CNY counters. Old v1 keys expire naturally; no
   bulk Redis deletion is part of the rollout.
8. `000036` refuses active reservations, unresolved provider attempts,
   incomplete terminal hooks, invalid price authority or any conversion that
   could overflow. Its down path is lossless-only and refuses after new
   micro-ledger evidence is written.

## Isolated migration, API and browser evidence

The local check used a newly created database
`uniroute_v1_browser_20260714`, Redis logical DB 15, backend port `18086` and
frontend port `3006`. Existing development processes on ports 8080/3000 were
left untouched.

- A fresh migration applied `000001` through `000036`; `ledger.version`
  read back as `"micro-cny.v1"`, and all four wallet authority columns read
  back as `BIGINT` `*_micros` columns.
- The product seed created a cash balance of `10000000000` micros and a grant
  balance of `10000000000` micros. Seeded request costs read back as `0`,
  `20000` and `40000` micros.
- Authenticated API responses returned `currency=CNY`,
  `amount_scale=1000000` and decimal-string wallet/log amounts. Usage summary
  returned `cost_micros="60000"` and estimated ratio `1/3`, rather than a
  permanently zero estimate.
- A controlled `+2` micro fixture was displayed by the real wallet page as
  `¥10000.000002`; managed and available totals preserved the same unit.
- Dashboard and request logs displayed `¥0.000000`, `¥0.020000`,
  `¥0.040000` and total `¥0.060000` consistently.
- Usage Analysis and Reconciliation both displayed estimated share `33.3%`;
  the latter showed the expected estimated/upstream token split and deviation.
- The real Model Pricing form created and read back
  `0.00001` / `1234567890123.12345` CNY per million tokens and
  `0.123456` / `99.999999` cache multipliers without JavaScript number
  coercion. That one explicit local test row was then deleted through the UI.
- A fresh migrated browser/API/database check opened the edit drawer for
  `v1-immutable-browser`, rendered the model identity as disabled with the
  explicit migration guidance, and preserved `1.25` / `2.5` prices plus
  `0.1` / `1.25` cache ratios. Updating the input price to `1.5` succeeded
  without changing the identity. A direct PUT that combined a different
  `model_name` with price `999` returned `400`; PostgreSQL retained the prior
  price row and organization multiplier, created no renamed row and recorded
  no audit event for the rejected request.
- Migrated pricing-authority integration sent overflow combinations through
  each base-price and organization-multiplier create/update endpoint. Every
  request returned `400`, retained its prior row and wrote no audit event. A
  simultaneous base-price/organization-multiplier race produced exactly one
  safe commit and one rejection; the resulting runtime quote remained usable.
- The X-Insight generator exported one explicitly selected request as
  `unirouter.prd5.xinsight.snapshot.v2` with `cost_micros="40000"`,
  `currency=CNY`, `amount_scale=1000000` and
  `money_schema=micro-cny.v1`; raw prompts, responses, API keys and IP values
  remained excluded. This proves the UniRouter producer only, not consumer
  acceptance.
- Final page reloads had no API, network or rendering errors. React Router and
  Semi UI emitted existing development-only deprecation warnings; they did not
  affect these flows.
- Local screenshots are retained in ignored test artifacts:
  `output/playwright/v1-micro-ledger-wallet-local.png` and
  `output/playwright/v1-micro-ledger-reconciliation-local.png`, plus
  `output/playwright/v1-model-price-immutable-local.png` for the immutable
  model-price identity.

## Local regression gates

The final branch state passed:

```text
git diff --check
go vet ./...
REQUIRE_TEST_DATABASE=1 go test -race -coverprofile=/tmp/unirouter-v1-coverage.out ./...
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/migrate -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/billing ./internal/migrate -run 'TestV1(Pricing(Authority|UnitContract)|MicroPrecisionLedger)' -count=1
npm run type-check
npm test -- --run
npm exec -- vite build
```

The known macOS linker `LC_DYSYMTAB` warnings and Vite chunk-size warning are
non-failing toolchain/build observations. Docker is left to the required
GitHub `Docker Build` job because a local Docker daemon is not available.

## R3 target activation checklist and stop gate

Before an authorized target activation:

1. record the exact application image/commit and verify `000035` precedes
   `000036`;
2. stop and drain every old stable/canary API, worker and terminal-recovery
   writer;
3. back up PostgreSQL and record monetary row counts/checksums by affected
   table;
4. prove zero active `frozen`/`settlement_pending` reservations, unresolved
   provider attempts and incomplete terminal hooks;
5. review every routed target model price for source, currency and effective
   date; no automatic repricing is authorized;
6. prove there is no active `ur:agentcap:v1:*:workflow` authority before
   switching to v2; do not bulk-delete Redis keys;
7. obtain X-Insight v2 snapshot-consumer acknowledgement or pause the next
   snapshot exchange;
8. apply the migration once, verify converted sums and constraints, then start
   only the exact new image;
9. prove zero-use, two-micro use, cash/grant settlement, outbox v2 and Redis v2
   readback before reopening traffic; and
10. treat post-activation rollback as roll-forward after any micro evidence is
    written.

At the time this local evidence was recorded, creating the Draft PR and running
required CI were authorized while merge and target activation remained an
explicit User decision. That later authorization and its completed execution
are recorded in the target evidence linked at the top; this historical local
checklist is not the current deployment status.
