# FIX-SIGNAL.md - PRD4 Batch 1H Real Upstream Evidence Harness

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary
**Issue:** `PRD4-UPSTREAM-001`
**Matrix Row:** `P4-F08`

## Verification Request

Please verify PRD4 Batch 1H: Real Upstream Evidence Harness.

This batch adds a guarded evidence path for PRD4 real upstream target-environment
proof. It does not claim real upstream E2E acceptance in the current workspace,
because provider credentials and approved target-environment access are not
present.

## Scope To Verify

Harness files:

- `scripts/prd4-real-upstream-smoke.sh`
- `scripts/prd4_real_upstream_seed.go`
- `scripts/prd4_real_upstream_smoke.mjs`
- `docs/tech-ref/23-prd4-real-upstream-evidence.md`

Expected behavior:

- `scripts/prd4-real-upstream-smoke.sh --preflight` is safe without credentials.
- Preflight reports `status: SKIP` when required credentials/secrets are missing.
- Preflight does not mutate the database and does not call real providers.
- Real mode is blocked unless
  `UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream`.
- Real mode requires:
  - `OPENROUTER_API_KEY`
  - `OPENROUTER_MODEL`
  - `ANTHROPIC_API_KEY`
  - `ANTHROPIC_MODEL`
  - backend-matching `ENCRYPTION_KEY` or `UNIROUTE_SMOKE_ENCRYPTION_KEY`
  - backend-matching `API_KEY_HASH_SECRET`,
    `UNIROUTE_SMOKE_API_KEY_HASH_SECRET`, or `JWT_SECRET`
- Real mode checks backend readiness before seeding target-evidence fixtures.
- Real mode, when approved credentials are available, covers:
  - OpenRouter-compatible non-stream.
  - OpenRouter-compatible stream.
  - Direct Anthropic-native non-stream.
  - Direct Anthropic-native stream.
- Evidence includes provider/model/adapter/channel, request ids, log ids, usage
  source, settled cost, latency, TTFT, trace event names, backend URL, timestamp,
  and current git commit.
- Evidence must not include upstream API keys, generated UniRouter raw API keys,
  prompts, or full response bodies.

Docs/status:

- `P4-F07` / `PRD4-MONITORING-001` is marked Canary passed after Batch 1G PASS
  9/9 and Sentinel merge `bb291a4` / PR #90.
- `P4-F08` / `PRD4-UPSTREAM-001` is marked fixed awaiting Canary for harness
  verification only.
- `P4-F08` must not be marked `E2E accepted` or `Production verified` until both
  OpenRouter-compatible and direct Anthropic-native real-provider non-stream and
  stream checks pass in an approved target environment.

## Non-Scope

Do not expect this batch to implement:

- Real upstream provider evidence without approved credentials.
- Any automatic provider calls during preflight.
- Cleanup/deletion of target fixtures.
- Real SSO / Logto browser evidence.
- OIDC/IAM rewrite.
- OpenTelemetry export, trace retention cleanup, or full observation platform.
- Scheduler V2, model-capacity dispatch, token reservation, or batch routing.
- Budget hard-block admission behavior.
- Runtime plugin loading, manifest parsing, sandboxing, marketplace, third-party
  installation, or PlungeIn Mutate behavior.

## Required Verification Commands

```bash
rg -n "prd4-real-upstream-smoke|prd4_real_upstream_seed|prd4_real_upstream_smoke|OPENROUTER_MODEL|ANTHROPIC_MODEL|approved-target-real-upstream|PRD4 Batch 1H|P4-F08|PRD4-UPSTREAM-001" scripts docs/tech-ref/23-prd4-real-upstream-evidence.md docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PRD-Phase4.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/tech-ref/README.md docs/CODEBASE-MAP.md
scripts/prd4-real-upstream-smoke.sh --preflight
node --check scripts/prd4_real_upstream_smoke.mjs
go test ./internal/adapters ./internal/gateway ./internal/admin ./cmd/uniroute -count=1
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Optional negative guard check:

```bash
go run ./scripts/prd4_real_upstream_seed.go
```

Expected result: the command fails before connecting to the database unless
`UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream` is set.

## Acceptance Criteria

1. Batch 1G status is reconciled as Canary passed and Sentinel merged in current
   PRD4 docs.
2. The Batch 1H harness files exist and are indexed from current docs.
3. Preflight mode returns safe JSON and does not require provider credentials.
4. Preflight mode explicitly reports that it will not mutate the database or
   call providers.
5. Real mode requires the exact mutation approval token and required credentials.
6. The seed step is guarded before any DB mutation and uses backend-compatible
   encryption/API-key hashing inputs.
7. The runner checks backend readiness before seeding.
8. The runner validates gateway success, call logs, settled cost, usage source,
   and trace events for the approved real-provider path.
9. Evidence output includes commit/timestamp/request/log/trace/cost metadata and
   rejects secret leakage.
10. Docs clearly state that Batch 1H verifies harness and guardrails only; no
    real upstream E2E acceptance or Production verified status is claimed without
    approved target-provider evidence.
11. Focused syntax/preflight checks, broad Go/frontend tests, frontend build,
    docs grep, and `git diff --check` pass.
