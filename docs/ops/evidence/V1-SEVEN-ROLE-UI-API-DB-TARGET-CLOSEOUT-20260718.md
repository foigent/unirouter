# V1 Seven-Role UI/API/DB Target Closeout — 2026-07-18

**Environment:** Alibaba Cloud development/test ECS; not Production or a
production-equivalent customer environment

**Batch:** `V1-UX-IA-001` plus the target-verifiable authentication, session,
local-login, RBAC, API and release slices of `V1-SECURITY-REMEDIATION-001`

**Status:** **Target verified / closed only for the explicitly enumerated
surfaces below**

**PRD6 boundary:** PRD6 remains `Completed (Accepted Scope)` with 27/27
accepted rows closed. This evidence does not reopen PRD6, accept PRD7, or prove
X-Insight integration.

## 1. Scope And Authority

The User authorized the development/test ECS release and a bounded seven-role
acceptance run. The run checked role-specific entry points, visible and denied
routes, representative API authorization, administrator-session logout,
developer access-key lifecycle, desktop/mobile layout, release/database
identity, and cleanup.

The code-bearing repository and development-target release authority is PR
#217 merge `cdbaa972db9b318b4412dd2aebce118c934debac`. A later docs-only merge
that publishes this file is documentation authority only; it does not replace
that code-bearing release identity.

## 2. Repository, CI And Signed Release Truth Chain

| Item | Verified fact |
|------|---------------|
| PR | #217, `fix: close first-release security and UX gaps` |
| PR head | `afb5269a15619858f7eb6c477a35d59ca5753151` |
| Required CI | run `29590689179`, conclusion `success` |
| CI jobs | Change Routing plus branch-protection-required Backend Tests, Frontend Build and Docker Build all `success` |
| Squash merge / release commit | `cdbaa972db9b318b4412dd2aebce118c934debac` |
| Release | version `1.0.1`, signed sequence `5`, target `linux/amd64` |
| Target schema | `000042_v1_local_login_identity` |
| Signed package SHA-256 | `79a3a04fe697cef6bc668f51cc6616ca271f4d475037e223e8deb22fe2abb489` |

The PR head and squash merge resolve to the same Git tree
`497a68989e195c0fd3120d70a7a8c99cdfa11f71`; the pull-request CI therefore
validated the exact merged content. No automatic post-merge main run was
created, which is the repository's intentional no-duplicate-CI policy.

The signed package bound the exact Compose override, build provenance,
migration hashes, schema-37-to-42 isolated rehearsal receipt and runtime image
references. Its rehearsal migrated all five new ledger entries, reached
`000042`, produced no quarantine rows, and validated the session, guardrail,
delivery, stream-fence and identity objects. The target database later read
back sequence `5`, version `1.0.1`, the exact release commit and schema
`000042` as one installed release row.

## 3. Exact Runtime Images

| Service | Digest-pinned image |
|---------|---------------------|
| Backend stable/canary | `127.0.0.1:5000/unirouter-backend@sha256:fa3b3adb4142b67000fb2d85275e2c68257508b78d1520968f313ba526c1e75d` |
| Frontend | `127.0.0.1:5000/unirouter-frontend@sha256:a7ddc9e71ab64544aae955da8b498c6d05c05f24c32eaa278b2dd3e8fb016bec` |
| Nginx | `nginx@sha256:0d3b80406a13a767339fbe2f41406d6c7da727ab89cf8fae399e81f780f814d1` |
| PostgreSQL | `postgres@sha256:e013e867e712fec275706a6c51c966f0bb0c93cfa8f51000f85a15f9865a28cb` |
| Redis | `redis@sha256:6ab0b6e7381779332f97b8ca76193e45b0756f38d4c0dcda72dbb3c32061ab99` |
| Prometheus | `prom/prometheus@sha256:5ccad477d0057e62a7cd1981ffcc43785ac10c5a35522dc207466ff7e7ec845f` |
| Grafana | `grafana/grafana@sha256:0dc5a246ab16bb2c38a349fb588174e832b4c6c2db0981d0c3e6cd774ba66a54` |

Final readback reported eight expected containers running, with every
service-specific probe passing: PostgreSQL, Redis, stable API, canary API,
frontend, Prometheus, Grafana and Nginx.

## 4. Acceptance Runner And Artifact Integrity

The final acceptance run was Cloud Assistant invocation
`t-wl06r9vvdaxn1fk`, from `2026-07-18T07:58:34Z` through
`2026-07-18T08:01:08Z`, exit `0`. Its structured run ID was
`20260718075833-e8d4b221`, status `passed`, with an empty failure list.

| Artifact | SHA-256 |
|----------|---------|
| Final runner source | `e0bb10503b260bb1c7448fd92c06660df1f4d077a3b60df672706e18bc4336ad` |
| Cloud Assistant output | `9837833e66ed3d11022888ad2e7813d492d5eab076471747e4ed1b64ecb1ae2a` |
| Secret-minimized browser result archive | `2e769728d569de6557b13215ba1eddb606639ef9539ee706f84f4d18c8488d71` |
| Pre-cleanup database/runtime readback | `4b68817195784f34a5e855dd47297d9bdc7248cfe99f3da6f180b70ce8b3429f` |
| Post-cleanup database/runtime readback | `0595e8e6e87088041d6c83cf197eb10b38396902abc9e65dbba492ebfca394c2` |

The extracted evidence was scanned for bearer headers, authorization values,
JWT shapes, complete API keys and password-value signatures; none were found.
Playwright traces were deliberately disabled because authenticated network
traces can retain login responses, JWTs or API keys. Raw archives and
screenshots remain local temporary evidence and are not committed.

## 5. Role-By-Role Browser Matrix

The source-derived contract enumerated seven roles and 52 allowed route
visits. All `52/52` route visits reached the expected heading and final path,
the tested denied routes redirected as specified, settings opened as `我的账号`,
and visible navigation matched the role contract.

| Role | Allowed route visits | Representative outcome |
|------|----------------------|------------------------|
| `platform_admin` | 21/21 | Platform dashboard plus all platform task groups and API documentation |
| `org_admin` | 14/14 | Organization dashboard and organization-owned governance/operations surfaces; platform channels excluded |
| `billing_admin` | 8/8 | Finance/usage dashboard and finance/observability surfaces; user administration excluded |
| `org_viewer` | 2/2 | Read-oriented organization dashboard and documentation only |
| `project_admin` | 3/3 | Project workbench, project members, project docs; scoped notification control visible |
| `developer` | 2/2 | Project workbench and docs; key-create/debug entry available, notification control hidden |
| `viewer` | 2/2 | Project workbench and docs; key-create/debug/notification controls hidden |

For `project_admin` and `developer`, the `创建项目访问凭证` side sheet was
opened and cancelled without creating a credential. This proves the control is
operable and safely cancellable; the separate developer-key lifecycle below
proves actual creation. The viewer had no such control.

At 1440 px, the application shell reported a 252 px sidebar and a 28 px content
gap; content began at x=280 and had no horizontal overflow or render anomaly.
This directly addresses the earlier excessive-left-blank concern for the
tested pages. It is not a visual claim about every future dataset or browser.

## 6. API Authorization Matrix

Every enumerated positive and negative API check matched its expected status:

| Role | Positive check | Negative/scope check |
|------|----------------|----------------------|
| `platform_admin` | License and channel reads `200` | Platform authority projection checked against UI |
| `org_admin` | User list `200` | Platform channel list `403` |
| `billing_admin` | Wallet read `200` | User list `403` |
| `org_viewer` | Dashboard stats `200` | Project list `403` |
| `project_admin` | Project members and project-scoped notifications `200` | Unscoped notification list `403` |
| `developer` | Project list `200` | Project-members administration `403` |
| `viewer` | Project/key reads `200` | Key create `403` |

The License projection was also compared across all seven roles and was
consistent with their exposed surfaces.

## 7. Developer Access-Key Lifecycle

A developer created one personal project key through the supported API. Only
its non-secret identifier and prefix were retained in evidence: key ID `48`,
project `1`, prefix `ur-0001-0e8d25011d77`. The full key was never written to
the result JSON or this document.

The new key returned `200` from `/v1/models` with exactly two authorized model
entries. Disabling it returned `200`; replay then returned exact `401` /
`auth_error.api_key_disabled`. The database readback matched the developer
owner, personal scope, project ID, disabled state and create/disable audit
source `developer_console`.

This validates authentication, project/model authorization and disabled-key
rejection without dispatching a paid provider request.

## 8. Session, Database And Runtime Reconciliation

All seven final logout calls returned `200` with `revoked=true`. Database
readback found 42 fixture sessions in total, zero live and 42 revoked; each of
the seven users had six revoked sessions and no live session. The corresponding
login/logout and key create/disable audit rows were present.

Before cleanup, the exact seven fixture users were active, their three project
memberships matched `project_admin`, `project_developer` and `project_viewer`,
and key `48` was disabled. After cleanup:

- all seven fixture users were disabled;
- all 42 sessions remained revoked with zero live;
- key `48` remained disabled;
- memberships and audit rows were retained for referential/audit evidence;
- release sequence `5`, version `1.0.1`, commit `cdbaa972...`, schema
  `000042`, and all eight service probes were unchanged.

The readback was observational; it did not reprice models, alter wallets,
dispatch a provider call or claim a supplier-billing result.

## 9. Responsive And Browser-Runtime Evidence

Mobile acceptance at 390x844 passed for `platform_admin` and `project_admin`:
no horizontal overflow, the 252 px drawer opened, its navigation labels matched
the contract, clicking `API 文档` reached `/docs` and closed the drawer, and
settings reached `/settings` / `我的账号`.

Across the final browser run, console errors, console warnings, page errors,
request failures, external requests and unexpected responses were all empty.
This is evidence for the exercised paths only, not every possible operation.

## 10. Cleanup And Credential Boundary

- Fixture-disable invocation `t-wl06r9ws9mtifi8` succeeded for the seven exact
  users; users were disabled rather than deleted so audit references remain
  valid.
- Bootstrap credential restoration invocation `t-wl06r9wxki876dc` exited `0`
  and reported release trust 1/1, password restoration verified, temporary
  keypair absence and loopback login/logout verified.
- The outer restore wrapper produced a false-negative receipt classification
  because a non-secret release-trust log line preceded its strict three-line
  success matcher. The remote operation itself succeeded; rerunning the
  password change would have created unnecessary risk and was not done.
- The exact remote temporary credential file was verified as a regular,
  non-symlink, root-owned mode-0600 file and then deleted by invocation
  `t-wl06r9x523q6by8`; the postcondition was absent.
- Four exact temporary OSS result objects were downloaded and hashed, then
  deleted one at a time. Final object count was zero; the empty temporary
  bucket was removed without a force flag and read back absent.
- No private key, password, JWT, full access key, signed URL or cloud
  credential is recorded in the repository.

## 11. Final Classification

| Surface | Result |
|---------|--------|
| PR #217 required CI and merge | CI verified / closed |
| Signed release `1.0.1`, sequence `5`, schema `000042` | Target verified |
| Seven-role UI/navigation/route matrix | Target verified, 52/52 enumerated visits |
| Representative seven-role API matrix | Target verified |
| Developer key create/model-list/disable/replay | Target verified |
| Logout and database session revocation | Target verified |
| Desktop/mobile layout and browser runtime for exercised paths | Target verified |
| Fixture/credential/temporary-transfer cleanup | Verified complete within the stated boundary |
| PRD6 | Remains `Completed (Accepted Scope)`; not reopened |
| X-Insight consumer/joint acceptance | Coordination pending; not tested here |

The final R7 selector correction was a test-harness issue, not a product bug:
the visible create button existed, while an earlier exact accessible-name
selector included icon/AX text and failed to find it. The corrected selector
was scoped to the project-access container, opened the side sheet, cancelled
it, and then the full run passed. No new product regression was identified by
this acceptance run.

## 12. Explicit Non-Claims

This evidence does **not** claim:

- Production, customer-production-equivalent, TLS, HA, capacity, DR or general
  migration-down verification;
- that every one of the 39 security findings was independently exercised in
  the target; their full closure is CI verified, while only the slices above
  are Target verified;
- cross-organization target isolation; this run used one organization and
  repository integration tests remain the evidence for that boundary;
- a real enterprise IdP/HR/domain/Feishu integration or external detector;
- provider invoice, billing-console, export/API, discount, cache, tax,
  rounding, supplier COGS, pass-through or margin reconciliation;
- a paid provider generation call in this run;
- X-Insight consumer acknowledgement or joint integration acceptance;
- Update Center, click-to-upgrade, automatic apply or general N-1 rollback;
- customer License key ceremony, customer trust-root custody or customer
  offline OCI media delivery.
