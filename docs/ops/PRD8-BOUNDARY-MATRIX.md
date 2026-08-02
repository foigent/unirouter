# PRD8 Product Boundary Matrix

> Date: 2026-08-02
> Status: Product ownership frozen; local UI/architecture and bounded runtime-control implementation verified
> Runtime boundary: this product contract alone authorizes no deployment, live
> integration, credential, traffic, production DB, Cursor, or Checkpoint
> mutation. PR/CI and release to the existing UniRouter environment were
> separately authorized by the Product Owner on 2026-08-02.

## 1. Reading Rules

This matrix is the authoritative disposition of PRD7 rows for forward product
planning. It does not rewrite PRD7 evidence:

- `Local verified` remains true only for the exact repository-local artifact and
  test boundary recorded by PRD7.
- `V8 disposition` decides the future owner, UI host and investment path.
- A row may be implemented locally and still be `offline_lab_only`,
  `shared_contract_evidence` or `external_owner` as a product classification.
- The 2026-08-01 Product Owner authorization permits the reversible local UI
  and architecture migration. The 2026-08-02 authorization separately permits
  PR/CI and an evidence-backed release to the existing UniRouter environment;
  it still does not permit live directory/ERP integration, new credentials,
  Provider test traffic or destructive route/API removal.

## 2. PRD7 → PRD8 Disposition

| ID | Existing PRD7 asset/evidence | V8 classification | Fact / analysis / enforcement owner | Forward product and UI disposition |
|---|---|---|---|---|
| P7-F01 | Execution Decision Receipt Workspace V0, Local verified | `product_core`; future handoff is `cross_product_integration` + `not_implemented` | UniRouter / X-Insight for future cross-execution analysis / UniRouter | Keep the single model-execution Receipt and persistent operational Inspector. Cases, Findings, Evidence Packs and cross-execution investigation live in X-Insight; a controlled contextual handoff is a future integration, not a current capability. |
| P7-F02 | Contextual inspectors, browser-local views and deep links, Local verified | `product_local_supporting_component` | Each product / each product / n/a | Keep as a UniRouter UX pattern for UniRouter facts, not a separately sold capability. X-Insight owns investigation saved views and Evidence deep links. |
| P7-F03 | Provider Conformance Pack V1 and three synthetic packs, Local verified | `product_core`; `target_or_runtime_unverified` for real-provider claims | UniRouter / UniRouter / UniRouter | Keep under upstream channels/model governance. It is a model-adapter contract, not a general Connector or plugin framework. |
| P7-F04 | Immutable Provider Pack diff plus exact Provider Pack/Manifest pins at validation and every forward transition, Local verified | `product_core`; offline comparison remains `offline_lab_only` | UniRouter / X-Insight for historical impact / UniRouter | Keep offline Pack/config diff and the product-local activation gate in Model Governance → Change & Release. Pin drift and breaking simulation fail closed. Cross-run drift/impact analysis remains in X-Insight. |
| P7-F05 | Default-off Outbox-posted OTel/OpenInference exporter, Local verified | `cross_product_integration`; `target_or_runtime_unverified` | UniRouter / receiver product / UniRouter source export policy | Keep Source-side metadata export. Receiver normalization, storage and analysis are not UniRouter capabilities. |
| P7-F06 | Backend-neutral secret reference and fixture lifecycle, Local verified | `product_local_supporting_component`; `target_or_runtime_unverified` | UniRouter for model-provider refs / n/a / UniRouter | Limit to UniRouter Provider and UniRouter-owned Connector secrets. Tool credentials belong to Tool Gateway; MCP OAuth/session credentials belong to MCP Gateway. Do not present a production rotation workflow until a real backend is selected. |
| P7-F07 | Exact response-cache service/Redis fixture, Local verified but not Gateway-served | `product_local_supporting_component`; `target_or_runtime_unverified` | UniRouter / UniRouter / UniRouter | Keep only exact model-response caching and its billing semantics. Tool-result, MCP-resource and X-Insight query caches stay with their owners. |
| P7-F08 | Responses/rerank/image Endpoint Profiles; GPT Image local runtime | `product_core`; partial `target_or_runtime_unverified` | UniRouter / UniRouter / UniRouter | Keep model endpoint protocols. Tool Schema/execution belongs to Tool Gateway; MCP methods/resources/prompts/tools belong to MCP Gateway. Endpoint Profiles cannot become a dynamic plugin registry. |
| P7-F09 | Provider COGS offline import and Financial Truth preview, Local verified | `product_core` + `cross_product_integration`; real import unverified | Provider/supplier owns raw billing facts; UniRouter owns captured evidence, customer ledger and reconciliation record / X-Insight for trends/anomalies / UniRouter financial PEP | Rename UI to Billing & Reconciliation. UniRouter does not become the source of supplier truth; it preserves captured evidence and owns its customer ledger and final reconciliation record. X-Insight may analyze drivers/gaps but never creates debt or a second ledger. |
| P7-F10 | Shared routing evaluator, offline simulator and bounded runtime release controller, Local verified | `product_core`; offline simulation remains `offline_lab_only` | UniRouter / X-Insight for historical cohort analysis / UniRouter | Model Governance now owns draft/validate/shadow/canary/promote/rollback for channel admission, priority and weight only, with deterministic assignment, 50% cap, one canary per model and emergency-disable precedence. Historical counterfactual/long-running analysis remains in X-Insight. |
| P7-F11 | Member lifecycle fixture plus real `auth_epoch`/session-revoke changes, Local verified | `product_core` for local membership; `offline_lab_only` for directory integration | UniRouter local IAM / n/a / UniRouter | Merge into Organization & Members. Official organization/person fields remain external-source projections; UniRouter owns account binding and project membership. No Tool/MCP Audience or enterprise-wide IAM claim. |
| P7-F12 | Runtime-backed Model Call Authorization plus separately retained metadata-only Agent Trust evidence, Local verified | `product_core` after narrowing; historical trust fixture remains `offline_lab_only` | UniRouter for model-call authority / X-Insight for authority-risk analysis / UniRouter model PEP | Model Call Authorization now narrows API-key/project/user/agent access by environment, model and endpoint with deny precedence, allow intersection, validity, revision and revoke. Tool permission belongs to Tool Gateway; MCP trust/session belongs to MCP Gateway; cross-product drift/coalition hypotheses remain in X-Insight. |
| P7-F13 | Fixture-only signed metadata decision Receipt, Local verified | `product_local_supporting_component`; runtime trust unverified | UniRouter / X-Insight may verify/cite / UniRouter | Keep as part of UniRouter Execution Receipt, not a standalone workspace. Other products sign their own decisions with separate audiences and keys. |
| P7-F14 | Synthetic Governance Digital Twin wrapper over F10 evaluator, Local verified | `offline_lab_only`; independent product feature retired | UniRouter Source config / X-Insight for cross-history/system analysis / UniRouter for model promotion | Retire the broad name. Keep only Route & Policy Change Preview inside F10; the wider twin/impact-analysis product boundary is X-Insight. |
| P7-F15 | Exact Source provenance/gap passport offline projection, Local verified | `cross_product_integration` + `shared_contract_evidence` | UniRouter facts / X-Insight projection/investigation / n/a | Remove standalone UniRouter Passport navigation. Keep local provenance/gaps inside a Receipt; shared protocol owns wire semantics; X-Insight owns cross-source Passport/Evidence Pack. |
| P7-F16 | Pinned LiteLLM second-Connector artifacts and cross-language conformance, Local verified | `shared_contract_evidence` + `external_owner` | Actual source owner / X-Insight projection / actual source runtime | Remove from UniRouter product scope and UI. UniRouter may pin protocol compatibility but must not host a generic third-party Connector runtime. |
| P7-F17 | Offline risk advice/shadow with no real risk input or enforcement, Local verified | `cross_product_integration` + `offline_lab_only`; downscope/canary/enforcement `not_implemented` | UniRouter Source facts / X-Insight or bounded Value Plugin / UniRouter model PEP | Remove standalone Risk & Control navigation. Current evidence ends at offline scoped/TTL validation and shadow. Future downscope/canary/final routing belongs to UniRouter but is not implemented; X-Insight owns evidence-linked risk discovery and reasons. |
| P7-F18 | Offline Evidence-to-Control draft with human-review stop, Local verified | `cross_product_integration` + `offline_lab_only`; inbox/accept/rollout `not_implemented` | Each source owner / X-Insight proposal generation / target action owner | Current evidence ends at an offline draft, simulation and human-review stop. X-Insight owns future Finding→proposal generation; UniRouter owns the future model-policy inbox, accept/reject, rollout and rollback, none of which is currently implemented. Tool/MCP owners process their own proposals. |
| P7-F19 | Repository-local single-image GPT Image V1 async job plane, Local verified; no target/provider/object-store proof | `product_core`; `target_or_runtime_unverified` | UniRouter / n/a for job-local state; X-Insight for future cross-job analysis / UniRouter | Rename Image Jobs to Model Jobs. GPT Image V1 is one job type; do not imply image understanding/editing, audio, video, tool pipelines or arbitrary external side effects. |

## 3. Current Route Disposition

| Current route | Current evidence type | PRD8 forward state |
|---|---|---|
| `/governance-simulation` | offline preview plus runtime-backed bounded release workspace | Merge into Model Governance → Change & Release; preserve compatibility redirect and keep offline evidence separate from runtime lifecycle |
| `/risk-control` | offline preview | Remove from main navigation; future model proposal inbox/risk summary only |
| `/organization-members` | composed local account/project workspace plus Local-verified source-neutral directory projection and IAM convergence | Canonical Organization & Members workspace; defaults to current local-account management; real directory Connector remains POC-gated |
| `/member-lifecycle` | offline contract plus local auth changes | Compatibility redirect to the lifecycle tab inside Organization & Members |
| `/users` | runtime-backed local/OIDC accounts | Compatibility redirect to the default account tab inside Organization & Members |
| `/project-management` | runtime-backed projects and membership | Compatibility redirect to the project tab inside Organization & Members |
| `/agent-trust` | runtime-backed Model Call Authorization only; auxiliary `/agent-trust-evidence` holds historical fixture validation | Keep the primary authorization flow free of offline evidence; never expose Tool/MCP authority or relabel the historical fixture as runtime evidence |
| `/provenance-passport` | exact-Source offline projection | Remove from main navigation; keep Receipt facts and X-Insight integration link |
| `/wallet` | runtime settlement wallet plus Local-verified fiscal periods, cost centers and report-only enterprise budgets | Budgets & Cost Centers explicitly separates compatibility settlement from enterprise planning; budgets do not become request admission policy |
| `/model-prices` | runtime base prices/multipliers plus Local-verified five-layer enterprise Rate Cards and snapshot reads | Rates & Rating retains compatibility APIs and request-path pricing; enterprise Rate Cards are not activated for settlement |
| `/reconciliation` | request reconciliation and supplier preview plus Local-verified allocation, close and adjustment evidence | Allocation & Reconciliation keeps customer debt/ledger mutation at none and ERP/GL export unconfigured; do not claim global financial truth |
| `/media-jobs` | real repository-local Image V1 code, target unverified | Rename Model Jobs and place in the model-execution domain |
| `/data-exchange` | UniRouter-owned integration control | Keep; no embedded X-Insight investigation UI or shared runtime |

## 4. Compatibility And Non-Claims

The boundary freeze itself changed no current bytes or behavior. The authorized
local migration and bounded runtime controls intentionally add new management
APIs/tables and narrow route-selection/model-call decisions only in their owned
surfaces, while preserving these invariants:

- PRD7 evidence files remain unchanged.
- Navigation labels and page composition changed, while removed duplicate
  entries remain reachable through compatibility redirects. Migrations
  `000047`–`000053` and their product-local management APIs are additive.
  Runtime release may only override channel admission/priority/weight and Model
  Call Authorization may only narrow existing authority; provider credentials,
  endpoint configuration, request DTOs, pricing/settlement, Outbox, Feed,
  Source hash, Cursor and Checkpoint identities are unchanged.
- No X-Insight, Tool Gateway, MCP Gateway or shared-protocol repository was
  modified.
- No test result in PRD7 is promoted above its recorded evidence level.
- No Tool Gateway or MCP Gateway implementation is claimed.
- The source-neutral `ORG-1`/`ORG-2`, `FIN-1` through `FIN-3`, bounded runtime
  release and Model Call Authorization additions are Local verified. They
  select no real directory/ERP target and create no cross-product runtime.

The UI/route migration is now authorized and is governed by
`docs/architecture/PRD8-ENTERPRISE-ORG-FINANCE-UI-MIGRATION-20260801.md`.
No API, ledger, Feed or runtime behavior may be silently redefined by a label.
