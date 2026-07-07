# VERIFY-RESULT.md - PRD4 Batch 1H Real Upstream Evidence Harness

**Date:** 2026-07-07
**Signal:** `docs/agent-workflows/FIX-SIGNAL.md`
**Issue:** `PRD4-UPSTREAM-001`
**Matrix Row:** `P4-F08`
**Conclusion: ✅ PASS (11/11)**

## Verification Summary

| # | Acceptance Criteria | Result | Evidence |
|---|---------------------|--------|----------|
| 1 | Batch 1G status reconciled as Canary passed and Sentinel merged | ✅ PASS | `CURRENT-STATUS.md:4` — "Batch 1G External Monitoring Integration passed Canary 9/9, and Sentinel merge is reflected by `bb291a4` / PR #90" |
| 2 | Harness files exist and are indexed from current docs | ✅ PASS | 4 files confirmed: `scripts/prd4-real-upstream-smoke.sh`, `scripts/prd4_real_upstream_seed.go`, `scripts/prd4_real_upstream_smoke.mjs`, `docs/tech-ref/23-prd4-real-upstream-evidence.md`; indexed in `CODEBASE-MAP.md:71-73` |
| 3 | Preflight mode returns safe JSON without provider credentials | ✅ PASS | `scripts/prd4-real-upstream-smoke.sh --preflight` returns `{"status":"SKIP","missing":["OPENROUTER_API_KEY",...],"would_mutate_database":false,"would_call_real_providers":false}` |
| 4 | Preflight explicitly reports no DB mutation or provider calls | ✅ PASS | Preflight JSON: `"would_mutate_database":false,"would_call_real_providers":false` |
| 5 | Real mode requires exact mutation approval token and required credentials | ✅ PASS | `prd4_real_upstream_smoke.mjs:9` — `requiredMutationValue = 'approved-target-real-upstream'`; `prd4_real_upstream_seed.go:22` — `prd4UpstreamAllowMutation = "approved-target-real-upstream"`; seed fails without it: "UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream is required before seeding" |
| 6 | Seed step guarded before DB mutation and uses backend-compatible encryption/API-key hashing | ✅ PASS | `go run ./scripts/prd4_real_upstream_seed.go` exits with guard error before DB connection; seed requires `ENCRYPTION_KEY`/`UNIROUTE_SMOKE_ENCRYPTION_KEY` and `API_KEY_HASH_SECRET`/`UNIROUTE_SMOKE_API_KEY_HASH_SECRET`/`JWT_SECRET` |
| 7 | Runner checks backend readiness before seeding | ✅ PASS | `prd4_real_upstream_smoke.mjs` calls backend readiness check before invoking seed step |
| 8 | Runner validates gateway success, call logs, settled cost, usage source, and trace events | ✅ PASS | `prd4_real_upstream_smoke.mjs` validates non-stream/stream responses, checks call log entries, billing settlement, usage source, and trace event names for both OpenRouter-compatible and direct Anthropic-native paths |
| 9 | Evidence includes commit/timestamp/request/log/trace/cost metadata and rejects secret leakage | ✅ PASS | Evidence output fields: git_commit, timestamp, request_id, log_id, trace_event_names, cost_cents, latency_ms, ttft_ms, backend_url; explicitly excludes API keys, raw API keys, prompts, response bodies |
| 10 | Docs state harness/guardrails only; no E2E accepted or Production verified without approved evidence | ✅ PASS | `23-prd4-real-upstream-evidence.md:111` — "Batch 1H verifies the harness and its guardrails. It does not move P4-F08 to E2E accepted"; `OPEN-ISSUES.md:69` — "Do not mark P4-F08 E2E accepted or Production verified until approved real-provider non-stream and stream checks pass"; `CURRENT-STATUS.md:40` — "No real upstream E2E acceptance is claimed" |
| 11 | Syntax/preflight checks, broad Go/frontend tests, build, grep, git diff --check pass | ✅ PASS | Preflight: SKIP with safe JSON; `node --check`: clean; Go focused 4/4 PASS; full Go 22 packages PASS; frontend 114 tests (24 files) PASS; build 8.32s; `git diff --check` clean |

## Detailed Verification

### Preflight Safety (AC3/AC4)
```
$ bash scripts/prd4-real-upstream-smoke.sh --preflight
{
  "status": "SKIP",
  "scope": "PRD4 Batch 1H real upstream evidence harness preflight",
  "backend_url": "http://localhost:8080",
  "git_commit": "bb291a47278a7ad38516655a8186665ccece3183",
  "missing": ["OPENROUTER_API_KEY", "OPENROUTER_MODEL", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL",
              "UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION", "ENCRYPTION_KEY or UNIROUTE_SMOKE_ENCRYPTION_KEY",
              "API_KEY_HASH_SECRET or UNIROUTE_SMOKE_API_KEY_HASH_SECRET or JWT_SECRET"],
  "required_mutation_value": "approved-target-real-upstream",
  "would_mutate_database": false,
  "would_call_real_providers": false
}
```

### Mutation Guard (AC5/AC6)
```
$ go run ./scripts/prd4_real_upstream_seed.go
seed failed: UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream is required before seeding real-upstream evidence fixtures
exit status 1
```
Fails before any DB connection attempt.

### Node Syntax Check
```
$ node --check scripts/prd4_real_upstream_smoke.mjs
(no output = clean)
```

### Evidence Sanitization (AC9)
Evidence writer outputs: commit SHA, ISO timestamp, request_id, log_id, trace event names, cost_cents, latency_ms, ttft_ms, backend_url, provider/model/adapter/channel metadata.
Evidence writer excludes: OPENROUTER_API_KEY, ANTHROPIC_API_KEY, generated UniRouter raw API keys, prompt text, full response bodies.

## New Issues Found

None.

## Verdict

**PASS.** All 11 acceptance criteria verified. The harness is ready for use when approved target-environment credentials are supplied. `P4-F08` remains below `E2E accepted` / `Production verified` until real-provider checks pass.
