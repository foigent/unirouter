# PRD6 Backlog And Decision Register

> Date: 2026-07-11
> Rule: this is a classified input queue, not an implementation promise.

## 1. Accepted Active Work

| ID | Item | Why it is active | Exit evidence |
|----|------|------------------|---------------|
| P6-ROUTE-001 | Half-open recovery deadline correction | Confirmed current-code outage risk; no new product scope | focused/race/full tests and CI |
| P6-RL-001 | Atomic exact Redis RPM/RPS enforcement | Confirmed over-admission and multi-instance race; no new surface | sequential/concurrent/context/failure tests and CI |
| P6-DOC-001 | Independent PRD6 truth chain | Required to prevent PRD5 reopening and scope leakage | entry, PRD, matrix, plan, backlog, status/inventory sync |
| P6-TENANT-001 | Enforce organization ownership and block org-admin platform-role escalation | Accepted D6-01 P0; prevents cross-tenant key/user/project/project-policy control | migrated-PostgreSQL matrix and PR #140 required CI are green |
| P6-AUDIT-TENANT-001 | Enforce actor-organization ownership on audit-log reads | Confirmed cross-tenant evidence disclosure; safe P0 remediation under PRD6 work rule 7 | migrated two-tenant JWT matrix, full local checks and required CI |
| P6-CI-FRONTEND-001 / P6-F38 | Run existing frontend behavior tests in required CI | Prevents type-correct/buildable UI regressions from merging; safe P1 engineering remediation | full 126-test suite and exact required-job log with no skips |
| P6-FIN-TENANT-001 / P6-F32 | Tenant-scope wallet, grant, transaction and reconciliation operations | Prevents cross-tenant financial read/write and first-row fallback | Merged and CI verified by PR #145 / `609385f` |
| P6-USAGE-TENANT-001 / P6-F39 | Tenant-scope request logs and usage-derived administrator reads | Prevents cross-tenant evidence, incident, token and cost disclosure | Merged and CI verified by PR #145 / `609385f` |
| P6-TEST-DB-001 | Make legacy tagged admin integration tests deterministic on the current migrated schema | Prevents stale shared-database state from hiding or inventing regressions | focused touched cases pass; full tagged suite must move to isolated migrations or be retired before becoming required CI |
| P6-CONTROL-AUTH-001 / P6-F35 | Platform-gate deployment settings, license and global sensitive words | Prevents tenant roles mutating deployment-wide controls or bypassing dedicated license/word paths | implementation/local verification/independent review complete; PR, exact-head required CI and merge pending |

## 2. Accepted MVP — Ordered Work Queue

| ID | Item | Value | Dependency / risk | Priority |
|----|------|-------|-------------------|----------|
| P6-TENANT-001 | Enforce organization ownership on key/user/project/project-policy operations | prevents accepted-scope cross-tenant access and malformed policy influence | CI verified by PR #140; preserve regressions | P0 |
| P6-IDEM-001 | Separate server operation, client idempotency, trace/span, workflow and Agent identities | prevents cross-tenant/retry/child charge collisions | schema migration and payload fingerprint semantics | P0 |
| P6-BILL-003 | Pool-safe reservation and single durable terminal state | prevents pool starvation, double terminal transitions and free delivered output | financial state migration, recovery and invariant tests | P0 |
| P6-AUTH-001 | Versioned user/project/key/role revocation | bounds stale authority across replicas | revocation SLO, cache and outage posture | P0 |
| P6-ROUTE-002 | Retry classification and response preservation | prevents unsafe/expensive retry and misleading 502 | provider error matrix and billing invariants | P0 |
| P6-ROUTE-003 | Embeddings failover parity | removes endpoint reliability asymmetry | adapter and billing/trace parity | P0 |
| P6-CONFIG-001 | Effective routing/circuit config truth | operator control and explainability | migration/default compatibility | P0 |
| P6-BILL-002 | Multiplier-aware freeze/settle correctness | prevents uncollected cost | wallet/transaction semantics | P0 |
| P6-CAP-001 | Short, attenuating Agent capability and ancestor budget | avoids shared-secret/per-child-key explosion and quota amplification | claim contract, key rotation, TTL/refresh, revocation | P1 |
| P6-LIMIT-002 | Shared RPM/RPS/TPM/concurrency policy | enterprise isolation/capacity | failure-mode and output TPM semantics | P1 |
| P6-ROUTE-004 | Routing Policy V1 | predictable priority/weight/retry/failover | telemetry, policy version and rollback | P1 |
| P6-DURABLE-001 | Durable minimal usage/finalization and bounded evidence workers | prevents post-delivery revenue loss and evidence self-DDoS | transactional outbox, replay and priority semantics | P1 |
| P6-WORK-001 | Weighted workload admission | prevents long streams/media starving short work and replica-multiplied quotas | workload envelope, lease/fairness semantics | P1 |
| P6-BILL-001 | Prompt-cache token usage and billing | accurate provider economics | versioned adapter/price sources | P1 |
| P6-GUARD-001 | Guardrail Kernel | enterprise safety and PII controls | detector/failure/latency policy | P1 |
| P6-STREAM-001 | Streaming interception and termination | limits unsafe live disclosure | buffer/latency/protocol contract | P1 |

## 3. D6-08 Accepted Administrator Integrity Queue

| ID | Item | Confirmed value/risk | Current state | Priority |
|----|------|----------------------|---------------|----------|
| P6-FIN-TENANT-001 / P6-F32 | Tenant-scope wallet, grant, transaction and reconciliation operations | prevents cross-tenant financial read/write and removes first-wallet behavior | Merged and CI verified by PR #145 / `609385f` | P0 |
| P6-NOTIFY-TENANT-001 / P6-F33 | Tenant-scope notification reads and webhook sinks | closes a source-reachable victim-event exfiltration chain | CI verified by PR #142 / `7173c59`; target cutover unexecuted | P0 |
| P6-PRICE-AUTH-001 / P6-F34 | Platform-only global base-price and organization-multiplier mutation | prevents tenant roles changing platform/victim billing | PR #146 squash-merged as `517ce45381429781f7e81e492bdd9f38093c21ba`; final-head run `29137670055` and post-merge main run `29137781625` passed all required jobs; historical branch retained | P0 |
| P6-CONTROL-AUTH-001 / P6-F35 | Gate deployment settings, license and global sensitive words | prevents tenant roles changing global runtime controls | Local verified and independently reviewed; PR/required CI/merge pending; no target/production claim | P0 |
| P6-MODELGOV-TENANT-001 / P6-F36 | Tenant-scope registry, alias, catalog and organization model sets | prevents cross-tenant model-policy disclosure/mutation | Accepted; not started | P1 |
| P6-AUDIT-TENANT-001 / P6-F37 | Tenant-scope audit-log reads | prevents organization/billing roles reading another tenant's control-plane evidence | Merged and CI verified by PR #143 / `a1e0ad6` | P0 |
| P6-USAGE-TENANT-001 / P6-F39 | Tenant-scope request logs, usage analytics/internal status, dashboard usage-derived aggregates and runtime traffic | prevents cross-tenant request, incident, token and cost disclosure | Merged and CI verified by PR #145 / `609385f` | P0 |

Evidence: `docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md`,
`docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md`,
`docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md`,
`docs/ops/evidence/PRD6-B0B-P6F34-pricing-authority-local-20260711.md` and
`docs/ops/evidence/PRD6-B0B-P6F35-global-control-authority-local-20260711.md`.
P6-F32 through P6-F36 were accepted through D6-08. P6-F37 is a separately
recorded safe P0 remediation under the PRD6 work rule; P6-F39 uses the same safe
remediation rule for adjacent cross-tenant usage reads. None of these rows
reopens PRD5 or retroactively changes D6-01.

P6-F35 target execution remains deferred by D6-09. Any later activation must
drain old control-plane binaries and be non-rolling because old binaries do not
provide mixed-version safety. New-version replicas converge global-word changes
through the existing approximately one-minute reload. A typed configuration
registry and the confirmed budget/alert key, scope and audit truth gaps remain
P6-F05/P6-F17; ambiguous network commit outcome/readback is also P6-F17. No
global-settings UI was added.

## 4. Advanced Candidates

| ID | Item | Reopen prerequisite | Priority |
|----|------|---------------------|----------|
| P6-CRED-001 | Vault/KMS/cloud secret references and versioned rotation | select first backend, threat model and rotation SLO | P2 |
| P6-CACHE-001 | Exact response cache | measured workload value; tenant/auth/TTL/purge/billing design | P2 |
| P6-CACHE-002 | Semantic response cache | exact-cache safety first; semantic poisoning/equivalence red team | P2 |
| P6-ROUTE-005 | Latency/cost/rate-aware routing | trustworthy telemetry and stable Routing V1 | P2 |
| P6-ROUTE-006 | Canary/shadow traffic policy | explicit paid-request/data boundary and rollback | P2 |
| P6-GUARD-002 | BYO guardrails and policy simulation | versioned kernel, RBAC and compatibility contract | P2 |
| P6-ADAPTER-001 | Selected provider/SDK expansion | named customer/provider need and conformance ownership | P2/P3 |
| P6-BILL-004 | Measured wallet credit/escrow sharding | same-tenant wallet row remains hot after correctness fixes | prove hotspot; lease/refill/reconciliation design | P2 |
| P6-MEDIA-001 | Asynchronous image/video job plane | isolates large payloads and long work from synchronous chat | explicit media roadmap, object/queue/retention contract | P2 |
| P6-DATA-001 | Time-partitioned high-volume evidence | avoids unbounded delete/WAL/bloat at 100x retention volume | volume benchmark and migration/rollback plan | P2 |
| P6-TRUST-001 | Signed attenuation-only risk assertion and audience-specific local policy | keeps adaptive analysis off the request path | identity/capability foundation, signing/epoch/replay/outage contract | P2 |
| P6-EVAL-001 | Offline Governance Digital Twin and failure-to-eval manifest | prevents untested model/policy promotion | provenance/privacy review, fixed fixtures and zero-side-effect runner | P2 |
| P6-SHADOW-001 | Authorized live shadow routing | measures candidate behavior on selected traffic | separate provider/data/cost authorization and independent settlement | P2 |
| P6-LINEAGE-001 | Authenticated data-lineage passport and sink-bound enforcement | prevents untrusted data from widening purpose or export authority | authenticated delegation/data-class facts and deterministic action-owner checks | P2 |
| P6-COLLUSION-001 | Semantic collusion/cascade radar | detects split-step policy bypass without treating collaboration as malicious fact | ground-truthed fixtures, shadow precision/recall/abstention and action-owner coverage | P3 |
| P6-RECEIPT-001 | Verifiable MaaS trust receipt | lets customers verify the recorded policy/model/usage statement | canonical signing, rotation, tamper and privacy contract | P2 |
| P6-CLAIM-001 | Domain-bounded claim assurance and risk-adjusted routing | routes high-stakes claims through evidence/abstention controls | authoritative domain fixtures and non-truth-claim metrics | P2 |
| P6-TRAIN-001 | Approved failure case to external model-lifecycle workflow | supports improvement without gateway-owned training | data rights, poisoning screen, holdout, human promotion and rollback | P3 |

## 5. External-Project Boundary

| Boundary | UniRouter owns | External owner may own | Current disposition |
|----------|----------------|------------------------|---------------------|
| Evidence consumption | authoritative request identity, policy/routing decisions, usage/billing truth, sanitized gateway audit creation | independent investigation, aggregation, storage and UI | asynchronous boundary only; no external feature is PRD6 scope or a request dependency |
| Secret backend | selected versioned references and gateway consumption after acceptance | Vault/KMS/cloud service operation | deferred pending backend decision |

## 6. Target Evidence Queue

These are evidence needs, not automatic source implementation:

The User directed on 2026-07-11 that this queue is executed only after PRD6
source closeout and with its existing item-specific authorization. The queue is
retained, not cancelled or satisfied. The previously accepted 2026-07-17 Gate A
file deadline is not postponed by this sequencing decision.

- P5-F35 concrete target route/failover detail;
- migration `000019` down path;
- TLS 443 behavior;
- real upstream and real SSO;
- selected replica/load/capacity test;
- selected HA/DR, restore and production-hardening evidence.

Each needs an exact environment, operator, data, credential, cost and retention
authorization before execution.

## 7. Deferred Or Rejected By Default

| Item | Disposition | Reason |
|------|-------------|--------|
| Full Scheduler V2 | Deferred | Routing V1 correctness and evidence must stabilize first |
| Plugin/marketplace/runtime installation | Deferred | supply-chain, sandbox and governance surface is disproportionate to current value |
| IAM rewrite | Deferred | fix the confirmed tenant predicates and add a narrow capability layer first; neither requires a wholesale rewrite |
| Managed HA/IaC/customer SLA operations | Target/commercial prerequisite | requires chosen topology and operating model, not a generic code task |
| Runtime budget hard-block | Not auto-restored from PRD5 | PRD6 proposes explicit ancestor/reservation semantics; it does not silently restore the prior backlog item |
| Plaintext prompt/response cache | Rejected | conflicts with accepted default-off payload posture |
| Broad SDK suite | Deferred | provider/customer demand must justify maintenance |
| Service-account runtime permission | Deferred | requires identity/threat model and explicit authorization |
| Public payment/recharge/referral/resale features | Rejected by current positioning | UniRouter is scoped as enterprise-local MaaS, not a public token storefront |
| External exporter/continuous ingestion | External/future contract | P6-DURABLE-001's internal outbox does not authorize an external delivery channel |

## 8. Decision Register

| Decision | Recommended default | Status | Effect |
|----------|---------------------|--------|--------|
| D6-01 MVP boundary | P0 tenant/financial integrity + existing correctness + Agent capability/distributed policy + Routing V1 + durable/cache-aware billing + guardrail/stream foundation | Accepted by User 2026-07-11 | authorizes Batches 0B-5 subject to batch/PR/CI gates |
| D6-02 Enforcement failure mode | identity/tenant/mandatory safety/hard budget fail closed; optional analysis continues only with durable degraded evidence; capacity gets only a bounded last-known-safe lease | Accepted by User 2026-07-11 | governs auth, limiter, routing, guardrail and finalization design |
| D6-03 Response cache | keep out of MVP; advanced opt-in only | Proposed | avoids premature privacy/retention expansion |
| D6-04 Product position | enterprise-local MaaS gateway/control plane | Proposed based on current repository | rejects public-commerce copying by default |
| D6-05 External-project boundary | external systems may asynchronously consume accepted sanitized facts; no external feature or runtime is a request dependency | Proposed clarification consistent with the accepted PRD5 boundary | prevents ownership and availability coupling |
| D6-06 Post-MVP differentiator | adopt Evidence-to-Control Flywheel while accepting each P6-F23-F31 row separately | Proposed; User decision not required for current P0/P1 work | first recommended experiment is fixture-only P6-F24 |
| D6-07 Innovation data posture | metadata-only by default; content analysis requires tenant opt-in plus purpose/retention/delete/access controls | Proposed | gates semantic analysis, shadow data and training cases |
| D6-08 Administrator authority split | platform owns global price/settings/license/global sensitive words and organization multipliers; tenant roles are confined to their JWT org | Accepted by User 2026-07-11 with option B | authorizes P6-F32-P6-F36; organization multipliers are `platform_admin` only until a separately accepted global billing-authority role exists |
| D6-09 Closeout sequencing | complete accepted PRD6 repository scope first; defer all PRD7 implementation and target-evidence execution | Accepted by User 2026-07-11 | preserves both future queues without authorizing target action; historical branches remain retained; the existing Gate A file deadline is unchanged |
