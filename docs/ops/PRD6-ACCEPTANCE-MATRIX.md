# PRD6 Acceptance Matrix

> Date: 2026-07-11
> Status: D6-01/D6-02/D6-08/D6-09 accepted; Batch 0C active; P6-F03/P6-F04/P6-F05 CI verified / closed; P6-F06 Local verified / CI pending
> Rule: a proposed row is not accepted implementation scope until its scope
> state changes through an explicit decision. PRD5 evidence is referenced, not
> rewritten.

## Evidence Levels

| Level | Meaning |
|-------|---------|
| Proposed | Product value and acceptance design exist; implementation is not authorized by this matrix |
| Local verified | Repository implementation passed the listed local tests and evidence review |
| CI verified | Required GitHub checks passed for the exact PR commit |
| Target verified | The exact authorized non-production target behavior was observed |
| Production verified | Explicit production acceptance evidence exists; never inferred from lower levels |
| External integration verified | A separate project/system completed its accepted contract independently of target/production status |

## Active And Proposed Rows

| ID | Requirement | Owner | Priority / tier | Scope state | Acceptance evidence | Current result |
|----|-------------|-------|-----------------|-------------|---------------------|----------------|
| P6-F01 | Expired half-open channels regain bounded probe eligibility | UniRouter | P0 / MVP correctness | Accepted Batch 0A | query-semantic regression; router unit and race tests; full backend test; CI | CI verified by PR #139 |
| P6-F02 | Redis API-key RPM/RPS checks are atomic and enforce N allowed / N+1 rejected | UniRouter | P0 / MVP correctness | Accepted Batch 0A | strict QPS/RPM boundary; concurrent exact-admission test; canceled-context test; Redis-error fail-open compatibility; race/full backend/CI | CI verified by PR #139 |
| P6-F16 | Organization administrators cannot list/read/create/update/disable/delete another tenant's key, user, project or project-owned model-set assignment, influence routing through a malformed cross-org assignment, or self-elevate into a platform role | UniRouter | P0 / MVP integrity | Accepted; CI verified by PR #140 | authenticated handler matrix: same tenant succeeds, foreign object access 403/no side effect, foreign model-set ID 400/no partial change, platform/legacy exception, org-zero fail closed; role-race predicate; atomic project/model-set replace; malformed membership/assignment negatives; CI-executed migrated PostgreSQL tests | PR #140 required checks pass; Backend log proves migrated matrix and all twelve subtests PASS, not SKIP |
| P6-F32 | Billing-admin wallet, grant, transaction and reconciliation operations are actor-tenant scoped | UniRouter | P0 / MVP integrity | Accepted by D6-08 | two-tenant signed-JWT matrix; no "first wallet" fallback; cross-tenant 403/no balance, grant, transaction, audit, notification or export side effect; platform default-own/explicit-target behavior; CI | Merged and CI verified by PR #145 / `609385f`; post-merge main run `29136296782` passed; evidence: `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md` |
| P6-F33 | Notification events, deliveries and webhooks are actor-tenant/project scoped and cannot redirect another tenant's event payload | UniRouter | P0 / MVP integrity | Accepted by D6-08 | two-tenant read/create/update matrix; same-org project invariant; pre-fix rows retained/quarantined until version-bound atomic re-verification; pre-up retry never auto-replays; old-writer rejection; safe down→up lockdown; foreign ID non-enumeration; platform exception; CI | CI verified by PR #142 / merge `7173c59`; post-merge main CI run `29132520222` passed; target non-rolling notification-control-plane cutover remains unexecuted; evidence: `docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md` |
| P6-F37 | Audit-log reads are actor-tenant scoped; tenant roles cannot widen scope with organization/project filters | UniRouter | P0 / MVP integrity | Accepted safe remediation under PRD6 work rule 7 | migrated two-tenant signed-JWT matrix; omitted/foreign org and project negatives; org-zero fail closed; same-tenant filters; platform exception; no victim marker disclosure; full local and required CI | CI verified by PR #143 / merge `a1e0ad6`; post-merge main CI run `29133690338` passed; evidence: `docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md` |
| P6-F38 | Existing frontend tests are part of required CI with a realistic interaction timeout and unchanged assertions | UniRouter | P1 / engineering integrity | Accepted safe remediation under PRD6 work rule 7 | reproduce default-timeout failures under suite contention; isolated green; full 126-test suite with configured timeout; type-check/build; exact PR Frontend Build log; no skipped tests | CI verified by PR #143 / merge `a1e0ad6`; PR and post-merge logs prove the Frontend tests step passed before build; evidence: `docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md` |
| P6-F39 | Authenticated request-log list/detail/trace, usage analytics/internal status, dashboard usage-derived aggregates and runtime traffic are actor-tenant scoped; platform cross-tenant selection is explicit | UniRouter | P0 / MVP integrity | Accepted safe remediation under PRD6 work rule 7 | migrated two-tenant signed-JWT matrix; omitted/foreign organization negatives; foreign/missing log non-enumeration; org-zero fail closed; platform default-own/explicit-target behavior; tenant-specific counts/tokens/cost/incidents; full local and required CI | Merged and CI verified by PR #145 / `609385f`; post-merge main run `29136296782` passed; evidence: `docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md` |
| P6-F34 | Global base-price and organization-multiplier mutations are platform-authority only | UniRouter | P0 / MVP integrity | Accepted by D6-08 | route-role matrix; own/foreign org multiplier tests; global-price platform gate; billing calculation non-regression; audit/CI | PR #146 squash-merged as `517ce45381429781f7e81e492bdd9f38093c21ba`; final-head run `29137670055` and post-merge main run `29137781625` passed Backend Tests, Frontend Build and Docker Build; historical branch retained; evidence: `docs/ops/evidence/PRD6-B0B-P6F34-pricing-authority-local-20260711.md`; no new global billing role is implied |
| P6-F35 | Deployment settings, license material and the current global sensitive-word list are not mutable by tenant roles | UniRouter | P0 / MVP integrity | Accepted by D6-08 | production route-registration/handler authority matrix; reserved `license.*`/`sensitive_words` bypass denial; redacted settings reads; atomic business-write/audit rollback; concurrent global-word version ordering and replica convergence; startup fail-closed; full local/race/frontend/API-contract verification; required CI | PR #147 final head `fb77083d0f04f5cdaa20f38e763c93b51b89f4d1` / run `29139408226` passed all required jobs; squash-merged as `c08db96167837ade685708c2ab204861a8ecc0ca`; post-merge main run `29139534691` passed; historical branch restored and retained; no target or production claim; later target activation must be non-rolling after old control-plane drain; other new-version instances converge in approximately one minute; evidence: `docs/ops/evidence/PRD6-B0B-P6F35-global-control-authority-local-20260711.md` |
| P6-F36 | Organization-level model registry, alias, catalog and model-set endpoints enforce actor ownership | UniRouter | P1 / MVP security prerequisite | Accepted by D6-08 | query/body/global-ID cross-tenant matrix; final SQL org predicates; project catalog same-org rule; platform exception; live model-admission non-regression; atomic model-set/audit rollback; CI | PR #148 final evidence head passed required run `29140739477`, squash-merged as `d83dbbc0ac7137bb44d2346adc5a25debde804e1`, and post-merge main run `29140850345` passed; no target claim; evidence: `docs/ops/evidence/PRD6-B0B-P6F36-model-governance-tenant-isolation-local-20260711.md` |
| P6-F17 | Server operation, client idempotency, trace/span, workflow and Agent identities are separate and tenant scoped | UniRouter | P0 / MVP integrity | Accepted urgent Batch 0B | 100-child shared-trace and 100-settlement test; same-payload metadata replay; changed-payload 409; 100-way claim; cross-org collision negative; final-attempt readback; migration safe up/down/roll-forward | CI verified: caller-unoverrideable `op_` identity, separate client/workflow/Agent/attempt/upstream fields, HMAC metadata-only Scheme A, tenant/key/payload scope, financial composite key and Logs/Audit readback are implemented; PR #149 run `29152968280`, squash merge `c9402fa` and post-merge main run `29153106336` passed; no target claim; evidence: `docs/ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md` |
| P6-F18 | Reservation is pool-safe and reaches exactly one durable terminal state after billable output | UniRouter | P0 / MVP financial integrity | Accepted urgent Batch 0B; CI verified / closed | pool-size barrier; 100-way Settle/Cancel race; post-chunk DB failure/restart; wallet/ledger invariants; multiplier price version | F18A verifies pool-safe Freeze, source-preserving grant/cash holds and refunds, immutable effective price/multiplier, exact-funds duplicate handling and one terminal ledger event. F18B persists `settlement_pending` before the first billable stream data event, persists immutable final usage, forbids post-output cancellation, and uses leased bounded restart recovery. Twenty concurrent workers after a simulated post-chunk DB loss produce one terminal settlement; payload-missing rows remain held without guessed charge/refund. PR #151 / run `29154664491` / merge `7db1efc` and PR #153 / run `29155948787` / merge `b30bc9d` passed; post-merge main runs `29154820102` and `29156096777` passed all required jobs. No target claim; per-chunk journal and general outbox remain P6-F20. Evidence: `docs/ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md`, `docs/ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md` |
| P6-F03 | Retry only retryable provider failures and preserve safe client/policy response semantics | UniRouter | P0 / MVP correctness | Accepted Batch 0C | adapter matrix tests; no retry on 4xx/policy errors; bounded retry on classified transient failures; trace and billing invariants | CI verified / closed by PR #156, merge `f6f075a` and post-merge main run `29158395734`; evidence: `docs/ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md` |
| P6-F04 | Chat and Embeddings have route/failover parity | UniRouter | P0 / MVP correctness | Accepted Batch 0C | multi-route embeddings integration tests; auth/billing/trace parity; all-routes-fail behavior | CI verified / closed by PR #157, merge `36f736c` and post-merge main run `29159155254`; evidence: `docs/ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md` |
| P6-F05 | Effective retry/circuit/cooldown and project budget-alert configuration are wired, versioned and observable | UniRouter | P0 / MVP correctness | Accepted Batch 0C | config default/override tests; no hidden hard-coded divergence; route-decision config evidence; one tenant/project alert ledger with retired ambiguous global keys | CI verified / closed: `effective-config.v1` contains the driving immutable `routing-config.v1` and active `project-budget-alert.v1` contract; route and two-tenant budget state tests, trace, runtime API/UI and retired-global-key behavior passed PR #158 run `29160329032`, merge `ca02ecb` and post-merge main run `29160494169`; evidence: `docs/ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md` |
| P6-F06 | Organization multiplier cannot create an uncollected under-freeze settlement | UniRouter | P0 / MVP correctness | Accepted Batch 0C | insufficient-freeze tests; atomic debit/loss rules; cancel/refund and concurrent settlement tests | Local verified / CI pending: the P6-F18A effective-price snapshot already removes the defect mechanism; a migrated PostgreSQL matrix now proves 19/20-cent rejection, exact-funds 32-way settlement with one receipt and `uncovered_cost=0`, and idempotent full cancel/refund. Required Backend has a fail-closed P6-F06 gate. Evidence: `docs/ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md` |
| P6-F07 | Shared RPM/RPS/TPM/concurrency policy with explicit selectors and failure semantics | UniRouter | P1 / MVP | Accepted | multi-instance load/fault tests; shadow/enforce modes; policy-version trace; bounded metrics; fail-mode tests | Not started |
| P6-F08 | Routing Policy V1 provides explainable priority/weight/retry/cooldown/failover | UniRouter | P1 / MVP | Accepted | deterministic policy fixtures; Chat/Embeddings parity; fault injection; candidate/exclusion/fallback evidence | Not started |
| P6-F09 | Provider cache-read/cache-create usage is normalized, priced and reconciled | UniRouter | P1 / MVP | Accepted | version-fixed provider fixtures; source/price metadata; ledger/log/UI and reconciliation tests; no fake cache-hit billing | Not started |
| P6-F10 | Versioned Guardrail Kernel covers input and non-stream output | UniRouter | P1 / MVP | Accepted | per-scope policy tests; redact/block/audit modes; external-detector failure tests; no raw-payload persistence | Not started |
| P6-F11 | Stream policy hook has bounded disclosure, protocol termination, cancel and settlement behavior | UniRouter | P1 / MVP | Accepted | cross-chunk/oversized/adversarial SSE tests; measured exposure; client disconnect/upstream cancel; exactly-once settlement | Not started |
| P6-F19 | Short Agent capabilities attenuate authority and share ancestor budget/revocation | UniRouter | P1 / MVP | Accepted | offline verification; depth/fan-out/model/cost/rate/concurrency attenuation; 100-child aggregate cap; refresh jitter/singleflight; all-replica revocation SLO | Not started |
| P6-F20 | Minimal usage/finalization is durable while detailed evidence and notifications use bounded replayable workers | UniRouter | P1 / MVP | Accepted | process-kill/outbox replay; duplicate/reorder tests; billing/audit priority; bounded memory/goroutines; trace-lag SLO | Not started |
| P6-F21 | Shared admission is workload weighted, replica invariant and fair to short/long work | UniRouter | P1 / MVP | Accepted | 2/3-replica exact concurrency; lease expiry/crash; Redis latency/outage; workload-class fairness; bounded wait and `Retry-After` | Not started |
| P6-F12 | External secret references, versions, rotation and rollback | UniRouter plus selected secret backend | P2 / Advanced | Deferred pending backend decision | rotation, stale-worker, outage, audit and rollback evidence | Not accepted |
| P6-F13 | Opt-in exact/semantic response cache is tenant-isolated and purgeable | UniRouter | P2 / Advanced | Deferred pending value/security decision | isolation, auth, TTL, purge, stampede, poisoning, encryption and billing red-team tests | Not accepted |
| P6-F14 | Latency/cost/rate-aware and canary routing | UniRouter | P2 / Advanced | Deferred until Routing V1 telemetry is trustworthy | shadow comparison, SLO, rollback and route-explainability evidence | Not accepted |
| P6-F15 | Policy simulation, BYO guardrails and config rollback | UniRouter; external evidence consumers remain optional | P2 / Advanced | Deferred pending policy lifecycle design | version/RBAC/audit, dry-run diff, compatibility and rollback tests | Not accepted |
| P6-F22 | Asynchronous object-reference image/video job plane | UniRouter | P2 / Advanced | Deferred pending explicit media roadmap decision | tenant-scoped upload/job/status/cancel; queue lease/retry; idempotent completion; retention; final usage/settlement; load isolation | Not accepted |
| P6-F23 | Signed, expiring, attenuation-only risk assertion and audience-specific local enforcement snapshot | X-Insight risk signal; policy authority; UniRouter/Tool/IAM enforcement | P2 / Advanced | Proposed; separate decision | signature/audience/tenant/epoch/replay tests; outage/stale-TTL behavior; no privilege increase; bounded broad-downscope blast radius | Not accepted |
| P6-F24 | Offline Governance Digital Twin and failure-to-evaluation manifest | X-Insight plus isolated evaluation runner | P2 / Advanced | Proposed; recommended first innovation experiment | provenance/privacy approval; deterministic replay; safety/quality/cost/latency diff; zero paid calls and external side effects | Not accepted |
| P6-F25 | Explicitly authorized live shadow routing | UniRouter | P2 / Advanced | Proposed; separate data/provider/cost authorization | separate request IDs and paired experiment ID; shadow never returned; tenant/provider/residency/budget bounds; independent settlement | Not accepted |
| P6-F26 | Authenticated data-lineage passport and sink-bound enforcement | UniRouter context propagation; Tool/data sink enforcement | P2 / Advanced | Proposed; separate decision | authenticated lineage/data class/purpose/sink; cross-tenant/forgery/delegation tests; deterministic sink allow/deny; capture-gap semantics | Not accepted |
| P6-F27 | Verifiable MaaS trust receipt | UniRouter | P2 / Advanced | Proposed; separate decision | canonical form; issuer/key/version/rotation; tamper/replay verification; bounded metadata; statement-not-truth semantics | Not accepted |
| P6-F28 | Domain-bounded claim assurance and risk-adjusted routing | UniRouter enforcement; X-Insight calibration | P2 / Advanced | Proposed; separate decision | authoritative-domain fixtures; source coverage, contradiction and abstention metrics; route/cost/latency evidence; no truth guarantee | Not accepted |
| P6-F29 | Approved failure cases feed an external model-lifecycle workflow | External training/lifecycle owner | P3 / Long-tail | Deferred; reject direct automatic raw-case training/promotion | data rights, poisoning review, holdout, reproducible artifact, human promotion and rollback | Not accepted |
| P6-F30 | Multimodal provenance passport | UniRouter media plane plus selected provenance standard | P3 / Long-tail | Deferred pending media roadmap | signed provenance validation, unknown-provenance semantics, modality conformance, retention and key lifecycle | Not accepted |
| P6-F31 | Semantic collusion/cascade risk analysis | X-Insight analysis; action owners consume only accepted later policy | P3 / Research | Proposed; shadow only until separately accepted | ground-truthed multi-Agent fixtures; gap/reorder/semantic-rewrite and legitimate-collaboration negatives; precision/recall/abstention; no score-only hard denial | Not accepted |

User direction on 2026-07-11 records the future exact-cache end state as an
independently deployable `D7-CACHE-001` Cache Service, documented in
`docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md`. This is a future architecture
and backlog decision only: P6-F13 remains not accepted in PRD6, no response
persistence is authorized, and semantic cache still requires a separate later
decision.

P6-F35 did not close P6-F05. The typed configuration registry and budget-alert
key/scope/audit truth chain are now CI verified / closed under P6-F05. P6-F17
supplies separate
server/client correlation plus tenant-scoped Audit readback for an ambiguous
control-plane response; it does not silently make every administrator mutation
replayable or close P6-F20 durability. P6-F35 adds no global-settings UI.

P6-F36 does not reopen P6-F16 or change live admission. It adds no migration,
RLS/composite/alias foreign key, unregistered-model denial, `/v1/models`
semantic change, platform selector UI or target cleanup.

## External-Project Boundary

| ID | Boundary | UniRouter responsibility | External responsibility | PRD6 disposition |
|----|----------|--------------------------|-------------------------|------------------|
| P6-EXT-01 | Sanitized evidence consumption | create authoritative request/authorization/routing/usage/billing/audit facts and expose only an accepted versioned asynchronous contract | independently store, aggregate, investigate or display consumed evidence | boundary only; no external feature is a UniRouter PRD6 row or request-path dependency |
| P6-EXT-02 | Adaptive risk | enforce model actions locally from an accepted audience-specific policy snapshot | X-Insight may aggregate facts and derive risk; a separately owned policy authority may compile attenuation-only snapshots | proposed boundary only; X-Insight is not a synchronous request decision service |
| P6-EXT-03 | Tool/data/export side effects | carry authenticated actor/workflow/capability and enforce only gateway-owned model actions | the actual Tool Gateway, IAM, data, write or export owner mediates its irreversible action | external/action-owner dependency; UniRouter cannot claim enforcement for unseen actions |
| P6-EXT-04 | Fine-tuning and model promotion | route only to an approved registered model/version | model-lifecycle service trains, registers, evaluates and rolls back; human or approved control promotes | external responsibility; no raw production case may auto-train or auto-promote |

## Target-Evidence Rows

The User directed on 2026-07-11 that P6-E01 through P6-E04 remain queued for
later, separately authorized execution after PRD6 source closeout. This
sequencing decision does not satisfy, cancel or promote any evidence row. The
previously accepted 2026-07-17 Gate A file-retention/delete deadline is a
separate timed obligation and is not moved by this queue decision.

| ID | Matter | Owner | Source-code status | Required evidence |
|----|--------|-------|--------------------|-------------------|
| P6-E01 | P5-F35 concrete target route/failover detail | Authorized target operator | PRD5 source scope complete; CI verified | one separately authorized target request detail |
| P6-E02 | Migration down and TLS 443 | Authorized target operator | Not a confirmed source defect | rollback/HTTPS run evidence on selected target |
| P6-E03 | Real upstream and enterprise SSO | User-selected provider/IdP owner | Harness exists; credentials/environment absent | separately authorized real integration evidence |
| P6-E04 | Multi-instance load, capacity and HA/DR | Target infrastructure owner | Product/target prerequisite not selected | selected topology, load model, fault/restore evidence |

## Explicit Non-Scope Guard

No row in this matrix silently accepts Scheduler V2, a plugin/marketplace,
IAM rewrite, managed HA/IaC, public commerce, plaintext response cache, broad
SDK, service-account runtime permission, customer SLA operation, external
exporter/continuous ingestion, or an asynchronous media job implementation.
It also does not accept a synchronous X-Insight policy dependency, automatic
privilege elevation, direct raw-production-case fine-tuning, semantic score as
the only hard-denial condition, or a zero-leakage/truth guarantee.

P6-F20's internal transactional outbox is a UniRouter durability mechanism. It
does not authorize an external exporter or continuous delivery contract.

## Batch 0A Promotion Gates

P6-F01 and P6-F02 may move from focused local evidence to `CI verified` only
after:

1. full backend tests and vet pass;
2. frontend type-check/tests/build confirm repository-wide non-regression;
3. `git diff --check` passes;
4. a `codex/` branch is pushed and a PR is opened;
5. `Backend Tests`, `Frontend Build`, and `Docker Build` pass for that PR.

## Batch 0B Promotion Gates

Each Batch 0B R4 tenant, security or financial PR must additionally:

1. add a regression that fails on the reviewed pre-fix baseline when feasible;
2. test authenticated same-tenant success, cross-tenant/replay/race denial and
   the explicitly allowed platform-admin or idempotent exception;
3. prove denied requests have no database, billing, provider or audit side
   effect beyond the accepted denial evidence;
4. run focused tests, `go test -race ./...`, `go vet ./...`, repository frontend
   non-regression checks and `git diff --check` as applicable;
5. pass `Backend Tests`, `Frontend Build`, and `Docker Build` for the exact PR
   head before promotion.
