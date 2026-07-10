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
| P6-F01 | Expired half-open channels regain bounded probe eligibility | UniRouter | P0 / MVP correctness | Accepted Batch 0A | query-semantic regression; router unit and race tests; full backend test; CI | Local verified; CI pending |
| P6-F02 | Redis API-key RPM/RPS checks are atomic and enforce N allowed / N+1 rejected | UniRouter | P0 / MVP correctness | Accepted Batch 0A | strict QPS/RPM boundary; concurrent exact-admission test; canceled-context test; Redis-error fail-open compatibility; race/full backend/CI | Local verified; CI pending |
| P6-F03 | Retry only retryable provider failures and preserve safe client/policy response semantics | UniRouter | P0 / MVP correctness | Proposed Batch 0B | adapter matrix tests; no retry on 4xx/policy errors; bounded retry on classified transient failures; trace and billing invariants | Not started |
| P6-F04 | Chat and Embeddings have route/failover parity | UniRouter | P0 / MVP correctness | Proposed Batch 0B | multi-route embeddings integration tests; auth/billing/trace parity; all-routes-fail behavior | Not started |
| P6-F05 | Effective retry/circuit/cooldown configuration is wired, versioned and observable | UniRouter | P0 / MVP correctness | Proposed Batch 0B | config default/override tests; no hidden hard-coded divergence; route-decision config evidence | Not started |
| P6-F06 | Organization multiplier cannot create an uncollected under-freeze settlement | UniRouter | P0 / MVP correctness | Proposed Batch 0B | insufficient-freeze tests; atomic debit/loss rules; cancel/refund and concurrent settlement tests | Not started |
| P6-F07 | Shared RPM/RPS/TPM/concurrency policy with explicit selectors and failure semantics | UniRouter | P1 / MVP | User decision pending | multi-instance load/fault tests; shadow/enforce modes; policy-version trace; bounded metrics; fail-mode tests | Not accepted |
| P6-F08 | Routing Policy V1 provides explainable priority/weight/retry/cooldown/failover | UniRouter | P1 / MVP | User decision pending | deterministic policy fixtures; Chat/Embeddings parity; fault injection; candidate/exclusion/fallback evidence | Not accepted |
| P6-F09 | Provider cache-read/cache-create usage is normalized, priced and reconciled | UniRouter | P1 / MVP | User decision pending | version-fixed provider fixtures; source/price metadata; ledger/log/UI and reconciliation tests; no fake cache-hit billing | Not accepted |
| P6-F10 | Versioned Guardrail Kernel covers input and non-stream output | UniRouter | P1 / MVP | User decision pending | per-scope policy tests; redact/block/audit modes; external-detector failure tests; no raw-payload persistence | Not accepted |
| P6-F11 | Stream policy hook has bounded disclosure, protocol termination, cancel and settlement behavior | UniRouter | P1 / MVP | User decision pending | cross-chunk/oversized/adversarial SSE tests; measured exposure; client disconnect/upstream cancel; exactly-once settlement | Not accepted |
| P6-F12 | External secret references, versions, rotation and rollback | UniRouter plus selected secret backend | P2 / Advanced | Deferred pending backend decision | rotation, stale-worker, outage, audit and rollback evidence | Not accepted |
| P6-F13 | Opt-in exact/semantic response cache is tenant-isolated and purgeable | UniRouter | P2 / Advanced | Deferred pending value/security decision | isolation, auth, TTL, purge, stampede, poisoning, encryption and billing red-team tests | Not accepted |
| P6-F14 | Latency/cost/rate-aware and canary routing | UniRouter | P2 / Advanced | Deferred until Routing V1 telemetry is trustworthy | shadow comparison, SLO, rollback and route-explainability evidence | Not accepted |
| P6-F15 | Policy simulation, BYO guardrails and config rollback | UniRouter; X-Insight may consume evidence | P2 / Advanced | Deferred pending policy lifecycle design | version/RBAC/audit, dry-run diff, compatibility and rollback tests | Not accepted |

## Other-Project Rows

| ID | Capability | Owner | PRD6 disposition | Evidence boundary |
|----|------------|-------|------------------|-------------------|
| P6-XI-01 | Execution search, execution detail/timeline, node detail, source/provenance and capture gaps | X-Insight | X-Insight Phase 2 accepted priority; not a UniRouter feature row | X-Insight source/tests/UI evidence |
| P6-XI-02 | Durable import job and deep link | X-Insight or future joint contract | Not implemented; no UniRouter dependency | future versioned contract and X-Insight runtime evidence |
| P6-XI-03 | Automated/authenticated receipt transport | Joint contract, X-Insight receiver ownership | Not implemented; v1 remains operator mediated | explicit auth/retry/idempotency contract and external integration evidence |
| P6-XI-04 | Retention/delete/audit workflow, stable public adapters, continuous ingestion | X-Insight or future joint contract | Not implemented; outside current PRD6 proposal | separate X-Insight scope and target evidence |

## Target-Evidence Rows

| ID | Matter | Owner | Source-code status | Required evidence |
|----|--------|-------|--------------------|-------------------|
| P6-E01 | P5-F35 concrete target route/failover detail | Authorized target operator | PRD5 source scope complete; CI verified | one separately authorized target request detail |
| P6-E02 | Migration down and TLS 443 | Authorized target operator | Not a confirmed source defect | rollback/HTTPS run evidence on selected target |
| P6-E03 | Real upstream and enterprise SSO | User-selected provider/IdP owner | Harness exists; credentials/environment absent | separately authorized real integration evidence |
| P6-E04 | Multi-instance load, capacity and HA/DR | Target infrastructure owner | Product/target prerequisite not selected | selected topology, load model, fault/restore evidence |

## Explicit Non-Scope Guard

No row in this matrix silently accepts Scheduler V2, a plugin/marketplace,
IAM rewrite, managed HA/IaC, public commerce, plaintext cache, broad SDK,
service-account runtime permission, customer SLA operation, exporter/outbox, or
continuous ingestion.

## Batch 0A Promotion Gates

P6-F01 and P6-F02 may move from focused local evidence to `CI verified` only
after:

1. full backend tests and vet pass;
2. frontend type-check/tests/build confirm repository-wide non-regression;
3. `git diff --check` passes;
4. a `codex/` branch is pushed and a PR is opened;
5. `Backend Tests`, `Frontend Build`, and `Docker Build` pass for that PR.
