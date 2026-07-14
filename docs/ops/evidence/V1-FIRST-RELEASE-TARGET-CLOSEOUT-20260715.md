# V1 First Release Product Audit — Target Closeout Evidence

**Date:** 2026-07-15 (Asia/Shanghai)

**Target:** existing Alibaba Cloud development/test ECS in
`cn-wulanchabu`; this is not a Production claim

**Scope:** the first-release defect/UX corrections classified in
`V1-FIRST-RELEASE-PRODUCT-AUDIT-LOCAL-20260715.md`

**Result:** **Target verified / closed** for the accepted code and presentation
corrections. Target upstream allowlist alignment, paid provider traffic, a real
external detector, enterprise IdP login, Production readiness and X-Insight v2
consumer acceptance remain separate gates.

PRD6 remains `Completed (Accepted Scope)`. This Post-PRD6 evidence neither
reopens a PRD6 row nor accepts PRD7 Update Center, vendor catalog discovery,
directory synchronization or automatic-upgrade product scope.

## Repository and CI truth chain

| Item | Exact evidence | Result |
|---|---|---|
| PR | #207, `fix(release): close first-version product audit gaps` | Squash-merged |
| Final PR head | `f63bcb268251cd6ec0d8701afc20a7928cd32ecb` | Exact head used by required CI |
| Required CI | run `29368640933` | Change Routing, Backend Tests, Frontend Build and Docker Build all passed |
| Merge | `a0a567dd169dcb133b09eed3f711cdcf87214641` at 2026-07-15 05:24:12 Asia/Shanghai | `main` release authority |
| Post-merge CI | no run exists for the merge commit | Expected: the CI-efficiency policy does not repeat the already-passed full PR run on automatic `main` push |

The merged tree contains the same 41 files as the exact tested head. Before
image construction, all 41 changed target files were checked against the
merged-tree SHA-256 manifest. No unmerged branch commit was deployed.

## Deployment boundary and rollback

- The User authorized updating the existing test system and stated its data is
  non-important. Existing records were nevertheless preserved; no bulk data,
  Redis, release, image or backup deletion was performed.
- Release working directory:
  `/opt/unirouter/releases/a0a567dd169dcb133b09eed3f711cdcf87214641`.
- Exact image IDs:
  - stable and canary backend:
    `sha256:a7fe9237174862c5afcfaab892d486a61acde44684923a522f8b6ffc0ddfaa82`;
  - frontend:
    `sha256:5b4ad05756ed9f3cc9c5815fa2e19ce0461d7575076493425213f6663120db7e`.
- Stable and canary report version `1.0.0`, commit
  `a0a567dd169dcb133b09eed3f711cdcf87214641`, environment `development`
  and roles `stable` / `canary` respectively.
- Latest migration is `000037_v1_commercial_release_control`.
- Pre-cutover PostgreSQL backup:
  `/opt/unirouter/backups/uniroute-before-a0a567d-20260714T214829Z.dump`,
  size `298,845` bytes, SHA-256
  `04d35dd8096556f14f1f1e33f7b20a1ae4dbc1b0e2b47bebc4cfb11d15ff1214`;
  `pg_restore --list` succeeded before cutover.
- The first cutover attempt reached all HTTP readiness endpoints but stopped
  because the bounded assertion also required Docker health to leave
  `starting`. Automatic rollback restored the prior healthy release. The
  corrected retry waited for both HTTP and Docker health and completed.
- Old releases, rollback images and the validated backup are retained.

Final independent readback showed stable/canary healthy with restart count
`0`; frontend running with restart count `0`; all four application/Nginx
Compose working directories point to the exact release above. Public, stable
and canary readiness each returned:

```json
{"status":"ok","db":"ok","redis":"ok"}
```

Startup-log checks found no panic, fatal, invalid-License or server-error
signal. Root disk usage was 54% at final readback.

## Commercial License and signed-upgrade target evidence

The development target uses deliberately public **test** signing trust, not a
customer or production key ceremony:

- License ID `lic_unirouter_devtest_20260715`;
- deployment ID `unirouter-devtest-ecs-cn-wulanchabu`;
- License key ID `test-license-20260715-a`;
- License expiry `2026-10-15T00:00:00Z`;
- signed limits: 100 active API keys and 20 active users/projects/channels;
- authenticated License status reported active, valid, signature-valid,
  deployment-matched, enforceable and commercial.

Target-specific signed acceptance bundle:

- file:
  `/opt/unirouter/incoming/unirouter-1.0.0-ecs-acceptance-seq2-a0a567d.tar.gz`;
- size `7,759` bytes;
- SHA-256
  `9e09b1c495a6da8255bb350f3b59c59373606aa2413aff7886d1d7a6a39e9aa3`;
- upgrade key ID `test-upgrade-20260715-a`;
- release sequence `2`, version `1.0.0`, exact commit `a0a567d`, target
  `linux/amd64`, schema `000037`.

The old runtime verified the bundle before cutover. The new runtime verified
it, recorded it once and read back release state sequence `2` with the exact
commit, bundle hash, key and schema. Both a second verify and a second
`record-applied` rejected replay because current sequence `2` is outside the
signed window `1-1`. `release_history` contains sequences `1` and `2` exactly
once each.

This proves the current offline verification/record/replay foundation on this
test target. It does **not** prove a customer delivery key ceremony, general
N-1 rollback, arbitrary package upload, click-to-upgrade Update Center,
automatic apply or Production operational readiness.

## Authenticated API acceptance

Temporary target QA exercised the real merged API and existing database. Raw
tokens, passwords and full caller secrets are not stored in this repository.

| Surface | Target evidence |
|---|---|
| Access credentials | 38 records exposed display prefixes and creation times. A temporary caller key was created, authenticated against `/v1/models`, disabled and then rejected with `401 auth_error.api_key_disabled`. No hard-delete behavior was added. |
| Upstream credentials | Three existing records exposed masked prefixes, status, model scope and creation time. Secret-preserving edit semantics and future-write separator normalization are active. Existing records were not rewritten. |
| Routing preview | The real endpoint returned deterministic snapshots, candidates/exclusions and reasons without an upstream request. Every exact catalog model currently has zero candidates because its channel credential allowlist does not match the channel model identifiers; exclusions report `key_model_not_allowed`. This is target configuration, not a fake preview or code regression. |
| Content safety | Policy CRUD, versioning and runtime snapshots are real. A shadow policy was created, read from the snapshot, changed to disabled version 2 and read back. No real external detector or customer content was invoked. |
| Model governance and pricing | Governance/catalog records are real and stale PRD labels are gone. Pricing returns canonical CNY-per-million-token fields. Provider catalog auto-discovery is not implemented or implied. |
| Notifications | Six existing events had `not_configured`: events were persisted, but no enabled matching Webhook route existed. UI now shows human resource labels such as `访问凭证 #28` and `未配置 Webhook`. |
| Finance and reconciliation | Wallet, transactions, model prices and 188 reconciliation rows returned successfully. Estimated ratio `0` is correct for the current authoritative source set; request rows may still contain preflight estimates without becoming `estimated` usage authority. No target financial mutation was performed. |
| Users and audit | User responses include existing email/auth-provider/timestamps without password fields. Audit rows show tenant-safe organization and actor names where retained associations exist; historical deleted actors retain an explicit `用户 #ID` fallback. |
| Runtime and topology | Runtime reported the exact release/environment and healthy dependencies. Restricted topology returned redacted nodes and edges; notification summaries are an operational projection of the Notification Center ledger, not a second event store. |

No supplier secret, channel credential allowlist, financial record or
production setting was changed during these checks.

## Real-browser acceptance

Playwright drove a real Chromium browser at 1440 x 1000 against the public
development target. All 24 authenticated platform-admin routes resolved to
their expected paths:

```text
/, /developer, /channels, /channel-keys, /keys, /limit-policies,
/guardrail-policies, /logs, /usage-analytics, /notifications, /test,
/wallet, /model-prices, /org-model-prices, /reconciliation, /grants,
/users, /audit-logs, /data-exchange, /license, /status, /settings,
/access, /docs
```

Result: 24/24 rendered, no visible loading spinner, no error boundary, no
horizontal overflow, no captured 4xx/5xx resource request and no console
error. At this viewport, the sidebar ended and main content began at x=252;
page content began at x=280, which is the intended 28-pixel padding rather
than an unexplained empty gutter.

Focused UI readback verified:

- access-key one-time-secret guidance, creation time and explicit edit/
  enable/disable actions;
- upstream credential secret-preserving edits and model-scope guidance;
- readable notification resource/delivery labels;
- model pricing in CNY per million tokens and reconciliation explanations;
- user identity source fields with no false password-edit field;
- audit organization/actor names, date-time range picker and wrapped detail
  drawer without table overflow;
- real restricted topology, event/runtime distinction and active License;
- honest content-safety text that does not claim a real detector run.

A separate real developer session reached `/developer`, displayed project and
personal requests/tokens/cost/error-rate data, and could access `scope=my`
usage. Direct `/api/v1/users` access returned `403`, and platform navigation
such as User Management and License was absent. This verifies that ordinary
users already receive a restricted usage console rather than the administrator
workspace.

Ignored local screenshots:

- `output/playwright/v1-first-release-a0a567d-audit.png`;
- `output/playwright/v1-first-release-a0a567d-keys.png`;
- `output/playwright/v1-first-release-a0a567d-license.png`.

Browser local/session storage and cookies were cleared and all browser sessions
were closed. Temporary QA user `13` and developer user `14` both read back as
`disabled`; retaining the rows preserves audit-name association.

## Final issue disposition

| Issue | Final status | Owner / next action |
|---|---|---|
| `V1-FIRST-RELEASE-UX-001` | **Target verified / closed** | UniRouter: preserve required CI and target regressions |
| `V1-TARGET-ROUTING-001` | **Open — target environment configuration** | Target owner must approve exact supplier model-scope normalization; any paid call needs explicit model/cost authorization |
| `V1-TARGET-PILOT-001` | **Open — target/Production evidence** | Separately approve real provider, external detector, enterprise IdP and customer backup/restore/upgrade scenarios |
| `V1-XI-LEDGER-001` | **Coordination pending** | X-Insight owner must acknowledge and jointly read back the v2 `cost_micros` contract |

## Explicit non-claims

1. No real or paid upstream provider request was made.
2. No real external detector or real customer content was used.
3. No real enterprise OIDC/HR/SCIM/Feishu directory integration was run.
4. HTTPS/customer DNS, multi-instance capacity/HA, off-host encrypted backup,
   migration-down and Production operational gates were not tested.
5. Test signing keys and a test License do not substitute for production key
   custody, customer issuance, notarized tooling or a support/release process.
6. X-Insight v2 consumer acceptance remains external coordination work.
7. PRD7 and Production evidence remain future decisions; PRD6 remains closed.
