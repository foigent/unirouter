# Current Execution Record

**Batch:** PRD6 Batch 1 P6-F07 Distributed Limit Policy
**Owner:** Codex
**Risk:** R4 distributed admission, tenant control and migration
**Status:** CI verified / closed
**Integration:** PR #168 exact head `f52a151` / run `29173848811`; squash merge `2610e317`; post-merge main run `29174001859`; historical branch restored; no deployment, target or production claim
**Previous batch:** P6-F19 CI verified / closed by PR #164/#166 and post-merge main runs `29170507588`/`29171807693`
**Previous phase:** PRD5 Accepted Scope Complete; target backend evidence remains `95d1da9` / PR #133

## Current Scope

1. Enforce exact shared API-key RPS/RPM and every matching global,
   organization, project, API-key, model and server-owned label policy.
2. Reserve RPS/RPM/TPM/concurrency before wallet/upstream work, then replace
   TPM estimates with actual terminal usage and release safe cancellations.
3. Provide disabled/shadow/enforce, fail-closed and bounded last-known-safe
   semantics with Redis time, cluster-safe keys and bounded local state.
4. Add tenant-scoped optimistic control APIs, non-technical UI, version/header/
   trace evidence and low-cardinality metrics without raw payload persistence.
5. Prove multi-instance exact concurrency, authoritative labels, TPM terminal
   behavior and guarded down→up rollback with real Redis 7/PostgreSQL in CI.
6. Keep P6-F20 durable cross-process terminal hooks, P6-F21 weighted fairness,
   D6-10 long-stream extension, deployment and target capacity out of closure.

## Current P6-F07 Behavior Change

- Chat and Embeddings count every authenticated call against a shared API-key
  rate window before more-specific policy/capacity denial can be amplified;
- all matching selectors tighten monotonically, with exact rolling RPM/RPS,
  reserved-then-actual TPM and expiring shared concurrency across replicas;
- server-owned API-key labels cannot be injected by a request or Agent claim;
  stale snapshots and overlong policy chains fail closed;
- shadow decisions, degraded last-known-safe admissions and enforced denials
  emit distinct sanitized evidence under one policy-set fingerprint;
- platform/org administrators mutate their allowed scope with optimistic
  versions; billing administrators read only; built-in 100 global/10 org
  concurrency ceilings are visible rather than hidden;
- migration down succeeds only before configuration exists and otherwise
  refuses destructive policy/label loss; subsequent roll-forward is tested;
- PR #168 exact-head and post-merge Backend/Frontend/Docker CI passed,
  including the new real policy and rollback gates; and
- evidence:
  `docs/ops/evidence/PRD6-B1-P6F07-distributed-limit-policy-local-20260712.md`.

## Previous P6-F19 Behavior Change

- children never receive or copy the root API key; explicit exchange/delegation
  creates signed workflow, lineage and Agent identity with a maximum five-
  minute token and minute-three jittered refresh;
- atomic topology registration enforces ancestor depth/fan-out and exactly 100
  of 101 concurrent children under the accepted fixture;
- shared Redis and a bounded root key/org/project/owner/membership authority
  cache reject revoked, superseded, changed or unverifiable authority;
- every capability-authenticated Chat/Embeddings call reserves cost, tokens,
  rolling rate and concurrency across its complete signed lineage before route
  selection or upstream dispatch;
- wallet Freeze denial or runtime-state uncertainty never dispatches; normal
  settlement writes actual cost/tokens and cancellation releases only budget
  reservations/concurrency, never RPM/RPS attempts;
- 20 concurrent admissions across two service instances under a limit of three
  admit exactly three; Redis time survives a two-hour application-clock skew;
- PR #164 and #166 exact-head plus post-merge Backend/Frontend/Docker CI passed,
  including migrated PostgreSQL and Redis 7 authority/runtime integration;
- P6-F19 is CI verified / closed. P6-F20 and P6-F21 remain separate; P6-F07
  is independently closed by PR #168;
- evidence:
  `docs/ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md`
  and
  `docs/ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md`.

## Previous P6-STREAM-002A Behavior Change

- `stream-transport.v1` validates a 2-MiB maximum SSE line, 30-second
  write-idle deadline, 15-second heartbeat and 30-minute maximum duration;
- the metrics wrapper exposes `Unwrap`, per-write deadline renewal reaches the
  native server connection, and one writer serializes Write+Flush;
- large/over-limit/read-failure fixtures prove bounded frames, partial-output
  settlement and no fabricated success/`[DONE]`;
- PR #162 exact-head and post-merge main Backend/Frontend/Docker CI passed;
- D6-10 dynamic reservation extension and P6-F11 semantic interception remain;
- evidence: `docs/ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md`.

## Previous P6-F06 Behavior Change

- P6-F18A already reads and applies the organization multiplier before Freeze,
  persists the effective prices/multiplier/version, and settles from that
  immutable snapshot; no billing formula rewrite is needed;
- a 19-cent wallet facing a 20-cent effective hold returns
  `insufficient_balance` with unchanged cash/frozen state and no reservation or
  freeze transaction;
- an exact 20-cent wallet facing 32 concurrent identical Settle calls returns
  one durable receipt to every caller, charges 20 once and records
  `uncovered_cost=0`;
- duplicate cancellation restores the complete 20-cent adjusted hold once and
  creates exactly one terminal cancel event;
- the focused migrated PostgreSQL race matrix passed ten repetitions; full Go
  vet and repository race tests pass;
- required Backend now runs `TestP6F06` with
  `REQUIRE_TEST_DATABASE=1`, so PostgreSQL absence cannot become a skipped green;
- PR #160 exact-head Backend/Frontend/Docker required CI passed, the change was
  squash-merged as `15ea8a2`, and post-merge main CI passed the same jobs;
- evidence: `docs/ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md`.

## Previous P6-F05 Behavior Change

- `effective-config.v1` is the typed read-only registry; its
  `routing-config.v1` values are the immutable startup driver for provider retry
  budget, channel threshold/recovery/probe, rate/auth cooldown and key failure
  threshold;
- existing environment names now drive runtime behavior, two previously hidden
  key controls are declared, and invalid bounds fail startup validation;
- retry budgets 0/1/2 prove exact provider dispatch counts 1/2/3;
- route traces and runtime API/UI expose the same safe version and scalar values;
- `project-budget-alert.v1` declares project budget/period sources,
  tenant-scoped event/delivery ledgers and the post-settlement trigger; the
  unsafe global worker/first-wallet/audit path is removed, while historical
  global keys remain stored but are marked retired/ineffective and reject writes;
- isolated PostgreSQL evidence proves threshold transition, real recovery
  deadline exclusion/admission, success reset, configured cooldowns and key
  invalidation; a second real PostgreSQL matrix proves foreign project denial
  and exact second-tenant budget-event scope; both are fail-closed Backend steps;
- focused race matrices pass ten repetitions; full Go race/vet, admin and
  routing/budget integration, OpenAPI parse, frontend 27 files / 137 tests,
  type-check and production build pass locally;
- PR #158 exact-head Backend/Frontend/Docker required CI passed, including both
  P6-F05 PostgreSQL gates; post-merge main CI passed the same jobs;
- evidence: `docs/ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md`.

## Previous P6-F04 Behavior Change

- Embeddings uses bounded RouteAll attempts, safe shared HTTP/transport
  classification, one terminal billing action and truthful final/fallback
  attempt evidence; malformed 200 responses never settle or panic;
- PR #157 required run `29158981445`, squash merge
  `36f736c0ca02f483b408c94350c8e5d1497b759e` and post-merge main run
  `29159155254` passed all three jobs;
- evidence: `docs/ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md`.

## Previous P6-F03 Behavior Change

- shared adapters classify safe terminal versus transient HTTP/transport
  failures; Chat consumes only retryable candidates and preserves safe client,
  notification, trace and billing semantics;
- policy rejection remains before Freeze/routing and every terminal attempt has
  truthful route/upstream identity;
- PR #156 required run `29158184240`, squash merge
  `f6f075aa4333f2b81f888f3505c2c2b44e77c449` and post-merge main run
  `29158395734` passed all three jobs;
- evidence: `docs/ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md`.

## Previous P6-TEST-DB-001 Behavior Change

- pre-change `go test -tags=integration ./internal/admin -count=1` failed twelve
  tests because the shared `uniroute_test` schema lacked current tables/columns;
- all 48 legacy admin setups now use isolated migrated databases and required
  CI fails rather than skips when PostgreSQL is unavailable;
- frontend 27 files / 137 tests, type-check and production build pass;
- PR #155 required run `29157383889`, squash merge `4b18ace92df49ad74048acc7197992990f14ca8e`
  and post-merge main run `29157561036` passed all three required jobs;
- evidence: `docs/ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md`.

## Previous P6-F17 Behavior Change

- response `X-Request-ID` is a cryptographically random `op_` server operation;
  caller values are retained only as `X-UniRouter-Client-Request-ID`;
- `Idempotency-Key` uses HMAC-only storage and a canonical request fingerprint;
  same completed/failed/in-progress operations return a metadata `409`, while a
  changed payload returns a distinct conflict and never dispatches;
- migration `000021` adds the metadata receipt, tenant-scoped financial/grant
  uniqueness and separate call-log/trace/audit identity fields;
- route attempts receive distinct `att_` identities; successful responses and
  failure logs report truthful fallback/attempt state; a mixed HTTP/network
  failure regression proves no prior status/upstream ID leaks into the final
  attempt record;
- concurrent top-up/grant calls with the same tenant/client correlation produce
  one financial effect and a deterministic duplicate receipt, while keeping
  separate server operation IDs;
- Logs and Audit expose/filter operation, client, trace, workflow, Agent,
  attempt and upstream identifiers without widening tenant/project access;
- full Go normal/race/vet, migrated integration, frontend 27-file/136-test,
  type-check/build, OpenAPI parse and diff checks pass; PR and post-merge main
  required CI also pass;
- activation must drain old writers before applying `000021`; the safe partial
  down is not an old-binary rollback and no target claim exists.

## Previous P6-F36 Behavior Change

- tenant registry, alias and model-set list/create operations derive scope from
  the signed identity, reject foreign/zero selection and remove organization-1
  fallback;
- global-ID mutations use both the resolved organization and row ID in final
  SQL; implicit foreign/missing IDs are non-enumerating `404`, while explicit
  foreign organization selection is `403`;
- project catalog uses the authorized project's organization and ignores
  malformed cross-organization assignments in catalog/effective-set queries;
- model-set creation and item replacement are transactional, organization
  scoped and audit-backed; an injected audit failure restores the complete
  prior policy;
- existing live model admission, unregistered-model compatibility and
  `/v1/models` semantics remain unchanged;
- full Go normal/race/vet, frontend 27-file/136-test/type-check/build, OpenAPI,
  diff, migrated signed-JWT/PostgreSQL matrix and audit-trigger rollback checks
  pass; independent review found no remaining blocker/high/medium/functional-low
  finding after both Low observations and the `org_viewer` expectation were
  corrected and targeted tests passed;
- PR #148 final required run `29140739477` passed; it was squash-merged as
  `d83dbbc0ac7137bb44d2346adc5a25debde804e1`, and post-merge main run
  `29140850345` passed all required jobs.

## Previous P6-F35 Behavior Change

- all eight production global-control routes and their handlers require platform
  authority and fail closed without it;
- generic settings cannot write `license.*` or `sensitive_words`, and generic
  reads redact license, words and webhook destinations;
- settings, license and word mutations use an advisory lock plus same-transaction
  sanitized audit; injected audit failure rolls the business write back;
- sensitive-word startup is synchronous/fail-closed, versions are strictly
  monotonic, and stale/equal reloads cannot replace a newer filter;
- two independent handlers sharing PostgreSQL preserve the concurrent word union
  and audit/version chain; separately controlled reload makes both filters
  converge;
- local full/race/vet, frontend 27-file/135-test/type-check/build, OpenAPI parse,
  migrated signed-JWT/PostgreSQL and failure-injection checks pass; independent
  review reports no remaining blocker/high/medium;
- PR #147 final head `fb77083d0f04f5cdaa20f38e763c93b51b89f4d1`
  passed all required jobs in run `29139408226`, was squash-merged as
  `c08db96167837ade685708c2ab204861a8ecc0ca`, and post-merge main run
  `29139534691` passed all required jobs; its historical branch is restored and
  retained.

## Previous P6-F34 Behavior Change

- all six pricing mutations are platform-only and fail closed without a role;
- organization reads default to the JWT organization; foreign and org-zero
  tenant reads are denied;
- writes and audit rows share one transaction; same-key upserts/renames use a
  transaction advisory lock for accurate before-state;
- multiplier values are limited to `0.001..99.999`;
- the UI uses the multiplier contract, removes organization-1 fallback and
  hides mutations from tenant roles;
- PR #146 final-head run `29137670055` passed all three required jobs, PR #146
  squash-merged as `517ce45381429781f7e81e492bdd9f38093c21ba`, and post-merge
  main run `29137781625` passed the same jobs; its historical branch is retained.

## Previous P6-F32/P6-F39 Behavior Change

- one organization resolver binds non-platform actors to the organization in
  their JWT, rejects foreign selection and has no database-order fallback;
- wallet, top-up, adjustment, transactions, export, grants and reconciliation
  use that organization in their final SQL and derived event input;
- request-log list/detail/trace, analytics, internal status, dashboard activity
  and runtime traffic are tenant scoped; foreign/missing log IDs both return
  `404`;
- platform administrators default to their own JWT organization and cross
  tenants only through an explicit target;
- CSV escaping/formula-prefix handling and billing-admin UI capability flags
  are locally verified;
- global grant-key uniqueness remained P6-F17 in that historical branch and is
  now closed by PR #149; wallet pool/grant semantics remain P6-F18, and
  project-dashboard granularity remains a separate permission item.

## Current P6-F18 Billing Reservation And Stream Recovery

- Retained branch `codex/prd6-p6f18a-billing-invariants` removes Freeze pool
  re-entry, snapshots the effective price/multiplier version, preserves grant
  and cash source allocations, fixes grant issuance/expiry accounting and
  enforces one terminal ledger event.
- Local verification passed full backend, vet, race, migration up/down, 100-way
  terminal competition, full 137-test frontend suite, type-check and build.
- PR #151 head `84f3c4b` passed required run `29154664491`, was squash-merged as
  `7db1efc`, and post-merge main run `29154820102` passed all required jobs.
- F18B persists `settlement_pending` before the first billable stream event,
  persists immutable final usage, rejects post-output cancellation and starts a
  leased/backoff recovery loop in every replica.
- Output-marker failure withholds model content and safely cancels pre-output;
  final-usage failure never refunds and omits a false `[DONE]` terminal.
- Simulated post-chunk DB loss plus a new Billing instance and 20 concurrent
  workers produces one settlement; failed recovery releases its lease, backs
  off and later settles. Payload-missing rows remain held without guessed
  charge/refund.
- PR #153 final head `0ca2bbb` passed required run `29155948787`, was
  squash-merged as `b30bc9d`, and post-merge main run `29156096777` passed all
  required jobs. P6-F18 is CI verified / closed. P6-F20 remains open and no
  target or production claim is made.

## Previous P6-F37 Behavior Change

- Non-platform audit reads always include the signed actor organization in
  count and row queries.
- A caller-supplied foreign `org_id` cannot replace that constraint; a foreign
  or nonexistent project produces the same `403` denial.
- Organization `0` fails closed and platform administrators retain deliberate
  cross-tenant query authority.
- UI/architecture/upgrade findings are documented and classified; this branch
  does not imply that the still-open wallet, reconciliation/usage, pricing,
  global-control or model-governance surfaces are remediated.

## Previous P6-F33 Behavior Change

- Non-platform notification list/read conditions are constrained to the signed
  actor organization even when a foreign `org_id` is supplied.
- Webhook create/update validates actor org and project/org ownership; update's
  final SQL retains the authorized org predicate.
- Delivery display joins cannot resolve a cross-org Webhook name.
- Migration `000020` marks existing routes unverified without deleting them;
  both new delivery and retry require explicit verification. New authorized
  creates are verified, while old routes use an audited confirmation action and
  visible Notification Center state.
- New-code edits re-quarantine a route; a monotonic database write generation
  rejects old-binary create/update. Confirmation uses an expected update time,
  row lock and atomic audit/state commit.
- Pre-up failed deliveries retain their old due time only as evidence and never
  auto-replay after route verification. Down disables all routes, leaves a
  database lockdown, removes the migration ledger row and supports safe up
  reconstruction.
- Foreign and missing Webhook IDs share the same non-platform `404` result.
- Same-tenant and explicit platform-admin behavior remains available.
- No new X-Insight runtime field or dependency is introduced.
- D6-08 accepts P6-F32-P6-F36. Only P6-F33 changed runtime behavior in that
  historical branch; the other rows were unremediated there and no target verification was
  claimed.

## Files And Evidence

- Baseline evidence:
  `docs/ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md`.
- Product/competitor evidence:
  `docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md`.
- Scalability/anti-pattern evidence:
  `docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md`.
- P6-F16 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md`.
- Follow-up administrator surface audit:
  `docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md`.
- P6-F33 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md`.
- P6-F37 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md`.
- P6-F38 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md`.
- P6-F32/P6-F39 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md`.
- P6-F34 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F34-pricing-authority-local-20260711.md`.
- P6-F35 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F35-global-control-authority-local-20260711.md`.
- P6-F36 local evidence:
  `docs/ops/evidence/PRD6-B0B-P6F36-model-governance-tenant-isolation-local-20260711.md`.
- P6-F17 local and CI evidence:
  `docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md`.
- Product/architecture/UX audit:
  `docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md`.
- PRD7 candidate queue:
  `docs/planning/PRD7-CANDIDATE-BACKLOG.md`.
- Future required Cache Service architecture:
  `docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md`.
- Adaptive-trust innovation evidence:
  `docs/planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md`.
- Structural hardening portfolio:
  `docs/planning/prd6-adaptive-trust-hardening/hardening.md`.
- Truth chain: `docs/planning/PRD6-START-HERE.md`, `docs/PRD-Phase6.md`,
  `docs/ops/PRD6-ACCEPTANCE-MATRIX.md`,
  `docs/planning/PRD6-IMPLEMENTATION-PLAN.md`, and
  `docs/planning/PRD6-BACKLOG.md`.
- Current code/tests: server/client request identity, HMAC metadata idempotency,
  tenant-scoped financial and administrator correlation, workflow/Agent/attempt
  evidence, Logs/Audit UI/API readback, migration, replay/concurrency and
  migrated PostgreSQL gateway/finance matrices.

## Verification

Current P6-F17 results on `codex/prd6-p6f17-idempotency`, based on
`d83dbbc0ac7137bb44d2346adc5a25debde804e1`:

- 100 child operations share one trace/workflow with distinct server/Agent
  identity; 100 real reservations settle once each and reject a duplicate:
  PASS;
- same-payload metadata replay, changed-payload 409, in-progress overlap,
  100-way PostgreSQL claim, cross-org collision and expired replacement: PASS;
- migration `000021` safe up/down/roll-forward and cross-tenant grant/billing
  uniqueness: PASS;
- real PostgreSQL Audit operation/client filtering and concurrent top-up/grant
  one-effect duplicate handling: PASS;
- migrated gateway E2E performs one provider call and returns no stored answer
  on replay; migrated finance/usage tenant matrix: PASS;
- `go test ./... -count=1`, `go test -race ./... -count=1` and `go vet ./...`:
  PASS; race linking retains the existing non-failing macOS warning;
- frontend 27 files / 136 tests, type-check and production build: PASS;
- OpenAPI YAML parse and `git diff --check`: PASS;
- PR #149 required run `29152968280`, squash merge
  `c9402fad323e8656493ff1bb29367a5dcf118331`, and post-merge main run
  `29153106336`: PASS.

Previous P6-F36 results on `codex/prd6-p6f36-modelgov-tenant`, based on
`c08db96167837ade685708c2ab204861a8ecc0ca`:

- migrated signed-JWT production-wrapper matrix across all eleven registry,
  catalog, alias and model-set methods: PASS;
- actor-organization query/body/global-ID negatives, final SQL predicates,
  project-canonical catalog scope, platform exception and live-admission
  non-regression: PASS;
- real PostgreSQL audit-trigger rollback for model-set create and item replace:
  PASS;
- `go test ./... -count=1`, `go test -race ./... -count=1` and `go vet ./...`:
  PASS; race linking retains the existing non-failing macOS warning;
- frontend type-check, 27 files / 136 tests and production build: PASS; OpenAPI
  YAML parse and `git diff --check`: PASS;
- independent review: COMPLETE, with no remaining
  blocker/high/medium/functional-low finding; both Low observations were fixed
  and targeted tests passed;
- PR #148 implementation-head run `29140520863`, final-head run `29140739477`,
  squash merge `d83dbbc` and post-merge main run `29140850345`: PASS.

Previous P6-F35 results on `codex/prd6-p6f35-global-controls`:

- migrated signed-JWT production-route/handler role matrix, reserved-key bypass
  denial, redaction and denied-no-side-effect tests: PASS;
- real PostgreSQL audit-failure rollback across settings/license/words and two
  independent-handler concurrent version/audit/filter convergence: PASS;
- startup missing/invalid policy fail-closed, stale/equal reload rejection and
  post-commit event/filter ordering tests: PASS;
- `go test ./... -count=1`, `go test -race ./... -count=1` and `go vet ./...`:
  PASS; race linking retains the non-failing macOS warning;
- frontend type-check, 27 files / 135 tests and production build: PASS; OpenAPI
  YAML parse and route-role contract checks: PASS;
- independent review: no remaining blocker/high/medium;
- PR #147 final head `fb77083d0f04f5cdaa20f38e763c93b51b89f4d1`,
  run `29139408226`: Backend Tests PASS in 1m9s, Frontend Build PASS in 3m14s
  with 27 files / 135 tests, and Docker Build PASS in 1m4s;
- PR #147 squash-merged as `c08db96167837ade685708c2ab204861a8ecc0ca`;
  post-merge main run `29139534691` passed Backend Tests in 1m38s, Frontend
  Build in 3m10s and Docker Build in 1m1s; historical branch restored and
  retained.

Previous P6-F34 results on `codex/prd6-p6f34-price-authority`:

- migrated two-organization signed-JWT route-role, read-scope, denied
  no-side-effect, billing-math and audit matrix: PASS;
- same-key global/organization concurrent upsert before-state tests and audit
  failure rollback: PASS;
- `go test ./...`, `go test -race ./...` and `go vet ./...`: PASS;
- frontend type-check, 27 files / 135 tests and production build: PASS;
- independent review: no remaining blocker/high/medium;
- PR #146 implementation head `0e15dbb`, run `29137361064`, and final branch
  head run `29137670055`: `Backend Tests`, `Frontend Build` and `Docker Build`
  PASS; PR #146 squash-merged as `517ce45381429781f7e81e492bdd9f38093c21ba`;
  post-merge main run `29137781625` passed the same three jobs; historical
  branch retained.

Previous P6-F32/P6-F39 results on `codex/prd6-p6f32-finance-tenant`:

- migrated two-organization signed-JWT finance/usage matrix: PASS;
- focused legacy `-tags=integration` wallet/grant handler cases: PASS after
  supplying their previously absent authenticated actor context;
- `go test ./internal/admin`, `go test ./...`, `go test -race ./...` and
  `go vet ./...`: PASS; race linking retains the non-failing macOS warning;
- frontend type-check, 26 files / 128 tests, and production build: PASS; the
  existing large-chunk warning remains;
- `git diff --check`: PASS;
- PR #145 implementation head `3ee6a41`, run `29135964946`: `Backend Tests`,
  `Frontend Build` and `Docker Build` PASS; final documentation-only head must
  pass the same checks before merge.
- at that historical point, the full tagged admin suite still depended on a
  separately provisioned shared schema and was correctly not represented as
  green; later `P6-TEST-DB-001` PR #155 removed that dependency and closed it.

Focused P6-F33 results on `codex/prd6-b0b-notification-tenant`:

- regression-before-fix integration matrix: FAILED on foreign read, create,
  update, org-zero and denied-sink paths after correcting a fixture-only schema
  assumption;
- `go test ./internal/admin -run 'TestP6F33NotificationTenantIsolationIntegration|TestListNotification|TestCreateNotificationWebhook' -count=1 -v`: PASS;
- migrated PostgreSQL/JWT subtests prove same-tenant behavior, foreign denial,
  project/org consistency, project-admin membership, org-zero fail closed,
  platform exception, historical quarantine/re-verification, retry exclusion,
  atomic-audit rollback, ID non-enumeration and zero denied receiver calls;
- `go test -race ./... -count=1`: PASS, with the existing macOS linker warning;
- `go vet ./...`: PASS;
- frontend type-check, 25 files / 126 tests, and production build: PASS, with
  the existing large-chunk warning;
- `git diff --check`: PASS;
- PR #142 exact-head `cb8a5ea` passed `Backend Tests`, `Frontend Build` and
  `Docker Build`; it was squash-merged as `7173c59`, and post-merge main CI run
  `29132520222` passed.

Focused P6-F37 results on `codex/prd6-audit-tenant-operability`:

- `go test ./internal/admin -run 'TestP6F37AuditLogTenantIsolationIntegration|TestListAuditLogs' -count=1 -v`: PASS;
- migrated JWT subtests prove omitted-filter tenant scoping, foreign org/project
  denial, same-tenant filters, org-zero fail closed, platform exception and no
  victim-marker disclosure;
- `go test -race ./... -count=1` and `go vet ./...`: PASS;
- the first frontend suite attempts exposed pre-existing 5-second interaction
  timeout instability under parallel load; isolated reruns passed, then P6-F38
  set a bounded 10-second timeout and added the suite to required CI;
- final frontend type-check, 25 files / 126 tests, and production build: PASS;
- PR #143 run `29133309062`: Backend Tests PASS, Frontend Build PASS and Docker
  Build PASS; the required frontend log explicitly reports 25 files / 126 tests.
- independent review reproduced the P6-F37 pre-fix cross-tenant leak on
  `7173c59`, found no bypass candidate, and found no P6-F38 gate weakening.
- PR #143 was squash-merged as `a1e0ad6`; post-merge main CI run `29133690338`
  passed Backend Tests, Frontend Build (including the new test step) and Docker
  Build.

Pre-change repository results on 2026-07-11:

- `go test ./... -count=1`: PASS;
- `go vet ./...`: PASS;
- `npm --prefix frontend run type-check`: PASS;
- `npm --prefix frontend test -- --run`: PASS, with existing React Router,
  Semi Form, and `findDOMNode` warnings; 25 files / 125 tests;
- `npm --prefix frontend run build`: PASS, with existing large chunk warning;
- `git diff --check`: PASS;
- X-Insight `go test ./... -count=1`: PASS.

Focused Batch 0A post-change results:

- `go test -race ./internal/gateway -count=1`: PASS;
- `go test -race ./internal/middleware -count=1`: PASS;
- strict limiter tests repeated 20 times: PASS;
- real Redis 8.8.0 script check admitted two and rejected the third: PASS;
- focused middleware vet and `git diff --check`: PASS.

Focused read-only Batch 0B audit results on baseline `d5a5249`:

- `go test ./internal/auth ./internal/middleware ./internal/admin ./internal/billing -count=1`: PASS;
- `go test ./internal/gateway ./internal/middleware ./internal/billing ./internal/circuitbreaker -count=1`: PASS;
- `go test -race ./internal/gateway ./internal/middleware ./internal/circuitbreaker -count=1`: PASS;
- `git diff --check` and cited-repository-path existence check: PASS;
- these results do not cover PostgreSQL/Redis multi-instance races or prove a
  100x workload.

Focused P6-F16 results on `codex/prd6-b0b-tenant-isolation`:

- regression-before-fix suite: FAILED on the expected tenant, membership,
  model-set atomicity/runtime and role-race paths;
- `go test -race ./internal/admin -run '^TestP6F16' -count=1`: PASS;
- the authenticated test created a temporary database, applied migrations
  `000001`-`000019`, and ran twelve subtests without skip;
- `go test ./internal/admin -count=1`: PASS;
- `go test ./internal/modelgov -count=1`: PASS;
- `go test -race ./... -count=1`: PASS, with the existing macOS linker warning;
- `go vet ./...`: PASS;
- frontend type-check and build: PASS;
- frontend standalone rerun: PASS, 25 files / 125 tests; an earlier run
  concurrent with race/vet/build had one five-second Data Exchange test timeout;
- `git diff --check`: PASS;
- independent final code/test review: no P6-F16 blocker;
- PR #140 implementation head `ee025ec04b677c7ba0371b9a247f0ca70ec9aa0e`:
  `Backend Tests`, `Frontend Build`, and `Docker Build` PASS;
- Backend CI log: migrated P6-F16 integration and all twelve subtests PASS,
  not SKIP; injected partial-write rollback regression PASS;
- PR #140 final head `fd1ab56db8b308ecb5c9e4165ed5a8e13dda9aea`:
  the same three required checks PASS;
- squash merge `f244f669c529fec8767f85fff209882b333d8ed2`:
  post-merge `main` run `29129070253` Backend, Frontend and Docker jobs PASS.

Full Batch 0A/kickoff repository results:

- `go test ./... -count=1`: PASS;
- `go vet ./...`: PASS;
- `go test -race ./internal/gateway ./internal/middleware -count=1`: PASS,
  with the existing macOS linker `LC_DYSYMTAB` warning only;
- `npm --prefix frontend run type-check`: PASS;
- `npm --prefix frontend test -- --run`: PASS, 25 files / 125 tests, with the
  existing framework warnings;
- `npm --prefix frontend run build`: PASS, with the existing large-chunk
  warning;
- `git diff --check`: PASS.

PR #139 required checks for implementation commit `000ea64`:

- `Backend Tests`: PASS;
- `Frontend Build`: PASS;
- `Docker Build`: PASS.

PR #139 required checks for final scalability-audit evidence head `6c8c192`:

- `Backend Tests`: PASS;
- `Frontend Build`: PASS;
- `Docker Build`: PASS.

P6-F01, P6-F02, P6-F16 and P6-F32-P6-F39 are merged and `CI verified`.
D6-08 accepts P6-F32-P6-F36. P6-F35 was
squash-merged by PR #147 as `c08db96167837ade685708c2ab204861a8ecc0ca`, and
post-merge main run `29139534691` passed all required jobs. P6-F36 PR #148
final evidence head `75be4f3526aee710a9472e01ac0f5dee4079beaa` passed required
CI run `29140739477`, was squash-merged as
`d83dbbc0ac7137bb44d2346adc5a25debde804e1`, and post-merge main run
`29140850345` passed all required jobs. No documentation or local test promotes
a row to target or production verified.

P6-F35 target activation is deferred by D6-09. A later authorized run must
drain old control-plane binaries before a non-rolling activation of only the new
version; old binaries do not provide mixed-version safety. New-version replicas
converge a committed sensitive-word version through the existing approximately
one-minute reload. This batch adds no global-settings UI. Typed configuration
ownership and the budget/alert key, scope and audit truth chain are CI verified /
closed by P6-F05.
P6-F17 provides metadata-only request replay protection; durable
ambiguous-commit recovery remains P6-F20.

P6-F36 preserves P6-F16 and does not add a migration, RLS, composite/alias
foreign keys, a platform selector UI or target cleanup. Unregistered-model
compatibility and `/v1/models` semantics are unchanged.

No deployment, target mutation/probe, real or paid request, credential change,
exporter/outbox, deletion or expanded data exchange occurred. Migration down,
TLS 443 behavior and concrete target route/failover detail remain target-evidence
debt and, by User decision, are executed only after PRD6 source closeout with
their existing authorizations. PRD7 implementation is also deferred; historical
branches remain retained. The 2026-07-17 Gate A exact-path deadline is unchanged.
Unimplemented X-Insight runtime capabilities remain external.

## Previous Batch 1Z Record

PRD5 Batch 1Z remains Target verified in
`docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`.
It records migration `000019` forward apply, stable/canary deployment, selected
browser evidence, canary parity, cleanup, and the authorized `INFRA-SEC-001`
least-privilege cutover. The target backend remains `95d1da9` / PR #133.

## Previous Batch 1W Record

## Scope

1. Remove the duplicate pre-relay API-key `allowed_models` denial from the live Chat wrapper.
2. Keep project model-governance resolution in the wrapper and let Relay own API-key denial correlation and decision evidence.
3. Add full-handler integration coverage for response correlation, trace evidence, and pre-billing termination.
4. Preserve the immutable Gate A snapshot as diagnostic evidence and reconcile X-Insight persistent import/readback without rewriting target history.
5. Replace the mandatory Codex -> Canary -> Sentinel -> Atlas chain with one execution owner plus optional independent review.

## Behavior Change

- A project-allowed but API-key-denied Chat request now reaches Relay.
- Relay returns HTTP 403 with `X-Request-ID` and records `auth.model_not_allowed` decision evidence.
- The denial still occurs before billing freeze, routing, provider dispatch, or call-log write.
- Project model-set, retired-model, unregistered-model and alias-rewrite behavior remains wrapper-owned.

## Files And Evidence

- Code: `cmd/uniroute/gateway_handlers.go`
- Integration coverage: `cmd/uniroute/prd2_p0_e2e_integration_test.go`
- Local evidence: `docs/ops/evidence/PRD5-B1W-pre-relay-model-permission-parity-local-20260710.md`
- External integration evidence: `docs/ops/evidence/PRD5-GATE-A-XINSIGHT-external-integration-20260710.md`
- Target rerun evidence: `docs/ops/evidence/PRD5-B1W-target-rerun-20260710.md`
- Workflow decision evidence: `docs/ops/evidence/AGENT-PROTOCOL-v2-single-owner-20260710.md`
- Gate A artifact remains in the UniRouter/X-Insight shared folder with SHA-256 `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8`.

## Verification

Required before PR:

```bash
go test ./cmd/uniroute ./internal/gateway -count=1
go test -tags=integration ./cmd/uniroute -run TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration -count=1 -v
go test ./... -count=1
git diff --check
```

Current local results on 2026-07-10:

- focused `cmd/uniroute` and `internal/gateway`: PASS;
- migrated-DB `TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration`: PASS;
- full `go test ./... -count=1`: PASS;
- `git diff --check`: PASS.

Required in GitHub before merge:

- `Backend Tests`
- `Frontend Build`
- `Docker Build`

PR #126 results:

- `Backend Tests`: PASS;
- `Frontend Build`: PASS;
- `Docker Build`: PASS;
- squash merge: `931400a`.

## Non-Goals

- No project model-governance semantic change.
- No route, migration, frontend, billing amount, provider or raw-payload change.
- No regeneration of the Batch 1V snapshot or reuse of its two-request authorization.
- No read-only X-Insight DB/view, shared ECS staging or exporter/outbox.
- No PRD4 reopen and no production claim.

## Target Execution

- User authorization `UR-XI-GATE-A-20260710-02` permitted deployment of the
  CI-passing target and exactly one synthetic denied-scenario rerun.
- Target source/runtime metadata is
  `e823abc293f996264763c6c1aca19d3d4d6e705f`, containing Batch 1W
  `931400a` / PR #126. Stable and canary readiness checks passed.
- Request `gatea-model-denied-b1w-283194e67a0c3ff9` returned HTTP 403 with a
  matching `X-Request-ID` and the expected `auth.model_not_allowed` trace.
- The request created zero call logs, billing freezes, transactions, and
  provider events. No paid upstream request was sent.
- Temporary API keys 36 and 37 are disabled.
- The second snapshot receipt is `persisted`: first persistence inserted two,
  rerun produced two duplicate/no-op with zero conflict, and readback returned
  two events/two gaps for exchange
  `uxchg_8deae233291214154815c336c9278983`.
- The first immutable Gate A snapshot remains externally verified: 10 accepted,
  first persistence 10 inserted, rerun 10 duplicate/no-op, and readback 10
  events/3 gaps.
- X-Insight target verification is separate from the receipt and is now Target
  verified for the fixture/synthetic-only non-production release artifact
  `b9e5811`; this is not a public API or production claim.
- Both temporary Gate A snapshot JSON artifacts are due for exact-path deletion
  by 2026-07-17. Receipts, hashes, sanitized evidence, and persisted normalized
  evidence remain retained under the accepted decision.
- UniRouter Batch 1Z completed the approved infrastructure-security remediation;
  remaining capacity and production-hardening work does not alter this result.

## Rollback

- Code rollback: revert the Batch 1W commit through a new PR; do not bypass branch protection.
- Deployment rollback: stable/canary rollback images are retained as
  `rollback-20260710151314`; no rollback was required.
- Data rollback: none; Batch 1W has no migration and does not mutate the immutable Gate A artifact.

## Optional Independent Review

Reviewers may verify this batch without becoming a serial gate. Write results under `docs/agent-workflows/reviews/` and include:

1. Diff review for the removed duplicate authorization path.
2. Targeted and migrated-DB integration test output.
3. Confirmation that no billing freeze or call log is written on denial.
4. PR/CI verification against the merged commit.
5. If target access exists, a newly authorized single-scenario rerun and evidence comparison.

P0/P1 findings block or reopen the batch. P2/P3 findings are triaged under the protocol.
