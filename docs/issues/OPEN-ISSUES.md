# Open Issues

> Purpose: single active list of unresolved product, engineering, documentation, and verification issues.
> Historical bug narratives may remain in `docs/testing/archive/BUG-LOG.md`, but this file is the active queue.

## Rules

1. Issue IDs are stable and must not be reused.
2. Status values: `Open`, `In progress`, `Fixed awaiting Canary`, `Canary passed`, `Closed`, `Won't fix`.
3. `Closed` issues should stay here only until the next documentation cleanup pass, then may be copied to an archive file.
4. Runtime fixes still require the Codex/OpenClaw protocol: Codex updates `FIX-SIGNAL.md`, OpenClaw writes `VERIFY-RESULT.md`.

## Active Issues

| ID | Severity | Status | Title | Source/Evidence | Next Step |
|----|----------|--------|-------|-----------------|-----------|
| DOC-001 | High | Fixed awaiting Canary | Current status is split across too many docs | `CURRENT-STATUS.md`, `DOC-INVENTORY.md`, and docs indexes now define current vs historical sources | OpenClaw to verify the new entrypoints are sufficient |
| DOC-002 | High | Canary passed | Historical docs contain conflicting bug states and dates | `VERIFY-RESULT.md` reports Fix #4 PASS; stale docs no longer override current truth | Keep current truth in `CURRENT-STATUS.md`, `DOC-INVENTORY.md`, and `PRD1-REMAINING-ACCEPTANCE-PACK.md` |
| DOC-003 | Medium | Fixed awaiting Canary | PRD acceptance checklist is not evidence-backed | `PRD-ACCEPTANCE-MATRIX.md` now separates implemented/unit-tested/E2E evidence gaps | OpenClaw to verify the matrix is usable |
| DOC-004 | Medium | Fixed awaiting Canary | Agent onboarding lacked a single handoff and code map | New `PROJECT-HANDOFF.md`, `CODEBASE-MAP.md`, and `DOC-INVENTORY.md` added | OpenClaw to verify clarity and completeness |
| DOC-005 | High | Canary passed | OpenAPI Admin spec is stale | `VERIFY-RESULT.md` reports Fix #4 PASS; OpenAPI parses with 38 paths and no `/api/v1/admin/*` paths | Keep schema detail expansion for PRD2 typed-client work |
| QA-001 | High | In progress | Phase 1 E2E acceptance still needs current evidence | Alibaba Cloud deployment smoke passed; `PRD1-REMAINING-ACCEPTANCE-PACK.md` records remaining real-provider/stress/failover/browser/Grafana-panel evidence | Configured runtime must complete real-provider and advanced acceptance items |
| QA-002 | Medium | Canary passed | Frontend browser validation is not current | Codex ran local Playwright screenshot validation for Login, Dashboard, Logs, Wallet, API Docs, Channels, and mobile Dashboard/Logs; Canary/OpenClaw verified source/build/test/docs but cannot provide visual acceptance | User/cloud visual review remains the final visual gate |
| ENG-001 | Medium | Canary passed | Frontend client may expose API paths not registered by backend | `VERIFY-RESULT.md` reports Fix #1/#2/#3 PASS; `/me/password` and route parity are verified | Keep route parity checks in future frontend/API changes |
| SEC-001 | High | Canary passed | Channel-key encryption docs conflict with current code | `VERIFY-RESULT.md` reports Fix #4 PASS; XOR note corrected and AES-GCM evidence retained | Keep as PRD1 evidence |
| UI-001 | High | Canary passed | Admin UI quality and frontend architecture need PRD1 closeout review | `VERIFY-RESULT.md` reports Fix #1 PASS for `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md`; visual/manual UI flows remain separate | Keep as PRD1 evidence; schedule separate visual/manual walkthrough |
| UI-002 | High | Canary passed | Frontend API route drift in key and wallet pages | `VERIFY-RESULT.md` reports Fix #2/#3/#4 PASS; `/dashboard/models` and `/wallet/recharge` are cleared from frontend source | Keep route parity checks in future frontend work |
| UI-003 | Medium | Canary passed | Dashboard uses invalid Semi Button theme values and misleading request delta | `VERIFY-RESULT.md` reports Fix #5 PASS and no Semi Button theme warning | Keep as PRD1 evidence |
| PROC-001 | High | Canary passed | Four-role delivery workflow needed in protocol | OpenClaw/Sentinel verification passed; minor README wording was fixed in follow-up | Keep protocol as source of truth |
| PRD1-ENG-003 | Medium | Canary passed | Admin list handlers can hide row scan errors and emit zero-value fields | User reported Canary passed for the cloud API smoke follow-up; `ListAPIKeys` and `ListLogs` use null-safe SQL and return 500 on scan errors; authenticated cloud reads returned 200 on 2026-07-01 | Keep list handler scan-error regressions in tests |
| UI-004 | High | Canary passed | PRD1 browser UI findings from user report | `VERIFY-RESULT.md` reports PRD1 UI redesign closeout 4/4 PASS; concrete bugs fixed in Dashboard, Logs, Wallet, Audit Logs, and Docs route | User/cloud visual acceptance remains separate from Canary |
| UI-005 | High | Canary passed | Overall admin UI lacks a polished design system | `VERIFY-RESULT.md` reports PRD1 UI redesign closeout 4/4 PASS; `frontend/src/ui/` and updated page shells implement a PRD1 visual redesign | Visual judgment remains Codex/user responsibility |
| SEC-002 | Critical | Closed | Public seeded default admin password works on Alibaba Cloud | Redeployed cloud returned 403 for `admin/admin123456` on 2026-07-01; `scripts/prd1-cloud-smoke.sh` passed `default-admin-password-rejected` | Keep the cloud smoke check in release verification |
| PRD1-GW-001 | High | Closed | Gateway relay double-appends `/v1` for channels whose `base_url` already ends in `/v1` | After redeploy, channel test curl example no longer contains `/v1/v1/`; cloud provider E2E no longer returns 404 and now reaches upstream auth with 401 | Keep `upstreamEndpoint()` regression test |
| PRD1-PROVIDER-001 | High | Open | `Mimo Token Plan` upstream key is rejected by provider | Channel 8 test for `mimo-v2.5` returns `success=false`, `status_code=401`, upstream `Invalid API Key`; non-stream gateway returns 502 last upstream status 401; stream then returns 503 because key cooldown excludes the only route; wallet unchanged and `cost_cents=0` | Replace or correct channel 8 upstream key, wait for cooldown or reset key state, then rerun non-stream, stream, wallet/log checks, and only then run 20 VU / 60s load test |
| PRD1-STREAM-001 | High | Fixed awaiting Canary | Streaming response sends UniRouter metadata after `[DONE]` and without `_uniroute` wrapper | Cloud stream E2E for `moonshot-v1-8k` returned 200 but sent `[DONE]` before provider/cost metadata; PRD requires final chunk containing `usage` and `_uniroute` | Canary verify local `relayStreamRequest` fix; redeploy; rerun cloud stream E2E |
| PRD1-LOAD-001 | High | Open | 20 VU / 60s provider load is not stable with current channel capacity | k6 run 1: 100 requests, 3 successes, 97% error; k6 run 2 with stagger: 50 requests, 20 successes, 60% error; Kimi key entered cooldown and later rows were `503 no_route`; wallet spend matched successful calls only | Decide PRD1 acceptance bar: add sufficient provider keys/capacity and rerun, or explicitly defer provider load capacity to PRD2 while retaining spend-safety evidence |
