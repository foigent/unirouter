# Current Execution Record

## Active Boundary — X-Insight Cooperation Only

**Owner:** Codex for UniRouter status reconciliation; X-Insight consumer/joint
acceptance requires the external project owner.

**Risk:** R2 documentation/status decision only

**Status:** the selected Post-PRD6 V1 first-release work window is closed. No
active UniRouter-owned code, schema, deployment, credential, provider-price or
target-configuration work remains in this window. `V1-XI-LEDGER-001` is the
sole active follow-up within this V1 Post-PRD6 window and remains
`Coordination pending`.

**Decision baseline:** clean/aligned `main` and `origin/main` at
`90ffc010c4220d4cae738f04cad4021d2eb20f0e`; latest code-bearing repository and
development-target release remains PR #214 merge
`46b49b60835225a0b32b641615e6afe4a8a9696b`.

**Closeout evidence:**
`docs/ops/evidence/V1-POST-PRD6-WINDOW-CLOSEOUT-20260716.md`.

The non-X-Insight residuals are recorded without false completion:

- `V1-TARGET-PILOT-001`: deferred to a separately approved future
  customer/Production acceptance window;
- `V1-PROVIDER-BILL-RECON-001`: actual reconciliation deferred/not executed;
  recurring/productized reconciliation is not implemented and remains PRD7
  candidate `D7-COST-001/002`;
- `V1-CHANNEL-MAPPING-001`: deferred until post-pilot user feedback; the
  controlled V1 operator procedure remains the current boundary.

Supplier-bill reconciliation does not block the current functional pilot but
continues to block supplier-cost equality, pass-through, gross-margin and
Production-financial-accuracy claims. A one-time bounded manual/account
reconciliation may be authorized separately from PRD7; recurring product
capability requires separate `D7-COST-001/002` acceptance. Neither path is
authorized here. PRD6 remains `Completed (Accepted Scope)` and is not reopened.
Reactivation requires an explicit new decision/window.

**Publication validation:** this closeout changes documentation only. Change
Routing should explicitly skip Backend Tests, Frontend Build and Docker Build.

## Previous Batch — V1 Admin Principal State And Pilot Catalog Target Closure

**Owner:** Codex

**Risk:** R3 administrator authentication and model-admission behavior; R2
frontend/API presentation and status reconciliation; R4 approved development-
target formal release, reversible pilot configuration and credential cleanup

**Status:** Target verified / closed for `V1-ADMIN-STATUS-001`,
`V1-PILOT-CATALOG-001`, and the exact two-model configuration slice of
`V1-TARGET-ROUTING-001`. The later window-closeout decision defers the broader
pilot/Production and supplier-bill items; X-Insight remains coordination
pending.

**Repository and release authority:** PR #214 exact head
`4cf13761edbdc50163cbade1151051a74d3d42dc` passed Change Routing and the
three required checks in run `29441931266`, then squash-merged and formally
released as `46b49b60835225a0b32b641615e6afe4a8a9696b`. PR-head and merge trees
are identical. No automatic post-merge run was created, as intended by the
trusted-dispatch CI design.

**Evidence:**

- immutable local checkpoint:
  `docs/ops/evidence/V1-ADMIN-JWT-STATUS-AND-PILOT-CATALOG-LOCAL-20260716.md`;
- target release/API/DB/browser/cleanup checkpoint:
  `docs/ops/evidence/V1-ADMIN-JWT-STATUS-AND-PILOT-CATALOG-TARGET-20260716.md`.

Stable/canary run exact formal backend image
`sha256:60b2f5620f815eff3e75716c7f20516dda49c611cfa8202df37ae98643634e7f`;
the frontend runs
`sha256:7a3c8eca48309e6743386bb2d24c1f66fb7838c2d57970fdba34876d72e36993`.
Migration `000037`, formal release trust 1/1, active enforceable test License,
signed-upgrade sequence `4`, readiness and zero-restart checks pass.

The same temporary administrator JWT reached the protected endpoint with HTTP
200 while the exact principal was active and returned 401 after disablement.
Database uncertainty remains typed 503 and the frontend preserves the token for
explicit retry; 401 clears the session. This is state-dependent request
rejection, not a token blacklist or permanent per-session/password/logout
revocation.

Model set `v1-pilot-20260716` is active with exactly
`DeepSeek-V4-Flash` and `mimo-v2.5-pro`; project #1 has exactly that one model-
set binding and `approved_catalog` is true only for those two identities.
Stable/canary `/v1/models`, routing preview and authenticated browser readback
match the exact two-model boundary. Kimi channel #6 is disabled at routing
epoch 4, not deleted, retired or declared broken.

Excluded operation `op_163bb39294d192e19e664d7912111512` returned 403 before
provider dispatch. It has zero call log, freeze, provider-attempt, transaction
and internal-outbox rows, with an unchanged wallet. Temporary key #44 replayed
as 401 after disablement; temporary administrator #17 is disabled. Final DB
readback found two temporary users and two temporary keys, all inactive.

The authenticated browser verified the development label, exact two approved
models, exact two model-set items/project assignment, channel states, routing
preview, key one-time-display/created-time semantics and disabled temporary
keys, then logged out to `/login` with no console errors observed. RSA protected
only the ECS-to-automation credential handoff; the actual login POST used the
public HTTP target. The temporary credential was never printed, was disabled
after verification and was cleared when the tab closed. This is not TLS or
Production evidence.

**Preserved non-claims:** no supplier billing console/export/API/invoice or
isolated balance delta was inspected. Historical provider-reported usage and
810007 micro-CNY settlement prove UniRouter's customer ledger, not supplier
COGS. Actual reconciliation is deferred/not executed; recurring/productized
reconciliation is not implemented and remains candidate `D7-COST-001/002`.
Capacity, detector, enterprise IdP, customer
backup/restore/upgrade, Production/TLS/HA, long-running reliability, broader
providers and permanent JWT revocation are deferred outside this window;
X-Insight v2 acceptance remains the sole active follow-up within this V1
Post-PRD6 window. PRD6 remains
`Completed (Accepted Scope)` and is not reopened.

**Publication validation:** this reconciliation changes documentation only.
Change Routing should explicitly skip Backend Tests, Frontend Build and Docker
Build because the code-bearing PR #214 already passed all three required checks
before merge.

## Previous Batch — V1 DeepSeek Flash Official Price Target Evidence

**Owner:** Codex

**Risk:** R4 User-approved target price mutation and exactly one additional
bounded paid request; R2 status/evidence reconciliation

**Status:** Target verified / closed for `V1-TARGET-PRICING-001` within the
dated DeepSeek Flash pass-through pilot scope; broader provider, pilot,
Production and X-Insight gates remain open

**Repository baseline:** clean `main` / `origin/main` at documentation-only
checkpoint `e61feb96071e88b6f45016239148160b7c374c51`; latest code-bearing
release remains `ae9ce2c927460e993dd00c6d81c8736cc5a015a2`

**Documentation branch:** `codex/v1-deepseek-flash-price-evidence`

**Documentation publication at this record checkpoint:** pending docs-only
commit, pull request, required CI and merge. The target runtime result below is
already independently read back; publication state must not be mistaken for a
pending target mutation or a reopening of PRD6.

**Evidence:**
`docs/ops/evidence/V1-DEEPSEEK-FLASH-OFFICIAL-PRICE-TARGET-20260716.md`

DeepSeek's official 2026-07-16 page publishes V4 Flash at CNY 1 per million
cache-miss input tokens, CNY 0.02 per million cache-hit input tokens and CNY 2
per million output tokens. Target preflight found price row #9 at the preserved
legacy CNY 1000/2000 public rates, cache ratios 1/1 and no organization
multiplier. Route channel #5/key #4, wallet and pending state were healthy.

Supported `PUT /api/v1/model-prices/9` changed only the exact price fields to
`0.1/0.2` cents per 1K and cache ratios `0.02/1`. Transactional audit #196
records exact public before/after state `1000/2000/1/1` to `1/2/0.02/1`, server
operation `op_c4080691d371ed08e61a70dd33df5988` and a separate client
correlation.

Exactly one additional non-stream request,
`op_240a63dd8e3de67036d894cd5b7d0d8f`, returned gateway/upstream HTTP 200 and
response model `deepseek-v4-flash`. Authoritative usage is 5 uncached input, 0
cache-read, 0 cache-creation and 1 output token. Provider attempt
`att_f427b66ed0a10fd1484eaeb07f674da9` was the only attempt and settled known
usage for 7 micro-CNY. Wallet balance/spent changed
`7,980,000/2,640,000 -> 7,979,993/2,640,007`, while granted balance remained
zero; the 100,000-micro-CNY minimum
freeze released all but 7, leaving zero frozen/uncovered/pending state.

The approved CNY 0.01 value is an actual-settlement spend boundary, not a
technical API-key cap or maximum freeze. Temporary key #42 and operator #15 are
disabled; key audits #198/#199 and zero outbox are read back. The 24-hour admin
JWT was never printed or persisted and was cleared with the completed process,
but account disablement is not claimed as server-side JWT revocation.

Independent invocation `t-wl06r0meexcuu4g` passed exact price, audit, one-call,
one-attempt, pricing snapshot, ledger conservation, cleanup, capacity and zero
pending assertions. Stable/canary remain healthy on formal ae9 image
`sha256:fca7fe8f...`, signed sequence `3`, migration `000037` and the unchanged
development/test License. No code, image, schema, supplier credential, Kimi,
DeepSeek Pro, permanent markup policy, Production or X-Insight state changed.

## Previous Batch — V1 DeepSeek Flash Target Routing Evidence

**Owner:** Codex

**Risk:** R4 User-approved one-provider target configuration and exactly one
bounded paid request; R2 status/evidence reconciliation

**Status:** Target verified / closed for the named DeepSeek Flash route and
single request; target pricing authority, Kimi/DeepSeek Pro and broader
target/Production/X-Insight gates remain open

**Repository baseline:** clean `main` / `origin/main` at documentation-only
checkpoint `998ff1bb79df0d83c7cdd2d9c3be80e6f44c0ab3`; latest code-bearing
release remains `ae9ce2c927460e993dd00c6d81c8736cc5a015a2`

**Documentation branch:** `codex/v1-deepseek-flash-target-evidence`

**Evidence:**
`docs/ops/evidence/V1-DEEPSEEK-FLASH-ROUTING-TARGET-20260715.md`

Official DeepSeek documentation and the target credential's live `GET /models`
response independently proved `deepseek-v4-flash` as the exact current supplier
identifier. Preflight found channel #5 healthy but scoped to retiring aliases,
while key #4 allowed the public `DeepSeek-V4-Flash` identity and no mapping
existed.

One exact-precondition database transaction changed only channel #5 to public
model `DeepSeek-V4-Flash` with mapping to `deepseek-v4-flash` and advanced its
routing epoch from 1 to 2. Audit #193 captures the before/after state. Stable
and canary previews then each selected that one healthy channel/key with zero
exclusions.

Exactly one approved non-stream request,
`op_773ffcc46d00b98e1bb373ef161b90d1`, returned gateway/upstream HTTP 200 with
5 input plus 1 output token and response model `deepseek-v4-flash`. Provider
attempt `att_084c92d8a84aa85f8c2ca3ac858c1633` settled known usage at HTTP
200. Exact charge was 7000 micro-CNY, below the CNY 0.35 cap; wallet
conservation holds, temporary key #41 is disabled, and pending attempt/outbox/
reservation plus active temporary-key counts are zero.

No code, image, schema, supplier credential, price, multiplier, License, Kimi,
DeepSeek Pro, Production or X-Insight state changed. Stable/canary remain on
formal ae9 image `sha256:fca7fe8f...`, signed release sequence 3 and migration
`000037`.

The target DeepSeek Flash customer rate is CNY 1000 input / 2000 output per
million tokens, 1000x the supplier's current published CNY 1 cache-miss input /
CNY 2 output rates. This may be intentional resale pricing, so it is not
classified as a code bug. It is `V1-TARGET-PRICING-001`, a Critical decision
gate before customer traffic; no automatic repricing is authorized. The
operator-only `model_mapping` procedure is separately tracked as Medium
`V1-CHANNEL-MAPPING-001` and does not justify adding unapproved V1 feature
scope before pilot feedback.

## Previous Batch — V1 Mimo Routing, Upstream Status And Formal ae9 Target

**Owner:** Codex

**Risk:** R2 evidence regression fix; R4 User-approved test signing-root
rotation, development-target deployment and one bounded paid request

**Status:** Target verified / closed for the upstream-status regression and
bounded Mimo single-call path; remaining target/Production/X-Insight gates open

**Code baseline before fix:** clean `main` / `origin/main` at
`dfb9ecb52d20d1c0b69d0ea43285dbac69e12ef1`

**Implementation branch / PR:** `codex/v1-upstream-status-evidence`; PR #210;
exact head `5f8cd6a4aedb74354dea9f33223d4aca95e3f2e9`; required run
`29419821405`

**Merge / backend release:**
`ae9ce2c927460e993dd00c6d81c8736cc5a015a2`

**Evidence:**
`docs/ops/evidence/V1-MIMO-ROUTING-AND-UPSTREAM-STATUS-TARGET-20260715.md`

Two pre-fix real Mimo requests proved the regression: their provider attempts
were `settled/known_usage/http 200`, but both call-log upstream statuses were
null. PR #210 makes the durable outbox and synchronous fallback resolve the
exact attempt HTTP status; Backend and Docker required jobs passed while the
unaffected Frontend job was correctly skipped.

The User approved a persistent test upgrade key and formal ae9 activation. The
private key remains outside the repository under
`~/Documents/UniRouter-Test-Keys/2026-07-15-ae9-upgrade/` with mode `0600`;
only its public ring reached the ECS. Formal backend image
`sha256:fca7fe8fdabfcbfd32198c8bb4ba054e1b465f8342f671af39dba76b54b822d0`
embeds one License root and one upgrade root. Signed package sequence `3`, hash
`ac7413476b08e54561a8e243d5a0bc32916f8312443d62f82025613413ff3746`,
verified against current sequence `2`, was recorded once and rejects replay.

The first ae9 attempt accidentally used the source/development Compose trust
mode. License admission failed closed, release state stayed at sequence `2`,
and the prior formal backend was restored immediately. The first formal canary
operator script later rolled back on a `pipefail`/SIGPIPE false negative after
the application was healthy; readback proved rollback and the corrected
output-matching check passed. Final stable/canary promotion and sequence record
completed with zero restarts. Frontend/Nginx remain on the unchanged a0a
release because PR #210 is backend-only.

Exactly one additionally approved non-stream Mimo request returned request
`op_1dc206219bcbafc45d4b9df2b66915c7`, HTTP 200, 257 input plus 1 output
token, call-log upstream 200 and a `settled/known_usage/http 200` attempt. The
259000-micro-CNY charge is below the approved CNY 0.35 additional ledger cap;
wallet deltas conserve exactly, frozen balance is zero, temporary key #40 and
QA user #13 are disabled, and pending attempt/outbox counts are zero.

Final independent readback passed public/stable/canary readiness, authentication
boundary, exact image/commit, formal trust, active enforceable test License,
migration/release history, ledger conservation and cleanup. No DeepSeek/Kimi,
capacity, detector, IdP, supplier-invoice, customer, Production or X-Insight
claim is made. PRD6 remains closed.

## Previous Batch — V1 First Release Product Audit And Target Closeout

**Owner:** Codex

**Risk:** R2 UI/API/document corrections; R4 authorized development-target deployment after merge

**Status:** Target verified / closed for accepted first-release corrections

**Baseline:** clean `main` / `origin/main` at `c051e0abe2a02382cefc8a77b04a9bdcc45828a1`

**Implementation branch:** `codex/v1-release-ui-target-closeout`

**PR / required CI:** #207; exact head
`f63bcb268251cd6ec0d8701afc20a7928cd32ecb`; run `29368640933`; Change
Routing, Backend Tests, Frontend Build and Docker Build passed

**Merge:** `a0a567dd169dcb133b09eed3f711cdcf87214641`

**Evidence:** `docs/ops/evidence/V1-FIRST-RELEASE-PRODUCT-AUDIT-LOCAL-20260715.md`;
`docs/ops/evidence/V1-FIRST-RELEASE-TARGET-CLOSEOUT-20260715.md`

Scope is limited to first-release defects and unreasonable presentation found
in the User's complete product checklist: contributor/README truth, route and
upstream-credential operability, caller-key clarity, stale PRD labels,
notification semantics, audit readability, existing OIDC identity visibility,
page-size correctness and taxonomy alignment. It adds no vendor discovery,
directory synchronization, Update Center, new menu hierarchy, paid provider
call or X-Insight behavior.

The existing development target was first audited at exact release `c051e0a`,
then updated only after the exact PR head passed required CI and PR #207 merged.
Stable/canary/frontend now run exact merge `a0a567d`, version `1.0.0`,
environment `development`, migration `000037`. Stable/canary use exact image
`sha256:a7fe9237174862c5afcfaab892d486a61acde44684923a522f8b6ffc0ddfaa82`;
frontend uses
`sha256:5b4ad05756ed9f3cc9c5815fa2e19ce0461d7575076493425213f6663120db7e`.

Verification passes:

- `go test ./internal/admin ./internal/auth -count=1`;
- six affected frontend test files, 18 tests;
- `go test ./... -count=1`, `go vet ./...` and `go build ./...`;
- required local PostgreSQL admin integration suite; zero temporary databases
  remained;
- full frontend suite, 33 files / 167 tests, type-check and production build;
- `git diff --check`;
- required PR CI run `29368640933`, all four jobs;
- public/stable/canary readiness with PostgreSQL and Redis healthy;
- signed upgrade sequence `2` verify/record/readback plus verify and
  `record-applied` replay rejection;
- 24/24 authenticated admin routes in a real browser with no captured console
  error, error boundary, 4xx/5xx resource failure or horizontal overflow;
- restricted developer usage console and API authorization readback;
- temporary QA/developer actors disabled and browser state cleared.

Existing route allowlists still do not match the current model catalog. No
allowlist, supplier secret or financial record was changed and no paid upstream
request was made. Real provider/detector/enterprise-IdP, Production and
X-Insight v2 evidence remain explicit later gates. PRD6 remains closed.

## Previous Batch Record — Commercial License Studio And Signed Upgrade

**Batch:** V1 Commercial License Studio, Embedded Trust And Signed Offline Upgrade Bundle
**Owner:** Codex
**Risk:** R3 authorization/signing implementation; any target activation is R4
**Status:** Merged and CI verified; production key ceremony and target activation pending separate decisions
**Baseline:** clean `origin/main` / `e94a4b184a6f675d76c292602b7c18293ab2ce61`
**Branch:** implementation branch `codex/v1-commercial-license-upgrade` merged to `main`
**PR:** #205 squash-merged as `5c1313521870731b0164d4eecfbbb06d7a7f0704`; final head `f1fdee12499aa399e63f205a00040d772425e7df` passed required CI run `29360683242`
**Decision:** User requires commercial authorization enforcement, signed upgrades and a click-only License generation program before customer delivery
**Evidence:** `docs/planning/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-CONTRACT-20260714.md`; `docs/ops/evidence/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-LOCAL-20260715.md`; `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-LOCAL-20260715.md`; `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-CI-20260715.md`; `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-MERGE-20260715.md`
**Next product gate:** production key custody/ceremony and any non-production target activation remain separate decisions
**Previous batch:** PR #204 documentation closeout after V1 micro-ledger PR #202 and environment-label PR #203
**Previous phase:** PRD5 Accepted Scope Complete; its Batch 1Z historical target backend was `95d1da9` / PR #133 and is now superseded on the same target by this PRD6 deployment

## Current Batch Scope

1. Replace the PRD2 payload-only License foundation with an explicit,
   key-identified commercial schema whose signature covers its schema, key and
   payload.
2. Bind an enforceable License to the configured stable deployment identity;
   block new model/Agent admission for missing, invalid, mismatched,
   not-yet-valid or expired authority while retaining recovery/admin access.
3. Enforce signed feature and positive active-resource limits without a
   configuration switch that disables enforcement in the customer release.
4. Remove customer-controlled runtime trust-root replacement: formal release
   images embed separate License and upgrade public rings at build time and
   reject runtime overrides.
5. Add a double-click macOS License Studio for native-folder key generation,
   customer form signing and verification without putting private-key bytes in
   the browser, runtime image, database, logs or evidence.
6. Retain vendor-side License CLI compatibility tooling for CI/controlled
   automation, not as the normal operator workflow.
7. Add a separately trusted signed offline upgrade archive with signed
   manifest, payload hash/size, platform, release-sequence, metadata-expiry and
   schema-compatibility checks before extraction or execution.
8. Add tamper, traversal, rollback, wrong-key/platform/product, concurrency and
   recovery-path regression tests, documentation, PR and required CI.

## Current Non-Goals And Stop Conditions

- No PRD6 row is reopened and no existing PRD6 completion claim is promoted.
- No production signing key, persistent target key, customer License or
  production deployment is created without a separate concrete approval.
- No hardware fingerprint, online activation, arbitrary Admin Console package
  execution or full TUF-conformance claim.
- Stop before target activation if the new runtime cannot retain health/login/
  License recovery while unlicensed, if a denied request can reach API-key
  lookup/billing/provider dispatch, if limits race above signed capacity, or if
  any archive content can be extracted/executed before complete verification.

## Current Batch Result

1. Commercial License admission now requires a strict key-identified Ed25519
   document, exact deployment/time/feature authority and concurrency-safe
   signed active-resource limits. Missing/invalid authority fails before new
   model, Agent or real channel-test upstream dispatch while retaining local
   recovery/import/diagnostic access.
2. Migration `000037` projects the verified entitlement and prevents API-key,
   user, project or channel creation/re-enable beyond capacity or after expiry;
   startup safely rebuilds restored derived state, recurring refresh rejects a
   mismatched projection, and guarded down refuses while entitlement or release
   history remains.
3. Formal release binaries now load only build-embedded License and upgrade
   public rings. Runtime `LICENSE_PUBLIC_KEY`, `LICENSE_PUBLIC_KEYS` or
   `UPGRADE_PUBLIC_KEYS` cannot replace trust; source/development mode remains
   explicit and is not customer-release eligible.
4. Vendor-only `UniRouter License Studio.app` uses a random loopback session,
   strict origin/token/CSP/host checks and macOS native file dialogs. It
   generates separate License/upgrade keys, issues and self-verifies strict
   customer documents, refuses overwrite and never sends private-key bytes to
   browser JavaScript. The CLI remains for compatibility only.
5. `uniroute-upgrade` packs and verifies deterministic signed archives before
   extraction/execution, rejects untrusted/tampered/unsafe/rollback/schema-
   incompatible content and binds provenance to its fixed declared artifact.
6. Runtime `upgrade verify/status/record-applied` uses actual database/schema
   state and records only the exact healthy immutable version/commit under a
   serializable/advisory-lock release sequence.
7. `deploy/customer` is a no-build/source-free Compose profile with mandatory
   image references, embedded binary trust roots, external signed
   config/secrets, unexposed dependencies and a local-only frontend default.
8. Current-tree serial full Go tests, focused race tests, vet/build, 31 frontend
   files / 164 tests, type-check/build and YAML parsing pass locally. Exact
   code-bearing head `cd05055` also passed all four required checks in run
   `29359673125`, including the full race-enabled Go suite, migrated-PostgreSQL
   gates, frontend suite/build and formal release Docker image build.
9. No production/customer private key or License was generated. No ECS embedded
   trust root, deployment identity, License, migration, release state or
   image was changed. Target activation is a separate R4 decision and this
   batch does not reopen PRD6 or accept PRD7 Update Center/automatic apply.

## Current Focused Verification

- Commercial License/Admin/signing/release-trust/License-Studio/upgrade packages
  and CLIs: focused `PASS`.
- License Studio HTTP keygen→issue→verify, no-overwrite, wrong-deployment,
  malformed-key, dialog-cancel, non-loopback, wrong-origin/token and security-
  header tests: `PASS`.
- macOS arm64 `.app`: `plutil`, ad-hoc `codesign --verify --strict`, JavaScript
  syntax and embedded-release `go build -ldflags` checks: `PASS`. Developer ID
  signing/notarization remains a distribution prerequisite, not a local claim.
- Playwright headed desktop and 390 px mobile UI: tabs/defaults/feature toggle,
  responsive render, zero console errors/warnings and only three loopback
  static requests: `PASS`.
- Exact current tree `go test -p 1 ./... -count=1`, `go vet ./...` and
  `go build ./...`: `PASS`. The new signing/trust/Studio/License/upgrade slice
  also passes `-race`; the exact code-bearing PR head passed the full CI Go and
  integration-test path.
- Migrated PostgreSQL `000037` capacity/expiry/down guard and upgrade
  record/replay integration gate under `-race`: `PASS`; generated temporary
  database count returned to zero.
- Frontend Vitest 31 files / 164 tests, type-check and production build:
  `PASS`.
- CI/OpenAPI/customer Compose YAML parse and `git diff --check`: `PASS`.
- Local Docker: not run because this host has no `docker` executable. Required
  Docker CI passed the formal release image and Docker-context checks on exact
  code-bearing head `cd05055`.
- Evidence:
  `docs/ops/evidence/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-LOCAL-20260715.md`
  and
  `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-LOCAL-20260715.md` and
  `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-CI-20260715.md` and
  `docs/ops/evidence/V1-LICENSE-STUDIO-AND-EMBEDDED-TRUST-MERGE-20260715.md`.

## Previous CI Efficiency Result

1. PR #198 exact head `e9d0847` passed Change Routing, Backend Tests, Frontend
   Build and Docker Build in run `29299202652`, then squash-merged as
   `31d87def0e7c689a6bea8f4f1e39cd9583959694`.
2. The merge commit created no automatic `push` CI. Trusted main dispatch
   `29299765793` then passed the complete suite exactly once.
3. The trusted run saved shared Go cache `5720112747` under the fixed Go
   `1.25.12` key. After readback, only obsolete main cache `5576166108` was
   deleted by exact ID.
4. Final capacity readback is 32 caches / `9,503,335,891` bytes, including 31
   Go caches / `9,431,942,142` bytes. No wildcard or bulk deletion occurred.
5. Docs-only PR #199 first-head run `29300434659` completed successfully:
   Change Routing passed and Backend/Frontend/Docker each reported explicit
   `skipped`, with no expensive runner started. `V1-CI-001` is closed.
6. `V1-FIN-001` is confirmed P0: cent truncation plus a one-cent terminal clamp
   overcharges sub-cent usage and charges zero-token calculations. User-approved
   option B is fixed in
   `docs/planning/V1-MICRO-PRECISION-LEDGER-PLAN-20260714.md`; implementation
   follows prerequisite PR #196 and #197 and stops before the R3 merge/deploy
   gate.

No application behavior, target environment, credential, database, production
or PRD6 accepted-scope state changed.

## Previous PRD6 Completion Result

1. Re-read the PRD6 PRD, acceptance matrix, closeout evidence, implementation
   plan, backlog, current status, handoff, open issues and execution protocol.
2. Fetched `origin/main` and verified clean/aligned pre-publication baseline
   `9c73d6fd00fd5e58d126d9a6adc7fe9decf91cdd`.
3. Confirmed 41 feature rows: 27 accepted and closed, 14 explicitly not
   accepted; the separate P6-TEST-DB-001 engineering gate is also closed.
4. Read back PR #190/#193 merge/check state and exact-head main run
   `29274010760`; Backend Tests, Frontend Build and Docker Build passed.
5. Re-read the development/test ECS public readiness endpoint; application,
   PostgreSQL and Redis returned healthy without a target mutation.
6. Corrected the stale implementation-plan body from 26 closed / one in
   progress to the already-evidenced 27 closed / zero unclosed state.
7. Marked current truth entrypoints `Completed (Accepted Scope)` while preserving
   all proposed/deferred/target/production evidence boundaries.

No code/runtime behavior, deployment, credential, database, export, external
project or file-retention state changes in this completion publication.

## Previous Target Result

1. Backed up the target PostgreSQL database, verified the custom-format catalog
   and retained prior stable/canary/frontend images under rollback tags.
2. Applied and reconciled PRD6 migrations `000020` through `000034`; stable and
   canary now run exact backend commit `ce9307a` and report healthy PostgreSQL
   and Redis dependencies.
3. Verified authenticated Dashboard, shared limits, content safety, channels,
   reconciliation, request trace, status and API docs without creating a policy
   or sending a new provider/detector request.
4. Opened one historical request's seven-event request/billing/routing/provider
   trace and route/failover summary, satisfying P6-E01 without a paid rerun.
5. PR #191 removed the explicit `/vite.svg` 404 and documented the
   self-transactional migration rehearsal boundary; PR #192 then removed the
   browser's implicit `/favicon.ico` 404 with an inline icon. The exact
   `f4fe41d` frontend cutover and QA cleanup are recorded in
   `docs/ops/evidence/PRD6-TARGET-DEPLOYMENT-AND-UI-VERIFICATION-20260713.md`.

Audit taxonomy/name/date UX is target-confirmed but was not one of the 27
accepted PRD6 rows; it remains in the PRD7 `UI 与可运营性` candidate. At that
previous PRD6 cut the target's `环境未配置` badge was truthful; the User later
selected `development`, and PR #203 / final target `545e514` supersedes it with
the browser-verified `开发环境` label.

## Closed P6-F11 Scope

1. Add a normalized stream-output stage with synchronous 200 ms/4 KiB
   micro-buffering and detector-declared bounded cross-chunk context.
2. Keep external semantic stream inspection shadow/audit by default; permit
   enforcement only for a fixed-registry detector that declares bounded
   synchronous capability. Do not auto-enable a policy or real detector.
3. On policy, detector, frame or hard-budget termination, cancel the accepted
   upstream attempt, do not retry/fail over, emit an explicit non-success
   terminal and charge only safe output already accepted downstream.
4. Extend the frozen reservation idempotently before the D6-10 guard band,
   retaining the immutable price and original cash/grant funding sources.
5. Persist monotonic post-write stream checkpoints with a renewable owner lease;
   expired-lease recovery settles no more than the checkpoint, marks explicit
   interruption and gives idempotent retries metadata-only readback.
6. Preserve P6-F09/P6-F18/P6-F20/P6-F40/P6-F41 invariants and add guarded
   migration, focused/race/PostgreSQL tests, evidence, PR and required CI.

## Current Non-Goals

- No transparent stream migration, automatic content replay or zero-leakage
  claim.
- No negative customer debt, automatic platform subsidy, raw response storage,
  Cache Service or separate Cost Control Service.
- D6-14 additionally forbids aggregate provider bills, balance deltas or
  free-form operator amounts from creating customer debt. Provider-native
  runtime/conformance packs, live billing connectors, controlled bill import,
  generalized units, async media and managed billing remain future product
  candidates.
- No real/paid request, persistent service/provider credential change,
  production data access, data-scope expansion or target mutation beyond the
  explicitly authorized release, migration reconciliation and temporary-QA
  lifecycle recorded above.
- No age-based guess for a process that dies while an attempt still appears
  `dispatch_started`/`response_accepted`; a future lease/heartbeat design must
  distinguish failure from a valid hours-long stream.
- No real external detector configuration/call, arbitrary tenant detector URL,
  detector credential store or automatic PII/semantic blocking on upgrade.
- No dedicated FinOps workflow UI, supersession workflow, provider COGS import
  or supplier connector; those remain future D7-COST candidates.

## Previous P6-F11 Focused Verification

- P6-F11 guardrail, gateway, billing, Agent-capability, shared-policy,
  idempotency and control-plane tests: `PASS`, including measured 200 ms / 4
  KiB buffering, reasoning/unknown-shape handling, cross-frame/lookbehind
  enforcement and advisory context, explicit termination, three-layer
  extension ordering, owner-loss handoff and exact retry readback.
- Real PostgreSQL/race gates: `PASS`, including 32-way wallet extension,
  multi-replica single recovery, recovery-worker crash/reclaim, zero/latest
  checkpoint settlement, existing-terminal-receipt recovery, append-only
  extension receipts and guarded `000034` down/roll-forward/refusal.
- `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet ./...`,
  OpenAPI YAML parse and `git diff --check`: `PASS`.
- Frontend type-check, 30 files / 148 tests and production build: `PASS`.
- Local Docker was not run because this host has no `docker` executable. PR
  #189 exact implementation/test head `8ed6a83` passed required run
  `29245696926`: Backend Tests, Frontend Build and Docker Build, including both
  explicit P6-F11 gates. Final evidence head `3d4c61b` passed run `29246431182`;
  merge `4c78a5b7` passed post-merge main run `29246975012`.
- Evidence:
  `docs/ops/evidence/PRD6-B5-P6F11-stream-policy-termination-local-20260713.md`.

## Current Stop And Rollback Conditions

- Stop if an enforced batch can disclose before verdict, accepted-stream
  termination retries or fails over to another paid channel, unknown text is
  treated as inspected, hard-budget denial occurs after the next flush,
  withheld output is customer-charged, checkpoint counters regress, or two
  owners can finalize one attempt.
- Repository rollback requires a new reviewed PR. Migration down is allowed
  only before Guardrail policy/hold evidence exists. Evidence-bearing rollback
  refuses destructive change.
- A target rollout must drain old data/control-plane binaries before `000034`
  becomes active; mixed-version policy or financial safety is not claimed.

## Previous P6-F40C Behavior Change

1. Block the affected API key and Agent descendants for explicit ambiguous,
   unusable or funding-pending provider cost while ordinary in-flight work
   remains concurrent.
2. Provide platform-only amount-free collect/refund/bounded-subsidy
   reconciliation with tenant, concurrency and crash-gap safety.
3. PR #186 exact implementation/test head `49d740d` and required run
   `29224762127` passed all required jobs. Evidence:
   `docs/ops/evidence/PRD6-B3H-P6F40C-provider-key-reconciliation-local-20260713.md`.

## Previous P6-F40B Behavior Change

1. Persist bounded provider-attempt identity before paid Chat/Embeddings
   dispatch and authorize retry only from durable provider rejection.
2. Keep ambiguous/unusable/accepted-unsettled liability non-cancellable and
   atomically link known cost plus terminal receipt to the attempt.
3. PR #184 exact implementation/test head `ac2bfb5` and required run
   `29209644118` passed all required jobs. Evidence:
   `docs/ops/evidence/PRD6-B3H-P6F40B-provider-attempt-ledger-local-20260713.md`.

## Previous P6-F40A Behavior Change

1. Treat the original freeze as an admission hold, not a terminal charge
   ceiling; consume only existing prepaid cash for an atomic top-up.
2. Keep an unfunded known cost non-terminal/non-cancellable with durable usage
   and provider-cost evidence; create no terminal write-off or automatic debt.
3. Exclude funding-pending rows from automatic recovery and expose successful
   top-up in response/trace evidence.
4. PR #183 exact implementation/test head `d1afd97` and required run
   `29207889734` passed Backend, Frontend, Docker and both explicit P6-F40 gates.
   Evidence: `docs/ops/evidence/PRD6-B3H-P6F40A-cost-shortfall-local-20260713.md`.

## Previous P6-F20 Behavior Change

1. Add a bounded PostgreSQL `internal-outbox.v1` with leased multi-replica
   workers, priority/fairness, retry/dead-letter policy and lag metrics.
2. Enqueue prepared/settled/cancelled minimal usage in the authoritative
   financial transaction and materialize one retained usage journal plus
   incremental project-budget truth.
3. Materialize sanitized call logs, traces, audits and tenant Webhook delivery
   through destination idempotency keys, retaining direct evidence fallback when
   enqueue is not accepted.
4. Reconcile Agent, distributed-policy and weighted-workload terminal state from
   committed financial truth without storing bearer capability material.
5. Add guarded migration `000027` and explicit required-CI PostgreSQL/race gates.
6. D6-12 assigns mid-stream process-crash checkpoints, explicit interruption and
   client recovery to P6-F11. P6-F20 does not guess exact usage before final
   persistence and makes no managed-HA, target, exporter or production claim.
7. Evidence:
   `docs/ops/evidence/PRD6-B3-P6F20-durable-outbox-local-20260712.md` and
   `docs/planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md`.

## Previous P6-F09 Completed Scope

1. Normalize only authoritative OpenAI, DeepSeek and Anthropic Prompt Cache
   token fields into exact ordinary/read/create partitions; reject impossible
   partitions and never infer a discount from unknown fields.
2. Add platform-owned per-model read/create ratios defaulting to `1`, include
   them in a `pv2_` immutable price receipt, and reserve against the most
   expensive frozen class.
3. Settle, replay and recover the exact token partition and class costs with
   one idempotent terminal financial state.
4. Preserve the provider schema/status, frozen prices and class costs in logs,
   trace evidence and reconciliation without changing tenant boundaries.
5. Give operators plain-language price controls and separate provider Prompt
   Cache evidence from response-cache semantics and preflight variance.
6. Add guarded migration `000026`, real PostgreSQL financial/rollback tests,
   fixture-only provider contract tests and explicit required-CI gates; keep
   response Cache Service, deployment, real provider calls and P6-F20 out of
   scope.

## Current P6-F09 Behavior Change

- OpenAI cached/read and newer cache-write fields, DeepSeek hit/miss fields and
  Anthropic uncached/read/create fields use versioned source schemas and exact
  non-negative partitions; unknown cache-like fields remain ordinary input;
- platform administrators own read/create ratios, both default to `1`, the
  organization multiplier still applies, and the frozen `pv2_` receipt records
  all four effective prices before wallet mutation;
- settlement, durable stream-finalization recovery and replay carry the same
  partition and reject conflicting evidence while preserving one terminal;
- request logs, traces, reconciliation and UI distinguish ordinary, cache-read
  and cache-create tokens/costs and explain that total variance is actual minus
  preflight estimate;
- migration `000026` backfills legacy rows as ordinary input, permits old
  writers only before activation and refuses destructive down after a
  non-default ratio, `pv2_` reservation or cache-aware log exists;
- full backend/race/vet, real PostgreSQL billing and migration, full frontend
  tests/type-check/build, OpenAPI parse and diff checks pass locally; PR #174
  exact-head CI and final post-fix main run `29181137210` pass Backend Tests,
  Frontend Build and Docker Build; and
- evidence:
  `docs/ops/evidence/PRD6-B3-P6F09-prompt-cache-billing-local-20260712.md`.

## Previous P6-F08 Behavior Change

- equal-priority traffic is distributed by static weight without allowing a
  client routing hint, while lower priority tiers remain failover-only;
- the same server request ID produces the same ordered plan across replicas;
- normal successful requests reuse a one-second snapshot instead of querying
  PostgreSQL, and database refresh failure uses a bounded 30-second LKG before
  failing closed;
- half-open dispatch requires one renewable PostgreSQL lease, including when
  upstream activity exceeds the base lease;
- routing, circuit and key epochs prevent stale completions from healing newer
  state, and the configured channel degradation threshold drives transitions;
- Chat and Embeddings share the plan; valid output rather than HTTP 200 headers
  confirms non-stream/stream health;
- platform preview/UI/trace expose safe candidate, exclusion, snapshot,
  priority and weight evidence without URLs, keys, payloads or probe tokens;
- unit, race, full backend, real PostgreSQL routing/migration, full frontend
  tests/type-check/build, vet and diff checks pass locally; PR #172 exact-head
  run `29178419505` and post-merge main run `29178588075` passed Backend Tests,
  Frontend Build and Docker Build; the historical implementation branch is
  restored at `0e374d3`; and
- evidence:
  `docs/ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md`.

## Previous P6-F21 Behavior Change

- two- and three-replica real Redis fixtures enforce the same global/org
  weighted totals instead of multiplying capacity by replica count;
- stream work consumes four units and cannot consume the reserved short-work
  region, while fair virtual finish eventually coalesces enough free units for
  long work under continuous short contention;
- a noisy tenant has a bounded queue and cannot block another tenant's usable
  capacity or queue space;
- lease expiry recovers crashed holders, Redis uncertainty consumes at most
  one short pre-issued permit, and cancelled callers cannot consume it;
- mixed configuration fingerprints return typed 503 until old work drains;
- full repository race, CI-style PostgreSQL/Redis integration, 28 frontend
  files / 141 tests, type-check and build passed locally; PR #170 exact-head
  Backend/Frontend/Docker CI and post-merge main run `29176358459` passed, while
  local Docker remains unavailable; and
- evidence:
  `docs/ops/evidence/PRD6-B1-P6F21-weighted-workload-admission-local-20260712.md`.

## Previous P6-F07 Behavior Change

- Chat and Embeddings count every authenticated call against a shared API-key
  rate window before more-specific policy/capacity denial can be amplified;
- all matching selectors tighten monotonically, with exact rolling RPM/RPS,
  reserved-then-actual TPM and expiring shared concurrency across replicas;
- server-owned API-key labels cannot be injected by a request or Agent claim;
  stale snapshots and overlong policy chains fail closed;
- shadow decisions, degraded last-known-safe admissions and enforced denials
  emit distinct sanitized evidence under one policy-set fingerprint;
- platform/org administrators mutate their allowed scope with optimistic
  versions; billing administrators read only; the then-current 100 global/10
  org concurrency ceilings were visible and are superseded for model calls by
  P6-F21 weighted capacity;
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
- P6-F19 is CI verified / closed. P6-F20 was closed separately by PR #177;
  P6-F07 and P6-F21 are independently CI verified / closed;
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
- D6-10 dynamic reservation extension and P6-F11 semantic interception were
  later closed by PR #189; this transport slice did not claim them early;
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
  required jobs. P6-F18 is CI verified / closed. P6-F20 was closed separately
  by PR #177 and post-merge main run `29194150134`; no target or production
  claim is made.

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

At this Accepted Scope Closeout checkpoint, no deployment, target
mutation/probe, real or paid request, credential change, exporter/outbox,
deletion or expanded data exchange had occurred. The later separately
authorized target run satisfied the concrete route/failover detail for one
historical request under P6-E01; migration down and TLS 443 remain later and
require their existing item-specific authorizations even though PRD6 source
scope is closed. PRD7 implementation is also deferred; historical
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
