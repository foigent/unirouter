# PRD7 Acceptance Matrix

> Date: 2026-07-31
> Status: Accepted repository-local scope; implementation in progress
> Runtime boundary: no deployment, traffic, credentials, external calls, push,
> PR or merge is authorized by this matrix

## Evidence Levels

| Level | Meaning |
|-------|---------|
| Accepted | Product and repository-local implementation scope is authorized |
| Local verified | Exact repository implementation passed listed local tests and evidence review |
| CI verified | Required CI passed for the exact code revision |
| Target verified | An explicitly authorized non-production target was observed |
| External integration verified | Another owner independently accepted its contract |
| Production verified | Explicit production evidence exists |

## Active Rows

| ID | Requirement | Priority | Current result | Local acceptance |
|----|-------------|----------|----------------|------------------|
| P7-F01 | Execution Decision Receipt Workspace V0 | P0 | Local verified at `e361964` | persistent no-click Requests inspector; settled/cancelled fail-closed facts; responsive UI; evidence `UR-EXECUTION-DECISION-RECEIPT-V0-LOCAL-20260731.md` |
| P7-F02 | Persistent contextual inspector, saved views and deep links across Requests, Audit, Providers and Projects | P0 | Local verified across all four surfaces | auto-select without a second detail click; URL-restorable selection/filter; keyboard and narrow-width behavior; Provider evidence `PRD7-B1-P7F02-P7F03-provider-conformance-local-20260731.md`; Audit/Projects evidence `PRD7-B2-P7F02-contextual-inspectors-local-20260731.md` |
| P7-F03 | Provider Conformance Pack V1 and three core fixture-verified packs | P0 | Local verified | strict manifest and synthetic fixture validation; OpenAI, Anthropic and Azure OpenAI packs; 3 packs / 5 endpoint-auth fixtures / 20 deterministic cases; exact artifact digests; Admin API and explicit non-network claim; evidence `PRD7-B1-P7F02-P7F03-provider-conformance-local-20260731.md` |
| P7-F04 | Provider Change Firewall | P1 | Local verified through immutable local Pack snapshots | exact schema/manifest/fixture identities; SemVer and semantic add/remove/drift classification; current catalog identity case; no automatic runtime promotion; evidence `PRD7-B8-P7F04-P7F10-governance-simulation-local-20260731.md` |
| P7-F05 | OTel/OpenInference metadata export | P0 | Local verified | default-off, Outbox-posted, bounded async OTLP/HTTP exporter; OpenInference Chat/Embedding/Guardrail/Chain mapping; source facts and Feed behavior preserved; recursive raw-content and cardinality negatives; exact golden SHA; evidence `PRD7-B3-P7F05-otel-openinference-export-local-20260731.md` |
| P7-F06 | Secret references and rotation lifecycle | P0 | Local verified through backend-neutral/in-memory-fixture boundary | immutable versions; CAS stage/activate/rollback/revoke; fixed backend registry; stale-worker, concurrent-writer, outage and disclosure tests; metadata-only golden SHA; no secret readback/logging; evidence `PRD7-B4-P7F06-secret-reference-local-20260731.md` |
| P7-F07 | Exact tenant-isolated response cache | P0 | Local verified through standalone service/Redis-fixture boundary; Gateway hit serving gated by P7-D07 | tenant/project/key/auth/policy/model/pricing/region/exact-request binding; opaque HMAC index; AES-256-GCM values; strict text-only eligibility; final provider proof; TTL/generation purge; lease stampede protection; tamper/key-rotation/outage tests; hit provider request/cost zero and customer charge explicitly unpriced; evidence `PRD7-B5-P7F07-exact-response-cache-local-20260731.md` |
| P7-F08 | Responses, rerank and image protocol coverage | P1 | Local verified through zero-network endpoint-contract boundary | fixed Responses text, Cohere Rerank V2 and GPT Image profiles; strict request/terminal/unit semantics; 3 immutable manifests/fixtures and 25-case deterministic report; unsupported semantics fail explicitly; image runtime remains P7-D03-gated and all observations are non-customer-chargeable; evidence `PRD7-B6-P7F08-endpoint-protocol-conformance-local-20260731.md` |
| P7-F09 | Provider COGS connector and financial-truth workspace | P0 | Local verified through zero-persistence offline/synthetic boundary | supplier/customer/estimate authorities separated; request attribution requires one unique upstream match; aggregates never create customer debt; exact decimals, pagination, semantic replay, fixed gaps, duplicate/conflict fixtures; 2 fixtures/15 cases; no-store API/CLI and persistent auto-selected Financial Truth inspector; evidence `PRD7-B7-P7F09-provider-cogs-financial-truth-local-20260731.md` |
| P7-F10 | Offline route/policy simulator and progressive release | P0 | Local verified through zero-persistence synthetic boundary | Gateway and simulator share one credential-free evaluator; immutable route/policy/Pack snapshots; deterministic diff; shadow/canary/enforced evaluation modes; CAS progression, rollback, blocked-change non-advancement and audit; 1 fixture/17 cases; platform-only no-store API and persistent auto-selected Governance Inspector; runtime promotion remains external-gated; evidence `PRD7-B8-P7F04-P7F10-governance-simulation-local-20260731.md` |
| P7-F11 | Project member lifecycle and directory provisioning contract | P1 | Local verified through zero-network lifecycle/synthetic boundary | IdP-neutral create/update/suspend/reactivate/terminal deprovision; exact membership/capability revocation; role/status auth-epoch versioning and live-session revoke; replay/CAS; 1 fixture/17 cases; no-store API/CLI and persistent Lifecycle Inspector; SCIM vendor remains P7-D02-gated; evidence `PRD7-B9-P7F11-member-lifecycle-local-20260731.md` |
| P7-F12 | Agent Trust Firewall Console | P0 | Local verified through metadata-only synthetic boundary | current Agent limits; effective tree; ancestor lineage/budget; attenuation and TTL/revocation proof; no root secret/token; persistent inspector; evidence `PRD7-B10-P7F12-P7F13-P7F14-agent-trust-local-20260731.md` |
| P7-F13 | Signed decision receipt | P1 | Local verified with fixture-only Ed25519 trust profile | canonical metadata receipt; key/version/audience/tenant/decision/evidence binding; tamper negatives; cryptographic validity explicitly not runtime trust; same B10 evidence |
| P7-F14 | Governance Digital Twin | P0 | Local verified through direct B8 evaluator reuse | pinned B8 schema/fixture; route/policy/Pack diff and deterministic replay; no synchronous request-path dependency or runtime promotion; same B10 evidence |
| P7-F15 | Provenance and Capture-Gap Passport | P0 | Local verified through exact-Source offline boundary | exact settled Source bytes/hash/revision and current Reference identity; 9 direct/2 derived/2 declared/4 missing facts; fixed Gap provenance; Feed/DTO/page/Cursor compatibility; persistent inspector; evidence `PRD7-B11-P7F15-P7F16-provenance-passport-local-20260731.md` |
| P7-F16 | Second heterogeneous Connector | P1 | Local verified through shared-authority fixture/conformance boundary | LiteLLM manifest/mapping/compatibility/contract/vector pins; independent Go/TypeScript 20 cases/11 classes; no UniRouter runtime registration, credential, Checkpoint or vendor branch; same B11 evidence |
| P7-F17 | Risk-adjusted Routing | P1 | Local verified for replay/advice/shadow; enforcement gated by P7-D04 | exact B8/B10/B11 composition; tenant/Audience/TTL/epoch-bound categorical assertion; exact Source-local scope hash; tenant-to-Source mapping explicitly unverified; downscope-only advice; no remote synchronous dependency, runtime policy read, traffic or enforcement; 1 fixture/28 cases/32 classes; evidence `PRD7-B12-P7F17-P7F18-risk-control-local-20260731.md` |
| P7-F18 | Evidence-to-Control proposal workflow | P1 | Local verified through offline simulation and human-review boundary | owner-specific evidence-derived draft including mandatory tenant-to-Source scope review; B8 simulation reuse; no self-promotion or runtime authorization; explicit P7-D04 gate; persistent no-click inspector; same B12 evidence |
| P7-F19 | Async multimodal execution plane | P2 | Pre-gate contract Local verified; runtime decision gated by P7-D03 | strict metadata-only job-plan Schema; text-to-image and image-to-video fixtures; tenant/object/idempotency identity; exact B6 image evidence pins; 4 unresolved product gaps plus video profile gap; 9 threat classes; 55 cases/36 classes; no API, queue, lease, object, provider, callback, credential, charge or runtime mutation; evidence `PRD7-B13-P7F19-async-media-pre-gate-local-20260731.md` |

## Explicit Non-Claims

- No row is CI, target, external-integration or production verified unless its
  exact evidence is added later.
- Existing completed-execution Feed, credentials, source bytes, identity,
  endpoint, page and cursor behavior are not changed by acceptance.
- Semantic cache, managed/unified billing, plugin marketplace and universal
  high-privilege plugin execution are not included.
