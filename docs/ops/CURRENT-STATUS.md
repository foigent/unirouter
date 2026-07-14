# UniRouter Current Status

> Purpose: single current-state entrypoint for humans, executors, and reviewers.
> The User-approved Post-PRD6 V1 delivery gate for commercial License
> enforcement and signed offline upgrades is locally verified on
> `codex/v1-commercial-license-upgrade`, based on clean `origin/main`
> `e94a4b184a6f675d76c292602b7c18293ab2ce61`. It adds fail-closed signed
> deployment/feature/resource authority, migration `000037`, vendor License
> tooling, deterministic pre-extraction signed-bundle verification, installed
> release-state readback and a no-build/source-free customer Compose profile.
> PR #205 is open. A follow-up P0 review found that customer Compose could
> replace both public trust roots; exact code-bearing head
> `cd05055afe25cedff77830230d2788d8f06c96b3` moves them into the formal
> release binary, rejects runtime overrides and adds a double-click macOS
> License Studio for separate
> key generation, customer-form signing and verification. Headed desktop/mobile
> browser checks have zero console errors and only loopback requests. Required
> CI run `29359673125` passed Change Routing, Backend Tests, Frontend Build and
> Docker Build, including the commercial-License/signed-upgrade integration
> gate and a release image built with separate embedded public rings. No
> production/customer signing key or License was created, and no ECS embedded
> trust root, deployment ID, License, migration or image was changed. Merge,
> production key ceremony and target activation remain separate decisions.
> Evidence: `docs/ops/evidence/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-LOCAL-20260715.md`,
> `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-LOCAL-20260715.md` and
> `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-CI-20260715.md`.
> PRD6 is formally `Completed (Accepted Scope)` at repository/CI evidence level. All 27
> accepted rows are verified or closed. The final row, P6-F11, passed PR #189
> implementation run `29245696926`, final-head run `29246431182`, squash merge
> `4c78a5b7`, and post-merge main run `29246975012`; Backend Tests, Frontend
> Build and Docker Build are green. A separately authorized 2026-07-13 ECS
> update deployed exact PRD6 backend commit `ce9307a` to stable/canary and the
> final PR #192 frontend correction from `f4fe41d`; authenticated UI, zero
> missing-favicon/browser-console errors and one selected historical request
> detail are Target verified. This is not Production,
> capacity/HA, real-provider/detector, TLS or migration-down evidence.
> A fresh 2026-07-14 completion audit verified clean/aligned
> `main`/`origin/main` at `9c73d6f`, successful exact-head main run
> `29274010760`, and current public ECS readiness with DB/Redis healthy. See
> `docs/ops/evidence/PRD6-FINAL-COMPLETION-20260714.md`.
> Post-PRD6 V1 release hardening is tracked separately and does not reopen that
> completion. CI-efficiency PR #198 passed exact-head run `29299202652`, merged
> as `31d87de`, produced no automatic post-merge CI, and passed one trusted main
> dispatch `29299765793`. Shared Go cache `5720112747` is seeded; final capacity
> is 32 caches / `9,503,335,891` bytes after four explicit single-ID deletions.
> User-approved micro-CNY option B passed PR #202 exact-head required run
> `29334221779` and squash-merged as
> `ec0dde201841b8286f6802cc7e588e79eda40719`. The authorized non-rolling ECS
> activation backed up PostgreSQL, proved zero active financial state, applied
> `000035` then `000036`, and switched stable/canary/frontend to exact V1
> images. Checked micro-CNY authority, immutable model identity, exact
> effective-price range validation, migrated sequence repair, BigInt UI money,
> outbox/Redis v2 and cleanup regressions are CI verified; the selected target
> schema, API and UI surfaces are Target verified.
> Deployment-label PR #203 passed exact-head required run `29339373907`, merged
> as `545e514ab51937e6589512192141b9359cfcb504`, and is the final ECS release.
> Stable/canary report exact commit `545e514`, version `v1`, environment
> `development`, healthy DB/Redis and migration `000036`; authenticated browser
> checks display `开发环境` with zero console errors/warnings. Evidence:
> `docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-TARGET-20260714.md`.
> This development/test target is not Production, real-provider/detector,
> TLS, capacity/HA or migration-down evidence. X-Insight v2 consumer acceptance
> and the broader Post-PRD6 product/UI issue queue remain separate.
> `V1-BUILD-CONTEXT-001` is CI verified / closed by PR #195:
> backend and independent frontend Docker contexts exclude local environment,
> private-key and generated files; the backend performs a post-copy invariant
> check and required CI uses root, nested and private-key regression markers
> without repeating the expensive backend build. Exact head `8a082cf` passed
> run `29333175230`, including the real Docker assertions, and squash-merged as
> `65a0d07`. PR #195 itself did not deploy an image; its protections are now
> included in the later exact V1 target image documented above.
> PRD6 Batch 0B P6-F16 was squash-merged by PR #140 as
> `f244f669c529fec8767f85fff209882b333d8ed2`. P6-F33 was squash-merged by
> PR #142 as `7173c5967a42c112fd0b219938a0901c1a0b9633`; its PR and post-merge
> `main` required CI are green. D6-08 is accepted with option B. P6-F37 audit-log
> tenant isolation and P6-F38 frontend-test required-CI enforcement were
> squash-merged by PR #143 as `a1e0ad61ed44f02f32317131a1920a6181e43645`;
> post-merge main CI run `29133690338` passed all three jobs.
> P6-F32 finance/reconciliation and adjacent P6-F39 usage-derived tenant
> isolation were squash-merged by PR #145 as
> `609385f663cee883ad5965facb25ce4185d8b89a`; post-merge main CI run
> `29136296782` passed all three jobs. P6-F34 pricing authority was
> squash-merged by PR #146 as
> `517ce45381429781f7e81e492bdd9f38093c21ba`; final-head required CI run
> `29137670055` and post-merge main run `29137781625` passed all three jobs,
> and its historical branch is retained. P6-F35 global-control authority PR
> #147 final head `fb77083d0f04f5cdaa20f38e763c93b51b89f4d1` passed required
> CI run `29139408226`, was squash-merged as
> `c08db96167837ade685708c2ab204861a8ecc0ca`, and post-merge main run
> `29139534691` passed all three jobs; its historical branch is restored and
> retained. P6-F36 PR #148 final evidence head passed required run
> `29140739477`, was squash-merged as
> `d83dbbc0ac7137bb44d2346adc5a25debde804e1`, and post-merge main run
> `29140850345` passed. P6-F17 Scheme A operation/idempotency identity PR #149
> passed required run `29152968280`, was squash-merged as
> `c9402fad323e8656493ff1bb29367a5dcf118331`, and post-merge main run
> `29153106336` passed all required jobs.
> P6-F18A billing reservation invariants passed PR #151 required run
> `29154664491`, were squash-merged as
> `7db1efc9cf7c0c78fd6348446ed8701ecf86e8ee`, and post-merge main run
> `29154820102` passed Backend Tests, Frontend Build and Docker Build. P6-F18B
> stream settlement recovery passed PR #153 required run `29155948787`, was
> squash-merged as `b30bc9d1cb237703cc9479c3161c9cfa2c28f3f6`, and post-merge main run
> `29156096777` passed all three jobs. P6-F18 is CI verified / closed; P6-F20
> remains separate.
> P6-TEST-DB-001 passed PR #155 required run `29157383889`, was squash-merged
> as `4b18ace92df49ad74048acc7197992990f14ca8e`, and post-merge main run
> `29157561036` passed all three jobs, including the isolated tagged admin
> integration gate. P6-F03 passed PR #156 run `29158184240`, was squash-merged
> as `f6f075aa4333f2b81f888f3505c2c2b44e77c449`, and post-merge main run
> `29158395734` passed all three jobs. P6-F04 passed PR #157 run `29158981445`,
> was squash-merged as `36f736c0ca02f483b408c94350c8e5d1497b759e`, and
> post-merge main run `29159155254` passed all three jobs. P6-F05 typed
> routing and project-budget-alert configuration truth passed PR #158 required
> run `29160329032`, was squash-merged as `ca02ecb39abc24a6ce25d8c4926d72b11b612590`,
> and post-merge main run `29160494169` passed all three jobs. P6-F05 is CI
> verified / closed.
> P6-F06 multiplier-aware reservation passed PR #160 required run
> `29161370623`, was squash-merged as
> `15ea8a27a0a514189c5af6f676af1e987f5c6763`, and post-merge main run
> `29161527988` passed all three jobs. P6-F06 is CI verified / closed.
> P6-STREAM-002A typed stream transport passed PR #162 exact-head run
> `29168427875`, was squash-merged as
> `7737f8b2fc5a896e690d5316aa06016d71ee2e18`, and post-merge main run
> `29168586124` passed all required jobs. Its historical branch is restored.
> It is a CI-verified prerequisite slice, not P6-F11 completion.
> P6-F19A Agent capability kernel passed PR #164 exact-head required run
> `29170347267`, was squash-merged as
> `43b90f85b0d4c655bc833a42829a8b458c581d35`, and post-merge main run
> `29170507588` passed Backend Tests, Frontend Build and Docker Build. Its
> historical branch is restored. The slice implements explicit
> supervisor exchange, parent delegation, five-minute token, refresh
> convergence, atomic topology cap, shared workflow revocation, 15-second
> bounded root-authority refresh and signed evidence identity.
> P6-F19B Agent ancestor budget passed PR #166 exact-head run `29171653038`,
> was squash-merged as `b491817072384aa29b192e7dd121075d3ae9f867`,
> and post-merge main run `29171807693` passed Backend Tests, Frontend Build
> and Docker Build. Its historical branch is restored. Atomic per-ancestor
> cost/token reservation and actual settlement, exact rolling RPM/RPS,
> Redis-time shared concurrency, typed fail-closed denial and real Redis 7 plus
> migrated-PostgreSQL readback are verified. Together with P6-F19A, P6-F19 is
> CI verified / closed; P6-F20 and P6-F21 remain separate rows.
> P6-F07 distributed limit policy passed PR #168 exact-head run `29173848811`,
> was squash-merged as `2610e31790d7964d5c9e491cb1a11552f1ac22ea`,
> and post-merge main run `29174001859` passed Backend Tests, Frontend Build
> and Docker Build. Its historical branch is restored at exact head `f52a151`.
> Exact shared API-key RPM/RPS, versioned selector RPM/RPS/TPM/concurrency,
> shadow/enforce modes, bounded last-known-safe failure behavior, sanitized
> policy evidence, tenant-scoped optimistic control UI/API, real Redis 7 plus
> migrated PostgreSQL and guarded down→up migration are CI verified / closed.
> P6-F21 weighted workload admission passed PR #170 exact-head required run
> `29176160987`, was squash-merged as
> `04af91f2d5c829796c394d8f4509678cc674f642`, and post-merge main run
> `29176358459` passed Backend Tests, Frontend Build and Docker Build. Its
> historical branch is restored at exact head `a9d8f3e`. Server-derived 1/4/1
> classes, Redis-time global/org leases, short reserve, bounded fair/per-org
> queues, crash recovery, bounded LKS, mixed-config fail-closed, typed 429/503
> and UI/OpenAPI evidence are CI verified / closed. P6-F20 durable terminal
> recovery and D6-10 dynamic long-stream extension remain separate. Test-only
> PR #180 removed the unreliable miniredis replica/load claim; PR #181 isolated
> emulator execution budgets without changing runtime behavior. PR #181 passed
> run `29199377213`, merged as `815fb31`, and final main run `29199635598`
> passed Backend Tests, Frontend Build and Docker Build.
> P6-F08 Routing Policy V1 passed PR #172 exact-head run `29178419505`, was
> squash-merged as `54d88335126a5e9829cf28c720ed98ad6258fb52`, and
> post-merge main run `29178588075` passed Backend Tests, Frontend Build and
> Docker Build. The historical branch is restored at exact head `0e374d3`.
> It adds deterministic equal-priority weighting from the server operation ID,
> one-second singleflight route snapshots, bounded LKG then fail-closed,
> renewable cross-replica PostgreSQL probe ownership, routing/circuit/key
> epoch CAS, effective per-channel breaker thresholds, Chat/Embeddings parity,
> safe preview/UI/trace evidence and guarded migration `000025`. No deployment,
> target-capacity or adaptive-routing claim is made. Evidence:
> `docs/ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md`.
> The implementation keeps outer `effective-config.v1` stable and advances its embedded
> routing startup/snapshot contract additively to `routing-config.v2`.
> No target or production verification is claimed.
> P6-F09 provider Prompt Cache accounting is CI verified / closed.
> Implementation branch `codex/prd6-prompt-cache-billing` is retained at exact
> head `711e7182c4875ed8324f6e379f73107ebcb30ffb`, which passed PR #174 run
> `29180539621` before squash merge `6515b3f`. Authoritative OpenAI,
> DeepSeek and Anthropic usage fields now drive an exact ordinary/read/create
> partition; immutable `pv2_` pricing freezes per-model read/create ratios and
> organization multiplier; settlement/recovery, request logs, reconciliation
> and operator UI carry the same evidence; migration `000026` is guarded after
> activation. The first post-merge main run `29180730830` failed only the
> pre-existing P6-F21 miniredis fixture. Production-neutral stability PR #175
> passed run `29180946857`, merged as `257a9eb`, and final post-fix main run
> `29181137210` passed Backend Tests, Frontend Build and Docker Build, including
> the two explicit P6-F09 PostgreSQL gates. This is provider Prompt
> Cache billing only: no response Cache Service, deployment, target,
> production or real/paid-provider claim is made. Evidence:
> `docs/ops/evidence/PRD6-B3-P6F09-prompt-cache-billing-local-20260712.md`.
> D6-01, D6-02, D6-08 and D6-09 were accepted by the User on 2026-07-11.
> D6-10 long-stream hard-budget Scheme A and D6-11 explicit five-minute Agent
> capability lifecycle were accepted on 2026-07-12; advanced innovation rows
> P6-F23 through P6-F31 remain proposed.
> D6-12 was accepted on 2026-07-12: P6-F20 closes at final-usage/terminal
> durability, while P6-F11 owns mid-stream process-crash checkpoints, explicit
> interruption and client recovery. P6-F20 passed PR #177 exact-head required
> run `29193900584`, was squash-merged as `30d1e8f00cfcd45879f9650a3a98f178e844c945`,
> and post-merge main run `29194150134` passed Backend Tests, Frontend Build and
> Docker Build, including both explicit P6-F20 gates. P6-F20 and Batch 3 are CI
> verified / closed; no managed HA/IaC, target or production claim is made.

## PRD6 Completed Window

- Baseline evidence: `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md`.
- Scalability/anti-pattern evidence:
  `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md`.
- P6-F16 implementation and CI evidence:
  `docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md`.
- Follow-up administrator tenant-surface audit:
  `docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md`.
- P6-F33 notification tenant-isolation local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md`.
- P6-F33 full local verification passed backend race tests, vet, frontend
  type-check, 25 files / 126 tests, frontend build and `git diff --check`.
- PR #142 passed `Backend Tests`, `Frontend Build` and `Docker Build`, was
  squash-merged as `7173c59`, and post-merge main CI run `29132520222` passed.
- P6-F37 audit-log tenant-isolation local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md`.
- P6-F38 frontend-test required-CI local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md`.
- P6-F32/P6-F39 finance and usage tenant-isolation local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md`.
- P6-F34 pricing-authority local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F34-pricing-authority-local-20260711.md`.
- P6-F35 global-control authority local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F35-global-control-authority-local-20260711.md`.
- P6-F36 model-governance tenant-isolation local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F36-model-governance-tenant-isolation-local-20260711.md`.
- P6-F17 operation/idempotency identity local and CI evidence:
  `docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md`.
- P6-F18A billing reservation invariant local and CI evidence:
  `docs/ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md`.
  PR #151 and post-merge main required CI passed.
- P6-F18B stream settlement recovery local and CI evidence:
  `docs/ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md`.
  PR #153 and post-merge main required CI passed; this closes P6-F18 but not the
  P6-F20 durable outbox or P6-F11 mid-stream checkpoint/recovery semantics.
- P6-TEST-DB-001 isolated admin integration evidence:
  `docs/ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md`.
  PR #155 and post-merge main required CI passed; the engineering-integrity
  issue is CI verified / closed.
- P6-F03 retry classification local evidence:
  `docs/ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md`.
  PR #156 and post-merge main required CI passed; P6-F03 is CI verified /
  closed.
- P6-F04 Embeddings failover local evidence:
  `docs/ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md`.
  PR #157 and post-merge main required CI passed; P6-F04 is CI verified /
  closed.
- P6-F05 typed effective configuration and budget-alert evidence:
  `docs/ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md`.
  `effective-config.v1` contains the driving `routing-config.v1` plus the active
  project-scoped budget-alert contract. Safe trace and runtime API/UI expose
  effective values/sources; routing transitions and a two-tenant budget test
  pass against real PostgreSQL. The broken global first-wallet path is removed
  and its historical keys are explicitly retired. PR #158 and post-merge main
  required CI passed; P6-F05 is CI verified / closed.
- P6-F06 multiplier-aware freeze evidence:
  `docs/ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md`.
  P6-F18A already supplies the effective-price snapshot; the new dedicated
  PostgreSQL matrix proves atomic insufficient rejection, exact-funds 32-way
  settlement with `uncovered_cost=0`, and idempotent full cancellation refund.
  The focused race matrix passed ten repetitions and required Backend has a
  fail-closed P6-F06 step. PR #160 and post-merge main required CI passed;
  P6-F06 is CI verified / closed.
- P6-STREAM-002A typed stream transport evidence:
  `docs/ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md`.
  Local tests prove bounded 64-KiB-plus and 1-MiB frames, configured oversize
  rejection, serialized write/flush, ResponseController deadline passthrough,
  exact body close, partial-output settlement and non-success terminals. Runtime
  Status/UI expose `stream-transport.v1`. PR #162 exact-head and post-merge main
  Backend/Frontend/Docker CI passed. D6-10 dynamic reservation extension and
  P6-F11 semantic interception remain.
- P6-F19 Agent capability and ancestor-budget evidence:
  `docs/ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md`
  and
  `docs/ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md`.
  PR #164/main run `29170507588` verify the capability lifecycle, topology and
  revocation kernel; PR #166/main run `29171807693` verify shared runtime
  cost/token/rate/concurrency enforcement. P6-F19 is CI verified / closed.
- P6-F07 distributed limit-policy evidence:
  `docs/ops/evidence/PRD6-B1-P6F07-distributed-limit-policy-local-20260712.md`.
  PR #168 exact-head run `29173848811`, merge `2610e317` and post-merge main
  run `29174001859` passed all required jobs, including real Redis/PostgreSQL
  multi-instance policy and migration rollback gates. P6-F07 is CI verified /
  closed without claiming P6-F20, P6-F21, D6-10 or target capacity.
- P6-F21 weighted workload-admission evidence:
  `docs/ops/evidence/PRD6-B1-P6F21-weighted-workload-admission-local-20260712.md`.
  Full local race, two/three-replica real Redis, lease/fault/fairness/config,
  UI and OpenAPI verification pass. PR #170 exact-head run `29176160987`,
  merge `04af91f` and post-merge main run `29176358459` passed all required
  jobs; the row and Batch 1 are CI verified / closed. No deployment or
  target-capacity claim is made.
- P6-F08 Routing Policy V1 repository and CI evidence:
  `docs/ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md`.
  Unit/race plus real PostgreSQL fixtures prove deterministic static weight,
  bounded snapshot failure behavior, one renewable cross-replica probe,
  routing/circuit/key stale-result rejection, per-channel thresholds, safe
  candidate/exclusion preview and guarded migration rollback. PR #172 exact-head
  run `29178419505`, merge `54d8833` and post-merge main run `29178588075`
  passed all required jobs; the row and Batch 2 are CI verified / closed.
- P6-F09 provider Prompt Cache billing repository evidence:
  `docs/ops/evidence/PRD6-B3-P6F09-prompt-cache-billing-local-20260712.md`.
  Provider-contract fixtures, exact price freeze/settlement/recovery, migrated
  PostgreSQL readback, guarded down/roll-forward, tenant-scoped logs and
  reconciliation, UI semantics, full backend race/vet and frontend
  tests/type-check/build pass. PR #174 exact-head run `29180539621`, PR #175
  stabilization run `29180946857` and final post-fix main run `29181137210`
  passed all required jobs; this row is CI verified / closed.
- P6-F20 durable internal outbox CI closure evidence:
  `docs/ops/evidence/PRD6-B3-P6F20-durable-outbox-local-20260712.md`.
  Focused race and isolated migration tests pass locally; PR #177 exact-head
  run `29193900584`, merge `30d1e8f` and post-merge main run `29194150134`
  passed all required jobs. D6-12 remains the accepted mid-stream boundary.
- P6-F40A cost-shortfall implementation evidence:
  `docs/ops/evidence/PRD6-B3H-P6F40A-cost-shortfall-local-20260713.md`.
  The pre-fix migrated-PostgreSQL reproducer terminally charged only 100 of a
  known 500-cent cost. The current branch atomically consumes 400 cents of
  available cash and records zero terminal uncovered cost; insufficient cash
  remains non-terminal/non-cancellable and is excluded from automatic stream
  recovery. Focused billing, 32-way concurrency, gateway no-cancel, guarded
  migration, full backend/frontend, race, vet and contract checks pass locally.
  PR #183 exact implementation/test head `d1afd97` passed required run
  `29207889734`, including both explicit P6-F40 gates. That Slice A evidence
  does not claim the later Slice B or Slice C behavior.
- P6-F40B provider-attempt/retry-cancel authority evidence:
  `docs/ops/evidence/PRD6-B3H-P6F40B-provider-attempt-ledger-local-20260713.md`.
  Migration `000029` adds bounded metadata-only attempt evidence before Chat
  and Embeddings dispatch. Durable HTTP rejection may authorize existing
  bounded failover; transport ambiguity, accepted-but-unsettled responses and
  unusable 2xx bodies retain provider liability and cannot ordinarily cancel.
  Settlement/funding-pending cost is linked atomically to the attempt, and the
  `policybilling -> capbilling -> billing` production chain preserves the
  attempt envelope. Focused race, migration, full backend/vet/contracts and
  frontend 29-file/144-test/build gates pass locally. PR #184 exact
  implementation/test head `ac2bfb5` passed required run `29209644118`:
  Backend Tests, Frontend Build, Docker Build and both explicit F40B gates are
  green.
- P6-F40C affected-key/operator-reconciliation local evidence:
  `docs/ops/evidence/PRD6-B3H-P6F40C-provider-key-reconciliation-local-20260713.md`.
  Explicit ambiguous/unusable/funding-pending states block new paid Chat and
  Embeddings work for the root key and Agent descendants; ordinary in-flight
  work does not serialize the key. The platform-only API accepts no amount,
  and collect requires stored authoritative upstream usage, immutable customer
  price and a billable-output marker. Undelivered/estimated collect is rejected;
  refund/subsidy release only the existing hold; cross-org, 32-way concurrency,
  collect crash-gap and guarded `000030` proofs pass. Full backend/vet,
  frontend 29-file/144-test/build and focused race/migration race pass locally.
  PR #186 exact implementation/test head `49d740d` passed required run
  `29224762127`: Backend Tests, Frontend Build and Docker Build are green,
  including both explicit P6-F40C gates and strict successor rollback order.
  P6-F40 is CI verified / closed.
- P6-F41 typed usage/delivery repository and CI evidence:
  `docs/ops/evidence/PRD6-B3H-P6F41-usage-delivery-integrity-local-20260713.md`.
  `unirouter.usage.v1` distinguishes provider zero, missing usage and versioned
  estimates across adapters, billing, logs, recovery and idempotency. Non-stream
  customer collection now requires a completed local write; interrupted streams
  charge only the independently counted delivered subset, while provider usage
  remains separate evidence. Append-only `000032` receipts retain bounded
  started/partial/completed/interrupted metadata without payloads. Full Go
  tests/vet, focused PostgreSQL/race and strict migration gates, frontend 29
  files / 144 tests, type-check and build pass locally. PR #187 exact
  implementation/test head `6d0e6ba` passed required run `29229320432`:
  Backend Tests, Frontend Build and Docker Build are green, including both
  explicit P6-F41 package/migration gates. P6-F41 is CI verified / closed, not
  target verified or production verified.
- P6-F11 stream policy and termination evidence:
  `docs/ops/evidence/PRD6-B5-P6F11-stream-policy-termination-local-20260713.md`.
  PR #189 exact implementation/test head `8ed6a83` passed run `29245696926`,
  final evidence head `3d4c61b` passed run `29246431182`, and squash merge
  `4c78a5b7` passed post-merge main run `29246975012`, including the explicit
  stream contract and `000034` rollback gates. P6-F11 is CI verified / closed;
  no real detector/provider, target, production, zero-leakage or transparent
  stream-migration claim is made.
- PRD6 closeout evidence:
  `docs/ops/evidence/PRD6-ACCEPTED-SCOPE-CLOSEOUT-20260713.md`.
  Closeout publication PR #190 exact initial documentation head `87c462a`
  passed required run `29248279987` (Backend Tests, Frontend Build and Docker
  Build); any later evidence-only head remains subject to exact-head required
  CI before merge and does not change product scope.
- Accepted customer/provider ledger boundary and future supplier coverage
  reference:
  `docs/planning/PRD6-UPSTREAM-COST-RECONCILIATION-AND-PROVIDER-COVERAGE-20260713.md`.
  D6-14 forbids aggregate provider bills, balance deltas or free-form operator
  amounts from creating customer debt. It records provider-native runtime and
  conformance packs, live billing connectors, controlled bill import,
  generalized units, async media and managed billing as future candidates only;
  no implementation or production claim is made.
- HA/stream-failover competitor and architecture reference:
  `docs/planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md`.
- Source-backed product/architecture/UX audit:
  `docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md`.
- PRD7 candidate-only split, including future D7-ADAPTER-001/D7-COST-001/
  D7-COST-002/D7-METER-001/D7-MEDIA-001 and decision-gated D7-BILLING-001
  (not Accepted Scope):
  `docs/planning/PRD7-CANDIDATE-BACKLOG.md`.
- Accepted future Cache Service target architecture (implementation still
  deferred): `docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md`.
- Adaptive-trust/innovation audit:
  `docs/planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md`.
- Adaptive-trust structural options and signed split-plane recommendation:
  `docs/planning/prd6-adaptive-trust-hardening/hardening.md`.
- Scope entry: `docs/planning/PRD6-START-HERE.md`.
- Accepted-scope PRD: `docs/PRD-Phase6.md`; accepted/proposed state is authoritative
  in `docs/ops/PRD6-ACCEPTANCE-MATRIX.md`.
- Accepted feature accounting: all 27 accepted rows are repository/CI verified
  or closed. P6-F11 was the final row and closed through PR #189 / merge
  `4c78a5b7` / post-merge main run `29246975012`.
  `P6-TEST-DB-001` is a separate engineering-integrity issue outside that
  27-row feature count; it is CI verified / closed by PR #155 and post-merge
  main run `29157561036`.
- Fresh pre-change verification passed backend tests/vet, frontend type-check,
  25 files / 125 tests, frontend build, and X-Insight full Go tests.
- Focused read-only audit verification passed auth/middleware/admin/billing and
  gateway/middleware/billing/circuitbreaker packages, including the applicable
  gateway/middleware/circuitbreaker race suites. This is source/local evidence,
  not a multi-instance or 100x result.
- PR #136, #137 and #138 are merged with all three required checks successful.
- PR #139 was squash-merged as `62f161e2866a3a4c699abb9a647feef48bded1b2`;
  `Backend Tests`, `Frontend Build`, and `Docker Build` passed for its final
  exact head. It was not deployed.
- PR #140 final head `fd1ab56` passed `Backend Tests`, `Frontend Build`, and
  `Docker Build`; Backend logs prove the migrated twelve-case matrix ran and did
  not skip. It was squash-merged as `f244f669`; post-merge `main` run
  `29129070253` passed the same three jobs.
- At this initial audit/Batch 0B checkpoint, no deployment, target
  probe/mutation, real or paid request, credential change, data-scope expansion,
  exporter/outbox or continuous ingestion had occurred. The later separately
  authorized target run is recorded at the top of this file and in its dedicated
  evidence.

The second PRD6 audit confirmed source-level P0 candidates in tenant isolation,
request/financial idempotency identity, Freeze pool re-entry, Settle/Cancel
terminal-state concurrency, post-delivery settlement handling, and revocation.
It also confirmed P1 synchronous-request-path, process-local admission, route/
breaker, Redis hot-key, streaming, evidence-write and retention blind spots.
The User accepted the updated MVP and policy-specific failure posture. P6-F16
is CI verified by PR #140 with a migrated two-tenant PostgreSQL/JWT matrix; the
Backend log shows all twelve authenticated subtests PASS rather than SKIP. A
later read-only audit found P6-F32 through P6-F36 across finance, notifications,
pricing, global settings and organization model governance. D6-08 separately
accepted those rows with tenant roles confined to their JWT organization and
platform-only global/multiplier authority. P6-F32/P6-F33/P6-F34/P6-F35/P6-F36/
P6-F37/P6-F38/P6-F39 are merged and CI verified. P6-F36 PR #148 final required
run `29140739477`, merge `d83dbbc` and post-merge run `29140850345` passed.
P6-F17 is merged and CI verified. No audit row is presented as a
production incident, 100x proof, or implemented remediation before its exact
evidence exists.

D6-10 accepts hard-budget Scheme A for long streams: extend the reservation
idempotently before a guard band; if extension fails, stop upstream, return a
safe terminal error and settle only delivered output. D6-11 accepts explicit
supervisor exchange/registration for child Agents, a five-minute maximum token,
minute-three jittered/singleflight refresh, admitted-work continuity on normal
expiry and all-replica active revocation within 30 seconds. Workflow lifetime,
depth, fan-out and ancestor budgets are separate. UniRouter grants only model-
gateway authority; Tool/IAM/database/export action owners remain separate.
Neither accepted decision is presented as implemented before its exact row
passes repository and CI evidence. P6-F19A and P6-F19B now have local,
exact-head and post-merge CI evidence, so P6-F19 is CI verified / closed.
P6-F07 independently has local, exact-head and post-merge CI evidence for
generic selectors/TPM/modes and is CI verified / closed. P6-F21 also has local,
exact-head and post-merge CI evidence and is CI verified / closed by PR #170
and main run `29176358459`. Durable cross-process terminal coupling is CI
verified / closed by P6-F20 PR #177 and main run `29194150134`; dynamic
long-stream extension and mid-stream process-crash recovery remain D6-10/P6-F11.

P6-F35 local evidence covers the repository boundary only. D6-09 defers its
target activation; a later authorized cutover must drain old control-plane
binaries and activate only the new version non-rolling because mixed old/new
binaries are not safe for these endpoints. Other new-version instances learn a
committed sensitive-word version through the existing approximately one-minute
reload path. No global-settings UI was added. The typed configuration registry
and budget/alert key-scope-audit truth are CI verified / closed by P6-F05; they
are not retroactive P6-F35 claims.
P6-F17 now supplies separate server/client correlation and tenant-scoped Audit
readback for ambiguous responses; generic mutation replay and durable audit do
not follow automatically.

P6-F17 repository/CI verification covers server-owned operation IDs, separate client
correlation, tenant/API-key/payload-bound metadata-only Chat/Embeddings
idempotency, distinct workflow/Agent/attempt/upstream identities, tenant-scoped
financial uniqueness, and Logs/Audit readback. It stores no prompt/answer and
does not itself close P6-F20; P6-F18 is separately closed by the F18A/F18B
delivery. Migration `000021` requires old-writer drain; its safe partial down
is not authorization to restart an old binary.

P6-F18 is CI verified for the wallet/reservation and billable-stream foundation:
price lookup no
longer re-enters a held pool transaction; the effective price/multiplier
version is frozen; grant/cash holds and refunds preserve their source; grant
expiry is atomic; duplicate exact-funds Freeze succeeds from one receipt; and a
100-way mixed Settle/Cancel race creates one terminal ledger event. PR #151
required run `29154664491`, squash merge `7db1efc` and post-merge main run
`29154820102` passed. F18B then adds a durable pre-output
`settlement_pending` marker, immutable final-usage payload, non-cancellable
post-output failure posture and leased/backoff recovery. PR #153 required run
`29155948787`, squash merge `b30bc9d` and post-merge main run `29156096777`
passed. Simulated post-chunk DB loss plus 20 concurrent workers produces one
terminal receipt. This closes P6-F18 at CI evidence level only; rows missing
final usage remain held. The general outbox is independently CI verified /
closed by P6-F20; D6-12 assigns per-chunk/process-crash checkpoints and client
recovery to P6-F11. No target or production verification is claimed.

P6-F36 local evidence also covers only the repository boundary. It does not
reopen P6-F16, add a migration/RLS/foreign-key redesign, change the existing
unregistered-model compatibility rule or `/v1/models`, add a platform selector
UI, or perform target malformed-assignment cleanup.

The User selected D7-UPGRADE-001/002 as the first future PRD7 direction and
accepted the complete exact-response Cache Service as future required
`D7-CACHE-001`, but
deferred every PRD7 implementation and target-evidence execution through PRD6
source closeout. PRD6 is now closed, but those queues remain later work and are
not authorized by closeout. Historical branches remain retained and no
cleanup/deletion is authorized. This ordering does not postpone the existing
2026-07-17 Gate A exact-path file deadline.

The third PRD6 research round confirmed that UniRouter currently owns model
request enforcement, not arbitrary MCP/tool/data/export side effects. X-Insight
currently owns controlled evidence import/readback and may later own cross-event
analysis, but must not become a synchronous UniRouter request dependency. The
proposed post-MVP design separates source facts, X-Insight risk inference,
policy authority, and local enforcement at UniRouter or the actual action owner.
No new runtime contract or P6-F23-P6-F31 implementation is accepted by that
research.

PRD5 remains `Accepted Scope Complete`, and Batch 1Z evidence remains historical
truth for that release. The separately authorized PRD6 target update now runs
stable/canary backend commit `ce9307a`; one selected historical route/failover
detail satisfies P6-E01. Migration down, TLS 443, real provider/SSO,
capacity/HA and later X-Insight contracts remain separate evidence/ownership
matters, not reopened PRD5 source gaps.

The two temporary Gate A JSON snapshots remain present with accepted hashes and
are due for exact-path deletion by 2026-07-17. Receipts, hashes, sanitized
evidence and persisted normalized evidence remain retained.

The accepted PRD5 cross-project contract remains operator-mediated
`xinsight.import-receipt.v1` sidecar reconciliation. PRD6 considers only the
boundary: UniRouter creates authoritative sanitized gateway facts; an external
system may consume an accepted versioned contract asynchronously and is never a
request-path dependency. External product functionality is not PRD6 scope.

## Historical Accepted Phase Record

> PRD4 preflight hardening passed Canary 6/6 and User reported Sentinel/Atlas completed after `e335a9e` / PR #83: price lookup DB/context/query errors no longer fall back to hardcoded billing prices, `errdocs` has direct unit coverage, and graceful shutdown timeout is configurable with a 120-second default.
> PRD4 Batch 1A Auth UX hardening passed Canary 6/6 and User reported Sentinel completed after `620794b` / PR #84: login, change-password, and admin user password fields share hidden-by-default visibility controls with accessible show/hide buttons. Atlas deployment has not been reported for Batch 1A.
> PRD4 Batch 1B Usage And Cost Attribution foundation passed Canary and User reported Sentinel completed after `7dd8aa0` / PR #85: API keys now carry owner/type metadata, gateway call logs snapshot owner/type at request time, usage analytics supports `scope=my`, owner/key-type filters and groupings, and the frontend exposes the new analytics controls. Atlas deployment has not been reported for Batch 1B.
> PRD4 Batch 1C Flow/Event/Trace Foundation passed Canary and User reported Sentinel completed after `4f62e34` / PR #86: it adds metadata-only `request_trace_events`, deterministic trace sampling config, gateway/billing/routing/provider/call-log events for chat and embeddings, `GET /api/v1/logs/{id}/trace` with project usage RBAC, and Logs detail UI rendering. Atlas deployment has not been reported for Batch 1C.
> PRD4 Batch 1D Runtime Observability Foundation passed Canary and User reported Sentinel completed after `349ac75` / PR #87: it adds `GET /api/v1/runtime/status`, runtime metadata config, app memory, DB/Redis health, recent traffic summary, Prometheus/Grafana configuration state, and `/status` Runtime Observability cards while keeping current Internal Status event-derived. Batch 1D did not include host CPU/disk/network, cluster inventory, active Prometheus/Grafana probes, or embedded dashboards; Batch 1J later adds the single-node host summaries. Atlas deployment has not been reported.
> PRD4 Batch 1E Quota Vocabulary And Key Quota Foundation passed Canary and User reported Sentinel completed after `a1acdb6` / PR #88: it standardizes user-facing RPM/RPS/TPM/tokens/s vocabulary, exposes preferred `rps_limit` plus structured key `quota` data while preserving legacy `qps_limit` compatibility, and updates Admin API Keys / Developer Console quota UI. Scheduler V2, key-level TPM enforcement, budget hard-blocking, quota audit reason/idempotency fields, and schema migration are not included. Atlas deployment has not been reported.
> PRD4 Batch 1F PlungeIn Port Architecture passed Canary and Sentinel merge is reflected by `87bfc17` / PR #89: it accepts ADR 0001 for PRD4 scope and adds `docs/tech-ref/22-prd4-plungein-port-contract.md` for Observe, Act, limited Enrich, advisory Decide, and forbidden Mutate boundaries. Runtime plugin loading, manifest, sandbox, marketplace, third-party installation, and auth/billing/routing/wallet/settlement/provider-dispatch mutation are not included. Atlas deployment has not been reported.
> PRD4 Batch 1G External Monitoring Integration passed Canary 9/9 and Sentinel merge is reflected by `bb291a4` / PR #90: it extends `GET /api/v1/runtime/status` from static Prometheus/Grafana configuration display to active Prometheus readiness and `up{job="unirouter"}` scrape checks plus Grafana `/api/health`, latency/reason/scrape/version fields, and frontend Runtime Observability rendering. Embedded dashboards, Grafana datasource validation, new metrics auth, OpenTelemetry export, host telemetry collectors, and generic monitoring connectors are not included. Atlas deployment has not been reported.
> PRD4 Batch 1H Real Upstream Evidence Harness passed Canary 11/11 and Sentinel merge is reflected by `d3acfa6` / PR #91: it adds a guarded preflight/seed/runner path for `P4-F08` real upstream target-environment evidence covering OpenRouter-compatible and direct Anthropic-native non-stream/stream checks. User decision on 2026-07-08: because no approved real upstream environment exists, current PRD4 keeps the harness only and defers real target execution to `PRD5-UPSTREAM-001`.
> PRD4 Batch 1I Real SSO Evidence Harness passed Canary 11/11 and Sentinel merge is reflected by `2c02430` / PR #92: it adds a guarded preflight/browser/audit-check path for `P4-F09` real SSO target-environment evidence covering Logto or another approved enterprise OIDC provider. User decision on 2026-07-08: because no approved real SSO environment exists, current PRD4 keeps the harness only and defers real target execution to `PRD5-SSO-001`.
> PRD4 Batch 1J Runtime Host Telemetry passed Canary 8/8 and Sentinel merge is reflected by `8707346` / PR #94: it replaces fixed CPU/disk/network `telemetry_source_not_configured` placeholders with structured host telemetry in `GET /api/v1/runtime/status` and separate `/status` cards. Linux reports sampled CPU usage and network byte counters when `/proc` is available; other hosts expose structural data with explicit reason fields. Cluster inventory, raw topology, private IP/hostname/cloud identifiers, OpenTelemetry, metric alerts, and Production verified target evidence are not included.
> PRD4 Batch 1K Trace Retention And Cleanup passed Canary 10/10 and Sentinel merge is reflected by `1d9ea50` / PR #95. It adds trace-event retention config and cleanup wiring only; it does not add OpenTelemetry, partitioning, archival, prompt/response plaintext, runtime cache, or Production verified evidence.
> PRD4 Batch 1L Runtime Monitoring URL Redaction passed Canary 8/8 and Sentinel merge is reflected by `4426efd` / PR #96. It redacts Prometheus/Grafana display URLs in runtime status while preserving raw configured URLs for probes; it does not add monitoring auth config, dashboards, OpenTelemetry, cluster inventory, or Production verified evidence.
> PRD4 Batch 1M Trace Metadata Read-Side Redaction passed Canary 8/8 and Sentinel merge is reflected by `2f43fca` / PR #97 for `P4-F12` / `PRD4-REDACTION-002`: it adds shared trace metadata allowlisting for gateway write-side events and admin read-side responses, plus focused regressions proving seeded secrets are absent from the trace API response.
> PRD4 Batch 1N Developer Console Self Usage And Model Cost passed Canary 9/9 and Sentinel merge is reflected by `6194062` / PR #98 for `P4-F02` / `PRD4-USAGE-002`: Developer Console requests `scope=my` usage analytics for the selected project, uses the owner-attributed summary for KPI totals, and displays model/alias usage and cost breakdown.
> PRD4 Batch 1O API Key Input And Security Header Hardening passed Canary 8/8 and Sentinel merge is reflected by `59721d7` / PR #99 for `P4-F12` / `PRD4-SECURITY-001`: API key names are trimmed and rejected when empty, over 255 characters, control-containing, markup-like, or SQL-like; negative `project_id` is rejected on API key create/update; all HTTP responses get baseline security headers; and the root handler returns a standard 404 for unknown GET paths without hardcoded version exposure.
> PRD4 Batch 1P Scope Deferral And Closeout Alignment passed Canary 6/6 and Sentinel merge is reflected by `9e2927f` / PR #100 for `P4-F08` / `P4-F09` / `P4-F13`: real upstream and real SSO target execution are deferred to `PRD5-UPSTREAM-001` and `PRD5-SSO-001`; Batch 1H/1I harnesses remain accepted assets; and PRD5 backlog consolidation is accepted.
> PRD4 Batch 1Q Production Evidence Promotion passed Canary 8/8 and Sentinel commit is reflected by `f0d34e1` for `P4-F11`: `docs/ops/evidence/PRD4-B1Q-production-evidence-promotion-20260708.md` classifies the final target report as target-smoke evidence for selected PRD4 areas, promotes no PRD4 matrix row to `Production verified`, and keeps `PRD4-RUNTIME-002`, `PRD5-UPSTREAM-001`, and `PRD5-SSO-001` as explicit boundaries.
> PRD4 Batch 1R Runtime Build Chain Follow-up passed Canary 9/9 and Sentinel merge is reflected by `819846c` / PR #102 for `PRD4-RUNTIME-002`: `go.mod` pins `toolchain go1.25.12`, the Docker backend build uses `golang:1.25.12-alpine` with version/commit `-ldflags`, `cmd/uniroute` falls back to build metadata when env vars are blank, compose defaults the runtime environment and Grafana service URL correctly, and `uniroute-api` has a healthcheck. Docker CLI was unavailable in the Canary environment, so live container health remains a CI/Atlas check before any Production verified runtime claim.
> PRD4 Batch 1S Open Issue Boundary Closeout passed Canary 7/7 and Sentinel merge is reflected by `5b41b87` / PR #103 for PRD4/PRD5 issue classification: `PRD4-PLUGIN-001` maps to `PRD5-PLUGIN-001` / `PRD5-MARKET-001`, `PRD4-DEPLOY-001` maps to `PRD5-DEPLOY-001`, and `PRD4-INFRA-001` maps to `PRD5-INFRA-001`. User reported Atlas deleted `uniroute_test`, closing `PRD4-DEPLOY-002`. PRD4 is complete for the confirmed current scope; no PRD4 row is promoted to `Production verified` without target evidence.
> PRD5 Batch 1A/1B/1C Scope / Snapshot / Structural Evidence / Trace UI passed Canary 17/17 and Sentinel merge is reflected by `e1e76e3` / PR #106. PRD5 Batch 1D Upstream Request Correlation passed Canary 11/11 and Sentinel merge is reflected by `bf435ca` / PR #107: it captures provider-returned upstream request IDs from a bounded response-header allowlist and writes sanitized values to provider trace events plus call logs. PRD5 Batch 1E Event Capture Gap Emission passed Canary 11/11 and Sentinel merge is reflected by `a19830f` / PR #108: it writes `capture_gap='redacted'` when trace metadata is safely dropped, shape-rejected, or truncated. PRD5 Batch 1F Payload Capture Mode Configuration passed Canary 11/11 and Sentinel merge is reflected by `e9670ad` / PR #109: it adds restricted `PAYLOAD_CAPTURE_MODE` config for `metadata_only` and `disabled` evidence state only. PRD5 Batch 1G Trusted Evidence Context Intake passed Canary 11/11 and Sentinel merge is reflected by `c701843` / PR #110: it adds default-off shared-secret-gated inbound trace/session/actor/delegation evidence context. PRD5 Batch 1H Gateway Span Hierarchy passed Canary 10/10 and Sentinel merge is reflected by `61f5610` / PR #111: it parents later UniRouter-emitted trace events to the request gateway root span and preserves trusted inbound parent spans for the root. PRD5 Batch 1I Payload Policy Decision Evidence passed Canary 11/11 and Sentinel merge is reflected by `1dc20b5` / PR #112: it emits explicit `payload.policy.applied` trace events with sanitized policy metadata and `decision_kind=payload_capture`, while raw payload capture remains off. PRD5 Batch 1J Provider Upstream Capture Gap passed Canary and Sentinel merge is reflected by `f5223df` / PR #113: provider response trace events without sanitized provider-returned upstream request IDs write `capture_gap=provider_not_returned`. PRD5 Batch 1K Gate A Snapshot Handoff Note passed Canary 10/10 and Sentinel merge is reflected by `2db830a` / PR #114. PRD5 Batch 1L Component Span Refinement passed Canary 10/10 and Sentinel merge is reflected by `ffebf83` / PR #115. PRD5 Batch 1M Provider Failure Capture Gaps passed Canary 10/10 and Sentinel merge is reflected by `4a4e254` / PR #116. PRD5 Batch 1N Snapshot Capture Gap Candidates passed Canary 10/10 and Sentinel merge is reflected by `92cb8ce` / PR #117. PRD5 Batch 1O Gate A Controlled Snapshot Handoff Prep passed Canary 8/8 and Sentinel merge is reflected by `b2eea2e` / PR #118.
> PRD5 Batch 1T Gate A Exchange Envelope And Controlled Run Authorization passed Canary 7/7 and Sentinel merge is reflected by `fc62e65` / PR #123. PRD5 Batch 1U Embeddings Model Permission Parity passed Canary 7/7 and Sentinel merge is reflected by `b2ba892` / PR #124. Atlas executed authorization `UR-XI-GATE-A-20260710-01`; the immutable snapshot remains diagnostic evidence for the original gap. Codex executed `UR-XI-GATE-A-20260710-02` against target `e823abc293f996264763c6c1aca19d3d4d6e705f`, closing Batch 1W target verification for the denied path and producing a second externally persisted/read-back X-Insight snapshot.

## PRD5 Closeout Summary

The fact-audited classification and next-window plan are in
`docs/planning/PRD5-CURRENT-GAP-AUDIT-20260710.md`; the current P5-F12 screening
evidence is `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`. The
36 matrix rows preserve accepted evidence and explicit defer/reject/prerequisite
decisions. Batch 1Z promotes structural search,
Data Exchange Control, and redacted topology to Target verified and route/failover
visibility to CI verified; P5-F12 screening remains Local verified. Stable X-Insight durable-job
deep links, automated receipt transport, advanced payload modes, and optional
OTel/OpenInference bridging remain unimplemented or X-Insight-owned.

The X-Insight runtime-contract request is Answered. Reconciliation evidence is
`docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md`:
the current sidecar v1 contract remains accepted, X-Insight's synthetic-only
target is verified, and durable jobs/deep links, automated/authenticated receipt,
retention/deletion/audit, stable public adapters, and continuous ingestion are
explicitly not implemented.

UniRouter does not wait on X-Insight for the repository-wide architecture/product
audit, P5-F12 screening, or UniRouter-owned product work. Cross-project blockers
are limited to future X-Insight durable import-job/deep-link contracts, receipt
automation/authenticity, retention/deletion/audit, public adapters, any
continuous ingestion decision, and artifact retention reconciliation. Current X-Insight source has a dedicated UniRouter
adapter but no dedicated LiteLLM/NewAPI/one-api adapter implementation.

## Summary

Per User direction in the PRD2 planning session, PRD1 is treated as closed/frozen for forward planning. Historical PRD1 closeout evidence remains in this file and related PRD1 ops documents, but new product planning should use the PRD2 documents listed below. PRD2 has formal requirements, an acceptance matrix, and implementation evidence through Batch 25. Batch 25 passed Canary 4/4 and User reported Sentinel completed: it adds `docs/tech-ref/19-prompt-response-storage-policy.md`, updates the docs index, reconciles all PRD2 matrix rows away from `Planned` / `Implemented not accepted` / `Unit tested`, and keeps external target-evidence gaps explicit. PRD2 E2E closeout is complete within the documented scope.

Post-closeout Batch 26 UI/UX hardening passed Canary 5/5 and User reported Sentinel/Atlas completed. Batch 27 copy/navigation polish passed Canary 7/7 and User reported Canary/Sentinel/Atlas completed.
Batch 28 product consistency/filter parity passed Canary 6/6 and User reported Canary/Sentinel/Atlas completed. PRD2 is frozen for new feature work; final freeze evidence is recorded in `docs/ops/evidence/PRD2-final-freeze-20260705.md`. PRD3 planning baseline passed Canary 6/6 and User reported Sentinel completed on 2026-07-05: PRD3 is cleanup/optimization of existing functionality only, with PRD4 separated for runtime plugins, plugin marketplace/third-party installation, enterprise IaC packaging, budget hard-block enforcement, runtime cache, maintenance scheduling/SLA/SLO, new persistent roles, and other new capabilities. PRD3 Batch 1 role/navigation/default landing and User Management role workflow passed Canary 6/6; User reported Canary/Sentinel/Atlas completed. PRD3 Batch 2 product glossary, Usage Analytics overall/provider/channel semantics, and Internal Status event-derived semantics passed Canary 5/5; Sentinel merged PR #64 at `a1baf8f`; User reported Atlas completed. PRD3 Batch 3 control-panel semantics passed Canary 5/5; Sentinel merged PR #65 at `8cb71f6`; User reported Atlas completed. PRD3 Batch 3 follow-up for empty-search coverage and comprehensive report evidence routing passed Canary 5/5; Sentinel merged PR #66 at `8a0f670`; User reported Atlas completed. PRD3 Batch 4 Dashboard taxonomy passed Canary 5/5; Sentinel merged PR #67 at `6cbc5b8`; User reported Atlas completed. PRD3 Batch 5 PRD4 backlog hygiene passed Canary 6/6; Sentinel merged PR #68 at `261787a`: infrastructure observability and runtime environment visibility are recorded as PRD4 candidate scope, and current Internal Status remains notification-event-derived. PRD3 Batch 6 API contract refresh passed Canary 6/6; Sentinel merged PR #69 at `1ae60b0`: backend `/docs`, frontend `/docs`, OpenAPI, and route parity guards now align at route/method level for active backend routes. PRD3 Batch 7 document lifecycle cleanup passed Canary 9/9; Sentinel merged PR #70 at `8fe5055`: first-read docs were routed to the then-current PRD3 scope first, marked PRD2 frozen, kept PRD1 historical/reference, and kept PRD4 as candidate backlog. PRD3 Batch 8 engineering governance minimum passed Canary 7/7; Sentinel merged PR #71 at `ca95760`: concise ADR, API compatibility/versioning, migration/rollback, release checklist, incident-response, and deprecation rules plus an ADR template are documented with no runtime code or new product capability. PRD3 Batch 9 frontend shared formatting cleanup passed Canary 7/7; Sentinel merged PR #72 at `6a440b1`: shared frontend number/percentage/latency formatting helpers are in place as the first accepted P3-F10 subset. PRD3 Batch 10 role display label boundary cleanup passed Canary 7/7; Sentinel merged PR #74 at `ecefa1a`; User reported Canary/Sentinel/Atlas completed: role labels, User Management role options, and role tag colors now live in display-label utilities while navigation remains focused on route/menu/RBAC behavior. PRD3 Batch 11 status reconciliation and gap plan passed Canary 6/6; Sentinel merged PR #75 at `132ca79`; User reported Canary and Sentinel completed. PRD3 Batch 12 dependency vulnerability maintenance passed Canary 7/7; Sentinel merged PR #76 at `977b036`; User reported Canary/Sentinel/Atlas completed. PRD3 Batch 13 runtime exposure hardening passed Canary 7/7; Sentinel merged PR #77 at `e8053f3`; User reported Canary/Sentinel/Atlas completed. PRD3 Batch 14 admin helper boundary cleanup passed Canary 6/6; Sentinel merged PR #78 at `73bc34e`; User reported Canary/Sentinel completed. PRD3 Batch 15 frontend paged table cleanup passed Canary 7/7; Sentinel merged PR #79 at `b9ac4e6`; User reported Canary/Sentinel/Atlas completed. PRD3 Batch 16 navigation parity guard passed Canary 6/6; Sentinel merged PR #80 at `e7f264e`; User reported Canary/Sentinel/Atlas completed. PRD3 Batch 17 final evidence hygiene passed Canary 7/7. PRD3 is complete for the confirmed scope; PRD4 should start from `docs/planning/PRD4-BACKLOG.md` and current PRD2/PRD3 evidence boundaries.

Historical PRD1 summary: Core backend modules, admin APIs, frontend pages, migrations, Docker deployment files, and monitoring assets exist. The default-admin cloud security blocker and the channel-key/reconciliation/Dashboard follow-up fixes were redeployed and verified in cloud. The Mimo provider key was cloud-verified as valid; latency persistence and decimal pricing were production-verified after redeploy. Dashboard trend UX passed Canary and local visual smoke. User/Atlas reported the Grafana runtime fix resolved, and Codex verified Grafana default `admin/admin` is rejected. Some PRD1 status rows below still preserve historical follow-up language; they should not be interpreted as PRD2 scope unless explicitly converted into `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`.

## Phase Status

| Phase | Status | Active Documents |
|-------|--------|------------------|
| PRD1 | Closed/frozen for forward planning per User direction | `docs/PRD-Phase1.md`, `docs/ops/PRD-ACCEPTANCE-MATRIX.md`, PRD1 ops evidence docs |
| PRD2 | Batch 25 final closeout docs/status reconciliation passed Canary 4/4, and User reported Sentinel completed. Batch 26 UI/UX hardening passed Canary 5/5, and User reported Sentinel/Atlas completed. Batch 27 copy/navigation polish passed Canary 7/7, and User reported Canary/Sentinel/Atlas completed. Batch 28 product consistency/filter parity passed Canary 6/6, and User reported Canary/Sentinel/Atlas completed: shared display labels, Usage Analytics filter parity with backend-supported filters, localized enum/status labels, and stable `usage_source` API values. Batch 25 adds the P2-F11 prompt/response storage policy and reconciles the matrix to accepted/deferred/out-of-scope states. PRD2 is frozen for new feature work. Batch 24 OIDC-first SSO, Batch 23 P2 product features, Batch 22 License local E2E smoke, and Batch 21 HA readiness evidence remain the latest accepted implementation evidence. External boundaries remain explicit: P2-F10 License is E2E accepted but not `Production verified`; Batch 18 provider real-upstream acceptance needs credentials; budget hard-blocking is deferred; production HA/DR and real enterprise IdP browser login require target-environment evidence | `docs/planning/PRD2-DISCOVERY.md`, `docs/PRD-Phase2.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`, `docs/ops/PRD2-COMPLETION-PLAN.md`, `docs/ops/evidence/`, `docs/agent-workflows/FIX-SIGNAL.md`, `docs/agent-workflows/VERIFY-RESULT.md`, `docs/issues/OPEN-ISSUES.md`, `docs/ops/deployment-runbook.md`, `docs/tech-ref/15-ha-readiness.md`, `docs/tech-ref/16-license-foundation.md`, `docs/tech-ref/17-cache-research.md`, `docs/tech-ref/18-oidc-sso.md`, `docs/tech-ref/19-prompt-response-storage-policy.md`, `docs/tech-ref/14-notification-event-foundation.md` |
| PRD3 | Complete for confirmed scope. User confirmed PRD3 is not a new-feature implementation phase; it is PRD1->PRD2 consistency, semantics, contract, documentation lifecycle, module boundaries, frontend architecture, parity tests, and engineering governance for existing functionality only. PRD3 Batch 17 final evidence hygiene passed Canary 7/7, consolidates all P3 rows to `Canary passed`, preserves PRD2 E2E accepted versus Production verified boundaries, and keeps PRD4 backlog items separate. No planned PRD3 implementation work remains unless User reopens scope | `docs/planning/PRD3-CONSISTENCY-AUDIT.md`, `docs/PRD-Phase3.md`, `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`, `docs/issues/OPEN-ISSUES.md`, `docs/agent-workflows/VERIFY-RESULT.md` |
| PRD4 | Complete for the confirmed current scope. Batch 0 through Batch 1S passed the required gates for scoped PRD4 work. Batch 1S Open Issue Boundary Closeout passed Canary 7/7, Sentinel merge is reflected by `5b41b87` / PR #103, and User reported Atlas deleted `uniroute_test`, closing `PRD4-DEPLOY-002`. PRD4 delivered API key owner usage/cost visibility, quota vocabulary, Flow/Event/Trace foundation, PlungeIn observation-first port contract, Runtime Observability and host telemetry, Prometheus/Grafana integration health, real upstream/SSO guarded harness assets only, auth UX hardening, production-evidence boundary classification, runtime/build-chain hardening, security/redaction guardrails, and PRD5 backlog consolidation. No PRD4 matrix row is promoted to `Production verified` without target-environment evidence. Full Scheduler V2, runtime plugin marketplace, Logto/IAM rewrite, managed-HA IaC product, multi-instance infrastructure observability/topology, budget hard-block enforcement, runtime cache, full SDK, new persistent roles, real upstream target E2E, real SSO target E2E, and public SaaS/commercial growth features are deferred to PRD5 unless User reopens scope | `docs/PRD-Phase4.md`, `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`, `docs/planning/PRD5-BACKLOG.md`, `docs/ops/evidence/PRD4-final-closeout-20260708.md`, `docs/issues/OPEN-ISSUES.md`, `docs/agent-workflows/VERIFY-RESULT.md` |
| PRD5 | Accepted UniRouter scope complete. Historical Batch 1A through Batch 1V evidence remains valid; Batch 1W is target/external-integration verified for the authorized denied scenario. Batch 1X audit/screening is Local verified. Batch 1Y implementation is merged, and Batch 1Z verifies target migration/deployment/browser evidence plus the `INFRA-SEC-001` cutover. Structural search, Data Exchange Control, and redacted topology are Target verified; route/failover visibility is CI verified. X-Insight's synthetic-only target is verified and its runtime request is answered; unimplemented durable-job/deep-link/automated-receipt contracts, migration down, TLS 443 behavior, real upstream/SSO, read-only DB/view, exporter/outbox, and deferred P5-F12 products remain separate. PRD4 remains closed | `docs/PRD-Phase5.md`, `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`, `docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md`, `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`, `docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md`, `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`, `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`, `docs/planning/PRD5-BACKLOG.md`, `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md`, `docs/planning/PRD5-DEFERRED-CONTROL-DESIGNS-20260710.md`, `docs/agent-workflows/EXECUTION-RECORD.md` |
| PRD6 | Completed for Accepted Scope. All 27 accepted feature rows and the separate accepted engineering-integrity issue are repository/required-CI verified or closed. Selected ECS deployment/UI surfaces and P6-E01 are Target verified. Proposed/deferred rows, P6-E02 through P6-E04, PRD7, native IAM revocation, 100x/HA and Production evidence remain later and do not reopen PRD6 | `docs/ops/evidence/PRD6-FINAL-COMPLETION-20260714.md`, `docs/PRD-Phase6.md`, `docs/ops/PRD6-ACCEPTANCE-MATRIX.md`, `docs/ops/evidence/PRD6-ACCEPTED-SCOPE-CLOSEOUT-20260713.md`, `docs/ops/evidence/PRD6-TARGET-DEPLOYMENT-AND-UI-VERIFICATION-20260713.md` |

## Current Verification

| Area | Status | Evidence |
|------|--------|----------|
| V1 Financial Prerequisites | CI verified / closed; exact target rows preserved | PR #196 exact head `2c5e930` / run `29300560434` / merge `86a3bd8` closes unsafe absolute wallet adjustment. PR #197 exact head `74ebf62` / run `29301071299` / merge `94c93dc` closes the public price-unit/default-price defect and adds `000035` without rewriting existing rows. The test target retained every existing price exactly; no automatic conversion or repricing was performed. Evidence: `docs/ops/evidence/V1-POST-PRD6-FINANCIAL-PREREQUISITES-CI-20260714.md` and the V1 target evidence |
| V1 Micro-Precision Ledger And Environment Label | Target verified / closed for the selected development ECS | PR #202 exact head `1944e31` passed run `29334221779` and merged as `ec0dde2`; the authorized non-rolling activation applied `000035`/`000036`. PR #203 exact head `b76f253` passed run `29339373907`, merged as final release `545e514`, and wired the frontend build-time environment label. Stable/canary/runtime/schema/API and authenticated browser evidence passed; `开发环境` renders with zero console errors/warnings. Evidence: `docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-LOCAL-20260714.md`, `docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-TARGET-20260714.md`. No Production, real-provider/detector, HA/capacity, TLS, migration-down or X-Insight consumer claim |
| V1 Post-PRD6 CI Efficiency | CI verified / closed | PR #198 exact head run `29299202652`, squash merge `31d87de`, no automatic merge-commit CI, trusted main run `29299765793`, shared cache `5720112747` and final 32-cache / `9,503,335,891`-byte readback. Docs-only PR #199 run `29300434659` passed with explicit skipped Backend/Frontend/Docker contexts. Evidence: `docs/ops/evidence/V1-POST-PRD6-CI-EFFICIENCY-CI-20260714.md` |
| PRD6 Final Completion | Completed (Accepted Scope) | Fresh 2026-07-14 audit: 27/27 accepted rows closed, clean/aligned pre-publication baseline `9c73d6f`, exact-head main run `29274010760` green, public ECS readiness healthy; `docs/ops/evidence/PRD6-FINAL-COMPLETION-20260714.md`. No Production or future-scope promotion |
| PRD6 Target Deployment And UI Verification | Target verified for selected surfaces | `docs/ops/evidence/PRD6-TARGET-DEPLOYMENT-AND-UI-VERIFICATION-20260713.md`; exact PRD6 stable/canary commit `ce9307a`, exact final frontend `f4fe41d`, migrations through `000034`, public/internal readiness, authenticated Dashboard/limits/Guardrails/routing/reconciliation/logs/status/docs, one historical P6-E01 route/failover detail, mobile checks, PR #191 runbook/Vite correction, PR #192 inline favicon correction and zero-row QA cleanup. No Production, policy-enforcement, real-provider/detector, capacity/HA, TLS or migration-down claim |
| PRD5 X-Insight Runtime Contract Reconciliation | CI verified | PR #136 passed Backend Tests, Frontend Build, and Docker Build. `docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md` records the answered request, synthetic-only X-Insight target, disabled unimplemented capabilities, and UniRouter v1 contract lock. No deployment is claimed |
| PRD4 Final Closeout | Complete for current scope | `docs/ops/evidence/PRD4-final-closeout-20260708.md` records the final closeout. Batch 1S passed Canary 7/7 in `docs/agent-workflows/VERIFY-RESULT.md`, Sentinel merge is reflected by `5b41b87` / PR #103, and User reported Atlas deleted `uniroute_test`, closing `PRD4-DEPLOY-002`. No current PRD4 implementation work remains; PRD5 starts from `docs/planning/PRD5-BACKLOG.md` |
| PRD5 Batch 1X Repository Architecture Audit, P5-F12 Screening, Logs/CI Hygiene | CI verified | `docs/ops/evidence/PRD5-P5F12-architecture-audit-20260710.md`; integrated by PR #131 with all required CI checks passing. P5-F12 screening itself remains a Local verified scope decision rather than target runtime evidence |
| PRD5 Batch 1Y UniRouter-Owned Accepted Scope Completion | CI verified | `docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md`; PR #131 passed Backend Tests, Frontend Build, and Docker Build. Target evidence is split by feature in Batch 1Z |
| PRD5 Batch 1Z Target Deployment And Infrastructure Security | Target verified | `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`; final target backend commit `95d1da9`, migration `000019` applied, stable/canary healthy, target browser checks passed for Data Exchange/Logs/topology, canary runtime parity read back, and least-privilege security-group cutover verified. Route/failover target-detail UI and migration down remain pending |
| PRD5 Batch 1W Pre-Relay Model Permission Evidence Parity | Target verified; External integration verified | `UR-XI-GATE-A-20260710-02` deployed target `e823abc293f996264763c6c1aca19d3d4d6e705f`, containing `931400a` / PR #126. Exactly one denied request returned 403 with matching `X-Request-ID` and the accepted auth decision trace, with zero call logs, billing freezes, transactions, and provider events. Keys 36/37 are disabled. Exchange `uxchg_8deae233291214154815c336c9278983` persisted two events, reran as two duplicate/no-op, and read back two events/two gaps. Evidence: `docs/ops/evidence/PRD5-B1W-target-rerun-20260710.md` |
| PRD5 Batch 1V Gate A Authorized Controlled Execution | Target executed with known source gap; External integration verified | Atlas executed `UR-XI-GATE-A-20260710-01` on deployed app `b2ba892` and generated exchange `uxchg_f7de31e541516893a0d9b31cc0f3892a`, SHA-256 `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8`. X-Insight receipt is `persisted`: 10 accepted, first persistence 10 inserted, rerun 10 duplicate/no-op, 0 conflict, readback 10 events/3 gaps. The original denied request remains factual pre-fix source-gap evidence. Its follow-up is separately verified by Batch 1W; first-snapshot cleanup remains separate, while X-Insight's synthetic-only target is now verified |
| PRD5 Batch 1U Embeddings Model Permission Parity | Canary/Sentinel complete per repo state | Canary PASS 7/7 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `b2ba892` / PR #124. Closes the Embeddings `allowed_models` authorization bypass with a shared Chat/Embeddings predicate, pre-billing 403, `X-Request-ID`, and the existing auth decision evidence shape. No route, migration, frontend, billing amount, routing/provider success-path, raw payload, exporter/outbox, X-Insight runtime dependency, real Gate A execution, target claim, or PRD4 change is included |
| PRD5 Batch 1T Gate A Exchange Envelope And Controlled Run Authorization | Canary/Sentinel complete per repo state | Canary PASS 7/7 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `fc62e65` / PR #123. X-Insight accepted the envelope and Atlas later executed the controlled Gate A authorization in Batch 1V |
| PRD5 Batch 1S X-Insight Data Exchange Control Surface | Canary/Sentinel complete per repo state | Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `c6ae2a7` / PR #122. Defines the UniRouter outbound-control versus X-Insight downstream-observability boundary without UI/API/migration/runtime changes or PRD4 reopening |
| PRD5 Batch 1R Gate A Request ID Approval Runbook | Canary/Sentinel complete per repo state | Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `c15cd7d` / PR #121. Adds `docs/ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md` and the shared approval worksheet `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md`. The runbook explains request IDs as single-request receipts, defines `approved` / `pending` / `unavailable`, compares approval manifest versus auto-discovery/read-only DB/exporter alternatives, and gives a non-technical approval sentence. No real snapshot artifact, approved request ID invention, DSN/credential/token/raw payload, migration, runtime request-path change, new API route, frontend behavior, read-only DB/view, shared ECS staging, exporter/outbox, X-Insight runtime dependency, or PRD4 matrix change is included |
| PRD5 Batch 1Q Gate A Request Approval Manifest | Canary/Sentinel complete per repo state | Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `ed8c69d` / PR #120. Adds `--approval-manifest` to `scripts/prd5_xinsight_snapshot.go` for X-Insight's approved request ID data requirement. The tool exports only manifest rows marked `approved` with concrete IDs, treats `pending`/`unavailable` as non-exported, rejects placeholder approved IDs, and rejects combining manifest mode with direct request flags. No real snapshot artifact, approved request ID invention, DSN/credential/token/raw payload, migration, runtime request-path change, new API route, frontend behavior, read-only DB/view, shared ECS staging, exporter/outbox, X-Insight runtime dependency, or PRD4 matrix change is included |
| PRD5 Batch 1P Model Permission Decision Evidence | Canary/Sentinel complete per repo state | Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `61091c` / PR #119. Existing chat `allowed_models` rejects emit `auth.model_not_allowed` trace evidence before the existing 403 response. No migration, new API route, frontend behavior, read-only DB/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, Embeddings authorization behavior change, billing/routing/provider dispatch semantic change, or PRD4 matrix change is included |
| PRD5 Batch 1O Gate A Controlled Snapshot Handoff Prep | Canary/Sentinel complete per repo state | Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `b2eea2e` / PR #118. Reconciles PRD5 Batch 1N Canary/Sentinel completion as `92cb8ce` / PR #117 and prepares the next controlled sanitized snapshot handoff request. No real snapshot artifact, DSN, credential, raw prompt/response, raw IP, raw upstream error, read-only DB/view, shared ECS staging, exporter/outbox, runtime request-path change, new API route, frontend behavior, X-Insight runtime dependency, or PRD4 matrix change is included |
| PRD5 Batch 1N Snapshot Capture Gap Candidates | Canary/Sentinel complete per repo state | Canary PASS 10/10 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `92cb8ce` / PR #117. Adds optional `candidate_capture_gaps` to generated Gate A snapshot gap records and includes generated gaps in Markdown handoff notes. Missing trace rows with an existing call log list `sampled_out`, `retention_expired`, and `not_instrumented` as candidates while the primary generated `capture_gap` remains conservative. No migration, runtime request-path change, new API route, frontend behavior, read-only credential/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, definitive sampling/retention cause assertion, or PRD4 matrix change is included |
| PRD5 Batch 1M Provider Failure Capture Gaps | Canary/Sentinel complete per repo/User state | Canary PASS 10/10 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `4a4e254` / PR #116. Adds provider failure capture-gap semantics: pre-response provider failures without a sanitized upstream request ID write `capture_gap=not_available_from_source`, provider response events keep `provider_not_returned`, metadata redaction gaps keep priority, and embeddings parse failures preserve available upstream request IDs. No migration, new API route, frontend behavior, read-only credential/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, provider dispatch semantic change, or PRD4 matrix change is included |
| PRD5 Batch 1L Component Span Refinement | Canary/Sentinel complete per repo/User state | Canary PASS 10/10 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `ffebf83` / PR #115. Adds request-local component span tracking for trace events: the first UniRouter trace event remains the gateway root, first component events parent to that root, and repeated same-component events parent to that component's first span. No migration, new API route, frontend behavior, read-only credential/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, cross-system graph inference, or PRD4 matrix change is included |
| PRD5 Batch 1K Gate A Snapshot Handoff Note | Canary/Sentinel complete per repo/User state | Canary PASS 10/10 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `2db830a` / PR #114. Adds optional `--handoff-note <path>` support to `scripts/prd5_xinsight_snapshot.go` so controlled X-Insight snapshots can be paired with a DSN-free Markdown note containing generated date, source instance, schema version, snapshot output path, selected request IDs, scenario labels, counts, safety confirmation, and command shape without DSN. No real snapshot artifact is generated without approved request IDs and a read-only DSN; no migration, new API route, frontend behavior, read-only credential/view, shared ECS staging, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, or PRD4 matrix change is included |
| PRD5 Batch 1J Provider Upstream Capture Gap | Canary/Sentinel complete per repo/User state | Sentinel merge is reflected by `f5223df` / PR #113. Adds `capture_gap=provider_not_returned` to `provider.response_accepted` and `provider.response_rejected` trace events when UniRouter has no sanitized provider-returned `upstream_request_id`. Existing returned IDs and redaction gaps are preserved. No migration, new API route, frontend behavior, read-only credential, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, public inbound-header authority by default, provider semantic change, or PRD4 matrix change is included |
| PRD5 Batch 1I Payload Policy Decision Evidence | Canary/Sentinel complete per repo/User state | Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `1dc20b5` / PR #112. Adds explicit `payload.policy.applied` trace events for chat and embeddings after gateway request acceptance. Events write sanitized `payload_capture_mode`, `payload_policy_reason`, `raw_payload_stored=false`, `payload_ref_present=false`, and `decision_kind=payload_capture`; metadata-only mode maps to `allowed`, disabled mode maps to `skipped`. No migration, new API route, frontend behavior, read-only credential, exporter/outbox, raw prompt/response capture, payload hashing/reference storage, X-Insight runtime dependency, public inbound-header authority by default, or PRD4 matrix change is included |
| PRD5 Batch 1H Gateway Span Hierarchy | Canary/Sentinel complete per repo/User state | Canary PASS 10/10 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `61f5610` / PR #111. Adds per-request gateway root span state for UniRouter-emitted trace events, preserves trusted inbound parent span for the first UniRouter span when present, parents later UniRouter trace events to the gateway root span, and includes the resulting `parent_span_id` in source-event hashes. No migration, new API route, frontend behavior, read-only credential, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, public inbound-header authority by default, or PRD4 matrix change is included |
| PRD5 Batch 1G Trusted Evidence Context Intake | Canary/Sentinel complete per repo/User state | Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `c701843` / PR #110. Adds default-off `EVIDENCE_CONTEXT_TRUST_SECRET`, `X-UniRouter-Evidence-Auth` matching before accepting inbound evidence context, bounded `X-UniRouter-*` trace/session/actor/delegation headers, trusted `Traceparent` fallback, trace/log structural writes for trusted context, source-event hashes that include those fields, and env/compose/runbook docs. No migration, new API route, frontend behavior, read-only credential, exporter/outbox, raw prompt/response capture, X-Insight runtime dependency, public inbound-header authority by default, or PRD4 matrix change is included |
| PRD5 Batch 1F Payload Capture Mode Configuration | Canary/Sentinel complete per repo/User state | Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `e9670ad` / PR #109. Adds restricted `PAYLOAD_CAPTURE_MODE` config with default `metadata_only`, active `metadata_only`/`disabled` support, config validation for unsupported modes, trace/log evidence writes using the configured `payload_capture_mode`, source-event hashes that include the mode, and env/compose/runbook docs. No migration, new API route, frontend behavior, read-only credential, exporter/outbox, raw prompt/response capture, payload object store, redacted/hash/reference active modes, fabricated session/delegation fact, or PRD4 matrix change is included |
| PRD5 Batch 1E Event Capture Gap Emission | Canary/Sentinel complete per repo/User state | Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `a19830f` / PR #108. Adds `capture_gap='redacted'` for trace events whose metadata was intentionally dropped, shape-rejected, or truncated by UniRouter's sanitizer. No migration, new API route, frontend behavior, deployment config, read-only credential, exporter/outbox, raw prompt/response capture, fabricated session/delegation fact, or PRD4 matrix change is included |
| PRD5 Batch 1D Upstream Request Correlation | Canary/Sentinel complete per repo/User state | Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `bf435ca` / PR #107. Adds bounded provider response-header extraction for upstream request IDs, sanitizes values before persistence, writes `upstream_request_id` to provider response trace events and call logs, and updates P5-F13 docs/status. No inbound public-header trust, migration, new API route, frontend behavior, deployment config, read-only credential, exporter/outbox, raw prompt/response capture, or PRD4 matrix change is included |
| PRD5 Batch 1A/1B/1C Scope / Snapshot / Structural Evidence / Trace UI | Canary/Sentinel complete per repo/User state | Canary PASS 17/17 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `e1e76e3` / PR #106. Adds `docs/PRD-Phase5.md`, `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`, `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`, `docs/planning/PRD5-COMPETITOR-COMPARISON.md`, `docs/tech-ref/25-prd5-xinsight-sanitized-snapshot.md`, `docs/tech-ref/26-prd5-evidence-field-contract.md`, `scripts/prd5_xinsight_snapshot.go`, migration `000018`, gateway trace/log structural writes, existing trace API read-side structural fields, Logs detail evidence quality / structural evidence tags, docs routing updates, and a shared-folder Gate A snapshot contract |
| PRD4 Batch 1S Open Issue Boundary Closeout | Canary/Sentinel/Atlas complete per repo/User state | `PRD4-PLUGIN-001`, `PRD4-DEPLOY-001`, and `PRD4-INFRA-001` are deferred to PRD5; `PRD4-DEPLOY-002` is closed by User-reported Atlas cleanup. Scope is limited to docs/status-boundary classification and target cleanup evidence; it does not change Go, frontend, migration, API route, runtime behavior, deployment scripts, or `Production verified` claims |
| PRD4 Batch 1R Runtime Build Chain Follow-up | Canary/Sentinel complete per repo state | `PRD4-RUNTIME-002` repo-controllable gaps are fixed and verified: Go toolchain pin, Docker builder patch version, runtime version/commit injection, compose `RUNTIME_ENVIRONMENT`, compose Grafana service URL, backend healthcheck, env/runbook docs, and evidence file `docs/ops/evidence/PRD4-B1R-runtime-build-chain-local-20260708.md`. Canary PASS 9/9 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `819846c` / PR #102. Docker CLI was unavailable to Canary, so `docker compose config` and live container health remain CI/Atlas checks before any Production verified runtime claim |
| PRD4 Batch 1Q Production Evidence Promotion | Canary/Sentinel complete per repo state | `P4-F11` evidence classification is recorded in `docs/ops/evidence/PRD4-B1Q-production-evidence-promotion-20260708.md`. Current target evidence is useful target-smoke evidence for selected areas, but no PRD4 matrix row is promoted to `Production verified` because the report covers the PRD4 Batch 1N target state and Batch 1O fixed later findings. `PRD4-RUNTIME-002` was still open at Batch 1Q and later passed Batch 1R Canary for repo-controllable runtime/build-chain items. Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel commit is reflected by `f0d34e1` |
| PRD4 Batch 1P Scope Deferral And Closeout Alignment | Canary/Sentinel complete per repo state | User decision on 2026-07-08 moves `P4-F08` / `P4-F09` real target execution out of current PRD4 because no approved real upstream or real SSO environments are available. Batch 1H/1I harnesses remain accepted assets; `docs/planning/PRD5-BACKLOG.md` now tracks `PRD5-UPSTREAM-001` and `PRD5-SSO-001`; `P4-F13` PRD5 backlog consolidation is accepted. Canary PASS 6/6 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `9e2927f` / PR #100 |
| PRD4 Batch 1O API Key Input And Security Header Hardening | Canary/Sentinel complete per repo state | `PRD4-SECURITY-001` / `P4-F12` addresses concrete findings from `docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md`: API key create/update now rejects unsafe or overlong display names and negative `project_id`; the HTTP handler chain applies `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`, and a frame/base/form CSP; and unknown GET paths no longer fall through to the root service/version response. Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `59721d7` / PR #99. Repo-controllable runtime/build-chain follow-ups passed Batch 1R Canary as `PRD4-RUNTIME-002`; target DB cleanup is tracked as `PRD4-DEPLOY-002` |
| PRD4 Batch 1N Developer Console Self Usage And Model Cost | Canary/Sentinel complete per repo state | `PRD4-USAGE-002` / `P4-F02` adds self-owned usage/cost visibility to Developer Console using the existing Usage Analytics API: `scope=my`, selected `project_id`, and `group_by=alias`. KPI tiles use the owner-attributed summary when available, and the model usage table shows requests, success/failure, tokens, cost, and latency by model/alias. Canary PASS 9/9 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `6194062` / PR #98; target-environment regression evidence is archived at `docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md` |
| PRD4 Batch 1M Trace Metadata Read-Side Redaction | Canary/Sentinel complete per repo state | `PRD4-REDACTION-002` / `P4-F12` adds shared trace metadata sanitization for gateway write-side events and admin read-side responses. Safe scalar metadata such as `estimated_tokens` and `usage_source` remains visible; restricted keys such as `api_key`, `authorization`, `access_token`, `request_body`, and `prompt` are dropped, and unsupported nested values are not returned. Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `2f43fca` / PR #97 |
| PRD4 Batch 1L Runtime Monitoring URL Redaction | Canary/Sentinel complete per repo state | `PRD4-REDACTION-001` / `P4-F12` now has verified runtime code and tests: Prometheus/Grafana runtime status `url` and `health_url` strip URL userinfo, query strings, and fragments, while probes still use the original configured URL. Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `4426efd` / PR #96. No monitoring auth config, embedded dashboards, OpenTelemetry, cluster inventory, route/RBAC, frontend layout, billing, routing, auth, SSO, upstream, wallet, provider-dispatch, or Production verified behavior changed |
| PRD4 Batch 1K Trace Retention And Cleanup | Canary/Sentinel complete per repo state | `PRD4-TRACE-002` now has verified code/config/docs changes: `TRACE_EVENT_RETENTION_DAYS` defaults to `LOG_RETENTION_DAYS`, validates non-negative values, is exposed in `.env.example`, `docker-compose.yml`, and deployment runbook, and the existing cleanup loop deletes expired `request_trace_events`. Canary PASS 10/10 was recorded before Batch 1L overwrote the current `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `1d9ea50` / PR #95. No trace schema migration, prompt/response plaintext storage, partitioning, archival, OpenTelemetry export, runtime cache, or Production verified target evidence is included |
| PRD4 Batch 1J Runtime Host Telemetry | Canary/Sentinel complete per repo state | `P4-F06` now includes host CPU/disk/network summaries in `GET /api/v1/runtime/status` and `/status`: CPU logical cores plus Linux sampled usage where `/proc/stat` is available, disk total/used/free/root mount usage, and network total/active interface counts plus Linux rx/tx counters where `/proc/net/dev` is available. Canary PASS 8/8 was recorded before Batch 1K overwrote the current `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `8707346` / PR #94. No cluster inventory, cloud topology, private IP/hostname identifiers, OpenTelemetry, metric alerts, or Production verified target evidence is claimed |
| Protocol Risk-Gated Workflow Update | Canary passed | Protocol update passed before later PRD4 product batches; `docs/agent-workflows/VERIFY-RESULT.md` has since been overwritten by Batch 1L. The workflow gate changed no runtime code, migrations, PRD4 product behavior, or target-environment evidence |
| PRD4 Batch 1I Real SSO Evidence Harness | Canary/Sentinel complete per repo state; real execution deferred to PRD5 | `PRD4-SSO-001` adds guarded evidence tooling for `P4-F09`: `scripts/prd4-real-sso-smoke.sh --preflight` is safe without target credentials and reports `SKIP` without driving a browser, calling the IdP, or reading DB; real mode requires Logto or enterprise OIDC target access, a test user, console/backend/issuer URLs, DB DSN, and `UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso`. Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `2c02430` / PR #92. User decision on 2026-07-08 defers real browser-login execution to `PRD5-SSO-001` because no approved real SSO environment is currently available |
| PRD4 Batch 1H Real Upstream Evidence Harness | Canary/Sentinel complete per repo state; real execution deferred to PRD5 | `PRD4-UPSTREAM-001` adds guarded evidence tooling for `P4-F08`: `scripts/prd4-real-upstream-smoke.sh --preflight` is safe without credentials and reports `SKIP` without DB mutation or provider calls; real mode requires OpenRouter and direct Anthropic credentials/model slugs, matching backend secrets, and `UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream`. The harness seeds a dedicated target-evidence org/project/model/channel/API-key fixture, runs non-stream and stream gateway requests for both adapter paths, checks call logs/billing/trace events, and writes sanitized JSON evidence. Canary PASS 11/11 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `d3acfa6` / PR #91. User decision on 2026-07-08 defers real provider execution to `PRD5-UPSTREAM-001` because no approved real upstream environment is currently available |
| PRD4 Batch 1G External Monitoring Integration | Canary/Sentinel complete per repo state | `PRD4-MONITORING-001` extends `GET /api/v1/runtime/status` to actively probe configured Prometheus and Grafana: Prometheus `/-/ready` plus `up{job="unirouter"}`, Grafana `/api/health`, status reasons, latency, scrape status, version, and frontend Runtime Observability rendering. This is a Runtime Status integration only: no embedded dashboards, Grafana datasource validation, new metrics auth, OpenTelemetry export, host telemetry collectors, generic monitoring connector framework, or runtime plugin platform. Canary PASS 9/9 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `bb291a4` / PR #90. Atlas deployment has not been reported |
| PRD4 Batch 1F PlungeIn Port Architecture | Canary/Sentinel complete per User report and repo state | `PRD4-PLUNGEIN-001` accepts ADR 0001 for PRD4 scope and adds `docs/tech-ref/22-prd4-plungein-port-contract.md` with Observe, Act, limited Enrich, advisory Decide, and forbidden Mutate boundaries. This is documentation-only: no Go/frontend runtime code, migration, API route, deployment config, runtime plugin loader, manifest, sandbox, marketplace, third-party installation, or core auth/billing/routing/wallet/settlement/provider-dispatch mutation is included. Canary PASS 8/8 is recorded in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merge is reflected by `87bfc17` / PR #89. Atlas deployment has not been reported |
| PRD4 Batch 1E Quota Vocabulary And Key Quota Foundation | Canary/Sentinel complete per User report | `PRD4-QUOTA-001` standardizes user-facing RPM/RPS/TPM/tokens/s vocabulary, keeps legacy `qps_limit` compatibility, adds preferred `rps_limit` and structured key `quota` response data, updates Admin API Keys and Developer Console quota UI, and documents the compatibility boundary in `docs/tech-ref/21-prd4-quota-vocabulary.md`. Scheduler V2, key-level TPM enforcement, budget hard-blocking, quota audit reason/idempotency fields, and schema migration are not included. Canary PASS 8/8 in `docs/agent-workflows/VERIFY-RESULT.md`; User reported Sentinel completed after `a1acdb6` / PR #88. Atlas deployment has not been reported |
| PRD4 Batch 1D Runtime Observability Foundation | Canary/Sentinel complete per User report | `PRD4-RUNTIME-001` adds `GET /api/v1/runtime/status` for platform/org/billing roles, runtime metadata config (`RUNTIME_ENVIRONMENT`, `RUNTIME_DEPLOYMENT_MODE`, `RUNTIME_DEPLOYMENT_ROLE`, `RUNTIME_VERSION`, `RUNTIME_COMMIT`), optional Prometheus/Grafana URL validation, app memory, DB/Redis health, recent traffic summary, frontend `/status` Runtime Observability cards, and route/OpenAPI/frontend docs parity. Batch 1D originally kept CPU/disk/network as explicit `telemetry_source_not_configured`; Batch 1J supersedes that placeholder with host telemetry. User reported Canary/Sentinel completed after `349ac75` / PR #87; Atlas deployment has not been reported |
| PRD4 Batch 1C Flow/Event/Trace Foundation | Canary/Sentinel complete per User report | `PRD4-TRACE-001` adds migration `000017_prd4_request_trace_events`, metadata-only trace events, deterministic `TRACE_EVENTS_ENABLED` / `TRACE_EVENT_SAMPLE_RATE` config, gateway/billing/routing/provider/call-log event recording for chat and embeddings, `GET /api/v1/logs/{id}/trace` with project usage RBAC, Logs detail trace rendering, and route/OpenAPI/frontend docs parity updates. User reported Canary/Sentinel completed after `4f62e34` / PR #86; Atlas deployment has not been reported |
| PRD4 Batch 1B Usage And Cost Attribution | Canary/Sentinel complete per User report | `PRD4-USAGE-001` adds migration `000016_prd4_key_owner_usage`, API key owner/type fields, gateway `call_logs` owner/type snapshots, usage analytics `scope=my`, `owner_user_id`, `key_type`, `group_by=owner`, and `group_by=key_type`, plus frontend controls and API serialization. User deletion now disables that user's personal API keys before hard delete. Canary passed and User reported Sentinel completed after `7dd8aa0` / PR #85; Atlas deployment has not been reported |
| PRD4 Batch 1A Auth UX Hardening | Canary/Sentinel complete per User report | `PRD4-AUTHUX-001` adds shared `frontend/src/ui/form/PasswordField.tsx`, uses it in `LoginPage`, `SettingsPage`, and `UsersPage`, keeps passwords hidden by default, exposes explicit `显示密码` / `隐藏密码` icon buttons, and resets the admin drawer password field to hidden on drawer/user changes. `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 6/6 with focused frontend tests, broad frontend regression 108/108, build, status grep, and `git diff --check`. User reported Sentinel completed after `620794b` / PR #84; Atlas deployment has not been reported |
| PRD4 Preflight Billing Price Error Hardening | Canary/Sentinel/Atlas complete per User report | `PRD4-BILLING-001` fixes confirmed P0 billing correctness risk: `DBPriceProvider.QueryModelPrice` now defaults only on unconfigured model rows, `Billing.QueryModelPriceForOrg` returns errors on query failure, gateway cost calculation propagates price lookup errors, and non-stream/embedding failures cancel frozen billing instead of default-charging. `docs/agent-workflows/VERIFY-RESULT.md` reports PRD4 Preflight Hardening PASS 6/6; User reported Sentinel/Atlas completed after `e335a9e` / PR #83 |
| PRD4 Preflight Shutdown Timeout Hardening | Canary/Sentinel/Atlas complete per User report | `PRD4-SHUTDOWN-001` replaces the hardcoded 30-second graceful shutdown context with `SHUTDOWN_TIMEOUT_SECONDS`, defaulting to 120 seconds to match the current backend write timeout for normal streaming responses. `docs/agent-workflows/VERIFY-RESULT.md` reports config, cmd, env, compose, and runbook evidence passed; User reported Sentinel/Atlas completed after `e335a9e` / PR #83 |
| PRD4 Preflight Error Docs Coverage | Canary/Sentinel/Atlas complete per User report | `PRD4-ERRDOCS-001` adds direct unit tests for the user-visible `internal/errdocs` registry: required fields, duplicate code prevention, URL mapping, unknown-code fallback, and defensive-copy behavior. `docs/agent-workflows/VERIFY-RESULT.md` reports `internal/errdocs` coverage passed; User reported Sentinel/Atlas completed after `e335a9e` / PR #83 |
| PRD4 Batch 0 Scope Baseline | Canary/Sentinel/Atlas complete per User report | Drafted `docs/PRD-Phase4.md`, `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`, `docs/planning/PRD5-BACKLOG.md`, and PRD4 ADRs for PlungeIn ports, API key owner usage, and Status Center/runtime observability. No runtime code, API, migration, frontend behavior, or deployment config changed. User reported Canary/Sentinel/Atlas completed after `e24731d` / PR #82; later `VERIFY-RESULT.md` was overwritten by PRD4 Preflight Hardening |
| PRD3 Final Closeout | Complete for confirmed scope | PRD3 Batch 17 final evidence hygiene passed Canary 7/7 in `docs/agent-workflows/VERIFY-RESULT.md`. All P3-F01 through P3-F14 matrix rows are accepted, PRD2 `E2E accepted` versus `Production verified` gaps remain explicit, PRD4 backlog boundaries are preserved, and no runtime code changes were made in the final closeout. Evidence: `docs/ops/evidence/PRD3-B17-final-evidence-closeout-local-20260706.md` |
| PRD3 Batch 16 Navigation Parity Guard | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 16 PASS 6/6. Codex added a targeted `frontend/src/__tests__/navigation.test.tsx` guard that checks current visible menu leaf routes and default landing paths for `admin`, `platform_admin`, `org_admin`, `billing_admin`, `project_admin`, `developer`, `viewer`, `org_viewer`, and unknown roles against `isRouteAllowedForRole`. Sentinel merged PR #80 at `e7f264e`; User reported Canary/Sentinel/Atlas completed. Evidence: `docs/ops/evidence/PRD3-B16-navigation-parity-guard-local-20260706.md` |
| PRD3 Batch 15 Frontend Paged Table Cleanup | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 15 PASS 7/7. Codex added shared `PagedTable`, exported it from `frontend/src/ui`, moved repeated table shells in `ModelPricesPage`, `OrgModelPricesPage`, `ChannelKeysPage`, and `GrantsPage` to the shared component, fixed existing page-size passthrough drift in grants and org model prices, added `PagedTable` coverage, and updated docs/status. Sentinel merged PR #79 at `b9ac4e6`; User reported Canary/Sentinel/Atlas completed. Evidence: `docs/ops/evidence/PRD3-B15-frontend-paged-table-local-20260706.md` |
| PRD3 Batch 14 Admin Helper Boundary Cleanup | Canary/Sentinel complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 14 PASS 6/6. Codex split admin event emitters, HTTP JSON/pagination helpers, and model normalization helpers into package-local helper files; consolidated duplicate admin pagination parsing/offset logic; added pagination helper tests; and updated the codebase map. Sentinel merged PR #78 at `73bc34e`; User reported Canary/Sentinel completed. Evidence: `docs/ops/evidence/PRD3-B14-admin-helper-boundary-local-20260706.md` |
| PRD3 Batch 13 Runtime Exposure Hardening | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 13 PASS 7/7. Codex fixed SEC-005 on existing surfaces: strict admin CORS allowlist, Redis-backed login throttling, runtime-configurable `/metrics` enable/CIDR policy, env/compose/runbook/docs updates, and local regression/security checks. Sentinel merged PR #77 at `e8053f3`; User reported Canary/Sentinel/Atlas completed. Evidence: `docs/ops/evidence/PRD3-B13-runtime-exposure-hardening-local-20260706.md` |
| PRD3 Batch 12 Dependency Vulnerability Maintenance | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 12 PASS 7/7. Codex upgraded vulnerable Go and frontend dependencies for SEC-004, including `pgx`, `jwt`, `go-redis`, Vite, and esbuild. `pgx@v5.9.2` requires Go 1.25, so `go.mod` now declares `go 1.25.0`. Sentinel merged PR #76 at `977b036`; User reported Canary/Sentinel/Atlas completed. Evidence: `docs/ops/evidence/PRD3-B12-dependency-vulnerability-maintenance-local-20260706.md` |
| PRD3 Batch 11 Status Reconciliation And Gap Plan | Canary/Sentinel complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 11 PASS 6/6. Codex reconciled Batch 10 PASS/Sentinel/Atlas state into current status docs, updated the PRD3 matrix and open-issue next steps, and recorded the remaining PRD3 completion plan without runtime code or feature changes. Sentinel merged PR #75 at `132ca79`; User reported Canary and Sentinel completed. Evidence: `docs/ops/evidence/PRD3-B11-status-reconciliation-local-20260706.md` |
| PRD3 Batch 10 Role Display Label Boundary Cleanup | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 10 PASS 7/7. Codex moved role display labels, User Management role options, and role tag colors from `frontend/src/navigation.tsx` into `frontend/src/utils/displayLabels.ts`, updated App/AccessState/DeveloperConsole/Users consumers, moved role-option coverage into `frontend/src/utils/__tests__/displayLabels.test.ts`, and preserved navigation/RBAC behavior. Sentinel merged PR #74 at `ecefa1a`; User reported Canary/Sentinel/Atlas completed. Evidence: `docs/ops/evidence/PRD3-B10-role-display-labels-local-20260705.md` |
| PRD3 Batch 9 Frontend Shared Formatting Cleanup | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 9 PASS 7/7. Codex added `frontend/src/utils/formatNumber.ts` and `frontend/src/utils/__tests__/formatNumber.test.ts`, replaced duplicate formatting helpers in `DashboardPage`, `UsageAnalyticsPage`, and `ReconciliationPage`, and recorded local evidence in `docs/ops/evidence/PRD3-B09-frontend-formatting-local-20260705.md`. Sentinel merged PR #72 at `6a440b1`; User reported Atlas completed. Batch 9 is one accepted P3-F10 subset; later accepted P3-F10 subsets are recorded in Batch 10 and Batch 15. |
| PRD3 Batch 8 Engineering Governance Minimum | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 8 PASS 7/7. Codex added `docs/governance/README.md`, `docs/governance/ADR-TEMPLATE.md`, docs index/governance/inventory references, `CONTRIBUTING.md` governance pointers, and acceptance/open-issue status updates. Sentinel merged PR #71 at `ca95760`; User reported Atlas completed. This is docs-only PRD3 cleanup for P3-F12/PRD3-ENG-001; it does not implement plugin, deployment, runtime observability, SLA/SLO, or other PRD4 capabilities. Evidence: `docs/ops/evidence/PRD3-B08-engineering-governance-local-20260705.md` |
| PRD3 Batch 7 Document Lifecycle Cleanup | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 7 PASS 9/9. Codex updated root README, docs README, PROJECT-HANDOFF, DOCS-GOVERNANCE, DOC-INVENTORY, and CODEBASE-MAP so first-read docs reflected the then-current PRD3 scope, frozen PRD2 state, historical PRD1 references, and PRD4 candidate backlog. Sentinel merged PR #70 at `8fe5055`; User reported Atlas completed. No archive files were rewritten and no files were deleted. Evidence: `docs/ops/evidence/PRD3-B07-doc-lifecycle-cleanup-local-20260705.md` |
| PRD3 Batch 6 API Contract Refresh | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 6 PASS 6/6. Codex refreshed backend `/docs`, frontend `/docs`, `docs/tech-ref/05-openapi-spec.yaml`, route contract wording, and route parity guards. OpenAPI now covers registered backend route paths/methods at route level; response schemas stay generic unless stable typed contracts exist. Sentinel merged PR #69 at `1ae60b0`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B06-api-contract-refresh-local-20260705.md` |
| PRD3 Batch 5 PRD4 Backlog Hygiene | Canary/Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 5 PASS 6/6. Codex added `docs/planning/PRD4-BACKLOG.md`, recorded infrastructure observability/runtime environment visibility as `PRD4-INFRA-001`, updated PRD3 scope docs, glossary, issue routing, docs indexes, and acceptance matrix, and kept current Internal Status scoped to notification-event-derived semantics. Sentinel merged PR #68 at `261787a`. No runtime code, API, telemetry, or UI implementation changed. Evidence: `docs/ops/evidence/PRD3-B05-prd4-backlog-hygiene-local-20260705.md` |
| PRD3 Batch 4 Dashboard Taxonomy | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 4 PASS 5/5. Codex changed the Dashboard page header from `Gateway overview` / `AI Gateway 控制台` to `Platform Overview` / `平台总览`, adjusted the description to `统一观察...`, and added a DashboardPage regression test that asserts the new taxonomy and rejects the old labels. Sentinel merged PR #67 at `6cbc5b8`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B04-dashboard-taxonomy-local-20260705.md` |
| PRD3 Batch 3 Follow-up: Empty Search Test And Evidence Routing | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 3 follow-up PASS 5/5. Codex added explicit App shell coverage for empty topbar search routing to `/logs`, moved the 2026-07-05 comprehensive Canary report from historical archive to `docs/ops/evidence/COMPREHENSIVE-TEST-REPORT-20260705.md`, and registered reproduced dependency/runtime hardening risks as open issues. Sentinel merged PR #66 at `8a0f670`; User reported Atlas completed. |
| PRD3 Batch 3 Control Panel Semantics | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 3 PASS 5/5. Codex wired the topbar global search to existing request-log search (`/logs?search=...`), changed the environment label from hard-coded `生产环境` to explicit `VITE_APP_ENV` / `VITE_DEPLOY_ENV` with `环境未配置` fallback, and added App shell tests. Page-level scan found no additional P3-F03 blocking inert controls; stale API docs remain P3-F04. Canary noted one non-blocking test coverage suggestion for empty search. Sentinel merged PR #65 at `8cb71f6`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B03-control-panel-semantics-local-20260705.md` |
| PRD3 Batch 2 Usage And Status Semantics | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 2 PASS 5/5. Codex added `docs/tech-ref/20-prd3-product-glossary.md`, added `group_by=overall` for Usage Analytics, made the frontend default to explicit `总览（不分组）`, clarified provider versus upstream channel in the filter panel, localized event lifecycle labels, and marked maintenance windows as not enabled/PRD4. Canary verified glossary, Usage Analytics semantics, Internal Status semantics, evidence/status docs, full Go tests, full frontend tests (16 files / 92 tests), frontend build, and `git diff --check`. Sentinel merged PR #64 at `a1baf8f`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B02-usage-status-semantics-local-20260705.md` |
| PRD3 Batch 1 Role Navigation | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PRD3 Batch 1 PASS 6/6. Codex aligned frontend role navigation and User Management with current backend role groups: org admins no longer see platform/billing-only pages; `org_viewer` and unknown roles land on `/access` instead of API Docs; notification topbar access is role-aware; User Management exposes canonical non-legacy roles. Canary verified source, evidence/status docs, full frontend tests (16 files / 92 tests), frontend build, and `git diff --check`. Sentinel merged PR #63 at `9bed3be`; User reported Atlas completed. Evidence: `docs/ops/evidence/PRD3-B01-role-navigation-local-20260705.md` |
| PRD3 Planning Baseline | Canary passed; Sentinel completed per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 6/6. User confirmed PRD3 should not implement new capabilities. Codex created `docs/planning/PRD3-CONSISTENCY-AUDIT.md` with PRD1->PRD2 lineage findings and user-confirmed recommendations, created `docs/PRD-Phase3.md`, created `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`, updated `docs/issues/OPEN-ISSUES.md`, `docs/DOC-INVENTORY.md`, `docs/README.md`, and `docs/agent-workflows/FIX-SIGNAL.md`. PRD4 backlog is separated from PRD3 cleanup scope |
| PRD2 Batch 28 Product Consistency And Filter Parity | Canary passed; Sentinel/Atlas completed per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports Batch 28 PASS 6/6 for shared frontend display labels, Usage Analytics filter parity with backend-supported parameters, localized enum/status display, stable `usage_source` API values, and evidence/status handoff. Local validation passed: `npm --prefix frontend test -- --run` (16 files / 88 tests), `npm --prefix frontend run build` (existing large chunk warning only), `go test ./internal/admin -count=1`, `go test ./... -count=1`, and `git diff --check`. Evidence: `docs/ops/evidence/PRD2-B28-product-consistency-local-20260704.md` |
| PRD2 Batch 27 Copy And Navigation Polish | Canary passed; Sentinel/Atlas completed per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports Batch 27 PASS 7/7 for sidebar API docs/logout separation, request-log detail contrast, enterprise menu/page naming, login positioning copy, channel test latency/token boundary documentation, API docs row-key cleanup, and evidence/status handoff. Commands passed: full frontend tests (16 files / 88 tests), frontend build, `git diff --check`, and source grep checks; browser smoke was SKIP in Canary with Codex screenshots retained as boundary evidence. User reported Canary/Sentinel/Atlas completed. Evidence: `docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md`; screenshots: `output/playwright/prd2-batch27-copy-login.png`, `output/playwright/prd2-batch27-copy-sidebar.png`, `output/playwright/prd2-batch27-copy-logs-detail.png` |
| PRD2 Batch 26 UI/UX Hardening | Canary passed; Sentinel/Atlas completed per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 5/5 for sidebar hierarchy/scroll/collapse indicators/icons, audit table internal scroll, channel test configured-model selection without `gpt-3.5-turbo` fallback, visible PRD2 label localization, and evidence/status handoff. Canary commands passed: CSS/nav grep, audit logs grep, TestPage grep + tests, UI label grep + targeted tests, evidence/status grep, full frontend tests (16 files / 88 tests), frontend build, and `git diff --check`. Evidence: `docs/ops/evidence/PRD2-B26-ui-hardening-local-20260704.md`; User reported Sentinel/Atlas completed |
| PRD2 Batch 25 Final Closeout | Canary passed; Sentinel completed per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 4/4 for P2-F11 policy, matrix final states, open issue boundary discipline, and current-status/completion-plan handoff. Codex local validation passed: P2-F11/index grep, matrix non-final status grep returned no matches, `go test ./...`, `npm --prefix frontend test -- --run`, `npm --prefix frontend run build`, `git diff --check`, and current-code browser smoke via `scripts/prd2-batch23-product-smoke.sh` against backend `http://localhost:18084` and frontend `http://localhost:3000`. Evidence: `docs/ops/evidence/PRD2-B25-final-closeout-local-20260704.md`. User reported Sentinel completed. P2-F10 remains E2E accepted but not Production verified; provider real-upstream, budget hard-block, production HA/DR, and real enterprise IdP evidence remain explicit later/target gaps |
| PRD2 Batch 24 OIDC First SSO | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PASS (6/6): OIDC core service, admin OIDC routes/state/callback/provision/JWT/audit, config/env/compose, frontend login entry, docs/smoke/evidence boundary, and status discipline. Commands passed: `go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute -count=1`, `npm --prefix frontend test -- --run LoginPage client navigation`, `scripts/prd2-batch24-oidc-smoke.sh`, `go test ./...`, `npm --prefix frontend test -- --run`, `npm --prefix frontend run build`, and `git diff --check`. Evidence file: `docs/ops/evidence/PRD2-B24-oidc-local-smoke-20260704.md`. Real enterprise IdP browser login was SKIP because approved issuer/client credentials and target environment were not provided; do not treat this as Production verified |
| PRD2 Batch 23 P2 Product Features | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PASS (6/6): error docs and `doc_url`, Playground, usage analytics, internal status, cache research boundary, smoke/evidence/status discipline. Canary skipped only the real backend/browser smoke because it had no running environment; Codex local current-code smoke passed on 2026-07-04 against backend `http://localhost:18083`, frontend `http://localhost:3000`, local PostgreSQL/Redis, and mock upstream: `status=PASS`, `project_id=35`, `model_alias=batch23-company-chat`, `provider=batch23-mock`, `upstream_calls=1`, positive estimated/upstream split, `internal_status=warning`, screenshot `output/playwright/prd2-batch23-product.png`. Evidence file: `docs/ops/evidence/PRD2-B23-product-local-smoke-20260704.md`. Canary noted one non-blocking future hardening item: add direct `internal/errdocs` unit tests if the registry grows |
| PRD2 Batch 22 License Production Acceptance | Canary/Sentinel completed per User report; Atlas reported no application deployment required; target-environment evidence still required before production acceptance | `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 5/5, with real backend/browser smoke SKIP only because Canary had no running environment. Codex added `scripts/prd2-batch22-license-smoke.sh`, `scripts/prd2_batch22_license_seed.go`, and `scripts/prd2_batch22_license_smoke.mjs`. Local smoke passed on 2026-07-03 with current-code backend `http://localhost:18082`, existing Vite frontend `http://localhost:3000`, local PostgreSQL/Redis, generated test `LICENSE_PUBLIC_KEY`, and guarded `UNIROUTE_BATCH22_ALLOW_LICENSE_RESET=non-production-license-reset`. Result: `status=PASS`, `org_id=41`, `user_id=27`, `active_deployment_id=prd2-b22-active`, `expiring_deployment_id=prd2-b22-expiring`, `expired_deployment_id=prd2-b22-expired`, `warning_event_id=12`, screenshot `output/playwright/prd2-batch22-license.png`; default cleanup cleared the local `license.offline` fixture afterward. Coverage: missing/invalid/active/expiring/expired license statuses, browser `/license` import flow, invalid import rejection, `license.update` audit rows without raw signatures, and `license.warning` notification event. Atlas runtime smoke is optional for docs/script merge, but required if P2-F10 is to be promoted to `Production verified`. Evidence file: `docs/ops/evidence/PRD2-B22-license-local-smoke-20260703.md` |
| PRD2 Batch 21 HA and deployment validation tooling | Canary/Sentinel/Atlas complete per User report; backup/recovery drill PASS | `docs/agent-workflows/VERIFY-RESULT.md` reports Batch 21 PASS 5/5 with Docker runtime SKIP handed to Atlas. User reported Canary, Sentinel, and Atlas completed. Atlas evidence moved to `docs/ops/evidence/PRD2-B21-backup-recovery-drill-20260703.md`: Alibaba Cloud ECS dev/test PostgreSQL 16.14 backup produced a 108KB custom-format dump, `pg_restore -l` listed 243 TOC entries, restore into isolated `uniroute_drill` succeeded, migration/table/core row/FK/index/query checks matched source, drill DB cleanup succeeded, and WAL archiving was enabled and verified with 3 archived WAL files. Remaining HA/DR gaps from the report: backup encryption, NAS sync, cron backup, Feishu failure alerts, PITR recovery, remote restore, application-level restore on drill DB, Patroni/local DB HA, and application DB failover reconnection behavior |
| PRD2 Batch 20 Audit Completion and PRD2 Hardening | Canary passed; Sentinel merged | `docs/agent-workflows/VERIFY-RESULT.md` reports Batch 20 PASS 7/7. `main`/`origin/main` includes PR #53 at `6b1f5cc`. Batch 20 added Audit Logs frontend filters for org/project/actor/action/resource/resource_id/source/time range, audit API filter parameters, channel create/update/delete before/after audit snapshots, `channel.test` success/failure audit without provider key/prompt/response/curl leakage, 000009 historical audit project backfill, 000012 down migration `usage_source` reverse mapping, and gateway audit signal `model.unregistered.call` while keeping unregistered models callable |
| PRD2 Batch 19 Notification/Event and Project Budget completion | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 5/5 with one real-backend smoke SKIP because Canary had no running backend. `main`/`origin/main` includes PR #51 at `5bbbf07`. Codex added Notification Center frontend route `/notifications`, notification API client methods, webhook create/edit UI, background retry loop `startNotificationRetryLoop`, retry aggregation for delivery state, emitters for `api_key.disabled`, `model.deprecated`, `model.retired`, `provider.channel.failure`, and `license.warning`, plus project budget period-boundary integration coverage. Local verification passed: `go test ./internal/billing`, `go test -tags=integration ./internal/billing -run TestEventService_CheckProjectBudgetPeriodBoundaries_Integration -count=1 -v`, `go test ./...`, `npm --prefix frontend test -- --run`, `npm --prefix frontend run build`, `git diff --check`, and `scripts/prd2-batch19-notifications-budget-smoke.sh` against backend `http://localhost:18080`, frontend `http://localhost:3000`, local PostgreSQL/Redis, and local webhook receiver. Smoke result: `status=PASS`, `project_id=34`, `event_id=10`, `webhook_id=5`, `webhook_receipts=1`, `budget_cents=1000`, `spent_cents=1200`, screenshot `output/playwright/prd2-batch19-notifications-budget.png`. Production webhook delivery evidence remains a separate acceptance gap |
| PRD2 Batch 17 Model Governance and Usage/Metering completion | Canary/Sentinel/Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS for full-query reconciliation summary and estimated-ratio notification event, usage-source frontend filters, Developer Console migration recommendation visibility, Batch17 smoke structure, and status/matrix/issues discipline; independent smoke was SKIP only because Canary had no running backend. Sentinel merge is reflected in `main`/`origin/main` commit `f6bafb5` / PR #49, and User reported Atlas completed. Codex local smoke on 2026-07-02 used current-code backend `http://localhost:18080`, existing Vite frontend `http://localhost:3000`, local PostgreSQL/Redis, and Playwright Chromium. Command passed with `status=PASS`, `project_id=27`, `model_set_id=13`, `retired_model=batch17-retired-chat`, `reconciliation_model=batch17-recon-metering`, `estimated_ratio=0.8`, `estimated_ratio_scope=filtered_query`, and screenshot `output/playwright/prd2-batch17-modelgov-usage.png`. The smoke verifies Model Registry create/edit/list, project model-set assignment, approved catalog hides unapproved/retired models, deprecated migration recommendation is visible in developer catalog, retired model gateway call returns 403 `model_retired`, reconciliation summary is full-query despite `page_size=2`, and notification event payload is visible |
| PRD2 Batch 16 Developer Console real-backend browser E2E | Canary/Sentinel/Atlas complete per User report | User reported Canary, Sentinel, and Atlas completed; `main`/`origin/main` includes PR #48 at `6876746`. Codex had added `scripts/prd2-developer-console-smoke.sh`, `scripts/prd2_developer_console_seed.go`, and `scripts/prd2_developer_console_smoke.mjs`. Local smoke on 2026-07-02 used current-code backend `http://localhost:18080`, existing Vite frontend `http://localhost:3000`, PostgreSQL/Redis local services, and a mock OpenAI-compatible upstream; command passed with `status=PASS`, `project_id=4`, `other_project_id=5`, request id `batch16-browser-1782957796339`, and screenshot `output/playwright/prd2-batch16-developer-console.png`. The smoke verifies developer login, project context, approved catalog, project key one-time plaintext, real `/v1/chat/completions`, usage/log evidence, Request Lookup metadata-only detail, cross-project logs 403, direct `/api/v1/channels` 403, and `/channels` route redirect |
| PRD2 full-completion plan | Canary passed; Sentinel merged | `docs/agent-workflows/VERIFY-RESULT.md` reports 4/4 PASS for the docs-only full-completion plan update. User reported Canary and Sentinel completed; git `main`/`origin/main` includes PR #47 at `66bc000`. The plan records P2-F13/P2-F14/P2-F15 mandatory, OIDC-first SSO later, developer self-service key creation by default, alert-only budget first with hard-block tracked later, staging/docker-compose HA first with production HA still incomplete, and OpenRouter Claude only for `openai_compatible` E2E |
| PRD2 Batch 15 License foundation | Canary passed; Sentinel merged; Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports PASS 5/5 and no Batch 16 blocker. Commands passed: targeted backend license/config/admin/route tests, targeted frontend License/API/navigation tests, `go test ./... -count=1`, `npm --prefix frontend test`, `npm --prefix frontend run build`, docs grep, `git diff --check`, and untracked diff check. Sentinel merge is reflected in `main` commit `c997341` / PR #46. User reported Canary, Sentinel, and Atlas completed. Batch 15 added `internal/licensing` Ed25519 verifier, `GET/PUT /api/v1/license`, settings-backed license storage, platform-admin route gates, `license.update` audit summaries without raw signatures, Admin Console `/license` page, frontend navigation/route guard/API client wiring, `LICENSE_PUBLIC_KEY` and `LICENSE_EXPIRY_WARNING_DAYS` config, compose/env/runbook/canary docs, and `docs/tech-ref/16-license-foundation.md` |
| PRD2 Batch 14 API key hash secret and deployment env hardening | Canary passed; Sentinel merged; Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS and no Batch 15 blocker. Sentinel merge is reflected in `main` commit `30e74a3` / PR #45. User reported Canary, Sentinel, and Atlas completed. Batch 14 added `API_KEY_HASH_SECRET` config with fallback to `JWT_SECRET`, wired `admin.NewHandlerWithAPIKeyHashSecret` and `middleware.GatewayAuthWithAPIKeyHashSecret`, updated compose/env/canary docs, and hardened the deployment runbook against `rsync --delete` deleting `.env` |
| PRD2 Batch 13 Anthropic Native Stream Translation | Canary/Sentinel/Atlas complete per User report; PR #44 deployed with env incident | User reported Canary, Sentinel, and Atlas completed and PR #44 deployed; new Anthropic stream conversion is online. Deployment incident: `rsync --delete` deleted production `.env`; `ENCRYPTION_KEY` was recovered, preserving upstream channel keys, but `JWT_SECRET` was regenerated because the old value was unrecoverable, invalidating 31 API Keys and requiring admin re-login. Local `.env.production` backup now exists per User report; Batch 14 adds code/docs hardening for this failure mode |
| PRD2 Batch 12 Anthropic Native Adapter Proof | Canary passed; Sentinel complete; Atlas complete per User report | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS for `anthropic_native` non-stream request/response conversion, adapter-driven failure classification, admin channel create/test support, admin UI adapter configuration, and docs. Latest `main` includes Sentinel merge commit `24e0abf` (`feat: PRD2 Anthropic native adapter — request/response conversion, relay normalization, and admin UI (#43)`). User reported Sentinel and Atlas completed. Browser smoke was skipped by Canary; real Anthropic upstream E2E remains pending configured provider credentials |
| PRD2 Batch 11 Model Governance Admin UI | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 4/4 PASS: org-scoped model-set item read endpoint, frontend API/navigation/routing, admin Model Governance page, and status docs. Commands passed: targeted backend route tests, `go test ./internal/admin ./cmd/uniroute`, `go test ./... -count=1`, `npm --prefix frontend test -- ModelGovernancePage client navigation`, `npm --prefix frontend test`, `npm --prefix frontend run build`, `git diff --check`, and untracked diff check. User reported Sentinel completed. Browser smoke was skipped by Canary and remains a later environment/manual gap |
| PRD2 Batch 10 Developer Console MVP | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS: project-filtered key listing, auth profile refresh, role-aware routing, Developer Console page, API client contracts, and status docs. Commands passed: targeted `internal/admin` tests, `go test ./internal/admin ./cmd/uniroute`, `go test ./... -count=1`, `npm --prefix frontend test`, `npm --prefix frontend run build`, and `git diff --check`. User reported Sentinel completed. Codex Playwright mock-browser smoke also passed for `/login`, unauthenticated `/developer` redirect, and mocked `/developer` desktop/mobile rendering; screenshots are local ignored artifacts under `output/playwright/` |
| PRD2 Batch 9.5 hardening | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 4/4 PASS and all validation commands PASS. `PRD2-ERR-001` can be marked Canary passed; settle retry, cancel-on-failure, and stale frozen cleanup are verified. User reported Sentinel completed |
| PRD2 batch 1-9 full-context dynamic regression sweep | PASS; no Batch 10 blocker | `docs/agent-workflows/VERIFY-RESULT.md` reports `go test ./... -count=1`, PRD2 migration/gateway/billing integration tests, frontend `npm test -- --run`, frontend build, and `git diff --check` all PASS; `docs/issues/PRD2-REGRESSION-SWEEP-2026-07-02.md` reports 12/12 manual mutations covered and one Medium hardening issue `PRD2-ERR-001` |
| PRD2 implementation batch 9 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 8/8 PASS for P0 E2E Acceptance Consolidation, including migrated DB E2E, developer project-key gateway E2E, model governance denial E2E, and route-level RBAC negative E2E; User reported Sentinel completed |
| PRD2 batch 1-9 deep code review | No High/Critical findings; Low follow-ups tracked | `docs/issues/PRD2-DEEP-CODE-REVIEW-2026-07-02.md` reports 7-area review with 4 Low findings: audit historical project backfill, 000012 down migration source rollback, unregistered model visibility, and Channel CRUD audit coverage |
| PRD2 implementation batch 8 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 7/7 PASS for HA Readiness backend/ops foundation, global Go tests PASS, HA integration PASS with nonfatal truncate warnings, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 7 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 7/7 PASS for Notification/Event backend foundation, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 6 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS for Audit Schema Upgrade, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 5 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 4 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 3 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 2 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 implementation batch 1 | Canary passed; Sentinel complete | `docs/agent-workflows/VERIFY-RESULT.md` reports 5/5 PASS, global Go tests PASS, no new issues; User reported Sentinel completed |
| PRD2 planning docs round 1 | Canary passed; Sentinel CI green | `docs/agent-workflows/VERIFY-RESULT.md` reports 6/6 PASS, no new issues, no code changes; User reported Sentinel confirmed PRD2 planning documentation round 1 complete and CI passed on the first run |
| Go tests | PASS | `go test ./...` passed on 2026-07-01 |
| Frontend tests | PASS | `npm test -- --run` passed in `frontend/` with 43 tests on 2026-07-01; Canary verified earlier rounds as PASS |
| Frontend build | PASS with warning | `npm run build` passed; Canary verified PASS; Vite reported large chunk warning |
| Route parity guard | PASS | `go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v` passed on 2026-07-01 |
| Local current-source smoke | PASS | Temporary `go run ./cmd/uniroute` on port 18080 passed health, metrics, login, `/me`, settings, dashboard, and unauthenticated password-route checks |
| PRD1 latest Canary | PASS | `VERIFY-RESULT.md` reports 4/4 PASS for the latest PRD1 reference-inspired UI redesign; earlier closeout rounds also passed through the protocol, while external-only items remain in `PRD1-REMAINING-ACCEPTANCE-PACK.md` |
| PRD1 latest follow-up fixes | Canary passed and production verified | `VERIFY-RESULT.md` reports 3/3 PASS for channel-key edit, Token reconciliation pagination, and Dashboard trend fixes; cloud follow-up verified the channel selector expands, reconciliation `page_size=20` returns 20 rows, and 24h/7d/30d Dashboard totals match trend sums |
| PRD1 latency/decimal pricing fixes | Production verified | After redeploy, temporary request `codex-latency-smoke-1782874625` logged `latency_ms=1290`; Dashboard 24h average became `11.4159ms`; re-saving `mimo-v2.5` preserved `input_price_cents=0.2/output_price_cents=0.1` |
| PRD1 Dashboard/Grafana follow-up fixes | Production verified with operator evidence | Dashboard trend UX passed Canary and local Playwright visual smoke. User/Atlas reported Grafana runtime fixed; Codex verified Grafana health, Prometheus query success, and `admin/admin` rejection with HTTP 401 |
| PRD1 Logs/Dashboard UI follow-up fixes | Local verified; target browser verification pending | Local source now renders call-log timestamps as absolute time, adds Request ID copy fallback/toast, reorders Provider before Model, improves request detail layout, caps model distribution to Top 6, and clarifies channel health provider labels. Targeted frontend tests, full frontend tests, build, `git diff --check`, and Codex Playwright smoke passed locally |
| PRD1 Dashboard percentile/layout closeout | Local verified; target browser verification pending | Local source adds Dashboard API `p95_latency_ms/p99_latency_ms`, changes the main latency KPI to P95 with Avg/P99 context, aligns topbar/content at 1440px, replaces the loose hero with a compact status strip, pairs trend with a latency percentile panel, and adds channel-health status icons/progress. Admin targeted Go tests, Dashboard targeted frontend tests, full frontend tests, and build passed locally |
| Browser/manual frontend validation | Codex visual QA PASS locally | Codex Playwright screenshots captured Login, Dashboard, Logs detail, Wallet, API Docs, Channels, mobile Dashboard, mobile Logs, and the reference-inspired Login/Dashboard/mobile pass under local `output/playwright/`; Canary/OpenClaw visual acceptance remains not applicable |
| Real upstream provider E2E | E2E accepted for Moonshot and Mimo single calls | `moonshot-v1-8k` non-stream and stream calls reached a real provider and settled; after the user updated the key, channel 8 `mimo-v2.5` test returned HTTP 200 and a UniRouter gateway call returned HTTP 200 with `_uniroute.channel=Mimo Token Plan` |
| Production deployment status | PASS for S0/S1 smoke | `scripts/prd1-cloud-smoke.sh` passed against Alibaba Cloud on 2026-07-01: frontend, live/ready, metrics, docs, Prometheus, Grafana login, unauthenticated 401 checks, and default-admin rejection |
| Default admin password protection | Production verified | Cloud `POST /api/v1/auth/login` with `admin/admin123456` returned 403 on 2026-07-01; script check `default-admin-password-rejected` passed |
| `/health/ready` PRD response shape | Target verified | Historical direct `:8080` evidence returned the accepted JSON shape. Batch 1Z now blocks public `:8080`; readiness still returns `{"status":"ok","db":"ok","redis":"ok"}` internally and public HTTP health is reached through the intended Nginx entrypoint |

## Phase 1 Delivery State

| PRD Area | Current State | Notes |
|----------|---------------|-------|
| F01 OpenAI-compatible gateway | Implemented, cloud-accepted for sampled providers | Non-stream and stream real provider paths work for `moonshot-v1-8k`; after key update, `mimo-v2.5` works through `Mimo Token Plan` for a sampled non-stream call |
| F02 Channel management | Implemented | Admin API and frontend page exist |
| F03 API key management | Implemented | Includes allowed models and disable path |
| F04 Gateway auth middleware | Implemented | Tests pass |
| F05 Pre-freeze billing | Implemented | Needs current E2E balance and concurrency acceptance evidence |
| F06 Call logs | Implemented; latest UI follow-up Local verified with target browser verification pending | Logs page and admin APIs exist; local follow-up now uses absolute timestamps, Request ID copy feedback/fallback, and a structured detail inspector |
| F07 Admin API | Implemented | Broad handler coverage exists |
| F08 Wallet management | Implemented | Top-up, adjustment, transactions exist |
| F09 Model pricing | Production verified for decimal cents | Base and org pricing exist; cloud API preserved `mimo-v2.5` decimal-cent prices `0.2/0.1` after redeploy |
| F10 Admin frontend | Implemented; latest UI follow-up Local verified with target browser verification pending | Build, component tests, historical Canary source verification, and Codex local visual screenshot pass are complete; latest Logs/Dashboard UI fixes and Dashboard percentile/layout closeout passed local tests/build; user/cloud browser visual acceptance still gates strict close |
| F11 Rate limit and admission control | Implemented | Needs current concurrency acceptance evidence |
| F12 Failover and circuit breaker | Implemented | Needs current failover/circuit acceptance evidence |
| F13 Sensitive-word filter | Implemented | Needs current E2E request rejection evidence |
| F14 Idempotency | Implemented | Needs current repeated request acceptance evidence |
| F15 Token estimation | Implemented | Tests pass |
| F16 Budget alert | Implemented | Needs current webhook acceptance evidence |
| F17 System settings | Implemented | Settings page/API exist |
| F18 Health checks | Production verified for PRD shape | `/health/live`, `/health/ready`, `/metrics` exist; cloud `/health/ready` returned `{"status":"ok","db":"ok","redis":"ok"}` after redeploy |
| Observability dashboards | Production verified with operator evidence | Prometheus target and UniRouter metrics are present in cloud; Grafana dashboard config pins datasource UID `prometheus` and uses aggregated PromQL; user/Atlas reported the runtime Grafana issue resolved, and Codex verified Grafana health plus default credential rejection |

## Known Gaps

1. PRD acceptance checklist is partially reconciled with current evidence; cloud API smoke plus Moonshot and Mimo real-provider E2E are recorded, while capacity load and failover scenarios still need closure or explicit deferral.
2. Some historical status files disagree about whether specific bugs are open or fixed.
3. `docs/testing/archive/` contains useful history but should not be used as the current status source.
4. Active issues are now centralized in `docs/issues/OPEN-ISSUES.md`; older issue status still needs item-by-item reconciliation.
5. Frontend API/client route contract needs continued explicit verification; this round fixed confirmed `KeysPage`, `WalletPage`, and `/me/password` route drift, added `ApiError`, added a route parity guard, and Canary passed the remaining closeout verification.
6. Historical docs mention XOR channel-key encryption, but current code uses AES-GCM helpers in `internal/auth/auth.go`; `docs/ops/test-report.md` now marks the XOR note as stale, and Canary passed the correction.
7. UI quality, frontend architecture, component reuse, and browser/manual validation are explicit PRD1 closeout items, not deferred wholesale to PRD2. Current source audit is recorded in `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md`; PRD1 UI redesign execution is recorded in `docs/design/PRD1-UI-REDESIGN-SPEC.md`; Codex completed a local Playwright visual screenshot pass and a reference-inspired shell/login/Dashboard pass, while Canary remains responsible only for non-visual source/build/test verification. User/cloud visual acceptance remains the final visual gate.
8. Cloud smoke found API list handlers could hide row scan errors and emit zero-value fields when nullable database columns were scanned into non-nullable Go types. Codex fixed `ListAPIKeys` and `ListLogs`; after deployment, authenticated cloud reads for `/api/v1/keys` and `/api/v1/logs` returned 200 on 2026-07-01.
9. User-reported PRD1 UI findings on logs filters, Dashboard trend/health cards, wallet edit actions, audit login records, `/docs`, and overall visual quality were classified in `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md`; concrete bugs and the PRD1 visual redesign passed Canary non-visual verification.
10. Alibaba Cloud strict S0/S1 closeout smoke passed on 2026-07-01 after redeploy. The former default-admin blocker is fixed in cloud, and `/health/ready` now exposes the PRD DB/Redis shape. Remaining PRD1 closure depends on acceptance evidence or explicit deferral for the still-open matrix items.
11. Cloud Grafana runtime follow-up was reported resolved by user/Atlas. Codex verified Prometheus query success, Grafana health, and rejection of Grafana `admin/admin`; authenticated datasource UID inspection now requires the rotated Grafana credential.
12. User-reported Logs/Dashboard UI follow-up is fixed locally and tracked in `docs/issues/OPEN-ISSUES.md` plus `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md`: absolute log timestamps, Request ID copy feedback/fallback, log detail inspector, Provider-before-Model filters, and Dashboard model/channel health layout. Canary must verify non-visual source/build/tests before deployment.
13. Latest Dashboard percentile/layout closeout is locally verified in the historical PRD1 follow-up set: Dashboard API P95/P99 latency, P95 main KPI, aligned 1440px content/topbar, compact status strip, trend/latency split, and channel-health icons/progress. Current execution state is in `docs/agent-workflows/EXECUTION-RECORD.md`; `FIX-SIGNAL.md` is only a protocol v1.x compatibility entry.

## Active Sources Of Truth

| Need | Use This File |
|------|---------------|
| PRD1 product requirements | `docs/PRD-Phase1.md` |
| PRD2 discovery and decisions | `docs/planning/PRD2-DISCOVERY.md` |
| PRD2 product requirements draft | `docs/PRD-Phase2.md` |
| PRD2 acceptance planning | `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` |
| PRD2 provider adapter spec | `docs/tech-ref/10-provider-adapter-framework.md` |
| PRD2 Project/RBAC spec | `docs/tech-ref/11-project-rbac-model.md` |
| PRD2 Developer Console spec | `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md` |
| PRD2 prompt/response storage policy | `docs/tech-ref/19-prompt-response-storage-policy.md` |
| PRD3 consistency audit | `docs/planning/PRD3-CONSISTENCY-AUDIT.md` |
| PRD3 product requirements | `docs/PRD-Phase3.md` |
| PRD4 completed scope | `docs/PRD-Phase4.md` |
| PRD4 accepted/deferred matrix | `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` |
| PRD6 decision state | `docs/planning/PRD6-START-HERE.md` |
| PRD6 proposed scope | `docs/PRD-Phase6.md` |
| PRD6 acceptance truth | `docs/ops/PRD6-ACCEPTANCE-MATRIX.md` |
| PRD6 implementation order | `docs/planning/PRD6-IMPLEMENTATION-PLAN.md` |
| PRD6 competitor/Four-Quadrant audit | `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md` |
| PRD5 completed scope | `docs/PRD-Phase5.md` |
| PRD5 closeout matrix | `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` |
| PRD5 implementation plan | `docs/planning/PRD5-IMPLEMENTATION-PLAN.md` |
| PRD5 competitor comparison | `docs/planning/PRD5-COMPETITOR-COMPARISON.md` |
| PRD5 deferred backlog | `docs/planning/PRD5-BACKLOG.md` |
| PRD4 candidate backlog source record | `docs/planning/PRD4-BACKLOG.md` |
| PRD3 acceptance planning | `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` |
| Agent handoff | `docs/PROJECT-HANDOFF.md` |
| Codebase map | `docs/CODEBASE-MAP.md` |
| Documentation inventory | `docs/DOC-INVENTORY.md` |
| Current project status | `docs/ops/CURRENT-STATUS.md` |
| PRD1 closeout plan | `docs/ops/PRD1-CLOSEOUT-PLAN.md` |
| PRD1 strict closeout gate | `docs/ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md` |
| PRD1 acceptance test ownership | `docs/ops/PRD1-ACCEPTANCE-TEST-OWNERSHIP.md` |
| PRD1 UI redesign execution | `docs/design/PRD1-UI-REDESIGN-SPEC.md` |
| Remaining PRD1 acceptance pack | `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md` |
| PRD acceptance progress | `docs/ops/PRD-ACCEPTANCE-MATRIX.md` |
| Active unresolved issues | `docs/issues/OPEN-ISSUES.md` |
| Agent collaboration protocol | `docs/agent-workflows/AGENT-PROTOCOL.md` |
| Current execution-owner record | `docs/agent-workflows/EXECUTION-RECORD.md` |
| Optional independent review records | `docs/agent-workflows/reviews/` |
| Protocol v1.x compatibility/history | `docs/agent-workflows/FIX-SIGNAL.md`, `docs/agent-workflows/VERIFY-RESULT.md` |
