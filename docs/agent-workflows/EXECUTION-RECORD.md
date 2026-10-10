# Current Execution Record

## Prepared native maintenance acceptance — 2026-10-10

The M125-to-M126 test-only harness and scoped GitHub workflow are prepared
locally; see [the reviewed scope](../ops/COMMERCIAL-LICENSE-RETIREMENT.md#prepared-disposable-maintenance-acceptance--2026-10-10).
Independent pre-code and single closing reviews are complete. The three
required test corrections and offline checks passed. The owner explicitly
approved committing and executing this separate disposable GitHub maintenance
batch on 2026-10-10, including temporary keys, service lifecycle, synthetic
migration and independent backup restore. The earlier fresh-only approval is
not extended by inference. One Ubuntu 24.04 amd64 job is bounded to 45 minutes;
the existing account budget and hard stop remain. No ECS, provider call or
official publication is authorized. Native results are pending. Product
version remains 1.10.0; experience file needs no change.

The existing dependency-pin head `6bd3ae192c9e30fc17b68fc3bace5a7fcdc86497`
passed all seven PR checks: CI 38054463214, protocol 38054463203 and native
installer 38054463238. These are private PR/fresh-install results, not the
prepared maintenance test, exact-main, public distribution or Production.

Native runs 38058875535 (`bb9b26be`) and 38064314648 (`5e2d3543`) stopped
before builds/keys/database startup. The second report established 12.6 GiB
free but 83 percent used, above the original 80-percent limit. The test-only
follow-up uninstalls three exact, verified unused NDK packages through the
preinstalled package manager on the disposable GitHub VM. The original storage
thresholds and all deployment owners remain unchanged. Offline checks prove
exact uninstall arguments, refusal on self-hosted machines, mismatched package
identity and a package manager outside the expected SDK; no real local
uninstall or service operation ran. Native upgrade/restore remains pending.

## Licensed dependency repositories and contributor CI — 2026-10-10

The owner approved both complete integration-control and event-connector-spec
repositories under Apache-2.0, matching UR. This supersedes the prior unresolved
rights/consumer-kit decision. X-Insight remains a later open-source plan;
no X-Insight source or runtime is changed. Work stays in private candidates: [integration-control PR 1](https://github.com/foigent/integration-control/pull/1) and [event-connector-spec PR 10](https://github.com/foigent/event-connector-spec/pull/10).

integration-control `52bf197fafef2c55ff7717887b2396fa9df7f877` adds 14 files
to `db10730d64a9302887ad59c53921e98c757927e7`. event-connector-spec
`e01527350ab38d02c2bfdefe090ff72d6e48e749` adds 13 files to
`ad8fe7a38c9d7335657630baa8670dd07556b98c`. Every previously tracked byte
and file mode is unchanged in both repositories, including release manifests,
original READMEs, scripts and workflow files. English `.github/README.md`
provides the public entry without rewriting a release-bound root README.
Both retain vendor licenses and include explicit historical grant/delivery
instructions. Separate accompanying license review archives are outside N1's
strict five-asset input; no historical artifact or tag was rewritten.

Local component verification passed on those exact commits: full N1 D1 with
default clean-checkout and official-origin checks, tests/race/vet/schema/fixtures,
two equal builds and refusal/idempotence checks; full protocol Authority Kit
with immutable predecessors; and Git-less action snapshot Consumer Kit with
83 current cases / 29 classes. The actual snapshot includes the license files.
34 and 41 relative links respectively, forms and exact Apache text equality
passed. Evidence is retained under
`/private/tmp/unirouter-component-opensource-20261010/`.

Independent pre-code review by `/root/opensource_precode_review` required
separate CI-carrier and runtime protocol identities, complete accompanying
historical license materials, and honest fork versus upstream gate claims.
A follow-up read-only review confirmed the existing provenance test must pin
the new action carrier separately. UR updates only the two workflow carrier
pins, N1 tree and that exact test expectation; runtime constants, Source facts,
product pin, golden bytes, permissions and test coverage remain unchanged.
The public authentication patch stays inactive while dependencies are private.

UR documentation head `096e15343e2c8fb9348240fa30757cc9c990d78b` passed
all seven checks: CI 38044957141, protocol 38044957124 and native installer
38044957126. The new dependency-pin batch needs its own CI. Local UR provenance-passport tests, the snapshot product-pin verifier, all 19 release-policy checks, workflow YAML, changed-section links and unchanged runtime-identity checks passed. Existing Actions
budget headroom and hard stop were verified before private candidate pushes;
no budget or required check was changed. Public visibility, anonymous reads,
real fork execution, reporting availability, official assets, merge and target
deployment remain distinct publication/acceptance actions.

This batch changes license/community documentation and CI carrier bindings,
not product behavior. Product version stays at the existing unreleased 1.10.0.
Experience file: no change required.

## English-first GitHub entry and accepted installer evidence — 2026-10-10

The owner reaffirmed English as the primary GitHub language. The default
documentation index now follows understanding, installation, first request and
contribution, with the complete previous Chinese engineering navigation
preserved in `docs/README.zh-CN.md`. README language links, contributor naming
guidance and Issue/PR template hints are aligned. Owner-authored rationale and
source comparison content remain unchanged.

The owner selected GitHub private vulnerability reporting and supplied the
conduct email now in CODE_OF_CONDUCT.md. No email was sent and no account or
credential was created. GitHub reporting availability still needs verification
during an approved publication action.

Successful native run 38039244599 binds exact head
`b7432803a8ae7ef3b946501ae5f3e9ff0a905273`; CI 38039244481 and protocol
38039244482 bind PR merge commit `a66e19c54955130801de4292b51a356cab1d310c`
with the same tree `0c52df41786c09bc5c5fe1078e45788f5278c137`. All seven
checks passed. The installer runbook records scope, package identity and
limitations. This supersedes pending-native status in earlier entries while
preserving their failed candidates and historical results.

Release readiness now identifies the two private conformance dependencies and
their proposed public consumer-kit boundary, plus source dependency inventory
and Redis/Grafana license findings. This does not authorize copying, relicensing
or publishing other repositories, replacing dependencies or weakening CI.

Local verification passed: 378 relative links across 20 public-entry documents,
all previous Chinese index links and content preserved, owner essays/comparison
unchanged, Issue form YAML parsing and unique field IDs, current release
metadata, and whitespace checks.

This batch changes documentation and community template text only. Product
version remains 1.10.0; runtime, installer, CI workflows and required checks
remain unchanged. Private commit/push/PR authority continues. Merge, visibility,
official artifacts and deployment remain separate. Experience file: no change
required.

## Native frontend health correction — 2026-10-10

Diagnostic native run [38038691565](https://github.com/foigent/unirouter/actions/runs/38038691565)
for `3a30ddd0e38b9ce667c3f26f4bff6da265cf9819` isolated the failed startup:
frontend running with zero restarts, localhost/IPv6 readiness probes failing,
and the same endpoint succeeding through IPv4 loopback. Backend, PostgreSQL
and Redis were healthy; monitoring services were running. The installer had
recorded fresh-apply, release-registration and administrator-rotation steps,
but the post-install authority assertions had not run. This is retained failed
evidence, not installation acceptance.

Independent pre-code review from `/root/installer_precode_review` validated the
one-line customer frontend healthcheck correction to `127.0.0.1`. NGINX and
backend behavior, endpoint, retries and timeouts remain unchanged. Existing
native assertions provide behavioral regression coverage; no string-only test
is added. VERSION stays at the same unreleased 1.10.0 candidate, already higher
than main 1.9.8; its CHANGELOG records the correction. Public-facing guides
refer to the exact native workflow result and keep public distribution separate.

The corrected candidate requires a fresh signed synthetic package and native
run. Failed fixture identities remain in their original runs. Existing offline,
release metadata and Compose checks remain applicable. No Target, Provider,
Production or public publication claim is made. Experience file: no change
required.

## Disposable Linux installer acceptance — authorized CI batch — 2026-10-10

The owner explicitly approved GitHub-hosted disposable Ubuntu acceptance with
synthetic data, temporary signing keys and containers, within the existing CI
budget and hard stop. ECS, provider calls, official releases, registry
publication and permission/budget changes remain excluded.

This batch adds a separate scoped PR workflow and a CI-only harness that builds
the exact release-mode backend and frontend, prepares a real signed package
through existing CLIs and runs the shipped installer. It verifies read-only
checks, wrong-key/payload/resource refusal, a real fresh plan followed by
response-loss interruption and normal resume, preserved plan/config/database
identity, M126 and release registration, one admin password rotation, completed
resume without changes, loopback login and retired-License status. Only an
allowlisted summary leaves the disposable runner; raw logs and test secrets do
not. This does not establish provider usage, official downloads, ARM64 or
existing-database maintenance acceptance.

Independent pre-code review and the single closing review were completed by
`/root/installer_precode_review` against the design and exact candidate. The
closing review identified the root/runner Git ownership failure in helper Go
builds. Both test-helper builds now disable implicit VCS stamping; release
backend commit/tree/version binding is unchanged. No other validated blocker
was found. Local syntax/help/host-refusal, generated-wrapper syntax, workflow
permissions/identity/artifact checks and whitespace checks pass. Native CI is
pending, and its eventual result must be reported against its exact commit.

First native run [38038209649](https://github.com/foigent/unirouter/actions/runs/38038209649)
at `bd4d4b99461a775a54492f9fef373e8af725a02c` built both real images and the
signed fixture, passed read-only/signature/collision checks and reached resume
after the actual completed plan. It failed at `start-console`; that is not an
installation pass. A test-only follow-up records allowlisted container health,
exit/OOM state, source-authored fatal prefixes and loopback probe return codes,
without raw logs or credentials. No product check or timeout is relaxed.

This is test-only CI and documentation work, with no product contract or version
change. The earlier documentation entry's pending approval is superseded by the
explicit approval above. Private commit/push/PR authority continues; merge and
public visibility remain separate. Experience file: no change required.

## Owner design rationale and publication follow-up — 2026-10-10

The owner supplied the English and Simplified Chinese Why UniRouter essays and
an English source-based gateway comparison. The candidate preserves the essays'
substance, fixes the Chinese overview link, adds README/index/roadmap entry
points and replaces outdated statements that the essay is still to be written.
The original authoring checkout and its unrelated modifications are untouched.

The comparison now binds UR to `c5958c57106e7073e74653652b38dc549f482e10`.
The implementation files it cites are unchanged from `35546e2`. The cited
LiteLLM and New API commits were checked against their live official Git trees;
all 11 cached source files used for review match their Git blob identities.
Review covered execution admission, reservations/settlement/recovery, usage and
source-fact meaning, capture limits and the parity of the two rationale texts.
It does not claim comparative runtime experiments or a complete product audit.

Private PR 442's exact `c5958c5` CI
[38032678754](https://github.com/foigent/unirouter/actions/runs/38032678754)
and protocol
[38032678752](https://github.com/foigent/unirouter/actions/runs/38032678752)
were read back as successful: Change Routing, Backend Tests, Frontend Build,
Browser E2E, Docker Build and Protocol Pin. The earlier two CI failures below
remain historical evidence; the complete latest run resolves their CI blockers.
Main remains `59c47b852b7372951c5c49b0e6b23df8c83817b9`, and the repository
remains private with this PR open as a draft.

The release-readiness guide now records concrete fork-CI dependencies,
frontend license metadata inventory, limited history-disclosure triage and the
unconfirmed private-reporting channel. The next native-installation batch is
prepared in the existing COMMUNITY-INSTALLER runbook for a disposable
GitHub-hosted Ubuntu runner, synthetic data and ephemeral test keys. A separate
user question requests the additional environment/execution authority; no new
workflow, service, key, image or target has been created by this documentation
follow-up. Existing ECS provider evidence keeps its original boundary.

Local review checked 115 relative links across 16 documents, all 13 pinned UR
source/readiness files against both commits, and whitespace. The English essay
is byte-identical to the owner's original; the Chinese essay changes only its
overview link.

This batch changes documentation only; product version, runtime contracts,
required checks, permissions and budget remain unchanged. It continues the
approved private commit/push/PR scope. Publication, merge, official artifacts
and target deployment remain separate. Experience file: no change required.

## Private PR 442 — CI timeout follow-up — 2026-10-10

The approved candidate was committed and pushed as
`35546e2ccabc954b9cbff4c7b446e542d46ee652`, and opened as private draft
[PR 442](https://github.com/foigent/unirouter/pull/442). Protocol conformance
[38026577534](https://github.com/foigent/unirouter/actions/runs/38026577534)
passed. In CI [38026577564](https://github.com/foigent/unirouter/actions/runs/38026577564),
Change Routing, Frontend Build and Browser E2E passed. Backend ordinary race,
source-evidence and admin integrations passed, but the consolidated migration
package exhausted its existing 20-minute package timeout while constructing
another historical fixture. Docker stopped at its prerequisite gate and did
not build an image.

Historical M125 fixtures bypassed the existing process-scoped database template
and replayed the complete historical install for every test. The follow-up
reuses the existing template mechanism with separate exact schema ceilings.
Each template still uses the strict artifact install; each test receives its
own clone. Current M126 and historical M125 schemas and mutable data remain
separate. The added regression checks distinct database identities, exact heads,
commercial trigger presence/absence and isolation after data/schema mutations.
This changes test fixtures only; no product migration, selector, CI timeout,
required check, deployment or installation contract is changed.

Local verification used only the previously approved synthetic PostgreSQL 16
and Redis fixture. The isolation regression and testutil race suite passed.
The unchanged consolidated runner passed 19 packages, including the complete
migration package in 329.633 seconds. One content-capture observation exceeded
its existing 250 ms admission deadline under that run; its failure remains in
`/private/tmp/unirouter-oss-validation.upqUNT/formal-integration-pr442-fix/`.
The same complete cmd/uniroute selector then passed separately (131 tests,
158.766 seconds); no deadline was relaxed. The remaining original groups passed:
source-media 849 tests, agentcap 1, limitpolicy 1 and workloadadmission 3.
Logs are `/private/tmp/ur-oss-pr442-{cmd-recheck,source-media,agentcap,limitpolicy,workloadadmission}.jsonl`.
The first cmd summary invocation rejected an undeclared group name after the
tests passed; its JSON log was retained and inspected, without rerunning tests
for a reporting error. This is combined Local evidence, not a claim that the
first consolidated invocation passed or that resource contention is proven.

Follow-up `7cc3165b410046174d17963c608c196f49db94eb` passed frontend,
browser and protocol checks. CI
[38031024083](https://github.com/foigent/unirouter/actions/runs/38031024083)
then passed 106 packages in the ordinary race step, but `internal/testdb` failed:
its source-order guard still expected the template helper's old two-argument
call. The guard is updated to the exact three-argument call while retaining
all checks that DSN/server preflight precedes database writes. Both testdb and
testutil race suites passed locally. That CI run did not reach the consolidated
group and does not prove the earlier timeout resolved.

The PR's latest exact-head checks remain the authority for CI acceptance.
Actions budget protection was rechecked without changing it. No merge, public
visibility change, release, Target operation or provider call is authorized by
this follow-up. Native Linux installation remains pending. Experience file:
no change required.

## Private review dispatch — approved, pre-dispatch record — 2026-10-10

The owner explicitly confirmed committing and pushing the prepared open-source
and Linux-installer candidate, creating a draft PR, and running existing checks.
This does not authorize merging, changing visibility, publishing releases,
deployment, production traffic or new provider calls.

Before dispatch, all 224 candidate files matched the prepared scope receipt
`/private/tmp/ur-open-source-private-pr-candidate-20261010.json`; generated
`frontend/output/` remains excluded and preserved. The only subsequent local
edit is this authorization/status entry. GitHub `foigent/unirouter` remains
private, main remains `59c47b852b7372951c5c49b0e6b23df8c83817b9`, and no PR
already exists for `codex/open-source-preparation`. Repository identity and
write access were verified; existing Git author configuration is retained.

Actions budget and usage were checked before dispatch. No billing figures are
included here; budget settings and required checks remain unchanged. This entry
records pre-dispatch checks; the resulting PR and its exact-head checks record
remote outcomes. Earlier Local evidence below remains scoped to its tested
files and conditions. Experience file: no change required.

## Linux/ECS community installer — local checks complete, native acceptance pending — 2026-10-10

The owner selected Linux servers/ECS for the first simplified installation.
Work continues in the same `codex/open-source-preparation` candidate at
`/Users/m4/.codex/worktrees/remove-commercial-license/unirouter`, baseline
`59c47b852b7372951c5c49b0e6b23df8c83817b9`, version 1.10.0. This is local
implementation and offline validation only. Existing ECS onboarding/model
evidence remains valid within its original versions; no existing target is
reinstalled or revalidated by this work.

The wrapper at `deploy/customer/install.py` accepts a prepared signed release,
checks every declared payload and six immutable image references, generates
independent private configuration and uses the existing fresh-install owner.
Initial release registration and one administrator password rotation precede
HTTP startup. Resume reconciles original DB facts and preserves secrets;
uncertain password rotation stops for readback without a second audit write.
The standalone verifier exposes `manifest_sha256` to bind the parsed complete
artifact list to the actual verified signature. No new DB authority or trust
bypass is introduced. The reviewed scope and producer inputs are in
[COMMUNITY-INSTALLER.md](../ops/COMMUNITY-INSTALLER.md).

English and Chinese installation guides are linked from their READMEs.
[Three starter task cards](../community/starter-issues.md) are local drafts,
not published GitHub issues: Chinese getting started, a standard-library
request example, and Chinese architecture. The owner need not write technical
issue descriptions or the deferred Why UniRouter essay for this step.

New Local evidence, separate from the prior License-removal checks below:
- 23 offline installer cases passed, including no-write checks, signed input
  binding, copy-time tamper, project/daemon collisions, secret preservation,
  initialization order and interrupted-result handling.
  `/private/tmp/ur-community-installer-offline-4.log`.
- Race-enabled full tests for `cmd/uniroute-upgrade` and
  `internal/upgradebundle` passed. The Python installer consumed real Go
  Ed25519 package verification with synthetic keys; valid, prepared-file
  tamper, wrong-key and package-tamper cases behaved as expected.
  `/private/tmp/ur-community-installer-signatures-final.log`.
- Release-policy 19 cases and current version/toolchain metadata passed;
  installer `--help`, local links in 14 entry/community documents and
  `git diff --check` passed. The delivery CI preflight includes the offline
  installer tests; no GitHub CI run has occurred.
- An initial Go invocation could not load its cached toolchain with the
  checksum database disabled. Using the already installed exact Go 1.25.12
  binary resolved this without downloads or installation.

Read-only independent pre-code review and the one closing review use
`/root/installer_precode_review`. Closing input receipt:
`/private/tmp/ur-community-installer-closing-review-20261010.json`, SHA-256
`ca1b6c5dd021f3180e39fcc16919aca0c901c343d150401607c66565883d9e54`.
The four validated blockers were verified and fixed: reject unlabelled exact-name
resource collisions, resume retained pre-claim evidence, bind effective Compose
images/resources to the signed descriptor and bind exact reread descriptor bytes.
Targeted regressions and signature interoperability passed after corrections.
The contract document records adjudication; no extra review round is claimed.

Docker is unavailable locally. A reviewed actual release, image artifacts and
an authorized disposable Linux environment are still required for real Docker
acceptance. No production signing, public download, npm package, commit, push,
PR, Issue, deployment, new service or provider call was performed. The original
checkout remains on `ac53699d29c3791a514a045fe064ab63ff9f99c0` with its prior
edits preserved. Experience file: no change required.

## Open-source preparation — prior License-removal local checks complete, publication pending — 2026-10-10

Baseline: `59c47b852b7372951c5c49b0e6b23df8c83817b9`, tree
`35dea6fce43ff8391d7bb8ef4a9a500efbf329bf`; private main was read back before
preparation. Candidate: `/Users/m4/.codex/worktrees/remove-commercial-license/unirouter`,
branch `codex/open-source-preparation`, version 1.10.0. Original checkout edits
are preserved. No commit, push, PR, visibility, release, deployment or Provider
call was performed.

Accepted direction: Apache-2.0 core; runtime commercial License retirement;
independent future commercial services/modules; bilingual entry and contribution
docs. UR owns execution/economic facts. X-Insight is optional asynchronous
evidence/investigation infrastructure with its own product/license boundary.
The owner will write Why UniRouter later.

Local implementation includes public documentation/templates, retired HTTP/UI
License projections, removal of production commercial middleware/refresh,
mandatory upgrade trust, M126 commercial-trigger retirement, exact runtime schema
admission, fresh-install progression and preservation of M118 budget coverage.
Authorization, qualification, billing and historical records remain required.

Independent pre-code review and one closing review are recorded in
[OPEN-SOURCE-PRECODE-REVIEW.md](OPEN-SOURCE-PRECODE-REVIEW.md). Closing review found
the final-receiver maintenance gap, two omitted M126 consumers and stale tests.
Source Facts/counter consumers and route tests are corrected. Additional unit
tests found and corrected an empty-requirement error in M126 manifest admission.

Verified Local, using synthetic PostgreSQL 16/Redis where applicable:
- Full ordinary `go test -race -p=2 ./... -count=1 -timeout=20m` passed:
  106 packages, 24 without tests. Log: `/private/tmp/ur-oss-backend-race-final.log`.
  This supersedes the initial unavailable-database run and its stale assertions.
- Historical migration/domain regressions passed after explicitly pinning
  pre-retirement fixtures to M125. Current runtime/fresh fixtures retain M126;
  production ledger validation and the unsupported M126 down guard are unchanged.
  The affected seven-package integration run passed billing, gateway,
  modelexecution, sourcefacts and resetexec; its 11 remaining migration/runtimechange
  failures were corrected and passed a dedicated race rerun. Logs:
  `/private/tmp/ur-oss-history-regression-4.log` and
  `/private/tmp/ur-oss-history-retest-5.log`.
- Real PG16 retirement checks cover receiving-fleet refusal, explicit empty
  ingress and ambiguous transitions, check without writes, retired fleet,
  M126 migration/rejoin, retained expired/low-limit entitlement history and
  preserved real constraints. The exact M125 maintenance CLI rejects nonempty,
  null and Production requests without mutation. CLI log:
  `/private/tmp/ur-oss-maintenance-cli-4.log`.
- Frontend Vitest passed all 96 files / 853 tests:
  `/private/tmp/ur-oss-frontend-final-tests.log`. Fixed Node 24.19.0/npm 11.17.0
  formal build passed type, API, React and bundle guards; the API guard passed
  again after OpenAPI edits. Logs: `/private/tmp/ur-oss-frontend-build-3.log`
  and `/private/tmp/ur-oss-api-contracts-2.log`.
- Three real-browser acceptance cases passed with the existing Chromium and
  synthetic services: access/readiness, model onboarding and source budgets.
  Commercial gates are absent; model-registration and permission requirements
  remain. Log: `/private/tmp/ur-oss-browser-retest-2.log`.
- Offline controller tests passed: ingress 15, retirement 9, rollout 81;
  backend runner selection/orchestration 9 and release policy 19 passed.
  Current-mode product/toolchain metadata and `go vet ./...` passed; vet log:
  `/private/tmp/ur-oss-vet-confirmed.log`.
- Fresh installation, process-interruption/resume through M126, real pg_dump /
  psql restore, budget-proof rebase, mixed-database refusal and no-write plan/check
  guards passed with race enabled. Four top-level cases, no skips; log:
  `/private/tmp/ur-oss-fresh-recovery-final.log`.
- The repository's existing five-group `scripts/test-backend-integration.sh`
  passed with race enabled: consolidated 20 packages / 956 test results,
  source-media 3 / 849, agentcap 1 / 1, limitpolicy 1 / 1,
  workloadadmission 1 / 3. Counts include subtests. Summaries and exact JSONL
  logs: `/private/tmp/unirouter-oss-validation.upqUNT/formal-integration-final/`.
  Outer log: `/private/tmp/ur-oss-formal-integration-final.log`.
  Of seven recorded skips, four restore cases required an explicit PG tool
  directory and subsequently passed dedicated race runs, with no skips:
  `/private/tmp/ur-oss-source-restore-final.log` and
  `/private/tmp/ur-oss-budget-content-restore-final.log`.
  The other three are two deliberate shared Responses recovery cases and the
  opt-in query-scale benchmark. No new performance-scale claim is made.
- Final `git diff --check`, current-mode release metadata and local links in
  the 12 public entry/community documents passed. Original checkout readback
  remains `ac53699d29c3791a514a045fe064ab63ff9f99c0` on
  `codex/minimax-payg-glm-onboarding`; its pre-existing edits remain untouched.
- No CI, Docker Artifact, actual Docker/NGINX maintenance, Target, real Provider
  or Production acceptance is claimed.

User approved task-local PostgreSQL 16/Redis synthetic instances and temporary
npm under `/private/tmp/unirouter-oss-validation.upqUNT`. A later explicit approval
allowed only shared-memory segment 1900545/key 0x0b0a2d4f to be removed after
rechecking zero attachments and creator PID 38490 absent. Those checks passed;
only that segment was released. PostgreSQL 16.14 (65266) and Redis (65267) ran
only in that directory. After all tests, the exact data directory, ports,
PIDs 59441/59455 and Redis run identity were rechecked; PostgreSQL had zero
other client connections. Native pg_ctl/Redis shutdown stopped both services,
and both PIDs were confirmed absent. Synthetic data and logs are retained.
No existing service or system parameter was changed.
An additional synthetic, non-superuser role is used for the acceptance cases
that require it; no real credentials or business database are involved.

Automatic approval review previously rejected a rollout-script patch as outside
the earlier temporary-environment authorization. The user subsequently explicitly
approved maintenance-controller code and offline tests, limited to development/test.
The rejection is resolved for local code only. No Docker, Target, stop, removal,
deployment or migration operation against a real environment is authorized.

The local maintenance path now supports canonical full withdrawal, independent
Artifact CLI control after both receivers exit, clean-exit/removal proof, and
unknown-result readback. Its offline host tests do not establish real
Docker/NGINX acceptance. Source-budget restore/rebase recognizes M126 while
retaining the original M118 certificate and validating the exact predecessor
and absence of the five retired triggers. The customer helper hash is updated.

Required remaining work:
- Complete actual Docker image/native installation and Docker/NGINX maintenance
  acceptance in a separately authorized environment. Docker is unavailable on
  this host; the local-code approval expressly excludes running those operations.
- Validate the affected first-use path of the new installation under its own
  Provider/credential allowance before claiming the public journey complete;
  reuse unchanged model behavior from the recorded ECS evidence.
- Resolve private Fork CI dependencies, rights/history disclosure and confidential
  reporting before any public release. Private conformance cannot be skipped
  or copied to manufacture green CI.

Experience file: no change required.


## UR 1.9.7 DeepSeek output successor — local candidate — 2026-10-09

**Scope and baseline.** The accepted correction preserves the current explicit partial-estimate financial contract. Baseline/main readback: `c9bbaa106fe24e2468963eb5165a91d1654062d7`, tree `fa6b2163da5812f79ef358fca12151a82faa21eb`; GitHub `foigent/unirouter` main was read-only rechecked at 2026-10-08T19:57:11Z and still matches. The independent candidate is `/private/tmp/ur-deepseek-output-candidate-20261009`, branch `codex/deepseek-output-final-local`; no commit, push, PR, CI dispatch, Publication, Release, Target or paid Provider write was made. Original user-owned source changes are not copied or overwritten.

**Implementation identity.** Product1.9.7 metadata is consistent. New protocol `protocol.deepseek-openai-chat.v6` selects the existing `provider_completion_final_v1` engine with deviation0, sole `max_tokens`, minimum1, scope `generated_completion`, decoded16MiB/wire128MiB resource bounds. Public/default/frozen/emitted2048 is verified by the actual data Profile path; Profile-level provider384000/default32768 metadata is retained, and is not authorization to widen the future public Release's2048 limit. New Profile `deepseek.v4.1-flash.api@1.1.0` retains current floating `deepseek-flash`, lineage `deepseek/deepseek-flash`, Flash reasoning v5 and cache v1. Definition digest `2751c693d24905f1845ebb64253322e4cdaed5b327182cfd55890f5c72575a99` pins predecessor `6c6c7ae34b6a6dcf69b0a3a6b382a758ec04602b50067bb2bae927c76bf4a33f`. New unpublished canonical Bundle `deepseek-v41-flash-provider-completion.json`:7730bytes, SHA-256 `bc2baa2dd0f134c16f1e09a07ad39ec3da03d2dfde912c7c5b77051174819062`; six bindings/selectors share new `unirouter.capability-probe.deepseek-chat-stream-tools.v2@1.0.0`, request2048/thinking enabled/high, no redundant tool_choice. No old evidence is reinterpreted.

**Shared mechanics and protection.** Runtime and Probe select the existing exact DeepSeek preparer and shared function-tool parser/state; v6 requires usage on the empty finished terminal choice, complete tool JSON, valid counters and `[DONE]`. Provider completion<=B can finish despite text bytes>B. Independently known completion>B reuses the existing provider-evidence hold. Missing/unusable terminal without known overrun retains estimated partial settlement/observation; no new final-only guard, reservation-as-charge-ceiling or Key hold is added. Billing/auth production source, schema/checkpoint code and relay production code are unchanged. Existing safe error codes/messages and diagnostic fields suffice. The v4/1.5 clean-EOF compatibility resolver selects neither current Flash v5 nor v6: that historical exception is not a confirmed cause of this incident. Current v5 already requires DONE.

**Independent review.** Pre-code exact design review has no validated blocker; required finite identity, selected diagnostic values, shared terminal shape, exact preparer/Probe bindings and historical protection were applied. Record: `docs/agent-workflows/reviews/deepseek-output-precode-independent-20261009.md`. The overall owner independently reviewed the production candidate and requested complete real-Definition Route identities; both old and new test paths now carry their actual Bundle Definition, Profile/version, operation/protocol/version, provider/upstream/cache. This entry does not claim the overall owner's final frozen-patch review is complete.

**Original failure counterexample.** The same constructed stream, containing a12288-byte reasoning delta after an initial delivered frame, fails at the2048 byte boundary under the actual original1.0.0/v5 Definition and finishes under1.1.0/v6 with authoritative fixture completion20. The new path without terminal usage sends estimated counter-v3 completion3090 (>2048) to the existing settlement mock, with no overrun hold. These are synthetic transport/counter fixtures, not captured Supplier tokenization, paid usage, actual bills or PG transactions. The original real incident receipts remain separate: four continuations were cut by the byte boundary, and the earlier native optional=false argument rejection is not fixed by this candidate.

**Actual checks.** Go1.25.12 uses cached modules, GOPROXY/GOSUMDB off and a temporary GOCACHE. Where ordinary HTTP tests need a loopback listener, sandbox execution was escalated only for local tests.

- `go test ./... -count=1`:105 packages passed; the only failed package was `cmd/uniroute` because new identities were missing from OpenAPI enums. Both enums were corrected; full `go test ./cmd/uniroute -count=1` passed. The affected six packages also passed their ordinary full suites. This evidence excludes actual PostgreSQL tests that skip without a database.
- `go test ./cmd/uniroute ./internal/gateway -run 'TestOpenAPI|TestDeepSeekProviderCompletion' -count=1 -v`:passed, including original/new real identities, B1/2048/default, repeated preparation, bad aliases/above-public reservation, streamed success, overrun, missing usage/DONE, invalid finish/tool JSON, terminal fresh data/usage-only, invalid/duplicate usage, and non-stream withholding of known overrun.
- Focused `-race` for modelprofile/adapters/qualification/gateway/runtimegeneration:passed, including old Qwen/current Flash controls. A Darwin linker warning was non-fatal. New chat/Probe requirements are advertised by the existing registry; removing either denies successor support while retaining old support.
- Offline Bundle source checker:canonical Bundle/bindings passed. Release metadata current-mode checker and19 policy/router tests passed. `git diff --check` passed.
- Existing Node24.19.0/npm11.17.0 binaries from `/Users/m4/.openclaw/tools/node-v24.19.0` were verified and used only by the candidate build process. Lockfile `npm ci` installed517packages in the candidate with a temporary cache; no OpenClaw/global configuration or toolchain was changed. `npm run type-check`, `npm run build` and `npm test -- --run`:passed;96test files/854tests; bundle budget94JS assets, maximum362383bytes<460800. Earlier npm10.9.2 toolchain failure was resolved using the already installed exact versions. Only public npm metadata was queried during discovery; no new toolchain was installed.
- `go test -tags integration ... -run '^$'`:affected gateway/billing/admin/modelprofile/qualification/runtimegeneration compiled. DeepSeek2048/2049 hold/recovery cases cover both billing modes; existing counter/recovery cases now include reserved2048/estimated3090 in prepaid and observation, zero charged observation and no added provider-pending state. These tests have not executed against PG in this batch.
- Existing consolidated Backend runner now explicitly selects `TestStreamCounterVersionPinRecoveryAndReplay` as well as the already selected `TestProviderCompletionRelayHoldSurvivesRecoveryIntegration`;9 offline runner orchestration tests passed. Backend CI sets REQUIRE_TEST_DATABASE=1 for this group. Actual Go negative execution with that flag and an empty DSN failed all8 selected cases with `REQUIRE_TEST_DATABASE requires explicit TEST_DATABASE_URL`, zero skips, before database setup. This proves fail-closed configuration/selection, not PG behavior.

Evidence logs are `/private/tmp/ur-deepseek-output-{focused,packages,full-go,final-cmd-regression,final-focused,race,integration-final-compile,missing-pg-negative,runner,frontend-ci,frontend-typecheck,frontend-build,frontend-tests}-20261009.log` and `/private/tmp/ur-deepseek-release-metadata-20261009.log`. The frozen full patch and per-file SHA receipt are `/private/tmp/ur-deepseek-output-candidate-20261009-r2.patch` and `/private/tmp/ur-deepseek-output-candidate-20261009-r2.receipt.json`; their digest is recorded externally to avoid a self-referential patch digest. The first04:05 local patch/receipt/file-list snapshot is retained without overwrite; r2 supersedes it after removing the locally invented external budget/window, clarifying the actual independent-review source, and replacing the fixture trailing blank EOF with a harmless synthetic-evidence SSE comment. The same gateway/qualification fixture regressions passed again after that fixture edit; subsequent documentation corrections reuse unchanged code checks.

**Required gaps.** PG16:port55437 has no response and TEST_DATABASE_URL is absent; no DB/role/service was created or restarted, no financial/recovery transaction is claimed. Docker and its daemon are unavailable; Backend/Frontend runtime images, Nginx and Browser E2E were not tested. No CI/exact-main protocol, Artifact, Target, Publication, real Provider or Production claim follows from local checks. Historical DeepSeek/Qwen Bundle bytes and existing Release interpretation remain retained; no new Release was authored or activated. Existing paid-call/credential approvals are not expanded.

**External application owner.** The overall owner alone prepares the external upgrade/live-acceptance application, using the existing `p3-deepseek-current-flash-plan-20261008.md` subsequent-operation draft and its actual cumulative approved-call/fee scope. This local candidate creates no additional call count, fee limit, native-turn count or validity window. It provides the exact successor/patch/check evidence for that single application; it does not execute or authorize submission, CI dispatch, Publication, Target changes, credential use or paid calls. Before any approved external action, the overall owner must revalidate exact live identity/compatibility and retain the old Release/rollback; historical incident IDs are navigation, not current Target proof. Other providers, token-budget increase, suppression of thinking, broad native-schema repair and unrelated XI work remain outside this correction.

**Experience review.** Refined existing MAR-001 (units), MAR-003 (shared parser and complete actual Route identity), MAR-004 (estimated partial versus known-overrun branches). No new broad lesson, retirement, finance implementation or lesson-admission gate was added. PG transaction evidence remains expressly pending.

## UR 1.9.6 independent current Flash Local candidate — 2026-10-08

**Scope and baseline.** Approved option A adds a new public-model identity for
current official Flash without migrating an old public call name across
Profiles. Source starts at `14cf857804675d2eca8beccafa4ac5174d1c85e8`
(version 1.9.5; tree `0756fb1dd08fde24e69aee92ac1b532ce73f42ff`). Its tree
matches the 1.9.5 main merge; this batch does not fetch, push, merge, deploy,
sign, read supplier credentials, or call a real Provider. Version 1.9.6 is a
local source candidate. No migration, account system, vision, Responses or
cross-Profile successor is added.

**Exact immutable data.** `capability-bundles/deepseek-v41-flash.json` carries
`deepseek.v4.1-flash.api@1.0.0`, lineage `deepseek/deepseek-flash`, with no
predecessor. The only requested upstream is `deepseek-flash`, family
`deepseek-flash`, version policy `floating`; the label describes current V4.1,
not a pinned upstream version guarantee. Definition SHA-256 is
`6c6c7ae34b6a6dcf69b0a3a6b382a758ec04602b50067bb2bae927c76bf4a33f`.
Bundle SHA-256 is
`c3ef35d6c082f924c4a817976a75f383487b87634c2b571cc3757432cd102b7a`
(7,642 canonical bytes). The Bundle has one Definition, six bindings and six null-predecessor selector
transitions: v1 streamed-tools for streaming/reasoning/tools/function_tools/
tool_choice_auto, and v3 for reasoning_effort. Every binding pins the exact
Definition, Profile/version, protocol and upstream. Historical Flash data,
selectors, Probe v1/v2 and sealed Releases are retained; an old model response
cannot satisfy the new identity.

**Implementation and review.** Existing protocol v5, Flash reasoning v5 and
cache v1 support this bounded text streamed-tool composition. The v3 predicate
checks those explicit components plus the sole current upstream/floating
family, without hardcoding a Profile ID/version as general adaptation. v3 is
registered in runtime support and the shared-group gate, and explicitly emits
thinking enabled/high with include_usage and no tool_choice. Existing safe
request/response summaries carry the Probe/Profile/protocol tuple, effort and
diagnostic allowlists. The immutable binding resolves its unique Definition;
GET reads the exact immutable operation Definition, and Release seals its digest
and evidence pins. Source protection hashes and request summaries are distinct;
no source protection hash or request-summary format changes in this batch. Independent pre-code review
required the floating identity, upstream predicate and exact HTTP lifecycle;
all were accepted. The one closing review found a CI fixture blocker: CI's
service administrator would fail the new unconditional non-superuser check.
The source fix prepares only a synthetic job-local Flash role and passes its
DSN only to this test via `CURRENT_FLASH_TEST_DATABASE_URL`. The test installs
through the exact current migration head in its own isolated database, avoiding
a package-template DSN change; other tests keep their original CI DSN. This CI
configuration has not executed. The local role setup used the subsequent
explicit approval recorded below. No real Provider conformance is claimed.

**Verified Local evidence.** The new Bundle passes the formal
`go run ./cmd/uniroute-capability-bundle-check` entry. Probe tests cover canonical
digests/selectors, v3 registration/request, alternate data address under the
same exact composition, retired v3 rejection, malformed/missing/duplicate or
mismatched model, bad JSON, missing/conflicting usage and missing DONE; existing
v1/v2 tests pass unchanged. `TestCurrentDeepSeekFlashFormalPreflightAndRuntimePreparation`
passes the formal preflight owner with payload retention and the Runtime
outbound preparer, checks exact deepseek-flash, default qualification bindings,
auto omission and zero execution effects during preflight. Affected package
regression passed for modelcapabilityqualification, modelprofile, adapters,
gateway, admin, runtimegeneration, modelcapabilitypublication and cmd/uniroute.
The first sandbox run blocked loopback httptest listeners; only the three
failed packages were rerun with local listener permission and passed.
The runtime-support v3 inclusion/rejection regression passes. Logs are
`/private/tmp/ur-deepseek-recovery-20261008/v41-offline-regression.log`,
`v41-http-regression.log` and `v41-final-unit.log` in the same directory.

The frontend source changes only version metadata. Using the bundled exact
Node 24.19.0 and the existing dependency tree (dependency/devDependency maps
match), direct TypeScript, API-contract, React compatibility, Vite production
build and asset-budget checks passed. Local npm is 10.9.2; the npm 11.17.0
wrapper toolchain check was not run or claimed. The generated assets are local
verification output, not an immutable Artifact. Release metadata and diff
checks pass. The 19 release/CI-routing regressions and nine backend-integration
runner checks pass; the updated workflow YAML parses successfully.
No CI, Docker, signed Artifact, Target or Production evidence is
added here.

**Actual PG16 acceptance and cleanup.** The task-owned cluster was
`/private/tmp/ur-xi-control-lifecycle-pg-20261006`, port 55437, system ID
`7693457567414990508`, PostgreSQL 16.14. Initial discovery found only `m4` and
`xinsight_admin`, both superusers. Automatic review rejected adding a role under
the earlier reuse-only authority; the subsequent explicit instruction approved
creation and cleanup of the necessary isolated test role. PostgreSQL's existing
CLI prepared `ur_v41_flash_local_test` with LOGIN/NOSUPERUSER/NOCREATEROLE/
CREATEDB/NOREPLICATION/NOBYPASSRLS and no password in this local trust-only test
cluster. SQL through that role returned
`ur_v41_flash_local_test|160014|f|t`. The existing test preparation owner created,
installed and removed temporary databases; no administrator connection was
used as the test's runtime pool.

`TestCurrentDeepSeekFlashDataFirstHTTPPublicationQualificationActivationIntegration`
passed with the formal materializer, a separate model reusing an existing
connection/binding ID without a supplier secret, individual Probe POST/storage/
GET diagnostics, HTTP group plan/execution/readback, six independent approvals,
six exact batch pins frozen in a new Release, normal initial activation,
preflight and real Relay/coordinator/billing owners with a fake Provider.
The batch plan has two groups: v1 five capabilities plus v3 effort. Actual
execution dispatches two synthetic requests, produces six exact facts across
two source refs, and freezes those six exact Probe IDs in the Release. Readback,
approval and freeze do not dispatch. Individual diagnostic fixtures and the
Relay fixture are additional local synthetic validation requests, not the
minimum supplier-call plan.

**First failure adjudication.** The initial test incorrectly expected
`request_summary.profile_definition_digest`. That field is not in the existing
request-summary contract. A provisional five-line field addition was withdrawn:
M114 enforces a unique immutable Profile address, the stored Probe tuple joins
its immutable binding/digest, GET resolves the operation's immutable Definition,
and Release seals the digest plus binding/evidence/selector pins. The corrected
test asserts these formal owners, stored upstream and source fingerprint,
instead of introducing a redundant product field to satisfy a self-created
expectation. The final request-summary writer is byte-identical to the baseline;
this was a test-contract error, not a missing source-protection hash or product
identity defect.

The final PG16 `-race` regression covered the new chain, old existing-family
Probe dispatch/persistence (DeepSeek/Kimi/MiniMax/Qwen), uncompiled data-first
creation through activation, and shared-group success/data-definition/drift/
unknown-outcome/invalid-response/atomic-rollback/readback/independent-approval
scenarios. All four selected tests and their subtests passed, with no skipped
subtest. Log: `/private/tmp/ur-deepseek-recovery-20261008/v41-pg16-final-race.log`.
After withdrawing the provisional product field and correcting the identity
assertion, the changed new chain alone passed again with `-race`; its final log
is `v41-pg16-final-contract.log` in the same directory. Relevant old-path
regressions remain applicable because the writer reverted exactly. Admin/API
contracts also passed in `v41-final-admin-contract.log`.

The execution entry used an explicit DSN and refused missing/non-PG16 DBs:

```bash
REQUIRE_TEST_DATABASE=1 \
TEST_DATABASE_URL='postgres://ur_v41_flash_local_test@127.0.0.1:55437/postgres?sslmode=disable' \
GOCACHE=/private/tmp/ur-deepseek-recovery-20261008/offline-go-cache \
go test -tags=integration -race ./internal/admin \
  -run '^TestCurrentDeepSeekFlashDataFirstHTTPPublicationQualificationActivationIntegration$' \
  -count=1 -v
```

This command requires approved test setup before reuse; the temporary role was
subsequently cleaned up. SQL confirmed zero owned databases and zero role
connections before DROP ROLE, then confirmed the role no longer existed. The
same PG16 `pg_ctl` stopped the cluster with `-m fast`; `status` confirmed no
server running. Final control data retained system ID `7693457567414990508`
and reported `shut down`, restoring its original stopped state. Default PG18
and existing roles/data are unchanged.

**Minimum formal supplier-call plan: three.** Two batch requests are sufficient
for six capability facts, but do not establish ordinary operation/credential
eligibility. Chat is outside `IsQualificationRequired`'s extra executor-approval
catalog; `deployment_manifest.go` still requires verified operation evidence
and the latest successful `model_deployment_probes` for the same deployment,
binding, upstream, protocol and revisions. Retain one ordinary connectivity
Probe plus the two batch groups: **1 + 2 = 3**. This batch does not remove that
existing protection, and does not authorize any supplier call.

**Remaining external boundary and lessons.** A Target operation sheet must
bind the final reviewed diff/source, new runtime implementation and exact
Bundle before publication. New public-model pricing and project/key/model-set
access need their own applicable authorization; an old Flash price/access does
not transfer to the new model. The parent owns any approved supplier-call
budget. Real default-effort behavior, supplier usage, fees, client access and
Target activation remain outside this Local batch. Rechecked MAR-001,
MAR-003, MAR-005, MAR-007 and MAR-009: the effective-default, exact-data and safe
receipt constraints already cover this change. 经验文件无需变化; the new floating
alias incident does not warrant a duplicate lesson. The PG16 identity and
non-superuser acceptance enforce existing constraints rather than adding a new
model-adaptation rule.


## DeepSeek Probe 265 stream diagnostic correction, local candidate — 2026-10-08

**Boundary.** Development Probe 265 for deployment 24 `reasoning_effort` recorded HTTP 200 and `provider_success=true`, but failed the stream contract with the public code `stream`; its response summary did not retain the underlying parse stage or original frame. The old shared Function Tools path did not populate `data_event_count` or `delta_count`, so its 0/0 was a default value, not proof of no events. The exact upstream cause remains unknown; alias-model mismatch and an unsupported SSE field are distinct offline reproductions of the same old receipt. No new Provider call, Target action, credential read or Release occurred in this diagnostic batch.

**Local 1.9.5 candidate.** Based on clean source `ef04bb9b`, the shared stream validator now preserves the old error code while classifying only fixed failure stages/reasons; the read projection rechecks the same allowlist. It counts fully assembled `data:` events (including `[DONE]`) and parsed choice/delta frames separately. The POST result, immutable response summary, GET view, OpenAPI and existing admin panel carry the safe classification without a raw frame, response body, supplied model name, tool arguments or credential. This changes diagnosis only: strict model identity, SSE fields, request shape, binding, qualification and accounting stay unchanged. Version 1.9.5 metadata is prepared locally; this is not a commit, published version, deployed Target, or recovery claim for DeepSeek.

**Verification and operating lesson.** Synthetic DeepSeek v2/v1 fixtures distinguish `model_identity/mismatch` from `sse/unsupported_field`, check rejection before/after a parsed frame, malformed JSON, bad usage, success and private-marker exclusion. The migrated local PostgreSQL test `TestExistingFamilyDataProbeDispatchAndPersistenceIntegration` checked pre-Probe GET plus POST/storage/GET consistency, with the retained DeepSeek, Kimi, MiniMax and Qwen fixtures and no skipped subtest; this was local fake-provider HTTP, not actual supplier evidence. An initial optional local run accidentally used the default PostgreSQL 18.3 and is not PG16 evidence. The final required run used the previously approved task-owned cluster `/private/tmp/ur-xi-control-lifecycle-pg-20261006`, port 55437, role `m4`; `PG_VERSION=16`, `/opt/homebrew/opt/postgresql@16/bin/pg_ctl --version` returned 16.14, and read-only SQL returned `server_version_num=160014` with original system ID `7693457567414990508`. It set `REQUIRE_TEST_DATABASE=1` and the explicit `postgres://m4@127.0.0.1:55437/postgres?sslmode=disable` test DSN, so the test helper also refused a missing DSN or non-PG16 server before creating an isolated test database. In the delivery sequence, Publication of a binding must not be assumed to add the existing deployment's explicit capability declaration: read exact qualification before an authorized paid Probe. A source Git bundle verification must be followed by an isolated empty-repository import and `git fsck` before it is treated as transferable; after a long transfer, dependent steps begin only after the transfer exits 0 and the receiver's exact bytes are checked. Safe diagnostics must reach the persisted receipt and readback, not stop at an in-memory validator. These checks do not retroactively recover Probe 265's missing frame.

The exact PG16 verification entry, used only after confirming the task-owned cluster is running, is:

```bash
set -euo pipefail
pg16_bin=/opt/homebrew/opt/postgresql@16/bin
pg16_data=/private/tmp/ur-xi-control-lifecycle-pg-20261006
pg16_dsn='postgres://m4@127.0.0.1:55437/postgres?sslmode=disable'
test "$(cat "$pg16_data/PG_VERSION")" = 16
test "$("$pg16_bin/pg_ctl" --version)" = 'pg_ctl (PostgreSQL) 16.14 (Homebrew)'
"$pg16_bin/pg_ctl" -D "$pg16_data" status >/dev/null
test "$("$pg16_bin/psql" -w -h 127.0.0.1 -p 55437 -U m4 -d postgres -Atc "SELECT current_setting('server_version_num') || '|' || system_identifier FROM pg_control_system()")" = '160014|7693457567414990508'
REQUIRE_TEST_DATABASE=1 TEST_DATABASE_URL="$pg16_dsn" go test -tags=integration ./internal/admin -run '^TestExistingFamilyDataProbeDispatchAndPersistenceIntegration$' -count=1 -v
```

After the PASS, the same absolute PG16 `pg_ctl` stopped that cluster with `-m fast`; `status` returned no server running, `pg_controldata` retained system ID `7693457567414990508` and reported `shut down`, and port 55437 had no listener. The default PostgreSQL 18 service was not changed.

**Next evidence boundary.** The repository has an offline provider conformance fixture and a paid, evidence-writing Capability Probe endpoint, but no supported online read-only diagnostic entry that uses the existing binding secret in host memory while avoiding qualification, billing and credential writes. A new formal rollout and single same-shape Probe would need separately bounded authorization; any semantic or model-identity contract change depends on that new evidence and independent review.

**Proposed single-Probe operation sheet (not executed or authorized here).** Complete the normal 1.9.5 review, CI, exact artifact and development Target rollout gates first, then read back the deployed binary/version and current Target state. Before spending a call, GET `/api/v1/model-deployments/24/operations/openai.chat_completions.v1/capabilities/reasoning_effort` and confirm the exact DeepSeek Flash v2 Probe binding, deployment declaration, current eligibility and linked credential binding 9; recheck the approved provider-call/fee remainder. Only with a new authorization covering this exact development action, POST once to that capability's `/probes` endpoint with `{"credential_binding_id":9}` using the existing administrator identity. Preserve the returned Probe ID and safe POST result, persisted `response_summary` (`contract_error_code`, `diagnostic_stage`, `diagnostic_member`, both counts and usage), and GET readback; stop after the single attempt without retry or Release activation. If `model_identity/mismatch` is observed, obtain the actual provider/model contract evidence before considering a versioned exact identity change; do not broadly relax validation. This sheet neither grants deployment nor a paid Probe.

## UR 1.9.3/M125 development Target rollout and original XI recovery — 2026-10-07

**Target identity and upgrade.** The executing operator reported completed
development rollout of source
`5412769528de1d63328900663d938a96e9dc5aad` (UR 1.9.3), backend digest
`c6fe51387ba8c44766ebe97e2017cf3dfc8e8a1c8430cabeeb63e4c635aacd21`
and frontend digest
`3cfeab27240da9fa08cbb0fe879d6579766cb04c1c58b883bae8d4baa43ff277`.
The original PostgreSQL system ID was `7655821080247336993`. The formal M125
compatible-expand check and apply succeeded. The authorized body snapshot
matched across the check; after apply, only `schema_migrations` changed in the
compared table-data snapshot. M125 also installed its declared tables, functions
and triggers. Both backend instances were ready and serving after rollout. The
preceding `37fe` stable/canary pair and historical `93ef`/`9db` instances were
formally retired. This is a development Target result, not a
Production deployment or a new signed Artifact claim.

**Original installed XI connection.** XI FPV16/schema45 upgrade operation
`20261006T223634Z-17c39ac28f348993` completed. The two-administrator UI
recovery kept installation `xci_khXsqM6fp6PzPAbV5VDAAw` and its original
identity. The UR page showed an effective connection and its control recovery reached
`promoted`; XI recovery `xcr_W4PBgwe2Cr_BNENdTdsJ7w` reached `committed`.
The old control token was revoked. The new token was valid at the readback and
had reported expiry
`2026-10-07T22:44:46.898667Z`; this timestamp is not evidence of subsequent
renewal. The original project86 had eight active streams. XI accepted ten
content-body copies, and the original reviewer read 1,817 bytes through the
formal HTTPS path with matching SHA-256
`aac8b1b38735df22395e0c00ed6f9fe2624b89b652630b4902502bda04c67ce0`.
These observations establish the
bounded installed recovery and real source/content read path. They do not
establish uninterrupted operation across 24 hours, a new model invocation,
or two-model acceptance. No new model request was made in this batch.

**Ingress closure and retained evidence.** At 07:00 CST one temporary UR
security-group rule, one XI security-group rule and one XI UFW rule were
revoked, three rules in total; the readback found other rules unchanged. The
durable non-secret receipt is
`/Users/m4/Desktop/projects/About Me/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/supporting/20260929-formal-ingress/p3-fpv16-original-connection-result-20261007.json`
(SHA-256 `efc1b1cc033a23865b99e5a7d33475c979bd1f67c5f6b4b620d64576a0f4524f`,
recorded `2026-10-06T22:59:51Z`). It also records the original protected files
and historical row sets unchanged, and distinguishes the HTTPS protected-body
API read from browser rendering. This document uses the operator's Target
readback and that receipt; no new Target action was made for the documentation
update.

**Separate model-acceptance setup and current blocker.** The approved org2
project87 and development Application
`app_generated_b541fae2032288f9fc261753` were created through the formal
UI. No Grant or Key was created. Its access form requires an effective Sponsor
from the customer/project; project87 had no selectable member. The User was
asked to decide whether existing user 41 may be added as project87
`project_user` and used as Sponsor. Browser invitation confirmation and the M4
private download path also await the User; no invitation was created. No
platform-administrator identity was substituted, and no direct SQL or
credential workaround was used. The application-Key UI does not expose RPM/RPS
fields. The already-approved limits are 10 RPM, 1 RPS and concurrency 1, with
an already-approved credential-custody scope. The
supported Grant-bound credential API can set those limits but cannot widen the
approved Grant; no further rate or custody decision is pending. Existing
project86 XI feed/source keys are purpose-bound and cannot substitute for a
project87 model-calling Key.

**Retrospective and evidence boundary.** Earlier guidance named `/projects`,
which the 1.9.3 router does not register; its wildcard redirects to the user's
default route. The actual project tab is
`/organization-members?section=projects`, and `/applications` is the guarded
Application route. The correction was established from the route table before
further UI work. Sponsor eligibility and the Key form/API rate difference were
then checked together before any Grant or secret creation. This batch changes
only the two existing status documents; it does not change frozen runtime
source, create credentials, repeat full CI or operate the Target.

## M124→M125 installed-upgrade correction, source/CI closeout — 2026-10-07

**Scope and cause.** The 1.9.2/M125 source had a working M123→M124 active-source
helper, but `Rollout.plan()` selected it only for M123. An active M124 source
therefore reached the disabled-source fallback and the development/test plan
refused. The compatible-expand CLI's manually accumulated predecessor
conditions selected M123 for target M125, so an actual M124 ledger was rejected.
The earlier tests checked the helper and domain migration separately, missing
these two original entrances. Independent read-only pre-code review validated
both blockers. The M125 migration SQL and XI wire contracts were not changed.

**Original failures and correction.** A new `Rollout.plan()` regression with
stable/canary active A/B/C/full capture, original producer, content storage and
static peer first failed on the unmodified branch with `schema bridge requires
disabled source capture and empty storage settings`. A real CLI check on an
M124 ledger first failed with `expansion predecessor differs`; that first
reproduction accidentally used the default local PostgreSQL 18 and is only a
code-path failure, not PG16 acceptance evidence. The repair reuses the M123
source-preservation check for M124, retaining exact environment and role parity
and rejecting capture, identity, storage or static-peer drift. CLI predecessor
selection now uses the ordered `SupportedSchemaHeads()` authority; an M123→M125
jump remains rejected. Version 1.9.3 and the operator version were updated.

**Database gate and current evidence.** The PostgreSQL 18 drift exposed that
`REQUIRE_TEST_DATABASE=1` did not require `TEST_DATABASE_URL` or a server
version check before creating test databases. The required path now refuses an
implicit DSN and verifies PostgreSQL 16 before isolated CREATE, migrated
template/clone CREATE and ordinary test-table TRUNCATE; optional developer
tests keep their previous fallback. Unit checks cover missing DSN, PG18
rejection, optional behavior and write-before-preflight wiring. The task-owned
PG16.14 at `127.0.0.1:55437` returned `server_version_num=160014`. Against
this explicit DSN, the real CLI check/apply and nonadjacent rejection passed
with `-race`, as did the original three-package early M125 CI selector. The
Python rollout suite passed 81 tests; release metadata and integration-runner
policy suites passed 19 and 9 tests respectively. These were Local results
before the source/CI closeout below.

**Exact source and CI closeout.** PR #433 head
`7cc255c6416080609c54bca0c1243888e23c0e09` passed all five applicable
checks in [run 37489026277](https://github.com/foigent/unirouter/actions/runs/37489026277):
Change Routing, Backend Tests, Frontend Build, PR4 Browser E2E and Docker Build.
Its normal merge produced main `5412769528de1d63328900663d938a96e9dc5aad`;
both commits have tree `015f53dbce56cf509abb000bf0ea58a42da605fe`
and an empty diff. The separate exact-main
[protocol run 37496389077](https://github.com/foigent/unirouter/actions/runs/37496389077)
passed on that main commit. This documents source and CI acceptance only, not
an immutable runtime Artifact or Target success. The document-only closeout
that records these facts leaves runtime source bytes unchanged; the approved
runtime delivery identity remains `54127695`, without a new Artifact build.

**Long-task retrospective.** First, the upgrade tests had exercised the M123
source-preservation helper but omitted the real active M124 `Rollout.plan()`
entrance; the new original-entry regression now fails if that exact capture,
producer, storage, static-peer or stable/canary preservation is lost. Second,
`REQUIRE_TEST_DATABASE=1` still allowed an implicit default DSN, so the initial
CLI red reproduction reached the user's PostgreSQL 18 instead of the intended
task-owned PG16. The existing required-test entrances now demand an explicit
`TEST_DATABASE_URL` and check PostgreSQL 16 before isolated database CREATE,
migrated template CREATE, clone CREATE and shared-table TRUNCATE; tests lock
those write-before-preflight orders and preserve optional developer behavior.
Third, the accepted CI-efficiency contract already removed automatic duplicate
post-merge full CI. For an identical PR/main tree, future closeout should
verify the exact tree, reuse all applicable successful PR checks without
reducing their coverage, and retain the separately required exact-main protocol
evidence; a full main dispatch is not added merely by habit.

**External boundary.** The 2026-10-06 read-only Target receipts show UR
1.9.1/M124 and XI FPV15/schema44. Version 1.9.3 has not been signed or deployed;
the original installed XI connection has not been recovered. Target upgrade,
external installed lifecycle and Production remain separate pending evidence,
and this documentation closeout grants no deployment, credential or traffic
authority.

## X-Insight control credential lifecycle, UR Local candidate — 2026-10-06

**1. Goal and evidence.** Preserve one installed connection and its original
installation/deployment, control project, completed-event scope, source binding,
origins, fence, permissions, history and ACKs across 24-hour bearer renewal and
administrator-approved post-expiry recovery; revoked connections remain final.
The shared approved contract is the 2026-10-06 P3 control credential lifecycle
document (SHA-256 `60073ab48c36599bd21193c76f0daf9f7090fd88e8044f6c888648f5acf2518c`).
At the initial Local cutoff, the UR source candidate was uncommitted on
`codex/ur-xi-control-lifecycle-20261006`, base
`a831e6bc33ca25edd7ccb07f8a036d84f5bae591`. Version 1.9.2/M125 adds
purpose-separated encrypted pending operations and receipts, fixed-origin
rotate/query/confirm and recovery, production-loop due discovery ahead of
Poll, original/current administrator reauthorization, local status and a
recovery page. The page first uses a local, ticket-free lookup of the globally
unique installation ID to find the original business organization, rechecking
both current and saved setup actor authority; it does not assume the current
platform administrator's technical `org_id` is the business organization.
[Operator and API instructions](../ops/XINSIGHT-CONTROL-CREDENTIAL-LIFECYCLE.md)
are in this repository; OpenAPI has the exact admin routes.

**2. Actual result by layer.** Local PG16.14 `-race` selectors for
`TestControlCredential.*PostgresIntegration` and
`TestXInsightCredentialRuntimePostgresIntegration` and
`TestXInsightCredentialRecoveryScopeTechnicalOrgPostgresIntegration` passed.
The runtime test uses the
actual production `runCycle` and PostgreSQL journal: healthy Poll, due-before-
Poll, same pending operation after reconstructing journal and supervisor over
the durable database, and revoked exclusion. It simulates the XI response
loss; it is not a real process restart or two-product proof. The credential
journal tests also cross the actual saved replacement receipt expiry with a
controlled clock, reconstruct the journal, and recover the original connection
from prepared or XI-committed/UR-unpromoted states. A pending operation without
a receipt remains undecided even when the old current expires. The recovery test
uses a platform actor from a distinct technical organization with the original
business customer context, rejects foreign/mismatched scopes, and asserts zero
XI outbound calls on denied local authorization. The affected Go package
suites, frontend TypeScript/Vite and 23 routing/recovery tests, migration/budget
and release metadata checks passed. The restore helper delivered with M125 is
`scripts/source-budget-restore-rebase.sql`, 10,838 bytes, SHA-256
`d9939ccad3bd962e23a2f8ee2123d8efbe265d74e13352c3bc6c31171f20ff62`;
`check-release-metadata.sh` verifies these README-declared bytes. The isolated
PG16.14 cluster was task-owned under
`/private/tmp/ur-xi-control-lifecycle-pg-20261006`, port 55437. Before the
final joint run, its `PG_VERSION` read `16`, the exact
`/opt/homebrew/opt/postgresql@16/bin/pg_ctl --version` read `16.14`, and a
read-only SQL connection confirmed the live server was PostgreSQL 16.14.
The same PG16 `pg_ctl` stopped the task-owned cluster, and its `status`
confirmed no server running; no existing
database or Target was modified. CI, immutable Artifact, Target, Production
and real external integration remain unverified.

The original managed new-install UR/XI Local joint test subsequently passed
on isolated PG16.14 in 42.51 seconds, with real HTTPS, all eight profiles,
post-registration C manifests, XI accepted encrypted body, and independent
XI body readback. Its successful assertions do not substitute for the
unexecuted installed Linux lifecycle driver.

**3. Deviation and failure class.** The first optional managed UR/XI joint
run failed in XI's local host fixture before source enrollment with “target
network publication is incomplete.” After XI fixed that fixture, the second
run reached all eight successful source profile polls and active UR A/C
enrollment. In the latest run, XI fetched C manifests three times after
publication with HTTP 200 and stored the expected complete manifest, but its
body copy was excluded with `effective_policy_disabled`; XI's setup policy was
not applied and its effective review-capture default was absent. The 100-second
child context then expired without a body GET or accepted/readable copy. The
failure was at XI custody setup, not the UR C cursor or credential lifecycle.
XI fixed the missing fixture deployment identity and the original new-install
joint entrypoint passed on rerun. That does not prove a successful credential
lifecycle. The installed joint driver remains
unexecuted; its separate Linux service and credential assertions are not
claimed. Failed evidence is retained in task-local logs.

**4. Original assumption and correction.** Standalone PG lifecycle tests
cannot substitute for XI's service-auth HTTP, persisted XI identity and actual
worker readback. The UR joint fixture now discovers credential due/pending
before Poll and uses real UR handlers and encrypted journal, while a new
`cmd/uniroute` integration test covers the production supervisor rather than
relying on fixture scheduling alone. The initial network fixture failure is
old-fail/new-progress evidence only; neither it nor successful first worker
polls satisfy the original C or lifecycle exit conditions. C's empty cursor
starts at registration `coverage_from`, not the first-read high watermark;
the server-half test proves a manifest published after C registration but before
its first read appears in that initial page. No extra scheduler
or synthetic `loaded`/`completed` result was added.

**5. Remaining owner and action at the initial Local cutoff.** XI owner runs
the installed Linux joint driver's credential lifecycle scenarios on the exact
isolation; UR responds to any verified cross-product contract failure. UR
owner finishes independent diff review and repository preflight; the
coordinator then reconciles Local versus CI,
Artifact and Target separately. At that cutoff, no push, merge, deployment,
service change, real credential write, paid call or real traffic action occurred.
The model adaptation experience file needs no change: this batch adds no model,
Provider, Profile, usage or pricing result.

**6. PR #432 CI failure classification and original-entrypoint correction.**
The first PR Backend run (`37440417973`, job `112192673575`) failed in
`scripts/test-capability-carrier.py`: two fixtures targeted the current M125
head but still supplied only M124 implementations. The prior local selected
rollout tests covered M125, while Change Routing's existing Docker-classified
`delivery_preflight` ran rollout, ingress and image-build fixtures but omitted
the already existing carrier test. Thus that original short entrypoint did not
intercept the omission before the expensive Backend job. The carrier fixtures
now include M125's exact implementation and a missing-implementation negative
case. The same existing `delivery_preflight` now executes
`test-capability-carrier.py`, with a release-policy test guarding its wiring;
the routing category, job set and full required Backend suite are unchanged.

The next PR run (`37441809132`, Backend job `112197221665`) passed the Backend
runtime-ingress/carrier short check and the M125 PostgreSQL lifecycle selector,
then failed the full Go suite at `internal/resetplan`'s
`TestEveryCurrentMigrationTableHasAnExplicitResetClassification`:
`xinsight_connector_credential_operations` was absent from the explicit reset
classification. This is a new M125 schema lifecycle omission, not a stale test
expectation. The encrypted credential-generation journal has a restrictive FK
to its installation and the M125 down migration refuses nonempty history. An
independent read-only pre-code review found it must follow the existing
X-Insight machine-authority rule: `review_required`/`preserve_all` and refuse
reset apply, including when empty. The local fix adds exactly that classification
and an isolated 0/1-row negative test for retained rows, zero deletion, absent
delete-order entry and failed apply validation. The original Backend early Go
step now runs the existing migration-table enumeration and this negative test
before the full suite; no new job or reset deletion scope was introduced.

Local evidence after the corrections: Change Routing's four Python test commands
passed in workflow order (80, 13, 10 and 6 tests); its final host
`environment-check` could not run here because this macOS sandbox has no Docker
CLI. The targeted reset classification tests and full `internal/resetplan`
`-race` suite passed; the release-policy suite passed 19 tests. A local
`internal/resetexec` test attempt was blocked by sandbox denial of miniredis's
loopback bind, not by a product assertion. No corrected PR/CI, Docker Artifact,
installed XI lifecycle, Target or Production result is claimed by these Local
checks.

**7. PR #432 first-customer schema inventory correction.** The exact
`cadfca4e01f3ce2d8708c1ce46795457a14c7b72` PR head passed protocol run
`37445690343`. In main CI `37445690260`, Change Routing, Browser E2E and
Frontend passed. Backend's early M125 PostgreSQL/reset check, `go vet`, carrier
check, full ordinary Go suite, Neutral N1, source-evidence PostgreSQL and
X-Insight snapshot checks also passed. Its admin integration step failed on
`check exact public table inventory: got 158 want 157`; Docker then stopped at
its Backend prerequisite without building an image. The several FirstCustomer
HTTP 409 and negative-test failures in that step share the same precheck cause.

M125 created `xinsight_connector_credential_operations`, but the
first-customer exact public-table and zero-state inventory did not include it.
The previous reset correction covered resetplan's separate migration-table
enumeration; its unit test did not exercise the first-customer inventory. The
first-customer unit test compared its own static expected list and cardinality
against the same stale product list, so it missed the new migration table.
Independent read-only pre-code review required treating the journal as
X-Insight machine authority: all three first-customer checkpoints may accept
it only when empty, while resetplan still preserves history and refuses apply.
The local correction adds the exact table to their shared inventory, changes
the independent count to 158, and compares the inventory with actual CREATE
TABLE declarations from current migration files. The existing Backend early
Go step now runs those inventory tests before its full suite. No unknown-table
allowance, automatic journal deletion, reset-scope expansion, new job or timeout
was added.

Local evidence: the two inventory unit tests passed. The task-owned isolated
PostgreSQL 16.14 on `127.0.0.1:55437` ran all 17 `TestFirstCustomer...`
integration selectors with `REQUIRE_TEST_DATABASE=1` and `-race`: PASS in
22.924 seconds. The new nonempty-journal test inserts a valid parent
installation and pending operation, proves the read-only runtime checkpoint
rejects the journal, and proves both normal and dry-run bootstrap requests
return 409 without changing the installation or journal rows. The separate
single-test run passed in 5.449 seconds. The Backend early wiring check and
release metadata policy suite passed (19 tests), as did the release metadata
shell check, admin `go vet`, and `git diff --check`.

The same CI investigation found a separate installed-joint diagnostic defect:
UR's safety redactor discarded *all* XI stage receipts when more than 24 valid
lines arrived. XI's new-install run had 19 receipts, while adopt's longer
predecessor and credential sequence crossed that bound and the UR failure
reported `receipts=[]`. This is a diagnostic information loss, not evidence of
the underlying adopt failure cause. The local test-only correction accepts
only the exact installed stage/result v1 schemas and existing safe fields,
ignores malformed or private lines independently, retains at most 48 valid
receipts (including the last earlier result), and marks omitted counts. A
focused integration-tagged unit test covers 19, 30 and 61 valid lines, result
retention, fake schema, invalid bounds and private noise. It does not change
the product runtime or the XI-owned receipt protocol.

The resulting PR head `d798af8323543c6dcca346cb62827cd278e206f5`
passed protocol CI `37452146640`. Main CI `37452146716` passed Change
Routing, Browser E2E, Frontend, Backend's early M125 check and `go vet`, but
Backend `Run tests` failed only `TestAdminIntegrationFilesUseIntegrationSuffix`:
the new `TestRedactInstalledJointReceipts` in an integration-tagged file lacked
the required `Integration` suffix. Docker stopped at its failed Backend
prerequisite without building an image. The author's focused
`-run '^TestRedactInstalledJointReceipts$'` invocation had passed because it
selected only the new test, bypassing the existing ordinary naming guard; the
formal admin integration lane selects `-run 'Integration$'` and would omit it.
The previous early check ran migration, inventory and credential selectors but
not the naming guard. The local correction renames the test and runs the
existing guard in that early step. The guard failed on the prior head and now
passes; the renamed test passes with `-race`, appears in the formal
`Integration$` test list, and all 19 release-metadata policy tests including
the updated early wiring check pass. The pushed correction is head
`41e580c08fafc4d3ed193784620566934785c01f`; protocol CI
`37456231777` passed. Main CI `37456231759` was still running at this
record's local cutoff; none of these results establish Docker Artifact,
installed XI lifecycle, Target or Production acceptance.

**8. Installed XI new-install credential-recovery failure and local UR fix.**
XI's installed new-install CI `37456275687` passed its credential offline
expiry and human-confirmation preview stages, then UR's
`POST /api/v2/organizations/<org>/xinsight/setups/<installation>/credential-recovery`
returned 503. XI's safe stage receipts contain 19 entries with zero truncated
or invalid entries; the failure stage is
`credential_offline_expiry/failed/lifecycle_ur_human_confirmation`. This is
external integration evidence for the failure, not a completed installed
lifecycle qualification.

The UR confirmation handler inserted
`data_exchange.xinsight.control_credential.recovery_confirm` (58 characters)
into `audit_logs.action VARCHAR(50)`. The audit insert error reached the
generic setup-error writer as 503 and rolled back the credential-operation
transaction. On task-owned isolated PostgreSQL 16.14, the original handler
returned 503 after a successful preview, and a direct insert of the same
action returned SQLSTATE `22001`; no credential operation was committed. The
local fix uses the 50-character action
`data_exchange.xinsight.credential.recovery_confirm`, preserving the audit
write and transaction. The new PostgreSQL HTTP integration test passes with
`-race`: preview 200, confirmation 202 with one pending operation and one
audit, same-ticket reentry 202 with the same operation and a second audit,
and foreign resolution 409 without another operation or audit. A direct
literal scan of production X-Insight admin actions found no other action over
50 characters after the fix; an AST-based length guard now runs in Backend's
existing early check. The new real-PG confirmation test is also included in
that early PG selector; `go test -tags=integration -race ./internal/admin -list`
with its exact selector listed both recovery-scope and confirmation tests, and
the release-policy test verifies the workflow wiring. Admin `go vet`, the
static early check, all 19 release-policy tests, release metadata and
`git diff --check` passed locally. This local patch
has not been pushed or run in CI, and it does not establish XI rerun, Target
or Production success.

## Long-task retrospective entrypoint — 2026-10-06

**Baseline and scope.** This documentation branch
`codex/long-task-retro-docs-20261006` starts from verified main
`7ab84f1de23c4361939977fc5a0c0f7369619e5e` (PR #430 merged).
Exact-main CI run `37338248954` completed successfully for that commit.
PR #430 changed only `internal/admin/xinsight_joint_http_integration_test.go`
relative to deployed `37fe231df464f387a0f0582d809aef1672f3f4e1`;
the source VERSION remains 1.9.1. The coordinator reports the existing
development Target remains 1.9.1/`37fe231d`/M124; this documentation task
did not read or change Target. PR #430's new installed-XI test selector is
opt-in, so UR main CI success alone is not installed XI or external integration
acceptance.

**Retrospective and applied entrypoint.** The goal was to connect the approved
UR/XI flow while preserving original scopes and evidence. The bounded UR
runtime delivery is complete per the coordinator; XI's actual installed
controller, source worker, custody and future-project chain remains a separate
acceptance. The observed XI Target failure was an EROFS host permission error
while publishing the controller's systemd drop-in. The mistaken test
assumption was that real PostgreSQL and HTTPS plus a simulated systemd runner
covered installed service permissions; the controller never ran in its actual
mount namespace, so the local check could not intercept this failure. PR #430
added the explicit installed-service UR test entrypoint while preserving the
original local test. Its main CI verifies the test code but does not exercise
that opt-in path. The matching XI Linux qualification must run the released
services in both new-install and restored FPV14 predecessor states; only its
actual result can establish an old-failure/new-pass check, and Target recovery
still needs separate readback. The process correction in
[AGENT-PROTOCOL.md](AGENT-PROTOCOL.md#64-长任务完成复盘) requires a long-task
closeout to check the original missed entrypoint and complete necessary
changes before declaring the whole task done. No extra UR production change,
test framework or new model-adaptation lesson is introduced here.

**Remaining owner/action.** The XI owner must finish its Linux qualification
and actual Target connection, loaded custody, source facts and body readback;
the overall coordinator reconciles both projects' exact evidence and
authorization. This document update changes no runtime behavior,
required CI, permission, credential or deployment state. Its scope, relative
links and `git diff --check` were reviewed by the coordinator before delivery.
Earlier entries below retain their historical cutoffs.

## P3 step 2 — M121→M123 daily UR source-to-Target boundary — 2026-09-30

This bounded source review uses merged main `11052bb539903022713d3676222f7fda6a4cd957`,
tree `d467a590d9fd9f10bd0d93dab9e7be3cb78e1f61`, VERSION 1.8.0. The local
candidate `2b1820def9f19e6aa89b3242bc253e0e911203df` has that same tree.
Historical `2bad61e92a45ae5ce8e4205fdaaa9a6f580327f5` is 1.6.2/M121;
the exact migration difference is only the M122/M123 up/down pairs. Overall
reported PR425, candidate CI36659048365 and exact-main protocol36662848089
successful; this task did not rerun them or read the Target. September 28 Target
state and org2/project86 key105/106/107 identity remain historical leads.

The existing finite `schema_bridge_expand_carrier`, formal rollout, old-process
`runtime-generation-retire` and per-head `compatible-expand` describe an exact
M121→M123 path **if** the Target's complete old ledger/artifact/host state and
disabled-source gate match. The carrier also requires all five source
capture flags disabled and its three storage settings empty on both old/new
backends; if daily A/C reads depend on enabled source service, this path refuses
the rollout and continuity needs a separate decision, not silent flag changes.
Run the carrier build from a clean exact-main
checkout, plan/prepare/apply the one ordered M122/M123 bridge, retire the exited
old canary and stable process identities with check/apply, then M122 check/apply
and M123 check/apply with complete ledger and recovery-fleet readback after each.
`prepare` verifies a PG dump only; C body storage needs a paired same-window
backup if present. After new evidence exists, 1.6.2 cannot be assumed a valid
M123 rollback; use compatible roll-forward or guarded offline recovery including
the existing D-coverage/C-epoch rebase. Legacy A/C key continuity is conditional:
read exact key/enrollment/installation/coverage state, use existing admin
`bind-existing` separately for eligible facts/content, and verify XI actual reads.
No key ID is assigned to an A/C role from memory.

[Executable existing commands, reviewed M122/M123 digests, minimum Target
readback fields, key-continuity conditions and restore limits](</Users/m4/Desktop/test/ur-regression-20260914/DIAGNOSIS.md>)
are appended to the existing diagnosis. No ECS/Target, XI, credential, build,
deploy, traffic, test, commit or push action occurred in this step. The current
runbook section 9.10.3 records a September 29 unmerged-candidate cutoff; this
entry supersedes only that source status, not its Target preflight and authorization
gates. Experience file: no change needed; no new runtime/model result was gained.

The overall coordinator's 2026-09-30 04:00 UTC Target readback is retained at
`/private/tmp/ur-xi-p3-target-20260930/ur-readback.json` (SHA256
`9e10ee430759cc949019b186321c87087242fc4e073210573820277633da12a4`).
It reports healthy 1.6.2/M121 stable/canary processes on image
`sha256:f0000763e7ed55b3a1f93e4b12bb3392695c932016462fb6eb4122a74b83f225`
and two unretired `serving/absent/lease_live=false` old identities:
`uri_369b8dc1f9fd70ddb30f6b34a81b6eb2` and
`uri_b95435dec827c6414e446a1787b4dd9f`, each at revision 6739 on old image
`sha256:a5fb9213860232a19cdaccfec4ce4d236ea1eb5c441ee6f3e66ed7a46d265b13`.
The retained `/private/tmp/ur-rollout-2bad61e9-20260928-input.json` points to
`/opt/unirouter/deployment-evidence/20260928-source-digest-fix/rollout`, but the
original Plan and withdrawal/clean-stop/replacement/rejoin/verify receipts were
not read in this task. Their exact old-instance binding, current ingress and
container inventory, immutable manifest, and financial closure remain required
before formal retirement; an expired lease is insufficient. This is an archive
lead, not a retirement eligibility result. The diagnosis now records the exact
readback and proof requirements. If current source settings are enabled/nonempty,
the narrow M121→M123 carrier needs a reviewed compatibility correction that
preserves all eight old/new effective settings; no live source disablement,
product edit or Target operation was performed here.

The coordinator's 04:09 UTC detail readback
`/private/tmp/ur-xi-p3-target-20260930/ur-detail-readback.json` (SHA256
`b902976f17c435fea1da658a6237be0a788fddd529aec1d6a9454e7fe7983adc`)
confirms both backends have source A/B/C and full content enabled, D disabled,
and the same nonempty content storage binding. Therefore the existing disabled-only
bridge rejects the actual Target. It also identifies key105 as completed-feed,
key106 as legacy A/B, and key107 as content C for org2/project86; content scope
is active at state/access generation 2 with 6841 stored bytes and no reservation.
These do not prove future enrollment eligibility or XI read acceptance.

The pre-code review proposal in the diagnosis confines a source-preservation
allowance to the exact M121→M123 bridge, while keeping older bridges disabled-only
and every original source/image/mount/manifest/recovery gate. A second old→main
Compose difference adds `ASYNC_MEDIA_REQUEST_TTL_HOURS` (template default 24,
Target effective value not yet read); the current exact
config-delta comparison will reject it. The 1.8.0 media store also references
columns added only at M122, so enabled media on M121 is an unresolved service
compatibility condition, not an acceptable switch-off. The Target readback has
not established media settings or outstanding jobs. Existing M123 `bind-existing`
can conditionally attach old C key107 after an active enrollment and exact GET
candidate, without key or content reset; no new C bind channel is proposed.

### Local M121 bridge correction and verification — 2026-09-30

After independent pre-code review, the authorized local patch is VERSION 1.8.1.
Only the `schema_bridge_expand_carrier` branch whose exact predecessor is M121
preserves all eight source/storage values and
`COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE` across old Compose, both actual old
containers and new effective Compose; stable/canary must match. It requires
media already disabled on both old actors and the new actor, and permits only
the previously absent `ASYNC_MEDIA_REQUEST_TTL_HOURS=24` in the new Compose.
All other config/mount/source identity changes still fail the existing exact
checks. Older schema bridges retain the disabled-source rule. Enabled M121
media remains an independent unsupported predecessor, not a reason to switch
off a live service.

Local evidence: `scripts/test-internal-target-rollout.py` passed 76 tests;
`TestM121ActiveSourceBridgeMainStartup` passed against a task-owned isolated
PostgreSQL database installed through real M121 and local Redis. It built and
started the current binary with A/B/C and full content enabled, existing
producer identity and C binding, disabled media and TTL 24. The same fixture
authenticated an existing unmarked C key against M121, obtained an actual
HTTP 200 from `/health/ready`, wrote body bytes with the real file store and
read those bytes through the binary's authenticated HTTP C body endpoint. The
ready-object database row is test fixture setup; this does not prove a full
online capture or XI consumer flow. The test confirmed the ledger stayed M121
and the binding remained. The `cmd/uniroute` unit suite
also passed. `check-release-metadata.sh`
reported `RELEASE_METADATA_OK version=1.8.1 mode=current`; `git diff --check`
passed. The existing consolidated CI selector now includes the exact M121
integration test name; its offline runner passed all 9 checks and confirmed
`cmd/uniroute` remains selected. The full consolidated suite was not rerun.
These are Local results, not CI, signed Artifact or Target results.

The 04:19 UTC Target detail
`/private/tmp/ur-xi-p3-target-20260930/ur-retirement-r2-readback.json`
(SHA256 `37f07d6237e76215766bb130ed5350c8c6878b76cb2f4c53d486f4bc97479e29`)
shows the actual producer identity `unirouter-test-reset-20260824-v3` on both
backends, media disabled, old TTL absent and zero async-media jobs. The
September 28 rollout Plan names **different** old instances and cannot retire
`uri_369b…`/`uri_b954…`. The 09:27 config-rollout history and original log
identify these as predecessors of its canary/stable replacement; host proof,
current inventory and exact read-only retirement checks remain the coordinator's
separate Target gate. No remote action, retirement apply, commit or push occurred
in this local patch.

## Bounded native-regression diagnosis — 2026-09-14

The supervisor accepted the independent diagnosis plan, including resource-admitted
M4 synthetic checks. Product deadlines, mandatory source capture, replay identity,
Provider/freeze/terminal counts and the original F consolidated failure remain intact.
No product behavior repair is justified by the currently available evidence.

Test-only diagnostics distinguish Drop connection acquisition from execution while
preserving its caller's original deadline and error chain. Logs contain phase,
elapsed time, error class and SQLSTATE, not raw PostgreSQL details or DSNs. The
source HTTP fixture delegates the original capture interfaces and records failed
capture boundaries after return; replay assertion diagnostics also retain both
operation headers. No source publication, billing or middleware runtime changed.

M4 uses dedicated synthetic PG16.14 and Redis8.8, Go1.25.12, task-local cache and
logs under `/Users/m4/Desktop/test/ur-regression-20260914`. The adjacent
`resource-admission.json` records the 10 GiB incremental disk estimate, 21 GiB
retained floor and 5 GiB incremental memory estimate. These are M4 diagnosis
conditions, not verified equivalence to the original F run.

The initial narrow diagnostic run passed. The subsequent related matrix passes
all 3 packages and 31 tests/subtests, with unchanged source before/after execution:
the existing complete SourceObservations HTTP matrix, matching AtomicModelRelease
authoring cases and Drop diagnostic checks. The existing injected admission failure
returns 503 with SQLSTATE P0001; normal replay and single-effect assertions pass.
Controlled test-owned connection contention distinguishes acquire/execute timeout;
this proves the diagnostics, not the cause of the old cleanup failure.
See [exact run metadata](</Users/m4/Desktop/test/ur-regression-20260914/diagnostic-evidence/related-matrix.result.json>)
and [diagnosis and F reproduction handoff](</Users/m4/Desktop/test/ur-regression-20260914/DIAGNOSIS.md>).

Original F root causes remain unproven. Static review found no concurrent source
worker in this fixture and no evidence to blame RR serialization. Historical
launch/version/resource/server-log inputs were requested through the supervisor;
UR does not operate or contact XI/F. Original 19-package consolidated plus four
following groups remain the closure scope. A diagnostic/test-only commit is not
a real product successor, and does not satisfy same-M118 successor, old-runtime,
formal customer package or parent integration acceptance.

## Internal test and handoff follow-up — 2026-09-14

The user requested execution of the efficiency improvements; the supervisor
accepted this bounded follow-up. Work is in the persistent isolated checkout
`/Users/m4/Desktop/test/ur-regression-20260914/source`, branch
`codex/fresh-regression-20260914`, based on a84ac272. The previous tmp checkout
is gone. This entry supersedes historical path availability and native-pending
statements below, while retaining their original evidence and scope.

The existing five-group runner and six preparation lessons are reused. This
follow-up fixes the summarizer's hardcoded backend attribution: both callers
declare their actual runner, invalid group/runner pairs fail, and evidence records
runner path/hash plus summarizer hash. Logged backend and fresh checks now execute
the summarizer's `--help` before tests to verify Python, syntax and dependencies
without creating logs or contacting services. Independent precode review requested
this actual-script check instead of a guessed Python version or imports alone;
the correction is incorporated. No product runtime or test selector changes.

Validation: 9 offline runner and 5 summary regressions pass. Missing/broken Python,
missing/wrong runner, no-test/no-directory preflight, fail-stop exit propagation,
all-skipped/incomplete failure and package/subtest counts are covered. The complete
backend selector/execution suffix is byte-identical to a84, as is the fresh Go
test command. Actual Go 1.25.12 / Python 3.12.14 configuration preflight passes.
[Verification](</Users/m4/Desktop/test/ur-regression-20260914/optimization-evidence/verification.json>)
and adjacent runner.log, summary.log and actual-preflight.log retain the results.
Shell syntax and whitespace checks pass. No product test or service was run for
this tool change; product version remains 1.4.1.

[Current dependencies, durable evidence and reuse boundaries](LOCAL-TEST-REUSE.md)
distinguish available tools from missing jsonschema. Existing a84 native evidence
supports same-image fresh install, real M113 interruption/resume and new-database
restore/rebase in its accepted scope. Original consolidated remains FAIL (17 pass,
2 failed packages; 760 pass, 3 fail, 4 skipped tests/subtests). Clone cleanup timeout
and replay 409-to-503 remain open; isolated passes cannot close the original group.
Performance, supported successful M118 reset, real successor/old-runtime inputs,
formal customer delivery and full integration/parent acceptance remain separate.
Experience file: no change needed; this is not a model/Provider adaptation change.

## Fresh installer source candidate — 2026-09-12

The independent source closing review of `b4e1a77c` found that a disabled policy
or explicit column collation could pass the original-empty-ledger check. The
bounded follow-up rejects both catalogue differences, exercises formal
`plan/check` with unchanged-state assertions, and retains the normal original
empty-ledger positive case. This is a static-source finding and correction;
database reproduction and new Go tests remain unexecuted under the same resource
boundary. The follow-up remains part of the pending 1.4.1 candidate.

Current branch `codex/fresh-install-owner` reuses the existing isolated
`/private/tmp/unirouter-test-preparation-20260911` worktree from `6676c0c1`.
The supervisor accepted the bounded installer plan and three corrections before
source changes. The overall resource clarification permits small text/Git edits;
native compilation, DB/image work and cache generation remain outside the
current M4 resource admission. No protected ASUS asset or XI task is operated.

[Design, command contract, executable test matrix and native gaps](../ops/FRESH-INSTALL.md)
are the current source handoff. Version 1.4.1 is pending native validation, not
product acceptance. M1–M118 migration files and original publication contracts
are unchanged. The 1.4.0 product acceptance and 6676 test-tool evidence below
retain their original scope; neither proves the new installer. Experience file:
no change needed because the new recovery tests have not yet executed.

## Test preparation and handoff optimization — 2026-09-11

User authorized implementation of the execution-efficiency improvements. Isolated
worktree: `/private/tmp/unirouter-test-preparation-20260911`, based on accepted
product `59f7712d09653034756b846b15b8d69a16782791`. Original source and evidence
remain unchanged. UR A/B/D/X Local independent acceptance is complete; the
September 10 pending entries below are historical checkpoints.

Scope: reuse the five original backend integration selector groups between CI
and local execution, retain compact structured evidence, absorb the accepted
Docker logging fixture correction, and consolidate exact dependencies and reuse
boundaries in [LOCAL-TEST-REUSE.md](LOCAL-TEST-REUSE.md). No runtime/migration,
required-test reduction, new environment operation or product version change.
Validation: 71 existing rollout/carrier regressions pass, including the added
logging/2100-second-grace negative cases; 7 offline runner tests and 4 summary
tests pass. Exact old/new Go argv comparison against 59 preserves all five groups
(19/2/1/1/1 packages), selectors and flags. Proof:
`/private/tmp/ur-test-preparation-selector-parity.json`.
Logs: `/private/tmp/ur-test-preparation-rollout.log`,
`/private/tmp/ur-test-preparation-runner-final.log`,
`/private/tmp/ur-test-preparation-summary-final.log`.
Real Go 1.25.12 JSON smoke evidence in `/private/tmp/ur-go-summary-real-979ckeqf`
correctly counts pass/skip and propagates an intentional failure. An initial
tool-only smoke selected base Go 1.24.3 under `GOTOOLCHAIN=local` and refused
go.mod before execution; retained at
`/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/ur-go-summary-real-sfy19fg3`.
The runner now checks go.mod/toolchain compatibility offline before tests.
Actual local configuration-only preflight and Python/jsonschema dependency
readback passed; shell syntax and whitespace checks passed. The original final
acceptance SHA and referenced indices were verified. No full product/ASUS suite
or remote CI was rerun for this internal-tool change; original product acceptance
retains its exact 59 identity and limitations. Experience file: no change needed.

## Active local delivery — UR B/D/X after accepted A — 2026-09-10

The coordinator independently accepted complete UR-A Local at
`25f5c1fdfb6b5376496c34e9b9cd295efbaf17cd`, tree
`9444830ae7a12de8aa3087489420709a01735bab` (1.3.0). The acceptance source is
`/Users/m4/Desktop/projects/About Me/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/supporting/ur-iteration-20260909/independent-A-acceptance.json`;
the complete delivery and original failures remain in
`/private/tmp/ur-asus-a11-20260910/A-COMPLETE-DELIVERY.md` and its exact index.
The coordinator released the ASUS environment hold and the A-to-B/D gate.

Implementation now follows the jointly reviewed
[B/D v2 design](../analysis/UR-WORKFACTS-BD-DESIGN-V2-20260909.md), SHA-256
`0065cf2f2c7342fc652b2a4017eaec23666eb1c7755d7368c61776421b9c5ec7`, in the
same isolated worktree. The direct synthetic credential, local test, ASUS v2/N1,
source transfer and coordinator-report authorizations continue within their
original boundaries; no repeated user approval is required for covered actions.
No XI contact, real Provider/credential/traffic, push, merge, release or deployment.

B's implemented core and bounded cross-package regression have passed. D is
implemented in the local candidate: M118 transactional coverage
certification, project budget revision history, independent D scopes, explicit
enterprise project bindings, and catalog/current/alert snapshot readers. This
is ongoing implementation, not complete B/D/X acceptance. The A relative 5%
performance reference remains unmet; accepted A evidence is not B/D evidence.

The accepted dispatch amendment v2 (SHA-256
`471f360abad6b9b1c901025fe3ad866cf99e43cabe5225a17bd9c74851d5c113`)
is implemented without changing Authorize's RR, original retry budget or other
optional observation RR transactions. M117 now guards the original queue and
runtime-generation fleet; the original worker starts only after source and
runtime support checks. Local evidence under `/private/tmp/ur-bd-workfacts-20260910`:
- `dispatch-outbox-boundaries-v3-race.log`: eight transaction, concurrency,
  migration/Renew, replay/retention, RR visibility, maximum-payload and lease
  expiry tests pass. The boundary payload was 165055 JSONB bytes pending and
  165081 marked, below 262144; each source record remains bounded at 65536.
- `dispatch-main-startup-v1-race.log`: actual race-built main recovers pending
  events with both capture flags off; wrong source instance refuses before claim.
- `dispatch-public-recovery-v2-race.log`: original public paths and publication
  fault recovery pass, including current URL changes and late registration
  preserving an already observed result. The old health revision fence remains.
- Symmetric comparison is complete in `dispatch-performance-summary-v1.json`:
  all ten serial runs pass. Concurrent successes per 128 requests are A126,
  off125, B126, D123 and BD127. Every committed B/BD attempt has all three
  dispatch records; queues empty and original terminals/holds reconcile. Failed
  groups retain FAIL, successful-request latency is separate, and the large
  optional-observation gaps remain explicit. A diagnostic run identified the
  original attempt INSERT serialization path; no capacity claim is made.
- Broader checks exposed an old positional M113/M114 test ledger and stale
  reset table counts. Fixtures now use the exact head. The accepted D reset
  protection is also wired into the existing review-required refusal for all
  five D metadata tables and the journal; no reset ran. Counter v3 now accepts
  the already-supported M118 head. `dispatch-final-unit-delta-v1.log` passes.
Earlier failing logs are retained, including the missing sandbox network
permission in `dispatch-final-unit-race-v1.log`; it is not product-failure proof.

The current full CI integration selection passed in all unchanged packages;
its three old reset assumptions failed and are retained in
`dispatch-ci-selected-v1-race.log`. The independent coordinator's
`D-reset-boundary-review-20260910.json` accepted current M118 refusal and separate
historical A evidence. `reset-final-ci-delta-v1-race.log` passes the same complete
selector for the two changed packages. `dispatch-source-media-full-v1-race.log`
passes both complete source/media packages; vet and rollout tests also pass.
These are local executions of required checks, not GitHub CI runs.

M118 has no supported successful reset lifecycle, even with D capture disabled.
The real CLI writes a private read-only diagnostic with `scope_complete=false`;
`Unmarshal`/`Validate` and `Apply` refuse execution. The existing
`apply_supported=true` field reports implementation availability and grants no
authority without complete scope. An initial review incorrectly attributed the
old test's Unmarshal failure to CLI writing; actual execution corrected that
claim without changing the CLI. `reset-current-m118-refusal-v2-race.log` proves
diagnostics, plan/apply refusal and unchanged PostgreSQL/Redis state. The three
original A tests pass in `reset-accepted-a-exact-v1-race.log`;
`reset-historical-a-source-proof.json` verifies all 2482 source blobs against
accepted A and no extra Go files. This does not claim current-binary old-schema
reset support. Product/customer/contract notes disclose the restriction.

Coordinator communication was twice rejected by automatic approval review, so
those attempts were never recorded as delivered. The exact prior user approval
of network actions plus bidirectional reporting was then verified in this task's
original record; same-channel re-review passed and reporting resumed. Covered
actions continue under that authorization, without another user approval.
The one source closing review of `d60b833d95beccc4ee7fd1081a0906654f40079c`
found one product blocker, BD-CLOSE-01: an empty enterprise project allocation
slice incorrectly skipped the original run's currency/scale check. The repair
always checks run units and only conditions per-row checks on row presence.
The original admin Draft/Approve/AllocationRun and actual source read reproduce
both wrong-scale and wrong-currency zero cases on the preserved original code;
the repair passes both and the matching-unit exact-zero control. Evidence:
`bd-close-01-before-v4-race.log`, `bd-close-01-fixed-v1-race.log`, and
`bd-close-01-source-full-race.log`. Earlier test-input failures are retained.

The CI group count is 17 initially passing packages plus the two repaired reset
packages, not 18 initially passing. The three separately selected Redis gates
now pass against the current M118 candidate in `dispatch-agentcap-ci-v1-race.log`,
`dispatch-limitpolicy-ci-v1-race.log`, and
`dispatch-workloadadmission-ci-v1-race.log`. The performance executable is
byte-identical to exact d60 plus its original two test overlays; the binding is
in `perf-d60b833d-source-binding.json`, without rerunning the benchmark.
The closing review accepts the bounded dispatch-contention repair while keeping
mixed-error groups as FAIL. Further latency and optional-capture optimization
is deferred; this is not a performance pass or an ASUS/overall-acceptance waiver.
Only the repair delta review, reviewed-source ASUS validation and overall
acceptance remain. The d60 archive is preserved and must not be transferred.
The model-adaptation lesson admission criteria were checked; no applicable new
model/provider invariant was introduced, so the experience file needs no change.

### ASUS compatibility repair after source release — 2026-09-11

The coordinator accepted the exact 8e4af934 repair and released its source to
ASUS. The original Linux build helper produced backend image `sha256:9a04691bf81f8a09a2726ce0adce7f09f0f1e09298ffda103cfb944cf68acd98`.
Full M116 and M118 installations, source/media/admin/main race selections,
canonical lifecycle and actual PostgreSQL 16 backup/restore passed. The original
Docker-context CI body and rollout/ingress tests passed locally; the real owner
carrier used the same previously reviewed one-line explicit logging fixture.
It proves M117/M118 read carriers only; complete installation has separate proof.

The original M117 controller check then exposed a real blocker on the retained
M116 database: supported expired/absent identities left after a process restart
were required to match the two current receivers. Batch 08 proves the actual
CLI refusal with exact compact JSON, unchanged database/schema/sequences and
only two current host processes. Earlier stop-grace and diagnostic-serialization
fixture mistakes are preserved separately. No historical identity was rewritten
to force success. Every completed N1 batch restored task networking and stopped
containers, PostgreSQL and the dedicated WSL normally.

The coordinator's `BD-compatible-upgrade-review-8e4af934.json` accepted the
bounded repair before coding. Live or ingress-associated extra identities still
refuse; only expired/absent historical identities may use their own verified
canonical manifest and complete target requirements, including both counters,
source facts/observations/dispatch and M118 budget/journal. The migration bytes,
locks, Renew/bootstrap and host process inventory remain unchanged. This is not
retirement of unsupported identities or a new general upgrade lifecycle.
The fix and read-only/negative tests pass locally in
`compatible-recovery-fixed-v1-race.log`. New exact source/image validation and
retained-database M116→M117→M118 completion remain required before full delivery.
ASUS evidence is under `/private/tmp/ur-asus-a11-20260910`; the original performance
HTTP files now have a compact measurement index in
`dispatch-consumer-measurements-extracted-v1.json`, without rerunning benchmarks.

New D evidence in `/private/tmp/ur-bd-workfacts-20260910`:
- `source-d-m118-certificate-v2-race.log`: M118 fails/rolls back without a
  support certificate; exact ledger proof, atomic revisions, no-op/rollback,
  and immutable-history/down guards passed.
- `source-d-restore-metadata-v1-race.log`: the fixed restore SQL always rebases,
  retains prior certificates and A bytes, does not write on readiness, and
  preserves a pre-D restore. This is helper/DB evidence, not a full backup run.
- `source-d-original-enterprise-ab-v4-race.log`: original Draft/Approve/
  AllocationRun + explicit binding, scoped 600/1000 => 400, other-target privacy,
  original budget notification, D revoke and A/B backlog continuation passed.
- `source-d-financial-queue-v1-race.log`: original Freeze/Settle charged 600,
  prepared/settled queues with future availability/claim/retry/dead letter,
  materialized awaiting ack/replay, detail-only 200 exclusion and corrupted
  scope denial passed. Future UTC coverage cases are explicit synthetic event
  timestamps, not live future observations. The held-RR check subsequently passed in the boundaries export run.
- `source-d-ab-control-v1-race.log`: unchanged A/B explicit-grant tests and
  original enterprise route-role separation passed.
- One real D canonicalization bug (typed nil assigned to `error`, all valid
  snapshots rejected) was fixed and protected by exact bytes/money tests.
  Earlier fixture slug/type/assertion failures remain in their original logs.

D is default-off (`SOURCE_BUDGET_SNAPSHOTS_ENABLED`), requires A, M118 and the
original attached financial outbox. Main now attaches that original outbox
before source readiness, without starting another worker. D-only profile
changes preserve existing immutable key registrations and A/B generation.
The original SQL restore runbook/rehearsal calls
`scripts/source-budget-restore-rebase.sql` before any restored reader starts.
D metadata and the original financial journal are protected from resetplan;
no reset was executed.

Latest D/X local checks (still uncommitted, product metadata now 1.4.0):
- `source-d-boundaries-export-v2-race.log`: all D source/admin boundary tests
  passed, including old-key attenuation, current role/session commit checks,
  original audit failure rollback and concurrent binding revocation.
- `source-d-independent-http-v2.json`: 4 actual HTTP samples, 5 positive and
  8 negative schema/hash/math vectors, immutable SQLite reading retention.
- `source-d-restore-bootstrap-doc-v2-race.log`: actual pg_dump to a separate
  empty task DB, lost post-backup original charge, mandatory coverage rebase,
  preserved A/definition bytes and no false remaining; original first-customer
  bootstrap and API/route parity passed. Historical definition identity now
  survives original empty-project removal; current binding FK remains enforced.
- `source-d-ui-v2.log`: 10 UI tests passed. Full frontend type-check, 90 files /
  749 tests and production build passed (`source-bd-frontend-*-v1/v2.log`).
- `source-bd-full-unit-race-v1.log`: full Go race ran; only missing frontend
  route documentation failed, subsequently fixed and rerun above. `source-bd-vet-v1.log` passed.
- `source-bd-ci-selected-v1-race.log`: original CI selection completed; three
  old migration fixtures failed due to new schema predecessors. Guarded fixture
  ordering was fixed; `source-bd-legacy-fixtures-v2-race.log` passed all affected
  Feed/successor tests. No production down guard was weakened.
- `source-bd-source-media-admin-v1-race.log`: all source/media passed; initial
  bootstrap failures were fixed and rerun in the restore/bootstrap v2 evidence.
- `source-d-m118-rollout-v2-unit.log`: 70 finite-transition/recovery tests passed.
  `source-d-restore-script-v2.log`: password-boundary regression passed, including
  rebase after restore and before validation. VERSION/frontend metadata 1.4.0 match.
- Ten-thousand-scale assumptions were not used: `budget-scale-v1` contains real
  EXPLAIN over 100,000 synthetic journal and 100,000 outbox rows. Payload-org
  mismatch detection scanned unrelated queues; the narrow financial payload-org
  index passed. `budget-scale-v3` also puts 100 projects in the same organization:
  the settled subtotal uses the project/period index (0.185ms) and materialization
  checks use the two financial indexes (0.061ms). This is synthetic query-scale
  evidence, not original financial-writer or production capacity evidence.
- `source-d-final-v5-race.log`: final original charged/calculated distinction,
  actual PG16 backup/restore, boundaries and scale passed. `source-d-runtime-support-v1-race.log`
  passed missing schema/budget/journal support, including expired recovery writers.
- `source-d-independent-http-v3.json`: independently checked the later actual
  HTTP snapshot samples, hashes, arithmetic and retained reading identities.
- `source-d-browser-v3.log`: built frontend plus real login/control handlers passed
  A+D registration/activation, restricted source read, binding/revoke/rebind
  revisions 1/2/3, pause (`503 source_paused`) and resume. No browser page errors or
  external browser requests. Screenshots/text are under `output/playwright/source-budget-v3`.
  Earlier ambiguous locator and incorrect pause-status fixture assertions are retained.

**Current B blocker:** ten symmetric sequential groups in `perf-v3-results.json`
passed, but B median latency is about 10.96ms versus accepted A 7.55ms; this does
not pass the relative 5% reference. Four-credential concurrent groups in
`concurrent-v4-results.json` expose 36–42/64 HTTP 503s for B/BD versus 0–2/64 for
A/off/D. No mixed-failure p95 is accepted. The coordinator independently matched
all ten logs and classified B/BD as `validated_blocker`.

`concurrent-candidate-diagnostic-v2.log` locates SQLSTATE 40001 at direct B
source-stream updates inside original Authorize RR (33 failures), and original
provider-attempt insert (4 failures), after the existing one retry. A narrow
[dispatch contention amendment](../analysis/UR-WORKFACTS-B-DISPATCH-CONTENTION-20260910.md),
SHA-256 `ff9ab1f5f33fd93b5ea166fa1047588eff60e647268cc4a84a3e8ce1fe209d8a`,
was independently reviewed before implementation. Its amended v2 object,
SHA-256 `471f360abad6b9b1c901025fe3ad866cf99e43cabe5225a17bd9c74851d5c113`,
is now jointly accepted: durable pre-dispatch recording through the existing
outbox with later source publication, base64-frozen bytes, monotonic live-event
marker, capture-off recovery, down guards and exact handler support. The accepted
original design and amendment identities are retained separately.

The amendment is being implemented, not yet fully verified. Core source tests
passed in `dispatch-outbox-boundaries-v2-race.log`: original transaction rollback,
capture-off recovery, exact JSONB/base64 round trip, late first publication,
lease rejection, immutable marker/body, real retention and non-resurrection,
original worker ack, and all retained-event down states. Public/stream gateway
tests passed in `dispatch-outbox-public-v2-race.log`, including durable enqueue
failure before Provider and destination capture before the queued registration
is published. Earlier canonical-input error and empty-selector run remain
recorded and are not counted as passing evidence. Worker Run now follows actual
runtime-generation startup authority; startup/generation concurrency evidence
and the new performance comparison remain pending. No B/D source transfer yet.

Remaining: agreed B contention fix and repeated affected concurrency/performance,
final changed-code checks, customer/contract packaging, reviewed ASUS B/D candidate and Docker/N1,
one closing independent review and complete coordinator delivery. No B/D archive
has been transferred or accepted; no GitHub CI/Target/XI/Production claim.

Five B profiles use a new source identity domain; A's three profiles, immutable
bytes, descriptors and cursors remain separate. The finite observation-kind
sets and each 64KiB document limit are in `observation_contract.go`; measurement
boundaries have distinct identities and nanosecond representation is not a clock
accuracy claim. No per-token/frame/heartbeat publication is introduced. Final
per-handler row/byte bounds must be tied to the existing retry/policy/media
limits before the corresponding writers are enabled.

Implemented in the uncommitted B candidate: M117 finite bridge and explicit
profile expansion, independent v2 event/cursor transport, original coordinator
pre-dispatch transaction hook, post-commit Provider/delivery observations,
combined A+B journal terminal publication, and process-local response/stream/HTTP
measurements. B remains disabled unless SOURCE_OBSERVATIONS_ENABLED=true and A
capture is enabled. Only explicitly registered B profiles can be read.

Current Local evidence is under `/private/tmp/ur-bd-workfacts-20260910`:
- `source-dispatch-boundary-m117-race.log`: original coordinator source rollback
  and both landed/rolled-back ambiguous commit recovery passed.
- `observation-admin-source-lifecycle-race.log`: original A lifecycle plus new B
  explicit grant, profile narrowing, independent cursor, pause/revoke tests passed.
- `observation-monotonic-optional-race.log`: source M117 guarded upgrade/down,
  Responses/Messages original nonstream and complete/interrupted stream paths,
  and optional observation failure preserving original money/delivery passed.
- `observation-bounds-measurement-unit.log`: separate timing boundaries, true
  zero/missing distinction and 128 requirement/64 evidence entries (supported
  ASCII capability identifiers, worst escaped evidence states) encode to 46139
  bytes without truncation. This is not yet the complete per-handler bound.
- Independent Draft 2020-12 validation: 10 positive and 8 schema-negative vectors;
  the remaining identity-negative vector is rejected by the Go domain validator.
- `runtime-current-source-authorization-race.log`: original stream/media leases,
  expiry/fences/current-only pagination and authenticated profile/lifecycle scope
  passed. Synchronous requests remain explicitly outside lease inventory.
- `policy-complete-boundaries-race.log`: actual policy detectors and gateway
  shadow/enforce, input/output replacement/block, degraded/fail-closed and partial
  stream termination passed; metadata buffers retain no input or detector findings.
- `media-b-real-http-worker-v3-race.log`: actual media HTTP/auth/worker/Provider
  paths passed for legacy and grant identities. Each yielded 31 authenticated A/B
  facts over 19 pages. Independent v2 Python schema/hash/identity and SQLite
  restart/checkpoint/replay consumer passed both exported synthetic fixtures.
- `media-main-startup-matrix-v2-race.log`: the real race-built main binary passed
  blocked/mismatched source readiness and five media/A/B enablement combinations.
  Readiness refusal leaves queued/expired jobs, objects and source/money unchanged;
  successful readiness permits both original loops and original SIGTERM shutdown.
  This implements the coordinator's `media-worker-startup-review-20260910.json`
  validated_required decision; it does not promise no side effects before unrelated
  later startup failures. The same run passed original pre-dispatch cancellation
  with a B terminal observation and no manufactured provider attempt.
- `source-b-original-output-holds-race.log`: original nonstream/stream holds,
  commit ambiguity/rollback/replay and optional source failure preserve the
  original pending financial state; B records output_held after the commit.
- `source-b-native-media-http-matrix-v3-race.log`: complete A/B native Chat,
  Embeddings, cache, six identity kinds, actual two-route fallbacks, stream
  cancellation/tool output/Write/Flush faults, and media success/fault cases pass.
  `native-media-http-ab-pages-v3-independent.json` independently validates 61
  exported authenticated fixtures, including 32 B-containing cases with original
  A/I-01 joins and durable SQLite restart/checkpoint/replay checks.
- `source-b-native-admission-policy-v1-race.log`: rejection is now captured before
  the first 4xx header when no original admission exists; source failure returns
  503 with zero Provider calls. Original policy and middleware transport tests pass.
- `source-b-native-fallback-context-v1-race.log`: two actual attempts retain one
  original I-01 terminal; request_context_done conveys no human-cancel or Provider
  timeout inference. `source-b-independent-contract-v2.json` verifies both schema
  pins, source identities and all current observation/runtime positive/negative
  vectors. T01–T10 semantic vectors are separate from real HTTP observations.
- `source-b-current-probe-negative-race.log`: an active original routing Probe
  cannot enter the execution-lease inventory; probe-only sync remains last_known.
- B profiles are now exposed explicitly in admin/UI and OpenAPI; default-off
  config, M117 rollout classification and the actual M114/115/116/117 runtime CLI
  matrix are covered by `source-b-profile-ui-tests.log`,
  `source-b-available-profiles-admin-race.log`, `source-b-openapi-tests.log`,
  `source-b-rollout-m117-v2-unit.log` and
  `source-b-runtime-cli-m114-m117-v2-race.log`.

Retained test corrections: shared Redis originally collided on disposable PG
org/API-key IDs, and the first isolation attempt exceeded the original Freeze
int4 API-key field. Source HTTP fixtures now use separate IDs within that supported
range; no product limit is relaxed and shared Redis is not flushed. The transport
assertion now selects the last append-only receipt by sequence, rather than its
first write_started row. These were fixture corrections, not product fixes.

Later edits still require rerunning affected evidence. Conservative B row/raw-byte
bounds are documented in `contracts/source-facts/v2/README.md`; they exclude
DB/index/WAL overhead and do not replace benchmarks. Remaining B regression and
candidate performance review, D budgets and
coverage/restore, product version/OpenAPI/docs, full X regressions/performance,
review and fresh ASUS batches remain outstanding. Old failures remain retained.
No new tool was installed and no accepted A evidence was rewritten.

## Active local delivery — UR work facts A

2026-09-09: the user directly reaffirmed in the UR task the workflow of independent
UR planning, review and mutual agreement before execution, followed by full
delivery and independent acceptance. The planning/review condition is satisfied:
About Me coordinator `01a08371-5a0f-7d32-856e-340ccc778825` accepted the
[exact plan](../analysis/UR-WORKFACTS-LOCAL-PLAN-20260909.md), SHA-256
`0e504bffc251c815680485f1b2dab8b69b335e682ec4d66e586b9b7923a07cbd`.

Owner `01a08339-f189-7703-acd9-5d0a63a2873b` implements UR-A and necessary
X configuration/migration/recovery work in `/private/tmp/unirouter-workfacts-20260909`,
branch `codex/ur-workfacts-20260909`, base `cd9f354b8a104a6f0677345a5694b19c000ab015`,
tree `7725ffa81b6f6897f5ef6ff843f308c05d44fd63` (1.2.77).

Scope remains local implementation, synthetic identities/fake Provider,
task-owned PG16/Redis, tests and local commits. No push/PR/merge, official
signing/release, deployment, real credentials/calls, existing services,
user-data deletion or system installs. Do not message XI; collect its dependencies
in the final package. The other delivery below retains its historical scope and
authority; none is inherited by this batch. Required A01–12/I2, old V1,
protocol/auth/operation, migration/recovery and performance evidence remain in
the accepted plan.

Current state: local candidate 1.3.0 implements captured context, fixed source
profiles, transaction-bound publication, persisted billing basis, media writers,
M116 and the finite runtime/install compatibility changes. Targeted source and
middleware tests pass. Task-owned UTF-8 PG16 tests pass for M1–M116, immutable
publication, rollback/replay, retention, self-contained terminal facts and I-01
references, late capture, and media cancellation/expiry/success without invented
I-01. The rollout script's 66 tests and targeted route/OpenAPI/docs parity pass.
Further Local evidence: the real two-pass grant authentication/public relay/
billing path passes Responses and Messages non-streaming tests, including two
server requests for one client correlation, historical owner retention after
live-key mutation, and frozen observation economics after a mode change.
Responses stream success, Messages V2 success/interruption/client cancellation,
and Messages V1 stream refusal pass. Required source-write failures return 503
before dispatch or retry via the original financial outbox after settlement;
recovery preserves one dispatch/journal/I-01 and replay does not rebuild expired
facts. Eight independent valid and nine invalid frozen source vectors pass.

M116 atomicity, busy-owner yield, missing runtime implementations, unsafe down,
and old/new finite ledger tests pass alongside historical M114/M115 tests. Full
sourcefacts/mediajob integration race passes, including reverse profile input
order under concurrent publication. Full Go race initially exposed stale route
assertions and five unclassified source tables in reset preview; both affected
packages pass after correction. Source-bearing reset previews now preserve the
new data and block the older erasure scope; empty M116 keeps the existing path.
This protective compatibility decision still belongs in the final coordinator
review; no reset action was performed. `go vet ./...` passed before that focused
correction; affected-package incremental vet subsequently passed.

Frontend type-check, production build/bundle budget and all 88 test files / 739
tests pass. Offline dependency installation initially omitted native canvas;
the exact cached canvas 3.2.3/NAPI7/darwin-arm64 prebuild restored the required
environment without downloading or installing a system tool. Current CI's
consolidated integration selector, full admin Integration group and all three
sequential Redis groups passed against the task-owned services. Two runtimechange
historical fixtures initially omitted guarded M116 down; both passed after that
fixture correction. These are Local results, not GitHub CI/exact-main.

Additional actual-handler evidence now covers Chat and Embeddings under legacy
shared/personal/application keys, personal/workload grants and delegated Agent
capabilities (12 combinations), with real planner/coordinator, policybilling,
capbilling, PG16, dedicated Redis and a local fake Provider. Replay/conflict,
model denial, unauthenticated ingress and empty-wallet admission preserve one
execution/economic result. Historical owner/application/Sponsor/Agent facts
survive live ownership changes, Sponsor deactivation and the test workflow's
Redis expiry; Sponsor does not become a personal usage owner.

An actual PG16 custom-format pg_dump/pg_restore into another new temporary
database preserves exact hashes of 12 source/financial/recovery tables, including
retention floor and pending outbox. Continuing the original worker preserves
I-01 hashes and one terminal, with no Provider redispatch. Receiver credential
and authenticated cursor recovery are still separately pending. Fourteen frozen
public-page fixtures match actual Go Event projection; an independent Python/
SQLite consumer verifies seven invalid pages, the original R1–R5 individual
amounts/scopes, both I-01/I-02 arrival orders, partial-page restart, checkpoint
failure, repeated receipt and consumer-adapter replay (100→105 CNY calculated,
zero charged). This is offline fixture evidence, not authenticated source pull
or XI integration. Source-page/consumer fixture check is retained in local CI code.

### A12 jointly reviewed local performance boundary

The coordinator independently reviewed the raw base/candidate ABBA samples,
overlays and implementation, then approved bounded SQL batching within the
existing transaction and lock order. The source publisher now uses two bounded
SendBatch calls (rather than per-statement calls), not a new queue. Read result,
read/write Close failure, cancellation, revision predecessor, immutable replay,
reverse profile order, original outbox recovery and PG restore tests pass.
SendBatch call count is a code fact, not a measured network round-trip count.

After batching, 10 warmup + 60 measured requests per batch, base/on/off/off/on/base,
showed request p95 base 6.909125/6.771916ms, on 8.034750/7.701084ms and off
6.632125/6.609917ms. Enabled increments were 1.125625ms (+16.2919%) and
0.929168ms (+13.7209%); no increment was observed with capture off. Raw samples
are retained under `RDgTyF/perf-v2-results.json`, alongside the untouched first
round. The 5% relative experiment suggestion was not reached. Per the coordinator's
explicit joint review on 2026-09-09, this bounded approximately 1ms incremental
cost is acceptable for this 6–7ms fake-Provider Responses microbenchmark under
the existing ≤5ms absolute p95 increment suggestion; it is not a performance
promise for other protocols, loads, B stages or Production. No artificial
Provider latency was added to dilute the percentage. Original response, amount,
consistency, recovery and default-off requirements remain.

No other task-owned PG write test ran concurrently with that measurement. WAL
is instance-wide (including possible background activity); relation growth
includes page/index allocation and is not steady per-request storage. Current
samples do not establish a clear latency improvement from batching. Concurrent
load/lock sampling and original-worker catch-up are being measured separately;
authenticated page/consumer latency depends on the pending credential approval.
A12 as a whole, A01–12 and complete candidate acceptance remain open.

### Joint correction of the original cache-path assumption

The coordinator independently verified the exact cd9f354b baseline and agreed on
2026-09-09 that the accepted plan's statement that cache hits create no Freeze
or I-01 was incorrect. Preserve the original plan as historical review evidence.
The actual Chat path freezes before cache lookup. A cache hit retains the existing
zero-token, zero-calculated/charged delivery preparation, receipt, settlement and
recovery. It makes no new Provider attempt or call and cannot repeat the original
miss request's economic amount. With I-01 enabled and a valid project, the original
journal materializer also publishes that distinct zero terminal; it has no
zero-amount exclusion. The source must describe this existing path without
changing the financial contract. Actual miss-to-hit validation now passes with
real ten-request Shadow admission and the original Canary gate: one Freeze,
zero Provider attempts/calls for the hit, an independent zero-token/zero-amount
I-01 terminal and seven source facts with the exact I-01 hash. The earlier miss
snapshot stays unchanged; omitting opt-in keeps the Provider path. All thirteen
HTTP matrix branches pass with race detection. Existing cache trace projection
warnings remain recorded; this does not claim that trace delivery passed.

The first four-worker, same-key/route diagnostic already failed on 1.2.77.
Its follow-up preserved 64 outcomes: 8 HTTP 200, 40 HTTP 409 provider-cost-pending
and 16 HTTP 503 (14 public execution unavailable, 2 execution state unavailable).
The 409 guard also covers the existing delivery-pending window; it is neither
proof of the initial cause nor by itself a product defect. Auth can reject before
capture, so the diagnostic's candidate assertion that every request has at least
two facts was invalid. Those initial failures remain in the raw logs. A bounded
diagnostic now records states and errors; mixed response rate/latency cannot
establish successful-request throughput, capacity or candidate compatibility.

The additional PG16 diagnostic located SQLSTATE 40001 inside M36's
`mark_micro_ledger_runtime_evidence`: every ledger statement rewrites the same
already-marked singleton. The coordinator corrected the initial overstatement:
the excerpt contains fifty marker UPDATE contexts and six deployment-parent
foreign-key key-share contexts. M36 is a confirmed hotspot, not the sole
proven cause of all concurrent errors. Base, capture-on and capture-off all hit
the marker context during owned attempt insertion/health fence update. The
three 64-request samples had 13/4/3 successes, 34/56/59 pending refusals and
17/4/2 service errors, with 2/1/1 unresolved holds afterward. The candidate-on
sample's database publication lag p95 was 57.68335ms after a manually started
original-worker catch-up; it is not continuous-worker, authenticated pull or
consumer latency. No performance compatibility/capacity pass follows from these
diagnostics. The coordinator independently approved revised marker patch v2,
SHA-256 `2a491944f6842ac38b12b6947e486e54a8e8f90153b1d70080c72205362674c9`,
before implementation. It is now applied: prelock the exact projects parent
with NOWAIT, then replace the marker function only after dependent DDL. The
first rejected patch remains retained. Current M116 up/down hashes are
`f94c3617d33bece9b77871151a8f8330c9566b97898a0f3491dec71f765e7357` and
`92be74583b6f9706fac9116b668806d45746c09ffe4890908b792133fdc8b88c`.
Fresh-database tests cover actual M115/function predecessors, cold first-commit
and rollback interleavings, old RR snapshots, warm marker tuple/first-time
stability, busy projects/marker owners, migration failure rollback, all eleven
original triggers and unsafe M36 down rejection. All pass with race detection.
No isolation, retry or financial guard was relaxed.

After that change, another candidate-on/off four-worker diagnostic still had
5/5 HTTP successes, 57/58 pending refusals and 2/1 service errors, with 2/1
remaining holds. The three observed service errors occurred at the original
model-deployment health FOR UPDATE under RR, not the marker. Mixed response
latency/rate is not successful capacity, and the few successful samples do not
support a compatibility pass. Skipping an unchanged breaker update alone would
not prove closure: success also updates credential last_used_at in the same
health transaction. The coordinator subsequently classified that original
health failure as a current A12 blocker and independently accepted the bounded
health v2 plan before implementation (see the evidence below).
Raw samples, diagnostic overlays and PG excerpts remain in `RDgTyF`, including
`perf-concurrent-marker-v4-results.json` and all earlier failed experiments.

Latest independent checks pass: complete sourcefacts/mediajob race integration,
all thirteen Chat/Embeddings/cache HTTP cases, the frozen-page SQLite consumer,
affected source/gateway/main vet and whitespace validation. Tests use isolated
database clones. The cache test now uses a new server-operation prefix per run
and stays below the unchanged 10 RPS key limit; earlier fixture failures (429
and cross-run Redis operation collision) remain in separate logs.

The post-marker CI consolidation, complete source/media, admin, Agent,
limit-policy and workload-admission integration groups all pass (six exit-zero
groups in `marker-integration-results.json`). A fresh opt-in PG16 dump/restore
also passes with the actual current marker function definition compared before
and after restore, alongside the twelve table digests and original worker replay
(`source-marker-pg-restore-race.log`). Public Responses/Messages tests preserve
legacy wildcard/exact-endpoint denial and Messages header rejection before
capture/Freeze/Provider. Explicit endpoint grants on a real synthetic agent
workload support Exchange/Delegate/public execution and seven source facts;
personal grants cannot mint that workload authority, and Sponsor stays separate
from owner. These four public-auth cases pass with race detection in
`source-gateway-workload-auth-race.log`; fixtures use a fake Provider and synthetic
model authorization, not production licenses or actual Provider qualification.

### Reviewed health correction and further local evidence

The immutable health proposal v1 remains the prefix of
`docs/analysis/UR-HEALTH-CONCURRENCY-PLAN-20260909.md`; its appended v2 was
independently accepted at SHA-256
`fc6da3284d210adce183febdaefe02b7fcf825e7c98331cfa718617bab29ad3c`.
Implementation changes only `applyHealthOnce` to Read Committed and binds its
ten shared projection `updated_at` writes to the existing database time obtained
after the relevant row locks. Existing current-row validation, attempt fence,
CAS, atomic mirror writes, replay and commit-ambiguity handling remain.
Authorization stays Repeatable Read; retries and unresolved-hold protection
are unchanged. This approved design is not yet an A12 or full-A pass.

Twelve deterministic canonical/legacy/verify interleavings pass: an earlier
transaction acquiring rows after a later health commit, opposite outcomes,
new revision rejection, exact replay and the uniform lock-time timestamp.
Six actual PostgreSQL lock-wait and cancellation cases also pass. Full original
modelexecution integration initially failed three old fixtures before health
because their disabled/expired lease writes violated M70. Correcting only the
fixture predecessor (clear probes when disabling; await a real one-second
lease) makes both affected original groups and all eighteen new cases pass in
`health-current-wait-and-supported-lease-race.log`. The raw full failed log is
retained as `health-current-row-race.log`.

The real pre-v2 `ReconcileLegacyProjection` writer is blocked on the parent
while health owns it, and adds no fanout member before health commits. Its
new link nevertheless uses channel/key times read before that wait. The
additional assertion expecting the later health time failed, as recorded in
`health-reconcile-parent-timing-race.log`. Replacing only the four production
health files with exact cd9f versions via Go overlay reproduces the earlier
link time (`health-reconcile-parent-before-fix-race.log`). The parent-lock
proof passes; the cached-time assumption does not. This existing writer reads
channel/key metadata before upserting the parent. Full projection readback then
found an actual additional effect: canonical binding `last_used_at` is overwritten
to NULL while the legacy key retains the health time. Candidate
`health-reconcile-full-projection-race.log` and original four-file overlay
`health-reconcile-projection-before-fix-race.log` fail at the same credential
parity assertion. The earlier old-overlay timing test stopped first on the old
NOW()/DatabaseTime mismatch; its printed link time, not that failed assertion,
was the evidence of the existing early read.

The coordinator independently classified the cached link time and the actual
credential mirror overwrite as separate, bounded `valid_deferred` findings.
`deploy/customer/README.md:43–66` and the deployment runbook place explicit
verify preparation in an approved maintenance window with old writers drained
before startup/cutover. `Service.Apply` only accepts verify and the reconciler
rejects any current v2 authority. Online apply overlapping active health is not
an added support promise. Runtime protection does not completely exclude that
overlap; it is not claimed to be safe. The supported no-overlap order preserves
the unified link time and full breaker/credential parity. Original probe scope
and history regressions pass. No reconcile production writer was changed.

The failing full-parity source is preserved as
`RDgTyF/health-reconcile-overlap-regression-v1_test.go`, SHA-256
`3d3b668e4f386932bc8d3aac6e9e25699a777ad7e0e7ce649edda15a9edb1e08`.
The normal regression distinguishes parent/member commit protection from the
supported maintenance-order consumer/parity requirement. The separate
`TestKnownReconcileOverlappingHealthProjectionDiagnostic` retains the failing
assertion and runs only with `UR_RUN_KNOWN_RECONCILE_OVERLAP_DIAGNOSTIC=1`;
its default skip is not diagnostic success. Original FAIL evidence is retained,
not relabeled as PASS. Reopen these findings if a supported online apply path
is identified, drained maintenance still loses health or wrongly grants probe
eligibility, or a candidate-specific difference is demonstrated. This does not
prove other health values, all fanout states or qualification outcomes safe.

The main actual-handler matrix now has 21 cases: six Chat, six Chat SSE, two
partial/cancelled Chat SSE, six Embeddings and one cache case. All six streaming
auth combinations pass. The initial full run passed nineteen and exposed an
incorrect new-test assumption for the two partial Chat streams. Original Chat
uses delivered-counter partial settlement (`gateway_counter`,
`customer_charge`, `estimated`) rather than the strict Messages/Responses V2
pending contract. Both corrected assertions pass with one I-01, seven matching
source facts, zero charged amount and no repeated Provider call in
`source-chat-partial-counter-race.log`. The initial failure is preserved.
After the health change, the complete 21-case invocation exits zero with race
detection (`health-source-http-21-race.log`, SHA-256
`d98cc1e365ea233166eea96abe7b45a2d164e573e8e650feafbb962cf3b73216`).
Six actual same-attempt concurrent replay/opposite-outcome cases also pass;
the first outcome remains unique and a conflicting outcome is Stale. Original
reconcile probe history and deployment credential-scope negative tests, plus
the full source Responses/Messages auth/stream/economic recovery and original
concurrent outbox groups, all pass in the four-package
`health-same-attempt-probe-gateway-race.log`.

Four original outbox service instances (batch four) concurrently publish the
terminal facts for 32 actual fake-Provider Responses requests. Continuous profile
ordering, matching I-01 order/hash, one terminal per request, replay stability
and empty pending queues pass in `source-original-outbox-concurrent-v2-race.log`.
The initial batch-one experiment exposed existing high-priority starvation in
the original scheduler; its failed log is retained and the independent finding
was sent to the coordinator. The default batch is 25; no scheduler change was
made. Context tests also pass for int64 IDs, lineage copies, UTF-8/control/size
bounds and JSON escaping amplification (`source-context-bound-unit.log`).

Media tests now cover dispatched expiry after early facts have been retained
out: the real expiry writer emits a self-contained unknown terminal retaining
the exact historical seed. Revoking an ordinary synthetic model key between
admission and reservation blocks attempt/dispatch while preserving admission
facts. Both pass in `source-media-late-revoked-v2-race.log`; the first failed
attempt correctly rejected completion with an expired lease, and its log is
retained. No production lease guard or credential control path was changed.

### Final health candidate performance and remaining diagnostic limits

With the reviewed health change, the same four-worker/64-request experiment
records on=16 HTTP 200, 47 pending 409, 1 authorize 503; off=22 HTTP 200,
42 pending 409 and no service error. Both have zero health failure and zero
residual holds. After normal original-worker completion, one new request per
group succeeds without clearing holds or retrying that request. The on group
has 27 Provider calls and 27 successful I-01 records at 600 calculated micros
and zero charged, plus one pre-dispatch 0/0 cancellation; 28 total terminals,
196 source facts, no duplicate terminal and no I-01 hash mismatch. Off has
33 calls/terminals and zero source facts. The on diagnostic exits 1 because
it explicitly rejects service errors; off exits 0, still with legitimate 409s.
Raw outcomes are in `perf-concurrent-health-v5-results.json`, SHA-256
`1fd396124ba6d4617aef0b500b0216f4af53f97c9e5647b01dd540d362bfcf13`.

The remaining on-group 503 comes before Provider execution, from the original
RR authorizer's owned-attempt INSERT and deployment FK KEY SHARE (SQLSTATE
40001). Its original cancel path finishes with zero amount and no hold. The
coordinator accepts this exact category as a separate `valid_deferred`
availability gap. The category appeared in the original baseline, but these
small on/off samples do not prove an unchanged error probability or that source
capture cannot amplify it. Reopen if it produces dispatch, charge, retained
hold, source/I-01 mismatch, or a demonstrated new/amplified candidate defect.
Authorize RR, M77 attempt-first, FK and retry contracts remain unchanged.
No overall concurrency/capacity pass or elimination of all 40001 is claimed.

The final code was also recompiled for an independent six-group sequential
comparison: 10 warmup + 60 measured requests, old base/on/off/off/on/old base.
All six exit zero. Request p95 in milliseconds is
6.801583 / 7.689084 / 6.746000 / 6.672584 / 7.657375 / 6.690750.
Supply-on increments versus the same candidate off are 0.943084ms (13.9799%)
and 0.984791ms (14.7588%); versus the old base they are 0.887501/0.966625ms.
The 5% relative suggestion remains unmet, while the previously jointly accepted
bounded <=5ms absolute increment holds. Health improvement is not subtracted
from source overhead. Samples are in `perf-health-v6-results.json` and the six
`health-v6-*-perf.log` files; no other PG write test overlapped measurement.
WAL on is 2,921,696/2,917,528 bytes, off 1,855,560/1,855,384; relation growth
on is 2,400,256/2,392,064, off 1,261,568/1,261,568. These include allocation,
indexes and instance-wide WAL effects, not steady per-request storage. Neither
this microbenchmark nor the mixed concurrent DB-publication lag measures
authenticated pull-to-consumer delay. Those and full A acceptance remain open.

At this health cutoff, `go vet ./...` and `go test -race ./...` both exit zero
(`health-final-all-vet.log`, `health-final-all-race.log`); the latter reports
124 package outcomes, including packages with no tests, and no race warning.
Nonfatal macOS linker warnings remain in the raw log. The supported parent-lock
and maintenance-order consumer tests pass (`health-supported-reconcile-race.log`),
as do all three selected original apply/check groups
(`health-maintenance-apply-check-race.log`). The explicitly requested known
online-overlap diagnostic still fails at the retained credential parity assertion
(`health-known-reconcile-overlap-opt-in-race.log`); that is recorded separately
from default test results. The independent frozen-page SQLite consumer also
passes again. Original checkout branch/HEAD/tree, all eighteen recorded file
hashes and porcelain status are unchanged; only the isolated worktree was edited.

The source credential control handlers/routes are not written: automatic
approval review twice rejected their credential lifecycle/permission code even
after the accepted plan and default-off evidence were supplied. A direct user
question is pending for local implementation and synthetic tests only. Continue
independent source, migration, protocol and documentation work while that exact
part remains pending. No real credential or permission action was attempted.

Test state lives only under `/private/tmp/ur-workfacts-test.RDgTyF`: PG16 uses
`pgdata-utf8` and its private Unix socket, port 65419; dedicated Redis uses
127.0.0.1:16649. Existing user services are excluded. Ordinary-sandbox full
runtime tests hit an httptest socket permission error; that is an environment
failure requiring a scoped test rerun, not a product pass.
Docker runtime and exact Neutral N1 local artifact remain unverified environment
dependencies; they do not block independent local work or become skipped passes.
After complete A delivery the coordinator reviews acceptance before B/D/X.

### B/D design prepared while A remains unaccepted

The coordinator explicitly authorized B/D design preparation only while A's
source-control approval remains pending. The independent proposal is
[UR-WORKFACTS-BD-DESIGN-20260909.md](../analysis/UR-WORKFACTS-BD-DESIGN-20260909.md),
SHA-256 `c22f9895803edc777030e83bb429f0798219e6ff6875fe7c6a12439482d94f27`.
It maps original R/CT2/T/DATA/B/FA3 acceptance to real writers, transaction order,
bounded profiles, timing, budget authority and recovery. Two explicit review
points are a separate D current-snapshot contract and a controlled enterprise
project-scope binding; arbitrary scope_ref strings do not authorize project
access. Existing project counters do not prove full-period coverage.

No B/D product implementation or tests were performed. The accepted A plan,
its review and health plan remain byte-identical. A full delivery and independent
acceptance still precede B/D product implementation, even if this design is
accepted. The source-control auto-review blocker is unchanged; this design
neither applies its proposed patch nor grants new credential authority.

The coordinator reviewed the exact draft and classified four planning gaps as
validated_required. The consolidated revision is
[UR-WORKFACTS-BD-DESIGN-V2-20260909.md](../analysis/UR-WORKFACTS-BD-DESIGN-V2-20260909.md),
SHA-256 `0065cf2f2c7342fc652b2a4017eaec23666eb1c7755d7368c61776421b9c5ec7`.
The first draft remains unchanged. V2 separates mandatory original/dispatch
records from post-commit optional observations, adds bounded reads of actual
live stream/media leases, preserves the original I01/source/budget outbox order,
specifies journal/outbox coverage and mandatory rebasing after supported
restores, and restricts enterprise binding writes to the existing platform/billing
roles with a D-only revocation generation. Unchanged A/B cursors must continue
catch-up across D-only changes. The coordinator independently reread the exact
V2 and accepted all four planning corrections as the joint design. No further
formal plan review is required for that unchanged object. This is not product
acceptance or permission to start B/D before complete A acceptance.

### Source control implementation resumed under direct approval

On 2026-09-09 the user explicitly approved: “同意按提案，在隔离工作树实现源凭据生命周期及权限控制，仅使用合成身份进行本地测试”.
The prior source-control implementation blocker is therefore resolved. The owner
applied the retained proposal in the isolated worktree, added exact staged-key
identity checks and registration/IP replay checks, and implemented the management
UI against those real handlers. Live commit authorization, purpose isolation,
profile changes, pause/rotation/cancel/revoke, immutable authenticated pages and
real synthetic PG credential/cursor restoration now have passing Local tests.
The following earlier dependency table is a historical cutoff, not a current
request for the same approval. A final evidence index below this section records
the resumed candidate and remaining independent dependencies.

### A unblocked closeout and exact remaining dependencies

After that design acceptance, the owner checked the accepted A01–12 matrix
against current implementation and retained evidence. One independent gap was
closed: the actual source HTTP reader now has sixteen negative integration
cases for disabled capture, write methods, profile/query/page limits, missing
or duplicate/malformed authorization, an ordinary model token and unauthenticated
capabilities. All pass with race detection against the task-owned PG16, with
no change to source/credential/ack/business row counts and zero Provider calls.
No source receiver was registered or activated. The existing CI
`TestSourceFacts.*Integration` selector already includes this test.

Test: `internal/gateway/source_facts_read_boundary_integration_test.go`.
Evidence: `/private/tmp/ur-workfacts-test.RDgTyF/source-reader-negative-boundary-race.log`,
exit 0, SHA-256 `2fc807e7a0d331db85a133679ddd4fe441e00fe2f9110a8e2ec3986eff7be1c4`.
It does not prove accepted receiver-purpose/profile authorization or successful
pull. Runtime product code is unchanged since the health cutoff; prior fresh
Go/frontend/source/migration/performance results remain applicable and were not
rerun just to increase test count. The opt-in known failures retain their
separate valid_deferred classification and actual failing logs.

| Unfinished A requirement | Exact dependency and currently available evidence | Can it independently finish now? |
| --- | --- | --- |
| Source lifecycle handlers/routes and existing connection-management UI | Direct user approval is still pending after two automatic-review rejections of registration/activation/rotation/pause/revoke/profile-permission code. The review-only patch is retained and absent from runtime targets. UI must bind those real handlers rather than a mock success path | No; the UI/lifecycle completion depends on the pending implementation |
| A07 successful source-purpose authorization, field profiles, lifecycle/IP/project denial and old/new-purpose isolation | Pure cursor binding, generic-key exclusion and the new negative reader cases are partial evidence. A complete test must use a source receiver established through its real authorized lifecycle | No; dependent on source control, not replaceable with a direct SQL credential fixture |
| A01/A04/A05/A08/A10 authenticated model-handler→source HTTP pages→independent consumer, pause/rotation/ack/retention and restored signed-cursor behavior | Actual model handlers, PG publication, retained data and offline public-page/SQLite consumer pass. They do not establish authenticated source pull or live receiver checkpoint recovery | No; successful source receiver and lifecycle are missing |
| Remaining A12 authenticated pull-to-consumer availability/lag and recovery under the declared load | Final comparable base/on/off sequential samples and DB/original-worker diagnostics are retained; approximately 1ms bounded source overhead accepted only for that microbenchmark. The on concurrent diagnostic still contains its separately deferred authorize503 | No for the unmeasured authenticated chain; source control/pull prerequisite. No new broad capacity pass is inferred |
| A11 Docker Build/Compose/customer preflight and applicable container installer execution | Docker and Podman are not on PATH; `customer-preflight-environment.log` says Docker Compose v2 is required. Real PG migrations, guarded down, finite bridge/runtime and non-Docker checks already pass | No until an authorized compatible local runtime is available; no system installation or mock-Docker pass |
| Exact Neutral N1 qualification required by the existing protocol CI contract | `UNIROUTE_INTEGRATION_CONTROL_N1_ROOT` is not configured to a verified artifact. Existing CI qualifies a specific release through authorized installation-token/artifact orchestration; local protocol/strict V1 vectors do not substitute for it | No until the exact validated local artifact root is supplied or its acquisition is authorized; no network/authentication workaround |
| General JSON Schema validation of the candidate schema and vectors | Typed Go validation and independent stdlib Python canonical/hash/page/SQLite checks pass. No jsonschema/check-jsonschema/ajv CLI or declared existing validator dependency is available in checked runtimes; those checks are not a general JSON Schema engine | No until an approved available validator/dependency path is provided; no new tool or dependency was installed |
| Final complete A candidate/commit/package and coordinator product acceptance | Candidate schema/manifest/vectors/specification already exist, but lifecycle/UI and the above required verification remain unfinished. The exact working-tree/evidence cutoff remains reviewable | Final content cannot be sealed as complete yet. An intermediate commit would not satisfy the missing acceptance criteria |

The two source-control target files remain absent:
`internal/admin/xinsight_source_control.go` and
`cmd/uniroute/xinsight_source_control_routes.go`. The unapplied candidate is
`/private/tmp/ur-workfacts-test.RDgTyF/source-control-proposed.patch`, SHA-256
`47b2550c713f15b723e3c1985f38fd7fe1d7fdc2a937e2c2d1bbba07bfde6ca9`.
Approval for this specific local implementation/synthetic verification remains
distinct from real credentials, services, provider calls or deployment.

Resume in `/private/tmp/unirouter-workfacts-20260909` with the original
`/private/tmp/ur-workfacts-test.RDgTyF` state/evidence. The PG16 recovery test is
`TestSourceFactsPGBackupRestorePreservesIdentityAndPendingJournalIntegration`
in `internal/gateway/source_facts_restore_integration_test.go`; it requires
`SOURCE_FACTS_PG_BIN=/opt/homebrew/opt/postgresql@16/bin` and the explicit
task-owned PG16 socket/port 65419 and Redis 16649 settings above. It creates and
restores a separate disposable test DB; it is not permission to restore over a
user database. Current DB-only restore evidence is
`source-marker-pg-restore-race.log`; receiver/cursor restore is still pending.

After explicit approval, apply/review the retained control proposal against the
then-current A tree, complete real lifecycle/UI and dependent tests, then run
the checks affected by those new changes and form the final A delivery. Do not
restart B/D implementation on design acceptance alone. No other unfinished,
independently executable necessary A step was identified in this matrix audit.
Remote CI, official signing/release, Target, real Provider/XI and combined E
remain outside current local authorization; none was represented as passed.

### Resumed A candidate — source control and remaining local runtime gate

This checkpoint supersedes the unfinished-dependency classifications in the
historical table above. It records Local implementation and verification, not
complete A acceptance. Candidate version: 1.3.0; base: `cd9f354b8a104a6f0677345a5694b19c000ab015`.
The final local commit/tree, changed-file hashes, exact logs, dependency identities
and protection readback are bound in
`/private/tmp/ur-workfacts-test.RDgTyF/source-control-delivery-evidence.json`.

- Source lifecycle and its management UI are implemented. Platform-admin route
  guards and live transaction reauthorization cover registration, activation,
  pause, rotation, cancellation, resume, profile changes and permanent revoke.
  Registration accepts only receiver verifier metadata, never a Bearer. Staged
  activation/cancellation binds the exact decimal credential ID; operation replay
  also checks the original registration and IP. Identical profiles preserve the
  current generation; changed profiles revoke keys and invalidate cursors.
- Source reads lock the connection consistently with control writes. A forced
  revocation/read race proves that the old reader cannot publish a page or ACK;
  stale authorization fails on retry. Real task-owned PG16 dump/restore preserves
  credential binding and retained signed cursors. Safety actions remain available
  with capture disabled. All credentials, actors and Provider responses are synthetic.
- Final `-tags=integration -race` source regressions pass in admin, sourcefacts,
  gateway and cmd/uniroute. Whole untagged Go race and vet, route guards, OpenAPI
  and frontend API parity pass; later lifecycle changes are covered by the final
  integration regression. No production identity or Provider was used.
- Actual model handlers, authenticated source HTTP pages and the independent
  SQLite consumer pass across 29 cases: 637 facts, 360 pages and 69 original I-01
  records. The consumer covers replay, restart/checkpoint failure and both I-01
  arrival orders. These are UR-owned Local fixtures, not XI integration acceptance.
- A separate same-run sample issues 32 sequential model requests with four
  original outbox workers. It delivers 224 facts and 32 I-01 records; the latest
  journal/publication database timestamp to independent consumer verification is
  1265.716/1261.655 ms. These timestamps are not commit-ACK timestamps. Prior
  accepted bounded performance limits and the two valid_deferred diagnostics
  remain unchanged; no general concurrent capacity or production SLO is claimed.
- General Draft 2020-12 validation passes: 8 valid vectors, 5 schema-negative
  vectors, 2 wire-negative vectors, 13 static payloads and all 637 authenticated
  payloads (plus the separate 224-fact sample). Two domain-only negatives remain
  checked by the required Go decoder tests. Development-only dependencies are
  pinned with official PyPI wheel hashes and installed only in the task directory;
  their exact versions, licenses and provenance are retained. Required CI now
  installs those pins and includes the admin source integration package; CI was
  not dispatched.
- Exact Neutral N1 `v0.1.0-candidate.1` qualification passes using existing local
  assets whose five original hashes were checked. The original D1 qualifier at
  `db10730d64a9302887ad59c53921e98c757927e7` and the complete sourceevidence race
  suite pass with the qualified root. This local Darwin run does not substitute
  for future Linux CI execution.
- Final frontend result: 89 files / 743 tests, exit 0, with the original warning
  budgets intact (findDOMNode 10/10). The earlier all-assertions-pass run exited 1
  at 11/10 and remains retained. Native DOM confirmation triggers remove the new
  warning without weakening disabled-action controls. Production frontend build
  and bundle budget pass: 91 assets, largest 362364 bytes, limit 460800 bytes.
- The saved checkout's 18 protected file hashes remain unchanged. Its separately
  recorded research directory remains present; its contents were never hashed in
  the protection baseline and are not claimed verified. Generated Python caches
  are excluded from the local candidate commit; user files were not cleaned.

The remaining A11 local dependency is an actual compatible Docker/Compose runtime.
Docker Desktop, OrbStack and the checked Docker/Colima/Podman/Lima/nerdctl binaries
and local runtime sockets are absent. Customer preflight reports that Compose v2
is required. Required container build, merged Compose and applicable installer
runtime checks have not passed; no system tool was installed or mock used. The
formal development path requires Docker 28/29 and supported Compose merge syntax.
Remote CI, Target, deployment and XI acceptance remain outside this Local evidence.
No push, PR, merge, release, real credential or service action was performed.
B/D implementation remains gated on complete A delivery and coordinator acceptance.
经验文件无需变化：本次来源控制与本地验证未产生符合模型适配经验准入门槛的新条目。

### Media HTTP evidence follow-up after independent acceptance review

The coordinator classified the missing legacy async-media HTTP-to-consumer chain
as validated_required and accepted section 2 of
`/private/tmp/ur-workfacts-test.RDgTyF/source-media-and-container-verification-proposal.md`
(SHA-256 `e17b38c7bcb2f4a75c7b8c8091baa0f9c18143281057b4cec278ba90eb849063`).
The earlier claim that only Docker remained was a self-review cutoff; this
independent finding supplements it. The aa119e12 candidate and its evidence are
retained, not rewritten or reclassified as a full acceptance.

The follow-up mechanically extracts main's two media authorizers; their bodies
and error mappings are unchanged apart from the passed pool reference. Eight real
media HTTP cases now cover legacy/explicit-grant success and replay, key/grant
revocation before dispatch, queued and in-flight cancellation, real Agent/source
credential rejection, original identity snapshots and non-I-01 charge facts.
Real PG, Redis, model authorization, limit policy, Worker and source control/page
handlers are used. Provider/object storage remain synthetic. The independent
consumer compares original job charge metadata and preserves zero additional
money; media's 80 facts / 48 pages contain zero text I-01 records.

The first complete bounded regression passes the eight media and 21 original text
HTTP cases plus original media writer/re-authorization tests. Their 455 facts /
267 pages / 33 original I-01 records pass independent consumer and general Schema
validation. Subsequent explicit dual-role initialization of the synthetic receiver
admin passes full cmd race unit tests and a focused rerun of all eight media plus
seven selected text cases. Unchanged frontend, N1 and unrelated product evidence
are reused; no CI, Provider/S3 or container pass is inferred.

Retained unsuccessful runs distinguish fixture setup errors, a sandbox socket
restriction and a redundant overlapping Redis RPM-limit regression from product
failures. No assertion or rate-limit budget was relaxed. The exact passing logs,
failed diagnostics, extraction proof, successor commit/tree and changed-file
hashes are bound in
`/private/tmp/ur-workfacts-test.RDgTyF/source-media-delivery-evidence.json`.

The carrier read fixture actually derived all current migration names; its old
fixed M115 report did not identify its real input. It now separately selects the
M115 bridge and M116 current ledgers, checks actual CLI inspect and unchanged
read-fixture state, and reports both exact heads/digests. Two offline selection
regressions pass; actual Docker execution remains unperformed. Its four read
tables are explicitly not a complete M116 migration/install proof.

The concrete runtime proposal is
`/private/tmp/ur-workfacts-test.RDgTyF/a11-runtime-approval-proposal.md`, with pinned
Lima/Ubuntu/Engine/Compose identities, official hashes/licenses, bounded local
paths/permissions/downloads and stop conditions. Host capacity is a timed sample,
not a fixed fact; current free space is below the planned 32 GiB VM plus host
reserve. No data was cleaned, runtime installed, image pulled or VM started.
User approval and the stated resource prerequisites remain necessary for A11.
Full A awaits the coordinator's independent acceptance; B/D remain design-only.
经验文件无需变化：该续作修补媒体验证链与合成夹具，不新增模型适配合同。
## Completed delivery — 1.2.77 compatibility and onboarding closeout

The authorized bounded delivery is complete through review, CI/exact-main,
Artifact, formal development rollout/inspect and representative GUI. Runtime is
1.2.77/M115/v3 at cd9f354b. The user-approved exact compiler-cache cleanup restored
the unchanged80%/8GiB gate; no image/data/backup/evidence cleanup occurred.
Approval236 pins existing Function Tools Probe259; refresh and saved-connection
continuation created no new Probe, ProviderAttempt, Release or Slot. All other
approval pins remain; safe client configuration was verified. No paid model calls,
new credentials, Production, old-writer retirement or X-Insight work. Preserve
historical evidence/budgets and start the next integration/full-test task separately.
[Exact Target/GUI evidence and next-task baseline](../ops/evidence/DELIVERY-ONBOARDING-177-20260909.md).

## Active delivery — compatibility and onboarding closeout

The user authorized bounded closure before the separately planned X-Insight integration and full test campaign. Implement environment preflight, precise latest/approved evidence UI, existing-connection continuation and CI frontloading. Current Target remains 1.2.76/M115/v3; no schema/model/price/Release/Slot changes are planned. Original budgets remain, additional shared 30/CNY5 pool unused. Complete required CI/exact-main, official development build/rollout and one GUI Approval using existing facts259–264 without a paid Probe.
[Exact scope and acceptance](../analysis/DELIVERY-ONBOARDING-CLOSEOUT-20260909.md).

## Current resumed delivery — concentrated GUI result / next optimization

2026-09-09: login restored; one paid Flash shared Probe produced six validated
facts, and GUI refresh made no new call. Existing independent Approval references
and Release/Slot remain unchanged on exact1.2.76/M115/v3. The bounded
Probe/results/refresh path is verified; new-Approval UI and the broader approved
onboarding experience are not declared complete.
[Evidence, cumulative budget and prioritized follow-up](../ops/evidence/CONCENTRATED-QUALIFICATION-GUI-20260909.md).
Next: frontload existing rollout-tool compatibility checks before the next
affected Artifact build; improve the current onboarding flow and latest versus
approved evidence presentation; only then adjust CI routing after dependency
proof. No new architecture, all-model retest or old-writer retirement.
The user's additional30 calls/CNY5 is recorded conservatively as one shared
pool, unused; retained original pools are Flash30/CNY4.95651 andMax18/CNY4.689972.

## Current resumed delivery — counter v3 Target closure / GUI pending

The authorized development/test counter correction is Target verified on exact
1.2.76/M115/6a89eef6,including same-image v3 selection and one real Flash
cancellation with one terminal settlement. Source/CI/Artifact/Target and marked
estimated usage remain separate; no Production, price change or history rewrite.
[Exact evidence and remaining scope](../ops/evidence/STREAM-COUNTER-V3-TARGET-20260908.md).
Remaining current acceptance: user login to the connected UniRouter console,
then one bounded shared Flash Probe/six facts and GUI result/refresh verification.
Flash29/30 andCNY4.92284/5 reserved; Max18/30 andCNY4.689972/5 reserved. Keep the
existing cumulative guards; no new model or repeated Agent task to fill this gap.

## Completed Repository And Target Execution — PRD8 Cost Optimization CO-0 / CO-1

**Work item:** `UR-PRD8-COST-OPTIMIZATION-V1`

**Owner:** UniRouter product/implementation owner

**Risk:** `R2` read-only aggregation, admin API and UI. No migration or request
runtime behavior is part of CO-0/CO-1.

**Authority:** On 2026-08-04 the Product Owner accepted the researched
cost-optimization decisions and instructed autonomous implementation until a
material decision gate. Exact contract:
`docs/architecture/PRD8-COST-OPTIMIZATION-V1-20260804.md`.

**Baseline:** `main` commit/tree
`c93c12b3e082a39f6a29e3c95e3029e640980682` /
`a826702cfd3ccfa060a217ac961fb62d12835730` with a clean worktree before this
batch.

**Current result:** CO-0 and CO-1 are merged and verified on the existing
development/test Target. Provider Prompt Cache is the only realized savings
driver. Four
tenant-scoped GET APIs, exact micro-CNY formulas, daily/cumulative trend,
evidence coverage, deterministic review candidates and a persistent Inspector
are implemented. Exact Response Cache and Compression are explicitly excluded
from current savings.

**Verification:** full package-serialized Go using only `uniroute_test` and
temporary migrated databases, `go vet`, 55/55 frontend files and 365/365
tests, type-check, production build/bundle budget, route/OpenAPI/frontend-doc
parity and 1440/1024/768 local synthetic browser interaction checks pass.
Evidence:
`docs/ops/evidence/PRD8-COST-OPTIMIZATION-CO0-CO1-LOCAL-20260804.md`.

**CI and Target verification:** PR #255 exact head/tree
`20cf0303072713b42c3015ddbcb7211f5e0ca06c` /
`3b9351b4a18ba032e9b9fa6772eeb71c7cecec2b` passed Backend, Frontend,
Docker, Change Routing and Protocol checks, then squash-merged as
`dde6eff214067a594f26b6aa080286f6cb4682d9` with the same tree. The
commit-addressed release is running as `1.1.3-prd8-costopt`; stable, canary and
frontend health, HTTPS/401 boundaries, pre/post data equality and authenticated
Cost Optimization UI at 1440/1024/767 pass. Evidence:
`docs/ops/evidence/PRD8-COST-OPTIMIZATION-CO1-TARGET-20260804.md`.

**Boundary:** no Provider COGS claim, Prompt/response storage, migration,
runtime cache/compression, route, settlement, Outbox, Feed, Cursor, Checkpoint,
credential, data-reset or Provider-traffic change. CO-2/CO-3 and CO-4/CO-5
require their separately defined gates; the completed development Target is
not Production or second-device POC evidence.

## Completed Repository Execution — PRD8 Enterprise Control Plane Delivery

**Work item:** `UR-PRD8-ENTERPRISE-CONTROL-PLANE`

**Owner:** UniRouter product/implementation owner

**Risk:** `R2` for UI, `R3` for directory/IAM/finance persistence and `R4` for
the bounded route-release and model-call authorization PEP. External directory,
ERP and Provider integrations remain separately gated.

**Authority:** On 2026-08-02 the User instructed the implementation owner to
list the complete long-running task and close every non-decision batch, stopping
only where a material product decision or discussion is required. The User
later separately authorized commit/PR/CI and release to the existing UniRouter
environment after verification. The frozen
product contract is `docs/PRD-Phase8.md`; the exact dependency plan and stop
conditions are `docs/planning/PRD8-IMPLEMENTATION-PLAN.md`.

**Baseline:** branch `codex/product-iteration-20260730`, commit/tree
`c0182861c7ec1079e335e317863f7ec5bcabd731` /
`0bfeed8938eb6235ec20db05450f2472e43460a3`. This work continues in the
existing product-iteration working tree and preserves all prior changes.

**Current result:** the complete autonomous dependency sequence is implemented
and Local verified in the working tree: `UI-1` through `UI-5`, `UI-PERF-1`,
`DOMAIN-0`, `ORG-1`, `ORG-2`, `FIN-1`, `FIN-2`, `FIN-3`, `RUNTIME-1` and
`AUTHZ-1`. This includes
source-neutral directory health/tree projection, people/account binding,
fail-closed IAM convergence, fiscal periods, cost centers, report-only budgets,
five-layer Rate Cards, immutable pricing evidence, exact allocation, close
gates and adjustment/reversal journals, plus Provider Pack-pinned bounded
route releases and a runtime-backed Model Call Authorization PEP. `ORG-3` and
`FIN-4` are now the exact
stop gates because no real customer directory or ERP/GL target has been
selected. Evidence:
`docs/ops/evidence/PRD8-ORG1-enterprise-directory-foundation-local-20260802.md`,
`docs/ops/evidence/PRD8-ORG2-enterprise-directory-iam-convergence-local-20260802.md`,
`docs/ops/evidence/PRD8-FIN1-FIN3-enterprise-finance-local-20260802.md`, and
`docs/ops/evidence/PRD8-enterprise-org-finance-local-closeout-20260802.md`, and
`docs/ops/evidence/PRD8-P7F04-P7F10-P7F12-runtime-controls-local-20260802.md`.

**Current local verification:** full Go tests, required PostgreSQL integration
selection, complete migration integration rollback suite, full race run and
`go vet` pass. Frontend 50/50 files and 347/347 tests, type-check and
production build pass. The current production entry is `275.35 kB` minified /
`87.11 kB` gzip; the largest minified chunk is
`376.63 kB` / `100.87 kB` gzip, and the build has no 500 kB warning. Rendered
budget, Rate Card and allocation/close smoke at 1440/1024/768 used synthetic
API data, had no document-width overflow, and produced zero browser console
errors or warnings. This remains Local, not Target or Production evidence.

**Compatibility boundary:** route release intentionally changes only bounded
channel admission/priority/weight assignment and Model Call Authorization can
only deny/narrow an otherwise-authorized model call. No completed batch changes
request DTO/storage, credential/endpoint configuration, billing settlement,
Outbox identity, completed-execution Source bytes/hash, Feed/Cursor behavior or
the no-default-source-instance rule.

**Delivery boundary:** commit/PR/CI and release to the existing UniRouter
environment are separately authorized, but cannot be claimed until executed
and verified. Real directory/ERP credentials, Provider test traffic,
Cursor/Checkpoint changes and ORG-3/FIN-4 remain unauthorized/unselected.

## Completed Repository Execution — PRD8 Product Boundary Freeze

**Work item:** `UR-PRD8-FOUR-PRODUCT-BOUNDARY-FREEZE`

**Owner:** UniRouter product/implementation owner

**Risk:** R2 documentation and product-semantics change. No code or runtime
migration is part of this batch.

**Authority:** On 2026-08-01 the User requested a fresh audit of the latest PRD,
comprehensive X-Insight/Tool Gateway/MCP Gateway research, removal of misplaced
UniRouter ownership and a sealed PRD8. The User explicitly allowed independent
agents to review the cooperation/X-Insight context.

**Baseline:** branch `codex/product-iteration-20260730`, commit
`8467715efa270ea884ca6dc28b70c3dd5ed09bff`, clean worktree before this batch.

**Current product sources:** `docs/PRD-Phase8.md` and
`docs/ops/PRD8-BOUNDARY-MATRIX.md`. PRD7 and all B0–B14 evidence remain frozen
implementation history.

**Result:** Product ownership is split among UniRouter model execution,
X-Insight evidence/investigation, Tool Gateway tool/action mediation, MCP
Gateway protocol/session mediation and the shared Event/Connector contract.
P7-F01–F19 have an explicit keep/narrow/merge/relocate disposition. The UI
boundary removes duplicated cross-product workspaces from future UniRouter
navigation and resolves `图像任务` to the model-domain `模型任务` concept.

**Verification:** repository/document searches, exact PRD7/governance/status
inspection, current navigation/source audit, read-only X-Insight and cooperation
boundary review, primary-source review of seven gateway products/projects, MCP
official architecture/security guidance, AgentDojo, ToolEmu and recent gateway
research. Final checks: `git diff --check` PASS; all local links in the ten
changed Markdown files resolve; P7-F01–F19 occur once each in exact order; no
stale “PRD7 is current” wording remains in current entrypoints; independent
read-only boundary/evidence review PASS. No application test was run because
this batch changes documentation only.

**Read-only external identities:** X-Insight `origin/main` commit/tree
`c2012ae74ccc15dab9149bf8c673cb177e5c1c44` /
`ccc60e517d269d1228a523d81e0783a6509e9e16`; the explicitly unmerged local
candidate reviewed only for product-boundary direction is commit/tree
`37abf091d2e87bca5574a125b258b2d382542226` /
`19cc52dfd109382ab7c4402314d2a769d46c8934`. Cooperation evidence digests:
Phase4A shared-SDK ADR candidate `525211d2d733f3bbf330e9ec69caa8c9fe3c88a5182b7f46faf7789e040a963d`;
LiteLLM second-Connector closure
`2a33338dd4651fcbaaf5487cfc0809642f8a05c880d8188fbcf510d455c36024`;
X-Insight/UniRouter product handoffs
`cc4ce112775efb246f244fe753ce3b229ec46c3e0d39217e265abd70ef9fa0f9` /
`7f6d9815eee4ceb139160c0a1ff889b61323d7aa81c76c0a6a64fbd60ea1b8b6`.

**Non-goals:** no route or code deletion; no API/DTO/Schema/DB/IAM/routing/
billing/Outbox/Feed/Cursor/Checkpoint change; no counterpart-repository write;
no Tool/MCP Gateway implementation; no deployment, credentials, traffic, push,
PR, merge or release.

## Completed Repository Execution — PRD7 Enterprise AI Execution Trust Control Plane

**Work item:** `UR-PRD7-ENTERPRISE-EXECUTION-TRUST-CONTROL-PLANE`

**Owner:** UniRouter product/implementation owner

**Risk:** R2/R3 repository product implementation. Runtime, external systems and
release actions remain separately gated.

**Authority:** The User accepted the researched UniRouter product-iteration
direction on 2026-07-30 and on 2026-07-31 instructed the implementation owner
to close all planned iterations using the optimal architecture, stopping only
for a material User decision.

**Accepted source:** `docs/PRD-Phase7.md`,
`docs/ops/PRD7-ACCEPTANCE-MATRIX.md` and
`docs/planning/PRD7-IMPLEMENTATION-PLAN.md`. The older
`PRD7-CANDIDATE-BACKLOG.md` remains historical planning lineage.

**Start baseline:** branch `codex/product-iteration-20260730`; Receipt V0
commit `e36196427b12dfd5a120aab12ba9114a43a14afb`, tree
`87901d41e87f5913b0d79a0cd57a423f7d51c2b8`.

**Execution order:** deterministic contracts and synthetic conformance first;
persistent inspector UI and operability next; external adapters and enforcement
only after their explicit decision gates. Every batch requires focused tests,
affected full suites, local evidence, one intentional local commit and a clean
worktree.

**Completed batches:** B0 froze scope/governance at `67e6d79`. B1 is Local
verified: Provider Conformance Pack V1 embeds strict OpenAI, Anthropic and
Azure OpenAI manifests/fixtures, a deterministic 3-pack/5-fixture/20-case offline
runner, native deployment-scoped Azure OpenAI Chat/Embeddings, platform-admin
catalog API and a persistent no-click Provider inspector. Provider claims bind
`(provider, adapter_type)`, so compatible protocol reuse cannot fabricate a
vendor pack result. Full Go tests/vet, 41-file/300-test frontend suite,
type-check/build and synthetic 1440/1024/390 browser evidence passed. Exact
evidence:
`docs/ops/evidence/PRD7-B1-P7F02-P7F03-provider-conformance-local-20260731.md`.

B2 is Local verified: Audit and Projects now have persistent no-click
inspectors, fail-closed URL-restorable scope/page/selection state, keyboard
selection and bounded browser-local metadata views. Full Go tests/vet,
42-file/308-test frontend suite, type-check/build and synthetic
1440/1024/390 browser checks passed. The browser pass found and closed a
Projects StrictMode table loop and a mobile flex-height defect. Exact evidence:
`docs/ops/evidence/PRD7-B2-P7F02-contextual-inspectors-local-20260731.md`.

B3 is Local verified: a default-off, Outbox-posted metadata-only OTLP/HTTP
exporter provides bounded asynchronous delivery, exact OpenInference mapping
and strict raw-content/cardinality negatives. Exact evidence:
`docs/ops/evidence/PRD7-B3-P7F05-otel-openinference-export-local-20260731.md`.

B4 is Local verified through the backend-neutral/in-memory-fixture boundary:
immutable secret versions, lifecycle CAS, active-generation stale-worker
rejection, fixed backend registry, fail-closed outage behavior and
metadata-only golden/audit evidence. Current credential/runtime paths remain
unchanged and P7-D01 still gates a production backend. Exact evidence:
`docs/ops/evidence/PRD7-B4-P7F06-secret-reference-local-20260731.md`.

B5 is Local verified through the standalone service and Redis-fixture
boundary: exact authority/policy/model/pricing/request identity, opaque HMAC
keys, versioned AES-256-GCM values, final-settled usage proof,
TTL/generation-purge, single-owner leases and strict tamper/outage/eligibility
negatives. A hit records zero current provider execution/cost but remains
explicitly unpriced for the customer. P7-D07 gates Gateway hit serving and
customer-ledger integration. Exact evidence:
`docs/ops/evidence/PRD7-B5-P7F07-exact-response-cache-local-20260731.md`.

B6 is Local verified through a separate zero-network endpoint-contract
boundary. Three fixed profiles cover stateless text Responses, Cohere Rerank
V2 and decision-gated GPT Image generation. Strict request/response semantics,
provider finality, exact units, nested duplicate rejection and explicit
unsupported-semantics failures pass a deterministic 3-profile/25-case runner.
No Gateway route, adapter registry, Provider Pack V1 artifact, customer
billing or media runtime changed. Exact evidence:
`docs/ops/evidence/PRD7-B6-P7F08-endpoint-protocol-conformance-local-20260731.md`.

B7 is Local verified through a zero-persistence offline/synthetic boundary.
The Provider COGS contract keeps supplier cost, UniRouter estimate and customer
revenue authority separate; exact request attribution requires one unique
upstream match and aggregate evidence never creates customer debt. Exact
decimal/page/cursor/finality, canonical replay, four fixed gaps,
duplicate/conflict and privacy boundaries pass two fixtures and a deterministic
15-case golden report. Billing-authorized no-store API/CLI surfaces and the
persistent auto-selected Financial Truth inspector add no provider network,
credential, database or customer-ledger write path. Exact evidence:
`docs/ops/evidence/PRD7-B7-P7F09-provider-cogs-financial-truth-local-20260731.md`.

B8 is Local verified through a zero-persistence synthetic boundary. Gateway
and the simulator now share one credential-free routing evaluator. The strict
offline contract compares immutable route, full policy and Provider Pack
snapshots; classifies Pack/SemVer/fixture/semantic changes; and proves
deterministic progression, replay, CAS conflict and rollback evidence. All
reports keep runtime mutation `none` and runtime promotion unauthorized.
Platform-only no-store API/CLI and the persistent auto-selected Governance
Inspector passed a 1-fixture/17-case golden runner, including the invariant
that blocked breaking changes cannot advance even the offline release chain,
plus full Go/frontend regression and 1440/390 synthetic browser verification.
Exact evidence:
`docs/ops/evidence/PRD7-B8-P7F04-P7F10-governance-simulation-local-20260731.md`.

B9 is Local verified through a zero-network synthetic boundary. The strict
IdP-neutral contract proves create/update/suspend/reactivate/terminal
deprovision, assignment and capability revocation, semantic replay and CAS.
Role/status mutations now version the principal and revoke live administrator
sessions atomically so old JWTs cannot revive after account restoration. A
1-fixture/17-case golden runner, no-store API/CLI and persistent Lifecycle
Inspector are included; no SCIM vendor or IdP source of truth is selected.
Exact evidence:
`docs/ops/evidence/PRD7-B9-P7F11-member-lifecycle-local-20260731.md`.

B10 is Local verified through a metadata-only synthetic boundary. The Agent
Trust Firewall reuses the current Agent capability attenuation model and
ancestor budget lineage, verifies exact TTL/revocation evidence and validates
an Ed25519 metadata receipt whose key version, tenant, audience, decision and
governance hashes are signed. The B8 Governance Digital Twin evaluator is
called directly and pinned by its immutable artifact identities. One fixture
passes 22 deterministic cases and the persistent Trust Inspector requires no
detail-open click. Fixture cryptographic validity remains explicitly
non-runtime trust; no signing private key, synchronous risk dependency,
enforcement or policy promotion was added. Exact evidence:
`docs/ops/evidence/PRD7-B10-P7F12-P7F13-P7F14-agent-trust-local-20260731.md`.

B11 is Local verified through an exact-Source, zero-network boundary. The
Passport calls the existing completedfeed Reference compatibility bridge,
preserves Source/Envelope/idempotency Golden identity, separates
direct/derived/declared/missing facts and renders four fixed Capture Gaps. The
shared-authority LiteLLM Connector is recorded as the heterogeneous
Go/TypeScript conformance proof without entering UniRouter's runtime registry
or credential/Checkpoint ownership. One fixture passes 23 deterministic cases;
the no-store API/CLI and persistent Inspector require no detail-open click.
Exact evidence:
`docs/ops/evidence/PRD7-B11-P7F15-P7F16-provenance-passport-local-20260731.md`.

B12 is Local verified through exact B8/B10/B11 composition. Its categorical
assertion is tenant/Audience/TTL/epoch scoped and downscope-only; the Source
scope binding is explicitly unverified rather than inferred. Advice/shadow
results and four evidence-derived controls stop at human review and cannot
promote runtime policy. One fixture passes 28 cases across 32 classes. Exact
evidence:
`docs/ops/evidence/PRD7-B12-P7F17-P7F18-risk-control-local-20260731.md`.

B13 is the immutable pre-P7-D03 record. Its metadata-only job-plan Schema,
text-to-image and image-to-video fixtures, nine threat classes and 55-case /
36-class report remain unchanged and continue to describe the then-unresolved
product decisions. They are not rewritten as runtime evidence. Exact evidence:
`docs/ops/evidence/PRD7-B13-P7F19-async-media-pre-gate-local-20260731.md`.

B14 is Local verified after the User resolved P7-D03 for repository-local
Image V1. It binds the frozen GPT Image Endpoint Profile to strict public/admin
metadata APIs, org/project/API-key idempotency, replay-before-capacity
admission, PostgreSQL queue/lease/fence/recovery/purge state, fail-closed
unknown provider outcomes, tenant-scoped immutable S3-compatible image and
receipt objects, seven-day read expiry and authenticated content proxying. A
charge-eligibility fact is written only after validated provider success plus
durable output; no wallet debit or media price is inferred. Video remains
disabled. Full Go/vet, media-job race tests, isolated PostgreSQL integration,
48-file/336-test frontend regression, type-check/build, route/OpenAPI/Compose
checks and 1440/1024/390 static-mock Chromium verification pass. Browser
verification found and closed 1024px Inspector clipping and 390px internal
horizontal overflow. No real provider/object-store call, target migration,
credential, deployment or traffic occurred. Exact evidence:
`docs/ops/evidence/PRD7-B14-P7F19-async-media-image-v1-runtime-local-20260731.md`.

**Current batch:** B14 closed. There is no further autonomous repository-local
batch in the accepted plan; the next step requires an explicit remaining
P7-D01/D02/D04/D05/D06/D07 decision or authority.

**Authority boundary:** no deployment, traffic, credentials, live provider or
IdP calls, non-test database mutation, cursor/checkpoint change, product-repo
outside UniRouter, push, PR or merge. Existing completed-execution Feed and
X-Insight compatibility behavior remain unchanged.

## Historical Repository Execution — Execution Decision Receipt Workspace V0

**Work item:** `UR-PRODUCT-ITERATION-RECEIPT-WORKSPACE-V0`

**Owner:** UniRouter product/implementation owner

**Risk:** R2 user-visible frontend behavior; no API, persistence, routing,
billing, IAM or runtime semantic change

**Authority:** User accepted closed-loop execution of the first product
iteration slice on 2026-07-31. This acceptance covers the UniRouter
Requests/Logs experience only; it does not accept the full PRD7 candidate
backlog or authorize runtime/external actions.

**Baseline:** `origin/main` and local start head
`48a88bccbe182a3d3a6aec315d422f47d879a364`, tree
`31b651fb1b2718a09efa9bd1c3a2abb244ebb44e`; branch
`codex/product-iteration-20260730`.

**Implemented behavior:** Requests/Logs now keeps an Execution Decision Receipt
inspector visible. The first result from the existing server order
`created_at DESC` is selected automatically; row click, `ArrowUp`/
`ArrowDown`, and `log_id` deep links change selection. Decision, Attempts,
Policy & Guardrail, Cost & Finality, and Provenance & Gaps views use the
existing sanitized request-trace DTO.

**Truth boundary:** only explicit billing terminal events can show financial
finality. HTTP success and `call_logs.cost_micros` do not imply settlement.
Missing actor, policy, route or gap evidence remains missing/undeclared rather
than becoming a negative claim. Zero declared gaps does not claim complete
cross-product capture.

**Verification:** Local verified. Focused Logs test: 1 file / 24 tests passed.
Frontend type-check passed. Full frontend suite: 41 files / 299 tests passed.
Production build and focused Go trace/admin tests passed. A local static-mock
browser pass at 1440, 1024 and 390 CSS pixels found and then verified the fix
for a Semi Table nested-update loop; the receipt remains visible/reachable at
all three widths. Final repository diff checks passed; this record closes with
the local commit that contains it and the resulting clean worktree. Evidence:
`docs/ops/evidence/UR-EXECUTION-DECISION-RECEIPT-V0-LOCAL-20260731.md`.

**Claim boundary:** no backend/API/DTO/schema behavior, database, request
record, routing, billing result, Outbox identity, feed endpoint, cursor,
credential, deployment, traffic, X-Insight, protocol or external integration
changed. Browser evidence used only synthetic in-memory responses; this is not
CI, real-data/runtime, Target or Production evidence.

**Next gate:** closed by the later explicit PRD7 scope above. No push, PR,
merge or deployment was part of this historical work item.

## Historical Repository Execution — Existing-ECS Replacement Lifecycle

**Work item:** `FV-SOURCE-QUALIFICATION-CURRENT-ECS-REPLACEMENT`

**Capability budget:** `FV-CURRENT-ECS-REPLACEMENT-BUDGET-01`

**Owner:** UniRouter execution owner, single repository writer

**Risk:** Red bounded provider-metadata lifecycle plus R2 planning/status truth;
no traffic, source-row read or artifact action

**Authority:** exact User
[authorization](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-21-prd2-fresh-controlled-existing-ecs-replacement-lifecycle-authorization.md>)
SHA-256 `d1b235b4065b7868e97da7f565e1b832b5978ea8d5f3c04f26c45a0994a89f83`;
`scope_acceptance=accept_exact`; `Cross-project binding state: Not binding`

**Stage 0 release:** the task-private non-product harness source is SHA-256
`67b864cc94d0122727f0f3ed96e6a659a8e402945fc31ccd2e5bba972362ca12`;
the ordinary binary is SHA-256
`f354fe03af42b3b5215ff2a73adfc7fb3a4b943e3919cbab6c6cfe7a7c3ae90c`.
`gofmt`, build, `go vet ./...`, bounded self-test, race build/self-test and the
zero-dispatch owner preflight passed. Debug/proxy negative preflights failed
closed before dispatch. The same bounded independent reviewer verified the
three prior release findings closed and returned `P0=0`, `P1=0`, `P2=0`.

**Terminal result:** `DECISION_REQUIRED_provider_transport_dns_failed`. The
safe phase is `transport_no_response` and `http_response_observed=no`. The
bounded harness classified the observed transport outcome; it did not retain
or expose a raw error and this result does not diagnose the underlying DNS
cause.

**Exact counts:** logical provider API invocations `1`; actual adapter
dispatches `1`; accepted pages `0`; decoded records `0`; SDK retries `0`;
redirect follow-ups `0`; invocation errors/failures `1/1`; candidates `0`;
host read-only session attempts/completions `0/0`; database metadata
transaction attempts/completions `0/0`; source rows/values `0`; receipts `0`;
traffic/artifact/import/X-Insight actions `0`.

**Budget terminal state:** lifecycle allocated/consumed/remaining `1/1/0`;
provider dispatch capacity allocated/consumed/unused/remaining `2/1/1/0`.
Unused capacity expired at the first terminal result and is neither retry nor
carryover authority.

**Predicate boundary:** all candidate identity/running/custody/classification,
lineage, runtime, schema and control predicates remain `not_executed`; there
was no candidate and Stage 2 did not start.
`candidate_local_strict_primitive_status=not_executed_read_only_gate`. No ECS,
source, data, credential or architecture absence/failure claim follows.

**Next gate:** publish the exact owner
[response](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-21-unirouter-prd2-fresh-controlled-existing-ecs-replacement-lifecycle-response.md>)
after this docs-only PR/required-CI/merge evidence exists, then return to the
Integration Coordinator. Any diagnostic/retry requires new User authority.
`FV-CONTROLLED-TRAFFIC`, source rows, artifact/import, B3 and Binding remain
blocked.

**Claim boundary:** no product-code change, ECS/IAM/network/credential/host/
database/source mutation, source-row access, MaaS traffic, manifest/artifact,
transfer/import, X-Insight mutation, Target, Production, live feed, B3 or
deletion occurred.

## Historical Repository Execution — Existing-ECS Qualification Continuation

**Work item:** `FV-SOURCE-QUALIFICATION-CURRENT-ECS-CONTINUATION`

**Owner:** UniRouter execution owner, single repository writer

**Risk:** R2 planning/status truth with a bounded R4 provider-metadata action;
no traffic, source-row read or artifact action

**Authority:** exact User
[decision](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-21-prd2-fresh-controlled-existing-ecs-and-final-outcome-authorization.md>)
SHA-256 `d0464491727493939ad788698171f16474c6298db851502e04db77758315043f`,
under parent SHA-256
`6d8e32f33c59bb10799c629b0d3c929b6ff4980f520c074252d48f70c3628eb3`;
Authorization ID `UR-XI-FRESH-VALUE-AUTH-20260721-02`; `Cross-project binding
state: Not binding`

**Owner acceptance:** `scope_acceptance=accept_exact`. The continuation
corrected two self-imposed pre-call requirements from the prior response: it
does not require a Registry platform or proof of every lower-level network
mechanism. It uses the decision's bounded SDK invocation/dispatch semantics and
would create only one minimum private Receipt v0 after all candidate predicates.

**Qualification result:** `DECISION_REQUIRED_provider_invocation_failed`.
Build, vet, bounded synthetic tests, local preflight and the single independent
release review passed (`P0=0`, `P1=0`, `P2=2`). The one authorized provider API
invocation entered the adapter transport exactly once and returned an invocation
error before any response page was accepted. The decision terminates on that
error, so no retry or second nominal call occurred. The narrowest retained
classification is `unknown_invocation_error_class`; whether an HTTP response
was received is `unknown_not_persisted`. Zero accepted pages does not establish
a pre-response failure.

**Exact counts:** provider API invocations `1`; adapter-level provider
dispatches `1`; accepted pages `0`; decoded records `0`; SDK retries `0`;
invocation errors `1`; candidates `0`; SSH sessions `0`; database metadata
transaction attempts/completions `0/0`; source reads `0`; receipts `0`;
traffic/artifact/import/X-Insight actions `0`.

**Predicate boundary:** exact candidate identity, running state, custody,
non-customer/non-Production classification, lineage, runtime, schema and
control metadata were not executed because no provider page was accepted.
`candidate_local_strict_primitive_status=not_executed_read_only_gate`. No asset,
source, data or architecture absence claim follows.

**Next gate:** publish the exact owner
[response](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-21-unirouter-prd2-fresh-controlled-existing-ecs-qualification-continuation-response.md>)
after this docs-only PR/required-CI/merge evidence exists, then return to the
Integration Coordinator. `FV-CONTROLLED-TRAFFIC` and Binding remain blocked.

**Claim boundary:** no product-code change, ECS mutation, host/database/source
access, MaaS traffic, manifest/artifact, transfer/import, X-Insight mutation,
Target, Production, live feed, B3 or deletion occurred.

## Historical Repository Execution — Initial Current-ECS Qualification

**Work item:** `FV-SOURCE-QUALIFICATION-CURRENT-ECS`

**Owner:** UniRouter execution owner, single repository writer

**Authority:** exact User
[decision](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-21-prd2-fresh-controlled-existing-ecs-and-final-outcome-authorization.md>)
SHA-256 `d0464491727493939ad788698171f16474c6298db851502e04db77758315043f`;
Authorization ID `UR-XI-FRESH-VALUE-AUTH-20260721-02`; `Cross-project binding
state: Not binding`

**Owner acceptance:** `scope_acceptance=accept_exact`. This is a bounded
qualification attempt, not traffic, artifact, import or source acceptance.

**Terminal result:**
`DECISION_REQUIRED_pre_call_owner_controls_unproved`. The owner-authored local
non-network context preflight passed its safe configuration checks, but the
owner accepted independent static review that the available execution boundary
could not guarantee exact provider HTTP-attempt/redirect accounting and lacked
a validated durable owner-private Receipt v0 registry needed to complete the
authorized lifecycle. The temporary harness was therefore not released for
execution. Candidate-stage runtime/control-chain/destination predicates were
never executed and are not classified as failed pre-call predicates.

**Exact counts:** provider/API invocations `0`; pages `0`; records `0`; retries
`0`; invocation errors `0`; candidates `0`; SSH sessions `0`; database
transactions `0`; source reads `0`; receipts `0`. Candidate predicates remain
`not_executed`; `candidate_local_strict_primitive_status` is
`not_executed_read_only_gate`. No ECS/source/data absence claim follows.

**Finding gate:** `P0=0`, `P1=1`, `P2=0`. The single consolidated P1 is the
unproved owner execution-boundary capability above, not a UniRouter
product-code defect or a candidate-predicate result.

**Next gate:** publish the exact owner
[response](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-21-unirouter-prd2-fresh-controlled-existing-ecs-qualification-response.md>)
after the docs-only PR/CI/merge evidence exists. A later owner lifecycle must
run with the bounded instrumented-client/private-registry execution boundary,
or separately activate the task-owned Local/Linux/bootstrap route.
`FV-CONTROLLED-TRAFFIC` remains blocked.

**Claim boundary:** no cloud call, enumeration, retry, host/database/source
access, product-code change, MaaS traffic, manifest/artifact, transfer/import,
X-Insight mutation, Target, Production, live feed or deletion occurred.

## Historical Repository Execution — Fresh-Controlled Source Qualification

**Work item:** `FV-SOURCE-QUALIFICATION`

**Owner:** UniRouter execution owner, single repository writer

**Risk:** R2 current planning/status truth with an R4-gated future source and
traffic lifecycle; this work item performs no traffic, source-row read or
artifact action

**Authority:** exact User
[decision](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-21-prd2-fresh-controlled-product-value-proof-authorization.md>)
SHA-256 `6d8e32f33c59bb10799c629b0d3c929b6ff4980f520c074252d48f70c3628eb3`;
Authorization ID `UR-XI-FRESH-VALUE-AUTH-20260721-01`; `Cross-project binding
state: Not binding`

**Publication:** the immutable owner
[response](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-21-unirouter-prd2-fresh-controlled-value-proof-acceptance-readiness-response.md>)
is published at SHA-256 `d118f0c4...`; its final docs chain is merged on
`b3ecb4fad351e4ef04fb5033faaca4ee25771f27`.

**Qualification result:** `DECISION_REQUIRED`. The prior owner result still
provides no exact current environment identity, classification or custody.
This machine has local PostgreSQL tooling but no existing Docker, Podman,
Colima or Linux-VM runtime. The strict producer intentionally supports only
`linux/ext4/o_tmpfile/procfd-linkat`; Darwin cannot be declared equivalent.
Creating a macOS-only PostgreSQL source would therefore produce a candidate
that cannot complete the authorized strict-artifact lifecycle. No candidate
was bound and no Source Trust Receipt v0 was created.

**Counts:** external cloud/control-plane calls `0`; broad searches, retries and
pagination `0`; environment/database mutations `0`; source rows/values,
request IDs, raw IP and raw errors read `0`; MaaS requests `0`; manifests,
artifacts, transfers and imports `0`.

**Finding gate:** `P0=0`, `P1=1`, `P2=0`. The P1 is that no exact current source
identity/custody plus supported Linux producer path is proved and bound, not a
producer code defect or a source-absence finding. A migration-only local
database would also lack required runtime/admin secrets, test License, API key,
model price, model access, provider binding/route and positive wallet grant.
The smallest complete User choice is therefore either (a) two explicitly
separated authorities—first establish one task-owned local Linux/current-schema
source with task-specific runtime/admin secrets, then bootstrap the bounded
non-customer controls and positive grant using an encrypted one-source binding
to an existing owner-controlled development credential—or (b) designate one
already configured exact development source for owner qualification. The
external credential may not be created, changed, rotated, disclosed or
onboarded; its binding into the new source is itself an authorized private
configuration write. Retention and cleanup remain separately gated. No
environment or control-plane mutation is performed here.

**Next gate:** superseded by Authorization `UR-XI-FRESH-VALUE-AUTH-20260721-02`
and the current-ECS qualification record above. `FV-CONTROLLED-TRAFFIC` remains
blocked.

**Claim boundary:** no old-ECS diagnosis, cloud resource action, credential,
IAM, network or database-grant change, product-code change, X-Insight mutation,
Target, Production, live feed, Core Intake, second source or deletion occurred.

## Historical Completed Repository Execution — IR-IP Policy Adaptation

**Work item:** `IR-IP-POLICY-ADAPTATION`

**Owner:** UniRouter execution owner, single repository writer

**Risk:** R3 strict privacy boundary and cross-project producer/receiver
compatibility; source and artifact operations remain separately gated

**Authority:** exact
[request](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-20-x-insight-prd2-internal-real-raw-ip-policy-adaptation-b2-retry-request.md>)
SHA-256 `7517496d...`; `Decision owner: N/A`; `Cross-project binding state: Not
binding`

**Repository behavior:** designated `public.call_logs.ip_address` is evaluated
only as a presence boolean. The producer accepts only `false/false` and
`true/true` for `ip_address_was_present` / `ip_address_redacted`, rejects both
mismatches, and reconciles bounded positive counts per request. The frozen
declaration is
`raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained`.
Raw error and every other blocked family remain fail-closed; the Linux
anonymous-inode / atomic no-replace publication contract is unchanged.

**Verification level:** completed by PR #225, required CI run `29711162577`
attempt 2 and merged-main commit
`d43fef6eed92323da0880f0f6e0e8d35e388e8e0`; the immutable owner response and
X-Insight compatibility review are published. This remains producer/receiver
compatibility evidence, not source qualification or artifact authorization.

**Next gate:** completed. The identity-ambiguous historical ECS B2a is deferred
by the separate fresh-controlled decision; the independent UR-E lifecycle is
not rewritten by that decision. The current new-lifecycle gate is the source-
qualification result above.

**Claim boundary:** no source/cloud/private-value read, B2a/B2b execution,
manifest/artifact generation or transfer, X-Insight write/import, Target,
Production, live-feed or binding action is part of this repository execution.

## Active Cross-Project Execution — UR-E Bounded Synthetic Sidecar

**Batch:** `V1-XI-LEDGER-001 / UR-E`

**Owner:** Codex, sequential single execution owner

**Risk:** R3 controlled cross-project evidence transfer and exact monetary
schema compatibility; Stage B Target write remains R4-gated

**Status:** UniRouter Stage A generation/transfer complete; shared handoff is
`producer_complete_receiver_pending`; X-Insight Stage A.5 read-only preflight
is next

**Binding authority:** accepted decision SHA-256 `c674b04f...`, activated
`2026-07-19 12:15:26 CST`; corrected response SHA-256 `12857bd7...`.

**Producer authority:** clean generation head `9a94d549...`; actual producer
merge `5176654d...`, PR #219 head `be2d5f5...`, required CI `29640791281` and
producer source SHA-256 `a2f2fc72...`. The complete generation-head delta from
producer authority is seven `docs/**` paths only.

**Stage A result:** exact request
`req-ur-e-v2-synthetic-20260719-001`, source instance
`unirouter-ur-e-20260719-001`, source time after activation,
`cost_micros="314159"`, one call log, zero trace events and one approved
missing-trace gap. The new artifact is 3985 bytes, mode `0600`, SHA-256
`412c9b641cb6b6451a8b66f3ff808ef29750a7f3bedafdef93a473b66f641e7e`;
it embeds the exact 412-byte manifest SHA-256 `b1c096df...` and differs from
both historical artifacts. Safety/contract checks found no raw prompt,
response, API key, IP, upstream error, credential or `cost_cents`.

**Transfer:** corrected `ur-e-exclusive-copy-v1` created the manifest and then
artifact receiver once each at their exact non-Target paths. Both receivers
are separate mode-`0600` inodes with sender-equal length/SHA and bytewise
`cmp`. There was no retry, overwrite, substitution or Target access.

**Next gate:** the writer baton is released. X-Insight may append only its
Stage A.5 receiver/preflight half after independently recomputing receiver
hashes and completing read-only Target/release/database/path and source-
qualified pre-existence checks. Target placement/import is prohibited until
`stage_b_gate_passed`; any mismatch records `stopped` and the artifact cannot
be regenerated or substituted under this decision.

**Evidence:**
`docs/ops/evidence/V1-XI-LEDGER-V2-SIDECAR-UR-E-STAGE-A-20260719.md` and
[shared handoff](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-handoff.md>).

**Claim boundary:** Stage A owner-verified only. No X-Insight acceptance,
Target write/import, External integration verified, customer data, Production
or live-feed claim. PRD6 remains closed.

## Historical Completed UniRouter-Owned Closeout — V1 X-Insight Ledger v2 Sidecar

**Batch:** `V1-XI-LEDGER-001`

**Owner:** Codex

**Risk:** R3 cross-repository monetary schema, persistence and evidence
compatibility

**Historical status at 2026-07-18:** UniRouter implementation and required CI
were verified; X-Insight-owned consumer fix/publication/acceptance were then
pending. The newer active UR-E record above supersedes only that current-status
classification, not this evidence.

**Repository authority:** PR #219 exact head
`be2d5f5d1cd6d322728b3d7c9aa8a5a347650563` passed required CI run
`29640791281` and squash-merged as code-bearing commit
`5176654d7d320b56c1c136f39b3debb8156ece4b`. The development-target runtime
authority remains PR #217 / `cdbaa972db9b318b4412dd2aebce118c934debac`;
PR #219 was not deployed.

**Implemented boundary:** new exchange registrations default to
`unirouter.prd5.xinsight.snapshot.v2`; the operator producer's exact
micro-CNY contract is covered by an explicit query/mapper script test that
passed required CI and fails closed on invalid negative money;
the receipt regression proves the unchanged
`xinsight.import-receipt.v1` envelope accepts a v2 artifact; and the technical
reference no longer describes the build-ignored operator script as a deployed
runtime component.

**Local integration result:** the actual UniRouter producer generated one
synthetic metadata-only v2 artifact with `cost_micros="7"`,
`currency=CNY`, `amount_scale=1000000` and
`money_schema=micro-cny.v1`. Artifact SHA-256:
`93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1`.
At observation time the exact bytes existed as an untracked X-Insight
worktree fixture; this task makes no X-Insight publication claim. The actual
X-Insight CLI persisted 2 normalized events including 1 explicit capture gap into an
isolated local PostgreSQL database; a second identical CLI run returned
2 `duplicate_noop`, 0 inserts and 0 conflicts. SQL, search API, event-detail
API and the real loopback UI all retained the exact string `"7"` and displayed
`CNY 0.000007 (7 micro-CNY)`. No `cost_cents` or raw source excerpt was
returned for the v2 usage event.

**Verification boundary:** UniRouter's producer side is Local verified. The
cross-repository run is a synthetic compatibility probe against the then-current
uncommitted X-Insight worktree using task-owned isolated databases. After the
ownership correction, this closeout made no further X-Insight worktree writes
and did not stage, commit, push, reset or clean that repository. It is not
X-Insight acceptance, CI verification or a closed
External integration. Independent review found that a later v1 replay
can downgrade X-Insight's source registry after v2, which is X-Insight-owned
and remains open. This is not Target, Production or customer-data evidence.
The current UniRouter target proves the micro-CNY runtime, not execution of the
operator producer there. Completed-execution live feed, endpoint,
cursor/replay, service token, continuous polling and p95 freshness remain
proposed/unimplemented and were not changed.

**CI and cleanup:** CI run `29640791281` passed Change Routing, Backend Tests,
Frontend Build and Docker Build, including the new direct producer contract
step. Five exact mode-`0600` temporary JSON artifacts/receipts and the three
task-owned isolated databases `unirouter_xi_v2_019f747b`,
`xinsight_xi_v2_019f747b` and `xinsight_xi_v2_cli_019f747b` were removed one at
a time and verified absent. No temporary role or credential was created; no
customer data or X-Insight worktree file was deleted during cleanup.

**Evidence:**
`docs/ops/evidence/V1-XI-LEDGER-V2-SIDECAR-LOCAL-20260718.md`

## Completed Closeout — V1 Seven-Role Target Acceptance

**Batch:** `V1-UX-IA-001` plus the target-verifiable authentication, session,
local-login, RBAC, API and release slices of `V1-SECURITY-REMEDIATION-001`

**Owner:** Codex

**Risk:** R4 target release and authenticated role acceptance

**Status:** Required CI verified; Target verified / closed for the enumerated
seven-role UI, API, session, credential, database, release and cleanup surfaces

**Repository/release authority:** PR #217 exact head
`afb5269a15619858f7eb6c477a35d59ca5753151` passed CI run `29590689179` and
squash-merged as code-bearing commit
`cdbaa972db9b318b4412dd2aebce118c934debac`. The development/test ECS runs
version `1.0.1`, signed sequence `5`, schema
`000042_v1_local_login_identity`.

**Target result:** final run `20260718075833-e8d4b221` passed all seven roles,
52/52 enumerated allowed route visits, the representative positive/negative
API matrix, developer key create/model-list/disable/replay, all seven logout
revocations, desktop/mobile shell checks and browser-runtime assertions. Final
database readback found 42/42 fixture sessions revoked, zero live sessions, the
temporary key disabled, all seven temporary users disabled after cleanup, the
release identity unchanged and all eight expected service probes healthy.

**Evidence:**
`docs/ops/evidence/V1-SEVEN-ROLE-UI-API-DB-TARGET-CLOSEOUT-20260718.md`

**Non-goals:** Production/customer-equivalent evidence, cross-organization
target isolation, supplier-bill reconciliation, paid provider generation,
enterprise IdP/detector, X-Insight consumer acceptance, PRD7 and general
automatic/N-1 upgrade behavior remain unclaimed. PRD6 remains `Completed
(Accepted Scope)` and is not reopened.

## Completed Amendment — V1 Security Remediation

**Batch:** `V1-SECURITY-REMEDIATION-001`

**Scan:** `124093ab-e945-47e9-8fa3-2eb688dc31dc`

**Owner:** Codex

**Risk:** R3 for authentication, schema, distributed capacity and financial
fencing; the separately authorized R4 target cutover is recorded above.

**Status:** CI verified / closed for all 39 canonical findings at
`cdbaa972db9b318b4412dd2aebce118c934debac`; only the explicitly exercised
target slices recorded above are Target verified

**Baseline:** PR #217 base
`92ba78878782114a82411697597f5e58e10602a8`; merged code authority
`cdbaa972db9b318b4412dd2aebce118c934debac`

**Publication:** PR #217 / CI run `29590689179` / squash merge
`cdbaa972db9b318b4412dd2aebce118c934debac`

**User decision:** implement `A1 + B1 + C1 + D1`; preserve PRD6 as
`Completed (Accepted Scope)` and treat this as an independent Post-PRD6
amendment.

**Implemented boundary:** all 39 canonical scan findings are
`fixed_verified` in the scan-local ledger. The final approved package adds:

- A1: versioned administrator JWTs with random 256-bit session identity,
  server-side hashed revocation, live principal/epoch enforcement, atomic
  logout and password invalidation, idempotent retry, sibling-session
  preservation and non-cacheable login/token responses; migration `000038`;
- B1: platform plus copy-on-write tenant Guardrail snapshots, atomic active
  policy/cache/tenant quotas, tenant-fair bounded advisory scheduling, and
  durable metadata-only aggregate gap receipts; migration `000039`;
- C1.1: workflow-slot Agent refresh/supersession authority with an atomic Lua
  parent check and a token-TTL-bounded legacy read bridge;
- C1.2: immutable reservation/receipt/settlement/recovery binding to a unique
  delivery attempt, with uniquely evidenced historical backfill and preserved
  quarantine for ambiguous history; migration `000040`;
- C1.3: full `{org, request, attempt, owner, generation}` stream fencing,
  wall-clock expiry checks, final checkpoint authority recheck and durable
  settlement recovery claims; migration `000041`;
- D1: fail-closed new workload admission whenever Redis cannot prove shared
  capacity, while existing tracked work remains releasable; admission
  fingerprint v2 prevents mixed safety contracts;
- accepted release-adjacent correction: migration `000042` makes local login
  IDs deployment-global, case-insensitive and outer-Unicode-whitespace
  normalized, rejects ambiguous legacy data atomically, excludes OIDC
  identities and makes duplicate lookup fail closed. Provider-scoped indexes
  allow same-organization local/OIDC display overlap without shadowing local
  login; concurrent OIDC first login converges by issuer/subject and guarded
  down refuses activated cross-provider overlap;
- accepted release-adjacent correction: management CLI passwords no longer
  accept value-bearing argv. Interactive use is a twice-confirmed hidden TTY
  prompt; automation uses bounded, single-line `--password-stdin` with no
  password environment fallback. API, CLI and UI share 12 Unicode code points
  minimum, 72 UTF-8 bytes maximum and invalid-UTF-8 rejection.

**Local verification:**

- `GOCACHE=/private/tmp/unirouter-go-build-final go test -race
  -coverprofile=/private/tmp/unirouter-v1-security-final-postreview-20260717.out
  ./...`
  — pass for every package;
- `REQUIRE_TEST_DATABASE=1 GOCACHE=/private/tmp/unirouter-go-build-final go test
  -tags=integration -race ./...` — pass for
  every package against the repository integration services and migrations
  through `000042`;
- `GOCACHE=/private/tmp/unirouter-go-build-final go vet ./...`,
  `git diff --check` and `go mod verify` — pass;
- frontend `npm run type-check`, `npm test -- --run` (41 files / 277 tests), and
  `npm run build` — pass; existing framework warnings and the Vite large-chunk
  advisory remain non-failing and are not claimed as absent;
- workflow/Compose YAML, changed shell syntax, backup security regression,
  13 SSO origin-guard tests and the deployment-helper Python syntax check —
  pass;
- the full gates caught and corrected two stale cross-suite contracts: the
  limit-policy last-known-safe fixture could not override D1 workload
  fail-closed semantics, and the `000036` immediate-down fixture now stops its
  setup at `000036` instead of applying later activation evidence first. Final
  review additionally caught and fixed legacy local/OIDC same-name blocking,
  concurrent OIDC provisioning, frontend `409` mislabeling and Unicode-byte
  password drift in both user creation and self-service password change.

**Verification boundary at this local checkpoint:** local Docker/Compose is
unavailable, so the actual Docker/context gate must run in required GitHub CI.
The local evidence does not by itself claim a commit, push, pull request,
GitHub CI run, ECS migration/deployment, target-browser verification, or
Production verification. Immutable local evidence:
`docs/ops/evidence/V1-SECURITY-REMEDIATION-LOCAL-20260717.md`.

**Final local rebaseline:** safe temporary-cache cleanup restored enough local
space and every previously interrupted final-tree gate was rerun successfully.
The first focused sandboxed run could not bind an `httptest` loopback port;
the identical local-permission rerun passed, so the sandbox denial is recorded
as an environment restriction rather than a code result. Intentional
diff/sensitive-file review is complete with no repository-content blocker.
At that checkpoint the remaining gates were commit/PR and required GitHub CI;
those gates and the later authorized target cutover are now closed by the
current record above. Docker/context ran in CI because no local Docker CLI was
installed. The adjacent fixes are not new canonical scan findings and do not
reopen PRD6.

## Underlying Worktree Batch — V1 User-Task IA And Role Correctness

**Batch:** `V1-UX-IA-001`

**Owner:** Codex

**Risk:** R3 because the user-visible information architecture is coupled to
tenant selection, role landing, notification scope and financial control
surfaces; PRD6 remains closed and is not reopened.

**Status:** CI verified and Target verified / closed for the exact seven-role
UI/API acceptance matrix recorded above; this is not an assertion that every
backend operation or future feature was exercised

**Baseline:** PR #217 base `92ba78878782114a82411697597f5e58e10602a8`;
merged code authority `cdbaa972db9b318b4412dd2aebce118c934debac`

**Publication:** PR #217 / CI run `29590689179` / squash merge
`cdbaa972db9b318b4412dd2aebce118c934debac`

**User decision:** execute the user-task information architecture when it is
the best practical first-release design; keep at most two sidebar levels; make
every role land on a usable dashboard/workbench with discoverable working
entry points; verify that visible functions work rather than merely render.

**Accepted direction:**

- organize navigation by user task rather than implementation phase;
- preserve independent owner pages for independent objects and allow only
  read-only summaries with context-preserving drill-downs;
- merge funds overview/history/grants under one funds-account workspace and
  base price/organization multiplier under one model-billing workspace while
  preserving old deep links;
- keep the existing X-Insight control surface bounded; no new feed, exporter,
  credential, external call or integration contract is authorized;
- correct platform-tenant selection, project-role scope, role landing,
  misleading financial/monitoring semantics and dead or false navigation;
- use business language in primary views and move implementation details to
  advanced diagnostics.

**Non-goals:** PRD7 acceptance, actual provider-bill reconciliation, new SSO
provider integration, new model-discovery automation, Production deployment,
new X-Insight runtime behavior, destructive data operations or paid provider
calls.

**Historical target-planning boundary:** this batch required local/API checks
and required CI before any target mutation, followed by a merged signed release
package and a new sequence. PR #217, signed sequence `5` and the target
closeout at the top of this record now satisfy those preconditions; this
paragraph preserves the pre-cutover control order rather than a pending gate.

## Previous Boundary — X-Insight Cooperation Only

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
