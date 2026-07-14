# V1 First Release Product Audit — Local And Existing Target Evidence

**Date:** 2026-07-15

**Baseline:** `main` / `origin/main` at `c051e0abe2a02382cefc8a77b04a9bdcc45828a1`

**Working branch:** `codex/v1-release-ui-target-closeout`

**Status:** existing development target audited; code/document fixes are Local verified; exact-head CI and deployment of this batch remain later evidence gates

**Scope boundary:** first-release defect and unreasonable-UX correction only. This batch does not add a vendor discovery service, directory synchronization, Update Center, new menu product, new X-Insight behavior, or a new PRD. PRD6 remains `Completed (Accepted Scope)` and is not reopened.

## Conclusion

The inspected V1 is not a static mock. Its principal access-key, routing-preview,
guardrail, notification, finance, audit, role-specific console, License and
signed-upgrade paths have real API/database/runtime implementations. The audit
also found several first-release defects or misleading presentations that are
safe to correct without expanding product scope:

1. upstream credential model scope could not be maintained from the UI without
   rotating the secret, and the current ECS values include a Chinese comma that
   the runtime matcher does not interpret;
2. access-credential and channel pagination controls did not consistently send
   the selected page size;
3. access-credential creation time was present in the API but absent from its
   table, while icon-only actions obscured create/enable behavior;
4. audit logs exposed numeric identities and wide inline JSON instead of useful
   names, a date-time picker and a readable detail surface;
5. the users API/UI hid existing OIDC email/provider fields;
6. notifications exposed internal resource codes and an ambiguous
   `not_configured` delivery label;
7. release UI and contributor documentation retained PRD-era or obsolete
   workflow labels.

No paid provider request, external detector request, real IdP login, target
credential rotation, target finance mutation or production action was used to
reach these conclusions.

## Classification Vocabulary

- **confirmed bug/regression:** implemented behavior contradicts its UI/API
  contract or prevents a supported action;
- **UI/UX improvement:** underlying function is correct but presentation makes
  it hard to understand or operate safely;
- **target environment configuration:** code is working but deployed records,
  credentials, IdP or capacity are not configured for the intended scenario;
- **X-Insight joint integration:** completion depends on the separate consumer
  and cannot be proven by UniRouter alone;
- **misunderstanding or no change:** the observed behavior is intentional or the
  requested capability already exists; explanatory evidence is sufficient.

## Complete Issue Matrix — Product And Operations

| ID / user item | Classification | Impact and cause | Priority / owner | Resolution | Acceptance evidence |
|---|---|---|---|---|---|
| V1-AUDIT-01 / 1. PRD1-era docs and standards | confirmed bug/regression | `CONTRIBUTING.md` required an obsolete Codex→Canary→Sentinel chain, linked a non-existent repository `AGENTS.md`, used a watch-oriented frontend command, and described coverage and ignored errors as absolute merge rules. README also used the old `UniRoute` title, a case-unsafe clone directory, removed `web/*` directories and a visible `PRD1 UI` source label. This could make a contributor follow a workflow that contradicts the repository or current CI. | P1 / repository owner | Align contributing guidance with the single execution owner, `codex/` branch default, exact one-shot tests, Change Routing and required CI; fix README product/path/tree/stale phase facts. Historical PRD documents remain historical and are not rewritten. | Source diff in `README.md` and `CONTRIBUTING.md`; protocol authority is `docs/agent-workflows/AGENT-PROTOCOL.md`; CI authority is `.github/workflows/ci.yml`; the corrected README paths were checked against the tree. |
| V1-AUDIT-02 / 2. 上游通道路由预览是否真实 | target environment configuration | The endpoint executes the live routing-policy snapshot and returns candidates/exclusions without credentials or an upstream call. On the current target every configured catalog model returned zero candidates because model names do not match the three active upstream-key allowlists. One allowlist also contains a Chinese comma. The preview is real; the current target is not ready for those model names. | P1 / target operator; UniRouter owns safer editing | Keep the read-only preview. Expose upstream credential model scope/status in the UI, allow scope edits without rotating the secret, normalize Chinese/ASCII comma and newlines on future writes, and validate state/channel IDs. Do not rewrite current target records or silently enable paid routing. | Authenticated target `GET /api/v1/routing/preview` returned 200 with live deterministic snapshots and no candidate for all current models; `GET /api/v1/channel-keys` showed three active records and the mismatched allowlists. Backend/frontend regressions cover normalization and secret-preserving edits. |
| V1-AUDIT-03 / 3. 访问凭证只有修改和禁用 | misunderstanding or no change | Creation exists as `新增访问凭证`; disabled credentials can be enabled again. Hard deletion is intentionally absent because revocation plus retained identity/audit history is safer than erasing an issued credential. Icon-only actions made the existing behavior easy to miss. | P2 / UniRouter UI | Keep create/edit/disable/enable and no hard delete. Add explicit `编辑`、`停用`、`启用` text and accessible names. | Target created credential ID 38, authenticated `/v1/models` successfully, disabled it, then received 401 `auth_error.api_key_disabled`; UI/API tests cover create/update/disable contracts. |
| V1-AUDIT-04 / 4. Key prefix rule and creation time | UI/UX improvement | A caller key is `ur-{org id modulo 10000, four digits}-{32 random hex}`. Only the first 12 characters are stored as a non-secret display prefix; the full key is authenticated with HMAC and shown only once. The prefix is an identifier, not a security factor and not guaranteed globally unique by itself. `created_at` already existed in the API but was omitted from the admin table. | P1 / UniRouter | Correct the misleading source comment; explain one-time full-key/prefix semantics; display `创建时间`. | `internal/auth/auth.go`, key handler tests, target lifecycle evidence, and the `KeysPage` created-time column. |
| V1-AUDIT-05 / 5. 访问凭证页面整体可用性 | confirmed bug/regression | Core lifecycle, IP/model scope, RPM/RPS and policy labels are implemented. The page-size selector always requested 20 rows; edit form initialization relied on a timer; raw English status and icon-only actions reduced clarity. | P1 / UniRouter | Pass the selected page size, hydrate forms from state, localize status, add explicit actions and clarify the one-time secret. No new credential type is introduced. | Focused `KeysPage` tests assert exact API arguments, quota vocabulary and accessible actions; target real create/auth/disable path passed. |
| V1-AUDIT-06 / 6. 内容安全是否真实 | misunderstanding or no change | Guardrail policies are persisted, versioned and included in a runtime snapshot; they are not a decorative page. This audit did not send customer content through a real external detector or paid provider, so it does not prove detector quality or customer-traffic enforcement on ECS. | P1 for future target evidence / UniRouter + target operator | No product expansion in this batch. Retain the policy kernel and require a separately approved sanitized detector/request scenario before claiming real-detector or Production verification. | Target started with zero policies, created shadow policy ID 1, read the changed runtime snapshot, changed it to disabled version 2 and read the new snapshot. Both approved local detector definitions were present. |
| V1-AUDIT-07 / 7. 模型治理、PRD2 与厂商自动适配 | UI/UX improvement | CRUD/catalog/alias/governance enforcement are real. Visible `PRD2`/`PRD1` wording is release-inappropriate and is removed. Model metadata is currently operator-managed; there is no universal vendor metadata standard, and automatic vendor discovery/import would require provider-specific adapters, provenance, review and removal semantics. | P1 for stale labels; P2 candidate / UniRouter product | Remove phase labels now. Do not silently add auto-discovery: treat provider catalog import as a separately accepted feature/backlog item. | Target model-governance API/page returned real records; source removes phase labels. Existing pricing/governance tests remain authoritative. |
| V1-AUDIT-08 / 8. 通知中心语义 | UI/UX improvement | `api_key:28` is an internal resource type and row ID. `not_configured` means the event was persisted but no matching enabled Webhook route existed; it does not mean the event was lost. Calling the page `系统治理` and exposing internal codes made correct behavior look broken. | P1 / UniRouter | Display `访问凭证 #28`, localize known API-key events, rename delivery state to `未配置 Webhook`, explain event-versus-delivery semantics, and classify the page as 流量治理. | Target: six events, zero deliveries and zero Webhook routes; source label regressions plus notification page tests/full frontend suite. |
| V1-AUDIT-09 / 9. 资金账户业务逻辑 | misunderstanding or no change | The page represents CNY micro-units, separates cash/grant/frozen balances, and supports top-up, signed delta adjustment, organization selection and transaction history. Earlier absolute-balance race and sub-cent truncation defects were already closed by PR #196 and PR #202. This audit performed readback only, so it does not claim a new target finance mutation. | P0 history closed; P2 UX / billing owner | Keep atomic/idempotent delta and micro-CNY authority. Correct the page taxonomy to 财务治理. Do not invent refunds/invoicing/accounting features in this batch. | Target wallet API 200; 302 transaction rows with settle/freeze types; exact-money target evidence is `V1-POST-PRD6-MICRO-PRECISION-LEDGER-TARGET-20260714.md`. |
| V1-AUDIT-10 / 10. 模型定价单位 | misunderstanding or no change | Current V1 already uses `CNY / million tokens` for input/output, matching common supplier publication practice. The old per-thousand contract was fixed by PR #197; changing it again would regress the public contract. | P0 history closed / billing owner | No pricing-unit change. Keep exact decimal CNY/million-token fields and rename the page taxonomy to 财务治理. | Target listed 11 prices using canonical `*_cny_per_million_tokens`; pricing/micro-ledger CI and target evidence remain linked from current status. |
| V1-AUDIT-11 / 11. 用量对账与估算占比 0 | misunderstanding or no change | The target currently has 188 rows whose authoritative usage source is upstream; therefore estimated ratio is correctly 0, not a constant or division bug. It showed actual total 10,880 and variance 7,433. Earlier local evidence includes mixed data with a non-zero estimated ratio. | P2 / billing and metering | No calculation change. Keep source/variance/confidence explanations and correct taxonomy to 财务治理. A future provider run should verify mixed-source behavior with controlled data. | Target API and UI readback; existing reconciliation tests; prior local mixed-source evidence reports 33.3% estimated ratio. |

## Complete Issue Matrix — Identity, Release And Information Architecture

| ID / user item | Classification | Impact and cause | Priority / owner | Resolution | Acceptance evidence |
|---|---|---|---|---|---|
| V1-AUDIT-12 / 12. 用户管理与企业账号体系 | confirmed bug/regression | UniRouter already has generic OIDC login/mapping and DB fields for email, external subject and auth provider, but the users list API/UI returned only local-style identity. The edit UI also promised a password change that the update API never implemented, so the value was silently ignored. LDAP/AD direct binding, SCIM, HR and Feishu directory synchronization are distinct unimplemented integrations. | P1 / UniRouter identity; real IdP is target operator | Return/display existing email and local/OIDC source, preserve tenant scoping, fix the page-size request, label creation as local-user creation, and limit edit UI/payload to the actually supported role/status fields. Validate status values. Do not add a second standalone account system or new directory sync in this batch. | Source joins existing migrated OIDC fields; backend and `UsersPage` tests cover scan, status and enterprise-identity/edit contracts. A real enterprise IdP login remains target evidence, not inferred from code. |
| V1-AUDIT-13 / 13. 审计日志可读性 | confirmed bug/regression | Numeric organization and `User #1` values are technically traceable but operationally poor; inline no-wrap JSON forced excessive horizontal scrolling; RFC3339 text inputs were error-prone. | P1 / UniRouter | Join tenant-safe organization/project/actor display names, retain IDs, use a date-time range picker, move wrapped detail/before/after JSON to a side sheet and reduce table width. | Backend mock tests assert readable identities and existing tenant filters; focused frontend tests pass. Existing target audit API had 164 rows and included this audit’s test mutations. |
| V1-AUDIT-14 / 14. License generation/control/anti-crack | misunderstanding or no change | V1 now uses vendor-held Ed25519 signing keys, embedded public trust roots, deployment/time/feature/resource claims and fail-closed model traffic. It is materially stronger and more cloud-stable than MAC-address-only binding. No local software can be mathematically unpatchable for a customer with root access or source; source-free digest images, contracts, audit and controlled updates are part of the control. | P0 / release authority | Keep the commercial gate. Generate and sign through the double-click `UniRouter License Studio.app`; keep private keys off customer/ECS/Git. Do not claim “unbreakable.” | PR #205/#206 merge and CI evidence; current development target reports active test License `lic_unirouter_devtest_20260715`, valid signature/deployment binding and enforced admission. Runtime trust-root override was rejected. |
| V1-AUDIT-15A / 15. 内部状态、受限拓扑、通知事件 | UI/UX improvement | Internal Status aggregates open notification events; Runtime Status separately probes process/dependencies/monitoring. The restricted topology is a redacted logical node/edge graph, not a geographic/network map. Its notification list is a compact operational projection of the same event ledger; Notification Center owns filters, Webhook routes and deliveries. The overlap is intentional but was insufficiently explained. | P1 / UniRouter | Keep both views; remove stale PRD wording, use readable resources and retain explicit descriptions of event/runtime/delivery semantics. Do not infer multi-instance network inventory. | Target runtime showed application, PostgreSQL and Redis nodes/edges and operational dependency health; Internal Status warning matched open notification events. |
| V1-AUDIT-15B / 15. Patch/upgrade experience | misunderstanding or no change | Signed, deterministic offline upgrade verification, provenance/digest/schema/sequence preflight, installed-state recording and replay rejection exist. A click-to-upgrade Update Center, arbitrary upload/apply UI and general N-1 automatic rollback do not exist; adding them is a high-impact release-orchestrator feature, not a release bug fix. | P0 signing foundation closed; P2 future scope / release engineering | Preserve signed package foundation and customer Compose. Keep Update Center/automatic apply in PRD7 candidate scope until separately accepted. | Current target verified a target-specific preloaded-digest bundle, recorded sequence/state and rejected replay; this is not a production customer bundle or automatic update claim. |
| V1-AUDIT-16 / 16. 左侧菜单层级 | UI/UX improvement | Platform admin already has five coherent groups: 概览、流量治理、财务治理、访问控制、系统治理; other roles receive smaller dedicated menus. A third level would add navigation cost without a demonstrated task hierarchy. Several page eyebrow labels were inconsistent with their actual group. | P1 / UniRouter UI | Keep two-level navigation and role-aware reduction. Align affected page eyebrows; do not add a new information architecture in a bug-fix release. | `frontend/src/navigation.tsx` plus navigation tests; all 24 authenticated routes rendered on the target. |
| V1-AUDIT-17 / 17. 右侧内容左侧留空 | misunderstanding or no change | At the audited 1440px viewport, the sidebar occupies x=0–252 and main content begins at x=252, with no additional empty gutter or horizontal overflow. The perceived whitespace comes from deliberate content max-width/padding, not a detached layout. | P3 / UI | No global layout shift without evidence at the customer’s actual viewport. Recheck desktop/mobile after this batch deployment. | Browser geometry: main content x=252, no horizontal overflow across the 24-route audit. |
| V1-AUDIT-18 / 18. 普通用户使用情况 | misunderstanding or no change | Developer/project users already receive a restricted project console with project/personal usage, keys, models and recent calls; admin/finance/user-management routes are forbidden. | P1 evidence / UniRouter | No new dashboard. Preserve role-specific navigation and authorization. | Real target developer login: `/me` 200 with one project, personal keys/usage 200, `/users` and `/wallet` 403; UI showed usage tiles, models and recent calls. |
| V1-AUDIT-19 / 19. 防止用户用 AI 重构源码 | misunderstanding or no change | If customers receive source or control the host as root, AI lowers effort but does not change the fundamental impossibility of preventing binary patching. Obfuscation or MAC binding alone cannot solve this. | P0 commercial delivery / release authority | Do not ship source, build context/cache or signing keys; require six exact digest images, embedded trust roots, signed License/upgrades, audit and contractual/support controls. | Customer Compose contains no build/source mounts and requires exact digests; Docker-context and commercial-release CI evidence is linked from current status. |
| V1-AUDIT-20 / 20. 其他发布建议 | target environment configuration | Remaining release evidence is environmental, not hidden code completion: HTTPS/customer DNS, real provider capacity and controlled cost, real detector, enterprise OIDC, customer backup/restore/upgrade drill, production signing ceremony, platform packaging/notarization and X-Insight v2 acceptance. The historical provider load row also shows current keys cannot support a 20-VU/60s claim. | P1 before broad production claims / target owner + release engineering; X-Insight owns its consumer | Keep these as explicit target/production gates. Do not market them as verified or add new product scope inside this batch. | `OPEN-ISSUES.md`, PRD7 candidate backlog and the target/non-claim sections below. |
| V1-AUDIT-XI / existing Post-PRD6 joint item | X-Insight joint integration | UniRouter now emits the v2 monetary evidence contract with `cost_micros` and CNY scale metadata, but UniRouter-only source, CI or ECS checks cannot prove that X-Insight consumes, persists and reads it correctly. | P1 / X-Insight consumer owner with UniRouter contract support | Make no additional UniRouter feature in this batch. Require X-Insight acknowledgement plus a joint sanitized exchange/import/readback receipt before `External integration verified`. | `V1-XI-LEDGER-001` remains `Coordination pending`; no joint v2 receipt exists yet. |

## Existing Development Target Evidence

The current non-production ECS target was read and exercised before deploying
this batch:

- stable/canary/backend and frontend release commit:
  `c051e0abe2a02382cefc8a77b04a9bdcc45828a1`;
- product version `1.0.0`, environment `development`, migrations through
  `000037`;
- public/stable/canary readiness returned healthy PostgreSQL and Redis;
- active signed test License was deployment-bound and enforceable;
- all 24 authenticated admin routes returned HTTP 200 with zero captured
  console, page, request or resource-load errors;
- the current target baseline showed no horizontal overflow at 1440px;
- screenshots are local ignored browser artifacts and contain no raw secrets.

The browser run used temporary QA/developer accounts and one temporary caller
credential. Raw secrets were not written to this repository or evidence. Final
target closeout must disable/remove those temporary actors and clear browser
session state after the post-deployment verification.

## Verification For This Local Node

Focused verification:

```text
go test ./internal/admin ./internal/auth -count=1
npm test -- --run \
  src/pages/__tests__/ChannelsPage.test.tsx \
  src/pages/__tests__/ChannelKeysPage.test.tsx \
  src/pages/__tests__/KeysPage.test.tsx \
  src/pages/__tests__/UsersPage.test.tsx \
  src/pages/__tests__/AuditLogsPage.test.tsx \
  src/pages/__tests__/DashboardPage.test.tsx
```

Result: backend packages PASS; 6 frontend files / 18 tests PASS. The frontend
output retains existing React Router/Semi `findDOMNode` and form warnings; no
test failed.

Full verification:

```text
go test ./... -count=1                                      PASS
go vet ./...                                                PASS
go build ./...                                              PASS
REQUIRE_TEST_DATABASE=1 go test -tags=integration \
  ./internal/admin -count=1                                PASS
npm test                                                    PASS (33 files / 167 tests)
npm run type-check                                          PASS
npm run build                                               PASS
git diff --check                                            PASS
```

The migrated admin integration suite left zero `uniroute_it_*` temporary
databases. The production frontend build retains the existing >500 kB chunk
warning; it is a performance backlog signal, not a build failure introduced by
this batch.

`CI verified` and deployment of these fixes must be recorded only after the
exact PR head passes required checks and the merged `main` commit is deployed.

## Explicit Non-Claims And Decision Gates

1. No real or paid upstream invocation was made in this audit.
2. No real customer content or external detector was invoked.
3. No real enterprise IdP login or directory synchronization was performed.
4. No current ECS upstream allowlist, supplier credential or financial record
   was changed.
5. This is not Production, TLS, capacity/HA, migration-down or customer backup
   and restore evidence.
6. X-Insight v2 consumer acceptance remains joint work and cannot be closed by
   this repository.
7. Update Center, automatic upgrade apply, vendor catalog discovery and
   HR/SCIM/Feishu sync remain unaccepted new features, not silently added fixes.
