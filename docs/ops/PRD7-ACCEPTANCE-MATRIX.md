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
| P7-F04 | Provider Change Firewall | P1 | Accepted | pack/version/fixture diff; breaking-risk classification; no automatic runtime promotion |
| P7-F05 | OTel/OpenInference metadata export | P0 | Local verified | default-off, Outbox-posted, bounded async OTLP/HTTP exporter; OpenInference Chat/Embedding/Guardrail/Chain mapping; source facts and Feed behavior preserved; recursive raw-content and cardinality negatives; exact golden SHA; evidence `PRD7-B3-P7F05-otel-openinference-export-local-20260731.md` |
| P7-F06 | Secret references and rotation lifecycle | P0 | Local verified through backend-neutral/in-memory-fixture boundary | immutable versions; CAS stage/activate/rollback/revoke; fixed backend registry; stale-worker, concurrent-writer, outage and disclosure tests; metadata-only golden SHA; no secret readback/logging; evidence `PRD7-B4-P7F06-secret-reference-local-20260731.md` |
| P7-F07 | Exact tenant-isolated response cache | P0 | Local verified through standalone service/Redis-fixture boundary; Gateway hit serving gated by P7-D07 | tenant/project/key/auth/policy/model/pricing/region/exact-request binding; opaque HMAC index; AES-256-GCM values; strict text-only eligibility; final provider proof; TTL/generation purge; lease stampede protection; tamper/key-rotation/outage tests; hit provider request/cost zero and customer charge explicitly unpriced; evidence `PRD7-B5-P7F07-exact-response-cache-local-20260731.md` |
| P7-F08 | Responses, rerank and image protocol coverage | P1 | Accepted | normalized endpoint contracts, adapter support declarations, negative fixtures and no silent semantic degradation |
| P7-F09 | Provider COGS connector and financial-truth workspace | P0 | Accepted through offline/synthetic boundary | supplier facts never create customer debt; pagination/replay/gap/duplicate/conflict fixtures; explainable reconciliation |
| P7-F10 | Offline route/policy simulator and progressive release | P0 | Accepted | immutable input snapshot; deterministic diff; shadow/canary/enforced states; rollback and audit |
| P7-F11 | Project member lifecycle and directory provisioning contract | P1 | Accepted through local lifecycle/synthetic boundary | create/update/suspend/deprovision; membership and capability revocation; replay/idempotency; SCIM vendor gated |
| P7-F12 | Agent Trust Firewall Console | P0 | Accepted | effective-authority tree; ancestor budget; attenuation diff; TTL/revocation proof; no root-secret exposure |
| P7-F13 | Signed decision receipt | P1 | Accepted | canonical metadata-only receipt; key/version/audience/tenant binding; verification and tamper negatives |
| P7-F14 | Governance Digital Twin | P0 | Accepted | fixture/replay evaluation; policy/route/config diff; no synchronous request-path dependency |
| P7-F15 | Provenance and Capture-Gap Passport | P0 | Accepted | direct/derived/declared/missing groups; source hash/revision; compatibility and capture-gap rendering |
| P7-F16 | Second heterogeneous Connector | P1 | Accepted through fixture/conformance boundary | no core analysis engine or vendor branch; shared lifecycle/error/replay/conformance reuse |
| P7-F17 | Risk-adjusted Routing | P1 | Accepted for replay/advice/shadow; enforcement gated by P7-D04 | bounded risk assertion; deterministic local decision; no remote synchronous dependency |
| P7-F18 | Evidence-to-Control proposal workflow | P1 | Accepted | evidence-derived draft, human review, simulation and explicit promotion; no self-modifying enforcement |
| P7-F19 | Async multimodal execution plane | P2 | Decision gated by P7-D03 | schema/threat-model/fixture work only until provider/storage/retention/charge semantics are chosen |

## Explicit Non-Claims

- No row is CI, target, external-integration or production verified unless its
  exact evidence is added later.
- Existing completed-execution Feed, credentials, source bytes, identity,
  endpoint, page and cursor behavior are not changed by acceptance.
- Semantic cache, managed/unified billing, plugin marketplace and universal
  high-privilege plugin execution are not included.
