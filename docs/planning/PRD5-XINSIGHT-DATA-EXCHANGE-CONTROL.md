# PRD5 X-Insight Data Exchange Control Surface

> Date: 2026-07-10
> Status: PRD5 Batch 1V generated the first controlled snapshot with one known
> denied-scenario correlation/source-trace gap; X-Insight import/readback is
> External integration verified. Batch 1W `931400a` / PR #126 is target
> verified under `UR-XI-GATE-A-20260710-02`, keys 36/37 are disabled, and the
> second snapshot passed X-Insight persistence/idempotency/readback. X-Insight's
> fixture/synthetic-only non-production target is Target verified at release
> artifact `b9e5811`; real upstream provider E2E remains pending
> Scope: product and architecture decision for UniRouter-owned X-Insight data
> exchange control UI, audit boundary, and staged implementation
> Batch 1Z update: Stage 1-3 UniRouter-owned MVP is Target verified for migration
> forward apply and the platform-admin UI in
> `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`;
> automatic artifact generation/download, exporter/outbox, and X-Insight runtime
> dependency remain non-goals. Migration down remains unexecuted

## Recommendation

Build a UniRouter UI entry, but do not make it a generic X-Insight
observability or audit console.

The right product shape is a UniRouter-owned **Data Exchange Control** surface:

- it controls what UniRouter is allowed to send out;
- it records who approved the outbound evidence and why;
- it records what artifact, manifest, schema version, and counts were handed off;
- it shows X-Insight import receipts and deep links only when X-Insight exposes
  stable receipts or URLs;
- it does not reimplement X-Insight's import internals, investigation timeline,
  cross-system execution graph, policy simulation, evidence packs, or read-side
  audit UI.

In product terms: UniRouter owns the export authorization and outbound ledger.
X-Insight owns downstream observability, normalized evidence, and investigation
audit. The two products reconcile through explicit exchange IDs and receipts.

## Current X-Insight Inputs Considered

This decision uses the current shared-folder X-Insight responses:

- X-Insight asked for one small combined Gate A snapshot with only
  `request_trace_events`, `call_logs`, and generated `capture_gaps`.
- X-Insight said not to create read-only DB/view, shared ECS staging,
  exporter/outbox, or X-Insight runtime dependency before the snapshot loop is
  proven.
- X-Insight confirmed it owns evidence normalization, persistence/read APIs,
  capture-gap interpretation, UI and policy simulation, and cross-source
  compatibility with LiteLLM/new-api/one-api/OTel/MCP/business systems.
- Atlas executed `UR-XI-GATE-A-20260710-01` and produced the first immutable
  sanitized snapshot. The remaining cross-project step is X-Insight persistent
  import/readback and receipt, not a UniRouter UI implementation.
- X-Insight has implemented `xinsight.import-receipt.v1` with source/exchange,
  artifact hash, result counts, readback counts, and warning codes.
- X-Insight intentionally does not expose `import_job_id` or a deep link yet
  because no durable import-job/API model exists.

## Why A UI Entry Is Reasonable

A single entry is useful because approval and data exchange are operational
workflows, not developer-only scripts. Without a UI entry, approvals remain
spread across Markdown worksheets, shell commands, shared folders, and memory.
That is manageable for Gate A, but not for a real enterprise integration.

The entry should live under an admin/ops information architecture, for example:

```text
Integrations / Data Exchange
  - X-Insight
    - Approval queue
    - Export ledger
    - Handoff artifacts
    - Import receipts
    - Configuration and safety boundary
```

It should not be placed only inside Logs Explorer. Logs can offer contextual
actions later, such as "Add this request to X-Insight approval", but the source
of truth should be the integration control page.

## Why "Put All X-Insight Audit Here" Is Not Right

Putting every X-Insight audit inside UniRouter sounds convenient, but it creates
the wrong ownership boundary:

- UniRouter would start becoming an observability product instead of an
  enterprise gateway and control plane.
- X-Insight importer retries, normalization, dedupe conflicts, evidence read
  audits, investigation timeline, policy simulation, and evidence packs belong
  to X-Insight because they are downstream evidence operations.
- UniRouter cannot be the authority for audit events that happen after evidence
  leaves UniRouter unless X-Insight sends back signed or structured receipts.
- A full downstream audit UI in UniRouter would duplicate X-Insight and make
  open-source / separate-repo evolution harder.

The better boundary is split audit:

| Audit Area | UniRouter Should Own | X-Insight Should Own |
|------------|----------------------|----------------------|
| Approval | who approved which request IDs, scenario labels, status, reason, time | optional readback of approved manifest identity |
| Export | snapshot ID, manifest hash, schema version, row counts, artifact path/hash, safety flags | import input identity and validation result |
| Delivery / handoff | command shape without secrets, handoff note path/hash, operator, time | import job ID, dedupe result, rejected rows, importer warnings |
| Import / normalization | receipt status and deep link only when returned by X-Insight | normalized evidence events, capture-gap interpretation, source refs, idempotency behavior |
| Investigation | link out by request/exchange/execution ID | timeline, graph, node detail, policy simulation, evidence packs |
| Read audit | local export-ledger views | X-Insight evidence read/export/delete audit |

## Options And Tradeoffs

| Option | Strength | Cost / Risk | Recommendation |
|--------|----------|-------------|----------------|
| Keep Markdown only | Fast, low code risk, good for Gate A | Not scalable, weak operator UX, hard to audit over time | Keep only for current Gate A |
| Add a simple approval page | Better than Markdown, lowers User friction | Too narrow if it cannot show export history or receipts | Accept only as first UI slice |
| Build a Data Exchange Control surface | Best long-term boundary: approval, export ledger, receipts, safety posture | Requires schema/API/UI work and RBAC/audit discipline | Recommended staged target |
| Put all X-Insight audits in UniRouter | One place for operators | Duplicates X-Insight, blurs ownership, increases coupling | Do not do this |
| Build exporter/outbox first | Scales production flow | Premature before Gate A feedback and field quality evidence | Defer |

## Staged Implementation

### Stage 0: Current Gate A

Status: controlled run and immutable snapshot complete with one explicit
denied-scenario source gap; X-Insight receipt is reconciled and persistent
import/readback is externally verified.

- Use shared worksheet and approval manifest.
- Use exchange ID and exact approval-manifest hash for generated artifacts.
- Preserve the current snapshot and manifest unchanged; X-Insight imported them
  as 10 accepted events with 10 inserted on first persistence, 10 duplicate/no-op
  on rerun, and readback of 10 events/3 gaps.
- Do not hide the denied request's missing source rows or infer a decision event
  that UniRouter did not emit.
- Keep the reconciled receipt and import/read evidence in shared handoff files.

### Stage 1: UI Readiness Design

Add a product/UI spec before implementation:

- page location and role access;
- approval queue fields;
- export ledger fields;
- import receipt fields;
- audit and retention boundary;
- links from Logs detail into the future approval queue;
- empty/error/loading states;
- explicit non-goals so the page cannot become an X-Insight clone.

Do not start this stage merely because the first controlled snapshot exists.
First finish sidecar receipt reconciliation, then let the User decide whether
operator volume and audit needs justify UI implementation.

### Stage 2: Minimal UniRouter UI

Implement only if Stage 1 is accepted:

- list pending/approved/unavailable request ID candidates;
- allow status and reason updates by authorized admins;
- show generated manifest preview;
- show export ledger rows after snapshot generation;
- show safety summary: no raw prompt/response, no credentials, no raw IP, no raw
  upstream error;
- link to Logs detail for local request evidence.

Non-goals for the first UI slice:

- no automatic request discovery;
- no read-only DB/view credential handoff;
- no exporter/outbox;
- no X-Insight timeline/graph;
- no raw payload capture.

### Stage 3: Receipt Reconciliation

Only after a real Gate A sidecar receipt proves the current contract:

- reconcile source workflow status separately from X-Insight receipt status;
- store `exchange_id`, artifact SHA-256, `validated` / `persisted` / `rejected`
  status, counts, checked time, and sanitized warning codes;
- derive `receipt_outdated` only when the receipt artifact hash differs from the
  latest UniRouter artifact;
- add `import_job_id` and a deep link only after X-Insight exposes a durable
  import-job/API model;
- do not store X-Insight internal evidence read audit in UniRouter.

### Stage 4: Exporter / Outbox Decision

Only after Gate A proves concrete volume, cursor, retry, and field-quality needs:

- design non-blocking exporter/outbox;
- add retry/dedupe/backpressure/redaction;
- keep request handling independent from X-Insight availability.

## Durable Data Concepts

Batch 1Y implements these UniRouter-side records in migration `000019`:

- `integration_exchanges`: one outbound exchange run, source instance, schema
  version, manifest hash, artifact hash/path, generated time, generated by;
- `integration_exchange_items`: request ID, scenario label, approval status,
  approver, reason, included/excluded status;
- `integration_exchange_receipts`: downstream system, exchange ID, artifact
  hash, status, counts, checked/received time, sanitized warnings, and only later
  a durable import job ID/deep link when X-Insight implements them.

Batch 1Y also adds exchange membership and delivery/re-delivery tables. Durable
X-Insight job IDs and deep links remain nullable. X-Insight's answered request
confirms they are not implemented, so the receipt write API rejects both fields
and accepts only the canonical v1 warning-code registry. Reopening requires a
later implemented versioned contract and explicit acceptance.

## Decision Boundary

Recommended decision:

- yes, keep the implemented UniRouter UI entry;
- make it a Data Exchange Control surface, not a full X-Insight audit console;
- keep the implementation narrow: approval, export/delivery ledger, and receipt reconciliation;
- use current import receipts for downstream status; add X-Insight deep links
  only after a later durable-job contract is implemented and accepted;
- keep downstream investigation and read audit inside X-Insight.

## Batch 1Y Implementation

The local implementation now provides:

- platform-admin-only exact-request and controlled-scenario approvals;
- organization-scoped request-ID existence checks and controlled-run sealing;
- immutable exchange/member ledger with hashes, counts, retention, and fixed
  metadata-only safety flags;
- delivery/re-delivery history and ledger-only artifact deletion;
- full current sidecar receipt count storage, identity reconciliation, and
  `receipt_outdated` derivation;
- audit events for ledger views and state changes;
- a UniRouter page for approval, exchange registration, delivery, receipt JSON,
  safety summary, and explicit missing durable job/deep-link state.

The consolidated X-Insight information request is Answered. The accepted
boundary and source-backed reconciliation are recorded in
`docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md`.

## Current Non-Goals

Batch 1Y still does not:

- generate a real snapshot artifact;
- approve or invent request IDs;
- add read-only DB/view access;
- add exporter/outbox;
- add X-Insight runtime dependency;
- store raw prompt/response payloads;
- change PRD4.
