# PRD7 Implementation Plan

> Date: 2026-07-31
> Status: Completed for authorized repository-local scope through B14;
> target/external decision gates remain open
> Dependency rule: complete deterministic contracts and offline evidence before
> external integrations or enforcement

## Batch Order

| Batch | Scope | Depends on | Completion gate |
|-------|-------|------------|-----------------|
| B0 | Preserve clean baseline; formalize accepted PRD7 scope and decisions | Receipt V0 | docs, matrix and old candidate history agree without rewriting old evidence |
| B1 | Provider Pack V1, Azure OpenAI adapter, three fixtures and Provider inspector | B0 | strict offline conformance; adapter/request/error/usage tests; UI status without live calls |
| B2 | Persistent inspectors plus saved filters/deep links for Audit and Projects | Receipt inspector primitives | no second click for read-only detail; URL and responsive tests |
| B3 | Metadata-only OTel/OpenInference export | event/outbox and provenance contracts | default off; bounded async delivery; raw-content and cardinality negatives |
| B4 | Secret-reference interface and local rotation lifecycle | B1 | no secret readback/logging; stage/activate/rollback/revoke and stale-version tests |
| B5 | Exact response cache | identity, policy, finality and secret boundaries | isolation, poison, stampede, TTL, purge, billing/finality and outage tests; runtime hit serving remains gated by P7-D07 |
| B6 | Responses/rerank/image protocols | B1/B5 | endpoint-specific conformance; unsupported semantics fail explicitly |
| B7 | Provider COGS offline connector and Financial Truth workspace | B1, event/connector contract | supplier/customer authority separation; replay/gap/conflict fixtures |
| B8 | Route/policy simulator, progressive release and Provider Change Firewall | B1/B7 | deterministic replay/diff/rollback; no runtime promotion |
| B9 | Project-member lifecycle and provisioning conformance | existing IAM/capability kernel | deprovision revokes effective access; IdP-neutral replay tests |
| B10 | Agent Trust Firewall, signed receipts and Governance Digital Twin | B8/B9 | attenuation/revocation/tamper/replay proof and persistent inspector UI |
| B11 | Provenance Passport and second heterogeneous Connector | shared protocol pin, B3/B7 | no vendor branch in core; shared conformance reuse |
| B12 | Risk advice/shadow and Evidence-to-Control workflow | B8/B10/B11 | bounded advice, human-reviewed proposal, simulation; enforcement still gated |
| B13 | Async multimodal execution | User decision P7-D03 | pre-gate Schema, threat model and deterministic fixtures Local verified; runtime implementation must not begin before product semantics are chosen |
| B14 | GPT Image V1 repository-local runtime | User-resolved P7-D03, B6/B13 | strict tenant/idempotency/capacity contract; PostgreSQL lease/fence/recovery; S3-compatible immutable output and seven-day expiry; charge fact without wallet debit; video disabled; full local, isolated-PostgreSQL and browser evidence |

## Repository-Local Completion Boundary

B0 through B14 are complete at the evidence level allowed by their gates.
P7-F19 now has a repository-local Image V1 runtime implementation, but it is
not deployed and has no real provider or object-store evidence. No batch is
thereby authorized for deployment, live traffic,
credentials, persistent imports, external directory/provider integration,
runtime policy promotion, push, PR or merge.

The accepted plan has no further autonomous repository-local batch. A next
step requires the User to select and authorize one remaining material gate:
P7-D01, P7-D02, P7-D04, P7-D05, P7-D06 or P7-D07. P7-D03 is resolved only for
the repository-local Image V1 decisions recorded in
`docs/architecture/44-async-media-image-v1-runtime-contract.md`; it does not
authorize target migration, credentials, paid traffic or release.

## Verification Per Batch

Each implementation batch must include:

1. focused red/green unit and contract tests;
2. tenant, authority, duplicate, conflict, missing and unavailable negatives;
3. frontend interaction tests for new product surfaces;
4. affected Go package tests and frontend type-check/build;
5. full suites at the batch closeout boundary;
6. immutable local evidence stating exact commands, results and non-claims;
7. one intentional local commit and a clean worktree.

## Runtime And External Gates

No batch may infer authorization for deployment, real credentials, provider or
IdP traffic, data import, cursor/checkpoint changes, database mutation outside
isolated tests, push, PR or merge. Those remain separate actions even when the
repository implementation is complete.
