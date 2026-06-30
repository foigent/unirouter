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
| QA-002 | Medium | Fixed awaiting Canary | Frontend browser validation is not current | Codex ran local Playwright screenshot validation for Login, Dashboard, Logs, Wallet, API Docs, Channels, and mobile Dashboard/Logs; Canary/OpenClaw cannot provide visual acceptance | OpenClaw to verify source/build/test and artifact paths; user may still perform human visual review |
| ENG-001 | Medium | Canary passed | Frontend client may expose API paths not registered by backend | `VERIFY-RESULT.md` reports Fix #1/#2/#3 PASS; `/me/password` and route parity are verified | Keep route parity checks in future frontend/API changes |
| SEC-001 | High | Canary passed | Channel-key encryption docs conflict with current code | `VERIFY-RESULT.md` reports Fix #4 PASS; XOR note corrected and AES-GCM evidence retained | Keep as PRD1 evidence |
| UI-001 | High | Canary passed | Admin UI quality and frontend architecture need PRD1 closeout review | `VERIFY-RESULT.md` reports Fix #1 PASS for `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md`; visual/manual UI flows remain separate | Keep as PRD1 evidence; schedule separate visual/manual walkthrough |
| UI-002 | High | Canary passed | Frontend API route drift in key and wallet pages | `VERIFY-RESULT.md` reports Fix #2/#3/#4 PASS; `/dashboard/models` and `/wallet/recharge` are cleared from frontend source | Keep route parity checks in future frontend work |
| UI-003 | Medium | Canary passed | Dashboard uses invalid Semi Button theme values and misleading request delta | `VERIFY-RESULT.md` reports Fix #5 PASS and no Semi Button theme warning | Keep as PRD1 evidence |
| PROC-001 | High | Canary passed | Four-role delivery workflow needed in protocol | OpenClaw/Sentinel verification passed; minor README wording was fixed in follow-up | Keep protocol as source of truth |
| PRD1-ENG-003 | Medium | Canary passed | Admin list handlers can hide row scan errors and emit zero-value fields | User reported Canary passed for the cloud API smoke follow-up; `ListAPIKeys` and `ListLogs` use null-safe SQL and return 500 on scan errors | Deploy this build before rechecking cloud responses |
| UI-004 | High | Fixed awaiting Canary | PRD1 browser UI findings from user report | `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md` classifies 7 findings; concrete bugs fixed in Dashboard, Logs, Wallet, Audit Logs, and Docs route | OpenClaw to verify source/tests/docs; browser visual acceptance remains separate |
| UI-005 | High | Fixed awaiting Canary | Overall admin UI lacks a polished design system | `docs/design/PRD1-UI-REDESIGN-SPEC.md`, `frontend/src/ui/`, and updated page shells implement a PRD1 visual redesign; Codex visual screenshots are under local `output/playwright/` | OpenClaw to verify source/build/test/docs; visual judgment remains Codex/user responsibility |
