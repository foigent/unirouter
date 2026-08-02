# PRD8 Target Deployment And UI Verification

> Work item: `UR-PRD8-ENTERPRISE-CONTROL-PLANE-TARGET`
> Execution window: 2026-08-02 (Asia/Shanghai)
> Environment: existing Alibaba Cloud UniRouter development/test ECS
> Status: Target verified for the release and authenticated UI surfaces listed below

## Authorization And Boundary

The Product Owner authorized the complete PRD8 UI and function iteration,
required CI, and release to the existing UniRouter environment so the revised
product could be inspected with an existing login. The release covers
UI-1..5, UI-PERF-1, DOMAIN-0, ORG-1..2, FIN-1..3 and the bounded
P7-F04/P7-F10/P7-F12 runtime controls.

This authorization did not select a customer directory for ORG-3 or an ERP/GL
for FIN-4. It did not authorize X-Insight changes, real Provider traffic,
credential rotation, new Feed traffic, or Cursor/Checkpoint mutation.
P7-F17/P7-F18 remain bounded to offline/read-only evidence and an explicit
human-review stop; they were not promoted to automatic enforcement.

## Repository, CI And Merge

- PR: [#247](https://github.com/foigent/unirouter/pull/247).
- Exact CI-passed PR head:
  `aab74a669c2c8bff904a60c0ba859637ee746f05`.
- Exact tree:
  `f2f27618161d39888b7bb8a322c859e4a6deabe6`.
- Required run `30720441644`: Change Routing, Frontend Build, Backend Tests and
  Docker Build all passed.
- Protocol Conformance run `30720441664`: passed.
- Squash merge:
  `12efe9e3ba4dd753f6019468ca3b26d01a154c31`.
- The merge tree is byte-identical to the CI-passed PR-head tree. The workflow
  has no automatic post-merge application CI; release therefore remained
  pinned to the exact already-tested tree rather than claiming a nonexistent
  main run.
- The final CI correction changed one integration assertion from `active` to
  `staged`, matching the new Provider activation gate. The exact focused race
  test and the complete admin integration command passed locally before the
  PR rerun.

## Backup And Release Controls

- Pre-migration PostgreSQL custom-format backup:
  `/opt/unirouter/backups/pre-prd8-12efe9e3-20260801T225217Z.dump`.
- Backup size: 401,536 bytes.
- Backup SHA-256:
  `0fd38e62150bf49a42e55b61a53b0e1cdbada49261610c0bebf4a63132fd9b44`.
- `pg_restore -l` passed before migration.
- Commit-addressed release directory:
  `/opt/unirouter/releases/12efe9e3ba4dd753f6019468ca3b26d01a154c31`.
- Incremental Git bundle SHA-256:
  `e4be323b5e8f8090ab9c4ad0cfeae1d6de73b7244aad6d14e3cf552565c77b86`.
  Bundle verification passed before checkout.
- Release checkout readback: exact commit `12efe9e3...`, exact tree
  `f2f2761...`, zero tracked/untracked changes.
- The prior stable, canary and frontend images remain retained under explicit
  `unirouter-rollback/*:d86b28ef` tags. No image prune or bulk cleanup ran.
- A failed archive-transfer attempt left one unused partial archive beside the
  release directories. It was never extracted or included in a build context;
  it remains retained because this release did not authorize cleanup deletion.
- Existing `.env` and `.env.production` were copied with mode `0600`.
  Credential values were neither changed nor recorded. Only the non-secret
  runtime version, commit and development-environment labels were advanced.
- The pre-existing dirty `/opt/unirouter` checkout was not read as release
  authority and was not modified.

## Migration And Data Preservation

Before the non-rolling migration, readback returned zero active billing
reservations, zero unresolved Provider attempts and zero pending runtime
terminal hooks. Migrations `000046` through `000053` then applied exactly once:

1. async media Image V1 persistence;
2. enterprise directory foundation and people/account/IAM convergence;
3. enterprise fiscal periods, cost centers and budget versions;
4. enterprise Rate Cards and immutable pricing evidence;
5. allocation, close and adjustment/reversal evidence;
6. bounded route-release control; and
7. Model Call Authorization.

Stable startup subsequently reported every migration through `000053` already
applied; canary ran with `SKIP_MIGRATION=true`.

Post-migration readback preserved the exact pre-migration product and Feed
state:

| Fact | Result |
|---|---:|
| Organizations / users / projects | 1 / 20 / 1 |
| API keys / channels | 56 / 3 (`active=2`, `disabled=1`) |
| Call logs / transactions | 60 / 286 |
| Feed items / max sequence | 12 / 12 |
| Feed states / retained floor sum | 1 / 0 |
| Feed connections / acknowledged sequence sum | 1 / 12 |
| Active reservations | 0 |
| Unresolved Provider attempts | 0 |
| Pending terminal hooks / pending internal Outbox | 0 / 0 |

All newly introduced directory, finance, route-release and model-authorization
tables contained zero business rows after migration. No synthetic enterprise
directory, fiscal calendar, cost center, budget, Rate Card, allocation,
release, or authorization record was inserted to make the UI look populated.

## Runtime Deployment

- Stable backend image:
  `sha256:fa4762ec91dc185686a1bb68e37a723b1e5daaf724c30f9f94669677ce23c988`.
- Canary backend image:
  `sha256:73442681e55642d930f8224ae7fde5f30e65d276588db7f0bc631ab11d9fdfe5`.
- Frontend image:
  `sha256:cf179a84196527c5ceb955f330f1a65c76ef6f05571859f4446e74560c6f9eb2`.
- Stable and canary report `RUNTIME_VERSION=1.1.0-prd8`, exact commit
  `12efe9e3ba4dd753f6019468ca3b26d01a154c31`, and environment
  `development`.
- Stable `:8080`, canary `:8081`, local Nginx and public
  `http://8.130.18.85/health/ready` all returned
  `{"status":"ok","db":"ok","redis":"ok"}`.
- Stable and canary were healthy; frontend and Nginx were running from the
  exact commit-addressed release directory. Both Nginx configuration checks
  passed.
- The public root returned `200 OK` with the expected production SPA and
  security headers.

## Authenticated Browser Verification

The existing Chrome login was reused; no account, password, token, Provider
call or product record was created for this acceptance pass.

| Surface | Target result |
|---|---|
| Global shell | All primary groups and second-level items remain visible at once, with icons; the top row is a lightweight utility toolbar rather than a duplicate navigation row. |
| Platform overview | Existing dashboard data, channel health, recent requests and development label rendered after authenticated API loading. |
| Organization & Members | Local account list and role/project membership management rendered. The UI explicitly labels enterprise organization projection as POC-bound and does not treat account, person, workload identity and API-key owner as equivalent. |
| Model Call Authorization | Empty-policy default retained existing authority; explicit deny precedence, allow intersection and model-gateway-only narrowing boundary rendered. The right Authorization Inspector remained fixed and visible without an extra open action. |
| Budget & Cost Center | Existing request settlement/wallet semantics rendered separately from enterprise fiscal periods, cost-center versions and budget approvals. Empty enterprise state was truthful; request-path effect remained `report_only`. |
| Rates & Pricing | Existing Provider base rates remained available. Enterprise Rate Card showed separate catalog/contract/bill/display/settlement layers, immutable request evidence and no current request-path activation. |
| Allocation & Reconciliation | Period-gated allocation, close controls, adjustment/reversal evidence and zero-persistence offline Provider-cost preview rendered. The Financial Truth Inspector remained visible. |
| Runtime status | Stable runtime, PostgreSQL, Redis, Prometheus and Grafana reported available. Advanced retry, circuit, routing, streaming, Agent and budget settings rendered without exposing host identity or network assumptions. |

At a 2060-pixel browser viewport, document width equaled viewport width and no
horizontal overflow occurred. The fixed Authorization Inspector occupied the
right column while the main table retained usable width. The authenticated
production build emitted no captured page-console message during the target
pass. Separate Local browser checks had already passed at 1440, 1024 and 768
pixels; this target result does not relabel those Local viewport checks as
target-device coverage.

## Compatibility Conclusion

The deployment preserves request admission, model routing, current pricing,
reservation/settlement, wallet finality, Outbox identity, Source Snapshot
bytes/hash, completed-execution Feed endpoint/DTO/credential/order and
Cursor/Checkpoint behavior. Enterprise directory and finance are additive
product-local control/evidence layers. Model Call Authorization can only
narrow existing model-gateway authority; it cannot grant Tool or MCP rights.

X-Insight remains the cross-source investigation/projection owner. Tool
Gateway remains the tool authorization and side-effect owner. MCP Gateway
remains the MCP protocol/session/federation owner. No shared high-privilege
plugin runtime was introduced.

## Remaining Boundaries And Operational Risks

- ORG-3 still requires a named customer HCM/IdP/directory, supported delta or
  event interface, field authority, tenant topology and credential boundary.
- FIN-4 still requires a named ERP/GL, legal-entity and chart-of-accounts model,
  chargeback requirement, approval flow and export transport.
- P7-F17/P7-F18 intentionally remain offline/read-only plus human review; their
  automatic cross-product promotion is not an unfinished PRD8 target step.
- The ECS root filesystem had 7.8 GB free and 80.1% used after the build. No
  destructive cleanup was performed. Disk growth should be monitored before
  the next image-heavy release.
- The frontend image build reported two moderate and one high npm dependency
  advisories. The exact CI-tested lockfile was not mutated with an unreviewed
  `npm audit fix`; dependency remediation remains a separate maintenance item.
- Compose still reports the existing obsolete top-level `version` warning.
  It did not affect this release and should be removed in a separate minimal
  maintenance change.
- This is development/test Target evidence, not Production, real enterprise
  directory/ERP integration, paid Provider traffic, load, HA/DR or failover
  evidence.

## 2026-08-03 Agent And On-Prem POC Readiness Addendum

This addendum records the final development/test Target after the second
integration preparation work. It supersedes the earlier live release identity,
runtime measurements and remaining implementation gates in this document; the
earlier sections remain immutable historical evidence for PR #247.

### Repository And CI Chain

| Change | CI-passed head / tree | Required evidence | Squash merge / tree |
|---|---|---|---|
| [#250](https://github.com/foigent/unirouter/pull/250) Agent and on-prem POC readiness | `9144620ae350997d1ce3752716e9c2d7c472add3` / `3cebacc40fa607776a3cb227e66de896b3808417` | CI run `30758756376`: routing, backend, frontend and Docker passed; Protocol run `30758756367` passed | `7b8d6015c8a40499e333d2021d70c155dc49bf27` / same tree |
| [#251](https://github.com/foigent/unirouter/pull/251) overview attention closeout | `05799cc5a5276f2f7fd21a5068537d7e188a6c27` / `a4fc5651c35df273705a7205d63088eea1553325` | CI run `30761155534`: routing, frontend and Docker passed; backend correctly skipped for the frontend-only diff | `aac30fe1d2509c2e1fa624c5f836b94871b349b9` / same tree |
| [#252](https://github.com/foigent/unirouter/pull/252) channel-list fail-closed UI | `f1fd1482d163a0bb04a988efed79fbddf967537a` / `29ef34eb96557f3c0f2c16fc59f13fe57ab3fed9` | CI run `30762581400`: routing 9 s, frontend 8 m 08 s and Docker 1 m 39 s passed; backend correctly skipped for the frontend-only diff | `f81d8431aed9fea1f1ff63467f2c4df6cecfd2f5` / same tree |

The final code-bearing release commit is therefore `f81d8431...`; each squash
merge tree is byte-identical to its CI-passed head tree. The repository has no
automatic post-merge application CI, so this evidence does not invent one.
Local verification immediately before #252 included 54/54 frontend test files,
362/362 tests, TypeScript, the production build, `git diff --check`, and the
bundle budget. The largest JavaScript asset was 379,777 bytes against a
460,800-byte limit. PR #250's package-serialized Go, focused race, frontend,
compatibility-artifact and bundle checks remain recorded in
`PRD8-AGENT-ONPREM-POC-READINESS-LOCAL-20260803.md`.

### Product Scope Delivered

- `WorkloadApplication` is the long-lived enterprise application/Agent
  identity. Credentials bind to it without conflating person, account,
  project, application and API key. Effective readiness is the intersection of
  application, endpoint, credential, model authorization, limit, governance
  and eligible Provider route facts.
- Deployment Profiles and Gateway Endpoints persist versioned desired state.
  They do not store TLS private keys, apply host networking, or infer observed
  readiness. Public endpoints require HTTPS and were intentionally not created
  for the current HTTP-only ECS.
- Integration Management keeps the X-Insight completed-execution Feed as the
  Reference Connector and presents Feishu/Aily and future enterprise adapters
  without granting Tool Gateway, MCP Gateway or X-Insight authority.
- Zero-network compatibility artifacts cover the OpenAI SDK, OpenAI Agents SDK
  Chat path, LangChain, AutoGen, OpenClaw and Tencent WorkBuddy. They do not
  claim `/v1/responses` runtime support that UniRouter has not implemented.
- The reset CLI remains preview-only and fail-closed. It cannot delete data,
  overwrite an existing plan or reuse the current Source Instance. No reset was
  executed in this release.
- Platform Overview now provides compact, verifiable risk and pending-action
  summaries without duplicating the left navigation taxonomy. Runtime Status
  renders the exact version and commit.
- Upstream Test distinguishes channel-list loading, failure and ready states.
  A list failure reports unknown counts, offers retry and blocks every real
  Provider test; it can no longer present failure as zero channels.

### Final Target Backup And Release Identity

- Final pre-switch PostgreSQL custom-format backup:
  `/opt/unirouter/backups/pre-f81d8431-20260802T192126Z.dump`.
- Mode / size / SHA-256: `0600` / 571,421 bytes /
  `292cd7cd83fb752bb736b6c4cd02eea6578a0b39d9cee3a73311514afb38cc40`.
  `pg_restore -l` passed.
- Commit-addressed release directory:
  `/opt/unirouter/releases/f81d8431aed9fea1f1ff63467f2c4df6cecfd2f5`.
- Incremental `aac30fe1...` to `f81d8431...` Git bundle SHA-256:
  `71022eb59f468abeaa01ff90fd91c76485e7571a421c1c81a1b62041377af4a9`.
  Local and Target bundle verification passed; checkout commit/tree and a clean
  tracked state were read back before build.
- Final frontend image:
  `sha256:45a53f4061ded313331555f26696e468c2b7ab2ee4d930c2c3894f66e257c378`.
- Final stable backend image:
  `sha256:effbbe22d08beee93a5a0060f90b907edf4ecd9171a3fa5b47061b75ba862c94`.
- Final canary backend image:
  `sha256:9db4818ec4e5efeb88d0a2d7be7f4facc989b6b891177586487d164f75745bbf`.
- Explicit `aac30fe1` rollback tags retain frontend
  `sha256:f649d6a868474ce7da7e80fa80dd8ce12417f76c83d95459c67032368b7d0abf`,
  stable
  `sha256:ed9e5d930783317762249d88f49fc084f6320c098cbedcb17993ee3897213ff8`
  and canary
  `sha256:f10e0eeab472e8b32d4f9afc3b90eb76bfc5ef7ed78869da5d571434f140b249`
  image identities.
- Build and rollout used the existing Compose project `unirouter`. Canary,
  stable, frontend and Nginx were recreated separately with `--no-deps` and
  `--no-build`; PostgreSQL, Redis, Prometheus and Grafana were not restarted.
- Stable and canary were healthy with restart count zero. Nginx configuration
  validation passed. Local and public `/health/ready` returned
  `{"status":"ok","db":"ok","redis":"ok"}`.
- Runtime Status reported `1.1.2-prd8-poc · f81d8431aed9`, environment
  `development`, role `stable`, and available PostgreSQL, Redis, Prometheus and
  Grafana.

### Zero-Drift Data Readback

The same read-only queries were run immediately before and after the final
switch. Every result was byte-for-byte equal:

| Fact | Before | After |
|---|---:|---:|
| Migration rows / maximum | `54 / 000055_prd8_deployment_profiles` | same |
| Organizations / users / projects | `1 / 20 / 1` | same |
| API keys / channels | `56 / 3` | same |
| Channel lifecycle | `active=2 / disabled=1` | same |
| Call logs / transactions | `60 / 286` | same |
| Feed items / max sequence / retained floor sum / acknowledged sum | `8 / 12 / 4 / 12` | same |
| Workload applications / deployment profiles / Gateway Endpoints | `0 / 0 / 0` | same |
| Freeze terminal rows | `cancelled=9 / settled=51` | same |
| Provider attempts | `rejected=6 / settled=13` | same |
| Pending runtime terminal hooks / internal Outbox rows | `0 / 0` | same |

No application, deployment profile, endpoint, credential, call, transaction,
Feed event, Cursor/Checkpoint, or synthetic POC record was created. Migrations
`000054` and `000055` were already applied by the prior #250 release; #251 and
#252 added no migration.

### Final Authenticated UI Verification

The existing authenticated browser session was reused. No password, token or
secret was read, and no real Provider test control was activated.

- `/test` rendered three channels, two `active` channels, one explicit
  `kimi（通道禁用）` exclusion and two executable routes. The result table
  remained empty. This confirms that an enabled Kimi credential does not
  override a disabled channel. The transient-failure branch was validated by
  an isolated regression test rather than deliberately breaking the Target
  API.
- `/status` rendered the exact final runtime identity and healthy dependencies.
- At 1,024- and 767-CSS-pixel widths, document `scrollWidth` equaled
  `clientWidth`; no horizontal overflow was present.
- Captured browser console logs were empty. The browser was left on `/test` at
  its default viewport for Product Owner inspection.

### Final Boundaries And Risks

- The visible Target is `http://8.130.18.85/`. Public HTTPS remains unavailable:
  Nginx is HTTP-only and the cloud 443 ingress/certificate decision is outside
  this release. A public Gateway Endpoint must remain unverified until TLS,
  ingress and second-device evidence exist.
- Root filesystem use after retaining exact release and rollback images was
  85%, with 6.0 GB available. No image prune or destructive cleanup ran.
- The existing Compose top-level `version` warning remains. Dependency builds
  also retain the previously observed two moderate and one high npm advisory;
  no unreviewed `npm audit fix` changed the CI-tested lockfile.
- ORG-3 still requires a real customer HCM/IdP/directory; FIN-4 still requires
  a named ERP/GL and finance mapping. Neither was simulated.
- This remains development/test Target evidence, not Production, customer
  directory/ERP, paid Provider traffic, load, HA/DR, failover or second-device
  POC evidence.
- A later documentation-only evidence commit is not part of the deployed
  `f81d8431...` code-bearing release and must not be reported as a runtime
  commit.
