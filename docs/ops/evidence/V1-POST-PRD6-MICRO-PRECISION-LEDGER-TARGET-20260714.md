# V1 Post-PRD6 Micro-Precision Ledger Target Evidence — 2026-07-14

## Result and boundary

- Status: **Target verified** on the existing Alibaba Cloud development/test
  ECS. The V1 micro-CNY ledger, migration `000036`, exact release runtime and
  frontend `开发环境` label are active.
- Final application commit: `545e514ab51937e6589512192141b9359cfcb504`.
- PRD6 remains `Completed (Accepted Scope)`. This Post-PRD6 release-hardening
  evidence neither reopens an accepted PRD6 row nor promotes Production
  evidence.
- The target contains non-important test/demo data. Existing model-price rows
  were preserved exactly; no automatic repricing or bulk data cleanup was
  performed.
- No real/paid provider request, real external detector request, credential
  reset, security-group change, Redis bulk deletion or X-Insight runtime change
  was made.

## Repository and CI truth chain

| Slice | Exact head / required CI | Merge result | Result |
|---|---|---|---|
| V1 micro-precision ledger | `1944e3103eee8f4c9a9c2303c4ae6c634798e13b`; run `29334221779` | PR #202 squash-merged as `ec0dde201841b8286f6802cc7e588e79eda40719` | Change Routing, Backend Tests, Frontend Build and Docker Build passed |
| Deployment environment label | `b76f253671a26fbd00df0ced978fbc229ff98e03`; run `29339373907` | PR #203 squash-merged as `545e514ab51937e6589512192141b9359cfcb504` | Change Routing, Backend Tests, Frontend Build and Docker Build passed |

PR #203's first Docker assertion run stopped before an application build
because `docker compose config` correctly required the pre-existing
`GRAFANA_ADMIN_PASSWORD` setting. The follow-up supplied harmless CI-only
Compose prerequisites to that assertion. The exact final head then passed the
real required Docker job; no product/runtime bypass was introduced.

## Why the ECS update required an explicit cutover

Migration `000036_v1_micro_precision_ledger` deliberately replaces canonical
cent-based database, outbox and Redis workflow authority with micro-CNY v2
authority. Mixed old and new financial writers are unsafe, so this was not a
normal rolling frontend/backend refresh. Before applying it, the operator had
to drain writers, prove no active financial state, create and validate a
database backup, apply `000035` before `000036`, and then start only the exact
new images. This was the reason the merged version was not automatically
updated to ECS before the User authorized the non-rolling operation.

## Initial V1 migration and release cutover

- Release commit: `ec0dde201841b8286f6802cc7e588e79eda40719`.
- Source archive: SHA-256
  `0505b3d9ff72a8e7d4bb115ee0ed36abd55cd63c1aab45c7368b47f5697517b4`,
  size `5,306,689` bytes.
- Built image IDs:
  - backend stable/canary:
    `sha256:130d6d8a8a590684b79c72e014518b2db0e7154c2290431a9dcc58b6e4c54313`;
  - frontend:
    `sha256:34aeba3b903da921ced9bfb1589e120c2468bdf1c65fd0c94e90d106bcd69828`.
- Pre-cutover PostgreSQL backup:
  `/opt/unirouter/backups/pre-v1-ec0dde201841b8286f6802cc7e588e79eda40719-20260714T134203Z.dump`,
  size `273,046` bytes, SHA-256
  `a96b25032beb6177ca9ed4582e8ffd1184bf6935b7779871ac58e8400f9929b3`;
  its manifest SHA-256 is
  `fe3186915bae179628b0852fa38ed60b146928451f309e6d8c0027db8e5dd19e`.
- The first backup-validation attempt stopped before migration because the ECS
  host did not contain `pg_restore`; the old runtime was restored
  automatically. Validation was corrected to use the PostgreSQL container,
  without changing data or advancing the migration.
- Successful cutover invocation: `t-wl06qwgoupatgqo`; measured application
  interruption was approximately eight seconds.
- Migration `000035` applied in approximately `3 ms`; deliberate non-rolling
  migration `000036` applied in approximately `265 ms`.
- `micro_ledger_runtime_state.micro_evidence_written` read back `true`, so a
  schema down-migration is no longer a valid rollback. Subsequent recovery is
  roll-forward with a `000036`-compatible binary.

## Development-environment label correction

The target backend already reported `RUNTIME_ENVIRONMENT=development`, but the
top bar displayed `环境未配置`. This was a target-build configuration defect:
Vite embeds `VITE_DEPLOY_ENV` at frontend build time, while the independent
Nginx frontend image had not accepted or forwarded that build argument. A
container runtime variable cannot rewrite an already-built JavaScript bundle.

PR #203 wires the Compose frontend build argument through
`frontend/Dockerfile`, documents it, and keeps the label's safe unconfigured
fallback. The target release sets both runtime and frontend build-time values
to `development`, which the existing UI maps to `开发环境`.

## Exact `545e514` build and final cutover

- Release directory:
  `/opt/unirouter/releases/545e514ab51937e6589512192141b9359cfcb504`.
- Authenticated source archive: SHA-256
  `4417fed5428d3fee781628f6f577c5a138d97e91242a0658b392123d52f3d403`,
  size `5,306,906` bytes. Local and target checksums and sizes matched.
- Release configuration retained existing secrets with mode `0600` and set
  only the non-secret version/environment contract: runtime environment
  `development`, frontend deploy environment `development`, version `v1`, and
  exact commit `545e514ab51937e6589512192141b9359cfcb504`.
- Successful prepare invocation: `t-wl06qwk4sx8edj4`. One earlier submission
  failed immediately because the default shell rejected Bash `pipefail`; no
  file or runtime operation occurred before the corrected Bash invocation.
- Build invocation: `t-wl06qwk6xmk5ibk`. Backend source-context assertions,
  Go `1.25.12` build with exact commit metadata, frontend `npm ci` and Vite
  build passed.
- Built image IDs:
  - stable backend:
    `sha256:23988e8ca838cf5c05f41c8bdc39b23fb410d376f84eb7e53eec598ccbb74578`;
  - canary backend:
    `sha256:988f6e50dfb5eefcabbb0675abd0c725e766fc7ea508fae75e87c817ebe97d43`;
  - frontend:
    `sha256:052f53bb0f2e5f8b4e970abb61389ef83ff11524408954d9aa9a564342afd20e`.
- The prior `ec0dde2` images were retained under explicit rollback tags. No
  image, release directory or backup was bulk-deleted.

### Safe failed attempt and recovery

Cutover invocation `t-wl06qwkpqcrueio` passed the database/financial preflight
and replaced canary and stable. It then tested the frontend as soon as Docker
reported the container `running`; Nginx had not yet accepted connections, so
the single immediate `curl` failed. The script automatically restored the old
image tags and recreated the old runtime. Independent invocation
`t-wl06qwkrk1vpcsg` subsequently proved the public, stable and canary old
runtime healthy.

This was a readiness-probe timing defect in the cutover script, not a failed
application image or migration. The retry used bounded HTTP readiness polling
and explicitly re-tagged the already-verified new image IDs. One Cloud
Assistant API submission timed out before any command reached ECS; the
sanitized resubmission succeeded.

### Successful final switch

- Final cutover invocation: `t-wl06qwkuymec2yo`.
- Preflight again proved zero active reservations, zero unresolved provider
  attempts, zero pending terminal hooks, latest migration `000036`, and
  `micro_evidence_written=true`.
- Measured cutover duration: approximately 16 seconds.
- Stable, canary and frontend containers use the exact new image IDs above.
- Public, stable and canary readiness each returned
  `{"status":"ok","db":"ok","redis":"ok"}`.

## Independent target readback

Post-cutover invocation `t-wl06qwl1fbyaiv4` independently verified:

- all four application containers were running with restart count `0` and the
  exact `545e514` release working directory;
- stable and canary reported environment `development`, version `v1`, exact
  commit `545e514ab51937e6589512192141b9359cfcb504`, Go `1.25.12`, and their
  respective stable/canary roles;
- PostgreSQL and Redis were connected; Prometheus and Grafana probes were
  connected;
- latest migration was `000036_v1_micro_precision_ledger`, the schema exposed
  `38` `*_micros` columns, and micro-ledger evidence was present;
- active reservations, unresolved provider attempts and pending terminal hooks
  all remained zero;
- wallet authority read back exactly as balance `8,790,000` micros, frozen
  `0`, granted `0`, and spent `1,830,000` micros;
- the current reconciliation set reported estimated ratio `0`, threshold
  `0.2`, and no estimated-ratio alert. Current test data contains no estimated
  usage, so zero is the correct result rather than a calculation regression;
- the built frontend bundle contains the static `development` deploy value.

## Authenticated browser evidence

A ten-minute ephemeral JWT was used only for read-only target QA. It was
cleared from browser storage and its single local token file was deleted after
the browser closed; no account password or persistent credential changed.

Real browser checks on the public target verified:

| Page | Evidence |
|---|---|
| Dashboard `/` | Top bar displayed `开发环境`; all 3/3 channels were healthy; wallet showed `¥8.79`; costs rendered through the micro-CNY formatter |
| Internal Status `/status` | Top bar displayed `开发环境`; runtime displayed `development / stable`; DB, Redis, Prometheus and Grafana were available; redacted topology and notification sections rendered |
| Model Pricing `/model-prices` | Top bar displayed `开发环境`; input/output labels used `CNY/百万 tokens`; all 11 existing test rows rendered without automatic repricing |
| Reconciliation `/reconciliation` | Top bar displayed `开发环境`; the current zero estimated ratio and its table rendered consistently with the API data |

The browser recorded zero console errors and zero warnings. OIDC configuration,
current user, dashboard statistics, internal status, runtime status, model
prices, reconciliation and model-name requests all returned HTTP 200.

Ignored local screenshots:

- `output/playwright/v1-ecs-development-dashboard-20260714.png`;
- `output/playwright/v1-ecs-development-status-20260714.png`;
- `output/playwright/v1-ecs-development-model-prices-20260714.png`;
- `output/playwright/v1-ecs-development-reconciliation-20260714.png`.

## Issue closure matrix

| ID | Final classification | Priority / impact | Cause | Owner | Resolution and acceptance evidence |
|---|---|---|---|---|---|
| `V1-FIN-001` | confirmed bug/regression; Target verified / closed | P0; sub-cent and zero-token calls could be overcharged | Integer-cent truncation and a one-cent terminal clamp | UniRouter billing/wallet/gateway | PR #202 required CI, target `000036`, exact stable/canary runtime, micro schema/API/UI readback |
| `V1-PRICE-RANGE-001` | confirmed bug/regression; CI and Target verified / closed | P1; accepted effective prices could overflow and fail a model closed | Independent field bounds did not validate their combined effective value | UniRouter pricing authority | Transactional per-model range validation passed migrated CI; exact code and schema are deployed |
| `V1-PRICE-RENAME-001` | confirmed bug/regression; CI and Target verified / closed | P1; a partial rename could orphan billing/routing references | Model identity was mutable in only the base-price row | UniRouter pricing API/UI | V1 identity is immutable; CI regressions passed and exact UI/API code is deployed |
| `V1-MIG-SEQ-001` | confirmed bug/regression; CI verified / closed | P1; first organization creation on a fresh install could collide with id `1` | Historical seed did not advance `orgs_id_seq` | UniRouter migration | `000036` advances without rewinding the sequence; migrated CI passed and target migration applied |
| `V1-UI-MONEY-001` | confirmed bug/regression; Target verified / closed | P1; valid micro balances could look unchanged | UI rounded exact strings to two decimals | UniRouter frontend | BigInt adaptive formatting passed tests and the exact new frontend is browser-verified |
| `V1-REPO-UI-ARTIFACT-001` | confirmed release-hygiene defect; CI verified / closed | P2; obsolete cent UI could be mistaken for release output | Stale PRD1 bundle remained after independent Nginx frontend adoption | UniRouter build/docs | Obsolete tracked files were removed individually; exact frontend image rebuilt and deployed |
| `V1-TEST-DB-CLEANUP-001` | confirmed test-hygiene defect; CI verified / closed | P2; green tests could leave isolated databases | Cleanup discarded database-drop errors | UniRouter test infrastructure | Exact-name forced cleanup now fails loudly; required CI passed |
| `V1-TARGET-LEDGER-001` | target environment configuration; Target verified / closed | P0 rollout gate; mixed writers would corrupt unit authority | Deliberate non-rolling schema/runtime contract | UniRouter deployment owner | Backup, drain/preflight, `000035`→`000036`, exact image switch, rollback retention and DB/API/browser readback completed |
| `V1-TARGET-ENV-001` | target environment configuration; Target verified / closed | P1 UX/trust; target displayed an ambiguous environment badge | Frontend build omitted `VITE_DEPLOY_ENV` | UniRouter build/deployment | PR #203 run `29339373907`, merge `545e514`, exact target image and browser `开发环境` evidence |
| `V1-XI-LEDGER-001` | X-Insight coordination; pending | P1 before the next snapshot exchange | Consumer must adopt `cost_micros` v2 metadata | X-Insight consumer owner with UniRouter evidence owner | UniRouter producer is ready; no X-Insight consumer acceptance or joint run is claimed |

## Remaining non-claims

- This is a development/test ECS target, **not Production verified**.
- No real provider, real detector, enterprise IdP, TLS 443, multi-instance load,
  capacity, managed HA/DR or migration-down test was performed.
- PRD6 target rows P6-E02 through P6-E04 remain later work and do not affect
  PRD6 Accepted Scope completion.
- X-Insight v2 consumer acceptance remains a separate coordination gate. No new
  snapshot should be exchanged under an assumed v1 money contract.
- The broader user-reported Post-PRD6 product/UI review is not declared fully
  complete by this financial/deployment closeout; separately classified items
  remain in the active issue and PRD7 candidate records.
