# PRD6 Acceptance Matrix

> Date: 2026-07-11
> Status: Discovery/Scope Gate
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
| P6-F16 | Organization administrators cannot list/read/create/update/disable/delete another tenant's key, user or project object | UniRouter | P0 / MVP integrity | User decision pending; urgent Batch 0B | handler matrix: same tenant succeeds, cross tenant 403, platform-admin exception; SQL ownership predicates; integration tests | Source-confirmed gap; not remediated |
| P6-F17 | Server operation, client idempotency, trace/span, workflow and Agent identities are separate and tenant scoped | UniRouter | P0 / MVP integrity | User decision pending; urgent Batch 0B | 100-child shared-trace test; same-payload replay; changed-payload 409; cross-org collision negative test; migration up/down | Source-confirmed gap; not remediated |
| P6-F18 | Reservation is pool-safe and reaches exactly one durable terminal state after billable output | UniRouter | P0 / MVP financial integrity | User decision pending; urgent Batch 0B | pool-size barrier; 100-way Settle/Cancel race; post-chunk DB failure/restart; wallet/ledger invariants; multiplier price version | Source-confirmed gaps; not remediated |
| P6-F03 | Retry only retryable provider failures and preserve safe client/policy response semantics | UniRouter | P0 / MVP correctness | Proposed Batch 0C | adapter matrix tests; no retry on 4xx/policy errors; bounded retry on classified transient failures; trace and billing invariants | Not started |
| P6-F04 | Chat and Embeddings have route/failover parity | UniRouter | P0 / MVP correctness | Proposed Batch 0C | multi-route embeddings integration tests; auth/billing/trace parity; all-routes-fail behavior | Not started |
| P6-F05 | Effective retry/circuit/cooldown configuration is wired, versioned and observable | UniRouter | P0 / MVP correctness | Proposed Batch 0C | config default/override tests; no hidden hard-coded divergence; route-decision config evidence | Not started |
| P6-F06 | Organization multiplier cannot create an uncollected under-freeze settlement | UniRouter | P0 / MVP correctness | Proposed Batch 0C | insufficient-freeze tests; atomic debit/loss rules; cancel/refund and concurrent settlement tests | Not started |
| P6-F07 | Shared RPM/RPS/TPM/concurrency policy with explicit selectors and failure semantics | UniRouter | P1 / MVP | User decision pending | multi-instance load/fault tests; shadow/enforce modes; policy-version trace; bounded metrics; fail-mode tests | Not accepted |
| P6-F08 | Routing Policy V1 provides explainable priority/weight/retry/cooldown/failover | UniRouter | P1 / MVP | User decision pending | deterministic policy fixtures; Chat/Embeddings parity; fault injection; candidate/exclusion/fallback evidence | Not accepted |
| P6-F09 | Provider cache-read/cache-create usage is normalized, priced and reconciled | UniRouter | P1 / MVP | User decision pending | version-fixed provider fixtures; source/price metadata; ledger/log/UI and reconciliation tests; no fake cache-hit billing | Not accepted |
| P6-F10 | Versioned Guardrail Kernel covers input and non-stream output | UniRouter | P1 / MVP | User decision pending | per-scope policy tests; redact/block/audit modes; external-detector failure tests; no raw-payload persistence | Not accepted |
| P6-F11 | Stream policy hook has bounded disclosure, protocol termination, cancel and settlement behavior | UniRouter | P1 / MVP | User decision pending | cross-chunk/oversized/adversarial SSE tests; measured exposure; client disconnect/upstream cancel; exactly-once settlement | Not accepted |
| P6-F19 | Short Agent capabilities attenuate authority and share ancestor budget/revocation | UniRouter | P1 / MVP | User decision pending | offline verification; depth/fan-out/model/cost/rate/concurrency attenuation; 100-child aggregate cap; refresh jitter/singleflight; all-replica revocation SLO | Not accepted |
| P6-F20 | Minimal usage/finalization is durable while detailed evidence and notifications use bounded replayable workers | UniRouter | P1 / MVP | User decision pending | process-kill/outbox replay; duplicate/reorder tests; billing/audit priority; bounded memory/goroutines; trace-lag SLO | Not accepted |
| P6-F21 | Shared admission is workload weighted, replica invariant and fair to short/long work | UniRouter | P1 / MVP | User decision pending | 2/3-replica exact concurrency; lease expiry/crash; Redis latency/outage; workload-class fairness; bounded wait and `Retry-After` | Not accepted |
| P6-F12 | External secret references, versions, rotation and rollback | UniRouter plus selected secret backend | P2 / Advanced | Deferred pending backend decision | rotation, stale-worker, outage, audit and rollback evidence | Not accepted |
| P6-F13 | Opt-in exact/semantic response cache is tenant-isolated and purgeable | UniRouter | P2 / Advanced | Deferred pending value/security decision | isolation, auth, TTL, purge, stampede, poisoning, encryption and billing red-team tests | Not accepted |
| P6-F14 | Latency/cost/rate-aware and canary routing | UniRouter | P2 / Advanced | Deferred until Routing V1 telemetry is trustworthy | shadow comparison, SLO, rollback and route-explainability evidence | Not accepted |
| P6-F15 | Policy simulation, BYO guardrails and config rollback | UniRouter; external evidence consumers remain optional | P2 / Advanced | Deferred pending policy lifecycle design | version/RBAC/audit, dry-run diff, compatibility and rollback tests | Not accepted |
| P6-F22 | Asynchronous object-reference image/video job plane | UniRouter | P2 / Advanced | Deferred pending explicit media roadmap decision | tenant-scoped upload/job/status/cancel; queue lease/retry; idempotent completion; retention; final usage/settlement; load isolation | Not accepted |

## External-Project Boundary

| ID | Boundary | UniRouter responsibility | External responsibility | PRD6 disposition |
|----|----------|--------------------------|-------------------------|------------------|
| P6-EXT-01 | Sanitized evidence consumption | create authoritative request/authorization/routing/usage/billing/audit facts and expose only an accepted versioned asynchronous contract | independently store, aggregate, investigate or display consumed evidence | boundary only; no external feature is a UniRouter PRD6 row or request-path dependency |

## Target-Evidence Rows

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
