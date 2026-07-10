# PRD6 Batch 0 Baseline And Competitor Audit Evidence

> Date: 2026-07-11
> Evidence level: Local verified plus cited CI/target/external evidence
> Mutation boundary: repository read/test work only; no deployment, target
> mutation, paid request, credential change, exporter/outbox enablement, or
> expanded data access

## 1. Git And CI Baseline

The following was verified after fetching `origin/main`:

```text
UniRouter HEAD       deacae2d4164b98c5241cf72844f09f53ca7510d
UniRouter main       deacae2d4164b98c5241cf72844f09f53ca7510d
UniRouter origin/main deacae2d4164b98c5241cf72844f09f53ca7510d
worktree             clean
```

GitHub readback confirmed PR #136, #137, and #138 were merged. Each showed
successful required checks:

- `Backend Tests`
- `Frontend Build`
- `Docker Build`

The current base is PR #138's merge result.

PRD6 Draft PR #139 ran the same three required checks for Batch 0/0A. Backend
Tests, Frontend Build, and Docker Build all passed for commit `000ea64` before
the evidence-status-only follow-up commit.

## 2. Fresh Local Verification

Commands executed against the clean base on 2026-07-11:

| Command | Result |
|---------|--------|
| `go test ./... -count=1` | PASS |
| `go vet ./...` | PASS |
| `npm --prefix frontend run type-check` | PASS |
| `npm --prefix frontend test -- --run` | PASS, 25 files / 125 tests |
| `npm --prefix frontend run build` | PASS |
| X-Insight `go test ./... -count=1` | PASS |

Frontend tests retained existing React Router, Semi Form, and
`findDOMNode` warnings. The frontend build retained the existing large-chunk
warning. Neither was promoted to a new correctness failure by this audit.

## 3. PRD5 And X-Insight Reconciliation

Confirmed repository facts:

- `docs/PRD-Phase5.md` classifies PRD5 as `Accepted Scope Complete` and keeps
  deferred, rejected, prerequisite-gated, X-Insight-owned, and target-evidence
  items outside that completion claim.
- `docs/ops/PRD5-ACCEPTANCE-MATRIX.md` contains 36 rows; each has completion
  evidence or an explicit non-implementation decision.
- `docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md`
  accepts only operator-mediated `xinsight.import-receipt.v1` sidecar
  reconciliation in v1.
- X-Insight Phase 1 remains complete. X-Insight commits after its prior
  capability baseline change planning and Phase 2 posture, not the accepted
  runtime contract implementation.
- X-Insight Phase 2 prioritizes execution search, execution detail/timeline,
  node detail, source/provenance, and capture-gap display. X-Insight owns that
  read-side product surface; UniRouter is not synchronously dependent on it.

The older statement in
`docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md`
that X-Insight still needed to answer is superseded by the dated 2026-07-11
runtime reconciliation. It is historical sequence, not a current conflict.

## 4. Gate A Retention Verification

The exact files and locally recomputed SHA-256 values are:

| Exact path | SHA-256 |
|------------|---------|
| `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-snapshot-20260710.json` | `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8` |
| `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-b1w-snapshot-20260710.json` | `3c0b13241eb369ca096115f420e73d1ae35c3e6ac4b794cb997f56cf09c7339c` |

Both match the accepted decision. They must be removed one exact path at a time
by 2026-07-17 after the required copy inventory. No deletion was due or
performed in this batch.

## 5. Confirmed UniRouter Findings

These findings were established from current code and tests. They are not
inferences from competitor marketing.

| ID | Confirmed finding | Current evidence | Classification |
|----|-------------------|------------------|----------------|
| P6-ROUTE-001 | At the base commit, a half-open channel was eligible only while `half_open_until > NOW()`. After the timestamp expired it could remain permanently excluded. | Base/current query location `internal/gateway/router.go:43`; future deadline write `internal/gateway/router.go:147-170`; Batch 0A regression `internal/gateway/router_mock_test.go:110-127` | Existing correctness defect; Batch 0A |
| P6-RL-001 | At the base commit, Redis RPM/RPS counted and added in separate operations and rejected only when the pre-add count was above the limit. This allowed `N+1` and was non-atomic across instances. | Batch 0A atomic script and request context `internal/middleware/ratelimit.go:17-38,63-80`; exact and concurrent regressions `internal/middleware/ratelimit_test.go:160-305` | Existing correctness defect; Batch 0A |
| P6-ROUTE-002 | Relay carries adapter `Retryable` metadata but the current non-200 loop does not use it to stop inappropriate failover; the final response can collapse to 502. | `internal/adapters/types.go:50-56`; `internal/gateway/relay.go:455-484,500-512` | Confirmed defect; proposed Batch 0B |
| P6-ROUTE-003 | Embeddings takes one route and lacks Chat-equivalent provider failover. | `internal/gateway/relay.go:1213-1227,1248-1280` | Confirmed endpoint parity gap; proposed Batch 0B |
| P6-CONFIG-001 | Config exposes retry/circuit/cooldown fields while relay/router paths retain hard-coded values; channel RPM/TPM fields are not enforced by the routing query. | `internal/config/config.go:92-97`; hard-coded behavior `internal/gateway/relay.go:443-473` and `internal/gateway/router.go:71-73,116-118`; unused route limits `internal/models/models.go:105-123` | Confirmed config-truth gap; proposed Batch 0B |
| P6-STREAM-001 | Streaming has no general output-policy hook. Scanner error handling and the 64 KiB limit do not provide a proven safe oversized-chunk termination contract. | scan/flush path `internal/gateway/relay.go:688-750`; loop exit proceeds to settlement without an explicit `scanner.Err()` branch at `internal/gateway/relay.go:867-900` | Confirmed safety/reliability gap; proposed MVP design |
| P6-GUARD-001 | Guarding is a global input sensitive-word DFA only; no PII policy chain, non-stream output guard, or live stream cut-off policy exists. | input-only gateway check `internal/gateway/relay.go:312-320`; DFA implementation/update `internal/filter/filter.go:43-95` | Confirmed product gap; proposed MVP |
| P6-BILL-001 | Usage normalizes prompt/completion/total tokens but not provider cache-read/cache-write token classes; cache-hit metadata is not authoritative billing input. | normalized usage `internal/adapters/types.go:33-38`; hard-coded response metadata `internal/gateway/relay.go:598-608,954-978` | Confirmed product gap; proposed MVP |
| P6-BILL-002 | Billing freeze is based on base price while settlement applies the organization multiplier; an under-freeze can become loss evidence without an additional debit. | base-price estimate `internal/billing/billing.go:114-124`; settle/loss behavior `internal/billing/billing.go:240-304`; organization-aware actual cost `internal/gateway/relay.go:993-1006` | Confirmed correctness risk; proposed Batch 0B |
| P6-LIMIT-002 | API-key RPM/RPS can use Redis, but TPM is not enforced and global/organization concurrency is process-local. Redis errors currently fail open. | `internal/middleware/ratelimit.go:40-98`; process-local admission `internal/middleware/admission.go:8-19,126-133`; API-key model has RPM/QPS but no TPM `internal/models/models.go:141-155` | Confirmed distributed-policy gap; proposed MVP |

## 6. Hypotheses And Known Unknowns

The following require benchmarks, failure injection, target evidence, or a
product decision and must not be reported as confirmed failures:

- acceptable limiter drift and latency at the intended replica count and load;
- exact versus best-effort output TPM semantics before a stream completes;
- guardrail false-positive/false-negative and p95 latency budgets;
- acceptable bytes or tokens exposed before a stream policy blocks output;
- provider-specific cache usage fidelity and price-source drift;
- response-cache value versus tenant-leakage, poisoning, stampede, and deletion
  risk;
- desired hard-quota behavior when Redis is slow or unavailable;
- real provider capacity, TLS 443, migration down, SSO, HA/DR, and concrete
  target route-detail behavior.

## 7. Evidence Boundary

This file proves the repository baseline, fresh local checks, document/CI
readback, snapshot presence/hash, and current-code findings. It does not prove a
new target deployment, production readiness, real paid-provider behavior, or a
new X-Insight runtime contract.
